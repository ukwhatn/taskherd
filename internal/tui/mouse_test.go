package tui

import (
	"maps"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/ukwhatn/taskherd/internal/model"
)

func TestMouseClickSelectsAndOpensDetail(t *testing.T) {
	h := newHarness(t, Deps{Tasks: newFakeStore(asciiTasks(2, "todo")...)}, Settings{})
	h.board.width, h.board.height = 80, 24
	h.board.render()

	region, ok := findRegion(h.board.cardRegions, 2)
	if !ok {
		t.Fatalf("#2 の矩形が見つからない: %+v", h.board.cardRegions)
	}

	h.dispatch(tea.MouseClickMsg{X: region.x, Y: region.y, Button: tea.MouseLeft})

	if h.board.mode != modeDetail {
		t.Fatalf("mode = %v, want modeDetail", h.board.mode)
	}
	if h.board.detail.taskID != 2 {
		t.Errorf("detail.taskID = %d, want 2（クリックしたカード）", h.board.detail.taskID)
	}
	if got := h.board.colIdx; got != 0 {
		t.Errorf("colIdx = %d, want 0", got)
	}
	if got := h.board.selected[h.board.columns[0].Key()]; got != 1 {
		t.Errorf("selected = %d, want 1（#2 の行）", got)
	}
}

func TestMouseClickOnBlankAreaDoesNothing(t *testing.T) {
	h := newHarness(t, Deps{Tasks: newFakeStore(task(1, "todo"))}, Settings{})
	h.board.width, h.board.height = 80, 24
	h.board.render()

	h.dispatch(tea.MouseClickMsg{X: h.board.width - 1, Y: h.board.height - 1, Button: tea.MouseLeft})

	if h.board.mode != modeBoard {
		t.Errorf("mode = %v, want modeBoard（何もない場所のクリック）", h.board.mode)
	}
}

func TestMouseClickIgnoresNonLeftButton(t *testing.T) {
	h := newHarness(t, Deps{Tasks: newFakeStore(task(1, "todo"))}, Settings{})
	h.board.width, h.board.height = 80, 24
	h.board.render()

	region, ok := findRegion(h.board.cardRegions, 1)
	if !ok {
		t.Fatalf("#1 の矩形が見つからない: %+v", h.board.cardRegions)
	}

	h.dispatch(tea.MouseClickMsg{X: region.x, Y: region.y, Button: tea.MouseRight})

	if h.board.mode != modeBoard {
		t.Errorf("mode = %v, want modeBoard（左クリック以外は無視）", h.board.mode)
	}
}

// Every mode besides modeBoard ignores both the click and the wheel, opened through the path that
// actually reaches it in the real program rather than by assigning b.mode directly: sessionSelect
// in particular only opens from inside the detail modal, and baseMode() would wrongly resolve every
// overlay opened from the board back to modeBoard, which is exactly the bug this gate exists to
// avoid (§2.5).
func TestMouseIgnoredOutsideBoardMode(t *testing.T) {
	tests := []struct {
		name string
		open func(t *testing.T) *harness
		want mode
	}{
		{
			name: "detail",
			open: func(t *testing.T) *harness {
				h := newHarness(t, Deps{Tasks: newFakeStore(task(1, "todo"), task(2, "todo"))}, Settings{})
				h.key("enter")
				return h
			},
			want: modeDetail,
		},
		{
			name: "add",
			open: func(t *testing.T) *harness {
				h := newHarness(t, Deps{Tasks: newFakeStore(task(1, "todo"), task(2, "todo"))}, Settings{})
				h.key("a")
				return h
			},
			want: modeAdd,
		},
		{
			name: "status-select",
			open: func(t *testing.T) *harness {
				h := newHarness(t, Deps{Tasks: newFakeStore(task(1, "todo"), task(2, "todo"))}, Settings{})
				h.key("tab")
				return h
			},
			want: modeStatusSelect,
		},
		{
			name: "session-select",
			open: func(t *testing.T) *harness {
				h := newHarness(t, Deps{Tasks: newFakeStore(task(1, "todo"), task(2, "todo")), Herdr: &fakeHerdr{}}, Settings{})
				h.key("enter")
				focusDetailItem(t, h, itemAddSession, "")
				h.key("enter")
				return h
			},
			want: modeSessionSelect,
		},
		{
			name: "jump",
			open: func(t *testing.T) *harness {
				sessions := []model.SessionRef{
					{Agent: "claude", SessionID: "s-1", Cwd: "/tmp/a"},
					{Agent: "claude", SessionID: "s-2", Cwd: "/tmp/b"},
				}
				h := newHarness(t, Deps{
					Tasks: newFakeStore(model.Task{ID: 1, Title: "t", Status: "todo", Sessions: sessions}, task(2, "todo")),
					Herdr: &fakeHerdr{},
				}, Settings{})
				h.key("g")
				return h
			},
			want: modeJump,
		},
		{
			name: "confirm",
			open: func(t *testing.T) *harness {
				h := newHarness(t, Deps{Tasks: newFakeStore(task(1, "todo"), task(2, "todo"))}, Settings{})
				h.key("delete")
				return h
			},
			want: modeConfirm,
		},
		{
			name: "session-start",
			open: func(t *testing.T) *harness {
				h := newHarness(t, Deps{Tasks: newFakeStore(task(1, "todo"), task(2, "todo")), Herdr: &fakeHerdr{}}, Settings{})
				h.key("g")
				return h
			},
			want: modeSessionStart,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := tc.open(t)
			if h.board.mode != tc.want {
				t.Fatalf("前提: mode = %v, want %v（到達経路が変わった？）", h.board.mode, tc.want)
			}

			// render() always draws the board underneath before splicing whatever is open on top of
			// it (render()'s own first line is renderBoard()), so this populates cardRegions with a
			// real rectangle regardless of mode. Clicking an arbitrary (5, 5) instead of a real
			// card's coordinates would make the gate untestable: hitCard on an empty cardRegions
			// always misses, so the click would do nothing whether or not the mode guard actually
			// worked, and reverting the guard to baseMode() would not be caught here.
			//
			// #2, not #1: every case above puts the cursor on #1 before opening its overlay, so a
			// click that leaked through onto #1's own region would leave colIdx/selected exactly
			// where they already were — indistinguishable from the guard actually holding. #2 makes
			// a leaked click move the cursor, which the checks below already look for.
			h.board.render()
			region, ok := findRegion(h.board.cardRegions, 2)
			if !ok {
				t.Fatalf("前提: #2 の矩形が見つからない（クリックが本当にカードへ当たるかを検証できない）: %+v", h.board.cardRegions)
			}

			colBefore := h.board.colIdx
			selBefore := maps.Clone(h.board.selected)

			h.dispatch(tea.MouseClickMsg{X: region.x, Y: region.y, Button: tea.MouseLeft})
			h.dispatch(tea.MouseWheelMsg{X: region.x, Y: region.y, Button: tea.MouseWheelDown})
			h.dispatch(tea.MouseWheelMsg{X: region.x, Y: region.y, Button: tea.MouseWheelRight})

			if h.board.mode != tc.want {
				t.Errorf("マウス操作後の mode = %v, want %v のまま", h.board.mode, tc.want)
			}
			if h.board.colIdx != colBefore {
				t.Errorf("colIdx が動いた: %d -> %d", colBefore, h.board.colIdx)
			}
			if !maps.Equal(h.board.selected, selBefore) {
				t.Errorf("selected が動いた: %v -> %v", selBefore, h.board.selected)
			}
		})
	}
}

// scroll delivers n wheel events in one direction, the way a real device does.
func scroll(h *harness, button tea.MouseButton, n int) {
	for i := 0; i < n; i++ {
		h.dispatch(tea.MouseWheelMsg{Button: button})
	}
}

func TestMouseWheelMatchesKeyMovement(t *testing.T) {
	newBoard := func() *harness {
		return newHarness(t, Deps{Tasks: newFakeStore(task(1, "todo"), task(2, "todo"), task(3, "working"))}, Settings{})
	}

	down := newBoard()
	scroll(down, tea.MouseWheelDown, wheelStepsPerMove)
	downKey := newBoard()
	downKey.key("down")
	if got, want := down.board.currentTask().ID, downKey.board.currentTask().ID; got != want {
		t.Errorf("wheel down の選択 = #%d, want #%d（key down と同じ）", got, want)
	}

	up := newBoard()
	scroll(up, tea.MouseWheelUp, wheelStepsPerMove)
	upKey := newBoard()
	upKey.key("up")
	if got, want := up.board.currentTask().ID, upKey.board.currentTask().ID; got != want {
		t.Errorf("wheel up の選択 = #%d, want #%d（key up と同じ）", got, want)
	}

	right := newBoard()
	scroll(right, tea.MouseWheelRight, wheelStepsPerMove)
	rightKey := newBoard()
	rightKey.key("right")
	if got, want := right.board.colIdx, rightKey.board.colIdx; got != want {
		t.Errorf("wheel right の colIdx = %d, want %d（key right と同じ）", got, want)
	}

	left := newBoard()
	left.key("right")
	scroll(left, tea.MouseWheelLeft, wheelStepsPerMove)
	leftKey := newBoard()
	leftKey.key("right")
	leftKey.key("left")
	if got, want := left.board.colIdx, leftKey.board.colIdx; got != want {
		t.Errorf("wheel left の colIdx = %d, want %d（key left と同じ）", got, want)
	}
}

func TestMouseWheelNeedsSeveralEventsPerStep(t *testing.T) {
	h := newHarness(t, Deps{Tasks: newFakeStore(task(1, "todo"), task(2, "todo"), task(3, "todo"))}, Settings{})
	first := h.board.currentTask().ID

	scroll(h, tea.MouseWheelDown, wheelStepsPerMove-1)
	if got := h.board.currentTask().ID; got != first {
		t.Fatalf("%d 件のスクロールで選択が #%d に動いた。%d 件までは動かないこと",
			wheelStepsPerMove-1, got, wheelStepsPerMove-1)
	}

	scroll(h, tea.MouseWheelDown, 1)
	if got := h.board.currentTask().ID; got == first {
		t.Fatalf("%d 件目のスクロールで選択が動いていない（#%d のまま）", wheelStepsPerMove, got)
	}
}

// A trackpad flick is not one axis: scrolling down emits the occasional lone left/right event
// mixed into the run. Measured on the real device, those strays are always single — so as long as
// one stray cannot reach the threshold on its own, the column stays put while the user reads down
// a column. This is the regression the accumulator exists for.
func TestMouseWheelIgnoresStrayHorizontalEventsWhileScrollingDown(t *testing.T) {
	h := newHarness(t, Deps{
		Tasks: newFakeStore(task(1, "todo"), task(2, "todo"), task(3, "todo"), task(4, "working")),
	}, Settings{})
	col := h.board.colIdx

	// The measured pattern: four down events, one stray right, then more down events.
	scroll(h, tea.MouseWheelDown, 4)
	scroll(h, tea.MouseWheelRight, 1)
	scroll(h, tea.MouseWheelDown, 6)

	if got := h.board.colIdx; got != col {
		t.Errorf("縦スクロール中に混ざった横イベント 1 件で列が %d → %d に動いた", col, got)
	}
}
