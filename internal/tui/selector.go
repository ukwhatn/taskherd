package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
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
	switch msg.String() {
	case "esc", "tab":
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
	cells := make([]string, 0, len(b.statusSel.targets))
	for i, col := range b.statusSel.targets {
		cell := " " + col.Label + " "
		if i == b.statusSel.cursor {
			cell = b.styles.cardTitleSelected.Render(cell)
		}
		cells = append(cells, cell)
	}
	return b.styles.prompt.Render(fmt.Sprintf("#%d の移行先", b.statusSel.taskID)) + "\n" +
		truncate(strings.Join(cells, " "), b.width) + "\n" +
		b.styles.dim.Render("←→ 選択 / enter 確定 / esc 取消")
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
	switch msg.String() {
	case "esc":
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
	lines := []string{b.styles.prompt.Render(fmt.Sprintf("#%d に紐づけるセッション", b.sessionSel.taskID))}

	switch {
	case b.sessionSel.loading:
		lines = append(lines, b.styles.dim.Render("herdr に問い合わせ中..."))
	case len(b.sessionSel.agents) == 0:
		lines = append(lines, b.styles.dim.Render("エージェントがいない"))
	default:
		for i, agent := range b.sessionSel.agents {
			marker := "  "
			if i == b.sessionSel.cursor {
				marker = "▌ "
			}
			line := fmt.Sprintf("%s%-8s %-10s %s", marker, agent.Agent, shortID(agent.SessionID()), agent.Cwd)
			if agent.SessionID() == "" {
				line = b.styles.dim.Render(truncate(
					fmt.Sprintf("%s%-8s %-10s %s", marker, agent.Agent, "(未検出)", agent.Cwd), b.width))
			} else {
				line = truncate(line, b.width)
				if i == b.sessionSel.cursor {
					line = b.styles.cardTitleSelected.Render(line)
				}
			}
			lines = append(lines, line)
		}
	}

	if b.sessionSel.err != "" {
		lines = append(lines, b.styles.alert.Render(truncate(b.sessionSel.err, b.width)))
	}
	lines = append(lines, b.styles.dim.Render("↑↓ 選択 / enter 紐づけ / esc 取消"))
	return strings.Join(lines, "\n")
}
