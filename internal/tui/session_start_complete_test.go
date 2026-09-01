package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/ukwhatn/taskherd/internal/model"
)

// homeTree is the fake home directory the completion tests type paths into.
func homeTree() (string, map[string][]string) {
	return "/home/u", map[string][]string{
		"/home/u":              {"dev", "docs", "Desktop"},
		"/home/u/dev":          {"taskherd", "taskrunner", "herdr"},
		"/home/u/dev/taskherd": {},
	}
}

// Tab on the free-text row completes the path rather than moving to the next field: that row is
// where a path is typed, and completion is what tab means in a path field.
func TestBoardSessionStartTabCompletesTheTypedPath(t *testing.T) {
	store := newFakeStore(model.Task{ID: 1, Title: "t", Status: "todo"})
	h := newHarness(t, startDeps(store, &fakeHerdr{}, &fakeLauncher{}), Settings{})
	h.useFakePaths(homeTree())

	h.key("g")
	h.typeText("~/dev/task")
	h.key("tab")

	if got := h.board.sessionStart.cwdInput.Value(); got != "~/dev/task" {
		t.Errorf("cwdInput = %q, want ~/dev/task のまま（taskherd と taskrunner の共通部分より伸びない）", got)
	}

	h.typeText("h")
	h.key("tab")

	if got := h.board.sessionStart.cwdInput.Value(); got != "~/dev/taskherd/" {
		t.Errorf("cwdInput = %q, want ~/dev/taskherd/", got)
	}
	if h.board.sessionStart.focus != sessionStartFocusCwd {
		t.Errorf("focus = %v, want 補完しても cwd 欄に留まる", h.board.sessionStart.focus)
	}
}

// Completion writes to the end of the field: bubbles' SetValue leaves the cursor where it was, so
// without an explicit move the next character lands inside what was just completed.
func TestBoardSessionStartTypingAfterCompletionAppends(t *testing.T) {
	store := newFakeStore(model.Task{ID: 1, Title: "t", Status: "todo"})
	h := newHarness(t, startDeps(store, &fakeHerdr{}, &fakeLauncher{}), Settings{})
	h.useFakePaths(homeTree())

	h.key("g")
	h.typeText("~/dev")
	h.key("tab") // the lone match gains its separator: ~/dev/
	h.typeText("h")

	if got := h.board.sessionStart.cwdInput.Value(); got != "~/dev/h" {
		t.Errorf("cwdInput = %q, want ~/dev/h", got)
	}
}

// shift+tab still walks the sections from the free-text row, so tab having been taken for
// completion does not leave that row without a keyboard way out.
func TestBoardSessionStartShiftTabLeavesTheFreeTextRow(t *testing.T) {
	store := newFakeStore(model.Task{ID: 1, Title: "t", Status: "todo"})
	h := newHarness(t, startDeps(store, &fakeHerdr{}, &fakeLauncher{}), Settings{})
	h.useFakePaths(homeTree())

	h.key("g")
	h.key("shift+tab")

	if h.board.sessionStart.focus == sessionStartFocusCwd {
		t.Error("shift+tab で cwd 欄から抜けられない")
	}
}

// The suggestions are drawn while the free-text row has the keyboard, and the candidate list they
// displace collapses to a single row naming how many there are.
func TestBoardSessionStartShowsSuggestionsInsteadOfCandidatesWhileTyping(t *testing.T) {
	store := newFakeStore(
		model.Task{ID: 1, Title: "old", Status: "todo",
			Sessions: []model.SessionRef{{Agent: "claude", SessionID: "s-1", Cwd: "/repo/a", LinkedAt: "2026-08-20T10:00:00+09:00"}}},
		model.Task{ID: 2, Title: "new", Status: "todo"},
	)
	h := newHarness(t, startDeps(store, &fakeHerdr{}, &fakeLauncher{}), Settings{})
	h.useFakePaths(homeTree())
	h.key("down") // select #2

	h.key("g")
	if got := h.board.render(); !strings.Contains(got, "/repo/a") {
		t.Fatalf("候補行にいるのに履歴候補が出ていない:\n%s", got)
	}

	h.key("down") // candidate row -> free-text row
	h.typeText("~/dev/")

	got := h.board.render()
	if !strings.Contains(got, "~/dev/taskherd/") {
		t.Errorf("サジェストが出ていない:\n%s", got)
	}
	if strings.Contains(got, "/repo/a") {
		t.Errorf("入力中なのに候補一覧が残っている:\n%s", got)
	}
	if want := fmt.Sprintf(ja.Start.CwdHistory, 1, h.board.icons.ArrowUp); !strings.Contains(got, want) {
		t.Errorf("履歴の畳んだ行 %q が出ていない:\n%s", want, got)
	}
}

// The completion list says how many it is not showing, so a list that stops at the row budget does
// not read as "that is all there is".
func TestBoardSessionStartSuggestionsReportTheOnesTheyOmit(t *testing.T) {
	store := newFakeStore(model.Task{ID: 1, Title: "t", Status: "todo"})
	h := newHarness(t, startDeps(store, &fakeHerdr{}, &fakeLauncher{}), Settings{})
	h.useFakePaths("/home/u", map[string][]string{
		"/home/u": {"a1", "a2", "a3", "a4", "a5", "a6", "a7"},
	})

	h.key("g")
	h.typeText("~/a")

	if want := fmt.Sprintf(ja.Start.MoreSuggestions, 3); !strings.Contains(h.board.render(), want) {
		t.Errorf("省いた件数 %q が出ていない:\n%s", want, h.board.render())
	}
}

// A ~ typed by hand is resolved before the launch is handed off: the process that runs it has no
// reason to share this one's idea of home.
func TestBoardSessionStartExpandsHomeBeforeHandingOff(t *testing.T) {
	store := newFakeStore(model.Task{ID: 1, Title: "t", Status: "todo"})
	launcher := &fakeLauncher{}
	h := newHarness(t, startDeps(store, &fakeHerdr{}, launcher), Settings{})
	h.useFakePaths(homeTree())

	h.key("g")
	h.typeText("~/dev/taskherd")
	h.key("enter")

	if len(launcher.starts) != 1 {
		t.Fatalf("starts = %+v, want 1 件", launcher.starts)
	}
	if got := launcher.starts[0].cwd; got != "/home/u/dev/taskherd" {
		t.Errorf("cwd = %q, want /home/u/dev/taskherd", got)
	}
}

// manyCandidates is a task file whose ranked cwd list is longer than the modal can draw, so the
// height tests exercise the budget rather than a list that happens to fit.
func manyCandidates() *fakeStore {
	var sessions []model.SessionRef
	for i := range 8 {
		sessions = append(sessions, model.SessionRef{
			Agent:     "claude",
			SessionID: fmt.Sprintf("s-%d", i),
			Cwd:       fmt.Sprintf("/repo/%d", i),
			LinkedAt:  "2026-08-20T10:00:00+09:00",
		})
	}
	return newFakeStore(
		model.Task{ID: 1, Title: "old", Status: "todo", Sessions: sessions},
		model.Task{ID: 2, Title: "new", Status: "todo"},
	)
}

// assertModalIntact fails when the modal overran the terminal, which renderModal answers by
// cutting its own last lines — the key help among them.
func assertModalIntact(t *testing.T, h *harness, height int) {
	t.Helper()
	help := h.board.sessionStartHelp()
	box := h.board.renderSessionStart()
	if rows := len(strings.Split(box, "\n")); rows > height-2 {
		t.Errorf("モーダルが %d 行、want %d 行以内:\n%s", rows, height-2, box)
	}
	if !strings.Contains(box, help) {
		t.Errorf("キーヘルプ %q が打ち切られている:\n%s", help, box)
	}
}

// An 80x24 terminal is the floor the modal is designed to fit, with a candidate list selected.
func TestBoardSessionStartFitsASmallTerminalOnACandidateRow(t *testing.T) {
	h := newHarness(t, startDeps(manyCandidates(), &fakeHerdr{}, &fakeLauncher{}), Settings{})
	h.dispatch(tea.WindowSizeMsg{Width: 80, Height: 24})
	h.key("down") // select #2

	h.key("g")
	assertModalIntact(t, h, 24)
}

// The same floor while typing, where the suggestions take the rows the candidate list gave up.
func TestBoardSessionStartFitsASmallTerminalWhileTyping(t *testing.T) {
	h := newHarness(t, startDeps(manyCandidates(), &fakeHerdr{}, &fakeLauncher{}), Settings{})
	h.useFakePaths("/home/u", map[string][]string{"/home/u": {"a1", "a2", "a3", "a4", "a5", "a6", "a7"}})
	h.dispatch(tea.WindowSizeMsg{Width: 80, Height: 24})
	h.key("down") // select #2

	h.key("g")
	for range 8 { // walk past every candidate to the free-text row
		h.key("down")
	}
	h.typeText("~/a")

	assertModalIntact(t, h, 24)
}
