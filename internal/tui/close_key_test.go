package tui

import (
	"testing"

	"github.com/ukwhatn/taskherd/internal/model"
)

// The board has one rule for getting out of things: q closes a screen, Esc leaves a text field.
// Which of the two a given screen answers to follows from whether it holds an input at all, so
// these cases are written as one table rather than scattered across each screen's own file —
// the point being tested is that they agree with each other.
//
// The screens with inputs (add, launch modal, the detail modal's edit mode) keep Esc because q is
// a character there; each is covered in its own file, where the typing behaviour it has to
// coexist with also lives.

// closeKeyCase is one screen reachable from the board, and how to get to it.
type closeKeyCase struct {
	name string
	open func(t *testing.T) *harness
	// back is where the screen returns to once closed.
	back mode
}

func inputlessScreens() []closeKeyCase {
	return []closeKeyCase{
		{
			name: "detail",
			open: func(t *testing.T) *harness {
				h := newHarness(t, Deps{Tasks: newFakeStore(task(1, "todo"))}, Settings{})
				h.key("enter")
				return h
			},
			back: modeBoard,
		},
		{
			name: "status-select",
			open: func(t *testing.T) *harness {
				h := newHarness(t, Deps{Tasks: newFakeStore(task(1, "todo"))}, Settings{})
				h.key("tab")
				return h
			},
			back: modeBoard,
		},
		{
			name: "session-select",
			open: func(t *testing.T) *harness {
				h := newHarness(t, Deps{Tasks: newFakeStore(task(1, "todo")), Herdr: &fakeHerdr{}}, Settings{})
				h.key("enter")
				focusDetailItem(t, h, itemAddSession, "")
				h.key("enter")
				return h
			},
			back: modeDetail,
		},
		{
			name: "jump",
			open: func(t *testing.T) *harness {
				sessions := []model.SessionRef{
					{Agent: "claude", SessionID: "s-1", Cwd: "/tmp/a"},
					{Agent: "claude", SessionID: "s-2", Cwd: "/tmp/b"},
				}
				h := newHarness(t, Deps{
					Tasks: newFakeStore(model.Task{ID: 1, Title: "t", Status: "todo", Sessions: sessions}),
					Herdr: &fakeHerdr{},
				}, Settings{})
				h.key("g")
				return h
			},
			back: modeBoard,
		},
		{
			name: "confirm",
			open: func(t *testing.T) *harness {
				h := newHarness(t, Deps{Tasks: newFakeStore(task(1, "todo"))}, Settings{})
				h.key("delete")
				return h
			},
			back: modeBoard,
		},
	}
}

func TestInputlessScreensCloseOnQ(t *testing.T) {
	for _, tc := range inputlessScreens() {
		t.Run(tc.name, func(t *testing.T) {
			h := tc.open(t)
			opened := h.board.mode
			if opened == tc.back {
				t.Fatalf("前提: 画面が開いていない（mode = %v）", opened)
			}

			h.key("q")

			if h.board.mode != tc.back {
				t.Errorf("mode = %v, want %v（q で閉じる）", h.board.mode, tc.back)
			}
			if h.quit {
				t.Error("q で board ごと終了した、want その画面だけ閉じる")
			}
		})
	}
}

// Esc is not an alias for q on these screens. Keeping both would leave the board with screens that
// close on either key and screens that close on only one, which is the mixture q exists to end.
func TestInputlessScreensIgnoreEsc(t *testing.T) {
	for _, tc := range inputlessScreens() {
		t.Run(tc.name, func(t *testing.T) {
			h := tc.open(t)
			opened := h.board.mode
			if opened == tc.back {
				t.Fatalf("前提: 画面が開いていない（mode = %v）", opened)
			}

			h.key("esc")

			if h.board.mode != opened {
				t.Errorf("mode = %v, want %v のまま（esc では閉じない）", h.board.mode, opened)
			}
		})
	}
}
