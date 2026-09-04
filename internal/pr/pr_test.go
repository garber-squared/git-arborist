package pr

import "testing"

func TestLinkedIssueNumber(t *testing.T) {
	for _, tc := range []struct{ branch, want string }{
		// The shape `gh issue develop` produces.
		{"1905-a-paired-partners-exit", "1905"},
		{"7-fix", "7"},
		{"1905", "1905"},
		// A path prefix is stripped, so a namespaced branch still resolves.
		{"ds/1905-loading-splash", "1905"},
		// Anything else must resolve to nothing: this drives a write to
		// GitHub, and these must never touch issues #1, #2 or #6.
		{"release-1.6.0", ""},
		{"v2-refactor", ""},
		{"fix/admin-ui-tuneup", ""},
		{"staging", ""},
		{"", ""},
		{"gh-123", ""},
	} {
		if got := LinkedIssueNumber(tc.branch); got != tc.want {
			t.Errorf("LinkedIssueNumber(%q) = %q, want %q", tc.branch, got, tc.want)
		}
	}
}

// TestIssueNumberFromBranchIsStillLoose documents the split: navigation and
// ARBORIST_ISSUE stay forgiving, labelling does not.
func TestIssueNumberFromBranchIsStillLoose(t *testing.T) {
	if got := IssueNumberFromBranch("gh-123"); got != "123" {
		t.Errorf("IssueNumberFromBranch(gh-123) = %q, want 123", got)
	}
	if got := LinkedIssueNumber("gh-123"); got != "" {
		t.Errorf("LinkedIssueNumber(gh-123) = %q, want empty", got)
	}
}
