package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/garber-squared/git-arborist/internal/activity"
	"github.com/garber-squared/git-arborist/internal/port"
	"github.com/garber-squared/git-arborist/internal/pr"
	"github.com/garber-squared/git-arborist/internal/tmux"
	"github.com/garber-squared/git-arborist/internal/worktree"
)

// createStage is the question the create-worktree picker is currently asking.
type createStage int

const (
	// stageBranch asks which branch the worktree is for: an existing one, a
	// new one, or the branch belonging to an issue.
	stageBranch createStage = iota
	// stageBase asks which branch a to-be-created branch starts from.
	stageBase
)

// createRequest is everything needed to create one worktree. It is filled in
// as the picker's stages are answered, then executed off the UI loop.
type createRequest struct {
	repoRoot string // repository the worktree is created in
	repo     string // submodule name, or "" for the superproject
	branch   string // existing branch to check out, or new branch to create
	startRef string // ref a new branch starts from; "" checks out branch as-is
	issue    string // when set, gh develops this issue's branch first
	base     string // base branch for a branch that does not exist yet
	// mode decides what the worktree's tmux window runs once setup finishes:
	// the project's start command, or a git status watcher instead.
	mode worktree.WindowMode
}

// createState holds the state of the interactive create-worktree flow, opened
// with `c`. Both stages render the same fuzzy-filtered picker over input.
type createState struct {
	active   bool
	mode     worktree.WindowMode // carried into the request the stages build
	stage    createStage
	input    textinput.Model
	cands    []worktree.Candidate // branches without a worktree (stageBranch)
	bases    []string             // base branch names (stageBase)
	sel      int                  // index into the current option list
	fetching bool                 // background fetch still refreshing cands
	busy     bool                 // creation in flight
	req      createRequest
	// branchQuery remembers what was typed at stageBranch so stepping back
	// from the base question doesn't throw it away.
	branchQuery string
}

// createOptionKind distinguishes the picker's row types.
type createOptionKind int

const (
	optBranch    createOptionKind = iota // check out an existing branch
	optIssue                             // develop an issue's branch, then check it out
	optNewBranch                         // create a branch from a base
	optBase                              // base branch for the two above
)

// createOption is one selectable row of the picker.
type createOption struct {
	kind createOptionKind
	cand worktree.Candidate // optBranch
	text string             // issue number, new branch name, or base branch
	raw  bool               // optBase typed by the user rather than listed
}

// openCreate starts the create-worktree flow. The branch list is read from
// local refs so the picker opens instantly, then refreshed in the background
// with a fetch so branches pushed from elsewhere show up too.
func (m *Model) openCreate(mode worktree.WindowMode) tea.Cmd {
	cands, err := worktree.ListCandidates(m.repoRoot)
	if err != nil {
		m.message = fmt.Sprintf("branch lookup failed: %v", err)
		return nil
	}
	m.create.active = true
	m.create.mode = mode
	m.create.stage = stageBranch
	m.create.cands = cands
	m.create.bases = nil
	m.create.req = createRequest{}
	m.create.sel = 0
	m.create.busy = false
	m.create.fetching = true
	m.message = ""
	m.create.input.SetValue("")
	m.create.input.Width = m.createInputWidth()
	return tea.Batch(m.create.input.Focus(), loadCandidatesCmd(m.repoRoot, true))
}

func (m *Model) closeCreate() {
	m.create.active = false
	m.create.busy = false
	m.create.fetching = false
	m.create.input.Blur()
	m.create.input.SetValue("")
}

// handleCreateKey owns every key while the picker is open.
func (m *Model) handleCreateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// ctrl+c quits arborist here as it does everywhere else; the picker is
	// torn down first so the normal quit path runs.
	if msg.String() == "ctrl+c" {
		m.closeCreate()
		return m.handleKey(msg)
	}

	// The worktree is already being created; nothing to steer until it lands.
	if m.create.busy {
		return m, nil
	}

	switch msg.String() {
	case "esc":
		if m.create.stage == stageBase {
			// Step back to the branch question, restoring what was typed.
			m.create.stage = stageBranch
			m.create.req = createRequest{mode: m.create.mode}
			m.create.sel = 0
			m.create.input.SetValue(m.create.branchQuery)
			return m, nil
		}
		m.closeCreate()
		return m, nil

	// Options are rendered bottom-up (index 0 next to the input), so "up"
	// walks further down the list.
	case "up":
		if opts := m.createOptions(); m.create.sel < len(opts)-1 {
			m.create.sel++
		}
		return m, nil

	case "down":
		if m.create.sel > 0 {
			m.create.sel--
		}
		return m, nil

	case "enter":
		opts := m.createOptions()
		if m.create.sel >= len(opts) {
			return m, nil
		}
		return m.chooseCreateOption(opts[m.create.sel])
	}

	before := m.create.input.Value()
	var cmd tea.Cmd
	m.create.input, cmd = m.create.input.Update(msg)
	if m.create.input.Value() != before {
		// The option list changed under the selection, so it no longer points
		// at what the user saw.
		m.create.sel = 0
	}
	return m, cmd
}

// chooseCreateOption acts on the highlighted row: an existing branch is
// created straight away, while a branch that does not exist yet needs a base
// branch first.
func (m *Model) chooseCreateOption(opt createOption) (tea.Model, tea.Cmd) {
	switch opt.kind {
	case optBranch:
		m.create.req = createRequest{
			repoRoot: opt.cand.RepoRoot,
			repo:     opt.cand.Repo,
			branch:   opt.cand.Branch,
			startRef: opt.cand.Remote,
			mode:     m.create.mode,
		}
		return m.startCreate()

	case optBase:
		m.create.req.base = opt.text
		return m.startCreate()

	// New branches go in the superproject: a submodule branch is created with
	// git and then picked from the list like any other.
	case optIssue:
		m.create.req = createRequest{repoRoot: m.repoRoot, issue: opt.text, mode: m.create.mode}
	case optNewBranch:
		m.create.req = createRequest{repoRoot: m.repoRoot, branch: opt.text, mode: m.create.mode}
	}

	m.create.stage = stageBase
	m.create.bases = worktree.BaseRefs(m.create.req.repoRoot)
	m.create.sel = 0
	m.create.branchQuery = m.create.input.Value()
	m.create.input.SetValue("")
	return m, nil
}

func (m *Model) startCreate() (tea.Model, tea.Cmd) {
	m.create.busy = true
	return m, createWorktreeCmd(m.create.req)
}

// createOptions builds the picker's rows for the current stage, best match
// first.
func (m *Model) createOptions() []createOption {
	query := strings.TrimSpace(m.create.input.Value())
	if m.create.stage == stageBase {
		return baseOptions(m.create.bases, query)
	}
	return branchOptions(m.create.cands, query)
}

func branchOptions(cands []worktree.Candidate, query string) []createOption {
	var opts []createOption
	exact := false
	for _, c := range cands {
		name := c.Branch
		if c.Repo != "" {
			name = c.Repo + "/" + c.Branch
		}
		if !fuzzyMatch(query, name) {
			continue
		}
		if c.Branch == query {
			exact = true
		}
		opts = append(opts, createOption{kind: optBranch, cand: c})
	}
	switch {
	case query == "":
	case isDigits(query):
		// A bare number is an issue: its linked branch is created on demand
		// via `gh issue develop`.
		opts = append(opts, createOption{kind: optIssue, text: query})
	case !exact:
		opts = append(opts, createOption{kind: optNewBranch, text: query})
	}
	return opts
}

func baseOptions(bases []string, query string) []createOption {
	var opts []createOption
	exact := false
	for _, b := range bases {
		if !fuzzyMatch(query, b) {
			continue
		}
		if b == query {
			exact = true
		}
		opts = append(opts, createOption{kind: optBase, text: b})
	}
	// Any ref is a legal starting point, so the typed text stays available
	// even when it matches no branch (a tag, another remote, a commit).
	if query != "" && !exact {
		opts = append(opts, createOption{kind: optBase, text: query, raw: true})
	}
	return opts
}

// isDigits reports whether s is a run of digits, i.e. an issue number rather
// than a branch name.
func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// createCandidatesMsg carries a refreshed branch list for the picker.
type createCandidatesMsg struct {
	cands []worktree.Candidate
}

// loadCandidatesCmd refreshes the branch list off the UI loop, optionally
// fetching first so branches pushed from elsewhere are offered.
func loadCandidatesCmd(repoRoot string, fetch bool) tea.Cmd {
	return func() tea.Msg {
		if fetch {
			worktree.FetchAll(repoRoot)
		}
		cands, _ := worktree.ListCandidates(repoRoot)
		return createCandidatesMsg{cands: cands}
	}
}

// applyCandidates installs a refreshed branch list, keeping the selection
// within bounds.
func (m *Model) applyCandidates(msg createCandidatesMsg) {
	if !m.create.active {
		return
	}
	m.create.fetching = false
	if msg.cands != nil {
		m.create.cands = msg.cands
	}
	if opts := m.createOptions(); m.create.sel >= len(opts) {
		m.create.sel = max(0, len(opts)-1)
	}
}

// worktreeCreatedMsg reports the outcome of a create request.
type worktreeCreatedMsg struct {
	wt    worktree.Worktree
	notes []string // what happened along the way, for the status line
	err   error
}

func createWorktreeCmd(req createRequest) tea.Cmd {
	return func() tea.Msg { return req.run() }
}

// run resolves the request to a concrete branch — asking GitHub for an issue's
// branch when needed — and adds the worktree. It talks to the network, so it
// only ever runs as a command.
func (r createRequest) run() worktreeCreatedMsg {
	branch, startRef := r.branch, r.startRef
	var notes []string

	if r.issue != "" {
		// A branch may already be linked to the issue (from an earlier run or
		// from the web UI); reuse it rather than linking a second one.
		if existing := pr.LinkedBranch(r.repoRoot, r.issue); existing != "" {
			branch = existing
			notes = append(notes, fmt.Sprintf("issue #%s already had branch '%s'", r.issue, existing))
		} else {
			developed, err := pr.DevelopBranch(r.repoRoot, r.issue, r.base)
			if err != nil {
				return worktreeCreatedMsg{err: fmt.Errorf("gh issue develop %s: %w", r.issue, err)}
			}
			branch = developed
			notes = append(notes, fmt.Sprintf("gh branched '%s' off %s", developed, r.base))
		}
		// gh creates the branch on the remote only, so fetch before checkout.
		worktree.Fetch(r.repoRoot)
		ref, err := worktree.StartRefFor(r.repoRoot, branch)
		if err != nil {
			return worktreeCreatedMsg{err: err}
		}
		startRef = ref
	} else if r.base != "" {
		startRef = worktree.BaseStartRef(r.repoRoot, r.base)
		notes = append(notes, fmt.Sprintf("branched off %s", startRef))
	}

	wt, err := worktree.Create(worktree.CreateOptions{
		RepoRoot: r.repoRoot,
		Repo:     r.repo,
		Branch:   branch,
		StartRef: startRef,
	})
	if err != nil {
		return worktreeCreatedMsg{err: err}
	}

	// Open the worktree's tmux window here, running the project's setup and
	// then its start command (or the watcher, in watch mode), rather than
	// leaving it to the refresh that follows — that one only ever opens a
	// plain shell. Only worktrees arborist just created get the command: the
	// refresh creates windows for any pane-less worktree, so running it there
	// would re-run installs, and relaunch agents, behind the user's back.
	cmd := worktree.WindowCommand(wt, r.mode)
	env := windowEnv(wt)
	switch err := tmux.NewWindowWithCommand(wt.Path, wt.Branch, cmd, env...); {
	case err != nil:
		notes = append(notes, fmt.Sprintf("tmux new-window failed: %v", err))
	case r.mode == worktree.ModeWatch:
		notes = append(notes, "watching git status")
	case cmd != "":
		notes = append(notes, "running setup")
	}
	// windowEnv has assigned the dev port by now; report it like the shell
	// script it replaces did, since the URL is what the user goes looking for.
	if p := port.Load(wt.RepoRoot).Port(wt.Path); p != 0 {
		notes = append(notes, fmt.Sprintf("port %d", p))
	}

	// Mark the linked issue as being worked on, when the repo asked for that.
	// The issue is known outright if the user picked one; otherwise it comes
	// from the branch name, and only from the `<number>-slug` form that
	// `gh issue develop` produces — labelling the wrong issue is worse than
	// labelling none.
	if label := worktree.IssueLabel(wt.Path); label != "" {
		issue := r.issue
		if issue == "" {
			issue = pr.LinkedIssueNumber(wt.Branch)
		}
		if issue != "" {
			if err := pr.AddIssueLabel(wt.Path, issue, label); err != nil {
				notes = append(notes, fmt.Sprintf("label failed: %v", err))
			} else {
				notes = append(notes, fmt.Sprintf("labeled #%s %s", issue, label))
			}
		}
	}

	return worktreeCreatedMsg{wt: wt, notes: notes}
}

// windowEnv is the context arborist exports into a worktree's tmux window so
// the setup and start commands can use it, e.g.
// `make issue-fetch ISSUE=$ARBORIST_ISSUE`. ARBORIST_ISSUE is empty when the
// branch name carries no number, and ARBORIST_PORT when the repository does
// not use dev ports.
//
// It assigns the worktree's dev port as a side effect, so a window always
// opens with one: assignment is idempotent, and a worktree's port must not
// change between windows.
func windowEnv(wt worktree.Worktree) []string {
	env := []string{
		"ARBORIST_WORKTREE=" + wt.Path,
		"ARBORIST_BRANCH=" + wt.Branch,
		"ARBORIST_ISSUE=" + pr.IssueNumberFromBranch(wt.Branch),
	}
	if p := assignPort(wt); p != 0 {
		env = append(env, fmt.Sprintf("ARBORIST_PORT=%d", p))
	}
	return env
}

// assignPort gives the worktree its dev port, returning 0 when the repository
// does not use them.
func assignPort(wt worktree.Worktree) int {
	p, err := port.Load(wt.RepoRoot).Assign(wt.Path)
	if err != nil {
		return 0
	}
	return p
}

// applyCreated closes the picker and, on success, refreshes the dashboard with
// the new worktree focused — refreshAll also gives it a tmux window.
func (m *Model) applyCreated(msg worktreeCreatedMsg) tea.Cmd {
	m.closeCreate()
	if msg.err != nil {
		m.message = fmt.Sprintf("create failed: %v", msg.err)
		return nil
	}

	m.expanded = false
	// Creating a worktree wrote its files, so it counts as activity: without
	// this the new tile would be hidden the moment it appears whenever the
	// active-only filter is on.
	m.activity.Record(msg.wt.Path, activity.KindFile, time.Now())
	cmd := m.refreshAll()
	found := false
	for i, row := range m.rows {
		if row.Worktree.Path == msg.wt.Path {
			m.cursorIdx = i
			m.ensureCursorVisible()
			found = true
			break
		}
	}
	m.message = fmt.Sprintf("Created worktree '%s'", msg.wt.Branch)
	if len(msg.notes) > 0 {
		m.message += " · " + strings.Join(msg.notes, " · ")
	}
	if !found {
		// The worktree exists but a filter keeps its tile off screen; say which
		// one rather than leaving the user hunting for it.
		inScope := false
		for _, row := range m.allRows {
			if row.Worktree.Path == msg.wt.Path {
				inScope = true
				break
			}
		}
		if inScope {
			m.message += " · hidden by the active-only filter (press f)"
		} else {
			m.message += fmt.Sprintf(" · hidden by scope '%s' (press s)", m.scope)
		}
	}
	return cmd
}

const (
	// createBoxW is the picker's preferred width; maxPickerVisible caps how
	// many options it lists at once.
	createBoxW       = 76
	maxPickerVisible = 10
)

var (
	styleCreateBox   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("4")).Padding(0, 1)
	styleCreateTitle = lipgloss.NewStyle().Bold(true)
	styleCreateNew   = lipgloss.NewStyle().Foreground(lipgloss.Color("2")) // green: something will be created
)

// createBoxWidth is the picker's total width, border included.
func (m *Model) createBoxWidth() int {
	return min(createBoxW, max(34, m.width-4))
}

// createTextWidth is the width available to a line of text inside the box:
// the total minus the border (2) and the box's horizontal padding (2).
func (m *Model) createTextWidth() int {
	return m.createBoxWidth() - 4
}

func (m *Model) createInputWidth() int {
	return max(10, m.createTextWidth()-4)
}

// renderCreateView draws the create-worktree picker centered over the
// dashboard. Options are listed bottom-up so the selection sits next to the
// input, matching insert mode's history list.
func (m *Model) renderCreateView() string {
	textW := m.createTextWidth()

	var b strings.Builder
	title, hint := m.createPrompt()
	if m.create.fetching {
		title += styleDim.Render(" · fetching…")
	}
	b.WriteString(styleCreateTitle.Render(title) + "\n")
	b.WriteString(styleDim.Render(truncateToWidth(hint, textW)) + "\n\n")

	box := styleCreateBox.Width(m.createBoxWidth() - 2)
	if m.create.busy {
		b.WriteString(truncateToWidth(m.createBusyLabel(), textW) + "\n")
		return m.placeBox(box.Render(b.String()))
	}

	opts := m.createOptions()
	start := 0
	if m.create.sel >= maxPickerVisible {
		start = m.create.sel - maxPickerVisible + 1
	}
	end := min(len(opts), start+maxPickerVisible)
	for i := end - 1; i >= start; i-- {
		b.WriteString(renderCreateOption(opts[i], i == m.create.sel, textW) + "\n")
	}
	if len(opts) == 0 {
		b.WriteString(styleDim.Render("  no matching branch") + "\n")
	}

	b.WriteString(m.create.input.View() + "\n")

	back := "esc: cancel"
	if m.create.stage == stageBase {
		back = "esc: back"
	}
	b.WriteString("\n" + styleDim.Render(truncateToWidth("enter: create worktree  ↑/↓: select  "+back, textW)))

	return m.placeBox(box.Render(b.String()))
}

func (m *Model) placeBox(box string) string {
	if m.width == 0 || m.height == 0 {
		return box
	}
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

// createPrompt returns the picker's title and one-line explanation. The title
// always says which mode the flow is in, since that decides whether the new
// window starts an agent or just watches the worktree.
func (m *Model) createPrompt() (title, hint string) {
	title = "New worktree"
	if m.create.mode == worktree.ModeWatch {
		title += " · watch"
	}
	if m.create.stage == stageBase {
		what := fmt.Sprintf("branch '%s'", m.create.req.branch)
		if m.create.req.issue != "" {
			what = fmt.Sprintf("the branch for issue #%s", m.create.req.issue)
		}
		return title + " · base", fmt.Sprintf("Start %s from…", what)
	}
	return title, "pick a branch, type a new branch name, or type an issue number"
}

// createBusyLabel describes the work in flight while the request runs.
func (m *Model) createBusyLabel() string {
	if m.create.req.issue != "" {
		return fmt.Sprintf("Creating worktree for issue #%s…", m.create.req.issue)
	}
	return fmt.Sprintf("Creating worktree for '%s'…", m.create.req.branch)
}

// renderCreateOption renders one picker row: the branch (or the thing that
// will be created) on the left, dim context on the right.
func renderCreateOption(opt createOption, selected bool, width int) string {
	var label, meta string
	create := true
	switch opt.kind {
	case optBranch:
		create = false
		label = opt.cand.Branch
		var parts []string
		if opt.cand.Repo != "" {
			parts = append(parts, opt.cand.Repo)
		}
		if opt.cand.Remote != "" {
			// Only the remote name: the rest of the ref repeats the branch.
			remote, _, _ := strings.Cut(opt.cand.Remote, "/")
			parts = append(parts, remote)
		}
		meta = strings.Join(parts, " · ")
	case optIssue:
		label = "+ branch for issue #" + opt.text
		meta = "gh issue develop"
	case optNewBranch:
		label = "+ new branch '" + opt.text + "'"
	case optBase:
		create = opt.raw
		label = opt.text
		if opt.raw {
			label = "+ base '" + opt.text + "'"
		}
	}

	marker := "  "
	if selected {
		marker = "▸ "
	}
	avail := width - lipgloss.Width(marker)

	labelW := avail
	if meta != "" && avail-lipgloss.Width(meta)-1 >= 12 {
		labelW = avail - lipgloss.Width(meta) - 1
	} else {
		meta = ""
	}

	text := truncateToWidth(label, labelW)
	pad := max(1, labelW-lipgloss.Width(text)+1)
	switch {
	case selected:
		text = styleHistSelected.Render(text)
	case create:
		text = styleCreateNew.Render(text)
	}

	line := marker + text
	if meta != "" {
		line += strings.Repeat(" ", pad) + styleDim.Render(meta)
	}
	return line
}
