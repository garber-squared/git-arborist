package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// keyBinding is one row of the `?` overlay.
type keyBinding struct {
	key, desc string
}

// keySection groups related bindings under a heading in the overlay.
type keySection struct {
	title    string
	bindings []keyBinding
}

// keySections is the keybinding reference shown by `?` — the only place the
// keys are listed, so every new key belongs here.
func keySections() []keySection {
	return []keySection{
		{"Navigation", []keyBinding{
			{"←/→", "previous / next tile"},
			{"↑/↓", "tile above / below (grid)"},
			{"h", "expand tile"},
			{"l / esc", "collapse expanded tile"},
			{"s", "scope: all / root / submodules"},
		}},
		{"Selection", []keyBinding{
			{"space", "mark / unmark tile"},
			{"a", "mark all / none"},
			{"esc", "clear marks"},
		}},
		{"Panes", []keyBinding{
			{"enter", "jump to tmux pane"},
			{"j / k", "send Down / Up to pane"},
			{"e", "send Enter to pane"},
			{"t", "send Tab+Enter to pane"},
			{"i", "type text into pane(s)"},
			{"n", "open pane for worktree"},
			{"N", "open panes for all worktrees"},
		}},
		{"Worktrees", []keyBinding{
			{"c", "new worktree"},
			{"C", "new worktree (watch mode)"},
			{"d", "delete worktree (d to confirm)"},
			{"D", "force-delete a dirty worktree"},
			{"g", "git status overlay (colored)"},
			{"o", "open PR in browser"},
			{"I", "open issue in browser"},
		}},
		{"General", []keyBinding{
			{"r", "refresh"},
			{"?", "toggle this help"},
			{"q", "quit (also ctrl+c)"},
		}},
	}
}

var (
	styleKeysBox     = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("4")).Padding(1, 2)
	styleKeysHeading = lipgloss.NewStyle().Foreground(lipgloss.Color("4")).Bold(true)
	styleKeysKey     = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
)

// keysCTA is the header badge pointing at the overlay, which replaced the
// footer's key hints.
const keysCTA = " ? keybindings "

var styleKeysCTA = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#000000")).Background(lipgloss.Color("#ffd700"))

// renderTitle lays out the dashboard's title line: the title, then the
// keybinding CTA right after it.
func (m *Model) renderTitle(title string) string {
	return title + "  " + styleKeysCTA.Render(keysCTA)
}

// renderSection renders one section as a heading over aligned key/desc rows.
func renderSection(s keySection, keyW int) string {
	lines := []string{styleKeysHeading.Render(s.title)}
	for _, kb := range s.bindings {
		key := styleKeysKey.Render(kb.key + strings.Repeat(" ", keyW-lipgloss.Width(kb.key)))
		lines = append(lines, key+"  "+kb.desc)
	}
	return strings.Join(lines, "\n")
}

// keysLayout is one arrangement of the overlay; renderKeysView tries them
// from roomiest to tightest.
type keysLayout struct {
	cols    int
	compact bool // no padding and no blank lines, to save rows
}

var keysLayouts = []keysLayout{{1, false}, {2, false}, {3, false}, {1, true}, {2, true}, {3, true}}

// renderKeysView renders the keybinding overlay centred on screen, in the
// roomiest layout that fits the terminal. When even the tightest does not fit,
// the body scrolls with j/k — so no binding is ever cut off unreachably.
func (m *Model) renderKeysView() string {
	sections := keySections()
	keyW := 0
	for _, s := range sections {
		for _, kb := range s.bindings {
			keyW = max(keyW, lipgloss.Width(kb.key))
		}
	}
	rendered := make([]string, len(sections))
	for i, s := range sections {
		rendered[i] = renderSection(s, keyW)
	}

	place := func(box string) string {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
	}
	for _, l := range keysLayouts {
		box := renderKeysBox(keysBody(rendered, l), l, "? / esc / q: close")
		if lipgloss.Width(box) <= m.width && lipgloss.Height(box) <= m.height {
			m.overlayScroll = 0
			return place(box)
		}
	}

	// Scroll the widest compact layout that still fits across.
	cols := 1
	for c := 3; c > 1; c-- {
		if lipgloss.Width(renderKeysBox(keysBody(rendered, keysLayout{c, true}), keysLayout{1, true}, "")) <= m.width {
			cols = c
			break
		}
	}
	lines := strings.Split(keysBody(rendered, keysLayout{cols, true}), "\n")
	return place(m.renderScrollBox("Keybindings", lines, "? / esc / q: close"))
}

// keysBody arranges the rendered sections into l.cols columns, splitting
// where each column holds about the same number of lines.
func keysBody(rendered []string, l keysLayout) string {
	sep := "\n\n"
	if l.compact {
		sep = "\n"
	}
	total := 0
	for _, r := range rendered {
		total += lipgloss.Height(r)
	}
	cols := make([][]string, l.cols)
	before := 0
	for _, r := range rendered {
		c := min(l.cols-1, before*l.cols/total)
		cols[c] = append(cols[c], r)
		before += lipgloss.Height(r)
	}
	var joined []string
	for i, c := range cols {
		if len(c) == 0 {
			continue
		}
		if i > 0 {
			joined = append(joined, "    ")
		}
		joined = append(joined, strings.Join(c, sep))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, joined...)
}

// renderKeysBox frames a body with the overlay's title and footer.
func renderKeysBox(body string, l keysLayout, footer string) string {
	title := styleKeysHeading.Render("Keybindings")
	if footer != "" {
		footer = styleDim.Render(footer)
	}
	if l.compact {
		return styleKeysBox.Padding(0, 1).Render(title + "\n" + body + "\n" + footer)
	}
	return styleKeysBox.Render(title + "\n\n" + body + "\n\n" + footer)
}
