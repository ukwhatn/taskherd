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
			rows = append(rows, LinkRow{
				Refs:     []string{fmt.Sprintf("他 %d 件", len(task.Links)-limit)},
				RefKind:  SegLinkUnfetched,
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
	phase := linkPhaseOf(state)
	glyph, tone := style.Icons.linkIcon(link.Kind, phase)

	row := LinkRow{
		URL:     link.URL,
		Icon:    Segment{Text: glyph, Kind: tone},
		Refs:    linkRefs(link, style.Classifier),
		RefKind: SegLink,
		Status:  linkStatus(state, style.Icons),
	}

	switch {
	case !state.Fetchable():
		// An "other" link has no live state to be stale about, so it stays plain.
		row.Icon.Kind = SegLink
	case !state.Fetched:
		row.Icon.Kind = SegLinkUnfetched
		if state.Err != "" {
			row.Icon.Kind = SegLinkAttention
		}
	case state.Stale:
		row.Status = append(row.Status, Segment{Text: FormatAge(state.Age)})
		row.Icon.Kind = SegLinkStale
		row.RefKind = SegLinkStale
		for i := range row.Status {
			row.Status[i].Kind = SegLinkStale
		}
	}
	return row
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
			return []Segment{{Text: "取得失敗", Kind: SegLinkAttention}}
		}
		return []Segment{{Text: "未取得", Kind: SegLinkUnfetched}}
	}

	switch state.Kind {
	case model.LinkKindGitHubPR:
		return prStatus(state.GitHub, icons)
	case model.LinkKindGitHubIssue:
		if state.GitHub == nil {
			return []Segment{{Text: "未取得", Kind: SegLinkUnfetched}}
		}
		if state.GitHub.State == "CLOSED" {
			return []Segment{{Text: "closed", Kind: SegLinkClosed}}
		}
		return []Segment{{Text: "open", Kind: SegLinkOpen}}
	case model.LinkKindJira:
		if state.Jira == nil {
			return []Segment{{Text: "未取得", Kind: SegLinkUnfetched}}
		}
		return []Segment{{Text: state.Jira.StatusName, Kind: jiraKind(state.Jira.StatusCategory)}}
	default:
		return nil
	}
}

func prStatus(data *fetch.GitHubData, icons IconSet) []Segment {
	if data == nil {
		return []Segment{{Text: "未取得", Kind: SegLinkUnfetched}}
	}

	var segments []Segment
	// Only an icon set with a glyph per state can say it in the icon; the others spell it out.
	if !icons.StateInLinkIcon {
		word, kind := prStateWord(data)
		segments = append(segments, Segment{Text: word, Kind: kind})
	}
	if glyph, kind, ok := checkMark(fetch.CheckStatus(data.Checks), icons); ok {
		segments = append(segments, Segment{Text: "CI" + glyph, Kind: kind})
	}
	switch data.ReviewDecision {
	case "APPROVED":
		segments = append(segments, Segment{Text: "rv" + icons.Pass, Kind: SegLinkOpen})
	case "CHANGES_REQUESTED":
		segments = append(segments, Segment{Text: "rv" + icons.Fail, Kind: SegLinkAttention})
	}
	return segments
}

func prStateWord(data *fetch.GitHubData) (string, SegmentKind) {
	switch {
	case data.State == "MERGED":
		return "merged", SegLinkMerged
	case data.State == "CLOSED":
		return "closed", SegLinkClosed
	case data.IsDraft:
		return "draft", SegLinkDraft
	default:
		return strings.ToLower(data.State), SegLinkOpen
	}
}

func checkMark(checks fetch.CheckStatus, icons IconSet) (string, SegmentKind, bool) {
	switch checks {
	case fetch.CheckPass:
		return icons.Pass, SegLinkOpen, true
	case fetch.CheckFail:
		return icons.Fail, SegLinkAttention, true
	case fetch.CheckPending:
		return icons.Pending, SegLinkPending, true
	default:
		return "", SegLink, false
	}
}

func jiraKind(category string) SegmentKind {
	switch category {
	case "done":
		return SegLinkMerged
	case "indeterminate":
		return SegLinkOpen
	default:
		return SegLinkDraft
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
