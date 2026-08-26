package tui

import (
	"strings"
	"testing"

	"github.com/ukwhatn/taskherd/internal/herdrc"
	"github.com/ukwhatn/taskherd/internal/model"
)

// focusDetailItem walks the detail cursor onto the first row of the given kind with the arrow
// keys, so the navigation under test is the real one rather than an assignment.
func focusDetailItem(t *testing.T, h *harness, kind detailItemKind, ref string) {
	t.Helper()
	task := h.board.activeTask()
	if task == nil {
		t.Fatal("詳細モーダルの対象タスクがない")
	}
	for i, item := range h.board.detailItems(*task) {
		if item.kind != kind || (ref != "" && item.ref != ref) {
			continue
		}
		for h.board.detail.cursor < i {
			h.key("down")
		}
		for h.board.detail.cursor > i {
			h.key("up")
		}
		return
	}
	t.Fatalf("詳細モーダルに該当項目がない: kind=%v ref=%q", kind, ref)
}

func TestDetailOpensAndReturns(t *testing.T) {
	store := newFakeStore(model.Task{ID: 1, Title: "設計", Status: "todo", Note: "詳細メモ"})
	h := newHarness(t, Deps{Tasks: store}, Settings{})

	h.key("enter")
	if h.board.mode != modeDetail {
		t.Fatalf("mode = %v, want modeDetail", h.board.mode)
	}
	view := h.board.render()
	for _, want := range []string{"#1 設計", ja.Detail.LabelTitle, ja.Detail.LabelStatus, "詳細メモ", ja.Detail.AddLink, ja.Detail.AddSession} {
		if !strings.Contains(view, want) {
			t.Errorf("詳細ビューに %q が無い:\n%s", want, view)
		}
	}

	h.key("q")
	if h.board.mode != modeBoard {
		t.Errorf("mode = %v, want modeBoard", h.board.mode)
	}
}

// One grammar for every field: pick the row with ↑↓, press Enter to edit it.
func TestDetailEditsTitle(t *testing.T) {
	store := newFakeStore(model.Task{ID: 1, Title: "旧", Status: "todo"})
	h := newHarness(t, Deps{Tasks: store}, Settings{})

	h.key("enter")
	focusDetailItem(t, h, itemTitle, "")
	h.key("enter")
	if !h.board.detail.editing {
		t.Fatal("編集モードに入っていない")
	}
	if got := h.board.detail.input.Value(); got != "旧" {
		t.Errorf("prefill = %q, want 旧", got)
	}
	h.typeText("新")
	h.key("enter")

	if got := store.snapshot().Tasks[0].Title; got != "旧新" {
		t.Errorf("title = %q, want 旧新", got)
	}
	// Editing a field returns to the item list, not out to the board.
	if h.board.mode != modeDetail || h.board.detail.editing {
		t.Errorf("mode = %v editing = %v, want 項目リストに戻る", h.board.mode, h.board.detail.editing)
	}
}

func TestDetailEditCancelled(t *testing.T) {
	store := newFakeStore(model.Task{ID: 1, Title: "旧", Status: "todo"})
	h := newHarness(t, Deps{Tasks: store}, Settings{})

	h.key("enter")
	focusDetailItem(t, h, itemTitle, "")
	h.key("enter")
	h.typeText("捨てる")
	h.key("esc")

	if got := store.snapshot().Tasks[0].Title; got != "旧" {
		t.Errorf("title = %q, want 旧（esc で取消）", got)
	}
	if h.board.detail.editing {
		t.Error("esc で編集モードを抜けていない")
	}
}

// The status row is the exception to "Enter to edit": ←→ switches it where it stands.
func TestDetailStatusShiftsInPlace(t *testing.T) {
	store := newFakeStore(model.Task{ID: 1, Title: "t", Status: "todo"})
	h := newHarness(t, Deps{Tasks: store}, Settings{})

	h.key("enter")
	focusDetailItem(t, h, itemStatus, "")
	h.key("right")

	if got := store.snapshot().Tasks[0].Status; got != "working" {
		t.Fatalf("status = %q, want working", got)
	}
	// The modal is pinned to the task, so it stays open on it even though the card moved column.
	if h.board.mode != modeDetail || h.board.detail.taskID != 1 {
		t.Fatalf("mode = %v taskID = %d, want #1 の詳細のまま", h.board.mode, h.board.detail.taskID)
	}

	h.key("left")
	if got := store.snapshot().Tasks[0].Status; got != "todo" {
		t.Errorf("status = %q, want todo", got)
	}
}

func TestDetailStatusSelectorFromDetail(t *testing.T) {
	store := newFakeStore(model.Task{ID: 1, Title: "t", Status: "todo"})
	h := newHarness(t, Deps{Tasks: store}, Settings{})

	h.key("enter")
	focusDetailItem(t, h, itemStatus, "")
	h.key("enter")
	if h.board.mode != modeStatusSelect {
		t.Fatalf("mode = %v, want modeStatusSelect", h.board.mode)
	}
	h.key("enter")

	if got := store.snapshot().Tasks[0].Status; got != "working" {
		t.Fatalf("status = %q, want working", got)
	}
	// The picker returns to the screen it was opened from.
	if h.board.mode != modeDetail {
		t.Errorf("mode = %v, want modeDetail", h.board.mode)
	}
}

func TestDetailEditsDue(t *testing.T) {
	store := newFakeStore(model.Task{ID: 1, Title: "t", Status: "todo"})
	h := newHarness(t, Deps{Tasks: store}, Settings{})

	h.key("enter")
	focusDetailItem(t, h, itemDue, "")
	h.key("enter")
	h.typeText("2026-09-30")
	h.key("enter")

	got := store.snapshot().Tasks[0].Due
	if got == nil || string(*got) != "2026-09-30" {
		t.Fatalf("due = %v, want 2026-09-30", got)
	}
}

func TestDetailRejectsBadDue(t *testing.T) {
	store := newFakeStore(model.Task{ID: 1, Title: "t", Status: "todo"})
	h := newHarness(t, Deps{Tasks: store}, Settings{})

	h.key("enter")
	focusDetailItem(t, h, itemDue, "")
	h.key("enter")
	h.typeText("2026/09/30")
	h.key("enter")

	if store.snapshot().Tasks[0].Due != nil {
		t.Error("不正な日付が保存された")
	}
	if !h.board.statusIsError {
		t.Error("エラーが報告されていない")
	}
}

func TestDetailAddsLink(t *testing.T) {
	store := newFakeStore(task(1, "todo"))
	links := &fakeLinks{}
	h := newHarness(t, Deps{Tasks: store, Links: links, Cache: &fakeCache{}}, Settings{
		Classifier: model.URLClassifier{},
	})

	h.key("enter")
	focusDetailItem(t, h, itemAddLink, "")
	h.key("enter")
	h.typeText("https://github.com/o/r/pull/7")
	h.key("enter")

	task := store.snapshot().Tasks[0]
	if len(task.Links) != 1 {
		t.Fatalf("links = %+v, want 1 件", task.Links)
	}
	if task.Links[0].Kind != model.LinkKindGitHubPR {
		t.Errorf("kind = %q, want github_pr", task.Links[0].Kind)
	}
	// A link with no cached status yet is worth fetching straight away rather than at the next tick.
	if len(links.calls) != 1 {
		t.Errorf("RefreshLinks 呼び出し = %d 回, want 1", len(links.calls))
	}
}

// A pasted block of URLs goes in as a batch, which is what makes pasting several PRs one gesture.
func TestDetailAddsMultipleLinksFromOnePaste(t *testing.T) {
	store := newFakeStore(task(1, "todo"))
	h := newHarness(t, Deps{Tasks: store, Links: &fakeLinks{}, Cache: &fakeCache{}}, Settings{
		Classifier: model.URLClassifier{},
	})

	h.key("enter")
	focusDetailItem(t, h, itemAddLink, "")
	h.key("enter")
	h.paste("https://github.com/o/r/pull/1\nhttps://github.com/o/r/pull/2 https://github.com/o/r/issues/3")
	h.key("enter")

	links := store.snapshot().Tasks[0].Links
	if len(links) != 3 {
		t.Fatalf("links = %+v, want 3 件", links)
	}
}

// All-or-nothing: one bad entry must not leave the good ones half-added.
func TestDetailRejectsBatchWithBadURL(t *testing.T) {
	store := newFakeStore(task(1, "todo"))
	h := newHarness(t, Deps{Tasks: store}, Settings{})

	h.key("enter")
	focusDetailItem(t, h, itemAddLink, "")
	h.key("enter")
	h.typeText("https://github.com/o/r/pull/1 github.com/o/r")
	h.key("enter")

	if got := len(store.snapshot().Tasks[0].Links); got != 0 {
		t.Errorf("links = %d 件, want 0（一部だけ登録しない）", got)
	}
	if !h.board.statusIsError {
		t.Error("エラーが報告されていない")
	}
}

func TestDetailEditsLinkNote(t *testing.T) {
	url := "https://github.com/o/r/pull/1"
	store := newFakeStore(model.Task{
		ID: 1, Title: "t", Status: "todo",
		Links: []model.Link{{URL: url, Kind: model.LinkKindGitHubPR}},
	})
	h := newHarness(t, Deps{Tasks: store}, Settings{})

	h.key("enter")
	focusDetailItem(t, h, itemLink, url)
	h.key("enter")
	h.typeText("本体実装PR")
	h.key("enter")

	if got := store.snapshot().Tasks[0].Links[0].Note; got != "本体実装PR" {
		t.Errorf("note = %q, want 本体実装PR", got)
	}
}

func TestDetailDeleteUnlinksLink(t *testing.T) {
	url := "https://github.com/o/r/pull/1"
	store := newFakeStore(model.Task{
		ID: 1, Title: "t", Status: "todo",
		Links: []model.Link{{URL: url, Kind: model.LinkKindGitHubPR}},
	})
	h := newHarness(t, Deps{Tasks: store}, Settings{})

	h.key("enter")
	focusDetailItem(t, h, itemLink, url)
	h.key("backspace")
	if h.board.mode != modeConfirm {
		t.Fatalf("mode = %v, want modeConfirm", h.board.mode)
	}
	h.key("n")
	if len(store.snapshot().Tasks[0].Links) != 1 {
		t.Fatal("n で中止したのにリンクが消えた")
	}
	// Cancelling returns to the detail modal, not out to the board.
	if h.board.mode != modeDetail {
		t.Fatalf("mode = %v, want modeDetail", h.board.mode)
	}

	h.key("backspace")
	h.key("y")

	if got := len(store.snapshot().Tasks[0].Links); got != 0 {
		t.Errorf("links = %d 件, want 0", got)
	}
}

func TestDetailDeleteUnlinksSession(t *testing.T) {
	store := newFakeStore(model.Task{
		ID: 1, Title: "t", Status: "todo",
		Sessions: []model.SessionRef{{Agent: "claude", SessionID: "s-1", Cwd: "/tmp/work"}},
	})
	h := newHarness(t, Deps{Tasks: store}, Settings{})

	h.key("enter")
	focusDetailItem(t, h, itemSession, "s-1")
	h.key("delete")
	h.key("y")

	if got := len(store.snapshot().Tasks[0].Sessions); got != 0 {
		t.Errorf("sessions = %d 件, want 0", got)
	}
}

// delete only means "unlink" on the rows that have something to unlink.
func TestDetailDeleteOnPlainFieldIsRefused(t *testing.T) {
	store := newFakeStore(model.Task{ID: 1, Title: "t", Status: "todo"})
	h := newHarness(t, Deps{Tasks: store}, Settings{})

	h.key("enter")
	focusDetailItem(t, h, itemTitle, "")
	h.key("backspace")

	if h.board.mode != modeDetail {
		t.Fatalf("mode = %v, want modeDetail のまま", h.board.mode)
	}
	if len(store.snapshot().Tasks) != 1 {
		t.Error("タイトル行の delete でタスクが消えた")
	}
}

func TestDetailLinksSessionFromSnapshot(t *testing.T) {
	store := newFakeStore(task(1, "todo"))
	herdrOps := &fakeHerdr{snapshot: &herdrc.Snapshot{Agents: []herdrc.Agent{
		agent("pane-1", "s-1", herdrc.StateWorking),
		agent("pane-2", "s-2", herdrc.StateIdle),
	}}}
	sessions := newFakeSessions(t)
	h := newHarness(t, Deps{Tasks: store, Sessions: sessions, Herdr: herdrOps}, Settings{})
	h.dispatch(snapshotUpdate())

	h.key("enter")
	focusDetailItem(t, h, itemAddSession, "")
	h.key("enter")
	if h.board.mode != modeSessionSelect {
		t.Fatalf("mode = %v, want modeSessionSelect", h.board.mode)
	}
	if len(h.board.sessionSel.agents) != 2 {
		t.Fatalf("agents = %+v, want 2 件", h.board.sessionSel.agents)
	}

	h.key("down")
	h.key("enter")

	linked := store.snapshot().Tasks[0].Sessions
	if len(linked) != 1 {
		t.Fatalf("sessions = %+v, want 1 件", linked)
	}
	if linked[0].SessionID != "s-2" || linked[0].Agent != "claude" || linked[0].Cwd != "/tmp/work" {
		t.Errorf("session = %+v, want s-2/claude/cwd 付き", linked[0])
	}
	if h.board.mode != modeDetail {
		t.Errorf("mode = %v, want modeDetail", h.board.mode)
	}
}

// A session linked from the board shows its live state at once. The states are derived from the
// task list, so a list that just gained a session has to re-derive them; without that the row the
// user just created reads "offline" until herdr happens to speak again.
func TestDetailLinkedSessionGetsLiveStateImmediately(t *testing.T) {
	store := newFakeStore(task(1, "todo"))
	live := agent("pane-1", "s-1", herdrc.StateWorking)
	herdrOps := &fakeHerdr{snapshot: &herdrc.Snapshot{Agents: []herdrc.Agent{live}}}
	sessions := newFakeSessions(t)
	h := newHarness(t, Deps{Tasks: store, Sessions: sessions, Herdr: herdrOps}, Settings{})
	h.dispatch(snapshotUpdate(live))

	h.key("enter")
	focusDetailItem(t, h, itemAddSession, "")
	h.key("enter")
	h.key("enter")

	if got := h.board.sessions.State["s-1"]; got != herdrc.StateWorking {
		t.Errorf("state = %q, want %q", got, herdrc.StateWorking)
	}
	if got := h.board.sessions.Pane["s-1"]; got != "pane-1" {
		t.Errorf("pane = %q, want pane-1", got)
	}
	if view := h.board.render(); !strings.Contains(view, "pane pane-1") {
		t.Errorf("セッション行に pane が出ていない:\n%s", view)
	}
}

// An agent herdr cannot name a session for is shown but cannot be picked: storing it would give a
// task a session reference that no jump could ever use.
func TestDetailSessionSelectRefusesAgentWithoutSessionID(t *testing.T) {
	store := newFakeStore(task(1, "todo"))
	herdrOps := &fakeHerdr{snapshot: &herdrc.Snapshot{Agents: []herdrc.Agent{
		{PaneID: "pane-1", Agent: "claude", Cwd: "/tmp/work"},
	}}}
	sessions := newFakeSessions(t)
	h := newHarness(t, Deps{Tasks: store, Sessions: sessions, Herdr: herdrOps}, Settings{})
	h.dispatch(snapshotUpdate())

	h.key("enter")
	focusDetailItem(t, h, itemAddSession, "")
	h.key("enter")
	h.key("enter")

	if len(store.snapshot().Tasks[0].Sessions) != 0 {
		t.Error("session_id 不明の agent が紐づけられた")
	}
	if !strings.Contains(h.board.sessionSel.err, "integration install") {
		t.Errorf("err = %q, want 検出できない旨の案内", h.board.sessionSel.err)
	}
}

// Without herdr there is nothing to pick from, so the row says so instead of opening an empty list.
func TestDetailSessionRowDisabledWithoutHerdr(t *testing.T) {
	store := newFakeStore(task(1, "todo"))
	h := newHarness(t, Deps{Tasks: store}, Settings{})

	h.key("enter")
	task := h.board.activeTask()
	items := h.board.detailItems(*task)
	last := items[len(items)-1]
	if last.kind != itemAddSession || !last.disabled {
		t.Fatalf("末尾の項目 = %+v, want ＋セッション行が無効表示", last)
	}

	focusDetailItem(t, h, itemAddSession, "")
	h.key("enter")

	if h.board.mode == modeSessionSelect {
		t.Error("herdr 不達なのにセレクタが開いた")
	}
	if !h.board.statusIsError {
		t.Error("エラーが報告されていない")
	}
}

// The modal is pinned to a task id, so a task removed underneath it closes it rather than leaving
// the screen on nothing.
func TestDetailClosesWhenTaskDisappears(t *testing.T) {
	store := newFakeStore(task(1, "todo"), task(2, "todo"))
	h := newHarness(t, Deps{Tasks: store}, Settings{})

	h.key("enter")
	if err := store.Update(nil, func(f *model.File) error {
		_, err := f.RemoveTask(1)
		return err
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	h.reload()

	if h.board.mode != modeBoard {
		t.Errorf("mode = %v, want modeBoard", h.board.mode)
	}
}

// The q that closes a quitOnClose detail has to end the program, not fall back to the board: the
// board behind it was never the point of a detail opened straight from prefix+t.
func TestDetailCloseQuitsWhenOpenedFromStart(t *testing.T) {
	store := newFakeStore(task(1, "todo"))
	h := newHarness(t, Deps{Tasks: store}, Settings{DetailTaskID: 1})
	if h.board.mode != modeDetail || !h.board.detail.quitOnClose {
		t.Fatalf("前提: quitOnClose 付きで detail が開いていること（mode=%v quitOnClose=%v）",
			h.board.mode, h.board.detail.quitOnClose)
	}

	cmd := h.dispatch(keyMsg("q"))

	if cmd == nil {
		t.Fatal("q がコマンドを返していない、want tea.Quit")
	}
	if h.board.mode != modeDetail {
		t.Errorf("mode = %v, want modeDetail のまま（quit する前に board へ落ちない）", h.board.mode)
	}
}

// The ordinary board-opened detail is the contrast case: its q returns to the board and quits
// nothing.
func TestDetailCloseReturnsToBoardWhenOpenedNormally(t *testing.T) {
	store := newFakeStore(task(1, "todo"))
	h := newHarness(t, Deps{Tasks: store}, Settings{})

	h.key("enter")
	cmd := h.dispatch(keyMsg("q"))

	if cmd != nil {
		t.Error("通常に開いた detail の q が何かコマンドを返している、want nil")
	}
	if h.board.mode != modeBoard {
		t.Errorf("mode = %v, want modeBoard", h.board.mode)
	}
}

// Once a detail opened from board closes over #1 disappearing, opening a fresh one over #2 gets a
// clean detailState — quitOnClose is not something the board carries between details.
func TestDetailReopenedAfterQuitOnCloseTargetDisappearsDoesNotQuit(t *testing.T) {
	store := newFakeStore(task(1, "todo"), task(2, "todo"))
	h := newHarness(t, Deps{Tasks: store}, Settings{DetailTaskID: 1})
	if !h.board.detail.quitOnClose {
		t.Fatal("前提: quitOnClose が立っていること")
	}

	if err := store.Update(nil, func(f *model.File) error {
		_, err := f.RemoveTask(1)
		return err
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	h.reload()
	if h.board.mode != modeBoard {
		t.Fatalf("mode = %v, want modeBoard（#1 消滅で board に戻る）", h.board.mode)
	}

	h.key("enter") // opens #2's detail fresh, from the board
	if h.board.mode != modeDetail || h.board.detail.quitOnClose {
		t.Fatalf("mode=%v quitOnClose=%v, want modeDetail かつ quitOnClose=false", h.board.mode, h.board.detail.quitOnClose)
	}
	cmd := h.dispatch(keyMsg("q"))

	if cmd != nil {
		t.Error("#2 の detail の q が何かコマンドを返している（プログラムが終了してはいけない）")
	}
	if h.board.mode != modeBoard {
		t.Errorf("mode = %v, want modeBoard", h.board.mode)
	}
}

// A quitOnClose detail with an overlay open over it (the status-select picker, opened from the
// modal's own ステータス row) still has to fall back to the board — not quit — when the task it is
// pinned to disappears underneath both of them.
func TestDetailQuitOnCloseDoesNotQuitWhenTaskDisappearsUnderOverlay(t *testing.T) {
	store := newFakeStore(task(1, "todo"))
	h := newHarness(t, Deps{Tasks: store}, Settings{DetailTaskID: 1})
	if h.board.mode != modeDetail || !h.board.detail.quitOnClose {
		t.Fatalf("前提: quitOnClose 付きで detail が開いていること（mode=%v quitOnClose=%v）",
			h.board.mode, h.board.detail.quitOnClose)
	}
	focusDetailItem(t, h, itemStatus, "")
	h.key("enter")
	if h.board.mode != modeStatusSelect {
		t.Fatalf("mode = %v, want modeStatusSelect", h.board.mode)
	}

	if err := store.Update(nil, func(f *model.File) error {
		_, err := f.RemoveTask(1)
		return err
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	h.reload()

	if h.board.mode != modeBoard {
		t.Errorf("mode = %v, want modeBoard（quitOnClose でも自動終了しない）", h.board.mode)
	}
}

func TestDetailNoteWithoutEditorReports(t *testing.T) {
	store := newFakeStore(model.Task{ID: 1, Title: "t", Status: "todo"})
	h := newHarness(t, Deps{Tasks: store}, Settings{})

	h.key("enter")
	focusDetailItem(t, h, itemNote, "")
	h.key("enter")

	if !strings.Contains(h.board.status, "EDITOR") {
		t.Errorf("status = %q, want エディタ設定の案内", h.board.status)
	}
}

// The editor named in config is the one the board opens, which is what makes note editing work in
// a herdr pane that never saw the user's environment.
func TestDetailNoteUsesConfiguredEditor(t *testing.T) {
	store := newFakeStore(model.Task{ID: 1, Title: "t", Status: "todo"})
	h := newHarness(t, Deps{Tasks: store}, Settings{Editor: "true"})

	h.key("enter")
	focusDetailItem(t, h, itemNote, "")
	if cmd := h.dispatch(keyMsg("enter")); cmd == nil {
		t.Fatal("エディタ起動コマンドが返っていない")
	}
	if h.board.status != "" {
		t.Errorf("status = %q, want 空（エディタは解決できている）", h.board.status)
	}
}

func TestDetailJumpUsesModalTask(t *testing.T) {
	store := newFakeStore(
		task(1, "todo"),
		model.Task{
			ID: 2, Title: "second", Status: "todo",
			Sessions: []model.SessionRef{{Agent: "claude", SessionID: "s-2", Cwd: "/tmp/work"}},
		},
	)
	sessions := newFakeSessions(t)
	herdrOps := &fakeHerdr{}
	h := newHarness(t, Deps{Tasks: store, Sessions: sessions, Herdr: herdrOps}, Settings{})
	h.dispatch(snapshotUpdate(agent("pane-2", "s-2", herdrc.StateWorking)))

	h.key("down")
	h.key("enter")
	h.key("g")

	if len(herdrOps.focused) != 1 || herdrOps.focused[0] != "pane-2" {
		t.Errorf("focused = %v, want [pane-2]", herdrOps.focused)
	}
}
