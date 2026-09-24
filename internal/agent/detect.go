package agent

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Activity represents the current activity state of an agent.
type Activity int

const (
	ActivityIdle    Activity = iota // no agent detected
	ActivityWaiting                 // agent exists but waiting for user input
	ActivityRunning                 // agent actively executing tools
)

// AgentInfo holds detection results for a worktree's tmux pane.
type AgentInfo struct {
	// Name is the agent found in the pane ("claude", "codex"), empty when the
	// pane is running something else.
	Name     string
	Activity Activity
	// Command is the pane's foreground command when that command is not a shell
	// — a test run, a build, a `watch` loop. Empty for an idle shell prompt.
	Command string
	// Working reports that Command is work rather than something merely left
	// running: a test run counts, a log tail does not.
	Working bool
}

// shellCommands are the foreground commands that mean "nothing is running
// here": a pane waiting at a prompt.
var shellCommands = map[string]bool{
	"bash": true, "zsh": true, "sh": true, "fish": true, "dash": true,
	"ksh": true, "csh": true, "tcsh": true, "tmux": true, "screen": true,
	"login": true, "su": true,
}

// workCommands are the foreground processes that count as work in the worktree
// they run in. The list is deliberately closed: treating every non-shell process
// as work means a `watch` loop, a log tail or an editor left open in a corner
// pins its worktree on screen for ever, which is what the active-only filter
// exists to prevent. Work that is not on this list still shows up through the
// files it writes.
var workCommands = map[string]bool{
	"rspec": true, "rake": true, "rubocop": true, "cucumber": true,
	"jest": true, "vitest": true, "eslint": true, "tsc": true,
	"webpack": true, "vite": true, "esbuild": true,
	"pytest": true, "tox": true, "mypy": true, "ruff": true, "black": true,
	"cargo": true, "make": true, "gradle": true, "mvn": true, "phpunit": true,
	"cypress": true, "playwright": true, "gotestsum": true,
}

// genericInterpreters are process names worth looking past: they say how a tool
// was started, not which tool it is.
var genericInterpreters = map[string]bool{
	"ruby": true, "node": true, "python": true, "python3": true, "bundle": true,
	"go": true, "npm": true, "npx": true, "yarn": true, "pnpm": true, "sh": true,
}

// runnerTokens are the tools worth naming when an interpreter is in the
// foreground. `bundle exec rspec` appears as "ruby" in tmux; "rspec" is what the
// user actually started. Matching one of these is itself evidence of work, so a
// dev server started through the same interpreter (`npm run dev`, which matches
// nothing here) does not count.
var runnerTokens = []string{
	"rspec", "rubocop", "rake", "jest", "vitest", "pytest", "cypress",
	"playwright", "webpack", "eslint", "tsc", "gotestsum",
	"go test", "cargo test", "npm test", "yarn test", "pnpm test", "mix test",
}

// DetectAll inspects all tmux panes and returns a map from worktree path to what
// that pane is doing. It walks the process tree from each pane's PID to find
// running agents and determine their activity state, and reads the pane's
// foreground command so that other work — a test run, a build — is detected too.
// Panes sitting at a shell prompt are left out.
// Returns an empty map if tmux is unavailable or /proc is not accessible.
func DetectAll() map[string]AgentInfo {
	result := make(map[string]AgentInfo)

	cmd := exec.Command("tmux", "list-panes", "-a", "-F", "#{pane_pid}\t#{pane_current_command}\t#{pane_current_path}")
	out, err := cmd.Output()
	if err != nil {
		return result
	}

	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) != 3 {
			continue
		}
		pid, paneCmd, panePath := parts[0], parts[1], parts[2]

		var info AgentInfo
		if name, agentPID := findAgentInTree(pid, 0); name != "" {
			info = AgentInfo{Name: name, Activity: classifyActivity(agentPID)}
		} else if cmd, working := foregroundCommand(pid, paneCmd); cmd != "" {
			info = AgentInfo{Command: cmd, Working: working, Activity: ActivityRunning}
		} else {
			continue
		}

		// Several panes can share a worktree. Keep the most telling one, so the
		// tile does not flicker between them as the map is rebuilt: an agent
		// outranks work, and work outranks something merely left running.
		if prev, ok := result[panePath]; ok && rank(prev) >= rank(info) {
			continue
		}
		result[panePath] = info
	}

	return result
}

// rank orders panes by how much their contents matter to the tile.
func rank(a AgentInfo) int {
	switch {
	case a.Name != "":
		return 3
	case a.Working:
		return 2
	case a.Command != "":
		return 1
	default:
		return 0
	}
}

// foregroundCommand names the non-agent process running in a pane and reports
// whether it is work. It returns "" when the pane is idle at a shell prompt.
func foregroundCommand(panePID, paneCmd string) (name string, working bool) {
	if paneCmd == "" || shellCommands[paneCmd] {
		return "", false
	}
	if genericInterpreters[paneCmd] {
		// An interpreter says how the tool was started, not which tool it is;
		// its command line does.
		if pid := findPIDByComm(panePID, paneCmd, 0); pid != "" {
			cmdline := readProcCmdline(pid)
			for _, token := range runnerTokens {
				if strings.Contains(cmdline, token) {
					return token, true
				}
			}
		}
	}
	return paneCmd, workCommands[paneCmd]
}

// findPIDByComm searches the process tree below pid for a process with the given
// name, so its command line can say which tool an interpreter is running.
func findPIDByComm(pid, comm string, depth int) string {
	if depth > 6 {
		return ""
	}
	if readProcComm(pid) == comm {
		return pid
	}
	for _, child := range getChildPIDs(pid) {
		if found := findPIDByComm(child, comm, depth+1); found != "" {
			return found
		}
	}
	return ""
}

// classifyActivity checks whether the agent process is actively executing
// tools (has non-node child processes) or waiting for user input.
func classifyActivity(agentPID string) Activity {
	children := getChildPIDs(agentPID)
	for _, child := range children {
		name := readProcComm(child)
		if name != "" && name != "node" {
			return ActivityRunning
		}
	}
	return ActivityWaiting
}

// findAgentInTree walks the process tree starting at pid (up to depth 5),
// looking for claude or codex processes. Returns the agent name and its PID.
func findAgentInTree(pid string, depth int) (name string, agentPID string) {
	if depth > 5 {
		return "", ""
	}

	comm := readProcComm(pid)
	if comm == "" {
		return "", ""
	}

	// Direct match on process name
	if comm == "claude" || comm == "codex" {
		return comm, pid
	}

	// Claude Code runs as a Node.js process — check cmdline
	if comm == "node" {
		if agent := checkCmdlineForAgent(pid); agent != "" {
			return agent, pid
		}
	}

	// Recurse into children
	children := getChildPIDs(pid)
	for _, child := range children {
		if n, p := findAgentInTree(child, depth+1); n != "" {
			return n, p
		}
	}

	return "", ""
}

func readProcComm(pid string) string {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%s/comm", pid))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// readProcCmdline returns a process's command line with the NUL separators
// turned into spaces, so it can be searched for a tool name.
func readProcCmdline(pid string) string {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%s/cmdline", pid))
	if err != nil {
		return ""
	}
	return strings.ReplaceAll(string(data), "\x00", " ")
}

func checkCmdlineForAgent(pid string) string {
	cmdline := readProcCmdline(pid)
	if cmdline == "" {
		return ""
	}
	if strings.Contains(cmdline, "claude") {
		return "claude"
	}
	if strings.Contains(cmdline, "codex") {
		return "codex"
	}
	return ""
}

func getChildPIDs(pid string) []string {
	path := fmt.Sprintf("/proc/%s/task/%s/children", pid, pid)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	fields := strings.Fields(string(data))
	return fields
}
