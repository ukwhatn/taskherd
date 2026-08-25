package tui

import (
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
			colBefore := h.board.colIdx
			selBefore := cloneSelected(h.board.selected)

			h.dispatch(tea.MouseClickMsg{X: 5, Y: 5, Button: tea.MouseLeft})
			h.dispatch(tea.MouseWheelMsg{X: 5, Y: 5, Button: tea.MouseWheelDown})
			h.dispatch(tea.MouseWheelMsg{X: 5, Y: 5, Button: tea.MouseWheelRight})

			if h.board.mode != tc.want {
				t.Errorf("マウス操作後の mode = %v, want %v のまま", h.board.mode, tc.want)
			}
			if h.board.colIdx != colBefore {
				t.Errorf("colIdx が動いた: %d -> %d", colBefore, h.board.colIdx)
			}
			if !selectedEqual(h.board.selected, selBefore) {
				t.Errorf("selected が動いた: %v -> %v", selBefore, h.board.selected)
			}
		})
	}
}

func TestMouseWheelMatchesKeyMovement(t *testing.T) {
	newBoard := func() *harness {
		return newHarness(t, Deps{Tasks: newFakeStore(task(1, "todo"), task(2, "todo"), task(3, "working"))}, Settings{})
	}

	down := newBoard()
	down.dispatch(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	downKey := newBoard()
	downKey.key("down")
	if got, want := down.board.currentTask().ID, downKey.board.currentTask().ID; got != want {
		t.Errorf("wheel down の選択 = #%d, want #%d（key down と同じ）", got, want)
	}

	up := newBoard()
	up.dispatch(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	upKey := newBoard()
	upKey.key("up")
	if got, want := up.board.currentTask().ID, upKey.board.currentTask().ID; got != want {
		t.Errorf("wheel up の選択 = #%d, want #%d（key up と同じ）", got, want)
	}

	right := newBoard()
	right.dispatch(tea.MouseWheelMsg{Button: tea.MouseWheelRight})
	rightKey := newBoard()
	rightKey.key("right")
	if got, want := right.board.colIdx, rightKey.board.colIdx; got != want {
		t.Errorf("wheel right の colIdx = %d, want %d（key right と同じ）", got, want)
	}

	left := newBoard()
	left.key("right")
	left.dispatch(tea.MouseWheelMsg{Button: tea.MouseWheelLeft})
	leftKey := newBoard()
	leftKey.key("right")
	leftKey.key("left")
	if got, want := left.board.colIdx, leftKey.board.colIdx; got != want {
		t.Errorf("wheel left の colIdx = %d, want %d（key left と同じ）", got, want)
	}
}

func cloneSelected(m map[string]int) map[string]int {
	out := make(map[string]int, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func selectedEqual(a, b map[string]int) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}
