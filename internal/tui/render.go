package tui

import (
	"fmt"
	"image/color"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/ukwhatn/taskherd/internal/herdrc"
)

// footerReserve is the lines the board keeps at the bottom: the key help, the sync line, and one
// blank line separating them from the columns.
const footerReserve = 3

// headerReserve is the lines each column spends before its first card: the header and the blank
// line under it.
const headerReserve = 2

// boardHelp is the footer's key list. The arrow keys are named with the icon set's glyphs, so
// the line stays readable in a terminal without a patched font.
func (b *Board) boardHelp() string {
	return fmt.Sprintf("%s 列  %s カード  tab 移行  enter 詳細  a 追加  delete 削除  g jump  r/R 取得  t 折り畳み  q 終了",
		b.icons.horizontalKeys(), b.icons.verticalKeys())
}

// View renders the whole board. AltScreen is declared here rather than entered with a command,
// which is how bubbletea v2 handles terminal state. Bracketed paste stays on (the default), which
// is what turns a paste into a single tea.PasteMsg instead of a burst of key presses.
func (b *Board) View() tea.View {
	view := tea.NewView(b.render())
	view.AltScreen = true
	return view
}

// render draws the board, then lays each open dialog over it in the order they were opened. The
// board stays visible around every dialog, so a modal never costs the user their context.
func (b *Board) render() string {
	screen := b.renderBoard()

	overlay := b.renderOverlay()
	switch b.baseMode() {
	case modeDetail:
		screen = spliceModal(screen, b.renderDetail(overlay == ""), b.width, b.height)
	case modeAdd:
		screen = spliceModal(screen, b.renderAdd(), b.width, b.height)
	}
	return spliceModal(screen, overlay, b.width, b.height)
}

func (b *Board) renderBoard() string {
	bodyHeight := b.height - footerReserve
	if bodyHeight < 1 {
		bodyHeight = 1
	}
	return padLines(b.renderColumns(bodyHeight), bodyHeight) + "\n\n" + b.renderFooter()
}

// renderColumns lays the columns out side by side with a gutter between them, each already padded
// to its own width.
func (b *Board) renderColumns(bodyHeight int) string {
	if len(b.columns) == 0 {
		return b.styles.dim.Render("列が定義されていない")
	}

	density := ChooseDensity(b.columns, b.width, 0)
	m := density.metrics()
	layout := LayoutColumns(b.columns, b.colIdx, b.width, 0, density)
	if len(layout.Widths) == 0 {
		return b.styles.dim.Render(truncate("端末が狭すぎて列を表示できない", b.width))
	}

	// The sideways-scroll notice only appears when some column is off screen, and it costs the
	// columns a line when it does.
	notice := ""
	if layout.Start > 0 || layout.End() < len(b.columns) {
		notice = b.styles.dim.Render(truncate(fmt.Sprintf("%s 列 %d-%d / %d（%s で移動）",
			truncateMark, layout.Start+1, layout.End(), len(b.columns), b.icons.horizontalKeys()), b.width))
		bodyHeight--
	}

	blocks := make([]string, 0, 2*len(layout.Widths))
	for i, width := range layout.Widths {
		if i > 0 && m.gap > 0 {
			blocks = append(blocks, strings.Repeat(" ", m.gap))
		}
		index := layout.Start + i
		blocks = append(blocks, b.renderColumn(b.columns[index], index, width, bodyHeight, m))
	}

	body := lipgloss.JoinHorizontal(lipgloss.Top, blocks...)
	if m.boardPad > 0 {
		body = lipgloss.NewStyle().PaddingLeft(m.boardPad).Render(body)
	}
	if notice != "" {
		body += "\n" + notice
	}
	return body
}

// renderColumn draws one column: its header, then whatever of its cards fits underneath.
func (b *Board) renderColumn(col Column, index, width, height int, m metrics) string {
	focused := index == b.colIdx
	lines := []string{b.renderColumnHeader(col, focused, width), strings.Repeat(" ", width)}

	avail := height - headerReserve
	switch {
	case col.Collapsed:
		lines = append(lines, b.renderCollapsed(width, m))
	case len(col.Tasks) == 0:
		lines = append(lines, b.renderEmptyColumn(width, m))
	default:
		lines = append(lines, b.renderCards(col, focused, width, avail, m)...)
	}
	return strings.Join(lines, "\n")
}

// renderCards draws the run of cards that fits, with an indicator on either side reporting the
// cards that did not: a column that runs out of room says so rather than cutting cards off.
func (b *Board) renderCards(col Column, focused bool, width, avail int, m metrics) []string {
	cards := make([]Card, len(col.Tasks))
	heights := make([]int, len(col.Tasks))
	for i, task := range col.Tasks {
		cards[i] = BuildCard(task, BuildSessionBadge(task, b.sessions, b.icons), b.links, b.cardStyle(), b.deps.now())
		heights[i] = cardHeight(cards[i], width, m)
	}

	selected := b.selectedIndex(col)
	window := FitCards(heights, m.cardGap, avail, selected, b.offsets[col.Key()])
	b.offsets[col.Key()] = window.Start

	lines := make([]string, 0, avail)
	if window.Above > 0 {
		lines = append(lines, b.overflowIndicator(b.icons.ScrollUp, window.Above, width))
	}
	for i := window.Start; i < window.End; i++ {
		for g := 0; i > window.Start && g < m.cardGap; g++ {
			lines = append(lines, strings.Repeat(" ", width))
		}
		lines = append(lines, b.renderCard(cards[i], col, width, focused && i == selected, m))
	}
	if window.Below > 0 {
		lines = append(lines, b.overflowIndicator(b.icons.ScrollDown, window.Below, width))
	}
	return lines
}

func (b *Board) overflowIndicator(arrow string, count, width int) string {
	return padCell(b.styles.dim.Render(truncate(joinIcon(arrow, fmt.Sprintf("%d件", count)), width)), width)
}

// renderColumnHeader labels the column in its own color, with the card count beside it. The
// focused column is reversed into a filled block, which is what says where the cursor is even when
// that column holds no cards to put a cursor on.
func (b *Board) renderColumnHeader(col Column, focused bool, width int) string {
	label := col.Label
	if col.Collapsed {
		label = joinIcon(b.icons.Collapsed, label)
	}

	style := b.styles.columnHeader
	if c, ok := columnColor(col.Color); ok {
		style = style.Foreground(c)
	}
	if focused {
		style = style.Reverse(true)
	}
	return padCell(style.Render(headerText(label, len(col.Tasks), width)), width)
}

// headerText fits a column's label and count into width, giving up the decoration around them
// before it gives up the label itself: a folded column is narrow enough that the padding and the
// parentheses are the difference between reading "Wontfix" and reading a cut-off label.
func headerText(label string, count, width int) string {
	for _, text := range []string{
		fmt.Sprintf(" %s (%d) ", label, count),
		fmt.Sprintf("%s (%d)", label, count),
		fmt.Sprintf("%s %d", label, count),
	} {
		if lipgloss.Width(text) <= width {
			return text
		}
	}
	return truncate(fmt.Sprintf("%s %d", label, count), width)
}

// renderCollapsed draws a folded terminal column as a narrow box holding the key that opens it.
func (b *Board) renderCollapsed(width int, m metrics) string {
	return b.renderNoteBox("t 展開", width, m)
}

// renderEmptyColumn draws the placeholder that stands in for a column with nothing in it, so the
// column still reads as a column rather than as a gap in the board.
func (b *Board) renderEmptyColumn(width int, m metrics) string {
	return b.renderNoteBox("カードなし", width, m)
}

// renderNoteBox draws one dim box holding a single line of text.
func (b *Board) renderNoteBox(text string, width int, m metrics) string {
	if !m.boxed {
		return padCell(b.styles.dim.Render(truncate(text, width)), width)
	}
	inner := width - 2 - 2*m.padX
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(dimColor).
		Padding(0, m.padX).
		Width(width).
		MaxWidth(width).
		Render(b.styles.dim.Render(truncate(text, inner)))
}

// renderCard draws one task as a card: its title over a line of due date and badges.
//
// The selected card is outlined in its column's color and the rest in the dim border color, which
// is what makes the cursor readable without a marker column stealing width from every card.
func (b *Board) renderCard(card Card, col Column, width int, focused bool, m metrics) string {
	inner := cardInner(width, m)

	titleStyle := b.styles.cardTitle
	if focused {
		titleStyle = b.styles.cardTitleFocused
	}
	title := wrapTitle(card.Title, inner, maxTitleLines)
	for i, line := range title {
		title[i] = titleStyle.Render(line)
	}
	body := strings.Join(title, "\n")
	if len(card.Meta) > 0 {
		body += "\n" + b.renderMeta(card.Meta, inner)
	}
	for _, row := range card.Links {
		body += "\n" + b.renderLinkRow(row, inner)
	}

	if !m.boxed {
		return b.renderCardBar(body, col, width, focused)
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(b.cardEdge(col, focused)).
		Padding(0, m.padX).
		Width(width).
		MaxWidth(width).
		Render(body)
}

// renderCardBar is the card without its box: the narrowest form that still reads as a card, used
// once the terminal cannot afford borders around every one of them.
func (b *Board) renderCardBar(body string, col Column, width int, focused bool) string {
	bar := b.styles.dim.Render(b.icons.CardEdge)
	if focused {
		bar = lipgloss.NewStyle().Foreground(b.cardEdge(col, true)).Render(b.icons.CardEdgeFocused)
	}

	lines := strings.Split(body, "\n")
	for i, line := range lines {
		lines[i] = padCell(bar+" "+line, width)
	}
	return strings.Join(lines, "\n")
}

// cardEdge is the border color of one card: its column's color when the card is selected, and the
// accent when the column's config named no color to use.
func (b *Board) cardEdge(col Column, focused bool) color.Color {
	if !focused {
		return dimColor
	}
	if c, ok := columnColor(col.Color); ok {
		return c
	}
	return accentColor
}

// cardInner is the cells a card drawn in a column of the given width has for its own text: the
// box takes its borders and padding out, and the bar form its edge and the space after it.
//
// renderCard and cardHeight both measure from here, so the height a column scrolls by cannot
// drift from the lines it actually draws.
func cardInner(width int, m metrics) int {
	inner := width - 2
	if m.boxed {
		inner -= 2 * m.padX
	}
	if inner < 1 {
		inner = 1
	}
	return inner
}

// cardHeight is how many lines the card takes once drawn in a column of the given width, which is
// what the column's scrolling is measured in. The title costs as many lines as it wraps to, a task
// with nothing to put on its meta line gives that line back, and every link it holds costs one.
func cardHeight(card Card, width int, m metrics) int {
	lines := len(wrapTitle(card.Title, cardInner(width, m), maxTitleLines)) + len(card.Links)
	if len(card.Meta) > 0 {
		lines++
	}
	if m.boxed {
		lines += 2
	}
	return lines
}

// renderLinkRow draws one link: its icon, the widest reference that still fits, and as much of the
// live state as the cells left over allow.
//
// The reference degrades from owner/repo#123 to repo#123 to #123 rather than being cut off, so the
// number — the part that identifies the link to a person — survives every width the board reaches.
func (b *Board) renderLinkRow(row LinkRow, width int) string {
	var (
		text string
		used int
	)
	if row.Icon.Text != "" {
		text = b.styles.segment(row.Icon.Kind).Render(row.Icon.Text) + " "
		used = lipgloss.Width(row.Icon.Text) + 1
	}

	ref := pickRef(row.Refs, width-used)
	if ref == "" {
		return ""
	}
	used += lipgloss.Width(ref)
	text += b.styles.segment(row.RefKind).Render(ref)

	for _, segment := range row.Status {
		segWidth := lipgloss.Width(segment.Text) + 1
		if used+segWidth > width {
			break
		}
		used += segWidth
		text += " " + b.styles.segment(segment.Kind).Render(segment.Text)
	}
	return b.linkText(row.URL, text)
}

// pickRef takes the widest reference that fits, falling back to a truncated shortest one when even
// that does not.
func pickRef(refs []string, width int) string {
	if width <= 0 || len(refs) == 0 {
		return ""
	}
	for _, ref := range refs {
		if lipgloss.Width(ref) <= width {
			return ref
		}
	}
	return truncate(refs[len(refs)-1], width)
}

// cursorWidth is the cells every row of a list spends on its selection marker, marked or not.
func cursorWidth(icons IconSet) int {
	return lipgloss.Width(icons.Cursor) + 1
}

// renderMeta styles the meta segments and drops the ones that no longer fit, so a narrow column
// keeps the leftmost (most important) badges rather than wrapping.
func (b *Board) renderMeta(segments []Segment, width int) string {
	var (
		parts []string
		used  int
	)
	for _, segment := range segments {
		segWidth := lipgloss.Width(segment.Text)
		if len(parts) > 0 {
			segWidth++
		}
		if used+segWidth > width {
			// The marker costs a separator and a cell of its own. A column too narrow for even
			// that keeps the segments it did fit rather than overflowing to say it dropped one.
			if len(parts) == 0 || used+1+lipgloss.Width(truncateMark) <= width {
				parts = append(parts, truncateMark)
			}
			break
		}
		used += segWidth
		parts = append(parts, b.styles.segment(segment.Kind).Render(segment.Text))
	}
	return strings.Join(parts, " ")
}

// renderOverlay draws whichever picker or confirmation has the keyboard.
func (b *Board) renderOverlay() string {
	switch b.mode {
	case modeStatusSelect:
		return b.renderStatusSelect()
	case modeSessionSelect:
		return b.renderSessionSelect()
	case modeJump:
		return b.renderJump()
	case modeConfirm:
		return b.renderConfirm()
	default:
		return ""
	}
}

func (b *Board) renderJump() string {
	width := b.modalWidth(72)
	inner := modalInner(width)

	lines := make([]string, 0, len(b.jump.sessions))
	for i, session := range b.jump.sessions {
		marker := padCell("", cursorWidth(b.icons))
		if i == b.jump.cursor {
			marker = b.icons.Cursor + " "
		}
		label := session.Label
		if label == "" {
			label = session.Cwd
		}
		state := sessionStateText(b.sessions.State[session.SessionID], b.icons)
		if !b.sessions.Available {
			state = sessionStateText(herdrc.StateOffline, b.icons)
		}
		row := truncate(fmt.Sprintf("%s%s %s  %s  %s", marker, session.Agent, shortID(session.SessionID), state, label), inner)
		if i == b.jump.cursor {
			row = b.styles.cardTitleSelected.Render(row)
		}
		lines = append(lines, row)
	}

	return b.renderModal(modal{
		title:   fmt.Sprintf("#%d の移動先セッション", b.jump.taskID),
		body:    lines,
		help:    fmt.Sprintf("%s 選択 / enter 決定 / esc 取消", b.icons.verticalKeys()),
		width:   width,
		focused: true,
	})
}

func (b *Board) renderConfirm() string {
	width := b.modalWidth(lipgloss.Width(b.confirm.prompt) + boxChrome)
	return b.renderModal(modal{
		title:   "確認",
		body:    []string{b.styles.alert.Render(truncate(b.confirm.prompt, modalInner(width)))},
		help:    "y で実行 / n で中止",
		width:   width,
		focused: true,
	})
}

// renderFooter shows the key help plus when each live source was last heard from.
func (b *Board) renderFooter() string {
	sync := []string{fmt.Sprintf("herdr: %s", b.herdrFooter())}
	if b.deps.Links != nil {
		sync = append(sync, fmt.Sprintf("live: %s", b.fetchFooter()))
	}

	statusLine := b.styles.dim.Render(strings.Join(sync, "  "))
	if b.status != "" {
		statusLine = b.statusLine() + "  " + statusLine
	}
	return b.styles.footer.Render(truncate(b.boardHelp(), b.width)) + "\n" + statusLine
}

// statusLine renders the last message the board reported, in the style its severity calls for.
func (b *Board) statusLine() string {
	style := b.styles.status
	if b.statusIsError {
		style = b.styles.alert
	}
	return style.Render(truncate(b.status, b.width))
}

func (b *Board) herdrFooter() string {
	switch {
	case b.deps.Sessions == nil:
		return "無効"
	case !b.sessions.Available:
		return "オフライン"
	case b.lastHerdrSync.IsZero():
		return "接続中"
	default:
		return b.lastHerdrSync.Format("15:04:05")
	}
}

func (b *Board) fetchFooter() string {
	switch {
	case b.fetching:
		return "取得中"
	case b.lastFetch.IsZero():
		return "未取得"
	default:
		suffix := ""
		if b.backoffSteps > 0 {
			suffix = fmt.Sprintf(" (次回 %s後)", b.refreshInterval())
		}
		return b.lastFetch.Format("15:04:05") + suffix
	}
}

// labelWidth is the display width the modals reserve for a row's label.
const labelWidth = 12

// padLabel pads a label to labelWidth display cells. A %-Ns verb pads by bytes, which leaves rows
// with Japanese labels ragged against rows with ASCII ones.
func padLabel(label string) string {
	return padCell(label, labelWidth)
}

// padCell pads an already-styled line out to width display cells, so the columns beside it start
// where they should.
func padCell(line string, width int) string {
	if pad := width - lipgloss.Width(line); pad > 0 {
		return line + strings.Repeat(" ", pad)
	}
	return line
}

func shortID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}
