package tui

import (
	"fmt"
	"time"

	"github.com/ukwhatn/taskherd/internal/herdrc"
	"github.com/ukwhatn/taskherd/internal/model"
)

// offlineLabel is what a session badge says when herdr cannot answer, or when the pane a session
// lived in is gone. Both are "no live state", which is why they read the same.
const offlineLabel = "offline"

// SessionStates is the live herdr state of the sessions linked to the board's tasks.
// Available is false while herdr is unreachable, which disables every session badge at once.
type SessionStates struct {
	Available bool
	Err       error
	// State, Pane and Agent are keyed by session id.
	State map[string]string
	Pane  map[string]string
	Agent map[string]string
}

// BuildSessionStates resolves the live state of every session linked to the given tasks.
func BuildSessionStates(snapshot *herdrc.Snapshot, tasks []model.Task) SessionStates {
	states := SessionStates{
		Available: true,
		State:     map[string]string{},
		Pane:      map[string]string{},
		Agent:     map[string]string{},
	}
	for i := range tasks {
		for _, session := range tasks[i].Sessions {
			states.State[session.SessionID] = snapshot.SessionState(session.SessionID)
			states.Agent[session.SessionID] = session.Agent
			if agent, ok := snapshot.AgentBySessionID(session.SessionID); ok {
				states.Pane[session.SessionID] = agent.PaneID
				states.Agent[session.SessionID] = agent.Agent
			}
		}
	}
	return states
}

// UnavailableSessions reports herdr as unreachable, so badges fall back to offline.
func UnavailableSessions(err error) SessionStates {
	return SessionStates{Err: err}
}

// SessionBadge is the one aggregated state shown for a task's sessions.
// Text is empty when the task has no sessions at all: there is nothing to report, as opposed
// to reporting that nothing is known.
type SessionBadge struct {
	State string
	Text  string
}

// sessionLabels name each herdr state. A resumed agent that finished its turn reports "done",
// which is deliberately distinct from "idle".
var sessionLabels = map[string]string{
	herdrc.StateBlocked: "blocked",
	herdrc.StateWorking: "working",
	herdrc.StateDone:    "done",
	herdrc.StateIdle:    "idle",
}

// sessionGlyph is the icon drawn before a state's label.
func sessionGlyph(state string, icons IconSet) string {
	switch state {
	case herdrc.StateBlocked:
		return icons.SessionBlocked
	case herdrc.StateWorking:
		return icons.SessionWorking
	case herdrc.StateDone:
		return icons.SessionDone
	case herdrc.StateIdle:
		return icons.SessionIdle
	default:
		return icons.SessionOffline
	}
}

// sessionStateText spells one session's state out the way every list row and badge shows it.
func sessionStateText(state string, icons IconSet) string {
	label, ok := sessionLabels[state]
	if !ok {
		label = offlineLabel
	}
	return joinIcon(sessionGlyph(state, icons), label)
}

// joinIcon puts a glyph in front of a label, leaving the label alone in a mode that has no glyph
// to put there.
func joinIcon(glyph, label string) string {
	if glyph == "" {
		return label
	}
	return glyph + " " + label
}

// BuildSessionBadge folds a task's sessions into the single badge shown on its card.
func BuildSessionBadge(task model.Task, states SessionStates, icons IconSet) SessionBadge {
	if len(task.Sessions) == 0 {
		return SessionBadge{}
	}
	if !states.Available {
		return SessionBadge{State: herdrc.StateOffline, Text: sessionStateText(herdrc.StateOffline, icons)}
	}

	live := make([]string, 0, len(task.Sessions))
	for _, session := range task.Sessions {
		state, ok := states.State[session.SessionID]
		if !ok {
			state = herdrc.StateOffline
		}
		live = append(live, state)
	}
	aggregate := herdrc.AggregateState(live)

	text := sessionStateText(aggregate, icons)
	if len(task.Sessions) > 1 {
		text = fmt.Sprintf("%s x%d", text, len(task.Sessions))
	}
	return SessionBadge{State: aggregate, Text: text}
}

// FormatAge renders an elapsed time compactly enough to sit inside a card row.
func FormatAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours())/24)
	}
}
