package tui

import (
	"fmt"
	"strings"

	"github.com/ukwhatn/taskherd/internal/fetch"
	"github.com/ukwhatn/taskherd/internal/model"
)

// maxCardLinkRows caps how many links a card spells out before it summarizes the rest.
//
// A card is a summary, not the link list: past a handful of rows the card stops fitting in a
// column and pushes its neighbours off screen, and the detail modal is where every link is listed.
const maxCardLinkRows = 3

// linkPhase is the coarse state a link's icon is coloured by. It is deliberately smaller than the
// states the providers report: the icon has one glyph to say it with.
type linkPhase int

const (
	phaseUnknown linkPhase = iota
	phaseOpen
	phaseDraft
	phaseMerged
	phaseClosed
)

// LinkRow is one link's line on a card: an icon, the reference the link points at, and whatever
// live state the cache holds for it.
type LinkRow struct {
	// URL is what the row links to, empty on the overflow row.
	URL  string
	Icon Segment
	// Refs name the link, widest first: "owner/repo#123", then "repo#123", then "#123". The
	// renderer takes the first that fits, so the number survives however narrow the column gets.
	Refs    []string
	RefKind SegmentKind
	Status  []Segment
	// Overflow marks the synthetic last row standing in for the links that did not fit.
	Overflow bool
}

// BuildLinkRows lays a task's links out one per row, in the order they were attached.
//
// The reference comes from the URL rather than from anything fetched, so a card names its PR
// before the first refresh cycle and keeps naming it while the network is down; only the state
// after the reference is cache-derived.
func BuildLinkRows(task model.Task, states map[string]fetch.LinkState, style CardStyle) []LinkRow {
	limit := style.MaxLinks
	if limit <= 0 {
		limit = maxCardLinkRows
	}

	rows := make([]LinkRow, 0, len(task.Links))
	for i, link := range task.Links {
		if i == limit && len(task.Links) > limit {
			// The summary keeps the icon column the rows above it use, so it reads as one more
			// row of the same list rather than as text that lost its indent.
			rows = append(rows, LinkRow{
				Icon:     Segment{Text: style.Icons.More, Kind: SegMuted},
				Refs:     []string{fmt.Sprintf("他 %d 件", len(task.Links)-limit)},
				RefKind:  SegMuted,
				Overflow: true,
			})
			break
		}
		state, ok := states[link.URL]
		if !ok {
			state = fetch.LinkState{URL: link.URL, Kind: link.Kind}
		}
		rows = append(rows, buildLinkRow(link, state, style))
	}
	return rows
}

func buildLinkRow(link model.Link, state fetch.LinkState, style CardStyle) LinkRow {
	row := LinkRow{
		URL:     link.URL,
		Icon:    Segment{Text: style.Icons.linkGlyph(link.Kind, linkPhaseOf(state)), Kind: linkTone(state)},
		Refs:    linkRefs(link, style.Classifier),
		RefKind: SegRef,
		Status:  linkStatus(state, style.Icons),
	}

	// A stale value says how old it is, in the dim tone, and keeps every other tone it had. Dimming
	// the whole row is what made a passing build and a failing one look alike: the state is the
	// part being asked for, and how old it is only qualifies it.
	if state.Fetched && state.Stale {
		row.Status = append(row.Status, Segment{Text: FormatAge(state.Age), Kind: SegDim})
	}
	// A refresh that is still failing is said last, in red. Without it a card shows an hour-old
	// value with nothing to distinguish it from a current one, which is the failure that costs
	// most: the board looks like it is working.
	if state.Fetched && state.Err != "" {
		row.Status = append(row.Status, Segment{Text: style.Icons.failureMark(failingAge(state)), Kind: SegAlert})
	}
	return row
}

// failingAge is how long the current run of failures has lasted, empty when the cache does not
// know: an entry written before failures were timed says that it is failing but not since when.
func failingAge(state fetch.LinkState) string {
	if state.FailingSince.IsZero() {
		return ""
	}
	return FormatAge(state.FailingFor)
}

// linkRefs are the ways of naming a link, widest first.
func linkRefs(link model.Link, classifier model.URLClassifier) []string {
	if ref, ok := classifier.GitHubRef(link.URL); ok {
		return []string{
			fmt.Sprintf("%s/%s#%d", ref.Owner, ref.Repo, ref.Number),
			fmt.Sprintf("%s#%d", ref.Repo, ref.Number),
			fmt.Sprintf("#%d", ref.Number),
		}
	}
	if key, ok := classifier.JiraKey(link.URL); ok {
		return []string{key}
	}
	if host := model.LinkHost(link.URL); host != "" {
		return []string{host}
	}
	return []string{link.URL}
}

// linkStatus is the live state drawn after the reference.
func linkStatus(state fetch.LinkState, icons IconSet) []Segment {
	if !state.Fetchable() {
		return nil
	}
	if !state.Fetched {
		if state.Err != "" {
			// Nothing has ever succeeded here, so the failure is the whole state, not a mark
			// qualifying a value.
			return []Segment{{Text: icons.failureMark(failingAge(state)), Kind: SegAlert}}
		}
		return []Segment{{Text: "未取得", Kind: SegMuted}}
	}

	switch state.Kind {
	case model.LinkKindGitHubPR:
		return prStatus(state.GitHub, icons)
	case model.LinkKindGitHubIssue:
		if state.GitHub == nil {
			return []Segment{{Text: "未取得", Kind: SegMuted}}
		}
		word := "open"
		if strings.EqualFold(state.GitHub.State, "CLOSED") {
			word = "closed"
		}
		return []Segment{{Text: word, Kind: issueTone(state.GitHub.State)}}
	case model.LinkKindJira:
		if state.Jira == nil {
			return []Segment{{Text: "未取得", Kind: SegMuted}}
		}
		return []Segment{{Text: state.Jira.StatusName, Kind: jiraTone(state.Jira.StatusCategory)}}
	default:
		return nil
	}
}

func prStatus(data *fetch.GitHubData, icons IconSet) []Segment {
	if data == nil {
		return []Segment{{Text: "未取得", Kind: SegMuted}}
	}

	var segments []Segment
	// Only an icon set with a glyph per state can say it in the icon; the others spell it out.
	if !icons.StateInLinkIcon {
		segments = append(segments, Segment{
			Text: prStateWord(data),
			Kind: prStateTone(data.State, data.IsDraft),
		})
	}
	if glyph, ok := checkMark(fetch.CheckStatus(data.Checks), icons); ok {
		segments = append(segments, Segment{
			Text: icons.tag("CI", glyph),
			Kind: checkTone(fetch.CheckStatus(data.Checks)),
		})
	}
	if tone, ok := reviewTone(data.ReviewDecision); ok {
		segments = append(segments, Segment{Text: icons.tag("rv", reviewMark(data.ReviewDecision, icons)), Kind: tone})
	}
	return segments
}

func prStateWord(data *fetch.GitHubData) string {
	switch {
	case strings.EqualFold(data.State, "MERGED"):
		return "merged"
	case strings.EqualFold(data.State, "CLOSED"):
		return "closed"
	case data.IsDraft:
		return "draft"
	default:
		return strings.ToLower(data.State)
	}
}

// checkMark is the glyph standing for a CI rollup, reporting whether there is one to draw: a
// repository with no checks configured gets no CI mark rather than a mark saying "none".
func checkMark(checks fetch.CheckStatus, icons IconSet) (string, bool) {
	switch checks {
	case fetch.CheckPass:
		return icons.Pass, true
	case fetch.CheckFail:
		return icons.Fail, true
	case fetch.CheckPending:
		return icons.Pending, true
	default:
		return "", false
	}
}

// reviewMark is the glyph standing for a review decision. Review-required borrows the pending
// glyph: both mean the thing has not happened yet.
func reviewMark(decision string, icons IconSet) string {
	switch strings.ToUpper(strings.TrimSpace(decision)) {
	case "APPROVED":
		return icons.Pass
	case "CHANGES_REQUESTED":
		return icons.Fail
	default:
		return icons.Pending
	}
}

// linkPhaseOf reduces a fetched state to the phase its icon is drawn for.
func linkPhaseOf(state fetch.LinkState) linkPhase {
	if !state.Fetched {
		return phaseUnknown
	}
	switch state.Kind {
	case model.LinkKindGitHubPR:
		if state.GitHub == nil {
			return phaseUnknown
		}
		switch {
		case state.GitHub.State == "MERGED":
			return phaseMerged
		case state.GitHub.State == "CLOSED":
			return phaseClosed
		case state.GitHub.IsDraft:
			return phaseDraft
		default:
			return phaseOpen
		}
	case model.LinkKindGitHubIssue:
		if state.GitHub == nil {
			return phaseUnknown
		}
		if state.GitHub.State == "CLOSED" {
			return phaseClosed
		}
		return phaseOpen
	case model.LinkKindJira:
		if state.Jira == nil {
			return phaseUnknown
		}
		switch state.Jira.StatusCategory {
		case "done":
			return phaseMerged
		case "indeterminate":
			return phaseOpen
		default:
			return phaseDraft
		}
	default:
		return phaseUnknown
	}
}
