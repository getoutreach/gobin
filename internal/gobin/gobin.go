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

var golangVerOutReg = regexp.MustCompile(` go(\d.+) `)
var removeVersion = regexp.MustCompile(`^/(v\d+)/`)

const toolVersions = ".tool-versions"

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

func Run(ctx context.Context, importPath string, printPath bool) error { //nolint:funlen
	ref := ""
	verSplit := strings.SplitN(importPath, "@", 2)
	if len(verSplit) == 2 {
		importPath = verSplit[0]
		ref = verSplit[1]
	}

	if ref == "" {
		return fmt.Errorf("expected a version, e.g. %s@vX.X.X", importPath)
	}

	root, err := vcs.RepoRootForImportPath(importPath, false)
	if err != nil {
		return errors.Wrap(err, "failed to parse import path")
	}

	m := &Module{
		OriginalImport: importPath,
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
		if err != nil {
			return err
		}

		err = buildRepository(ctx, sourceDir, m)
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

func buildRepository(ctx context.Context, sourceDir string, m *Module) error {
	binPath, err := m.GetBinaryCache()
	if err != nil {
		return err
	}

	// ensure we have a .tool-versions in the directory we're temporarily
	// building this source code in, based off of the current versions of tools
	// if we have asdf installed.
	if err := generateToolVersions(ctx, sourceDir); err != nil {
		return errors.Wrap(err, "failed to setup asdf integration")
	}

	cmd := exec.CommandContext(ctx, "go", "build", "-o", binPath, "./"+m.GetCommandPath())
	cmd.Dir = sourceDir
	b, err := cmd.CombinedOutput()
	if err != nil {
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

		f.Write([]byte(fmt.Sprintf("%s %s\n", lang, version)))
	}
	return scanner.Err()
}

func downloadRepository(_ context.Context, m *Module) (func(), string, error) { //nolint:gocritic // Why: These seem fine.
	sourceDir := filepath.Join(os.TempDir(), "gobin", time.Now().Format(time.RFC3339Nano))
	cleanupFn := func() { os.RemoveAll(sourceDir) }
	err := os.MkdirAll(filepath.Dir(sourceDir), 0755)
	if err != nil {
		return cleanupFn, "", err
	}
	return cleanupFn, sourceDir, m.repo.VCS.CreateAtRev(sourceDir, m.Repo, m.Version)
}
