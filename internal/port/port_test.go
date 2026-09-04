package port

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// repo builds a throwaway git repo, since the base port is read from git
// config and the main checkout holds the base itself.
func repo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", dir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	return dir
}

func TestDisabledWithoutRegistryOrConfig(t *testing.T) {
	root := repo(t)
	r := Load(root)
	if r.Enabled() {
		t.Error("a repo with no registry and no config must not use ports")
	}
	if p, err := r.Assign(filepath.Join(root, "worktrees", "x")); p != 0 || err != nil {
		t.Errorf("Assign = %d, %v; want 0, nil", p, err)
	}
	if _, err := os.Stat(filepath.Join(root, RegistryName)); !os.IsNotExist(err) {
		t.Error("Assign created a registry in a repo that does not use ports")
	}
}

func TestAssign(t *testing.T) {
	root := repo(t)
	// An existing registry is itself the opt-in.
	os.WriteFile(filepath.Join(root, RegistryName), nil, 0644)
	r := Load(root)
	if !r.Enabled() {
		t.Fatal("an existing registry must enable ports")
	}

	// The main checkout holds the base port.
	if p, err := r.Assign(root); p != DefaultBase || err != nil {
		t.Errorf("main checkout got %d, %v; want %d", p, err, DefaultBase)
	}
	// Worktrees count up from above it.
	a := filepath.Join(root, "worktrees", "a")
	b := filepath.Join(root, "worktrees", "b")
	if p, _ := r.Assign(a); p != DefaultBase+1 {
		t.Errorf("first worktree got %d, want %d", p, DefaultBase+1)
	}
	if p, _ := r.Assign(b); p != DefaultBase+2 {
		t.Errorf("second worktree got %d, want %d", p, DefaultBase+2)
	}
	// Assignment is idempotent — a worktree's port must never move.
	if p, _ := r.Assign(a); p != DefaultBase+1 {
		t.Errorf("reassigned to %d, want %d", p, DefaultBase+1)
	}
	if p := Load(root).Port(a); p != DefaultBase+1 {
		t.Errorf("port did not survive a reload: %d", p)
	}

	// The file is "<dir> <port>" lines, the format the repo's own script reads.
	data, _ := os.ReadFile(filepath.Join(root, RegistryName))
	want := a + " 8081"
	if !strings.Contains(string(data), want) {
		t.Errorf("registry = %q, want a line %q", data, want)
	}

	// A released port is the next one handed out.
	if err := r.Release(a); err != nil {
		t.Fatal(err)
	}
	if p := r.Port(a); p != 0 {
		t.Errorf("after Release, port = %d, want 0", p)
	}
	if p, _ := r.Assign(filepath.Join(root, "worktrees", "c")); p != DefaultBase+1 {
		t.Errorf("released port not reused: got %d, want %d", p, DefaultBase+1)
	}
	// Release leaves other assignments alone.
	if p := Load(root).Port(b); p != DefaultBase+2 {
		t.Errorf("Release disturbed another entry: %d", p)
	}
}

func TestConfiguredBase(t *testing.T) {
	root := repo(t)
	exec.Command("git", "-C", root, "config", "arborist.portBase", "4000").Run()
	r := Load(root)
	if !r.Enabled() {
		t.Fatal("arborist.portBase must enable ports")
	}
	if p, _ := r.Assign(root); p != 4000 {
		t.Errorf("main checkout got %d, want 4000", p)
	}
	if p, _ := r.Assign(filepath.Join(root, "w")); p != 4001 {
		t.Errorf("worktree got %d, want 4001", p)
	}
}

func TestSkipsPortsHeldByOthers(t *testing.T) {
	root := repo(t)
	// Entries written by someone else, including a stale one and a directory
	// with a space in it.
	os.WriteFile(filepath.Join(root, RegistryName), []byte(
		root+" 8080\n"+
			"/gone/old-worktree 8081\n"+
			"/my repo/with space 8082\n"), 0644)
	r := Load(root)
	if p := r.Port("/my repo/with space"); p != 8082 {
		t.Errorf("directory with a space parsed as %d, want 8082", p)
	}
	if p, _ := r.Assign(filepath.Join(root, "w")); p != 8083 {
		t.Errorf("got %d, want 8083 — ports held by other entries must be skipped", p)
	}
	// Foreign stale entries are not arborist's to reclaim.
	if p := Load(root).Port("/gone/old-worktree"); p != 8081 {
		t.Errorf("stale foreign entry was dropped (port %d)", p)
	}
}

func TestLastEntryWins(t *testing.T) {
	root := repo(t)
	dir := filepath.Join(root, "w")
	os.WriteFile(filepath.Join(root, RegistryName), []byte(dir+" 8081\n"+dir+" 8090\n"), 0644)
	if p := Load(root).Port(dir); p != 8090 {
		t.Errorf("port = %d, want 8090 (last line wins, as worktree-port.sh does)", p)
	}
}

// TestScriptCompatibility runs the repo's real worktree-port.sh against a
// registry arborist wrote, in both directions, so the two never disagree about
// a worktree's port. The script anchors its registry to the *current*
// directory's repo, so it has to run inside the temp repo.
func TestScriptCompatibility(t *testing.T) {
	script := "/home/alexanderg/Development/sideby/my-sideby-ai/scripts/worktree-port.sh"
	if _, err := os.Stat(script); err != nil {
		t.Skip("worktree-port.sh not available")
	}

	root := repo(t)
	os.WriteFile(filepath.Join(root, RegistryName), nil, 0644)
	r := Load(root)
	r.Assign(root)
	wt := filepath.Join(root, "worktrees", "feature")
	os.MkdirAll(wt, 0755)

	assigned, err := r.Assign(wt)
	if err != nil {
		t.Fatal(err)
	}

	// The script must read back what arborist assigned.
	if got := runScript(t, script, root, "--dir", wt); got != strconv.Itoa(assigned) {
		t.Errorf("script reads %q for a port arborist assigned as %d", got, assigned)
	}

	// ...and arborist must read back what the script assigns.
	other := filepath.Join(root, "worktrees", "other")
	os.MkdirAll(other, 0755)
	scriptPort := runScript(t, script, root, "--dir", other)
	if got := Load(root).Port(other); strconv.Itoa(got) != scriptPort {
		t.Errorf("arborist reads %d for a port the script assigned as %s", got, scriptPort)
	}
	if scriptPort == strconv.Itoa(assigned) {
		t.Errorf("script handed out %s, already held by another worktree", scriptPort)
	}
}

func runScript(t *testing.T, script, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("bash", append([]string{script}, args...)...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("worktree-port.sh %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}
