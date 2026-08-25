package tui

import (
	"context"
	"testing"
	"time"

	"github.com/ukwhatn/taskherd/internal/model"
)

// The very first View() bubbletea v2 renders can land before Init()'s tasksLoadedMsg arrives
// (§2.4): the board must still show itself, and the request must still be waiting for that first
// load rather than having been silently dropped. Built with New() directly rather than
// newHarness(), which reloads once in its own constructor — exactly the event this test has to
// observe the board's state ahead of.
func TestBoardDetailFromStartWaitsForFirstLoad(t *testing.T) {
	store := newFakeStore(task(1, "todo"))
	board := New(context.Background(), Deps{Tasks: store, Now: func() time.Time { return boardNow }},
		Settings{Columns: testColumns(), DetailTaskID: 1})
	board.width, board.height = 200, 30

	_ = board.View()

	if board.mode != modeBoard {
		t.Fatalf("mode = %v, want modeBoard（tasksLoadedMsg 到達前）", board.mode)
	}
	if board.pendingDetailTaskID != 1 {
		t.Fatalf("pendingDetailTaskID = %d, want 1（まだ消費されていない）", board.pendingDetailTaskID)
	}
}

// newHarness reloads once during construction, so by the time it returns the first (and, here,
// only) tasksLoadedMsg has already applied.
func TestBoardDetailFromStartOpensOnFirstSuccessfulLoad(t *testing.T) {
	store := newFakeStore(task(1, "todo"))
	h := newHarness(t, Deps{Tasks: store}, Settings{DetailTaskID: 1})

	if h.board.mode != modeDetail {
		t.Fatalf("mode = %v, want modeDetail", h.board.mode)
	}
	if h.board.detail.taskID != 1 {
		t.Errorf("detail.taskID = %d, want 1", h.board.detail.taskID)
	}
	if !h.board.detail.quitOnClose {
		t.Error("quitOnClose が立っていない")
	}
	if h.board.pendingDetailTaskID != 0 {
		t.Errorf("pendingDetailTaskID = %d, want 0（消費済み）", h.board.pendingDetailTaskID)
	}
}

// A transient load failure (msg.err != nil) must not spend the request: applyTasks returns before
// reaching the consumption logic, and the next successful load has to still open the detail.
func TestBoardDetailFromStartSurvivesLoadFailureThenOpensOnNextSuccess(t *testing.T) {
	store := newFakeStore(task(1, "todo"))
	board := New(context.Background(), Deps{Tasks: store, Now: func() time.Time { return boardNow }},
		Settings{Columns: testColumns(), DetailTaskID: 1})
	board.width, board.height = 200, 30
	h := &harness{t: t, board: board, store: store}

	h.dispatch(tasksLoadedMsg{err: errUnavailable})
	if h.board.mode != modeBoard {
		t.Fatalf("mode = %v, want modeBoard（読み込み失敗の時点）", h.board.mode)
	}
	if h.board.pendingDetailTaskID != 1 {
		t.Fatalf("pendingDetailTaskID = %d, want 1（失敗では消費しない）", h.board.pendingDetailTaskID)
	}

	h.reload()
	if h.board.mode != modeDetail || h.board.detail.taskID != 1 {
		t.Fatalf("mode=%v taskID=%d, want #1 の detail が開くこと（次の成功時）", h.board.mode, h.board.detail.taskID)
	}
	if h.board.pendingDetailTaskID != 0 {
		t.Errorf("pendingDetailTaskID = %d, want 0（消費済み）", h.board.pendingDetailTaskID)
	}
}

// A successful load that finds no such task consumes the request without opening anything, and a
// task with the same id showing up afterwards must not reopen it: the request was already spent.
func TestBoardDetailFromStartConsumedWithoutOpeningWhenTaskMissing(t *testing.T) {
	store := newFakeStore(task(2, "todo")) // no #1
	h := newHarness(t, Deps{Tasks: store}, Settings{DetailTaskID: 1})

	if h.board.mode != modeBoard {
		t.Fatalf("mode = %v, want modeBoard（対象タスクが無い）", h.board.mode)
	}
	if h.board.pendingDetailTaskID != 0 {
		t.Fatalf("pendingDetailTaskID = %d, want 0（消費済み）", h.board.pendingDetailTaskID)
	}

	if err := store.Update(nil, func(f *model.File) error {
		f.Tasks = append(f.Tasks, model.Task{ID: 1, Title: "reappeared", Status: "todo"})
		return nil
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	h.reload()

	if h.board.mode != modeBoard {
		t.Errorf("mode = %v, want modeBoard（後から現れた同 id では開かない）", h.board.mode)
	}
}
