package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type TMUXRef struct {
	Session string `json:"session"`
	Window  int    `json:"window"`
	Pane    int    `json:"pane"`
}

type State struct {
	Agent     string   `json:"agent"`
	Status    string   `json:"state"`
	Detail    string   `json:"detail"`
	UpdatedAt time.Time `json:"updated_at"`
	TMUX      TMUXRef  `json:"tmux"`
}

const stateFile = ".sideby/agent/state.json"

// ReadState reads the agent state file from the given worktree path.
// Returns nil if the file does not exist.
func ReadState(worktreePath string) *State {
	path := filepath.Join(worktreePath, stateFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil
	}
	return &s
}

// StatePath returns the absolute path to the agent state file for a worktree.
func StatePath(worktreePath string) string {
	return filepath.Join(worktreePath, stateFile)
}
