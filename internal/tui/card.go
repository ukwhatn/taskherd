package tui

import (
	"fmt"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/ukwhatn/taskherd/internal/fetch"
	"github.com/ukwhatn/taskherd/internal/model"
)

// truncateMark ends a string that had to be cut short.
//
// It is ASCII rather than an ellipsis because it is appended by the one function every rendered
// string passes through, including strings drawn before an icon set has been chosen, and because
// U+2026 is one of the ambiguous-width characters a Japanese font paints two cells wide.
const truncateMark = "~"

// SegmentKind is the tone one piece of a card is drawn in.
//
// The tones are named by what they mean on a board rather than by the states that map onto them:
// green says "nothing needs you", red says "something does", and a PR being open and its checks
// passing are the same green. Naming them per state instead would put the same colour under a
// dozen names and hide that the board only distinguishes this many things at a glance.
//
// The state to tone mapping is in tone.go and the tone to colour mapping in style.go, so a state
// gains a colour in one place and the palette changes in another.
type SegmentKind int

const (
	// SegPlain is the terminal's own foreground: the segment carries no state worth colouring.
	SegPlain SegmentKind = iota
	// SegRef is a link's reference, coloured as a link rather than as a state.
	SegRef
	// SegMuted is grey: a state that is real but inert (draft, idle, not fetched yet).
	SegMuted
	// SegDim is grey and faint: metadata about the value rather than the value (how stale it is,
	// that herdr is offline).
	SegDim
	// SegGood is green, SegCaution yellow and SegAlert red, in that order of wanting attention.
	SegGood
	SegCaution
	SegAlert
	// SegDone is magenta: finished, and so neither good nor in need of attention.
	SegDone
)

// Segment is one styled piece of a card. The card layer decides the text and what kind it is;
// applying colors is the view's job, which keeps this whole file testable as strings.
type Segment struct {
	Text string
	Kind SegmentKind
}

// CardStyle is the presentation a card is built with: which glyph vocabulary to draw, how to read
// a link URL, and how many link rows a card may spend.
type CardStyle struct {
	Icons      IconSet
	Classifier model.URLClassifier
	// MaxLinks caps the link rows before the rest fold into one summary row. Zero means the
	// default, maxCardLinkRows.
	MaxLinks int
}

// Card is the text of one card, split so the view can style each part independently.
type Card struct {
	TaskID int
	Title  string
	// Meta is the one line under the title: due date and session state.
	Meta []Segment
	// Links are the rows under the meta line, one per link.
	Links []LinkRow
}

// BuildCard assembles the card for one task: `#id title`, a meta line of due date and session
// state, then one row per link.
func BuildCard(task model.Task, session SessionBadge, links map[string]fetch.LinkState, style CardStyle, now time.Time) Card {
	card := Card{TaskID: task.ID, Title: fmt.Sprintf("#%d %s", task.ID, task.Title)}

	if task.Due != nil {
		kind := dueTone(*task.Due, now)
		glyph := style.Icons.Due
		if kind == SegAlert {
			glyph = style.Icons.DueOverdue
		}
		card.Meta = append(card.Meta, Segment{Text: joinIcon(glyph, string(*task.Due)), Kind: kind})
	}
	if session.Text != "" {
		card.Meta = append(card.Meta, Segment{Text: session.Text, Kind: sessionTone(session.State)})
	}

	card.Links = BuildLinkRows(task, links, style)
	return card
}

// isOverdue reports whether a due date has passed. The comparison is by calendar day, so a task
// due today is not overdue until tomorrow.
func isOverdue(due model.Date, now time.Time) bool {
	parsed, err := time.Parse("2006-01-02", string(due))
	if err != nil {
		return false
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	return parsed.Before(today)
}

// truncate shortens s to at most width display columns, marking the cut.
// Width is measured in terminal cells, so wide characters count double.
func truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	if width == 1 {
		return truncateMark
	}

	var (
		out   []rune
		total int
	)
	for _, r := range s {
		w := lipgloss.Width(string(r))
		if total+w > width-1 {
			break
		}
		out = append(out, r)
		total += w
	}
	return string(out) + truncateMark
}
