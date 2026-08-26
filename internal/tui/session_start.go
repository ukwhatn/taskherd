package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/ukwhatn/taskherd/internal/herdrc"
	"github.com/ukwhatn/taskherd/internal/model"
)

// promptVisibleLines is how many rows the prompt textarea shows inside the modal.
const promptVisibleLines = 6

// sessionStartFocus is which widget in the launch modal has the keyboard.
type sessionStartFocus int

const (
	sessionStartFocusCwd sessionStartFocus = iota
	sessionStartFocusPrompt
)

// sessionStartState is the launch modal opened by g on a task with no linked session.
type sessionStartState struct {
	taskID int
	title  string
	// candidates are the ranked cwd suggestions (model.RankSessionCwds); the row after the last
	// one is always "enter it by hand", selected when cwdCursor == len(candidates).
	candidates []string
	cwdCursor  int
	cwdInput   textinput.Model
	prompt     textarea.Model
	focus      sessionStartFocus
	err        string
}

func newFieldTextarea() textarea.Model {
	ta := textarea.New()
	ta.ShowLineNumbers = false
	ta.SetHeight(promptVisibleLines)
	return ta
}

// beginSessionStart opens the launch modal for a task with no linked session yet.
//
// A task whose first attempt never reached the link step (it failed before AddSession ran) has no
// SessionRef yet, so RankSessionCwds has no candidates to offer even when that attempt's pane is
// still alive and recoverable — the same gap start_cmd.go's resolveStartCwd closes on the CLI side
// (§4.4 update). Closing it here means one herdr snapshot first, off the update loop like every
// other herdr call in this file: opening the modal is deferred behind probeRecoverableCwdCmd rather
// than probed synchronously, since a herdr call blocking the update loop would freeze the whole
// board's key handling for however long it takes (including the CLI-fallback path, which can run to
// several seconds).
func (b *Board) beginSessionStart(task *model.Task) tea.Cmd {
	if b.deps.Launcher == nil {
		return status("セッションを起動する経路が無い", true)
	}
	if b.deps.Herdr == nil {
		return status("herdr に接続できないため起動できない", true)
	}
	if b.sessionStartProbe != 0 {
		return status("cwd の候補を確認中...", true)
	}

	candidates := model.RankSessionCwds(*b.file)
	if len(candidates) == 0 {
		b.sessionStartProbe = task.ID
		return b.probeRecoverableCwdCmd(*task)
	}
	b.openSessionStartModal(*task, candidates)
	return nil
}

// openSessionStartModal builds the launch modal's state and opens it. candidates is whatever the
// caller has already settled on: RankSessionCwds's own ranking, or — when there were none — a
// single recovered cwd from probeRecoverableCwdCmd standing in for it.
func (b *Board) openSessionStartModal(task model.Task, candidates []string) {
	b.sessionStart = sessionStartState{
		taskID:     task.ID,
		title:      task.Title,
		candidates: candidates,
		cwdInput:   newFieldInput(),
		prompt:     newFieldTextarea(),
		focus:      sessionStartFocusCwd,
	}
	// Mutated through the field rather than built as locals and assigned in: a bubbles Model's
	// Focus/Blur takes a pointer receiver, so calling it on a local variable before that variable
	// is copied into the struct would blur or focus the copy already inside it, not the one left
	// behind in the local.
	b.sessionStart.prompt.SetValue(model.RenderPrompt(b.settings.SessionStart.TemplateFor(task.Status), task))
	if len(candidates) == 0 {
		// No candidate row exists to land the initial cursor on, so it starts on the free-text
		// row instead — which means that row has the keyboard from the outset.
		b.sessionStart.cwdInput.Focus()
	}
	b.openOverlay(modeSessionStart)
}

// sessionStartProbedMsg carries probeRecoverableCwdCmd's outcome back to the update loop.
type sessionStartProbedMsg struct {
	task model.Task
	// recoveredCwd is the previous attempt's agent's cwd, or empty when there is none to recover (or
	// herdr could not be asked) — the launch itself decides the rest once it runs.
	recoveredCwd string
}

// probeRecoverableCwdCmd is beginSessionStart's no-candidates fallback: one snapshot read for this
// task's own previous-attempt agent, so the modal can offer a leftover pane's cwd as a candidate
// instead of asking the user to already know it.
func (b *Board) probeRecoverableCwdCmd(task model.Task) tea.Cmd {
	return func() tea.Msg {
		snapshot, err := b.deps.Herdr.Snapshot(b.ctx)
		if err != nil {
			return sessionStartProbedMsg{task: task}
		}
		agent, ok := snapshot.AgentByName(fmt.Sprintf("taskherd-%d", task.ID))
		if !ok || !agentIsUsable(agent) {
			return sessionStartProbedMsg{task: task}
		}
		return sessionStartProbedMsg{task: task, recoveredCwd: agent.Cwd}
	}
}

// agentIsUsable mirrors the CLI's own helper of the same name (internal/cli/start_cmd.go): agent has
// a session id and is not stuck waiting for input.
func agentIsUsable(agent *herdrc.Agent) bool {
	return agent.SessionID() != "" && agent.AgentStatus != herdrc.StateBlocked
}

// applySessionStartProbe is sessionStartProbedMsg's handler (Board.Update's only call site): a
// probe superseded by the task disappearing (applyTasks resets sessionStartProbe to 0 for exactly
// this) or by another one starting is dropped, its payload never reaching the modal.
func (b *Board) applySessionStartProbe(msg sessionStartProbedMsg) {
	if b.sessionStartProbe != msg.task.ID {
		return
	}
	b.sessionStartProbe = 0

	var candidates []string
	if msg.recoveredCwd != "" {
		candidates = []string{msg.recoveredCwd}
	}
	b.openSessionStartModal(msg.task, candidates)
}

func (b *Board) handleSessionStartKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s := &b.sessionStart

	if !isTextKey(msg) {
		if b.isNewlineKey(msg) && s.focus == sessionStartFocusPrompt {
			s.prompt.InsertRune('\n')
			return b, nil
		}
		switch msg.String() {
		case "esc":
			b.closeOverlay()
			return b, nil
		case "tab":
			b.toggleSessionStartFocus()
			return b, nil
		case "enter":
			return b, b.submitSessionStart()
		case "up":
			if s.focus == sessionStartFocusCwd {
				b.moveSessionStartCwd(-1)
				return b, nil
			}
		case "down":
			if s.focus == sessionStartFocusCwd {
				b.moveSessionStartCwd(1)
				return b, nil
			}
		case "ctrl+y":
			return b, b.copySessionStartPrompt()
		}
	}

	switch s.focus {
	case sessionStartFocusCwd:
		if s.cwdCursor == len(s.candidates) {
			updated, cmd := s.cwdInput.Update(msg)
			s.cwdInput = updated
			return b, cmd
		}
	case sessionStartFocusPrompt:
		updated, cmd := s.prompt.Update(msg)
		s.prompt = updated
		return b, cmd
	}
	return b, nil
}

// pasteIntoSessionStart routes a bracketed paste to whichever field has the keyboard. Without this
// tea.PasteMsg is never delivered to modeSessionStart at all (Board.handlePaste only forwards to
// the modes it knows about).
func (b *Board) pasteIntoSessionStart(msg tea.PasteMsg) (tea.Model, tea.Cmd) {
	s := &b.sessionStart
	switch s.focus {
	case sessionStartFocusPrompt:
		updated, cmd := s.prompt.Update(msg)
		s.prompt = updated
		return b, cmd
	case sessionStartFocusCwd:
		if s.cwdCursor == len(s.candidates) {
			updated, cmd := s.cwdInput.Update(msg)
			s.cwdInput = updated
			return b, cmd
		}
	}
	return b, nil
}

func (b *Board) toggleSessionStartFocus() {
	s := &b.sessionStart
	if s.focus == sessionStartFocusCwd {
		s.focus = sessionStartFocusPrompt
		s.cwdInput.Blur()
		s.prompt.Focus()
	} else {
		s.focus = sessionStartFocusCwd
		s.prompt.Blur()
		if s.cwdCursor == len(s.candidates) {
			s.cwdInput.Focus()
		}
	}
}

func (b *Board) moveSessionStartCwd(delta int) {
	s := &b.sessionStart
	total := len(s.candidates) + 1 // +1 for the "enter it by hand" row
	next := s.cwdCursor + delta
	if next < 0 || next >= total {
		return
	}
	s.cwdCursor = next
	if next == len(s.candidates) {
		s.cwdInput.Focus()
	} else {
		s.cwdInput.Blur()
	}
}

// copySessionStartPrompt copies the prompt to the clipboard via OSC 52. There is no way to detect
// whether the terminal actually accepted it, so the status line never claims success outright.
func (b *Board) copySessionStartPrompt() tea.Cmd {
	text := b.sessionStart.prompt.Value()
	return tea.Batch(
		tea.SetClipboard(text),
		status("クリップボードへコピーを試みた（対応端末のみ反映される）", false),
	)
}

// submitSessionStart validates the modal's fields and hands the launch to a process outside this
// one, then quits the board.
//
// The launch is not run here. Reaching a linked session with a prompt in it takes around half a
// minute — herdr's own readiness wait, then claude's integration hook reporting a session id — and
// the board is a herdr overlay that the user closes as soon as the new tab appears, which killed
// the launch partway through every time: the pane and the agent existed, but the link and the
// prompt never happened. Nothing that outlives the board may run inside it.
func (b *Board) submitSessionStart() tea.Cmd {
	s := b.sessionStart

	var cwd string
	switch {
	case s.cwdCursor == len(s.candidates):
		cwd = strings.TrimSpace(s.cwdInput.Value())
	case s.cwdCursor < len(s.candidates):
		cwd = s.candidates[s.cwdCursor]
	}
	if cwd == "" {
		b.sessionStart.err = "作業ディレクトリを入力するか候補から選ぶ"
		return nil
	}
	if b.deps.Launcher == nil {
		b.sessionStart.err = "セッションを起動する経路が無い"
		return nil
	}

	if err := b.deps.Launcher.StartSession(s.taskID, cwd, s.prompt.Value()); err != nil {
		// The board stays open: this failed before anything was created, so the status line is
		// still the only place the user would ever see it.
		b.closeOverlay()
		return status(fmt.Sprintf("#%d の起動を開始できない: %v", s.taskID, err), true)
	}
	return tea.Quit
}

func (b *Board) renderSessionStart() string {
	s := &b.sessionStart
	width := b.modalWidth(84)
	inner := modalInner(width)

	var lines []string
	lines = append(lines, b.styles.dim.Render(truncate("作業ディレクトリ:", inner)))
	for i, cwd := range s.candidates {
		lines = append(lines, b.sessionStartRow(cwd, inner, s.focus == sessionStartFocusCwd && i == s.cwdCursor))
	}
	isCustom := s.cwdCursor == len(s.candidates)
	marker := padCell("", cursorWidth(b.icons))
	if s.focus == sessionStartFocusCwd && isCustom {
		marker = b.icons.Cursor + " "
	}
	label := "入力する: "
	s.cwdInput.SetWidth(maxInt(inner-lipgloss.Width(marker)-lipgloss.Width(label), 1))
	lines = append(lines, truncate(marker+label+s.cwdInput.View(), inner))

	lines = append(lines, "")
	promptLabel := "プロンプト:"
	if s.focus == sessionStartFocusPrompt {
		promptLabel = b.icons.Cursor + " " + promptLabel
	} else {
		promptLabel = padCell("", cursorWidth(b.icons)) + promptLabel
	}
	lines = append(lines, b.styles.dim.Render(truncate(promptLabel, inner)))
	s.prompt.SetWidth(inner)
	lines = append(lines, strings.Split(s.prompt.View(), "\n")...)

	if s.err != "" {
		lines = append(lines, b.styles.alert.Render(truncate(s.err, inner)))
	}

	return b.renderModal(modal{
		title: fmt.Sprintf("#%d %s を起動する", s.taskID, s.title),
		body:  lines,
		help: fmt.Sprintf("tab 切替  %s cwd選択  %s 改行(プロンプト)  ctrl+y コピー  enter 起動  esc 取消",
			b.icons.verticalKeys(), b.newlineKey()),
		width:   width,
		focused: true,
	})
}

func (b *Board) sessionStartRow(cwd string, inner int, selected bool) string {
	marker := padCell("", cursorWidth(b.icons))
	if selected {
		marker = b.icons.Cursor + " "
	}
	row := truncate(marker+cwd, inner)
	if selected {
		row = b.styles.cardTitleSelected.Render(row)
	}
	return row
}
