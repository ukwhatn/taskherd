package tui

import (
	"strings"
	"testing"

	"github.com/ukwhatn/taskherd/internal/model"
)

// The add modal's whole point is that the focused field is already accepting text: a title and
// Enter is the shortest path through it, with no key to "open" the field first.
func TestAddModalTypesStraightIntoTitle(t *testing.T) {
	store := newFakeStore()
	h := newHarness(t, Deps{Tasks: store}, Settings{})
	h.key("right") // focus "working"

	h.key("a")
	if h.board.mode != modeAdd {
		t.Fatalf("mode = %v, want modeAdd", h.board.mode)
	}
	h.typeText("新規タスク")
	h.key("enter")

	file := store.snapshot()
	if len(file.Tasks) != 1 {
		t.Fatalf("tasks = %+v, want 1 件", file.Tasks)
	}
	if file.Tasks[0].Title != "新規タスク" || file.Tasks[0].Status != "working" {
		t.Errorf("task = %+v, want 新規タスク/working", file.Tasks[0])
	}
	if h.board.mode != modeBoard {
		t.Errorf("mode = %v, want modeBoard", h.board.mode)
	}
}

// Enter creates from wherever the cursor is, not only from the title row.
func TestAddModalEnterCreatesFromAnyField(t *testing.T) {
	store := newFakeStore()
	h := newHarness(t, Deps{Tasks: store}, Settings{})

	h.key("a")
	h.typeText("タイトル")
	for i := 0; i < int(addLink); i++ {
		h.key("down")
	}
	if h.board.add.cursor != addLink {
		t.Fatalf("cursor = %v, want addLink", h.board.add.cursor)
	}
	h.key("enter")

	if len(store.snapshot().Tasks) != 1 {
		t.Errorf("tasks = %+v, want 1 件", store.snapshot().Tasks)
	}
}

func TestAddModalStatusRowSwitchesWithArrows(t *testing.T) {
	store := newFakeStore()
	h := newHarness(t, Deps{Tasks: store}, Settings{})
	h.key("right") // focus "working"

	h.key("a")
	if h.board.add.status != "working" {
		t.Fatalf("既定ステータス = %q, want working（フォーカス列）", h.board.add.status)
	}
	h.key("down") // onto the status row
	h.key("left")
	if h.board.add.status != "todo" {
		t.Fatalf("← 後 = %q, want todo", h.board.add.status)
	}
	h.key("right")
	h.key("right")
	if h.board.add.status != "done" {
		t.Fatalf("→→ 後 = %q, want done（terminal 列も選べる）", h.board.add.status)
	}

	h.key("up")
	h.typeText("完了済み")
	h.key("enter")

	if got := store.snapshot().Tasks[0].Status; got != "done" {
		t.Errorf("status = %q, want done", got)
	}
}

// Moving between rows must not throw away what was typed in the ones left behind.
func TestAddModalKeepsValuesAcrossFieldMoves(t *testing.T) {
	store := newFakeStore()
	h := newHarness(t, Deps{Tasks: store}, Settings{})

	h.key("a")
	h.typeText("題名")
	h.key("down") // status
	h.key("down") // due
	h.typeText("2026-09-30")
	h.key("down") // note
	h.typeText("メモ")
	h.key("up")
	h.key("up")
	h.key("up")
	if got := h.board.add.value(addTitle); got != "題名" {
		t.Fatalf("タイトル = %q, want 題名", got)
	}
	h.key("enter")

	created := store.snapshot().Tasks[0]
	if created.Title != "題名" || created.Note != "メモ" {
		t.Fatalf("task = %+v, want 題名/メモ", created)
	}
	if created.Due == nil || string(*created.Due) != "2026-09-30" {
		t.Errorf("due = %v, want 2026-09-30", created.Due)
	}
}

func TestAddModalCancels(t *testing.T) {
	store := newFakeStore()
	h := newHarness(t, Deps{Tasks: store}, Settings{})

	h.key("a")
	h.typeText("捨てる")
	h.key("esc")

	if len(store.snapshot().Tasks) != 0 {
		t.Error("esc で取り消したのにタスクが作られた")
	}
	if h.board.mode != modeBoard {
		t.Errorf("mode = %v, want modeBoard", h.board.mode)
	}
}

// An empty title is the one thing that cannot be fixed after the fact, so the modal stays open
// with the error rather than closing on a task that was never created.
func TestAddModalEmptyTitleStaysOpen(t *testing.T) {
	store := newFakeStore()
	h := newHarness(t, Deps{Tasks: store}, Settings{})

	h.key("a")
	h.key("enter")

	if h.board.mode != modeAdd {
		t.Fatalf("mode = %v, want modeAdd のまま", h.board.mode)
	}
	if h.board.add.err == "" {
		t.Error("エラーが表示されていない")
	}
	if len(store.snapshot().Tasks) != 0 {
		t.Error("タイトル無しでタスクが作られた")
	}
}

// A multi-line paste is a list of tasks: the other fields apply to every line.
func TestAddModalMultiLinePasteCreatesOneTaskPerLine(t *testing.T) {
	store := newFakeStore()
	h := newHarness(t, Deps{Tasks: store}, Settings{})

	h.key("a")
	h.paste("設計する\n実装する\n\nレビューする\n")
	h.key("down")
	h.key("down") // due
	h.typeText("2026-09-30")
	h.key("enter")

	tasks := store.snapshot().Tasks
	if len(tasks) != 3 {
		t.Fatalf("tasks = %+v, want 3 件", tasks)
	}
	for i, want := range []string{"設計する", "実装する", "レビューする"} {
		if tasks[i].Title != want {
			t.Errorf("tasks[%d].Title = %q, want %q", i, tasks[i].Title, want)
		}
		if tasks[i].Due == nil || string(*tasks[i].Due) != "2026-09-30" {
			t.Errorf("tasks[%d].Due = %v, want 全行に適用", i, tasks[i].Due)
		}
	}
}

// A single-line paste is just text: it goes into the field like typing does.
func TestAddModalSingleLinePasteIsPlainText(t *testing.T) {
	store := newFakeStore()
	h := newHarness(t, Deps{Tasks: store}, Settings{})

	h.key("a")
	h.paste("貼り付けたタイトル")
	h.key("enter")

	tasks := store.snapshot().Tasks
	if len(tasks) != 1 || tasks[0].Title != "貼り付けたタイトル" {
		t.Fatalf("tasks = %+v, want 貼り付けたタイトル 1 件", tasks)
	}
}

// The URL a user pastes is the whole reason bracketed paste had to be routed at all.
func TestAddModalPasteIntoLinkField(t *testing.T) {
	store := newFakeStore()
	links := &fakeLinks{}
	h := newHarness(t, Deps{Tasks: store, Links: links, Cache: &fakeCache{}}, Settings{
		Classifier: model.URLClassifier{},
	})

	h.key("a")
	h.typeText("PR を追う")
	for i := 0; i < int(addLink); i++ {
		h.key("down")
	}
	h.paste("https://github.com/o/r/pull/7 https://github.com/o/r/issues/8")
	h.key("enter")

	task := store.snapshot().Tasks[0]
	if len(task.Links) != 2 {
		t.Fatalf("links = %+v, want 2 件", task.Links)
	}
	if task.Links[0].Kind != model.LinkKindGitHubPR || task.Links[1].Kind != model.LinkKindGitHubIssue {
		t.Errorf("kinds = %q/%q, want github_pr/github_issue", task.Links[0].Kind, task.Links[1].Kind)
	}
}

func TestAddModalRejectsBadLink(t *testing.T) {
	store := newFakeStore()
	h := newHarness(t, Deps{Tasks: store}, Settings{})

	h.key("a")
	h.typeText("t")
	for i := 0; i < int(addLink); i++ {
		h.key("down")
	}
	h.typeText("https://github.com/o/r/pull/1 github.com/o/r")
	h.key("enter")

	if h.board.mode != modeAdd {
		t.Fatalf("mode = %v, want modeAdd のまま", h.board.mode)
	}
	if len(store.snapshot().Tasks) != 0 {
		t.Error("不正な URL を含む入力で一部だけ登録された")
	}
	if !strings.Contains(h.board.add.err, "スキーム") {
		t.Errorf("err = %q, want スキームの案内", h.board.add.err)
	}
}

func TestAddModalRejectsBadDue(t *testing.T) {
	store := newFakeStore()
	h := newHarness(t, Deps{Tasks: store}, Settings{})

	h.key("a")
	h.typeText("t")
	h.key("down")
	h.key("down")
	h.typeText("2026/09/30")
	h.key("enter")

	if h.board.mode != modeAdd {
		t.Fatalf("mode = %v, want modeAdd のまま", h.board.mode)
	}
	if len(store.snapshot().Tasks) != 0 {
		t.Error("不正な日付でタスクが作られた")
	}
}

// A new task cannot be created with a status that does not exist, so the modal falls back to a
// real column when the cursor is parked on (unknown).
func TestAddModalFromUnknownColumnUsesRealColumn(t *testing.T) {
	store := newFakeStore(task(1, "retired"))
	h := newHarness(t, Deps{Tasks: store}, Settings{})
	h.board.colIdx = len(h.board.columns) - 1

	h.key("a")
	h.typeText("x")
	h.key("enter")

	created := store.snapshot().Tasks[1]
	if created.Status != "todo" {
		t.Errorf("status = %q, want todo", created.Status)
	}
}

func TestAddModalRendersFields(t *testing.T) {
	h := newHarness(t, Deps{Tasks: newFakeStore()}, Settings{})

	h.key("a")
	h.paste("一行目\n二行目")
	view := h.board.render()

	for _, want := range []string{"新しいタスク", "タイトル", "ステータス", "期限", "note", "リンク", "2 件のタスク"} {
		if !strings.Contains(view, want) {
			t.Errorf("描画に %q が無い:\n%s", want, view)
		}
	}
}
