package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/ukwhatn/taskherd/internal/config"
	"github.com/ukwhatn/taskherd/internal/herdrc"
	"github.com/ukwhatn/taskherd/internal/model"
)

// waitingHerdr answers WaitForAgentSession with sessionID on the pane fakeHerdr.CreateTab always
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
	h := newHarness(t, Deps{Tasks: store, Herdr: &fakeHerdr{}}, settings)

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
	h := newHarness(t, Deps{Tasks: store, Herdr: &fakeHerdr{}}, settings)

	h.key("g")

	if got := h.board.sessionStart.prompt.Value(); got != "" {
		t.Errorf("prompt = %q, want 空（明示的な空テンプレート）", got)
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

// A previous attempt's agent, idle with a session already but not yet linked to this task, at the
// same cwd this launch asked for, must be recovered rather than piling a second pane on top of it
// (§4 of the design).
func TestBoardSessionStartReusesIdleUnlinkedAgentInsteadOfCreatingANewPane(t *testing.T) {
	store := newFakeStore(model.Task{ID: 1, Title: "t", Status: "todo"})
	herdrOps := &fakeHerdr{snapshot: &herdrc.Snapshot{Agents: []herdrc.Agent{
		{PaneID: "wS:p9", Name: "taskherd-1", AgentStatus: herdrc.StateIdle, Cwd: "/repo/reused",
			Session: &herdrc.AgentSession{Agent: "claude", Kind: "id", Value: "s-prev"}},
	}}}
	h := newHarness(t, Deps{Tasks: store, Herdr: herdrOps}, Settings{})

	h.key("g")
	h.board.sessionStart.cwdInput.SetValue("/repo/reused")
	h.board.sessionStart.prompt.SetValue("続きから")
	h.key("enter")

	if h.board.launch.pending {
		t.Error("launch.pending が残っている")
	}
	if len(herdrOps.tabs) != 0 {
		t.Error("回収したのに tab create を呼んでいる")
	}
	if len(herdrOps.started) != 0 {
		t.Error("回収したのに agent start を呼んでいる")
	}
	if len(herdrOps.prompts) != 1 || herdrOps.prompts[0].Text != "続きから" || herdrOps.prompts[0].PaneID != "wS:p9" {
		t.Errorf("prompts = %+v", herdrOps.prompts)
	}
	task := store.snapshot().Tasks[0]
	if len(task.Sessions) != 1 || task.Sessions[0].SessionID != "s-prev" || task.Sessions[0].Cwd != "/repo/reused" {
		t.Errorf("sessions = %+v", task.Sessions)
	}
	if !strings.Contains(h.board.status, "紐づけた") {
		t.Errorf("status = %q, want 回収した旨", h.board.status)
	}
}

// A task whose first attempt never reached the link step has no SessionRef yet, so RankSessionCwds
// offers nothing — but if that attempt's pane is still alive and recoverable, the modal should offer
// its cwd as a candidate rather than making the user already know it (mirrors the CLI's own
// resolveStartCwd fallback, §4.4 update). One h.key("g") call must be enough: the probe this runs
// through in between (beginSessionStart, session_start.go) is itself entirely off the update loop.
func TestBoardSessionStartOffersRecoveredAgentCwdWhenNoCandidateExists(t *testing.T) {
	store := newFakeStore(model.Task{ID: 1, Title: "t", Status: "todo"})
	herdrOps := &fakeHerdr{snapshot: &herdrc.Snapshot{Agents: []herdrc.Agent{
		{PaneID: "wS:p9", Name: "taskherd-1", AgentStatus: herdrc.StateIdle, Cwd: "/repo/first-attempt",
			Session: &herdrc.AgentSession{Agent: "claude", Kind: "id", Value: "s-prev"}},
	}}}
	h := newHarness(t, Deps{Tasks: store, Herdr: herdrOps}, Settings{})

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
	h := newHarness(t, Deps{Tasks: store, Herdr: herdrOps}, Settings{})

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

// g itself only opens the launch modal for a task with zero linked sessions (beginJump), so the
// only way this branch is reachable is a session landing on the same task through another path
// (picker, CLI) between opening the modal and submitting it.
func TestBoardSessionStartRefusesWhenExistingAgentAlreadyLinkedToTask(t *testing.T) {
	store := newFakeStore(model.Task{ID: 1, Title: "t", Status: "todo"})
	herdrOps := &fakeHerdr{snapshot: &herdrc.Snapshot{Agents: []herdrc.Agent{
		{PaneID: "wS:p9", Name: "taskherd-1", AgentStatus: herdrc.StateIdle, Cwd: "/repo/a",
			Session: &herdrc.AgentSession{Agent: "claude", Kind: "id", Value: "s-linked"}},
	}}}
	h := newHarness(t, Deps{Tasks: store, Herdr: herdrOps}, Settings{})

	h.key("g")
	h.board.sessionStart.cwdInput.SetValue("/repo/a")

	if err := store.Update(context.Background(), func(f *model.File) error {
		task, err := f.Task(1)
		if err != nil {
			return err
		}
		_, err = task.AddSession(model.SessionRef{Agent: "claude", SessionID: "s-linked", Cwd: "/repo/a"}, boardNow)
		return err
	}); err != nil {
		t.Fatalf("外部からの事前紐づけに失敗: %v", err)
	}

	h.key("enter")

	if h.board.launch.pending {
		t.Error("launch.pending が残っている")
	}
	if len(herdrOps.tabs) != 0 {
		t.Error("既に紐づいているのに tab create を呼んでいる")
	}
	if !h.board.statusIsError {
		t.Error("status がエラー扱いでない")
	}
	if !strings.Contains(h.board.status, "紐づいている") {
		t.Errorf("status = %q, want 既に紐づいている旨", h.board.status)
	}
}

// blocked and "idle だがまだ session が無い" are the two states a recovered agent can be stuck in
// without being usable yet (§4 of the design); starting a second pane would only land on the same
// stuck agent, so both must refuse and point at the existing pane instead.
func TestBoardSessionStartRefusesWhenExistingAgentNotUsableYet(t *testing.T) {
	tests := []struct {
		name   string
		status string
	}{
		{"blocked", herdrc.StateBlocked},
		{"idle だが session 未到着", herdrc.StateIdle},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newFakeStore(model.Task{ID: 1, Title: "t", Status: "todo"})
			herdrOps := &fakeHerdr{snapshot: &herdrc.Snapshot{Agents: []herdrc.Agent{
				{PaneID: "wS:p9", Name: "taskherd-1", AgentStatus: tt.status, Cwd: "/repo/a"},
			}}}
			h := newHarness(t, Deps{Tasks: store, Herdr: herdrOps}, Settings{})

			h.key("g")
			h.board.sessionStart.cwdInput.SetValue("/repo/a")
			h.key("enter")

			if h.board.launch.pending {
				t.Error("launch.pending が残っている")
			}
			if len(herdrOps.tabs) != 0 {
				t.Error("使えない agent が居るのに tab create した")
			}
			if !h.board.statusIsError {
				t.Error("status がエラー扱いでない")
			}
			if !strings.Contains(h.board.status, "wS:p9") {
				t.Errorf("status = %q, want 既存 pane の案内", h.board.status)
			}
		})
	}
}

// Retrying with a different cwd (the user realized the first attempt used the wrong directory)
// must not silently link the old pane's cwd instead, and must not start a second agent under the
// same name either — a second pane is only ever created explicitly (CLI's --new).
func TestBoardSessionStartRefusesWhenExistingAgentIsInADifferentCwd(t *testing.T) {
	store := newFakeStore(model.Task{ID: 1, Title: "t", Status: "todo"})
	herdrOps := &fakeHerdr{snapshot: &herdrc.Snapshot{Agents: []herdrc.Agent{
		{PaneID: "wS:p9", Name: "taskherd-1", AgentStatus: herdrc.StateIdle, Cwd: "/repo/a",
			Session: &herdrc.AgentSession{Agent: "claude", Kind: "id", Value: "s-prev"}},
	}}}
	h := newHarness(t, Deps{Tasks: store, Herdr: herdrOps}, Settings{})

	h.key("g")
	// The task has no SessionRef yet, so the modal's one candidate row is the recovered agent's own
	// cwd (/repo/a) — moving off it onto the free-text row is what lets this test type a
	// deliberately different one.
	h.key("down")
	h.board.sessionStart.cwdInput.SetValue("/repo/b")
	h.key("enter")

	if h.board.launch.pending {
		t.Error("launch.pending が残っている")
	}
	if len(herdrOps.tabs) != 0 {
		t.Error("cwd が違うのに新規起動した")
	}
	if !h.board.statusIsError {
		t.Error("status がエラー扱いでない")
	}
	if !strings.Contains(h.board.status, "wS:p9") {
		t.Errorf("status = %q, want 既存 pane の案内", h.board.status)
	}
}

// The recovery check itself is only a snapshot read: when herdr cannot answer it, the launch must
// still attempt a fresh start rather than failing outright — CreateTab/StartAgent right after are
// what actually report herdr being unreachable, if it really is (mirrors the CLI's own
// findReusableAgent fallback).
func TestBoardSessionStartProceedsFreshWhenRecoveryCheckCannotReachHerdr(t *testing.T) {
	store := newFakeStore(model.Task{ID: 1, Title: "t", Status: "todo"})
	herdrOps := waitingHerdr("s-new")
	herdrOps.snapshotErr = errUnavailable
	h := newHarness(t, Deps{Tasks: store, Herdr: herdrOps}, Settings{})

	h.key("g")
	h.board.sessionStart.cwdInput.SetValue("/repo/work")
	h.key("enter")

	if len(herdrOps.tabs) != 1 {
		t.Fatalf("tabs = %+v, want 1 件（回収チェック失敗時は新規起動へフォールバック）", herdrOps.tabs)
	}
	task := store.snapshot().Tasks[0]
	if len(task.Sessions) != 1 || task.Sessions[0].SessionID != "s-new" {
		t.Errorf("sessions = %+v", task.Sessions)
	}
}

// blocked is what an untrusted cwd's agent settles into (herdr's own trust-folder gate), and it
// never carries a session id — the single most common way a wait ends without one. The status
// message must name that instead of the generic "herdr がセッション id を報告しなかった".
func TestBoardSessionStartBlockedAgentReportsWaitingForInput(t *testing.T) {
	store := newFakeStore(model.Task{ID: 1, Title: "t", Status: "todo"})
	herdrOps := &fakeHerdr{
		waitResult: herdrc.Agent{PaneID: "pane-new", AgentStatus: herdrc.StateBlocked},
	}
	h := newHarness(t, Deps{Tasks: store, Herdr: herdrOps}, Settings{})

	h.key("g")
	h.board.sessionStart.cwdInput.SetValue("/repo/work")
	h.key("enter")

	if h.board.launch.pending {
		t.Error("launch.pending が残っている")
	}
	if !h.board.statusIsError {
		t.Error("status がエラー扱いでない")
	}
	if !strings.Contains(h.board.status, "入力待ち") {
		t.Errorf("status = %q, want blocked を示す文言", h.board.status)
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

// Shift+Enter must behave like Alt+Enter/Ctrl+J in the prompt field — a newline, not a submit —
// once the terminal has answered the keyboard-enhancement query (Board.shiftEnter), the same rule
// isNewlineKey already applies to the add modal.
func TestBoardSessionStartShiftEnterInsertsNewlineInPrompt(t *testing.T) {
	store := newFakeStore(model.Task{ID: 1, Title: "t", Status: "todo"})
	herdrOps := &fakeHerdr{}
	h := newHarness(t, Deps{Tasks: store, Herdr: herdrOps}, Settings{})
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
	if len(herdrOps.tabs) != 0 {
		t.Error("shift+enter で起動が走った")
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

// A stale message can carry a non-nil file (the link step's own save, arriving after the
// operation was cancelled): that file must never be applied to the board, not even briefly.
// Checking staleness only inside advanceSessionStart, after the file has already been swapped
// into b.file by the caller, would miss this — the message is still dropped, but the swap already
// happened.
func TestBoardSessionStartStaleFileNeverReachesBoard(t *testing.T) {
	store := newFakeStore(model.Task{ID: 1, Title: "t", Status: "todo"})
	h := newHarness(t, Deps{Tasks: store, Herdr: &fakeHerdr{}}, Settings{})
	realFile := h.board.file

	h.key("g")
	h.board.sessionStart.cwdInput.SetValue("/repo/work")
	h.dispatch(keyMsg("enter")) // op starts, left pending (never run further)
	staleOpID := h.board.launch.opID
	h.key("esc") // cancel it before it ever completed a stage

	poisoned := model.NewFile()
	poisoned.Tasks = append(poisoned.Tasks, model.Task{ID: 999, Title: "poisoned", Status: "todo"})

	h.dispatch(sessionStartMsg{
		opID: staleOpID, taskID: 1, cwd: "/repo/stale",
		stage: sessionStageWaited, paneID: "pane-old", sessionID: "s-old",
		file: poisoned,
	})

	if h.board.file != realFile {
		t.Error("キャンセル済み operation の file が board に適用された")
	}
	for _, task := range h.board.file.Tasks {
		if task.ID == 999 {
			t.Fatal("poisoned タスクが board に載っている")
		}
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

// Reproduces the scenario the central Esc check exists for: opening the launch modal from inside
// detail (g on a task with no linked session) leaves the board back in modeDetail once the launch
// starts, since submitSessionStart closes its own modal immediately. An Esc caught only inside
// handleBoardKey never sees this: it only closes detail, leaving the operation running underneath.
func TestBoardSessionStartEscFromDetailStillCancelsPendingLaunch(t *testing.T) {
	store := newFakeStore(model.Task{ID: 1, Title: "t", Status: "todo"})
	h := newHarness(t, Deps{Tasks: store, Herdr: &fakeHerdr{}}, Settings{})

	h.key("enter") // open detail
	if h.board.mode != modeDetail {
		t.Fatalf("mode = %v, want modeDetail", h.board.mode)
	}
	h.key("g") // no linked session, so g opens the launch modal (via beginJump)
	if h.board.mode != modeSessionStart {
		t.Fatalf("mode = %v, want modeSessionStart", h.board.mode)
	}
	h.board.sessionStart.cwdInput.SetValue("/repo/work")
	h.dispatch(keyMsg("enter")) // submit: modal closes back to modeDetail, operation left pending

	if h.board.mode != modeDetail {
		t.Fatalf("mode = %v, want modeDetail（起動はモーダルを閉じて呼び出し元に戻る）", h.board.mode)
	}
	if !h.board.launch.pending {
		t.Fatal("launch.pending がセットされていない")
	}

	h.key("esc")

	if h.board.launch.pending {
		t.Error("detail 経由の esc で解除されていない")
	}
}

// tui.Run derives one cancellable context and defers its cancel, so that everything the board has
// in flight when the program exits — b.launch.ctx included, built as context.WithCancel(b.ctx) in
// submitSessionStart — is cut regardless of why the program returned (q, ctrl+c, a signal). This
// test stands in for that: it cancels what plays the role of tui.Run's own derived context and
// checks the effect through the same two signals the real fix is judged by — context.Done(), not
// a bool, and no further herdr call actually landing — rather than driving a real bubbletea program
// loop, which nothing in this package's tests does (Run itself hard-codes tea.NewProgram with no
// hook for injecting a fake terminal). fakeHerdr's ctx.Err() check above is what makes the second
// half observable at all: without it, a call made after cancellation would still "succeed" and this
// test would not fail even if submitSessionStart stopped deriving launch.ctx from b.ctx.
func TestBoardRootContextCancellationStopsInFlightSessionStart(t *testing.T) {
	store := newFakeStore(model.Task{ID: 1, Title: "t", Status: "todo"})
	herdrOps := &fakeHerdr{}
	rootCtx, cancelRoot := context.WithCancel(context.Background())
	h := newHarness(t, Deps{Tasks: store, Herdr: herdrOps}, Settings{})
	h.board.ctx = rootCtx // stands in for the boardCtx tui.Run derives and defers cancel on

	h.key("g")
	h.board.sessionStart.cwdInput.SetValue("/repo/work")
	tabCmd := h.dispatch(keyMsg("enter")) // op starts, left pending at the createTab stage

	if !h.board.launch.pending {
		t.Fatal("launch.pending がセットされていない")
	}
	launchCtx := h.board.launch.ctx

	select {
	case <-launchCtx.Done():
		t.Fatal("launch.ctx がキャンセル前から Done になっている")
	default:
	}

	cancelRoot() // stands in for tui.Run's deferred cancel firing on the way out

	select {
	case <-launchCtx.Done():
	default:
		t.Fatal("launch.ctx が親 context のキャンセルに追随していない")
	}

	// Nothing unsubscribes the createTab Cmd already in flight, so it still runs — but with its
	// context now cancelled it must stop cold rather than reach herdr, the same way a real
	// exec.CommandContext given an already-done context fails before ever spawning the process.
	h.run(tabCmd)
	if len(herdrOps.tabs) != 0 {
		t.Error("cancel 後なのに CreateTab が herdr 呼び出しを記録した")
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
