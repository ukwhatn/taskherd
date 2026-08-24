package herdrc

import (
	"encoding/json"
	"fmt"
)

// Agent states as reported by herdr. A resumed agent that finished a turn reports "done",
// which is distinct from "idle".
const (
	StateBlocked = "blocked"
	StateWorking = "working"
	StateDone    = "done"
	StateIdle    = "idle"
	StateUnknown = "unknown"

	// StateOffline is taskherd's own value for a session herdr does not know about.
	StateOffline = "offline"
)

// statePriority orders states for the aggregated badge: the most attention-worthy wins.
var statePriority = map[string]int{
	StateBlocked: 5,
	StateWorking: 4,
	StateDone:    3,
	StateIdle:    2,
	StateUnknown: 1,
}

// Snapshot is the part of herdr's session.snapshot that taskherd reads.
type Snapshot struct {
	Version            string  `json:"version"`
	Protocol           int     `json:"protocol"`
	FocusedWorkspaceID string  `json:"focused_workspace_id"`
	FocusedTabID       string  `json:"focused_tab_id"`
	FocusedPaneID      string  `json:"focused_pane_id"`
	Panes              []Pane  `json:"panes"`
	Agents             []Agent `json:"agents"`
}

// Pane is one terminal pane. Entity ids are pane_id/tab_id/workspace_id, not "id".
type Pane struct {
	PaneID      string            `json:"pane_id"`
	TabID       string            `json:"tab_id"`
	WorkspaceID string            `json:"workspace_id"`
	Cwd         string            `json:"cwd"`
	Focused     bool              `json:"focused"`
	Tokens      map[string]string `json:"tokens"`
}

// Agent is a pane with a detected agent. Agents are the subset of panes taskherd links tasks to.
type Agent struct {
	PaneID        string        `json:"pane_id"`
	TabID         string        `json:"tab_id"`
	WorkspaceID   string        `json:"workspace_id"`
	Agent         string        `json:"agent"`
	AgentStatus   string        `json:"agent_status"`
	Cwd           string        `json:"cwd"`
	Session       *AgentSession `json:"agent_session"`
	TerminalTitle string        `json:"terminal_title_stripped"`
}

// AgentSession is herdr's read-only reference to the agent's native session.
// Claude Code reports kind="id" with the session UUID in Value.
type AgentSession struct {
	Agent  string `json:"agent"`
	Kind   string `json:"kind"`
	Source string `json:"source"`
	Value  string `json:"value"`
}

// SessionID returns the native session id, or "" when the agent reports none.
func (a *Agent) SessionID() string {
	if a == nil || a.Session == nil {
		return ""
	}
	return a.Session.Value
}

// AgentBySessionID finds the live agent running the given native session id.
func (s *Snapshot) AgentBySessionID(sessionID string) (*Agent, bool) {
	if s == nil || sessionID == "" {
		return nil, false
	}
	for i := range s.Agents {
		if s.Agents[i].SessionID() == sessionID {
			return &s.Agents[i], true
		}
	}
	return nil, false
}

// AgentByPaneID finds the agent occupying the given pane.
func (s *Snapshot) AgentByPaneID(paneID string) (*Agent, bool) {
	if s == nil || paneID == "" {
		return nil, false
	}
	for i := range s.Agents {
		if s.Agents[i].PaneID == paneID {
			return &s.Agents[i], true
		}
	}
	return nil, false
}

// AgentPaneIDs lists the panes that currently hold an agent, in snapshot order.
func (s *Snapshot) AgentPaneIDs() []string {
	if s == nil {
		return nil
	}
	ids := make([]string, 0, len(s.Agents))
	for i := range s.Agents {
		if s.Agents[i].PaneID != "" {
			ids = append(ids, s.Agents[i].PaneID)
		}
	}
	return ids
}

// SessionState reports the live state of one linked session, or StateOffline when its pane is gone.
func (s *Snapshot) SessionState(sessionID string) string {
	agent, ok := s.AgentBySessionID(sessionID)
	if !ok {
		return StateOffline
	}
	if agent.AgentStatus == "" {
		return StateUnknown
	}
	return agent.AgentStatus
}

// AggregateState folds several session states into the one shown on a task,
// picking the most attention-worthy. Offline states only win when nothing else is live.
func AggregateState(states []string) string {
	best := ""
	bestRank := 0
	for _, state := range states {
		rank := statePriority[state]
		if rank > bestRank {
			best, bestRank = state, rank
		}
	}
	if best == "" {
		return StateOffline
	}
	return best
}

// decodeSnapshot reads the session.snapshot result, which nests the snapshot under "snapshot".
func decodeSnapshot(result json.RawMessage) (*Snapshot, error) {
	var payload struct {
		Type     string   `json:"type"`
		Snapshot Snapshot `json:"snapshot"`
	}
	if err := json.Unmarshal(result, &payload); err != nil {
		return nil, fmt.Errorf("snapshot を解析できない: %w", err)
	}
	return &payload.Snapshot, nil
}
