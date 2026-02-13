package gitstatus

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

type Status struct {
	Clean    bool
	Ahead    int
	Behind   int
	Staged   int
	Unstaged int
	Untracked int
}

// String returns a compact human-readable status.
func (s Status) String() string {
	if s.Clean && s.Ahead == 0 && s.Behind == 0 {
		return "clean"
	}
	var parts []string
	if !s.Clean {
		parts = append(parts, "dirty")
	} else {
		parts = append(parts, "clean")
	}
	if s.Ahead > 0 {
		parts = append(parts, fmt.Sprintf("↑%d", s.Ahead))
	}
	if s.Behind > 0 {
		parts = append(parts, fmt.Sprintf("↓%d", s.Behind))
	}
	if s.Staged > 0 {
		parts = append(parts, fmt.Sprintf("+%d staged", s.Staged))
	}
	if s.Untracked > 0 {
		parts = append(parts, fmt.Sprintf("%d untracked", s.Untracked))
	}
	return strings.Join(parts, " ")
}

// Get computes the git status for the worktree at the given path.
func Get(worktreePath string) Status {
	var s Status

	// porcelain status
	cmd := exec.Command("git", "-C", worktreePath, "status", "--porcelain")
	out, err := cmd.Output()
	if err != nil {
		return s
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) == 1 && lines[0] == "" {
		s.Clean = true
	} else {
		for _, line := range lines {
			if len(line) < 2 {
				continue
			}
			x, y := line[0], line[1]
			switch {
			case x == '?' && y == '?':
				s.Untracked++
			case x != ' ' && x != '?':
				s.Staged++
				if y != ' ' {
					s.Unstaged++
				}
			case y != ' ':
				s.Unstaged++
			}
		}
	}

	// ahead/behind
	cmd = exec.Command("git", "-C", worktreePath, "rev-list", "--left-right", "--count", "@{upstream}...HEAD")
	out, err = cmd.Output()
	if err == nil {
		parts := strings.Fields(strings.TrimSpace(string(out)))
		if len(parts) == 2 {
			s.Behind, _ = strconv.Atoi(parts[0])
			s.Ahead, _ = strconv.Atoi(parts[1])
		}
	}

	return s
}
