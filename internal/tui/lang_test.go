package tui

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/ukwhatn/taskherd/internal/i18n"
	"github.com/ukwhatn/taskherd/internal/model"
)

// japanese matches any kana or CJK ideograph, which is how these tests check that nothing was left
// behind as a literal: a missed string shows up as Japanese on an English board.
var japanese = regexp.MustCompile(`[\p{Hiragana}\p{Katakana}\p{Han}]`)

func TestBoardRendersInEnglish(t *testing.T) {
	store := newFakeStore(model.Task{
		ID: 1, Title: "design the thing", Status: "todo",
		Links: []model.Link{{URL: "https://github.com/acme/webapp/pull/7", Kind: model.LinkKindGitHubPR}},
	})
	h := newHarness(t, Deps{Tasks: store}, Settings{Text: i18n.For(i18n.LangEN)})

	view := stripANSI(h.board.render())
	if !strings.Contains(view, "q quit") {
		t.Errorf("英語のフッタが無い:\n%s", view)
	}
	if got := japanese.FindString(view); got != "" {
		t.Errorf("英語の board に日本語 %q が残っている:\n%s", got, view)
	}
}

// Every screen is checked, not only the board: a modal that kept its literals would otherwise only
// show up the first time someone opened it in English.
func TestEveryScreenRendersInEnglish(t *testing.T) {
	store := newFakeStore(model.Task{
		ID: 1, Title: "design the thing", Status: "todo",
		Note: "a note",
		// Two sessions, because g goes straight to the only one when there is just one and the
		// jump picker would never be drawn.
		Sessions: []model.SessionRef{
			{Agent: "claude", SessionID: "s-1", Cwd: "/repo/a"},
			{Agent: "claude", SessionID: "s-2", Cwd: "/repo/b"},
		},
	})
	sessions := newFakeSessions(t)
	h := newHarness(t, Deps{
		Tasks:    store,
		Sessions: sessions,
		Herdr:    &fakeHerdr{},
		Launcher: &fakeLauncher{},
	}, Settings{Text: i18n.For(i18n.LangEN)})
	h.dispatch(snapshotUpdate())

	for _, tc := range []struct {
		name string
		open func()
		// want is asserted before the view is read, so a subtest whose key sequence stopped
		// short fails instead of quietly checking the board it never left.
		want mode
	}{
		{"detail", func() { h.key("enter") }, modeDetail},
		{"add", func() { h.key("a") }, modeAdd},
		{"status select", func() { h.key("tab") }, modeStatusSelect},
		{"confirm", func() { h.key("delete") }, modeConfirm},
		{"jump", func() { h.key("g") }, modeJump},
		{"session select", func() {
			h.key("enter")
			h.board.detail.cursor = addSessionRow(t, h)
			h.key("enter")
		}, modeSessionSelect},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.open()
			if h.board.mode != tc.want {
				t.Fatalf("mode = %v, want %v", h.board.mode, tc.want)
			}
			view := stripANSI(h.board.render())
			if got := japanese.FindString(view); got != "" {
				t.Errorf("日本語 %q が残っている:\n%s", got, view)
			}
			h.board.mode, h.board.overlayBack = modeBoard, modeBoard
		})
	}
}

// addSessionRow finds the detail modal's "attach session" row by kind rather than by counting key
// presses, so adding a row to the modal does not silently turn the subtest into a no-op.
func addSessionRow(t *testing.T, h *harness) int {
	t.Helper()
	task := h.board.currentTask()
	if task == nil {
		t.Fatal("カードが選択されていない")
	}
	for i, item := range h.board.detailItems(*task) {
		if item.kind == itemAddSession {
			return i
		}
	}
	t.Fatal("セッション紐づけ行が見つからない")
	return 0
}

// A board built without a language still draws words. Settings.Text is optional, and a nil there
// used to be the sort of thing that renders as a screen full of blanks rather than as a failure.
func TestBoardWithoutCatalogFallsBackToDefault(t *testing.T) {
	store := newFakeStore(model.Task{ID: 1, Title: "t", Status: "todo"})
	h := newHarness(t, Deps{Tasks: store}, Settings{})

	if h.board.text != i18n.For(i18n.Default) {
		t.Fatalf("text = %p, want 既定のカタログ", h.board.text)
	}
	if view := stripANSI(h.board.render()); !strings.Contains(view, "q 終了") {
		t.Errorf("既定言語のフッタが無い:\n%s", view)
	}
}

// The status line goes through the catalog too, which is what a format-argument mistake would show
// up in first (as %!d(MISSING) rather than as a number).
func TestStatusLineIsFormattedFromCatalog(t *testing.T) {
	store := newFakeStore(model.Task{ID: 7, Title: "t", Status: "todo"})
	h := newHarness(t, Deps{Tasks: store}, Settings{Text: i18n.For(i18n.LangEN)})

	h.key("tab")
	h.key("enter")

	if want := fmt.Sprintf(en.Board.Moved, 7, "working"); h.board.status != want {
		t.Errorf("status = %q, want %q", h.board.status, want)
	}
}
