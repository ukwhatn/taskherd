package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/ukwhatn/taskherd/internal/model"
)

// beginJump starts the jump flow for the active task: straight to the session when there is only
// one, through a picker when there are several, and through the launch modal when there is none
// (g is one key for both "go to the session" and "start one" — §5 of the design).
func (b *Board) beginJump() tea.Cmd {
	task := b.activeTask()
	if task == nil {
		return status(b.text.Common.NoCardSelected, true)
	}
	if len(task.Sessions) == 0 {
		return b.beginSessionStart(task)
	}
	if b.deps.Herdr == nil {
		return status(b.text.Jump.HerdrDown, true)
	}
	if len(task.Sessions) == 1 {
		return b.jumpTo(task.ID, task.Title, task.Sessions[0])
	}

	b.jump = jumpState{taskID: task.ID, title: task.Title, sessions: task.Sessions}
	b.openOverlay(modeJump)
	return nil
}

func (b *Board) handleJumpKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if !isCommandKey(msg) {
		return b, nil
	}
	switch msg.String() {
	case "q":
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
			return status(fmt.Sprintf(b.text.Jump.ResumeManually, session.Cwd, session.SessionID), true)
		}
		return status(fmt.Sprintf(b.text.Jump.ResumeManuallyAgent, session.Cwd, session.Agent), true)
	}

	if paneID := b.sessions.Pane[session.SessionID]; paneID != "" {
		return b.focusCmd(taskID, title, paneID)
	}
	if session.Agent != resumeAgent {
		return status(fmt.Sprintf(b.text.Jump.PaneGoneUnsupported, session.Agent, session.Cwd), true)
	}

	b.beginResumeStart(taskID, title, session)
	return nil
}

// focusCmd moves herdr's focus to the pane and closes the board behind it. One focus call moves
// workspace, tab and pane together.
//
// The board is a herdr overlay drawn over the whole workspace, so leaving it open after a jump
// would leave it covering the very pane the jump moved to. A failed focus keeps it open instead:
// the status line is the only place that error can be read.
func (b *Board) focusCmd(taskID int, title, paneID string) tea.Cmd {
	return func() tea.Msg {
		if err := b.deps.Herdr.FocusAgent(b.ctx, paneID); err != nil {
			return statusMsg{text: fmt.Sprintf(b.text.Jump.FocusFailed, paneID, err), isError: true}
		}
		_ = b.deps.Herdr.ReportTaskDisplay(b.ctx, paneID, taskID, title)
		return tea.QuitMsg{}
	}
}

// resumeCmd reopens a session whose pane is gone, in a process of its own, and closes the board.
//
// The work itself is the CLI's jump (internal/cli/session_cmd.go), not a second copy of it here:
// a resume creates a tab and starts an agent, and herdr's readiness wait for a resumed transcript
// runs long enough that the board — closed by the user as soon as the new tab shows up — cannot be
// the process waiting on it.
func (b *Board) resumeCmd(state resumeStartState) tea.Cmd {
	taskID, sessionID, space := state.taskID, state.session.SessionID, state.space.choice()
	return func() tea.Msg {
		if b.deps.Launcher == nil {
			return statusMsg{text: b.text.Jump.NoLauncher, isError: true}
		}
		if err := b.deps.Launcher.ResumeSession(taskID, sessionID, space); err != nil {
			return statusMsg{text: fmt.Sprintf(b.text.Jump.ResumeFailed, taskID, err), isError: true}
		}
		return tea.QuitMsg{}
	}
}
