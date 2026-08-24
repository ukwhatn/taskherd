package tui

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// columnColors maps the color names config.toml uses onto the 16 ANSI colors.
//
// ANSI colors are used rather than hex so the board follows whatever palette the user's terminal
// theme defines, which also removes any need to detect a light or dark background.
var columnColors = map[string]color.Color{
	"black":   lipgloss.Black,
	"red":     lipgloss.Red,
	"green":   lipgloss.Green,
	"yellow":  lipgloss.Yellow,
	"blue":    lipgloss.Blue,
	"magenta": lipgloss.Magenta,
	"purple":  lipgloss.Magenta,
	"cyan":    lipgloss.Cyan,
	"white":   lipgloss.White,
	"gray":    lipgloss.BrightBlack,
	"grey":    lipgloss.BrightBlack,
}

// accentColor marks whatever has the keyboard when nothing more specific applies: a dialog's
// border, or the cursor in a column config gave no color to.
//
// dimColor is the resting state of every border and every piece of secondary text.
var (
	accentColor color.Color = lipgloss.Cyan
	dimColor    color.Color = lipgloss.BrightBlack
)

// columnColor resolves a config color name, reporting whether it was known. An unknown name is
// left uncolored rather than guessed, so a typo in config shows up as plain text.
func columnColor(name string) (color.Color, bool) {
	c, ok := columnColors[name]
	return c, ok
}

// styles holds every style the board renders with, built once per program.
type styles struct {
	columnHeader        lipgloss.Style
	columnHeaderFocused lipgloss.Style
	cardTitle           lipgloss.Style
	cardTitleFocused    lipgloss.Style
	cardTitleSelected   lipgloss.Style
	boxTitle            lipgloss.Style
	due                 lipgloss.Style
	dueOverdue          lipgloss.Style
	session             lipgloss.Style
	sessionOffline      lipgloss.Style
	link                lipgloss.Style
	linkStale           lipgloss.Style
	linkAttention       lipgloss.Style
	linkOpen            lipgloss.Style
	linkDraft           lipgloss.Style
	linkMerged          lipgloss.Style
	linkClosed          lipgloss.Style
	linkPending         lipgloss.Style
	linkUnfetched       lipgloss.Style
	footer              lipgloss.Style
	status              lipgloss.Style
	alert               lipgloss.Style
	prompt              lipgloss.Style
	dim                 lipgloss.Style
	heading             lipgloss.Style
}

func newStyles() styles {
	return styles{
		columnHeader:        lipgloss.NewStyle().Bold(true),
		columnHeaderFocused: lipgloss.NewStyle().Bold(true).Reverse(true),
		cardTitle:           lipgloss.NewStyle(),
		cardTitleFocused:    lipgloss.NewStyle().Bold(true),
		cardTitleSelected:   lipgloss.NewStyle().Reverse(true),
		boxTitle:            lipgloss.NewStyle().Bold(true),
		due:                 lipgloss.NewStyle().Foreground(lipgloss.BrightBlack),
		dueOverdue:          lipgloss.NewStyle().Foreground(lipgloss.Red).Bold(true),
		session:             lipgloss.NewStyle().Foreground(lipgloss.Cyan),
		sessionOffline:      lipgloss.NewStyle().Foreground(lipgloss.BrightBlack),
		link:                lipgloss.NewStyle().Foreground(lipgloss.Blue),
		linkStale:           lipgloss.NewStyle().Foreground(lipgloss.BrightBlack).Faint(true),
		linkAttention:       lipgloss.NewStyle().Foreground(lipgloss.Red),
		linkOpen:            lipgloss.NewStyle().Foreground(lipgloss.Green),
		linkDraft:           lipgloss.NewStyle().Foreground(lipgloss.BrightBlack),
		linkMerged:          lipgloss.NewStyle().Foreground(lipgloss.Magenta),
		linkClosed:          lipgloss.NewStyle().Foreground(lipgloss.Red),
		linkPending:         lipgloss.NewStyle().Foreground(lipgloss.Yellow),
		linkUnfetched:       lipgloss.NewStyle().Foreground(lipgloss.BrightBlack),
		footer:              lipgloss.NewStyle().Foreground(lipgloss.BrightBlack),
		status:              lipgloss.NewStyle().Foreground(lipgloss.Green),
		alert:               lipgloss.NewStyle().Foreground(lipgloss.Red),
		prompt:              lipgloss.NewStyle().Bold(true),
		dim:                 lipgloss.NewStyle().Foreground(lipgloss.BrightBlack),
		heading:             lipgloss.NewStyle().Bold(true),
	}
}

// segment styles the given card meta segment.
func (s styles) segment(kind SegmentKind) lipgloss.Style {
	switch kind {
	case SegDueOverdue:
		return s.dueOverdue
	case SegDue:
		return s.due
	case SegSession:
		return s.session
	case SegSessionOffline:
		return s.sessionOffline
	case SegLinkStale:
		return s.linkStale
	case SegLinkAttention:
		return s.linkAttention
	case SegLinkOpen:
		return s.linkOpen
	case SegLinkDraft:
		return s.linkDraft
	case SegLinkMerged:
		return s.linkMerged
	case SegLinkClosed:
		return s.linkClosed
	case SegLinkPending:
		return s.linkPending
	case SegLinkUnfetched:
		return s.linkUnfetched
	default:
		return s.link
	}
}
