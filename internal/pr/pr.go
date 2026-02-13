package pr

import (
	"encoding/json"
	"fmt"
	"os/exec"
)

type PullRequest struct {
	Number  int    `json:"number"`
	State   string `json:"state"`
	Title   string `json:"title"`
	IsDraft bool   `json:"isDraft"`
}

// String returns a compact display string.
func (p PullRequest) String() string {
	label := fmt.Sprintf("#%d (%s)", p.Number, p.State)
	if p.IsDraft {
		label = fmt.Sprintf("#%d (draft)", p.Number)
	}
	return label
}

// Fetch retrieves the PR associated with the branch checked out in worktreePath.
// Returns nil if no PR exists.
func Fetch(worktreePath string) *PullRequest {
	cmd := exec.Command("gh", "pr", "view", "--json", "number,state,title,isDraft")
	cmd.Dir = worktreePath
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var p PullRequest
	if err := json.Unmarshal(out, &p); err != nil {
		return nil
	}
	return &p
}

// OpenInBrowser opens the PR for the given worktree in the default browser.
func OpenInBrowser(worktreePath string) error {
	cmd := exec.Command("gh", "pr", "view", "--web")
	cmd.Dir = worktreePath
	return cmd.Run()
}
