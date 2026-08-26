package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/ukwhatn/taskherd/internal/fetch"
	"github.com/ukwhatn/taskherd/internal/i18n"
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
	Icons IconSet
	// Text is the language the rows are worded in. Nil falls back to the default catalog.
	Text       *i18n.Catalog
	Classifier model.URLClassifier
	// MaxLinks caps the link rows before the rest fold into one summary row. Zero means the
	// default, maxCardLinkRows.
	MaxLinks int
}

// text is the catalog the rows are worded from, never nil.
func (s CardStyle) text() *i18n.Catalog { return i18n.OrDefault(s.Text) }

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

// maxTitleLines is how many lines a card may spend on its title.
//
// One line is what made the board unreadable: a column on a split laptop screen holds under ten
// Japanese characters per line, well short of a typical title. Two doubles that for the cost of
// one line per card, and a third would buy less than it costs in cards per column.
const maxTitleLines = 2

// wrapTitle breaks s into at most maxLines lines of width display cells, marking the cut when the
// text runs past the last of them.
//
// The break is by cell rather than by word: the titles are mostly Japanese, which has no spaces to
// break on, and their ASCII is ids and URL fragments a word break would not help.
func wrapTitle(s string, width, maxLines int) []string {
	if width <= 0 || maxLines <= 0 {
		return nil
	}
	runes := []rune(normalizeTitle(s))
	if len(runes) == 0 {
		return []string{""}
	}

	lines := make([]string, 0, maxLines)
	for i := 0; i < len(runes); {
		end := takeCells(runes, i, width)
		// The last line carries whatever is left, and says so when that does not fit. So does a
		// line too narrow for even one character, which has nothing to break on.
		if len(lines) == maxLines-1 || end == i {
			return append(lines, truncate(string(runes[i:]), width))
		}
		lines = append(lines, strings.TrimRight(string(runes[i:end]), " "))
		for i = end; i < len(runes) && runes[i] == ' '; i++ {
		}
	}
	return lines
}

// takeCells returns the end of the longest run of runes from start that fits in width cells.
func takeCells(runes []rune, start, width int) int {
	used, end := 0, start
	for ; end < len(runes); end++ {
		w := lipgloss.Width(string(runes[end]))
		if used+w > width {
			break
		}
		used += w
	}
	return end
}

// normalizeTitle folds a title's whitespace into single spaces, so one that arrived carrying a
// newline is still drawn as the single heading a card treats it as.
func normalizeTitle(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
