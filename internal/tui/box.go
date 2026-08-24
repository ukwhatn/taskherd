package tui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
)

// Box geometry. A box spends two cells on its side borders and two more on the padding inside
// them, and one line each on its title edge and its bottom border.
const (
	boxChrome  = 4
	boxMinimum = 16
)

// modal is one dialog: a rounded box whose top edge carries its title.
type modal struct {
	title string
	// body is already styled and already trimmed to modalInner(width) cells.
	body []string
	// help is the dim key hint drawn as the box's last line.
	help string
	// width is the box's outer width, from modalWidth.
	width int
	// focused marks the box that has the keyboard. A box with another dialog open over it is
	// drawn dim instead, so a stack of them still reads in the right order.
	focused bool
}

// modalWidth caps a dialog's preferred width to what the terminal can give it, leaving a margin
// on both sides so the board stays visible around the box.
func (b *Board) modalWidth(preferred int) int {
	width := preferred
	if limit := b.width - 4; width > limit {
		width = limit
	}
	if width < boxMinimum {
		width = boxMinimum
	}
	return width
}

// modalInner is the text width inside a box of the given outer width.
func modalInner(width int) int {
	inner := width - boxChrome
	if inner < 1 {
		inner = 1
	}
	return inner
}

// modalBody is how many lines a dialog may fill, leaving a row of board above and below it.
func (b *Board) modalBody(reserve int) int {
	body := b.height - 4 - reserve
	if body < 1 {
		body = 1
	}
	return body
}

// renderModal draws the dialog. Lines past what the terminal has room for are dropped under an
// ellipsis rather than pushing the box off screen.
func (b *Board) renderModal(m modal) string {
	inner := modalInner(m.width)

	lines := append([]string(nil), m.body...)
	if m.help != "" {
		lines = append(lines, b.styles.dim.Render(truncate(m.help, inner)))
	}
	if limit := b.modalBody(0); len(lines) > limit {
		lines = append(lines[:limit-1:limit-1], b.styles.dim.Render(truncateMark))
	}

	edge := b.boxColor(m.focused)
	body := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderTop(false).
		BorderForeground(edge).
		Padding(0, 1).
		Width(m.width).
		MaxWidth(m.width).
		Render(strings.Join(lines, "\n"))
	return b.boxTop(m.title, m.width, edge) + "\n" + body
}

// boxTop draws the box's top border with the title set into it, which is what makes a dialog read
// as a window rather than as more board. The title is measured in display cells so a Japanese one
// lines the edge up as well as an ASCII one does.
func (b *Board) boxTop(title string, width int, edge color.Color) string {
	rule := lipgloss.NewStyle().Foreground(edge)
	if width < boxMinimum || title == "" {
		return rule.Render("╭" + strings.Repeat("─", maxInt(width-2, 0)) + "╮")
	}
	label := truncate(title, width-6)
	fill := width - 5 - lipgloss.Width(label)
	return rule.Render("╭─ ") +
		b.styles.boxTitle.Render(label) +
		rule.Render(" "+strings.Repeat("─", fill)+"╮")
}

func (b *Board) boxColor(focused bool) color.Color {
	if focused {
		return accentColor
	}
	return dimColor
}

// spliceModal lays a dialog over the middle rows of the screen it was opened from.
//
// Whole rows are replaced rather than single cells: splicing a box into the middle of a row would
// mean cutting styled text apart, and the escape sequences do not survive being cut.
func spliceModal(background, box string, width, height int) string {
	if box == "" {
		return background
	}
	rows := strings.Split(background, "\n")
	for len(rows) < height {
		rows = append(rows, "")
	}
	if len(rows) > height && height > 0 {
		rows = rows[:height]
	}

	lines := strings.Split(box, "\n")
	if len(lines) > len(rows) {
		lines = lines[:len(rows)]
	}

	pad := strings.Repeat(" ", maxInt((width-lipgloss.Width(box))/2, 0))
	top := maxInt((len(rows)-len(lines))/2, 0)

	// The row above and below the box are cleared too. Without them the box lands against a card
	// whose own border it has just cut through, and the half-drawn card reads as a glitch.
	for i := top - 1; i <= top+len(lines); i++ {
		if i >= 0 && i < len(rows) {
			rows[i] = ""
		}
	}
	for i, line := range lines {
		if top+i >= len(rows) {
			break
		}
		rows[top+i] = pad + line
	}
	return strings.Join(rows, "\n")
}

// padLines grows a block to exactly height lines, which is what anchors the footer to the bottom
// of the terminal instead of letting it float up under a half-empty board.
func padLines(block string, height int) string {
	rows := strings.Split(block, "\n")
	for len(rows) < height {
		rows = append(rows, "")
	}
	if len(rows) > height {
		rows = rows[:height]
	}
	return strings.Join(rows, "\n")
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
