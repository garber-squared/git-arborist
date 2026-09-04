package tmux

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// JumpToWindow switches tmux focus to the given session and window.
func JumpToWindow(session string, window int) error {
	target := fmt.Sprintf("%s:%d", session, window)
	return exec.Command("tmux", "select-window", "-t", target).Run()
}

// JumpToPane switches tmux focus to the window containing the given pane target
// (e.g. "session:3.0" → selects window "session:3").
func JumpToPane(paneTarget string) error {
	// Strip ".pane" suffix to get "session:window"
	windowTarget := paneTarget
	if idx := strings.LastIndex(paneTarget, "."); idx != -1 {
		windowTarget = paneTarget[:idx]
	}
	return exec.Command("tmux", "select-window", "-t", windowTarget).Run()
}

// KillWindow kills a tmux window by session and window index.
func KillWindow(session string, window int) error {
	target := fmt.Sprintf("%s:%d", session, window)
	return exec.Command("tmux", "kill-window", "-t", target).Run()
}

// KillWindowByPath finds a tmux window whose pane current path matches
// the given directory and kills it.
func KillWindowByPath(path string) error {
	cmd := exec.Command("tmux", "list-windows", "-a", "-F", "#{session_name}:#{window_index}\t#{pane_current_path}")
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("tmux list-windows: %w", err)
	}

	for _, line := range splitLines(string(out)) {
		if len(line) == 0 {
			continue
		}
		parts := splitTab(line)
		if len(parts) == 2 && normalizePath(parts[1]) == path {
			return exec.Command("tmux", "kill-window", "-t", parts[0]).Run()
		}
	}
	return fmt.Errorf("no tmux window found for path %s", path)
}

// FindWindowByPath searches tmux windows for one whose pane current path
// matches the given directory and switches to it.
func FindWindowByPath(path string) error {
	// List all windows with their pane current paths
	cmd := exec.Command("tmux", "list-windows", "-a", "-F", "#{session_name}:#{window_index}\t#{pane_current_path}")
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("tmux list-windows: %w", err)
	}

	for _, line := range splitLines(string(out)) {
		if len(line) == 0 {
			continue
		}
		// Format: "session:window\t/path/to/dir"
		parts := splitTab(line)
		if len(parts) == 2 && normalizePath(parts[1]) == path {
			return exec.Command("tmux", "select-window", "-t", parts[0]).Run()
		}
	}
	return fmt.Errorf("no tmux window found for path %s", path)
}

// PaneTarget represents a tmux pane and the directory it's in.
type PaneTarget struct {
	Target string // e.g. "session:1.0"
	Path   string
}

// CurrentPane returns the id of the tmux pane running this process, or "" when
// we are not inside tmux. tmux exports TMUX_PANE into every pane, which is the
// only reliable way to identify our own pane: `display-message` reports the
// session's *active* window, which stops being ours the moment we create a
// window elsewhere — and mistaking a worktree's pane for our own makes
// arborist think that worktree has no pane and spawn a duplicate window.
func CurrentPane() string {
	return os.Getenv("TMUX_PANE")
}

// ListPanes returns all tmux panes with their targets and current paths,
// excluding the pane that is running this process (to avoid recursive capture).
func ListPanes() ([]PaneTarget, error) {
	self := CurrentPane()

	cmd := exec.Command("tmux", "list-panes", "-a", "-F", "#{pane_id}\t#{session_name}:#{window_index}.#{pane_index}\t#{pane_current_path}")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("tmux list-panes: %w", err)
	}

	var panes []PaneTarget
	for _, line := range splitLines(string(out)) {
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) != 3 {
			continue
		}
		if self != "" && parts[0] == self {
			continue
		}
		panes = append(panes, PaneTarget{Target: parts[1], Path: normalizePath(parts[2])})
	}
	return panes, nil
}

// NewWindow creates a new tmux window in the current session rooted at path,
// named after the branch. Returns an error if tmux is unavailable.
func NewWindow(path, name string) error {
	return exec.Command("tmux", "new-window", "-c", path, "-n", name).Run()
}

// NewWindowWithCommand creates a window rooted at path that runs cmdline and
// then drops into an interactive shell, so the pane outlives the command —
// whether it succeeds or fails — and stays typeable from the dashboard. Each
// env entry ("VAR=value") is exported into the window. An empty cmdline gives
// a plain window, exactly like NewWindow.
func NewWindowWithCommand(path, name, cmdline string, env ...string) error {
	if strings.TrimSpace(cmdline) == "" {
		return NewWindow(path, name)
	}
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	args := []string{"new-window", "-c", path, "-n", name}
	for _, e := range env {
		args = append(args, "-e", e)
	}
	args = append(args, fmt.Sprintf("%s; exec %s", cmdline, shell))
	return exec.Command("tmux", args...).Run()
}

// SendKeys sends keys to a tmux pane.
func SendKeys(target string, keys ...string) error {
	args := append([]string{"send-keys", "-t", target}, keys...)
	return exec.Command("tmux", args...).Run()
}

// SendText types literal text into a tmux pane and submits it with Enter.
func SendText(target, text string) error {
	if err := exec.Command("tmux", "send-keys", "-t", target, "-l", "--", text).Run(); err != nil {
		return err
	}
	return exec.Command("tmux", "send-keys", "-t", target, "Enter").Run()
}

// CapturePaneContent grabs the visible text from a tmux pane.
func CapturePaneContent(target string) string {
	cmd := exec.Command("tmux", "capture-pane", "-p", "-e", "-t", target)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return string(out)
}

// normalizePath strips the " (deleted)" suffix the kernel appends to a
// process's cwd (and hence tmux's pane_current_path) when the directory it
// points at has been removed. Without this, a pane whose shell is sitting in a
// removed-and-recreated worktree would never match the worktree's real path,
// causing arborist to treat it as absent and spawn duplicate windows.
func normalizePath(s string) string {
	return strings.TrimSuffix(s, " (deleted)")
}

func splitTab(s string) []string {
	for i := 0; i < len(s); i++ {
		if s[i] == '\t' {
			return []string{s[:i], s[i+1:]}
		}
	}
	return []string{s}
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
