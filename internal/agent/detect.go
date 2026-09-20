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
	// — a test run, a build, an editor. Empty for an idle shell prompt.
	Command string
}

// Busy reports whether the pane is doing work right now: an agent executing
// tools, or any non-shell foreground process. An agent sitting at its prompt is
// not busy, even though it holds the foreground.
func (a AgentInfo) Busy() bool {
	if a.Name != "" {
		return a.Activity == ActivityRunning
	}
	return a.Command != ""
}

// shellCommands are the foreground commands that mean "nothing is running
// here": a pane waiting at a prompt.
var shellCommands = map[string]bool{
	"bash": true, "zsh": true, "sh": true, "fish": true, "dash": true,
	"ksh": true, "csh": true, "tcsh": true, "tmux": true, "screen": true,
	"login": true, "su": true,
}

// genericInterpreters are process names worth looking past: they say how a tool
// was started, not which tool it is.
var genericInterpreters = map[string]bool{
	"ruby": true, "node": true, "python": true, "python3": true, "bundle": true,
	"go": true, "npm": true, "npx": true, "yarn": true, "pnpm": true, "sh": true,
}

// runnerTokens are the tools worth naming when an interpreter is in the
// foreground, longest-running suspects first. `bundle exec rspec` appears as
// "ruby" in tmux; "rspec" is what the user actually started.
var runnerTokens = []string{
	"rspec", "rubocop", "rake", "jest", "vitest", "pytest", "cypress",
	"playwright", "webpack", "eslint", "tsc", "go test", "cargo",
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

		if name, agentPID := findAgentInTree(pid, 0); name != "" {
			result[panePath] = AgentInfo{Name: name, Activity: classifyActivity(agentPID)}
			continue
		}
		if cmd := foregroundCommand(pid, paneCmd); cmd != "" {
			result[panePath] = AgentInfo{Command: cmd, Activity: ActivityRunning}
		}
	}

	return result
}

// foregroundCommand names the non-agent process running in a pane, or "" when
// the pane is idle at a shell prompt.
func foregroundCommand(panePID, paneCmd string) string {
	if paneCmd == "" || shellCommands[paneCmd] {
		return ""
	}
	if !genericInterpreters[paneCmd] {
		return paneCmd
	}
	pid := findPIDByComm(panePID, paneCmd, 0)
	if pid == "" {
		return paneCmd
	}
	cmdline := readProcCmdline(pid)
	for _, token := range runnerTokens {
		if strings.Contains(cmdline, token) {
			return token
		}
	}
	return paneCmd
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
