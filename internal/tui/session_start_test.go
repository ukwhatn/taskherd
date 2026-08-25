package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/ukwhatn/taskherd/internal/herdrc"
	"github.com/ukwhatn/taskherd/internal/model"
)

// waitingHerdr answers WaitForAgentState with sessionID on the pane fakeHerdr.CreateTab always
// creates ("pane-new"), so a full session-start flow through this fake finds a session id waiting.
func waitingHerdr(sessionID string) *fakeHerdr {
	return &fakeHerdr{
		waitResult: herdrc.Agent{
			PaneID: "pane-new", AgentStatus: herdrc.StateIdle,
			Session: &herdrc.AgentSession{Agent: "claude", Kind: "id", Value: sessionID},
		},
	}
}

func TestBoardGOpensLaunchModalForTaskWithoutSession(t *testing.T) {
	store := newFakeStore(model.Task{ID: 1, Title: "t", Status: "todo"})
	h := newHarness(t, Deps{Tasks: store, Herdr: &fakeHerdr{}}, Settings{})

	h.key("g")

	if h.board.mode != modeSessionStart {
		t.Fatalf("mode = %v, want modeSessionStart", h.board.mode)
	}
	if h.board.sessionStart.taskID != 1 {
		t.Errorf("taskID = %d, want 1", h.board.sessionStart.taskID)
	}
}

func TestBoardSessionStartWithoutHerdrRefuses(t *testing.T) {
	store := newFakeStore(model.Task{ID: 1, Title: "t", Status: "todo"})
	h := newHarness(t, Deps{Tasks: store}, Settings{})

	h.key("g")

	if h.board.mode == modeSessionStart {
		t.Fatal("herdr が無いのにモーダルが開いた")
	}
	if !h.board.statusIsError {
		t.Error("status がエラー扱いでない")
	}
}

func TestBoardSessionStartCandidatesRankedByRankSessionCwds(t *testing.T) {
	store := newFakeStore(
		model.Task{ID: 1, Title: "existing", Status: "todo",
			Sessions: []model.SessionRef{{Agent: "claude", SessionID: "s-1", Cwd: "/repo/a", LinkedAt: "2026-08-20T10:00:00+09:00"}}},
		model.Task{ID: 2, Title: "new", Status: "todo"},
	)
	h := newHarness(t, Deps{Tasks: store, Herdr: &fakeHerdr{}}, Settings{})
	h.key("down") // select task #2 (below #1 in the same column)

	h.key("g")

	if got := h.board.sessionStart.candidates; len(got) != 1 || got[0] != "/repo/a" {
		t.Fatalf("candidates = %v, want [/repo/a]", got)
	}
	// With candidates present, the cursor starts on the first one rather than the free-text row.
	if h.board.sessionStart.cwdCursor != 0 {
		t.Errorf("cwdCursor = %d, want 0", h.board.sessionStart.cwdCursor)
	}
}

func TestBoardSessionStartNoCandidatesStartsOnFreeTextRow(t *testing.T) {
	store := newFakeStore(model.Task{ID: 1, Title: "t", Status: "todo"})
	h := newHarness(t, Deps{Tasks: store, Herdr: &fakeHerdr{}}, Settings{})

	h.key("g")

	s := h.board.sessionStart
	if len(s.candidates) != 0 {
		t.Fatalf("candidates = %v, want 0 件", s.candidates)
	}
	if s.cwdCursor != 0 {
		t.Errorf("cwdCursor = %d, want 0（候補が無いので手入力行そのもの）", s.cwdCursor)
	}
}

// The full sequence: pick a candidate cwd, edit the prompt, launch, and land linked + prompted.
func TestBoardSessionStartFullFlowLinksAndSendsPrompt(t *testing.T) {
	store := newFakeStore(
		model.Task{ID: 1, Title: "existing", Status: "todo",
			Sessions: []model.SessionRef{{Agent: "claude", SessionID: "s-1", Cwd: "/repo/a", LinkedAt: "2026-08-20T10:00:00+09:00"}}},
		model.Task{ID: 2, Title: "new task", Status: "todo"},
	)
	herdrOps := waitingHerdr("s-new")
	h := newHarness(t, Deps{Tasks: store, Herdr: herdrOps}, Settings{})
	h.key("down")

	h.key("g")
	if h.board.mode != modeSessionStart {
		t.Fatalf("mode = %v, want modeSessionStart", h.board.mode)
	}
	h.board.sessionStart.prompt.SetValue("進めてほしい")
	h.key("enter")

	if h.board.mode != modeBoard {
		t.Errorf("mode = %v, want modeBoard（送信直後にモーダルを閉じる）", h.board.mode)
	}
	if h.board.launch.pending {
		t.Error("launch.pending が残っている")
	}
	if len(herdrOps.tabs) != 1 || herdrOps.tabs[0].Cwd != "/repo/a" {
		t.Fatalf("tabs = %+v, want cwd=/repo/a（候補をそのまま使う）", herdrOps.tabs)
	}
	if len(herdrOps.started) != 1 {
		t.Fatalf("started = %+v, want 1 件", herdrOps.started)
	}
	if len(herdrOps.waited) != 1 || herdrOps.waited[0] != "pane-new" {
		t.Errorf("waited = %v, want [pane-new]", herdrOps.waited)
	}
	if len(herdrOps.prompts) != 1 || herdrOps.prompts[0].Text != "進めてほしい" {
		t.Errorf("prompts = %+v, want 進めてほしい", herdrOps.prompts)
	}

	task := store.snapshot().Tasks[1]
	if len(task.Sessions) != 1 || task.Sessions[0].SessionID != "s-new" || task.Sessions[0].Cwd != "/repo/a" {
		t.Errorf("sessions = %+v", task.Sessions)
	}
	if !strings.Contains(h.board.status, "起動した") {
		t.Errorf("status = %q, want 完了報告", h.board.status)
	}
}

func TestBoardSessionStartEmptyPromptSendsNothing(t *testing.T) {
	store := newFakeStore(model.Task{ID: 1, Title: "t", Status: "todo"})
	herdrOps := waitingHerdr("s-new")
	h := newHarness(t, Deps{Tasks: store, Herdr: herdrOps}, Settings{})

	h.key("g")
	h.board.sessionStart.cwdInput.SetValue("/repo/work")
	h.board.sessionStart.prompt.SetValue("")
	h.key("enter")

	if len(herdrOps.prompts) != 0 {
		t.Errorf("prompts = %+v, want 送信しない", herdrOps.prompts)
	}
	task := store.snapshot().Tasks[0]
	if len(task.Sessions) != 1 {
		t.Errorf("sessions = %+v, want 紐づけ自体は成功", task.Sessions)
	}
}

func TestBoardSessionStartBlankCwdIsRejectedBeforeLaunching(t *testing.T) {
	store := newFakeStore(model.Task{ID: 1, Title: "t", Status: "todo"})
	herdrOps := &fakeHerdr{}
	h := newHarness(t, Deps{Tasks: store, Herdr: herdrOps}, Settings{})

	h.key("g")
	h.board.sessionStart.cwdInput.SetValue("   ")
	h.key("enter")

	if h.board.mode != modeSessionStart {
		t.Fatalf("mode = %v, want modeSessionStart のまま", h.board.mode)
	}
	if len(herdrOps.tabs) != 0 {
		t.Error("空白だけの cwd なのに pane を作った")
	}
	if h.board.sessionStart.err == "" {
		t.Error("err が空")
	}
}

// A bracketed paste into the prompt textarea keeps its line breaks: this is the whole reason the
// prompt field is a textarea.Model rather than the single-line textinput.Model the other modals use.
func TestBoardSessionStartPasteIntoPromptKeepsLineBreaks(t *testing.T) {
	store := newFakeStore(model.Task{ID: 1, Title: "t", Status: "todo"})
	h := newHarness(t, Deps{Tasks: store, Herdr: &fakeHerdr{}}, Settings{})

	h.key("g")
	h.key("tab") // cwd -> prompt
	h.paste("1 行目\n2 行目")

	if got := h.board.sessionStart.prompt.Value(); !strings.Contains(got, "1 行目\n2 行目") {
		t.Errorf("prompt = %q, want 改行を保った貼り付け", got)
	}
}

// A paste into the free-text cwd row must reach it too, or a pasted path is silently dropped.
func TestBoardSessionStartPasteIntoCwdInput(t *testing.T) {
	store := newFakeStore(model.Task{ID: 1, Title: "t", Status: "todo"})
	h := newHarness(t, Deps{Tasks: store, Herdr: &fakeHerdr{}}, Settings{})

	h.key("g") // no candidates: cursor starts on the free-text row already
	h.paste("/pasted/path")

	if got := h.board.sessionStart.cwdInput.Value(); got != "/pasted/path" {
		t.Errorf("cwdInput = %q, want /pasted/path", got)
	}
}

// An IME commit arrives as one key event carrying the whole committed string; it must be typed
// into the free-text cwd row rather than matched against a binding by name.
func TestBoardSessionStartIMECommitIsTypedIntoCwdInput(t *testing.T) {
	store := newFakeStore(model.Task{ID: 1, Title: "t", Status: "todo"})
	h := newHarness(t, Deps{Tasks: store, Herdr: &fakeHerdr{}}, Settings{})

	h.key("g")
	h.dispatch(tea.KeyPressMsg{Code: tea.KeyExtended, Text: "enter"})

	if h.board.mode != modeSessionStart {
		t.Fatalf("mode = %v, want modeSessionStart（確定文字列で起動が走った）", h.board.mode)
	}
	if got := h.board.sessionStart.cwdInput.Value(); got != "enter" {
		t.Errorf("cwdInput = %q, want 確定文字列がそのまま入力される", got)
	}
}

// ctrl+j is the fallback newline key everywhere else in the board, and works the same way here:
// a literal line break in the prompt, not a submit.
func TestBoardSessionStartCtrlJInsertsNewlineInPrompt(t *testing.T) {
	store := newFakeStore(model.Task{ID: 1, Title: "t", Status: "todo"})
	herdrOps := &fakeHerdr{}
	h := newHarness(t, Deps{Tasks: store, Herdr: herdrOps}, Settings{})

	h.key("g")
	h.key("tab") // cwd -> prompt
	h.board.sessionStart.prompt.SetValue("1 行目")
	h.dispatch(tea.KeyPressMsg{Code: 'j', Mod: tea.ModCtrl})
	h.typeText("2行目")

	if got := h.board.sessionStart.prompt.Value(); !strings.Contains(got, "1 行目\n2行目") {
		t.Errorf("prompt = %q, want 改行で 2 行", got)
	}
	if len(herdrOps.tabs) != 0 {
		t.Error("改行で起動が走った")
	}
}

func TestBoardSessionStartAltEnterInsertsNewlineInPrompt(t *testing.T) {
	store := newFakeStore(model.Task{ID: 1, Title: "t", Status: "todo"})
	h := newHarness(t, Deps{Tasks: store, Herdr: &fakeHerdr{}}, Settings{})

	h.key("g")
	h.key("tab")
	h.board.sessionStart.prompt.SetValue("1")
	h.dispatch(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModAlt})
	h.typeText("2")

	if got := h.board.sessionStart.prompt.Value(); !strings.Contains(got, "1\n2") {
		t.Errorf("prompt = %q, want 改行で 2 行", got)
	}
}

func TestBoardSessionStartCtrlYCopiesPromptToClipboard(t *testing.T) {
	store := newFakeStore(model.Task{ID: 1, Title: "t", Status: "todo"})
	h := newHarness(t, Deps{Tasks: store, Herdr: &fakeHerdr{}}, Settings{})

	h.key("g")
	h.key("tab")
	h.board.sessionStart.prompt.SetValue("コピー対象")
	h.run(h.dispatch(tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl}))

	if h.board.mode != modeSessionStart {
		t.Fatalf("mode = %v, want モーダルを維持したまま", h.board.mode)
	}
	if !strings.Contains(h.board.status, "コピー") {
		t.Errorf("status = %q, want コピーを試みた旨", h.board.status)
	}
}

// g while a launch is already in flight must not start a second one. The chain is driven
// manually here (dispatch without run) so the operation stays pending mid-flight rather than
// completing within the same call.
func TestBoardSessionStartPendingBlocksSecondLaunch(t *testing.T) {
	store := newFakeStore(model.Task{ID: 1, Title: "t", Status: "todo"})
	herdrOps := &fakeHerdr{}
	h := newHarness(t, Deps{Tasks: store, Herdr: herdrOps}, Settings{})

	h.key("g")
	h.board.sessionStart.cwdInput.SetValue("/repo/work")
	h.dispatch(keyMsg("enter")) // submits but does not run the resulting Cmd chain

	if !h.board.launch.pending {
		t.Fatal("launch.pending がセットされていない")
	}
	before := len(herdrOps.tabs)

	h.key("g")

	if h.board.mode == modeSessionStart {
		t.Error("pending 中なのにモーダルが開いた")
	}
	if len(herdrOps.tabs) != before {
		t.Error("pending 中に 2 回目の tab create が走った")
	}
	if !h.board.statusIsError {
		t.Error("status がエラー扱いでない（起動処理中の案内）")
	}
}

// A message from an operation that has since been superseded by a newer one must be dropped: it
// carries the old operation's id, and advanceSessionStart is expected to ignore it.
func TestBoardSessionStartStaleOperationMessageIsDropped(t *testing.T) {
	store := newFakeStore(model.Task{ID: 1, Title: "t", Status: "todo"})
	herdrOps := waitingHerdr("s-genuine")
	h := newHarness(t, Deps{Tasks: store, Herdr: herdrOps}, Settings{})

	h.key("g")
	h.board.sessionStart.cwdInput.SetValue("/repo/work")
	h.dispatch(keyMsg("enter")) // op 1 starts, left pending (never run further)
	staleOpID := h.board.launch.opID
	h.key("esc") // cancel op 1 before it ever completed a stage

	// op 2 starts on the same still-unlinked task. Its Cmd is captured but deliberately not run
	// yet, so it is genuinely the operation in flight — pending, with its own opID — when the
	// stale message below is delivered, rather than there simply being nothing pending at all.
	h.key("g")
	h.board.sessionStart.cwdInput.SetValue("/repo/work")
	op2Cmd := h.dispatch(keyMsg("enter"))
	currentOpID := h.board.launch.opID
	if currentOpID == staleOpID {
		t.Fatal("opID が更新されていない（テストの前提が崩れている）")
	}

	// A stray, late message from the cancelled op 1 must not disturb op 2's in-flight state or
	// reach the task.
	h.dispatch(sessionStartMsg{
		opID: staleOpID, taskID: 1, cwd: "/repo/stale", prompt: "",
		stage: sessionStageWaited, paneID: "pane-old", sessionID: "s-old",
	})
	if !h.board.launch.pending || h.board.launch.opID != currentOpID {
		t.Fatalf("op 2 の pending 状態が乱された: pending=%v opID=%d, want true/%d",
			h.board.launch.pending, h.board.launch.opID, currentOpID)
	}
	if len(store.snapshot().Tasks[0].Sessions) != 0 {
		t.Error("古い operation の message でセッションが紐づいた")
	}

	// op 2 then runs to completion normally, undisturbed by the stale message it received midway.
	h.run(op2Cmd)

	task := store.snapshot().Tasks[0]
	if len(task.Sessions) != 1 || task.Sessions[0].SessionID != "s-genuine" {
		t.Errorf("sessions = %+v, want s-genuine の 1 件のみ", task.Sessions)
	}
}

// The task disappearing while its launch modal is still open (before Enter) closes the modal.
func TestBoardSessionStartClosesWhenTargetTaskDeletedBeforeSubmit(t *testing.T) {
	store := newFakeStore(model.Task{ID: 1, Title: "t", Status: "todo"})
	h := newHarness(t, Deps{Tasks: store, Herdr: &fakeHerdr{}}, Settings{})

	h.key("g")
	if h.board.mode != modeSessionStart {
		t.Fatalf("mode = %v, want modeSessionStart", h.board.mode)
	}

	h.dispatch(tasksLoadedMsg{file: model.NewFile()}) // task #1 gone

	if h.board.mode != modeBoard {
		t.Errorf("mode = %v, want modeBoard（対象タスクが消えたのでモーダルを閉じる）", h.board.mode)
	}
}

// The task disappearing after the launch already started (mid-flight) cancels the operation.
func TestBoardSessionStartCancelsWhenTargetTaskDeletedMidFlight(t *testing.T) {
	store := newFakeStore(model.Task{ID: 1, Title: "t", Status: "todo"})
	herdrOps := &fakeHerdr{}
	h := newHarness(t, Deps{Tasks: store, Herdr: herdrOps}, Settings{})

	h.key("g")
	h.board.sessionStart.cwdInput.SetValue("/repo/work")
	h.dispatch(keyMsg("enter")) // left pending, chain not run

	if !h.board.launch.pending {
		t.Fatal("launch.pending がセットされていない")
	}

	h.dispatch(tasksLoadedMsg{file: model.NewFile()}) // task #1 gone from under the operation

	if h.board.launch.pending {
		t.Error("launch.pending が残っている（対象タスク消滅で解除されるべき）")
	}
	if !h.board.statusIsError {
		t.Error("status がエラー扱いでない")
	}
}

// Esc while a launch is pending cancels it (the modal itself already closed on submit, so this is
// the board-level escape hatch).
func TestBoardSessionStartEscCancelsPendingLaunch(t *testing.T) {
	store := newFakeStore(model.Task{ID: 1, Title: "t", Status: "todo"})
	h := newHarness(t, Deps{Tasks: store, Herdr: &fakeHerdr{}}, Settings{})

	h.key("g")
	h.board.sessionStart.cwdInput.SetValue("/repo/work")
	h.dispatch(keyMsg("enter"))
	if !h.board.launch.pending {
		t.Fatal("launch.pending がセットされていない")
	}

	h.key("esc")

	if h.board.launch.pending {
		t.Error("esc で解除されていない")
	}
}

// A change made through another path (another taskherd process, or the file watcher) while the
// launch's own wait step is still running must survive the eventual save: AddSession is reached
// through Store.Update, which re-reads under its own lock rather than from whatever the board
// held when the operation started.
func TestBoardSessionStartConcurrentExternalUpdateSurvives(t *testing.T) {
	store := newFakeStore(model.Task{ID: 1, Title: "t", Status: "todo"})
	herdrOps := waitingHerdr("s-new")
	h := newHarness(t, Deps{Tasks: store, Herdr: herdrOps}, Settings{})

	h.key("g")
	h.board.sessionStart.cwdInput.SetValue("/repo/work")
	h.board.sessionStart.prompt.SetValue("")
	cmd := h.dispatch(keyMsg("enter"))

	// Drive the chain by hand up to (but not through) the link step.
	tabMsg := cmd().(sessionStartMsg)
	startedCmd := h.dispatch(tabMsg)
	startedMsg := startedCmd().(sessionStartMsg)
	waitedCmd := h.dispatch(startedMsg)
	waitedMsg := waitedCmd().(sessionStartMsg)
	if waitedMsg.stage != sessionStageWaited {
		t.Fatalf("stage = %q, want waited（link 直前まで進めたい）", waitedMsg.stage)
	}

	// An update from elsewhere lands while the launch is still in flight.
	if err := store.Update(context.Background(), func(f *model.File) error {
		task, err := f.Task(1)
		if err != nil {
			return err
		}
		task.SetNote("外部から更新", time.Now())
		return nil
	}); err != nil {
		t.Fatalf("外部更新に失敗: %v", err)
	}

	h.run(h.dispatch(waitedMsg)) // link, then finish (no prompt to send)

	task := store.snapshot().Tasks[0]
	if task.Note != "外部から更新" {
		t.Errorf("note = %q, want 外部更新が残る", task.Note)
	}
	if len(task.Sessions) != 1 || task.Sessions[0].SessionID != "s-new" {
		t.Errorf("sessions = %+v", task.Sessions)
	}
}
