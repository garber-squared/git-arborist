package tmux

import (
	"fmt"
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
	cmd := exec.Command("tmux", "list-windows", "-a", "-F", "#{session_name}:#{window_index} #{pane_current_path}")
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("tmux list-windows: %w", err)
	}

	for _, line := range splitLines(string(out)) {
		if len(line) == 0 {
			continue
		}
		var target, panePath string
		n, _ := fmt.Sscanf(line, "%s %s", &target, &panePath)
		if n == 2 && panePath == path {
			return exec.Command("tmux", "kill-window", "-t", target).Run()
		}
	}
	return fmt.Errorf("no tmux window found for path %s", path)
}

// FindWindowByPath searches tmux windows for one whose pane current path
// matches the given directory and switches to it.
func FindWindowByPath(path string) error {
	// List all windows with their pane current paths
	cmd := exec.Command("tmux", "list-windows", "-a", "-F", "#{session_name}:#{window_index} #{pane_current_path}")
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("tmux list-windows: %w", err)
	}

	for _, line := range splitLines(string(out)) {
		if len(line) == 0 {
			continue
		}
		// Format: "session:window /path/to/dir"
		var target, panePath string
		n, _ := fmt.Sscanf(line, "%s %s", &target, &panePath)
		if n == 2 && panePath == path {
			return exec.Command("tmux", "select-window", "-t", target).Run()
		}
	}
	return fmt.Errorf("no tmux window found for path %s", path)
}

// PaneTarget represents a tmux pane and the directory it's in.
type PaneTarget struct {
	Target string // e.g. "session:1.0"
	Path   string
}

// CurrentPane returns the target of the tmux pane running this process.
func CurrentPane() string {
	cmd := exec.Command("tmux", "display-message", "-p", "#{session_name}:#{window_index}.#{pane_index}")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// ListPanes returns all tmux panes with their targets and current paths,
// excluding the pane that is running this process (to avoid recursive capture).
func ListPanes() ([]PaneTarget, error) {
	self := CurrentPane()

	cmd := exec.Command("tmux", "list-panes", "-a", "-F", "#{session_name}:#{window_index}.#{pane_index}\t#{pane_current_path}")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("tmux list-panes: %w", err)
	}

	var panes []PaneTarget
	for _, line := range splitLines(string(out)) {
		if line == "" {
			continue
		}
		parts := splitTab(line)
		if len(parts) == 2 {
			if parts[0] == self {
				continue
			}
			panes = append(panes, PaneTarget{Target: parts[0], Path: parts[1]})
		}
	}
	return panes, nil
}

// SendKeys sends keys to a tmux pane.
func SendKeys(target string, keys ...string) error {
	args := append([]string{"send-keys", "-t", target}, keys...)
	return exec.Command("tmux", args...).Run()
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
