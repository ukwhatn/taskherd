package tui

import (
	"fmt"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/ukwhatn/taskherd/internal/model"
)

// SegmentKind tells the view how to style one piece of a card's meta line.
type SegmentKind int

const (
	SegDue SegmentKind = iota
	SegDueOverdue
	SegSession
	SegSessionOffline
	SegLink
	SegLinkStale
	SegLinkAttention
)

// Segment is one styled piece of a card's meta line. The card layer decides the text and what
// kind it is; applying colors is the view's job, which keeps this whole file testable as strings.
type Segment struct {
	Text string
	Kind SegmentKind
}

// Card is the text of one card, split so the view can style each part independently.
type Card struct {
	TaskID int
	Title  string
	Meta   []Segment
}

// BuildCard assembles the card for one task: `#id title` over a meta line of due date,
// session badge and link badges.
func BuildCard(task model.Task, session SessionBadge, links []LinkBadge, now time.Time) Card {
	card := Card{TaskID: task.ID, Title: fmt.Sprintf("#%d %s", task.ID, task.Title)}

	if task.Due != nil {
		kind := SegDue
		if isOverdue(*task.Due, now) {
			kind = SegDueOverdue
		}
		card.Meta = append(card.Meta, Segment{Text: string(*task.Due), Kind: kind})
	}
	if session.Text != "" {
		kind := SegSession
		if session.Text == offlineBadge {
			kind = SegSessionOffline
		}
		card.Meta = append(card.Meta, Segment{Text: session.Text, Kind: kind})
	}
	for _, badge := range links {
		text := badge.Text
		kind := SegLink
		switch {
		case badge.Stale:
			kind = SegLinkStale
			text = fmt.Sprintf("%s %s", text, FormatAge(badge.Age))
		case badge.Attention:
			kind = SegLinkAttention
		}
		card.Meta = append(card.Meta, Segment{Text: text, Kind: kind})
	}
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

// truncate shortens s to at most width display columns, marking the cut with an ellipsis.
// Width is measured in terminal cells, so wide characters count double.
func truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	if width == 1 {
		return "…"
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
	return string(out) + "…"
}
