package tmux

import (
	"fmt"
	"os/exec"
)

// JumpToWindow switches tmux focus to the given session and window.
func JumpToWindow(session string, window int) error {
	target := fmt.Sprintf("%s:%d", session, window)
	return exec.Command("tmux", "select-window", "-t", target).Run()
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
