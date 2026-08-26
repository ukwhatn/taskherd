package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/ukwhatn/taskherd/internal/config"
	"github.com/ukwhatn/taskherd/internal/herdrc"
	"github.com/ukwhatn/taskherd/internal/model"
)

// startDeps is the ordinary wiring for a launch: a herdr to read state from and a launcher to
// hand the launch off to. Both are required before g will open the modal at all.
func startDeps(store *fakeStore, herdrOps *fakeHerdr, launcher *fakeLauncher) Deps {
	return Deps{Tasks: store, Herdr: herdrOps, Launcher: launcher}
}

func TestBoardGOpensLaunchModalForTaskWithoutSession(t *testing.T) {
	store := newFakeStore(model.Task{ID: 1, Title: "t", Status: "todo"})
	h := newHarness(t, startDeps(store, &fakeHerdr{}, &fakeLauncher{}), Settings{})

	h.key("g")

	if h.board.mode != modeSessionStart {
		t.Fatalf("mode = %v, want modeSessionStart", h.board.mode)
	}
	if h.board.sessionStart.taskID != 1 {
		t.Errorf("taskID = %d, want 1", h.board.sessionStart.taskID)
	}
}

// The modal's initial prompt is settings.SessionStart.TemplateFor(task.Status), not the built-in
// default: a column with its own Templates entry must see that entry rendered from the outset.
// The task sits in "todo", the first of testColumns(), so it is the one already selected when g
// is pressed with nothing else to navigate to first.
func TestBoardSessionStartInitialPromptUsesColumnTemplate(t *testing.T) {
	store := newFakeStore(model.Task{ID: 1, Title: "設計する", Status: "todo"})
	settings := Settings{
		SessionStart: config.SessionStart{
			PromptTemplate: "デフォルト: #{{id}} {{title}}",
			Templates:      map[string]string{"todo": "todo専用: #{{id}} {{title}}"},
		},
	}
	h := newHarness(t, startDeps(store, &fakeHerdr{}, &fakeLauncher{}), settings)

	h.key("g")

	want := "todo専用: #1 設計する"
	if got := h.board.sessionStart.prompt.Value(); got != want {
		t.Errorf("prompt = %q, want %q", got, want)
	}
}

// An explicit empty-string override in Templates means "launch without a prompt" for that column
// (config.SessionStart.TemplateFor's own contract), and the modal's initial value must reflect
// that rather than falling back to PromptTemplate.
func TestBoardSessionStartInitialPromptEmptyColumnTemplateSuppressesPrompt(t *testing.T) {
	store := newFakeStore(model.Task{ID: 1, Title: "t", Status: "todo"})
	settings := Settings{
		SessionStart: config.SessionStart{
			PromptTemplate: "デフォルト: #{{id}} {{title}}",
			Templates:      map[string]string{"todo": ""},
		},
	}
	h := newHarness(t, startDeps(store, &fakeHerdr{}, &fakeLauncher{}), settings)

	h.key("g")

	if got := h.board.sessionStart.prompt.Value(); got != "" {
		t.Errorf("prompt = %q, want 空（明示的な空テンプレート）", got)
	}
}

func TestBoardSessionStartWithoutHerdrRefuses(t *testing.T) {
	store := newFakeStore(model.Task{ID: 1, Title: "t", Status: "todo"})
	h := newHarness(t, Deps{Tasks: store, Launcher: &fakeLauncher{}}, Settings{})

	h.key("g")

	if h.board.mode == modeSessionStart {
		t.Fatal("herdr が無いのにモーダルが開いた")
	}
	if !h.board.statusIsError {
		t.Error("status がエラー扱いでない")
	}
}

// Without a launcher there is nothing to hand the launch to, so the modal is refused rather than
// opened onto a submit that could only fail.
func TestBoardSessionStartWithoutLauncherRefuses(t *testing.T) {
	store := newFakeStore(model.Task{ID: 1, Title: "t", Status: "todo"})
	h := newHarness(t, Deps{Tasks: store, Herdr: &fakeHerdr{}}, Settings{})

	h.key("g")

	if h.board.mode == modeSessionStart {
		t.Fatal("launcher が無いのにモーダルが開いた")
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
	h := newHarness(t, startDeps(store, &fakeHerdr{}, &fakeLauncher{}), Settings{})
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
	h := newHarness(t, startDeps(store, &fakeHerdr{}, &fakeLauncher{}), Settings{})

	h.key("g")

	s := h.board.sessionStart
	if len(s.candidates) != 0 {
		t.Fatalf("candidates = %v, want 0 件", s.candidates)
	}
	if s.cwdCursor != 0 {
		t.Errorf("cwdCursor = %d, want 0（候補が無いので手入力行そのもの）", s.cwdCursor)
	}
}

// The whole submit path: pick the cwd, edit the prompt, press Enter — and the board hands both to
// the launcher and closes itself. Nothing about the launch runs here, which is the point: the
// board is a herdr overlay that goes away as soon as the new tab appears, and a launch running
// inside it would be cut off partway through (§1 of the PR-15 plan).
func TestBoardSessionStartHandsOffToLauncherAndQuits(t *testing.T) {
	store := newFakeStore(
		model.Task{ID: 1, Title: "existing", Status: "todo",
			Sessions: []model.SessionRef{{Agent: "claude", SessionID: "s-1", Cwd: "/repo/a", LinkedAt: "2026-08-20T10:00:00+09:00"}}},
		model.Task{ID: 2, Title: "new", Status: "todo"},
	)
	launcher := &fakeLauncher{}
	h := newHarness(t, startDeps(store, &fakeHerdr{}, launcher), Settings{})
	h.key("down") // select #2

	h.key("g")
	h.key("tab") // cwd -> prompt
	h.board.sessionStart.prompt.SetValue("これをやって")
	h.key("enter")

	if len(launcher.starts) != 1 {
		t.Fatalf("starts = %+v, want 1 件", launcher.starts)
	}
	got := launcher.starts[0]
	if got.taskID != 2 || got.cwd != "/repo/a" || got.prompt != "これをやって" {
		t.Errorf("start = %+v, want {2 /repo/a これをやって}", got)
	}
	if !h.quit {
		t.Error("board が終了していない（起動を渡したら閉じる）")
	}
	if store.updates != 0 {
		t.Errorf("updates = %d, want 0（紐づけは detach 先の仕事）", store.updates)
	}
}

// An empty prompt is passed through as an empty prompt rather than dropped: the CLI reads an
// explicit empty --prompt as "start without sending one", and omitting it would silently fall
// back to the config template the user just cleared.
func TestBoardSessionStartEmptyPromptIsHandedOffAsEmpty(t *testing.T) {
	store := newFakeStore(model.Task{ID: 1, Title: "t", Status: "todo"})
	launcher := &fakeLauncher{}
	h := newHarness(t, startDeps(store, &fakeHerdr{}, launcher), Settings{})

	h.key("g")
	h.board.sessionStart.cwdInput.SetValue("/repo/work")
	h.board.sessionStart.prompt.SetValue("")
	h.key("enter")

	if len(launcher.starts) != 1 {
		t.Fatalf("starts = %+v, want 1 件", launcher.starts)
	}
	if got := launcher.starts[0].prompt; got != "" {
		t.Errorf("prompt = %q, want 空", got)
	}
}

// A hand-off that fails has created nothing at all, so the board stays up: its status line is the
// only place that failure would ever be read.
func TestBoardSessionStartLauncherFailureKeepsBoardOpen(t *testing.T) {
	store := newFakeStore(model.Task{ID: 1, Title: "t", Status: "todo"})
	launcher := &fakeLauncher{err: errUnavailable}
	h := newHarness(t, startDeps(store, &fakeHerdr{}, launcher), Settings{})

	h.key("g")
	h.board.sessionStart.cwdInput.SetValue("/repo/work")
	h.key("enter")

	if h.quit {
		t.Fatal("起動を渡せなかったのに board が終了した")
	}
	if !h.board.statusIsError {
		t.Error("status がエラー扱いでない")
	}
	if !strings.Contains(h.board.status, "起動を開始できない") {
		t.Errorf("status = %q, want 起動を開始できない旨", h.board.status)
	}
}

func TestBoardSessionStartOffersRecoveredAgentCwdWhenNoCandidateExists(t *testing.T) {
	store := newFakeStore(model.Task{ID: 1, Title: "t", Status: "todo"})
	herdrOps := &fakeHerdr{snapshot: &herdrc.Snapshot{Agents: []herdrc.Agent{
		{PaneID: "wS:p9", Name: "taskherd-1", AgentStatus: herdrc.StateIdle, Cwd: "/repo/first-attempt",
			Session: &herdrc.AgentSession{Agent: "claude", Kind: "id", Value: "s-prev"}},
	}}}
	h := newHarness(t, startDeps(store, herdrOps, &fakeLauncher{}), Settings{})

	h.key("g")

	if h.board.mode != modeSessionStart {
		t.Fatalf("mode = %v, want modeSessionStart", h.board.mode)
	}
	if got := h.board.sessionStart.candidates; len(got) != 1 || got[0] != "/repo/first-attempt" {
		t.Fatalf("candidates = %v, want [/repo/first-attempt]", got)
	}
	if h.board.sessionStart.cwdCursor != 0 {
		t.Errorf("cwdCursor = %d, want 0（回収した候補が選ばれている）", h.board.sessionStart.cwdCursor)
	}
}

// A probe still in flight when the target task disappears must not open the modal once it lands:
// applyTasks (board.go) resets sessionStartProbe on exactly this, which is what makes the eventual
// result stale.
func TestBoardSessionStartProbeDroppedWhenTargetTaskDeletedBeforeItLands(t *testing.T) {
	store := newFakeStore(model.Task{ID: 1, Title: "t", Status: "todo"})
	herdrOps := &fakeHerdr{snapshot: &herdrc.Snapshot{Agents: []herdrc.Agent{
		{PaneID: "wS:p9", Name: "taskherd-1", AgentStatus: herdrc.StateIdle, Cwd: "/repo/a",
			Session: &herdrc.AgentSession{Agent: "claude", Kind: "id", Value: "s-prev"}},
	}}}
	h := newHarness(t, startDeps(store, herdrOps, &fakeLauncher{}), Settings{})

	cmd := h.dispatch(keyMsg("g")) // probe queued, not yet run
	if h.board.mode == modeSessionStart {
		t.Fatal("probe が完了する前にモーダルが開いている")
	}

	h.dispatch(tasksLoadedMsg{file: model.NewFile()}) // task #1 gone while the probe is still in flight

	h.run(cmd) // the probe lands now, its taskID no longer matching sessionStartProbe

	if h.board.mode == modeSessionStart {
		t.Error("消えたタスクのモーダルが開いている")
	}
}

func TestBoardSessionStartBlankCwdIsRejectedBeforeLaunching(t *testing.T) {
	store := newFakeStore(model.Task{ID: 1, Title: "t", Status: "todo"})
	launcher := &fakeLauncher{}
	h := newHarness(t, startDeps(store, &fakeHerdr{}, launcher), Settings{})

	h.key("g")
	h.board.sessionStart.cwdInput.SetValue("   ")
	h.key("enter")

	if h.board.mode != modeSessionStart {
		t.Fatalf("mode = %v, want modeSessionStart のまま", h.board.mode)
	}
	if len(launcher.starts) != 0 {
		t.Error("空白だけの cwd なのに起動を渡した")
	}
	if h.board.sessionStart.err == "" {
		t.Error("err が空")
	}
}

// A bracketed paste into the prompt textarea keeps its line breaks: this is the whole reason the
// prompt field is a textarea.Model rather than the single-line textinput.Model the other modals use.
func TestBoardSessionStartPasteIntoPromptKeepsLineBreaks(t *testing.T) {
	store := newFakeStore(model.Task{ID: 1, Title: "t", Status: "todo"})
	h := newHarness(t, startDeps(store, &fakeHerdr{}, &fakeLauncher{}), Settings{})

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
	h := newHarness(t, startDeps(store, &fakeHerdr{}, &fakeLauncher{}), Settings{})

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
	h := newHarness(t, startDeps(store, &fakeHerdr{}, &fakeLauncher{}), Settings{})

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
	launcher := &fakeLauncher{}
	h := newHarness(t, startDeps(store, &fakeHerdr{}, launcher), Settings{})

	h.key("g")
	h.key("tab") // cwd -> prompt
	h.board.sessionStart.prompt.SetValue("1 行目")
	h.dispatch(tea.KeyPressMsg{Code: 'j', Mod: tea.ModCtrl})
	h.typeText("2行目")

	if got := h.board.sessionStart.prompt.Value(); !strings.Contains(got, "1 行目\n2行目") {
		t.Errorf("prompt = %q, want 改行で 2 行", got)
	}
	if len(launcher.starts) != 0 {
		t.Error("改行で起動が走った")
	}
}

func TestBoardSessionStartAltEnterInsertsNewlineInPrompt(t *testing.T) {
	store := newFakeStore(model.Task{ID: 1, Title: "t", Status: "todo"})
	h := newHarness(t, startDeps(store, &fakeHerdr{}, &fakeLauncher{}), Settings{})

	h.key("g")
	h.key("tab")
	h.board.sessionStart.prompt.SetValue("1")
	h.dispatch(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModAlt})
	h.typeText("2")

	if got := h.board.sessionStart.prompt.Value(); !strings.Contains(got, "1\n2") {
		t.Errorf("prompt = %q, want 改行で 2 行", got)
	}
}

// Shift+Enter must behave like Alt+Enter/Ctrl+J in the prompt field — a newline, not a submit —
// once the terminal has answered the keyboard-enhancement query (Board.shiftEnter), the same rule
// isNewlineKey already applies to the add modal.
func TestBoardSessionStartShiftEnterInsertsNewlineInPrompt(t *testing.T) {
	store := newFakeStore(model.Task{ID: 1, Title: "t", Status: "todo"})
	launcher := &fakeLauncher{}
	h := newHarness(t, startDeps(store, &fakeHerdr{}, launcher), Settings{})
	h.dispatch(tea.KeyboardEnhancementsMsg{Flags: 1})

	h.key("g")
	h.key("tab") // cwd -> prompt
	h.board.sessionStart.prompt.SetValue("1")
	h.dispatch(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModShift})
	h.typeText("2")

	if got := h.board.sessionStart.prompt.Value(); !strings.Contains(got, "1\n2") {
		t.Errorf("prompt = %q, want 改行で 2 行", got)
	}
	if h.board.mode != modeSessionStart {
		t.Errorf("mode = %v, want モーダルを維持したまま（submit していない）", h.board.mode)
	}
	if len(launcher.starts) != 0 {
		t.Error("shift+enter で起動が走った")
	}
}

func TestBoardSessionStartCtrlYCopiesPromptToClipboard(t *testing.T) {
	store := newFakeStore(model.Task{ID: 1, Title: "t", Status: "todo"})
	h := newHarness(t, startDeps(store, &fakeHerdr{}, &fakeLauncher{}), Settings{})

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

// Esc is the launch modal's own cancel and stays that way: it holds text fields, where q is a
// character rather than a command (§3.4 of the PR-15 plan).
func TestBoardSessionStartEscClosesTheModal(t *testing.T) {
	store := newFakeStore(model.Task{ID: 1, Title: "t", Status: "todo"})
	launcher := &fakeLauncher{}
	h := newHarness(t, startDeps(store, &fakeHerdr{}, launcher), Settings{})

	h.key("g")
	h.key("esc")

	if h.board.mode != modeBoard {
		t.Errorf("mode = %v, want modeBoard", h.board.mode)
	}
	if len(launcher.starts) != 0 {
		t.Error("esc で起動が走った")
	}
}

// q belongs to the cwd field here, not to closing: typing a path that contains it must not be
// read as a command.
func TestBoardSessionStartQIsTypedIntoCwdInput(t *testing.T) {
	store := newFakeStore(model.Task{ID: 1, Title: "t", Status: "todo"})
	h := newHarness(t, startDeps(store, &fakeHerdr{}, &fakeLauncher{}), Settings{})

	h.key("g")
	h.key("q")

	if h.board.mode != modeSessionStart {
		t.Fatalf("mode = %v, want modeSessionStart のまま", h.board.mode)
	}
	if got := h.board.sessionStart.cwdInput.Value(); got != "q" {
		t.Errorf("cwdInput = %q, want q が文字として入る", got)
	}
}

// The task disappearing while its launch modal is still open (before Enter) closes the modal.
func TestBoardSessionStartClosesWhenTargetTaskDeletedBeforeSubmit(t *testing.T) {
	store := newFakeStore(model.Task{ID: 1, Title: "t", Status: "todo"})
	h := newHarness(t, startDeps(store, &fakeHerdr{}, &fakeLauncher{}), Settings{})

	h.key("g")
	if h.board.mode != modeSessionStart {
		t.Fatalf("mode = %v, want modeSessionStart", h.board.mode)
	}

	h.dispatch(tasksLoadedMsg{file: model.NewFile()}) // task #1 gone

	if h.board.mode != modeBoard {
		t.Errorf("mode = %v, want modeBoard（対象タスクが消えたのでモーダルを閉じる）", h.board.mode)
	}
}
