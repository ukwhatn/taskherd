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

// Row budgets for the launch modal. The prompt opens at promptVisibleLines and the candidate list
// at maxCwdCandidateRows, and both give way — the prompt first, down to promptMinLines — when the
// terminal cannot hold them.
const (
	promptVisibleLines  = 6
	promptMinLines      = 3
	maxCwdCandidateRows = 5
)

// sessionStartFocus is which section of the launch modal has the keyboard.
type sessionStartFocus int

const (
	sessionStartFocusSpace sessionStartFocus = iota
	sessionStartFocusCwd
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
	cwdOffset  int
	cwdInput   textinput.Model
	prompt     textarea.Model
	space      spaceSelectState
	focus      sessionStartFocus
	err        string
}

// resumeStartState is the modal shown before resuming a session whose pane is gone. It replaces
// what used to be a plain yes/no confirmation: a resume creates a tab, so it has a space to choose
// the same way a fresh launch does, and a yes/no prompt has nowhere to put that choice.
type resumeStartState struct {
	taskID  int
	title   string
	session model.SessionRef
	space   spaceSelectState
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
		return status(b.text.Start.NoLauncher, true)
	}
	if b.deps.Herdr == nil {
		return status(b.text.Start.HerdrDown, true)
	}
	if b.sessionStartProbe != 0 {
		return status(b.text.Start.ProbingCwd, true)
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
		space:      newSpaceSelect(b.snapshot),
		focus:      sessionStartFocusCwd,
	}
	// Mutated through the field rather than built as locals and assigned in: a bubbles Model's
	// Focus/Blur takes a pointer receiver, so calling it on a local variable before that variable
	// is copied into the struct would blur or focus the copy already inside it, not the one left
	// behind in the local.
	b.sessionStart.prompt.SetValue(model.RenderPrompt(b.settings.SessionStart.TemplateFor(task.Status), task))
	// With no candidate row to land the initial cursor on, cwdCursor is already the free-text row,
	// so this is what gives that row the keyboard from the outset.
	b.focusSessionStart(sessionStartFocusCwd)
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
			b.cycleSessionStartSection(1)
			return b, nil
		case "shift+tab":
			b.cycleSessionStartSection(-1)
			return b, nil
		case "enter":
			return b, b.submitSessionStart()
		case "left":
			if s.focus == sessionStartFocusSpace {
				s.space.move(-1)
				return b, nil
			}
		case "right":
			if s.focus == sessionStartFocusSpace {
				s.space.move(1)
				return b, nil
			}
		case "up":
			if b.sessionStartMoveUp() {
				return b, nil
			}
		case "down":
			if b.sessionStartMoveDown() {
				return b, nil
			}
		case "ctrl+y":
			return b, b.copySessionStartPrompt()
		}
	}

	switch s.focus {
	case sessionStartFocusSpace:
		return b, s.space.update(msg)
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

// sessionStartMoveUp walks the modal's sections upward. It reports false when the key belongs to
// the widget that has the keyboard — a prompt with lines above its cursor — so the caller passes
// the event on rather than stealing it.
func (b *Board) sessionStartMoveUp() bool {
	s := &b.sessionStart
	switch s.focus {
	case sessionStartFocusCwd:
		if s.cwdCursor > 0 {
			b.moveSessionStartCwd(-1)
			return true
		}
		b.focusSessionStart(sessionStartFocusSpace)
	case sessionStartFocusPrompt:
		if s.prompt.Line() > 0 {
			return false
		}
		b.focusSessionStart(sessionStartFocusCwd)
	}
	return true
}

func (b *Board) sessionStartMoveDown() bool {
	s := &b.sessionStart
	switch s.focus {
	case sessionStartFocusSpace:
		b.focusSessionStart(sessionStartFocusCwd)
	case sessionStartFocusCwd:
		if s.cwdCursor < len(s.candidates) {
			b.moveSessionStartCwd(1)
			return true
		}
		b.focusSessionStart(sessionStartFocusPrompt)
	case sessionStartFocusPrompt:
		return false
	}
	return true
}

// cycleSessionStartSection moves the keyboard to the next section, wrapping. The space row is
// skipped when herdr reported no spaces, since it is not drawn either.
func (b *Board) cycleSessionStartSection(delta int) {
	sections := []sessionStartFocus{sessionStartFocusCwd, sessionStartFocusPrompt}
	if b.sessionStart.space.available() {
		sections = append([]sessionStartFocus{sessionStartFocusSpace}, sections...)
	}
	at := 0
	for i, section := range sections {
		if section == b.sessionStart.focus {
			at = i
			break
		}
	}
	next := ((at+delta)%len(sections) + len(sections)) % len(sections)
	b.focusSessionStart(sections[next])
}

// focusSessionStart hands the keyboard to one section and takes it from the others. Every section
// change goes through here so that no two text fields hold focus at once.
func (b *Board) focusSessionStart(target sessionStartFocus) {
	s := &b.sessionStart
	if target == sessionStartFocusSpace && !s.space.available() {
		return
	}
	s.focus = target

	s.space.focusRow(target == sessionStartFocusSpace)
	if target == sessionStartFocusCwd && s.cwdCursor == len(s.candidates) {
		s.cwdInput.Focus()
	} else {
		s.cwdInput.Blur()
	}
	if target == sessionStartFocusPrompt {
		s.prompt.Focus()
	} else {
		s.prompt.Blur()
	}
}

// pasteIntoSessionStart routes a bracketed paste to whichever field has the keyboard. Without this
// tea.PasteMsg is never delivered to modeSessionStart at all (Board.handlePaste only forwards to
// the modes it knows about).
func (b *Board) pasteIntoSessionStart(msg tea.PasteMsg) (tea.Model, tea.Cmd) {
	s := &b.sessionStart
	switch s.focus {
	case sessionStartFocusSpace:
		return b, s.space.update(msg)
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
		status(b.text.Start.Copied, false),
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
		b.sessionStart.err = b.text.Start.NeedCwd
		return nil
	}
	if b.deps.Launcher == nil {
		b.sessionStart.err = b.text.Start.NoLauncher
		return nil
	}

	if err := b.deps.Launcher.StartSession(s.taskID, cwd, s.prompt.Value(), s.space.choice()); err != nil {
		// The board stays open: this failed before anything was created, so the status line is
		// still the only place the user would ever see it.
		b.closeOverlay()
		return status(fmt.Sprintf(b.text.Start.StartFailed, s.taskID, err), true)
	}
	return tea.Quit
}

// beginResumeStart opens the resume modal for a linked session whose pane herdr no longer has.
func (b *Board) beginResumeStart(taskID int, title string, session model.SessionRef) {
	b.resumeStart = resumeStartState{
		taskID:  taskID,
		title:   title,
		session: session,
		space:   newSpaceSelect(b.snapshot),
	}
	b.resumeStart.space.focusRow(true)
	b.openOverlay(modeResumeStart)
}

func (b *Board) handleResumeStartKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s := &b.resumeStart

	if !isTextKey(msg) {
		switch msg.String() {
		case "esc":
			b.closeOverlay()
			b.setStatus(b.text.Common.Cancelled, false)
			return b, nil
		case "enter":
			target := *s
			b.closeOverlay()
			return b, b.resumeCmd(target)
		case "left":
			s.space.move(-1)
			return b, nil
		case "right":
			s.space.move(1)
			return b, nil
		}
	}
	return b, s.space.update(msg)
}

func (b *Board) pasteIntoResumeStart(msg tea.PasteMsg) (tea.Model, tea.Cmd) {
	return b, b.resumeStart.space.update(msg)
}

func (b *Board) renderResumeStart() string {
	s := &b.resumeStart
	prompt := fmt.Sprintf(b.text.Jump.ConfirmResume, s.session.Cwd)
	// Sized to the question, with a floor that leaves the space row something to draw in: the
	// question names the cwd the session will be resumed in, which is the one thing being confirmed.
	width := b.modalWidth(maxInt(lipgloss.Width(prompt)+boxChrome, 72))
	inner := modalInner(width)

	lines := []string{b.styles.alert.Render(truncate(prompt, inner))}
	help := b.text.Jump.ResumeHelp
	if s.space.available() {
		lines = append(lines, "", b.renderSpaceRow(&s.space, b.text.Start.LabelSpace, inner, true))
		help = fmt.Sprintf(b.text.Jump.ResumeHelpSpace, b.icons.horizontalKeys())
	}

	return b.renderModal(modal{
		title:   b.text.Common.ConfirmTitle,
		body:    lines,
		help:    help,
		width:   width,
		focused: true,
	})
}

func (b *Board) renderSessionStart() string {
	s := &b.sessionStart
	width := b.modalWidth(84)
	inner := modalInner(width)
	candidateRows, promptRows := b.sessionStartHeights(len(s.candidates), s.err != "", s.space.available())

	var lines []string
	if s.space.available() {
		lines = append(lines, b.renderSpaceRow(&s.space, b.text.Start.LabelSpace, inner, s.focus == sessionStartFocusSpace))
	}

	lines = append(lines, b.styles.dim.Render(truncate(b.text.Start.LabelCwd, inner)))
	start, visible, before, after := listWindow(s.cwdOffset, s.cwdCursor, len(s.candidates), candidateRows)
	s.cwdOffset = start
	if before {
		lines = append(lines, b.styles.dim.Render(truncateMark))
	}
	for i := start; i < start+visible; i++ {
		lines = append(lines, b.sessionStartRow(s.candidates[i], inner, s.focus == sessionStartFocusCwd && i == s.cwdCursor))
	}
	if after {
		lines = append(lines, b.styles.dim.Render(truncateMark))
	}

	isCustom := s.cwdCursor == len(s.candidates)
	marker := padCell("", cursorWidth(b.icons))
	if s.focus == sessionStartFocusCwd && isCustom {
		marker = b.icons.Cursor + " "
	}
	label := b.text.Start.LabelCustom
	s.cwdInput.SetWidth(fieldWidth(s.cwdInput, inner-lipgloss.Width(marker)-lipgloss.Width(label)))
	lines = append(lines, truncate(marker+label+s.cwdInput.View(), inner))

	lines = append(lines, "")
	promptLabel := b.text.Start.LabelPrompt
	if s.focus == sessionStartFocusPrompt {
		promptLabel = b.icons.Cursor + " " + promptLabel
	} else {
		promptLabel = padCell("", cursorWidth(b.icons)) + promptLabel
	}
	lines = append(lines, b.styles.dim.Render(truncate(promptLabel, inner)))
	s.prompt.SetWidth(inner)
	s.prompt.SetHeight(promptRows)
	lines = append(lines, strings.Split(s.prompt.View(), "\n")...)

	if s.err != "" {
		lines = append(lines, b.styles.alert.Render(truncate(s.err, inner)))
	}

	return b.renderModal(modal{
		title:   fmt.Sprintf(b.text.Start.Title, s.taskID, s.title),
		body:    lines,
		help:    b.sessionStartHelp(),
		width:   width,
		focused: true,
	})
}

// sessionStartHeights divides the modal's body between the cwd candidate list and the prompt.
//
// Both grow on their own: the candidate list with every distinct cwd a session was ever started
// in, the prompt with whatever the template renders. Left alone they overrun the body an 80x24
// terminal gives the modal, and renderModal's own cut would then take the prompt's last lines and
// the key help — the two things the user is looking at — rather than a candidate further down a
// list. So the rows nothing can give up are subtracted first, and what is left is handed out with
// the prompt yielding before the candidates do.
func (b *Board) sessionStartHeights(candidates int, hasErr, hasSpace bool) (candidateRows, promptRows int) {
	// The cwd label, the free-text row, the blank line, the prompt label and the help line.
	fixed := 5
	if hasErr {
		fixed++
	}
	if hasSpace {
		fixed++
	}
	room := b.modalBody(0) - fixed

	promptRows = promptVisibleLines
	candidateRows = minInt(candidates, maxCwdCandidateRows)
	for promptRows+candidateRows > room {
		switch {
		case promptRows > promptMinLines:
			promptRows--
		// The last candidate row is kept whatever happens: listWindow spends it on whichever row
		// the cursor is on, and a list that hides the selection is worse than one that overruns
		// into renderModal's own cut.
		case candidateRows > 1:
			candidateRows--
		default:
			return candidateRows, promptRows
		}
	}
	return candidateRows, promptRows
}

// sessionStartHelp names the keys that actually do something where the cursor is: tab means two
// different things depending on the row, and a footer that claims both would describe neither.
func (b *Board) sessionStartHelp() string {
	if b.sessionStart.focus == sessionStartFocusSpace {
		return fmt.Sprintf(b.text.Start.HelpSpace, b.icons.horizontalKeys())
	}
	return fmt.Sprintf(b.text.Start.Help, b.icons.verticalKeys(), b.newlineKey())
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
