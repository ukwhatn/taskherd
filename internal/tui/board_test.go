package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/ukwhatn/taskherd/internal/fetch"
	"github.com/ukwhatn/taskherd/internal/herdrc"
	"github.com/ukwhatn/taskherd/internal/model"
)

func TestBoardMovesFocusBetweenColumns(t *testing.T) {
	store := newFakeStore(task(1, "todo"), task(2, "working"))
	h := newHarness(t, Deps{Tasks: store}, Settings{})

	if h.board.colIdx != 0 {
		t.Fatalf("colIdx = %d, want 0", h.board.colIdx)
	}
	h.key("right")
	if h.board.colIdx != 1 {
		t.Errorf("→ 後の colIdx = %d, want 1", h.board.colIdx)
	}
	h.key("left")
	if h.board.colIdx != 0 {
		t.Errorf("← 後の colIdx = %d, want 0", h.board.colIdx)
	}
	// The edges hold: ← at the leftmost column is a no-op rather than a wrap.
	h.key("left")
	if h.board.colIdx != 0 {
		t.Errorf("左端で ← した後の colIdx = %d, want 0", h.board.colIdx)
	}
}

func TestBoardMovesSelectionWithinColumn(t *testing.T) {
	store := newFakeStore(task(1, "todo"), task(2, "todo"), task(3, "todo"))
	h := newHarness(t, Deps{Tasks: store}, Settings{})

	h.key("down")
	if got := h.board.currentTask(); got == nil || got.ID != 2 {
		t.Fatalf("↓ 後の選択 = %+v, want #2", got)
	}
	h.key("down")
	if got := h.board.currentTask(); got == nil || got.ID != 3 {
		t.Fatalf("↓↓ 後の選択 = %+v, want #3", got)
	}
	h.key("down")
	if got := h.board.currentTask(); got == nil || got.ID != 3 {
		t.Errorf("末尾で ↓ した後の選択 = %+v, want #3 のまま", got)
	}
	h.key("up")
	if got := h.board.currentTask(); got == nil || got.ID != 2 {
		t.Errorf("↑ 後の選択 = %+v, want #2", got)
	}
}

// v0.2 dropped the vim bindings: the letters they used are free for other meanings, so pressing
// them must do nothing at all rather than quietly still moving the cursor.
func TestBoardVimKeysAreInert(t *testing.T) {
	store := newFakeStore(task(1, "todo"), task(2, "todo"), task(3, "working"))
	h := newHarness(t, Deps{Tasks: store}, Settings{})

	for _, name := range []string{"h", "j", "k", "l", "H", "L", "n", "x"} {
		h.key(name)
		if h.board.colIdx != 0 {
			t.Fatalf("%s で列が動いた: colIdx = %d", name, h.board.colIdx)
		}
		if got := h.board.currentTask(); got == nil || got.ID != 1 {
			t.Fatalf("%s で選択が動いた: %+v", name, got)
		}
		if h.board.mode != modeBoard {
			t.Fatalf("%s でモードが変わった: %v", name, h.board.mode)
		}
	}
	if got := store.snapshot().Tasks[0].Status; got != "todo" {
		t.Errorf("status = %q, want todo（旧 H/L で移動しない）", got)
	}
}

// Tab opens the destination picker on the next column, so Enter alone is the one-step move that
// H/L used to be.
func TestBoardStatusSelectorDefaultsToNextColumn(t *testing.T) {
	store := newFakeStore(task(1, "todo"))
	h := newHarness(t, Deps{Tasks: store}, Settings{})

	h.key("tab")
	if h.board.mode != modeStatusSelect {
		t.Fatalf("mode = %v, want modeStatusSelect", h.board.mode)
	}
	if got := h.board.statusSel.targets[h.board.statusSel.cursor].ID; got != "working" {
		t.Fatalf("既定の選択 = %q, want working（次の列）", got)
	}

	h.key("enter")

	if got := store.snapshot().Tasks[0].Status; got != "working" {
		t.Fatalf("status = %q, want working", got)
	}
	if h.board.mode != modeBoard {
		t.Errorf("mode = %v, want modeBoard", h.board.mode)
	}
	// The cursor follows the card into its new column instead of staying on an empty slot.
	if h.board.colIdx != 1 {
		t.Errorf("colIdx = %d, want 1（移動先の列）", h.board.colIdx)
	}
}

func TestBoardStatusSelectorMovesWithArrows(t *testing.T) {
	store := newFakeStore(task(1, "working"))
	h := newHarness(t, Deps{Tasks: store}, Settings{})
	h.key("right")

	h.key("tab")
	// Columns are todo / working / done, and the picker opened on done.
	h.key("left")
	h.key("left")
	h.key("enter")

	if got := store.snapshot().Tasks[0].Status; got != "todo" {
		t.Errorf("status = %q, want todo", got)
	}
}

func TestBoardStatusSelectorCancels(t *testing.T) {
	store := newFakeStore(task(1, "todo"))
	h := newHarness(t, Deps{Tasks: store}, Settings{})

	h.key("tab")
	h.key("esc")

	if h.board.mode != modeBoard {
		t.Fatalf("mode = %v, want modeBoard", h.board.mode)
	}
	if got := store.snapshot().Tasks[0].Status; got != "todo" {
		t.Errorf("status = %q, want todo（esc で取消）", got)
	}
	if store.updates != 0 {
		t.Errorf("updates = %d, want 0", store.updates)
	}
}

// The terminal column is a destination like any other; only (unknown) is excluded.
func TestBoardStatusSelectorExcludesUnknownColumn(t *testing.T) {
	store := newFakeStore(task(1, "retired"))
	h := newHarness(t, Deps{Tasks: store}, Settings{})
	h.board.colIdx = len(h.board.columns) - 1

	h.key("tab")

	for _, target := range h.board.statusSel.targets {
		if target.Unknown {
			t.Fatalf("移行先に (unknown) が含まれている: %+v", h.board.statusSel.targets)
		}
	}
	if got := h.board.statusSel.targets[h.board.statusSel.cursor].ID; got != "todo" {
		t.Errorf("既定の選択 = %q, want todo（(unknown) には次の列が無い）", got)
	}
	if !containsColumn(h.board.statusSel.targets, "done") {
		t.Error("terminal 列が移行先に含まれていない")
	}
}

func TestBoardStatusSelectorAtLastColumnStaysPut(t *testing.T) {
	store := newFakeStore(task(1, "done"))
	h := newHarness(t, Deps{Tasks: store}, Settings{})
	h.key("t") // a collapsed column has no card to focus
	h.key("right")
	h.key("right")

	h.key("tab")

	if got := h.board.statusSel.targets[h.board.statusSel.cursor].ID; got != "done" {
		t.Errorf("既定の選択 = %q, want done（右端は動かない）", got)
	}
}

func TestBoardDeleteTaskAsksFirst(t *testing.T) {
	store := newFakeStore(task(1, "todo"), task(2, "todo"))
	h := newHarness(t, Deps{Tasks: store}, Settings{})

	h.key("backspace")
	if h.board.mode != modeConfirm {
		t.Fatalf("mode = %v, want modeConfirm", h.board.mode)
	}
	h.key("n")
	if len(store.snapshot().Tasks) != 2 {
		t.Fatal("n で中止したのにタスクが消えた")
	}

	h.key("backspace")
	h.key("y")

	tasks := store.snapshot().Tasks
	if len(tasks) != 1 || tasks[0].ID != 2 {
		t.Fatalf("tasks = %+v, want #2 のみ", tasks)
	}
}

// The Delete key and Backspace are the same gesture on most keyboards, so both do this.
func TestBoardDeleteKeyAlsoDeletes(t *testing.T) {
	store := newFakeStore(task(1, "todo"))
	h := newHarness(t, Deps{Tasks: store}, Settings{})

	h.key("delete")
	h.key("y")

	if len(store.snapshot().Tasks) != 0 {
		t.Errorf("tasks = %+v, want 0 件", store.snapshot().Tasks)
	}
}

func TestBoardToggleTerminalCollapse(t *testing.T) {
	store := newFakeStore(task(1, "done"))
	h := newHarness(t, Deps{Tasks: store}, Settings{})

	terminal := h.board.columns[len(h.board.columns)-1]
	if !terminal.Collapsed {
		t.Fatalf("terminal 列 = %+v, want 既定で折り畳み", terminal)
	}

	h.key("t")
	if h.board.columns[len(h.board.columns)-1].Collapsed {
		t.Error("t を押しても展開されない")
	}
	h.key("t")
	if !h.board.columns[len(h.board.columns)-1].Collapsed {
		t.Error("t を再度押しても折り畳まれない")
	}
}

func TestBoardQuits(t *testing.T) {
	h := newHarness(t, Deps{Tasks: newFakeStore()}, Settings{})

	for _, name := range []string{"q", "ctrl+c"} {
		cmd := h.dispatch(keyMsg(name))
		if cmd == nil {
			t.Fatalf("%s でコマンドが返らない", name)
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Errorf("%s = %T, want tea.QuitMsg", name, cmd())
		}
	}
}

// An external change to tasks.json (a CLI command, a Claude session) is picked up without the
// user asking for it.
func TestBoardReloadsOnFileEvent(t *testing.T) {
	store := newFakeStore(task(1, "todo"))
	files := newFakeFiles(t)
	h := newHarness(t, Deps{Tasks: store, Files: files}, Settings{})

	if err := store.Update(nil, func(f *model.File) error {
		_, err := f.AddTask(model.TaskInput{Title: "外部から追加", Status: "todo"}, boardNow)
		return err
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	// The watcher only ever says "something changed"; the board goes and re-reads.
	cmd := h.dispatch(fileEventMsg{})
	h.run(firstOf(t, cmd))

	if len(h.board.columns[0].Tasks) != 2 {
		t.Errorf("todo 列 = %+v, want 2 件", h.board.columns[0].Tasks)
	}
}

func TestBoardSessionBadgesFromLiveUpdate(t *testing.T) {
	store := newFakeStore(model.Task{
		ID: 1, Title: "t", Status: "todo",
		Sessions: []model.SessionRef{{Agent: "claude", SessionID: "s-1", Cwd: "/tmp/work"}},
	})
	sessions := newFakeSessions(t)
	herdrOps := &fakeHerdr{}
	h := newHarness(t, Deps{Tasks: store, Sessions: sessions, Herdr: herdrOps}, Settings{})

	h.dispatch(snapshotUpdate(agent("pane-1", "s-1", herdrc.StateWorking)))

	badge := BuildSessionBadge(store.snapshot().Tasks[0], h.board.sessions, h.board.icons)
	want := sessionStateText(herdrc.StateWorking, h.board.icons)
	if badge.Text != want {
		t.Errorf("badge = %q, want %q", badge.Text, want)
	}
	if h.board.lastHerdrSync.IsZero() {
		t.Error("herdr 同期時刻が記録されていない")
	}
}

// §7.5: the task id stamped on a pane expires after 24h, so the board re-stamps what it finds
// when it opens.
func TestBoardStampsTaskTokensOnFirstSnapshot(t *testing.T) {
	store := newFakeStore(model.Task{
		ID: 7, Title: "t", Status: "todo",
		Sessions: []model.SessionRef{{Agent: "claude", SessionID: "s-1", Cwd: "/tmp/work"}},
	})
	sessions := newFakeSessions(t)
	herdrOps := &fakeHerdr{}
	h := newHarness(t, Deps{Tasks: store, Sessions: sessions, Herdr: herdrOps}, Settings{})

	cmd := h.dispatch(snapshotUpdate(agent("pane-1", "s-1", herdrc.StateIdle)))
	h.run(secondOf(t, cmd))

	if len(herdrOps.tokens) != 1 {
		t.Fatalf("tokens = %+v, want 1 件", herdrOps.tokens)
	}
	if herdrOps.tokens[0].paneID != "pane-1" || herdrOps.tokens[0].taskID != 7 {
		t.Errorf("token = %+v, want pane-1/#7", herdrOps.tokens[0])
	}

	// Only once per board: a later snapshot must not re-stamp everything again.
	h.dispatch(snapshotUpdate(agent("pane-1", "s-1", herdrc.StateWorking)))
	if len(herdrOps.tokens) != 1 {
		t.Errorf("tokens = %+v, want 1 件のまま", herdrOps.tokens)
	}
}

// §13 acceptance: with herdr unreachable the board still opens and works; only the herdr-backed
// parts degrade, and they say so.
func TestBoardDegradesWithoutHerdr(t *testing.T) {
	store := newFakeStore(model.Task{
		ID: 1, Title: "タスク", Status: "todo",
		Sessions: []model.SessionRef{{Agent: "claude", SessionID: "s-1", Cwd: "/tmp/work"}},
	})
	sessions := newFakeSessions(t)
	h := newHarness(t, Deps{Tasks: store, Sessions: sessions}, Settings{})

	h.dispatch(sessionUpdateMsg{update: herdrc.Update{Status: herdrc.Status{Err: errUnavailable}}})

	if h.board.sessions.Available {
		t.Fatal("herdr 不達なのに Available = true")
	}
	view := h.board.render()
	if !strings.Contains(view, "オフライン") {
		t.Errorf("フッタにオフライン注記が無い:\n%s", view)
	}
	if !strings.Contains(view, "#1 タスク") {
		t.Errorf("カードが描画されていない:\n%s", view)
	}

	// The core still writes: task management does not depend on herdr.
	h.key("tab")
	h.key("enter")
	if got := store.snapshot().Tasks[0].Status; got != "working" {
		t.Errorf("status = %q, want working（herdr 不達でも列移動は動く）", got)
	}
}

func TestBoardJumpWithoutHerdrShowsManualCommand(t *testing.T) {
	store := newFakeStore(model.Task{
		ID: 1, Title: "t", Status: "todo",
		Sessions: []model.SessionRef{{Agent: "claude", SessionID: "s-1", Cwd: "/tmp/work"}},
	})
	h := newHarness(t, Deps{Tasks: store, Herdr: &fakeHerdr{}}, Settings{})

	h.key("g")

	if !h.board.statusIsError {
		t.Fatalf("status = %q, want エラー扱い", h.board.status)
	}
	if !strings.Contains(h.board.status, "claude --resume s-1") {
		t.Errorf("status = %q, want 手動 resume コマンドの案内", h.board.status)
	}
}

func TestBoardJumpFocusesLivePane(t *testing.T) {
	store := newFakeStore(model.Task{
		ID: 3, Title: "t", Status: "todo",
		Sessions: []model.SessionRef{{Agent: "claude", SessionID: "s-1", Cwd: "/tmp/work"}},
	})
	sessions := newFakeSessions(t)
	herdrOps := &fakeHerdr{}
	h := newHarness(t, Deps{Tasks: store, Sessions: sessions, Herdr: herdrOps}, Settings{})
	h.dispatch(snapshotUpdate(agent("pane-9", "s-1", herdrc.StateWorking)))

	h.key("g")

	if len(herdrOps.focused) != 1 || herdrOps.focused[0] != "pane-9" {
		t.Fatalf("focused = %v, want [pane-9]", herdrOps.focused)
	}
	if len(herdrOps.tokens) == 0 || herdrOps.tokens[len(herdrOps.tokens)-1].taskID != 3 {
		t.Errorf("tokens = %+v, want #3 の記録", herdrOps.tokens)
	}
}

// A pane that is gone means a resume, and a resume creates a pane: that gets a confirmation.
func TestBoardJumpConfirmsResume(t *testing.T) {
	store := newFakeStore(model.Task{
		ID: 4, Title: "resume 対象", Status: "todo",
		Sessions: []model.SessionRef{{Agent: "claude", SessionID: "s-gone", Cwd: "/tmp/work"}},
	})
	sessions := newFakeSessions(t)
	herdrOps := &fakeHerdr{}
	h := newHarness(t, Deps{Tasks: store, Sessions: sessions, Herdr: herdrOps}, Settings{})
	h.dispatch(snapshotUpdate())

	h.key("g")
	if h.board.mode != modeConfirm {
		t.Fatalf("mode = %v, want modeConfirm", h.board.mode)
	}
	if len(herdrOps.tabs) != 0 {
		t.Fatal("確認前に tab が作られた")
	}

	h.key("n")
	if len(herdrOps.tabs) != 0 {
		t.Fatal("n で中止したのに tab が作られた")
	}

	h.key("g")
	h.key("y")

	if len(herdrOps.tabs) != 1 || herdrOps.tabs[0].Cwd != "/tmp/work" {
		t.Fatalf("tabs = %+v, want cwd=/tmp/work", herdrOps.tabs)
	}
	if herdrOps.tabs[0].Label != "resume 対象" {
		t.Errorf("label = %q, want タスクタイトル", herdrOps.tabs[0].Label)
	}
	if len(herdrOps.started) != 1 {
		t.Fatalf("started = %+v, want 1 件", herdrOps.started)
	}
	started := herdrOps.started[0]
	if started.Kind != "claude" || strings.Join(started.Args, " ") != "--resume s-gone" {
		t.Errorf("started = %+v, want claude --resume s-gone", started)
	}
}

// agent_not_ready is not a failed jump: the pane exists and only a human can answer the prompt.
func TestBoardResumeReportsNeedsAttention(t *testing.T) {
	store := newFakeStore(model.Task{
		ID: 1, Title: "t", Status: "todo",
		Sessions: []model.SessionRef{{Agent: "claude", SessionID: "s-gone", Cwd: "/tmp/work"}},
	})
	sessions := newFakeSessions(t)
	herdrOps := &fakeHerdr{startResult: herdrc.StartResult{PaneID: "pane-new", NeedsAttention: true}}
	h := newHarness(t, Deps{Tasks: store, Sessions: sessions, Herdr: herdrOps}, Settings{})
	h.dispatch(snapshotUpdate())

	h.key("g")
	h.key("y")

	if !strings.Contains(h.board.status, "入力待ち") {
		t.Errorf("status = %q, want 入力待ちの案内", h.board.status)
	}
}

// A non-claude agent cannot be resumed, so the board says where to restart it by hand rather
// than opening a pane it cannot drive.
func TestBoardJumpUnsupportedAgent(t *testing.T) {
	store := newFakeStore(model.Task{
		ID: 1, Title: "t", Status: "todo",
		Sessions: []model.SessionRef{{Agent: "cursor", SessionID: "s-1", Cwd: "/tmp/work"}},
	})
	sessions := newFakeSessions(t)
	herdrOps := &fakeHerdr{}
	h := newHarness(t, Deps{Tasks: store, Sessions: sessions, Herdr: herdrOps}, Settings{})
	h.dispatch(snapshotUpdate())

	h.key("g")

	if h.board.mode == modeConfirm {
		t.Fatal("resume 未対応の agent で確認プロンプトが出た")
	}
	if !strings.Contains(h.board.status, "未対応") {
		t.Errorf("status = %q, want 未対応の案内", h.board.status)
	}
}

func TestBoardJumpPicksBetweenSessions(t *testing.T) {
	store := newFakeStore(model.Task{
		ID: 1, Title: "t", Status: "todo",
		Sessions: []model.SessionRef{
			{Agent: "claude", SessionID: "s-1", Cwd: "/tmp/a"},
			{Agent: "claude", SessionID: "s-2", Cwd: "/tmp/b"},
		},
	})
	sessions := newFakeSessions(t)
	herdrOps := &fakeHerdr{}
	h := newHarness(t, Deps{Tasks: store, Sessions: sessions, Herdr: herdrOps}, Settings{})
	h.dispatch(snapshotUpdate(
		agent("pane-1", "s-1", herdrc.StateIdle),
		agent("pane-2", "s-2", herdrc.StateWorking),
	))

	h.key("g")
	if h.board.mode != modeJump {
		t.Fatalf("mode = %v, want modeJump", h.board.mode)
	}
	h.key("down")
	h.key("enter")

	if len(herdrOps.focused) != 1 || herdrOps.focused[0] != "pane-2" {
		t.Errorf("focused = %v, want [pane-2]（2 番目のセッション）", herdrOps.focused)
	}
}

func TestBoardRefreshAllFetchesEveryLinkOnce(t *testing.T) {
	url := "https://github.com/o/r/pull/1"
	store := newFakeStore(
		model.Task{ID: 1, Title: "a", Status: "todo", Links: []model.Link{{URL: url, Kind: model.LinkKindGitHubPR}}},
		model.Task{ID: 2, Title: "b", Status: "todo", Links: []model.Link{{URL: url, Kind: model.LinkKindGitHubPR}}},
	)
	links := &fakeLinks{}
	h := newHarness(t, Deps{Tasks: store, Links: links, Cache: &fakeCache{}}, Settings{})

	h.key("R")

	if len(links.calls) != 1 {
		t.Fatalf("calls = %v, want 1 サイクル", links.calls)
	}
	// The same URL on two tasks shares one cache entry, so it is fetched once.
	if len(links.calls[0]) != 1 {
		t.Errorf("URL = %v, want 重複排除して 1 件", links.calls[0])
	}
	if h.board.lastFetch.IsZero() {
		t.Error("最終取得時刻が記録されていない")
	}
}

func TestBoardRefreshTaskOnlyFetchesFocusedCard(t *testing.T) {
	store := newFakeStore(
		model.Task{ID: 1, Title: "a", Status: "todo", Links: []model.Link{{URL: "https://github.com/o/r/pull/1", Kind: model.LinkKindGitHubPR}}},
		model.Task{ID: 2, Title: "b", Status: "todo", Links: []model.Link{{URL: "https://github.com/o/r/pull/2", Kind: model.LinkKindGitHubPR}}},
	)
	links := &fakeLinks{}
	h := newHarness(t, Deps{Tasks: store, Links: links, Cache: &fakeCache{}}, Settings{})
	h.key("down")

	h.key("r")

	if len(links.calls) != 1 || len(links.calls[0]) != 1 {
		t.Fatalf("calls = %v, want #2 のリンク 1 件のみ", links.calls)
	}
	if links.calls[0][0] != "https://github.com/o/r/pull/2" {
		t.Errorf("URL = %q, want pull/2", links.calls[0][0])
	}
}

// The background cycle only chases what has aged out; the manual keys ignore the TTL entirely.
func TestBoardBackgroundRefreshSkipsFreshLinks(t *testing.T) {
	url := "https://github.com/o/r/pull/1"
	store := newFakeStore(model.Task{
		ID: 1, Title: "a", Status: "todo",
		Links: []model.Link{{URL: url, Kind: model.LinkKindGitHubPR}},
	})
	links := &fakeLinks{}
	cache := &fakeCache{file: &fetch.CacheFile{Version: 1, Entries: map[string]fetch.CacheEntry{}}}
	fresh := boardNow.Add(-time.Minute).Format(time.RFC3339)
	cache.file.Entries[url] = fetch.CacheEntry{FetchedAt: &fresh, OK: true, Data: []byte(`{"state":"OPEN"}`)}

	h := newHarness(t, Deps{Tasks: store, Links: links, Cache: cache}, Settings{})
	h.dispatch(cacheLoadedMsg{cache: cache.Load()})

	h.run(h.board.refreshStaleCmd())
	if len(links.calls) != 0 {
		t.Errorf("calls = %v, want TTL 内なので取得しない", links.calls)
	}

	h.key("R")
	if len(links.calls) != 1 {
		t.Errorf("calls = %v, want 手動なら TTL を無視して取得", links.calls)
	}
}

// tasks.json and cache.json are read by two independent commands and land in either order.
// Link badges are derived from both, so whichever arrives second has to re-derive them; getting
// this wrong leaves every badge reading "not fetched yet" on a board whose cache is full.
func TestBoardDerivesLinkStatesWhateverTheLoadOrder(t *testing.T) {
	url := "https://github.com/o/r/pull/1"
	tasks := []model.Task{{
		ID: 1, Title: "a", Status: "todo",
		Links: []model.Link{{URL: url, Kind: model.LinkKindGitHubPR}},
	}}

	fresh := boardNow.Add(-time.Minute).Format(time.RFC3339)
	cacheFile := &fetch.CacheFile{Version: 1, Entries: map[string]fetch.CacheEntry{
		url: {FetchedAt: &fresh, OK: true, Data: []byte(`{"state":"OPEN","checks":"pass"}`)},
	}}

	for _, cacheFirst := range []bool{false, true} {
		name := "tasks が先"
		if cacheFirst {
			name = "cache が先"
		}
		t.Run(name, func(t *testing.T) {
			store := newFakeStore(tasks...)
			links := &fakeLinks{}
			board := New(context.Background(), Deps{
				Tasks: store,
				Cache: &fakeCache{file: cacheFile},
				Links: links,
				Now:   func() time.Time { return boardNow },
			}, Settings{Columns: testColumns(), CacheTTL: 5 * time.Minute})
			board.width, board.height = 200, 30
			h := &harness{t: t, board: board, store: store}

			file, err := store.Load()
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if cacheFirst {
				h.dispatch(cacheLoadedMsg{cache: cacheFile})
				h.run(h.dispatch(tasksLoadedMsg{file: file}))
			} else {
				h.dispatch(tasksLoadedMsg{file: file})
				h.run(h.dispatch(cacheLoadedMsg{cache: cacheFile}))
			}

			rows := BuildLinkRows(tasks[0], board.links, CardStyle{Icons: testIcons, Classifier: testClassifier})
			if len(rows) != 1 || rowText(rows[0]) != "PR o/r#1 open CI+" {
				t.Fatalf("rows = %+v, want キャッシュ由来の open CI+", rows)
			}
			// Nothing was stale, so the startup fetch had nothing to do either way.
			if len(links.calls) != 0 {
				t.Errorf("calls = %v, want TTL 内なので取得なし", links.calls)
			}
		})
	}
}

// The startup fetch needs both sources before it can tell what is stale, and it runs exactly once.
func TestBoardStartupFetchWaitsForBothSources(t *testing.T) {
	url := "https://github.com/o/r/pull/1"
	store := newFakeStore(model.Task{
		ID: 1, Title: "a", Status: "todo",
		Links: []model.Link{{URL: url, Kind: model.LinkKindGitHubPR}},
	})
	links := &fakeLinks{}
	cache := &fakeCache{}

	board := New(context.Background(), Deps{
		Tasks: store, Cache: cache, Links: links,
		Now: func() time.Time { return boardNow },
	}, Settings{Columns: testColumns(), CacheTTL: 5 * time.Minute})
	board.width, board.height = 200, 30
	h := &harness{t: t, board: board, store: store}

	file, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	h.run(h.dispatch(tasksLoadedMsg{file: file}))
	if len(links.calls) != 0 {
		t.Fatalf("calls = %v, want cache 未着なので取得しない", links.calls)
	}

	h.run(h.dispatch(cacheLoadedMsg{cache: cache.Load()}))
	if len(links.calls) != 1 {
		t.Fatalf("calls = %v, want 1 サイクル", links.calls)
	}

	// A later cache read is not a new startup.
	h.run(h.dispatch(cacheLoadedMsg{cache: cache.Load()}))
	if len(links.calls) != 1 {
		t.Errorf("calls = %v, want 1 サイクルのまま", links.calls)
	}
}

// A rate limit stretches the next background cycle instead of retrying at the same cadence.
func TestBoardRateLimitBacksOff(t *testing.T) {
	store := newFakeStore()
	h := newHarness(t, Deps{Tasks: store, Cache: &fakeCache{}}, Settings{RefreshInterval: 10 * time.Minute})

	if got := h.board.refreshInterval(); got != 10*time.Minute {
		t.Fatalf("初期間隔 = %v, want 10m", got)
	}

	h.dispatch(refreshDoneMsg{
		result: &fetch.RefreshResult{GitHubInterrupted: true},
		at:     boardNow,
	})
	if got := h.board.refreshInterval(); got != 20*time.Minute {
		t.Errorf("1 回目の中断後 = %v, want 20m", got)
	}

	h.dispatch(refreshDoneMsg{
		result: &fetch.RefreshResult{GitHubInterrupted: true},
		at:     boardNow,
	})
	if got := h.board.refreshInterval(); got != 40*time.Minute {
		t.Errorf("2 回目の中断後 = %v, want 40m", got)
	}

	// A clean cycle puts the cadence back where it was.
	h.dispatch(refreshDoneMsg{result: &fetch.RefreshResult{}, at: boardNow})
	if got := h.board.refreshInterval(); got != 10*time.Minute {
		t.Errorf("正常サイクル後 = %v, want 10m", got)
	}
}

// Jira says how long to wait; that wins over the backoff when it asks for longer.
func TestBoardJiraRetryAfterWins(t *testing.T) {
	h := newHarness(t, Deps{Tasks: newFakeStore(), Cache: &fakeCache{}}, Settings{RefreshInterval: time.Minute})

	h.dispatch(refreshDoneMsg{
		result: &fetch.RefreshResult{
			JiraInterrupted: true,
			Outcomes: []fetch.RefreshOutcome{
				{URL: "https://x.atlassian.net/browse/A-1", Err: &fetch.JiraRateLimitError{RetryAfter: 30 * time.Minute}},
			},
		},
		at: boardNow,
	})

	if got := h.board.refreshInterval(); got != 30*time.Minute {
		t.Errorf("間隔 = %v, want Retry-After の 30m", got)
	}
}

// refresh_interval_minutes = 0 turns the timer off; r and R still work.
func TestBoardRefreshIntervalZeroDisablesTimer(t *testing.T) {
	h := newHarness(t, Deps{Tasks: newFakeStore(), Cache: &fakeCache{}, Links: &fakeLinks{}}, Settings{RefreshInterval: 0})

	if cmd := h.board.tickCmd(); cmd != nil {
		t.Error("interval=0 なのにタイマーが仕掛けられた")
	}
}

func TestBoardWithoutLinkFetchReportsDisabled(t *testing.T) {
	store := newFakeStore(model.Task{ID: 1, Title: "t", Status: "todo"})
	h := newHarness(t, Deps{Tasks: store}, Settings{})

	h.key("R")

	if !strings.Contains(h.board.status, "無効") {
		t.Errorf("status = %q, want 無効の案内", h.board.status)
	}
}

func TestBoardStoreErrorSurfacesOnFooter(t *testing.T) {
	store := newFakeStore(task(1, "todo"))
	store.failWith = errUnavailable
	h := newHarness(t, Deps{Tasks: store}, Settings{})

	h.key("tab")
	h.key("enter")

	if !h.board.statusIsError || h.board.status == "" {
		t.Errorf("status = %q err=%v, want 失敗の報告", h.board.status, h.board.statusIsError)
	}
}

func TestBoardRendersColumnHeadersAndCards(t *testing.T) {
	store := newFakeStore(
		model.Task{ID: 1, Title: "設計する", Status: "todo", Due: due("2026-08-20")},
		model.Task{ID: 2, Title: "実装する", Status: "working"},
	)
	h := newHarness(t, Deps{Tasks: store}, Settings{})

	view := h.board.render()

	for _, want := range []string{"ToDo (1)", "Working (1)", "#1 設計する", "#2 実装する", "2026-08-20"} {
		if !strings.Contains(view, want) {
			t.Errorf("描画に %q が無い:\n%s", want, view)
		}
	}
	// A collapsed terminal column shows only its header and count.
	if !strings.Contains(view, h.board.icons.Collapsed+" Done") {
		t.Errorf("折り畳まれた terminal 列が描画されていない:\n%s", view)
	}
}

func TestBoardRendersUnknownColumn(t *testing.T) {
	store := newFakeStore(task(1, "retired"))
	h := newHarness(t, Deps{Tasks: store}, Settings{})

	view := h.board.render()

	if !strings.Contains(view, unknownColumnLabel) {
		t.Errorf("(unknown) 列が描画されていない:\n%s", view)
	}
}

func containsColumn(columns []Column, id string) bool {
	for _, col := range columns {
		if col.ID == id {
			return true
		}
	}
	return false
}

// firstOf and secondOf pick one command out of a batch, so a test can run the part it means to
// without touching the ones that park on a channel.
func firstOf(t *testing.T, cmd tea.Cmd) tea.Cmd {
	t.Helper()
	return nthOf(t, cmd, 0)
}

func secondOf(t *testing.T, cmd tea.Cmd) tea.Cmd {
	t.Helper()
	return nthOf(t, cmd, 1)
}

func nthOf(t *testing.T, cmd tea.Cmd, n int) tea.Cmd {
	t.Helper()
	if cmd == nil {
		t.Fatal("コマンドが nil")
	}
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		if n == 0 {
			return cmd
		}
		t.Fatalf("batch でないコマンドから %d 番目を取れない", n)
	}
	if n >= len(batch) {
		t.Fatalf("batch は %d 件しかない（%d 番目を要求）", len(batch), n)
	}
	return batch[n]
}
