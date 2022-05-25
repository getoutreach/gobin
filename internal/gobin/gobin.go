// Copyright 2022 Outreach Corporation. All Rights Reserved.

// Description: See package description for one-file package.

// Package gobin implements retrieving, downloading, and running go binaries
// using a remote path and version.
package gobin

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/pkg/errors"
	"golang.org/x/tools/go/vcs"
)

// golangVerOutReg parses `go version`.
var golangVerOutReg = regexp.MustCompile(` go(\d.+) `)

// removeVersion removes version from a module path.
var removeVersion = regexp.MustCompile(`^/(v\d+)/`)

// toolVersions is the file name for asdf tool versions declarations.
const toolVersions = ".tool-versions"

// Module represents a Go module.
type Module struct {
	// The unmodified/parsed import path, includes the command, if present
	OriginalImport string

	// Path is the module path
	Path string

	// Repo is a full VCS url that can be used to download this module
	Repo string

	// Version is a git ref that this correlates to (generally @vx.x.x)
	Version string

	repo *vcs.RepoRoot
}

// GetCommandPath attempts to resolve the directory for building from source
// from a import path. This handles removal of major versions.
func (m *Module) GetCommandPath() string {
	// Trim a major version, if it exists, from the path
	cmdPath := strings.TrimPrefix(m.OriginalImport, m.Path)
	cmdPath = removeVersion.ReplaceAllLiteralString(cmdPath, "")
	return cmdPath
}

// GetBinaryCache returns the path that a binary should located, if it's been built before
func (m *Module) GetBinaryCache() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	goVer, err := getCurrentGoVersion()
	if err != nil {
		return "", err
	}

	binDir := filepath.Join(homeDir, ".outreach", ".cache", "gobin", "binaries", goVer, m.Path, "@v", m.Version, m.GetCommandPath())
	binPath := filepath.Join(binDir, filepath.Base(m.OriginalImport))
	return binPath, nil
}

// Run actually performs the binary retrieval, installation, and running.
func Run(ctx context.Context, fullPath, buildDir, buildPath string, printPath bool) error { //nolint:funlen,lll // Why: Not necessary to break up.
	ref := ""
	verSplit := strings.SplitN(fullPath, "@", 2)
	if len(verSplit) == 2 {
		fullPath = verSplit[0]
		ref = verSplit[1]
	}

	if ref == "" {
		return fmt.Errorf("expected a version, e.g. %s@vX.X.X", fullPath)
	}

	root, err := vcs.RepoRootForImportPath(fullPath, false)
	if err != nil {
		return errors.Wrap(err, "failed to parse import path")
	}

	m := &Module{
		OriginalImport: fullPath,
		Repo:           root.Repo,
		Path:           root.Root,
		Version:        ref,
		repo:           root,
	}

	// If we already built it, return it
	binPath, err := m.GetBinaryCache()
	if err != nil {
		return err
	}

	// Build it, because it wasn't found
	if _, err := os.Stat(binPath); err != nil {
		// Otherwise clone the repo, and build it
		cleanupFn, sourceDir, err := downloadRepository(ctx, m)
		fmt.Printf("Downloaded repository at %q\n", sourceDir)

		if err != nil {
			return err
		}

		buildDirPath := filepath.Join(
			strings.TrimSuffix(sourceDir, string(filepath.Separator)),
			strings.TrimPrefix(buildDir, string(filepath.Separator)))

		err = buildRepository(ctx, sourceDir, buildDirPath, buildPath, m)
		if err != nil {
			return err
		}

		// only cleanup the repository if we succeeded, otherwise leave
		// so it can be, potentially, inspected.
		defer cleanupFn()
	}

	if printPath {
		fmt.Println(binPath)
		return nil
	}

	// do nothing, we don't have support for -run right now.
	return nil
}

// getCurrentGoVersion retrieves the current go version running on the host.
func getCurrentGoVersion() (string, error) {
	cmd := exec.Command("go", "version")
	b, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Fprintln(os.Stderr, string(b))
		return "", errors.Wrap(err, "failed to detect go version")
	}

	ver := golangVerOutReg.FindStringSubmatch(string(b))
	if ver == nil {
		return "", fmt.Errorf("failed to detect go version")
	}
	return ver[1], nil
}

// buildRepository builds a go repository.
func buildRepository(ctx context.Context, rootPath, buildDirPath, buildPath string, m *Module) error {
	binPath, err := m.GetBinaryCache()
	if err != nil {
		return err
	}

	// ensure we have a .tool-versions in the directory we're temporarily
	// building this source code in, based off of the current versions of tools
	// if we have asdf installed.
	if err := generateToolVersions(ctx, rootPath); err != nil {
		return errors.Wrap(err, "failed to setup asdf integration")
	}

	if buildPath == "" {
		buildPath = fmt.Sprintf(".%s%s%s",
			string(filepath.Separator),
			strings.TrimPrefix(m.GetCommandPath(), string(filepath.Separator)),
			string(filepath.Separator))
	}

	args := []string{"build", "-o", binPath, buildPath}
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = buildDirPath

	b, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("ran command \"go %s\"\n", strings.Join(args, " "))
		fmt.Fprintln(os.Stderr, string(b))
	}
	return err
}

// generateToolVersions generates a .tool-versions off of the output of asdf current
// or, if asdf is not installed, noops instead. asdf current is used to allow us to keep
// normal .tool-verisons backtracking resolution behaviour for asdf to parse but allow
// us to run in a different, non-related directory tree.
func generateToolVersions(ctx context.Context, outputDir string) error {
	if path, err := exec.LookPath("asdf"); err != nil || path == "" {
		return nil
	}

	cmd := exec.CommandContext(ctx, "asdf", "current")
	out, err := cmd.Output()
	if err != nil {
		return errors.Wrap(err, "failed to get versions from asdf current")
	}

	tvp := filepath.Join(outputDir, toolVersions)
	f, err := os.Create(tvp)
	if err != nil {
		return errors.Wrapf(err, "failed to create %s", tvp)
	}
	defer f.Close()

	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		// https://goplay.space/#ewwP-RaSyFd
		spl := strings.Fields(scanner.Text())
		if len(spl) != 3 {
			continue
		}

		lang := spl[0]
		version := spl[1]

		if _, err := fmt.Fprintf(f, "%s %s\n", lang, version); err != nil {
			return errors.Wrap(err, "failed to write to .tool-versions in tempDir")
		}
	}
	return scanner.Err()
}

// downloadRepository downloads a repository into a temporary directory.
func downloadRepository(_ context.Context, m *Module) (func(), string, error) { //nolint:gocritic // Why: These seem fine.
	sourceDir := filepath.Join(os.TempDir(), "gobin", time.Now().Format(time.RFC3339Nano))
	cleanupFn := func() { os.RemoveAll(sourceDir) }
	err := os.MkdirAll(filepath.Dir(sourceDir), 0o755)
	if err != nil {
		return cleanupFn, "", err
	}
	return cleanupFn, sourceDir, m.repo.VCS.CreateAtRev(sourceDir, m.Repo, m.Version)
}
