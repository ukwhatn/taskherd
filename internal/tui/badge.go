package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/ukwhatn/taskherd/internal/fetch"
	"github.com/ukwhatn/taskherd/internal/herdrc"
	"github.com/ukwhatn/taskherd/internal/model"
)

// offlineBadge is what a session badge shows when herdr cannot answer, or when the pane a
// session lived in is gone. Both are "no live state", which is why they look the same.
const offlineBadge = "–"

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

// sessionSymbols renders each herdr state. A resumed agent that finished its turn reports
// "done", which is deliberately distinct from "idle".
var sessionSymbols = map[string]string{
	herdrc.StateBlocked: "■blocked",
	herdrc.StateWorking: "●working",
	herdrc.StateDone:    "✓done",
	herdrc.StateIdle:    "◌idle",
}

// BuildSessionBadge folds a task's sessions into the single badge shown on its card.
func BuildSessionBadge(task model.Task, states SessionStates) SessionBadge {
	if len(task.Sessions) == 0 {
		return SessionBadge{}
	}
	if !states.Available {
		return SessionBadge{State: herdrc.StateOffline, Text: offlineBadge}
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

	text, ok := sessionSymbols[aggregate]
	if !ok {
		text = offlineBadge
	}
	if len(task.Sessions) > 1 {
		text = fmt.Sprintf("%s×%d", text, len(task.Sessions))
	}
	return SessionBadge{State: aggregate, Text: text}
}

// LinkBadge is the aggregated live state of every link of one kind on a card.
// Several links of the same kind fold into one badge: a card shows at most one PR badge,
// one issue badge and one Jira badge, however many links it holds.
type LinkBadge struct {
	Kind model.LinkKind
	Text string
	// Stale marks a value older than the cache TTL; the card dims it and shows Age.
	Stale bool
	Age   time.Duration
	// Attention marks a state the user has to act on (failing checks, requested changes,
	// a link whose fetch keeps failing).
	Attention bool
}

// badgeKindOrder fixes the order badges appear in, so a card's look does not depend on the
// order links happened to be added in.
var badgeKindOrder = []model.LinkKind{
	model.LinkKindGitHubPR,
	model.LinkKindGitHubIssue,
	model.LinkKindJira,
}

var badgeKindLabels = map[model.LinkKind]string{
	model.LinkKindGitHubPR:    "PR",
	model.LinkKindGitHubIssue: "Issue",
	model.LinkKindJira:        "Jira",
}

// BuildLinkBadges folds a task's links into one badge per kind.
func BuildLinkBadges(task model.Task, states map[string]fetch.LinkState) []LinkBadge {
	byKind := make(map[model.LinkKind][]fetch.LinkState, len(badgeKindOrder))
	for _, link := range task.Links {
		state, ok := states[link.URL]
		if !ok {
			state = fetch.LinkState{URL: link.URL, Kind: link.Kind}
		}
		if !state.Fetchable() {
			continue
		}
		byKind[state.Kind] = append(byKind[state.Kind], state)
	}

	badges := make([]LinkBadge, 0, len(badgeKindOrder))
	for _, kind := range badgeKindOrder {
		group := byKind[kind]
		if len(group) == 0 {
			continue
		}
		worst := group[0]
		for _, state := range group[1:] {
			if linkAttention(state) > linkAttention(worst) {
				worst = state
			}
		}

		text := fmt.Sprintf("%s:%s", badgeKindLabels[kind], linkSummary(worst))
		if len(group) > 1 {
			text = fmt.Sprintf("%s×%d:%s", badgeKindLabels[kind], len(group), linkSummary(worst))
		}
		badges = append(badges, LinkBadge{
			Kind:      kind,
			Text:      text,
			Stale:     worst.Stale,
			Age:       worst.Age,
			Attention: linkAttention(worst) >= attentionThreshold,
		})
	}
	return badges
}

// Attention ranking of one link. Higher wins when several links of a kind are folded together,
// and anything at or above attentionThreshold is rendered as needing action.
const (
	attentionMerged     = 0
	attentionUnfetched  = 1
	attentionClosed     = 2
	attentionDraft      = 3
	attentionOpen       = 4
	attentionThreshold  = 5
	attentionFetchError = 5
	attentionChanges    = 6
	attentionChecksFail = 7
)

func linkAttention(state fetch.LinkState) int {
	if !state.Fetched {
		// A link that has been tried and keeps failing is worth flagging; one that has simply
		// not been reached yet is not, because the first refresh cycle is still in flight.
		if state.Err != "" {
			return attentionFetchError
		}
		return attentionUnfetched
	}

	switch state.Kind {
	case model.LinkKindGitHubPR:
		switch {
		case state.GitHub == nil:
			return attentionUnfetched
		case state.GitHub.Checks == string(fetch.CheckFail):
			return attentionChecksFail
		case state.GitHub.ReviewDecision == "CHANGES_REQUESTED":
			return attentionChanges
		case state.GitHub.State == "MERGED":
			return attentionMerged
		case state.GitHub.State == "CLOSED":
			return attentionClosed
		case state.GitHub.IsDraft:
			return attentionDraft
		default:
			return attentionOpen
		}
	case model.LinkKindGitHubIssue:
		if state.GitHub == nil || state.GitHub.State == "CLOSED" {
			return attentionClosed
		}
		return attentionOpen
	case model.LinkKindJira:
		if state.Jira == nil {
			return attentionUnfetched
		}
		switch state.Jira.StatusCategory {
		case "done":
			return attentionMerged
		case "indeterminate":
			return attentionOpen
		default:
			return attentionDraft
		}
	default:
		return attentionUnfetched
	}
}

// linkSummary is the state part of a badge, without the kind prefix.
func linkSummary(state fetch.LinkState) string {
	if !state.Fetched {
		if state.Err != "" {
			return "!"
		}
		return "…"
	}

	switch state.Kind {
	case model.LinkKindGitHubPR:
		if state.GitHub == nil {
			return "…"
		}
		var b strings.Builder
		b.WriteString(prStateText(state.GitHub))
		switch state.GitHub.ReviewDecision {
		case "APPROVED":
			b.WriteString("✓rv")
		case "CHANGES_REQUESTED":
			b.WriteString("✗rv")
		}
		b.WriteString(checksText(state.GitHub.Checks))
		return b.String()
	case model.LinkKindGitHubIssue:
		if state.GitHub == nil {
			return "…"
		}
		return strings.ToLower(state.GitHub.State)
	case model.LinkKindJira:
		if state.Jira == nil {
			return "…"
		}
		return state.Jira.StatusName
	default:
		return "…"
	}
}

func prStateText(data *fetch.GitHubData) string {
	if data.State == "OPEN" && data.IsDraft {
		return "draft"
	}
	return strings.ToLower(data.State)
}

func checksText(checks string) string {
	switch fetch.CheckStatus(checks) {
	case fetch.CheckPass:
		return "✓ci"
	case fetch.CheckFail:
		return "✗ci"
	case fetch.CheckPending:
		return "…ci"
	default:
		return ""
	}
}

// FormatAge renders an elapsed time compactly enough to sit inside a card badge.
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
