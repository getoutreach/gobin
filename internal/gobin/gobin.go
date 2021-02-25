package gobin

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/pkg/errors"
	"golang.org/x/tools/go/vcs"
)

var golangVerOutReg = regexp.MustCompile(` go(\d.+) `)
var removeVersion = regexp.MustCompile(`^/(v\d+)/`)

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
		sourceDir, err := downloadRepository(ctx, m)
		defer func() {
			if sourceDir != "" {
				os.RemoveAll(sourceDir)
			}
		}()
		if err != nil {
			return err
		}

		err = buildRepository(ctx, sourceDir, m)
		if err != nil {
			return err
		}
	}

	if printPath {
		fmt.Println(binPath)
		return nil
	}

	// run the command
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

	cmd := exec.CommandContext(ctx, "go", "build", "-o", binPath, "./"+m.GetCommandPath())
	cmd.Dir = sourceDir
	b, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Fprintln(os.Stderr, string(b))
	}
	return err
}

func downloadRepository(_ context.Context, m *Module) (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	goVer, err := getCurrentGoVersion()
	if err != nil {
		return "", err
	}

	sourceDir := filepath.Join(homeDir, ".outreach", ".cache", "gobin", "source", goVer, m.Path, m.Version)
	// Delete it in case it exists already
	os.RemoveAll(sourceDir)
	err = os.MkdirAll(filepath.Dir(sourceDir), 0755)
	if err != nil {
		return "", err
	}
	return sourceDir, m.repo.VCS.CreateAtRev(sourceDir, m.Repo, m.Version)
}
