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

// AgentInfo holds detection results for an agent in a worktree.
type AgentInfo struct {
	Name     string
	Activity Activity
}

// DetectAll inspects all tmux panes and returns a map from worktree path to
// detected agent info. It walks the process tree from each pane's PID to find
// running agent processes and determines their activity state.
// Returns an empty map if tmux is unavailable or /proc is not accessible.
func DetectAll() map[string]AgentInfo {
	result := make(map[string]AgentInfo)

	cmd := exec.Command("tmux", "list-panes", "-a", "-F", "#{pane_pid}\t#{pane_current_path}")
	out, err := cmd.Output()
	if err != nil {
		return result
	}

	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		pid, panePath := parts[0], parts[1]
		if name, agentPID := findAgentInTree(pid, 0); name != "" {
			activity := classifyActivity(agentPID)
			result[panePath] = AgentInfo{Name: name, Activity: activity}
		}
	}

	return result
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

func checkCmdlineForAgent(pid string) string {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%s/cmdline", pid))
	if err != nil {
		return ""
	}
	cmdline := string(data)
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
