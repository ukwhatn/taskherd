package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/ukwhatn/taskherd/internal/herdrc"
	"github.com/ukwhatn/taskherd/internal/model"
)

// statusSelectState is the destination picker opened with Tab.
type statusSelectState struct {
	taskID int
	// targets are the columns a task can hold as a status, in board order.
	targets []Column
	cursor  int
}

// beginStatusSelect opens the destination picker on the next column along: moving one step right
// is the common case, so Enter alone does it.
func (b *Board) beginStatusSelect() tea.Cmd {
	task := b.activeTask()
	if task == nil {
		return status("カードが選択されていない", true)
	}
	targets := selectableColumns(b.columns)
	if len(targets) == 0 {
		return status("移行先の列が定義されていない", true)
	}

	b.statusSel = statusSelectState{
		taskID:  task.ID,
		targets: targets,
		cursor:  defaultStatusTarget(targets, task.Status),
	}
	b.openOverlay(modeStatusSelect)
	return nil
}

// defaultStatusTarget is the column the picker opens on: the one after the task's own, or the
// first column when the task sits in (unknown) and has nowhere to step from.
func defaultStatusTarget(targets []Column, current string) int {
	idx := statusIndex(targets, current)
	switch {
	case idx < 0:
		return 0
	case idx+1 < len(targets):
		return idx + 1
	default:
		return idx
	}
}

func (b *Board) handleStatusSelectKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if !isCommandKey(msg) {
		return b, nil
	}
	switch msg.String() {
	case "q", "tab":
		b.closeOverlay()
	case "left":
		if b.statusSel.cursor > 0 {
			b.statusSel.cursor--
		}
	case "right":
		if b.statusSel.cursor < len(b.statusSel.targets)-1 {
			b.statusSel.cursor++
		}
	case "enter":
		target := b.statusSel.targets[b.statusSel.cursor]
		taskID := b.statusSel.taskID
		b.closeOverlay()
		return b, b.setStatusCmd(taskID, target.ID)
	}
	return b, nil
}

func (b *Board) renderStatusSelect() string {
	labels := make([]string, 0, len(b.statusSel.targets))
	for _, col := range b.statusSel.targets {
		labels = append(labels, " "+col.Label+" ")
	}

	// The row is windowed as plain text before any of it is styled: trimming it afterwards would
	// mean cutting a styled cell, and an escape sequence does not survive being cut.
	width := b.modalWidth(lipgloss.Width(strings.Join(labels, " ")) + boxChrome)
	cells, start := visibleCells(labels, b.statusSel.cursor, modalInner(width))
	for i := range cells {
		if start+i == b.statusSel.cursor {
			cells[i] = b.styles.cardTitleSelected.Render(cells[i])
		}
	}

	return b.renderModal(modal{
		title:   fmt.Sprintf("#%d の移行先", b.statusSel.taskID),
		body:    []string{strings.Join(cells, " ")},
		help:    fmt.Sprintf("%s 選択 / enter 確定 / q 閉じる", b.icons.horizontalKeys()),
		width:   width,
		focused: true,
	})
}

// visibleCells picks the run of labels that fits across width cells, sliding the window just far
// enough to keep the cursor's own label in it.
func visibleCells(labels []string, cursor, width int) (cells []string, start int) {
	if len(labels) == 0 {
		return nil, 0
	}
	start = clampIndex(cursor, len(labels))
	for start > 0 && cellsWidth(labels[start-1:cursor+1]) <= width {
		start--
	}
	end := cursor + 1
	for end < len(labels) && cellsWidth(labels[start:end+1]) <= width {
		end++
	}
	return append([]string(nil), labels[start:end]...), start
}

// cellsWidth is how wide a run of labels renders, including the single space between them.
func cellsWidth(labels []string) int {
	total := 0
	for i, label := range labels {
		if i > 0 {
			total++
		}
		total += lipgloss.Width(label)
	}
	return total
}

// sessionSelectState is the agent picker opened from the detail modal's ＋セッション紐づけ row.
type sessionSelectState struct {
	taskID  int
	loading bool
	err     string
	agents  []herdrc.Agent
	cursor  int
}

// beginSessionSelect asks herdr for its current agents and opens the picker over the answer. The
// snapshot is re-read rather than taken from the board's badges, so the list is what herdr has now.
func (b *Board) beginSessionSelect(taskID int) tea.Cmd {
	if b.deps.Herdr == nil {
		return status("herdr に接続できないためセッションを紐づけられない", true)
	}
	b.sessionSel = sessionSelectState{taskID: taskID, loading: true}
	b.openOverlay(modeSessionSelect)
	return b.fetchAgentsCmd()
}

func (b *Board) applyAgents(msg agentsLoadedMsg) {
	b.sessionSel.loading = false
	if msg.err != nil {
		b.sessionSel.err = fmt.Sprintf("herdr に接続できない: %v", msg.err)
		return
	}
	b.sessionSel.agents = msg.agents
	b.sessionSel.cursor = 0
	if len(msg.agents) == 0 {
		b.sessionSel.err = "herdr にエージェントが見つからない"
	}
}

func (b *Board) handleSessionSelectKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if !isCommandKey(msg) {
		return b, nil
	}
	switch msg.String() {
	case "q":
		b.closeOverlay()
	case "up":
		if b.sessionSel.cursor > 0 {
			b.sessionSel.cursor--
		}
	case "down":
		if b.sessionSel.cursor < len(b.sessionSel.agents)-1 {
			b.sessionSel.cursor++
		}
	case "enter":
		return b, b.linkSelectedAgent()
	}
	return b, nil
}

func (b *Board) linkSelectedAgent() tea.Cmd {
	if b.sessionSel.cursor < 0 || b.sessionSel.cursor >= len(b.sessionSel.agents) {
		return nil
	}
	agent := b.sessionSel.agents[b.sessionSel.cursor]
	if agent.SessionID() == "" {
		b.sessionSel.err = fmt.Sprintf(
			"pane %s ではセッション ID を検出できない。herdr integration install claude を実行して再試行する", agent.PaneID)
		return nil
	}

	taskID := b.sessionSel.taskID
	b.closeOverlay()
	return b.addSessionCmd(taskID, model.SessionRef{
		Agent:     agent.Agent,
		SessionID: agent.SessionID(),
		Cwd:       agent.Cwd,
	})
}

func (b *Board) renderSessionSelect() string {
	width := b.modalWidth(76)
	inner := modalInner(width)

	var lines []string
	switch {
	case b.sessionSel.loading:
		lines = append(lines, b.styles.dim.Render("herdr に問い合わせ中..."))
	case len(b.sessionSel.agents) == 0:
		lines = append(lines, b.styles.dim.Render("エージェントがいない"))
	default:
		for i, agent := range b.sessionSel.agents {
			marker := padCell("", cursorWidth(b.icons))
			if i == b.sessionSel.cursor {
				marker = b.icons.Cursor + " "
			}
			id := shortID(agent.SessionID())
			if id == "" {
				id = "(未検出)"
			}
			line := truncate(fmt.Sprintf("%s%-8s %-10s %s", marker, agent.Agent, id, agent.Cwd), inner)
			switch {
			case agent.SessionID() == "":
				line = b.styles.dim.Render(line)
			case i == b.sessionSel.cursor:
				line = b.styles.cardTitleSelected.Render(line)
			}
			lines = append(lines, line)
		}
	}

	if b.sessionSel.err != "" {
		lines = append(lines, b.styles.alert.Render(truncate(b.sessionSel.err, inner)))
	}
	return b.renderModal(modal{
		title:   fmt.Sprintf("#%d に紐づけるセッション", b.sessionSel.taskID),
		body:    lines,
		help:    fmt.Sprintf("%s 選択 / enter 紐づけ / q 閉じる", b.icons.verticalKeys()),
		width:   width,
		focused: true,
	})
}
