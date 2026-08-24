package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/ukwhatn/taskherd/internal/herdrc"
	"github.com/ukwhatn/taskherd/internal/model"
)

// beginJump starts the jump flow for the active task: straight to the session when there is only
// one, through a picker when there are several.
func (b *Board) beginJump() tea.Cmd {
	task := b.activeTask()
	if task == nil {
		return status("カードが選択されていない", true)
	}
	if len(task.Sessions) == 0 {
		return status(fmt.Sprintf("#%d にセッションが紐づいていない（詳細モーダルの ＋セッションを紐づける）", task.ID), true)
	}
	if b.deps.Herdr == nil {
		return status("herdr に接続できないため jump できない", true)
	}
	if len(task.Sessions) == 1 {
		return b.jumpTo(task.ID, task.Title, task.Sessions[0])
	}

	b.jump = jumpState{taskID: task.ID, title: task.Title, sessions: task.Sessions}
	b.openOverlay(modeJump)
	return nil
}

func (b *Board) handleJumpKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		b.closeOverlay()
	case "down":
		if b.jump.cursor < len(b.jump.sessions)-1 {
			b.jump.cursor++
		}
	case "up":
		if b.jump.cursor > 0 {
			b.jump.cursor--
		}
	case "enter":
		target := b.jump
		b.closeOverlay()
		return b, b.jumpTo(target.taskID, target.title, target.sessions[target.cursor])
	}
	return b, nil
}

// jumpTo acts on one session: focus its pane when herdr still has it, otherwise ask before resuming.
//
// Liveness is read from the last snapshot the board received rather than fetched again, so the
// decision matches the badge the user just looked at. A pane that died in between surfaces as
// herdr's own agent_not_found from the focus call.
func (b *Board) jumpTo(taskID int, title string, session model.SessionRef) tea.Cmd {
	if !b.sessions.Available {
		if session.Agent == resumeAgent {
			return status(fmt.Sprintf("herdr に接続できない。cd %s && claude --resume %s を実行する", session.Cwd, session.SessionID), true)
		}
		return status(fmt.Sprintf("herdr に接続できない。%s で %s セッションを手動再開する", session.Cwd, session.Agent), true)
	}

	if paneID := b.sessions.Pane[session.SessionID]; paneID != "" {
		return b.focusCmd(taskID, paneID)
	}
	if session.Agent != resumeAgent {
		return status(fmt.Sprintf("%s セッションの pane が消滅している。この agent の resume には未対応。%s で手動再開する", session.Agent, session.Cwd), true)
	}

	b.openConfirm(confirmState{
		kind:    confirmResume,
		prompt:  fmt.Sprintf("pane が消滅している。%s で claude --resume を起動する", session.Cwd),
		taskID:  taskID,
		title:   title,
		session: session,
	})
	return nil
}

// focusCmd moves herdr's focus to the pane. One focus call moves workspace, tab and pane together.
func (b *Board) focusCmd(taskID int, paneID string) tea.Cmd {
	return func() tea.Msg {
		if err := b.deps.Herdr.FocusAgent(b.ctx, paneID); err != nil {
			return statusMsg{text: fmt.Sprintf("pane %s へ移動できない: %v", paneID, err), isError: true}
		}
		_ = b.deps.Herdr.ReportTaskToken(b.ctx, paneID, taskID)
		return statusMsg{text: fmt.Sprintf("#%d のセッションへ移動した（pane %s）", taskID, paneID)}
	}
}

// resumeCmd opens a new tab in the session's original cwd and resumes the agent there. The cwd
// matters: Claude Code stores its sessions per working directory.
func (b *Board) resumeCmd(state confirmState) tea.Cmd {
	taskID, session := state.taskID, state.session
	return func() tea.Msg {
		tab, err := b.deps.Herdr.CreateTab(b.ctx, herdrc.TabSpec{Cwd: session.Cwd, Label: state.title})
		if err != nil {
			return statusMsg{text: fmt.Sprintf("タブを作成できない: %v", err), isError: true}
		}
		started, err := b.deps.Herdr.StartAgent(b.ctx, herdrc.AgentSpec{
			Name:   fmt.Sprintf("taskherd-%d", taskID),
			Kind:   resumeAgent,
			PaneID: tab.PaneID,
			Args:   []string{"--resume", session.SessionID},
		})
		if err != nil {
			return statusMsg{text: fmt.Sprintf("resume 起動に失敗した: %v", err), isError: true}
		}
		_ = b.deps.Herdr.ReportTaskToken(b.ctx, started.PaneID, taskID)

		if started.NeedsAttention {
			return statusMsg{
				text:    fmt.Sprintf("pane %s で resume 起動した。入力待ち。pane を開いて応答する", started.PaneID),
				isError: true,
			}
		}
		return statusMsg{text: fmt.Sprintf("#%d を pane %s で resume 起動した", taskID, started.PaneID)}
	}
}
