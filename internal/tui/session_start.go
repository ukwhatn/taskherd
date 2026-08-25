package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/ukwhatn/taskherd/internal/herdrc"
	"github.com/ukwhatn/taskherd/internal/model"
)

// sessionStartWaitTimeout bounds agent wait once StartAgent itself has returned. StartAgent's own
// timeout covers herdr's trust-folder gate; this one covers the shorter settle time between a
// freshly started agent process and herdr's integration hook reporting its session id. Mirrors the
// CLI's own constant of the same name (internal/cli/start_cmd.go).
const sessionStartWaitTimeout = 30 * time.Second

// Session-start stages, mirroring the CLI's own stage names (internal/cli/start_cmd.go) so the
// same launch is described the same way whether it ran from the board or from `taskherd start`.
// Each names the last step that completed, not the one being attempted.
const (
	sessionStageStarted  = "started"
	sessionStageWaited   = "waited"
	sessionStageLinked   = "linked"
	sessionStagePrompted = "prompted"
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

// launchState tracks the one session-start operation in flight, if any.
//
// opID lets a stage message be matched against the operation it belongs to: the wait step alone
// can run for sessionStartWaitTimeout, during which the task can be deleted or another launch
// started, and a message that arrives after either must be dropped rather than acted on.
type launchState struct {
	pending bool
	opID    int
	taskID  int
	ctx     context.Context
	cancel  context.CancelFunc
}

// sessionStartMsg carries the state of a session-start operation as it moves through its stages.
// One type is reused for every stage rather than one type per transition: each stage only adds a
// field or two to what the previous one already gathered, and advanceSessionStart's dispatch on
// Stage is what decides which Cmd runs next.
type sessionStartMsg struct {
	opID   int
	taskID int
	title  string
	cwd    string
	prompt string

	stage      string
	paneID     string
	sessionID  string
	linked     bool
	promptSent bool
	// file is set only by the link step, right after a successful save: the board's own copy is
	// stale until this is applied, since the save happened on Store's own re-read under its lock
	// rather than through the model the board is holding.
	file *model.File

	err  error
	hint string
}

func newFieldTextarea() textarea.Model {
	ta := textarea.New()
	ta.ShowLineNumbers = false
	ta.SetHeight(promptVisibleLines)
	return ta
}

// beginSessionStart opens the launch modal for a task with no linked session yet.
func (b *Board) beginSessionStart(task *model.Task) tea.Cmd {
	if b.deps.Herdr == nil {
		return status("herdr に接続できないため起動できない", true)
	}
	if b.launch.pending {
		return status("起動処理中...", true)
	}

	candidates := model.RankSessionCwds(*b.file)
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
	b.sessionStart.prompt.SetValue(model.RenderPrompt(b.settings.SessionStart.TemplateFor(task.Status), *task))
	if len(candidates) == 0 {
		// No candidate row exists to land the initial cursor on, so it starts on the free-text
		// row instead — which means that row has the keyboard from the outset.
		b.sessionStart.cwdInput.Focus()
	}
	b.openOverlay(modeSessionStart)
	return nil
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

// submitSessionStart validates the modal's fields, then starts the launch and hands the board back
// to the caller: the modal closes immediately, and progress from here on is reported on the status
// line, four stages at a time (§7.4).
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

	taskID, title, prompt := s.taskID, s.title, s.prompt.Value()

	b.launch.opID++
	opID := b.launch.opID
	ctx, cancel := context.WithCancel(b.ctx)
	b.launch.pending = true
	b.launch.taskID = taskID
	b.launch.ctx = ctx
	b.launch.cancel = cancel

	b.closeOverlay()
	b.setStatus(fmt.Sprintf("#%d を起動中...", taskID), false)

	return b.createTabCmd(ctx, sessionStartMsg{opID: opID, taskID: taskID, cwd: cwd, title: title, prompt: prompt})
}

// advanceSessionStart decides what happens next for one stage message: drop it when it belongs to
// an operation no longer in flight, report and stop on any error, or move on to the next stage.
func (b *Board) advanceSessionStart(msg sessionStartMsg) tea.Cmd {
	if !b.launch.pending || msg.opID != b.launch.opID {
		return nil
	}
	if msg.err != nil {
		b.finishSessionStart(msg)
		return nil
	}

	ctx := b.launch.ctx
	switch msg.stage {
	case "":
		return b.startAgentCmd(ctx, msg)
	case sessionStageStarted:
		return b.waitAgentCmd(ctx, msg)
	case sessionStageWaited:
		return b.linkSessionCmd(ctx, msg)
	case sessionStageLinked:
		if msg.prompt == "" {
			b.finishSessionStart(msg)
			return nil
		}
		return b.sendPromptCmd(ctx, msg)
	default: // sessionStagePrompted
		b.finishSessionStart(msg)
		return nil
	}
}

func (b *Board) createTabCmd(ctx context.Context, msg sessionStartMsg) tea.Cmd {
	return func() tea.Msg {
		tab, err := b.deps.Herdr.CreateTab(ctx, herdrc.TabSpec{Cwd: msg.cwd, Label: msg.title})
		if err != nil {
			// Nothing was created: report it, but there is no pane to point at.
			msg.err = err
			return msg
		}
		msg.paneID = tab.PaneID
		return msg
	}
}

func (b *Board) startAgentCmd(ctx context.Context, msg sessionStartMsg) tea.Cmd {
	return func() tea.Msg {
		result, err := b.deps.Herdr.StartAgent(ctx, herdrc.AgentSpec{
			Name:   fmt.Sprintf("taskherd-%d", msg.taskID),
			Kind:   resumeAgent,
			PaneID: msg.paneID,
		})
		if err != nil {
			msg.err = err
			msg.hint = fmt.Sprintf("pane %s を確認する（起動に失敗した）", msg.paneID)
			return msg
		}
		msg.paneID = result.PaneID
		msg.stage = sessionStageStarted
		if result.NeedsAttention {
			msg.err = fmt.Errorf("起動直後に入力待ちになっている（%s）", result.Code)
			msg.hint = fmt.Sprintf("pane %s を開いて応答してから、詳細モーダルの ＋セッションを紐づける で後から紐づける", result.PaneID)
		}
		return msg
	}
}

func (b *Board) waitAgentCmd(ctx context.Context, msg sessionStartMsg) tea.Cmd {
	return func() tea.Msg {
		agent, err := b.deps.Herdr.WaitForAgentState(ctx, msg.paneID,
			[]string{herdrc.StateIdle, herdrc.StateBlocked}, sessionStartWaitTimeout)
		if err != nil {
			msg.err = err
			msg.hint = fmt.Sprintf("pane %s を確認し、詳細モーダルの ＋セッションを紐づける で後から紐づける", msg.paneID)
			return msg
		}
		sessionID := agent.SessionID()
		if sessionID == "" {
			msg.err = errors.New("herdr がセッション id を報告しなかった")
			msg.hint = fmt.Sprintf("pane %s を確認し、詳細モーダルの ＋セッションを紐づける で後から紐づける", msg.paneID)
			return msg
		}
		msg.sessionID = sessionID
		msg.stage = sessionStageWaited
		return msg
	}
}

// linkSessionCmd saves the session through the store's own Update, the same path session link and
// the detail modal's add-session use: the task is re-read from disk under the lock rather than
// from whatever the board is holding, so a change made elsewhere while the launch was in flight
// (StartAgent and WaitForAgentState together can run for well over a minute) is not overwritten.
func (b *Board) linkSessionCmd(ctx context.Context, msg sessionStartMsg) tea.Cmd {
	return func() tea.Msg {
		now := b.deps.now()
		err := b.deps.Tasks.Update(ctx, func(f *model.File) error {
			t, err := f.Task(msg.taskID)
			if err != nil {
				return err
			}
			_, err = t.AddSession(model.SessionRef{Agent: resumeAgent, SessionID: msg.sessionID, Cwd: msg.cwd}, now)
			return err
		})
		if err != nil {
			msg.err = err
			msg.hint = fmt.Sprintf("pane %s / session %s を詳細モーダルの ＋セッションを紐づける で手動で紐づける", msg.paneID, msg.sessionID)
			return msg
		}
		if file, loadErr := b.deps.Tasks.Load(); loadErr == nil {
			msg.file = file
		}
		// Best-effort, same as the existing jump / session link paths: a failed stamp is only a
		// missing convenience in herdr's own UI, never a reason to fail the launch.
		_ = b.deps.Herdr.ReportTaskToken(ctx, msg.paneID, msg.taskID)
		msg.linked = true
		msg.stage = sessionStageLinked
		return msg
	}
}

func (b *Board) sendPromptCmd(ctx context.Context, msg sessionStartMsg) tea.Cmd {
	return func() tea.Msg {
		if err := b.deps.Herdr.SendAgentPrompt(ctx, msg.paneID, msg.prompt); err != nil {
			msg.err = err
			msg.hint = "起動と紐づけは済んでいる。プロンプトの送信だけ失敗した"
			return msg
		}
		msg.promptSent = true
		msg.stage = sessionStagePrompted
		return msg
	}
}

// finishSessionStart ends the operation, successfully or not, and releases its context: this is
// one of the four points (success, any error, Esc, target task gone) that must all cancel it.
func (b *Board) finishSessionStart(msg sessionStartMsg) {
	b.launch.pending = false
	if b.launch.cancel != nil {
		b.launch.cancel()
	}
	b.launch.cancel = nil

	if msg.err == nil {
		b.setStatus(fmt.Sprintf("#%d を pane %s で起動した", msg.taskID, msg.paneID), false)
		return
	}
	text := fmt.Sprintf("#%d の起動でエラー: %v", msg.taskID, msg.err)
	if msg.hint != "" {
		text += "（" + msg.hint + "）"
	}
	b.setStatus(text, true)
}

// cancelSessionStart stops the in-flight operation from outside its own stage chain: Esc on the
// board, or the target task disappearing out from under it.
func (b *Board) cancelSessionStart(reason string) {
	if !b.launch.pending {
		return
	}
	if b.launch.cancel != nil {
		b.launch.cancel()
	}
	b.launch.pending = false
	b.launch.cancel = nil
	if reason != "" {
		b.setStatus(reason, true)
	}
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
