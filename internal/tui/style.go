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

// toneColors is the palette: one ANSI color per SegmentKind, indexed by it.
//
// Which state gets which tone is decided in tone.go; this is the only place a tone becomes a
// color, so the whole board can be recolored without touching a single state.
var toneColors = [...]color.Color{
	SegPlain:   nil,
	SegRef:     lipgloss.Blue,
	SegMuted:   lipgloss.BrightBlack,
	SegDim:     lipgloss.BrightBlack,
	SegGood:    lipgloss.Green,
	SegCaution: lipgloss.Yellow,
	SegAlert:   lipgloss.Red,
	SegDone:    lipgloss.Magenta,
}

// styles holds every style the board renders with, built once per program.
type styles struct {
	columnHeader        lipgloss.Style
	columnHeaderFocused lipgloss.Style
	cardTitle           lipgloss.Style
	cardTitleFocused    lipgloss.Style
	cardTitleSelected   lipgloss.Style
	boxTitle            lipgloss.Style
	footer              lipgloss.Style
	status              lipgloss.Style
	alert               lipgloss.Style
	prompt              lipgloss.Style
	dim                 lipgloss.Style
	heading             lipgloss.Style

	// tones is toneColors turned into styles, indexed by SegmentKind.
	tones []lipgloss.Style
}

func newStyles() styles {
	return styles{
		columnHeader:        lipgloss.NewStyle().Bold(true),
		columnHeaderFocused: lipgloss.NewStyle().Bold(true).Reverse(true),
		cardTitle:           lipgloss.NewStyle(),
		cardTitleFocused:    lipgloss.NewStyle().Bold(true),
		cardTitleSelected:   lipgloss.NewStyle().Reverse(true),
		boxTitle:            lipgloss.NewStyle().Bold(true),
		footer:              lipgloss.NewStyle().Foreground(lipgloss.BrightBlack),
		status:              lipgloss.NewStyle().Foreground(lipgloss.Green),
		alert:               lipgloss.NewStyle().Foreground(lipgloss.Red),
		prompt:              lipgloss.NewStyle().Bold(true),
		dim:                 lipgloss.NewStyle().Foreground(lipgloss.BrightBlack),
		heading:             lipgloss.NewStyle().Bold(true),
		tones:               newToneStyles(),
	}
}

// newToneStyles builds one style per tone.
//
// Red is bold and grey-as-metadata is faint, which are the two tones that have to survive being
// read at a glance in a terminal whose theme may render either of them close to its foreground.
func newToneStyles() []lipgloss.Style {
	tones := make([]lipgloss.Style, len(toneColors))
	for kind, c := range toneColors {
		style := lipgloss.NewStyle()
		if c != nil {
			style = style.Foreground(c)
		}
		switch SegmentKind(kind) {
		case SegAlert:
			style = style.Bold(true)
		case SegDim:
			style = style.Faint(true)
		}
		tones[kind] = style
	}
	return tones
}

// segment styles the given card segment. An out-of-range kind is drawn plain rather than panicking:
// the board is the last thing that should crash over a color.
func (s styles) segment(kind SegmentKind) lipgloss.Style {
	if int(kind) < 0 || int(kind) >= len(s.tones) {
		return lipgloss.NewStyle()
	}
	return s.tones[kind]
}
