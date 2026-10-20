package tui

import (
	"os"
	"strings"
)

const maxHistoryEntries = 100

// loadHistory reads sent-text history from path, oldest entry first.
func loadHistory(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var entries []string
	for _, line := range strings.Split(string(data), "\n") {
		if line != "" {
			entries = append(entries, line)
		}
	}
	return entries
}

func saveHistory(path string, entries []string) {
	_ = os.WriteFile(path, []byte(strings.Join(entries, "\n")+"\n"), 0644)
}

// appendHistory records text as the most recent entry, dropping any earlier
// duplicate and capping the total length.
func appendHistory(entries []string, text string) []string {
	out := make([]string, 0, len(entries)+1)
	for _, e := range entries {
		if e != text {
			out = append(out, e)
		}
	}
	out = append(out, text)
	if len(out) > maxHistoryEntries {
		out = out[len(out)-maxHistoryEntries:]
	}
	return out
}

// fuzzyMatch reports whether every rune of query appears in s in order
// (case-insensitive subsequence match).
func fuzzyMatch(query, s string) bool {
	if query == "" {
		return true
	}
	qr := []rune(strings.ToLower(query))
	i := 0
	for _, r := range strings.ToLower(s) {
		if r == qr[i] {
			i++
			if i == len(qr) {
				return true
			}
		}
	}
	return false
}

// filteredHistory returns history entries fuzzy-matching the current input
// text, oldest first.
func (m *Model) filteredHistory() []string {
	query := m.input.Value()
	var out []string
	for _, e := range m.history {
		if fuzzyMatch(query, e) {
			out = append(out, e)
		}
	}
	return out
}

// historyCompletion returns the text tab should complete the input to: the
// highlighted history entry if there is one, else the most recent entry that
// starts with the typed text, else the most recent fuzzy match. Empty means
// nothing to complete.
func (m *Model) historyCompletion() string {
	filtered := m.filteredHistory()
	if m.histSel >= 0 && m.histSel < len(filtered) {
		return filtered[m.histSel]
	}
	query := strings.ToLower(m.input.Value())
	for i := len(filtered) - 1; i >= 0; i-- {
		if strings.HasPrefix(strings.ToLower(filtered[i]), query) {
			return filtered[i]
		}
	}
	if query != "" && len(filtered) > 0 {
		return filtered[len(filtered)-1]
	}
	return ""
}
