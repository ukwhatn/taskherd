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
	"github.com/ukwhatn/taskherd/internal/pathcomp"
)

// Row budgets for the launch modal. The prompt opens at promptVisibleLines and the cwd section's
// list at maxCwdListRows, and both give way — the prompt first, down to promptMinLines — when the
// terminal cannot hold them.
const (
	promptVisibleLines = 6
	promptMinLines     = 3
	maxCwdListRows     = 5
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
	// suggestions are the directories the free-text row's value matches, recomputed whenever it
	// changes and empty whenever the cursor is not on that row.
	suggestions pathcomp.Suggestions
	prompt      textarea.Model
	space       spaceSelectState
	focus       sessionStartFocus
	err         string
}

// typingCwd reports that the cursor is on the free-text row rather than on one of the ranked
// candidates. That row is the one with completion on it, and the one whose suggestions take the
// space the candidate list otherwise occupies.
func (s *sessionStartState) typingCwd() bool {
	return s.cwdCursor == len(s.candidates)
}

// refreshCwdSuggestions recomputes what the free-text row's value matches.
//
// The suggestions belong to that row alone: with the keyboard anywhere else they are cleared,
// which is also what hands the rows they were drawn in back to the candidate list.
func (b *Board) refreshCwdSuggestions() {
	s := &b.sessionStart
	if s.focus != sessionStartFocusCwd || !s.typingCwd() {
		s.suggestions = pathcomp.Suggestions{}
		return
	}
	s.suggestions = b.paths.Suggest(strings.TrimSpace(s.cwdInput.Value()), maxCwdListRows)
}

// completeCwd extends the free-text row as far as its matches agree on, which is what tab does
// while that row has the keyboard.
func (b *Board) completeCwd() {
	s := &b.sessionStart
	completed, suggestions := b.paths.Complete(strings.TrimSpace(s.cwdInput.Value()), maxCwdListRows)
	s.suggestions = suggestions
	if completed == s.cwdInput.Value() {
		return
	}
	s.cwdInput.SetValue(completed)
	// SetValue leaves the cursor where it was, so the next character typed would otherwise land in
	// the middle of what was just completed rather than after it.
	s.cwdInput.CursorEnd()
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
	// One visit to the modal is how stale a directory listing may get: a launch is the moment a
	// directory created since the board opened is most likely to be the one being typed.
	b.paths.Reset()
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
			// On the free-text row tab is completion, which is what a path field is expected to do
			// with it. shift+tab still walks the sections, so that row is never a dead end.
			if s.focus == sessionStartFocusCwd && s.typingCwd() {
				b.completeCwd()
				return b, nil
			}
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
		if s.typingCwd() {
			updated, cmd := s.cwdInput.Update(msg)
			s.cwdInput = updated
			b.refreshCwdSuggestions()
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
	if target == sessionStartFocusCwd && s.typingCwd() {
		s.cwdInput.Focus()
	} else {
		s.cwdInput.Blur()
	}
	if target == sessionStartFocusPrompt {
		s.prompt.Focus()
	} else {
		s.prompt.Blur()
	}
	b.refreshCwdSuggestions()
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
		if s.typingCwd() {
			updated, cmd := s.cwdInput.Update(msg)
			s.cwdInput = updated
			b.refreshCwdSuggestions()
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
	b.refreshCwdSuggestions()
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
	case s.typingCwd():
		cwd = strings.TrimSpace(s.cwdInput.Value())
	case s.cwdCursor < len(s.candidates):
		cwd = s.candidates[s.cwdCursor]
	}
	if cwd == "" {
		b.sessionStart.err = b.text.Start.NeedCwd
		return nil
	}
	// The launch runs in another process, which has no reason to share this one's idea of ~. It is
	// resolved here, where the home directory the completion offered paths against is the same one.
	cwd = b.paths.Expand(cwd)
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
	// The candidate list and the completion suggestions never appear together. The modal has room
	// for one list, and which one is worth drawing is exactly which row the cursor is on: a cursor
	// on a candidate is choosing from history, a cursor on the free-text row is typing a path.
	typing := s.focus == sessionStartFocusCwd && s.typingCwd()
	history := typing && len(s.candidates) > 0

	// Rows nothing can give up: the cwd label, the free-text row, the blank line, the prompt label
	// and the help line, plus the space, error and collapsed-history rows when they are drawn.
	fixed := 5
	if s.err != "" {
		fixed++
	}
	if s.space.available() {
		fixed++
	}
	if history {
		fixed++
	}
	listWant, listFloor := len(s.candidates), 1
	if typing {
		listWant, listFloor = suggestionRows(s.suggestions), 0
	}
	listRows, promptRows := b.sessionStartHeights(listWant, fixed, listFloor)

	var lines []string
	if s.space.available() {
		lines = append(lines, b.renderSpaceRow(&s.space, b.text.Start.LabelSpace, inner, s.focus == sessionStartFocusSpace))
	}

	lines = append(lines, b.styles.dim.Render(truncate(b.text.Start.LabelCwd, inner)))
	indent := padCell("", cursorWidth(b.icons))
	if history {
		// The candidates are still reachable while typing, so the row that replaces them says how
		// many there are and which key goes back to them.
		summary := fmt.Sprintf(b.text.Start.CwdHistory, len(s.candidates), b.icons.ArrowUp)
		lines = append(lines, b.styles.dim.Render(truncate(indent+summary, inner)))
	}
	if !typing {
		start, visible, before, after := listWindow(s.cwdOffset, s.cwdCursor, len(s.candidates), listRows)
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
	}

	marker := indent
	if typing {
		marker = b.icons.Cursor + " "
	}
	label := b.text.Start.LabelCustom
	s.cwdInput.SetWidth(fieldWidth(s.cwdInput, inner-lipgloss.Width(marker)-lipgloss.Width(label)))
	lines = append(lines, truncate(marker+label+s.cwdInput.View(), inner))
	if typing {
		lines = append(lines, b.suggestionLines(listRows, inner, padCell("", cursorWidth(b.icons)+lipgloss.Width(label)))...)
	}

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

// sessionStartHeights divides the modal's body between the one list it draws and the prompt.
//
// Both grow on their own: the list with every distinct cwd a session was ever started in (or with
// every directory the typed path matches), the prompt with whatever the template renders. Left
// alone they overrun the body an 80x24 terminal gives the modal, and renderModal's own cut would
// then take the prompt's last lines and the key help — the two things the user is looking at —
// rather than a row further down a list. So fixed, the rows nothing can give up, is subtracted
// first, and what is left is handed out with the prompt yielding down to promptMinLines before the
// list yields down to listFloor.
//
// listFloor is 1 for the candidate list, whose last row is what listWindow spends on the cursor —
// a list that hides the selection is worse than one that overruns. It is 0 for the completion
// suggestions, where nothing is selected and dropping the list only costs a hint.
func (b *Board) sessionStartHeights(listWant, fixed, listFloor int) (listRows, promptRows int) {
	room := b.modalBody(0) - fixed

	promptRows = promptVisibleLines
	listRows = minInt(listWant, maxCwdListRows)
	for promptRows+listRows > room {
		switch {
		case promptRows > promptMinLines:
			promptRows--
		case listRows > listFloor:
			listRows--
		default:
			return listRows, promptRows
		}
	}
	return listRows, promptRows
}

// suggestionRows is how many rows a completion list wants: one per suggestion, plus one for the
// count of the ones it has no room for.
func suggestionRows(s pathcomp.Suggestions) int {
	if s.Total > len(s.Items) {
		return len(s.Items) + 1
	}
	return len(s.Items)
}

// suggestionLines draws the completion list under the free-text row, within rows lines.
func (b *Board) suggestionLines(rows, inner int, indent string) []string {
	s := b.sessionStart.suggestions
	if rows <= 0 || len(s.Items) == 0 {
		return nil
	}
	shown := minInt(len(s.Items), rows)
	if s.Total > shown {
		// The last row goes to the count rather than to one more suggestion: a list that stops
		// without saying so reads as "that is all there is".
		shown--
	}

	var lines []string
	for _, item := range s.Items[:shown] {
		lines = append(lines, b.styles.dim.Render(truncate(indent+item, inner)))
	}
	if hidden := s.Total - shown; hidden > 0 {
		more := fmt.Sprintf(b.text.Start.MoreSuggestions, hidden)
		lines = append(lines, b.styles.dim.Render(truncate(indent+more, inner)))
	}
	return lines
}

// sessionStartHelp names the keys that actually do something where the cursor is: tab means three
// different things depending on the row, and a footer that claims all of them would describe none.
func (b *Board) sessionStartHelp() string {
	s := &b.sessionStart
	switch {
	case s.focus == sessionStartFocusSpace:
		return fmt.Sprintf(b.text.Start.HelpSpace, b.icons.horizontalKeys())
	case s.focus == sessionStartFocusCwd && s.typingCwd():
		return fmt.Sprintf(b.text.Start.HelpComplete, b.icons.verticalKeys(), b.newlineKey())
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
