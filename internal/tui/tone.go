package tui

import (
	"strings"
	"time"

	"github.com/ukwhatn/taskherd/internal/fetch"
	"github.com/ukwhatn/taskherd/internal/herdrc"
	"github.com/ukwhatn/taskherd/internal/model"
)

// Every state the board colours resolves to a tone here, and nowhere else.
//
// These are pure functions of a state so that "what colour is a closed issue" is answerable — and
// testable — without building a board, a terminal or a style. The renderer only ever asks for the
// tone of something it already has; it never decides one itself.

// prStateTone colours a pull request by its own state. Merged is magenta rather than green because
// a merged PR is finished, and the board's green is for what is currently fine, not for what
// succeeded: a card full of green should mean there is nothing to look at.
func prStateTone(state string, draft bool) SegmentKind {
	switch {
	case strings.EqualFold(state, "MERGED"):
		return SegDone
	case strings.EqualFold(state, "CLOSED"):
		return SegAlert
	case draft:
		return SegMuted
	default:
		return SegGood
	}
}

// checkTone colours a CI rollup. A repository with no checks configured is grey, not green: the
// absence of a verdict must not read as a passing one.
func checkTone(checks fetch.CheckStatus) SegmentKind {
	switch checks {
	case fetch.CheckPass:
		return SegGood
	case fetch.CheckFail:
		return SegAlert
	case fetch.CheckPending:
		return SegCaution
	default:
		return SegMuted
	}
}

// reviewTone colours a review decision, reporting whether the decision is one the board draws at
// all: a PR nobody has been asked to review yet has no decision to show.
func reviewTone(decision string) (SegmentKind, bool) {
	switch strings.ToUpper(strings.TrimSpace(decision)) {
	case "APPROVED":
		return SegGood, true
	case "CHANGES_REQUESTED":
		return SegAlert, true
	case "REVIEW_REQUIRED":
		return SegCaution, true
	default:
		return SegPlain, false
	}
}

// issueTone colours an issue by its state. A closed issue is magenta, the same tone as a merged
// PR, because both are the ordinary end of the thing's life; a closed PR is red because it is not.
func issueTone(state string) SegmentKind {
	if strings.EqualFold(state, "CLOSED") {
		return SegDone
	}
	return SegGood
}

// jiraTone colours a Jira issue by its status category, which is the only part of a Jira status
// that means the same thing across projects: the status names themselves are per-project.
func jiraTone(category string) SegmentKind {
	switch strings.ToLower(strings.TrimSpace(category)) {
	case "done":
		return SegGood
	case "indeterminate":
		return SegCaution
	default:
		return SegMuted
	}
}

// sessionTone colours a herdr session state. Blocked is red because it is the one state that is
// waiting on the person reading the board; done is yellow for the same reason at one remove.
func sessionTone(state string) SegmentKind {
	switch state {
	case herdrc.StateBlocked:
		return SegAlert
	case herdrc.StateWorking:
		return SegGood
	case herdrc.StateDone:
		return SegCaution
	case herdrc.StateIdle:
		return SegMuted
	default:
		return SegDim
	}
}

// dueTone colours a due date by how close it is: past is red, today or tomorrow is yellow, and
// anything further out is left in the terminal's own colour.
//
// Further-out dates are deliberately not grey. Grey would say the date is metadata, and a date the
// user typed is not; it is simply not urgent yet.
func dueTone(due model.Date, now time.Time) SegmentKind {
	parsed, err := time.Parse("2006-01-02", string(due))
	if err != nil {
		return SegPlain
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	switch days := int(parsed.Sub(today).Hours() / 24); {
	case days < 0:
		return SegAlert
	case days <= 1:
		return SegCaution
	default:
		return SegPlain
	}
}

// linkTone is the tone a link's own state is drawn in: its icon, and the word after the reference
// in an icon mode that spells the state out.
//
// A link that is failing to refresh keeps the tone of its last known state rather than turning
// red. The failure is said by a mark of its own, so that a merged PR whose refresh is broken still
// reads as merged instead of losing the state the user came to the board for.
func linkTone(state fetch.LinkState) SegmentKind {
	if !state.Fetchable() {
		return SegRef
	}
	if !state.Fetched {
		if state.Err != "" {
			return SegAlert
		}
		return SegMuted
	}
	switch state.Kind {
	case model.LinkKindGitHubPR:
		if state.GitHub == nil {
			return SegMuted
		}
		return prStateTone(state.GitHub.State, state.GitHub.IsDraft)
	case model.LinkKindGitHubIssue:
		if state.GitHub == nil {
			return SegMuted
		}
		return issueTone(state.GitHub.State)
	case model.LinkKindJira:
		if state.Jira == nil {
			return SegMuted
		}
		return jiraTone(state.Jira.StatusCategory)
	default:
		return SegRef
	}
}
