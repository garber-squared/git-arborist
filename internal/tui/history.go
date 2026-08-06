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
