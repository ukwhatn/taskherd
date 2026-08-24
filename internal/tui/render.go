package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/ukwhatn/taskherd/internal/model"
)

// cardHeight is how many lines one card occupies: a title line and a meta line.
const cardHeight = 2

// View renders the whole board. AltScreen is declared here rather than entered with a command,
// which is how bubbletea v2 handles terminal state.
func (b *Board) View() tea.View {
	view := tea.NewView(b.render())
	view.AltScreen = true
	return view
}

func (b *Board) render() string {
	if b.mode == modeDetail {
		return b.renderDetail()
	}

	prompt := b.renderPrompt()
	promptHeight := 0
	if prompt != "" {
		promptHeight = lipgloss.Height(prompt) + 1
	}

	// One line for the column headers, two for the footer, plus whatever the prompt needs.
	bodyHeight := b.height - 3 - promptHeight
	if bodyHeight < 1 {
		bodyHeight = 1
	}

	sections := []string{b.renderColumns(bodyHeight)}
	if prompt != "" {
		sections = append(sections, prompt)
	}
	sections = append(sections, b.renderFooter())
	return strings.Join(sections, "\n")
}

// renderColumns lays the columns out side by side, each already padded to its own width.
func (b *Board) renderColumns(bodyHeight int) string {
	if len(b.columns) == 0 {
		return b.styles.dim.Render("列が定義されていない")
	}

	layout := LayoutColumns(b.columns, b.colIdx, b.width)
	if len(layout.Widths) == 0 {
		return b.styles.dim.Render("端末が狭すぎて列を表示できない")
	}

	rendered := make([]string, 0, len(layout.Widths))
	for i, width := range layout.Widths {
		index := layout.Start + i
		rendered = append(rendered, b.renderColumn(b.columns[index], index, width, bodyHeight))
	}

	body := lipgloss.JoinHorizontal(lipgloss.Top, rendered...)
	if layout.Start > 0 || layout.End() < len(b.columns) {
		body += "\n" + b.styles.dim.Render(fmt.Sprintf("… 列 %d-%d / %d（h/l で移動）",
			layout.Start+1, layout.End(), len(b.columns)))
	}
	return body
}

func (b *Board) renderColumn(col Column, index, width, bodyHeight int) string {
	block := lipgloss.NewStyle().Width(width).MaxWidth(width)

	header := b.styles.columnHeader
	if index == b.colIdx {
		header = b.styles.columnHeaderFocused
	}
	if c, ok := columnColor(col.Color); ok && index != b.colIdx {
		header = header.Foreground(c)
	}

	title := fmt.Sprintf("%s (%d)", col.Label, len(col.Tasks))
	if col.Collapsed {
		title = fmt.Sprintf("▸%s %d", col.Label, len(col.Tasks))
	}
	lines := []string{header.Render(truncate(title, width))}

	if col.Collapsed {
		lines = append(lines, b.styles.dim.Render(truncate("t で展開", width)))
		return block.Render(strings.Join(lines, "\n"))
	}
	if len(col.Tasks) == 0 {
		lines = append(lines, b.styles.dim.Render("—"))
		return block.Render(strings.Join(lines, "\n"))
	}

	visible := bodyHeight / cardHeight
	if visible < 1 {
		visible = 1
	}
	selected := b.selectedIndex(col)
	offset := scrollOffset(b.offsets[col.Key()], selected, visible, len(col.Tasks))
	b.offsets[col.Key()] = offset

	end := offset + visible
	if end > len(col.Tasks) {
		end = len(col.Tasks)
	}
	for i := offset; i < end; i++ {
		focused := index == b.colIdx && i == selected
		lines = append(lines, b.renderCard(col.Tasks[i], width, focused)...)
	}
	if end < len(col.Tasks) {
		lines = append(lines, b.styles.dim.Render(fmt.Sprintf("… +%d", len(col.Tasks)-end)))
	}
	return block.Render(strings.Join(lines, "\n"))
}

func (b *Board) renderCard(task model.Task, width int, focused bool) []string {
	card := BuildCard(task, BuildSessionBadge(task, b.sessions), BuildLinkBadges(task, b.links), b.deps.now())

	titleStyle := b.styles.cardTitle
	if focused {
		titleStyle = b.styles.cardTitleSelected
	}
	marker := " "
	if focused {
		marker = "▌"
	}

	title := titleStyle.Render(truncate(card.Title, width-1))
	meta := b.renderMeta(card.Meta, width-1)
	return []string{marker + title, " " + meta}
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
			parts = append(parts, "…")
			break
		}
		used += segWidth
		parts = append(parts, b.styles.segment(segment.Kind).Render(segment.Text))
	}
	return strings.Join(parts, " ")
}

// renderPrompt draws whatever overlay has the keyboard: a text input, the session picker or a
// confirmation. The board stays visible above it so the user keeps their context.
func (b *Board) renderPrompt() string {
	switch b.mode {
	case modeInput:
		return b.styles.prompt.Render(b.inputPrompt()) + "\n" + b.input.View() +
			"\n" + b.styles.dim.Render("enter 確定 / esc 取消")

	case modeJump:
		lines := []string{b.styles.prompt.Render(fmt.Sprintf("#%d の移動先セッション", b.jump.taskID))}
		for i, session := range b.jump.sessions {
			marker := "  "
			if i == b.jump.cursor {
				marker = "▌ "
			}
			label := session.Label
			if label == "" {
				label = session.Cwd
			}
			state := b.sessions.State[session.SessionID]
			if !b.sessions.Available {
				state = offlineBadge
			}
			lines = append(lines, fmt.Sprintf("%s%s %s  %s  %s", marker, session.Agent, shortID(session.SessionID), state, label))
		}
		lines = append(lines, b.styles.dim.Render("j/k 選択 / enter 決定 / esc 取消"))
		return strings.Join(lines, "\n")

	case modeConfirm:
		return b.styles.alert.Render(b.confirm.prompt) + "\n" + b.styles.dim.Render("y で実行 / n で中止")

	default:
		return ""
	}
}

// renderFooter shows the key help plus when each live source was last heard from.
func (b *Board) renderFooter() string {
	help := "h/l 列 j/k カード H/L 移動 enter 詳細 g jump a 追加 n note x リンク r/R 取得 t 折り畳み q 終了"

	sync := []string{fmt.Sprintf("herdr: %s", b.herdrFooter())}
	if b.deps.Links != nil {
		sync = append(sync, fmt.Sprintf("live: %s", b.fetchFooter()))
	}

	statusLine := b.styles.dim.Render(strings.Join(sync, "  "))
	if b.status != "" {
		style := b.styles.status
		if b.statusIsError {
			style = b.styles.alert
		}
		statusLine = style.Render(truncate(b.status, b.width)) + "  " + statusLine
	}
	return b.styles.footer.Render(truncate(help, b.width)) + "\n" + truncate(statusLine, b.width)
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

func shortID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}
