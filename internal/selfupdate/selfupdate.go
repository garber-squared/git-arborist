// Package selfupdate keeps the installed arborist current with its own source
// checkout: on startup it fetches, fast-forwards, and rebuilds when anything
// came in.
package selfupdate

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// SourceDir returns the arborist checkout the running binary was built in.
// The arborist on PATH is a symlink to <repo>/arborist, so resolving the
// executable leads back to the repo. ok is false when the binary does not sit
// in an arborist checkout (a copied or `go install`ed binary), in which case
// there is nothing to update.
func SourceDir() (dir string, ok bool) {
	exe, err := os.Executable()
	if err != nil {
		return "", false
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return "", false
	}
	dir = filepath.Dir(exe)
	for _, marker := range []string{"cmd/arborist", "Makefile", ".git"} {
		if _, err := os.Stat(filepath.Join(dir, marker)); err != nil {
			return "", false
		}
	}
	return dir, true
}

// Run fetches dir's upstream and, when the current branch is behind it,
// fast-forwards and rebuilds with `make go-build`. It returns a one-line
// summary for the status bar, or "" when there was nothing to do — including
// when the fetch failed, since being offline is not worth a message on every
// launch. A branch without an upstream is left alone, as is one that has
// diverged: --ff-only never merges or rebases local work.
func Run(dir string) (string, error) {
	if _, err := git(dir, "rev-parse", "--abbrev-ref", "@{upstream}"); err != nil {
		return "", nil
	}
	if _, err := git(dir, "fetch", "--quiet"); err != nil {
		return "", nil
	}
	behind, err := git(dir, "rev-list", "--count", "HEAD..@{upstream}")
	if err != nil || behind == "0" {
		return "", err
	}

	before, err := git(dir, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	if _, err := git(dir, "pull", "--ff-only", "--quiet"); err != nil {
		return "", fmt.Errorf("pull: %w", err)
	}
	after, err := git(dir, "rev-parse", "HEAD")
	if err != nil || after == before {
		return "", err
	}

	// go build replaces the binary rather than writing into it, so rebuilding
	// under the running process is safe; the new build takes effect on the
	// next launch.
	if out, err := exec.Command("make", "-C", dir, "go-build").CombinedOutput(); err != nil {
		return "", fmt.Errorf("build: %s", lastLine(string(out), err))
	}
	return fmt.Sprintf("arborist updated (%s new commit(s)) — restart to use it", behind), nil
}

func git(dir string, args ...string) (string, error) {
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %s", args[0], lastLine(string(out), err))
	}
	return strings.TrimSpace(string(out)), nil
}

// lastLine keeps a failing command's message to one status-bar line.
func lastLine(out string, err error) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if l := strings.TrimSpace(lines[len(lines)-1]); l != "" {
		return l
	}
	return err.Error()
}
