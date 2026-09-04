package pr

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

type PullRequest struct {
	Number  int    `json:"number"`
	State   string `json:"state"`
	Title   string `json:"title"`
	IsDraft bool   `json:"isDraft"`
}

// String returns a compact display string.
func (p PullRequest) String() string {
	if p.IsDraft {
		return fmt.Sprintf("#%d (draft) %s", p.Number, p.Title)
	}
	return fmt.Sprintf("#%d %s", p.Number, p.Title)
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

var issueNumberRe = regexp.MustCompile(`\d+`)

// IssueNumberFromBranch treats the first run of digits in a branch name as
// the linked issue number (covers "123-fix", "feature/123-fix", "gh-123").
// Returns "" when the branch name contains no number.
func IssueNumberFromBranch(branch string) string {
	return issueNumberRe.FindString(branch)
}

// OpenIssueInBrowser opens the issue linked to the worktree's branch in the
// default browser. gh resolves the repo from the worktree directory.
func OpenIssueInBrowser(worktreePath, issueNumber string) error {
	cmd := exec.Command("gh", "issue", "view", issueNumber, "--web")
	cmd.Dir = worktreePath
	return cmd.Run()
}

// LinkedBranch returns the branch already linked to an issue via GitHub's
// development branches, or "" when the issue has none. gh resolves the repo
// from dir.
func LinkedBranch(dir, issueNumber string) string {
	cmd := exec.Command("gh", "issue", "develop", "--list", issueNumber)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	// Each line is "<branch>\t<url>".
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if name, _, ok := strings.Cut(line, "\t"); ok && name != "" {
			return name
		}
	}
	return ""
}

// DevelopBranch creates a branch linked to an issue, based on baseBranch, and
// returns its name. The branch is created on the remote, so callers must fetch
// before checking it out.
func DevelopBranch(dir, issueNumber, baseBranch string) (string, error) {
	cmd := exec.Command("gh", "issue", "develop", issueNumber, "--base", baseBranch)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return "", errors.New(strings.SplitN(msg, "\n", 2)[0])
		}
		return "", err
	}
	// gh prints the new branch's URL, e.g.
	// "https://github.com/owner/repo/tree/123-fix-thing".
	if _, branch, ok := strings.Cut(strings.TrimSpace(string(out)), "/tree/"); ok && branch != "" {
		return branch, nil
	}
	if branch := LinkedBranch(dir, issueNumber); branch != "" {
		return branch, nil
	}
	return "", fmt.Errorf("no branch linked to issue #%s after gh issue develop", issueNumber)
}

// linkedIssueRe matches the leading issue number of a branch name produced by
// `gh issue develop`: "1905-fix-thing", or "ds/1905-fix-thing" once the path
// prefix is stripped.
var linkedIssueRe = regexp.MustCompile(`^(\d+)(?:-|$)`)

// LinkedIssueNumber returns the issue number a branch name carries, or "" when
// it carries none. It is deliberately stricter than IssueNumberFromBranch,
// which accepts digits anywhere: this one drives writes to GitHub, so
// "release-1.6.0" must never resolve to issue #1.
func LinkedIssueNumber(branch string) string {
	segment := branch
	if idx := strings.LastIndex(branch, "/"); idx >= 0 {
		segment = branch[idx+1:]
	}
	m := linkedIssueRe.FindStringSubmatch(segment)
	if m == nil {
		return ""
	}
	return m[1]
}

// inDevelopmentColor is the green the worktree scripts create this label with,
// so a label arborist creates is indistinguishable from theirs.
const inDevelopmentColor = "0E8A16"

// AddIssueLabel adds label to an issue, creating the label in the repository
// first when it is missing — gh refuses to apply a label that does not exist.
// gh resolves the repository from dir.
func AddIssueLabel(dir, issueNumber, label string) error {
	if !labelExists(dir, label) {
		// Creation can lose a race with another client, or the token may not
		// carry label permissions; either way the add below is the verdict.
		create := exec.Command("gh", "label", "create", label,
			"--color", inDevelopmentColor,
			"--description", "Issue is actively being worked on")
		create.Dir = dir
		_ = create.Run()
	}

	cmd := exec.Command("gh", "issue", "edit", issueNumber, "--add-label", label)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return errors.New(strings.SplitN(msg, "\n", 2)[0])
		}
		return err
	}
	return nil
}

// labelExists reports whether the repository has a label with exactly this
// name. `gh label list --search` matches loosely — searching for
// "status:in-development" also returns "status:pending-deployment" — so the
// names have to be compared exactly.
func labelExists(dir, label string) bool {
	cmd := exec.Command("gh", "label", "list", "--search", label, "--json", "name", "--jq", ".[].name")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(line) == label {
			return true
		}
	}
	return false
}
