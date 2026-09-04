// Package port manages the dev-port registry that gives every worktree its own
// port for local servers and containers.
//
// The registry is <main repo root>/.worktree-ports: one "<dir> <port>" line per
// worktree, ports counting up from a base (8080 by default) with the main
// worktree always holding the base itself. That is the format of the
// worktree-port.sh helper a repo may already have, so arborist and that script
// read and write the same assignments — a `make` target that resolves the port
// itself keeps working unchanged.
package port

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// DefaultBase is the port the main worktree holds; worktrees count up from the
// one above it.
const DefaultBase = 8080

// RegistryName is the registry's filename at the repository's main checkout.
const RegistryName = ".worktree-ports"

// entry is one assignment line.
type entry struct {
	dir  string
	port int
}

// Registry is one repository's port assignments, in file order.
type Registry struct {
	path    string
	mainDir string // main checkout, which always holds the base port
	base    int
	entries []entry
	exists  bool
}

// Load reads the registry of the repository whose main checkout is repoRoot. A
// missing registry is not an error: the result is simply disabled.
func Load(repoRoot string) *Registry {
	r := &Registry{
		path:    filepath.Join(repoRoot, RegistryName),
		mainDir: filepath.Clean(repoRoot),
		base:    configuredBase(repoRoot),
	}
	data, err := os.ReadFile(r.path)
	if err != nil {
		return r
	}
	r.exists = true
	for _, line := range strings.Split(string(data), "\n") {
		if e, ok := parseEntry(line); ok {
			r.entries = append(r.entries, e)
		}
	}
	return r
}

// configuredBase returns the base port from `git config arborist.portBase`, or
// 0 when it is unset or unusable.
func configuredBase(repoRoot string) int {
	out, err := exec.Command("git", "-C", repoRoot, "config", "--get", "arborist.portBase").Output()
	if err != nil {
		return 0
	}
	base, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil || base <= 0 || base > 65535 {
		return 0
	}
	return base
}

// parseEntry splits an "<dir> <port>" line. The port is the last field, so a
// directory containing spaces still parses — the same rule worktree-port.sh
// uses.
func parseEntry(line string) (entry, bool) {
	line = strings.TrimRight(line, "\r")
	idx := strings.LastIndex(line, " ")
	if idx <= 0 {
		return entry{}, false
	}
	port, err := strconv.Atoi(strings.TrimSpace(line[idx+1:]))
	if err != nil || port <= 0 {
		return entry{}, false
	}
	return entry{dir: strings.TrimSpace(line[:idx]), port: port}, true
}

// Enabled reports whether this repository uses dev ports: it either already
// has a registry, or a base port is configured. arborist assigns ports only
// then, so a repository that wants nothing to do with them never grows a
// stray file.
func (r *Registry) Enabled() bool {
	return r != nil && (r.exists || r.base > 0)
}

// basePort is the configured base, or the conventional default.
func (r *Registry) basePort() int {
	if r.base > 0 {
		return r.base
	}
	return DefaultBase
}

// Port returns the port assigned to dir, or 0 when it has none. A later line
// for the same directory wins, matching worktree-port.sh's "last match".
func (r *Registry) Port(dir string) int {
	if r == nil {
		return 0
	}
	dir = filepath.Clean(dir)
	assigned := 0
	for _, e := range r.entries {
		if e.dir == dir {
			assigned = e.port
		}
	}
	return assigned
}

// Assign returns dir's port, giving it the next free one if it has none. It
// returns 0 when the repository does not use dev ports.
func (r *Registry) Assign(dir string) (int, error) {
	if !r.Enabled() {
		return 0, nil
	}
	if p := r.Port(dir); p != 0 {
		return p, nil
	}

	dir = filepath.Clean(dir)
	assigned := r.basePort()
	if dir != r.mainDir {
		// The main checkout keeps the base port; everything else takes the
		// lowest port above it that nothing else in the registry holds.
		used := make(map[int]bool, len(r.entries))
		for _, e := range r.entries {
			used[e.port] = true
		}
		for assigned++; used[assigned]; assigned++ {
		}
	}

	// Append rather than rewrite, so an assignment made by the repo's own
	// script at the same moment is not lost.
	f, err := os.OpenFile(r.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	if _, err := fmt.Fprintf(f, "%s %d\n", dir, assigned); err != nil {
		return 0, err
	}
	r.entries = append(r.entries, entry{dir: dir, port: assigned})
	r.exists = true
	return assigned, nil
}

// Release drops dir's assignment so its port can be reused. Only entries for
// dir are touched: stale entries for directories someone else removed are left
// alone, since arborist cannot know whether something still holds their ports
// (`worktree-port.sh --cleanup` is the tool for that).
func (r *Registry) Release(dir string) error {
	if !r.Enabled() {
		return nil
	}
	dir = filepath.Clean(dir)
	kept := make([]entry, 0, len(r.entries))
	for _, e := range r.entries {
		if e.dir != dir {
			kept = append(kept, e)
		}
	}
	if len(kept) == len(r.entries) {
		return nil
	}

	var b strings.Builder
	for _, e := range kept {
		fmt.Fprintf(&b, "%s %d\n", e.dir, e.port)
	}
	// Write and rename so a reader never sees a half-written registry.
	tmp := r.path + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0644); err != nil {
		return err
	}
	if err := os.Rename(tmp, r.path); err != nil {
		os.Remove(tmp)
		return err
	}
	r.entries = kept
	return nil
}
