package cli

import (
	"context"
	"fmt"

	"github.com/ukwhatn/taskherd/internal/herdrc"
	"github.com/ukwhatn/taskherd/internal/i18n"
	"github.com/ukwhatn/taskherd/internal/model"
)

// sessionState is the live state of one linked session, as reported in --json output.
type sessionState struct {
	State  string `json:"state"`
	Agent  string `json:"agent,omitempty"`
	PaneID string `json:"pane_id,omitempty"`
}

// herdrStatusJSON tells a --json consumer whether the live fields could be filled in at all.
type herdrStatusJSON struct {
	Available bool   `json:"available"`
	Error     string `json:"error,omitempty"`
}

// liveState holds the herdr-derived session states for the tasks of one command.
// The zero value means "not consulted", which is what commands use when no task has a session.
type liveState struct {
	consulted bool
	available bool
	err       error
	states    map[string]sessionState
}

// liveSessions resolves the live state of every session linked to the given tasks.
//
// herdr is only consulted when a task actually has a linked session, so a user without herdr
// never pays for it and never sees a notice about a feature they are not using.
func (a *app) liveSessions(ctx context.Context, tasks []model.Task) liveState {
	linked := false
	for i := range tasks {
		if len(tasks[i].Sessions) > 0 {
			linked = true
			break
		}
	}
	if !linked {
		return liveState{}
	}

	snapshot, status := a.herdr().Probe(ctx)
	if !status.Available {
		return liveState{consulted: true, err: status.Err}
	}

	states := map[string]sessionState{}
	for i := range tasks {
		for _, session := range tasks[i].Sessions {
			state := sessionState{State: snapshot.SessionState(session.SessionID), Agent: session.Agent}
			if agent, ok := snapshot.AgentBySessionID(session.SessionID); ok {
				state.Agent = agent.Agent
				state.PaneID = agent.PaneID
			}
			states[session.SessionID] = state
		}
	}
	return liveState{consulted: true, available: true, states: states}
}

// badge folds a task's sessions into the single state shown next to it.
func (l liveState) badge(task model.Task) string {
	if !l.available || len(task.Sessions) == 0 {
		return ""
	}
	states := make([]string, 0, len(task.Sessions))
	for _, session := range task.Sessions {
		states = append(states, l.states[session.SessionID].State)
	}
	return herdrc.AggregateState(states)
}

// forTasks narrows the states to the sessions of the given tasks, for --json output.
func (l liveState) forTasks(tasks []model.Task) map[string]sessionState {
	if !l.available {
		return nil
	}
	states := make(map[string]sessionState, len(l.states))
	for i := range tasks {
		for _, session := range tasks[i].Sessions {
			if state, ok := l.states[session.SessionID]; ok {
				states[session.SessionID] = state
			}
		}
	}
	return states
}

func (l liveState) statusJSON(t *i18n.Catalog) *herdrStatusJSON {
	if !l.consulted {
		return nil
	}
	payload := &herdrStatusJSON{Available: l.available}
	if l.err != nil {
		payload.Error, _ = i18n.Message(t, l.err)
	}
	return payload
}

// note reports the degradation once, on stderr so that --json stdout stays a single object.
// It is only written in text mode: with --json the same fact is in the herdr field.
func (l liveState) note(a *app) {
	if a.jsonOut || !l.consulted || l.available {
		return
	}
	fmt.Fprintf(a.env.Err, a.text.CLI.Session.HerdrDownNote, l.err)
}
