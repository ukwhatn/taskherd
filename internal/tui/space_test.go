package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/ukwhatn/taskherd/internal/herdrc"
	"github.com/ukwhatn/taskherd/internal/model"
)

func spacesSnapshot() *herdrc.Snapshot {
	return &herdrc.Snapshot{
		FocusedWorkspaceID: "wG",
		Workspaces: []herdrc.Workspace{
			{WorkspaceID: "wS", Label: "作業", Number: 1},
			{WorkspaceID: "wG", Label: "調査", Number: 2, Focused: true},
		},
	}
}

func TestSpaceSelectOpensOnTheFocusedSpace(t *testing.T) {
	state := newSpaceSelect(spacesSnapshot())

	if !state.available() {
		t.Fatal("space があるのに selector が無効")
	}
	if state.cursor != 1 {
		t.Errorf("cursor = %d, want 1（focused な space）", state.cursor)
	}
}

// Leaving the selection where it opened means "wherever herdr puts it", which is what keeps a
// leftover pane in another space recoverable.
func TestSpaceSelectSendsNothingUntilTheUserPicks(t *testing.T) {
	state := newSpaceSelect(spacesSnapshot())

	if got := state.choice(); got != (SpaceChoice{}) {
		t.Errorf("choice = %+v, want 空（未選択）", got)
	}

	state.move(-1)
	if got := state.choice(); got.WorkspaceID != "wS" {
		t.Errorf("choice = %+v, want wS", got)
	}
}

func TestSpaceSelectCreatesASpaceOnTheLastRow(t *testing.T) {
	state := newSpaceSelect(spacesSnapshot())
	state.move(1) // wG -> the create row

	if !state.creating() {
		t.Fatalf("cursor = %d, want 作成行", state.cursor)
	}
	state.newLabel.SetValue("  調査用  ")
	got := state.choice()
	if !got.Create || got.Label != "調査用" {
		t.Errorf("choice = %+v, want 作成 + トリム済みラベル", got)
	}
}

func TestSpaceSelectStopsAtTheEnds(t *testing.T) {
	state := newSpaceSelect(spacesSnapshot())
	for range 5 {
		state.move(1)
	}
	if state.cursor != 2 {
		t.Errorf("cursor = %d, want 2（作成行で止まる）", state.cursor)
	}
	for range 5 {
		state.move(-1)
	}
	if state.cursor != 0 {
		t.Errorf("cursor = %d, want 0", state.cursor)
	}
}

// herdr may be unreachable, or old enough not to report spaces at all. Either way the row is not
// drawn and the launch goes wherever herdr would have put it.
func TestSpaceSelectIsUnavailableWithoutASnapshot(t *testing.T) {
	for name, snapshot := range map[string]*herdrc.Snapshot{
		"snapshot なし": nil,
		"space が空":    {},
	} {
		t.Run(name, func(t *testing.T) {
			state := newSpaceSelect(snapshot)
			if state.available() || state.creating() {
				t.Errorf("available = %v, creating = %v, want どちらも false", state.available(), state.creating())
			}
			if got := state.choice(); got != (SpaceChoice{}) {
				t.Errorf("choice = %+v, want 空", got)
			}
		})
	}
}

func startModalHarness(t *testing.T, snapshot *herdrc.Snapshot) (*harness, *fakeLauncher) {
	t.Helper()
	store := newFakeStore(
		model.Task{ID: 1, Title: "既存", Status: "todo", Sessions: []model.SessionRef{
			{Agent: "claude", SessionID: "s-old", Cwd: "/repo/work"},
		}},
		model.Task{ID: 2, Title: "起動対象", Status: "todo"},
	)
	launcher := &fakeLauncher{}
	h := newHarness(t, Deps{
		Tasks:    store,
		Sessions: newFakeSessions(t),
		Herdr:    &fakeHerdr{},
		Launcher: launcher,
	}, Settings{})
	if snapshot != nil {
		h.board.snapshot = snapshot
	}
	return h, launcher
}

// openStartModal moves the cursor onto the task with no session and opens the launch modal on it.
func openStartModal(t *testing.T, h *harness) {
	t.Helper()
	h.key("down")
	h.key("g")
	if h.board.mode != modeSessionStart {
		t.Fatalf("mode = %v, want modeSessionStart", h.board.mode)
	}
}

func TestStartModalSendsTheChosenSpace(t *testing.T) {
	h, launcher := startModalHarness(t, spacesSnapshot())
	openStartModal(t, h)

	h.key("up")   // cwd list -> space row
	h.key("left") // 調査 -> 作業
	h.key("enter")

	if len(launcher.starts) != 1 {
		t.Fatalf("starts = %+v, want 1 件", launcher.starts)
	}
	if got := launcher.starts[0].space; got.WorkspaceID != "wS" || got.Create {
		t.Errorf("space = %+v, want {WorkspaceID: wS}", got)
	}
}

func TestStartModalSendsANewSpaceWithItsLabel(t *testing.T) {
	h, launcher := startModalHarness(t, spacesSnapshot())
	openStartModal(t, h)

	h.key("up")
	h.key("right") // 調査 -> 作成行
	h.typeText("調査用")
	h.key("enter")

	if len(launcher.starts) != 1 {
		t.Fatalf("starts = %+v, want 1 件", launcher.starts)
	}
	if got := launcher.starts[0].space; !got.Create || got.Label != "調査用" {
		t.Errorf("space = %+v, want 作成 + ラベル", got)
	}
}

// The label field is a text field like every other one in the modals: a bracketed paste and a
// multi-character IME commit both have to land in it rather than being read as bindings.
func TestStartModalNewSpaceLabelTakesPasteAndIME(t *testing.T) {
	h, launcher := startModalHarness(t, spacesSnapshot())
	openStartModal(t, h)
	h.key("up")
	h.key("right")

	h.dispatch(tea.PasteMsg{Content: "貼り付け"})
	h.dispatch(tea.KeyPressMsg{Code: 'a', Text: "確定文字列"})
	h.key("enter")

	if len(launcher.starts) != 1 {
		t.Fatalf("starts = %+v, want 1 件", launcher.starts)
	}
	if got := launcher.starts[0].space.Label; got != "貼り付け確定文字列" {
		t.Errorf("label = %q, want 貼り付け確定文字列", got)
	}
}

// "esc" arriving as a committed IME string must not close the modal.
func TestStartModalNewSpaceLabelDoesNotTreatIMETextAsAKey(t *testing.T) {
	h, _ := startModalHarness(t, spacesSnapshot())
	openStartModal(t, h)
	h.key("up")
	h.key("right")

	h.dispatch(tea.KeyPressMsg{Code: 'e', Text: "esc"})

	if h.board.mode != modeSessionStart {
		t.Fatalf("mode = %v, want modeSessionStart（IME 確定で閉じない）", h.board.mode)
	}
}

func TestStartModalHidesTheSpaceRowWithoutASnapshot(t *testing.T) {
	h, launcher := startModalHarness(t, nil)
	openStartModal(t, h)

	if b := h.board; b.sessionStart.space.available() {
		t.Fatal("snapshot が無いのに space 行が有効")
	}
	// Up from the top cwd row has nowhere to go, so the section stays where it is.
	h.key("up")
	if h.board.sessionStart.focus != sessionStartFocusCwd {
		t.Errorf("focus = %v, want cwd のまま", h.board.sessionStart.focus)
	}

	h.key("enter")
	if len(launcher.starts) != 1 {
		t.Fatalf("starts = %+v, want 1 件", launcher.starts)
	}
	if got := launcher.starts[0].space; got != (SpaceChoice{}) {
		t.Errorf("space = %+v, want 空", got)
	}
}

func TestResumeModalSendsTheChosenSpace(t *testing.T) {
	store := newFakeStore(model.Task{
		ID: 1, Title: "resume 対象", Status: "todo",
		Sessions: []model.SessionRef{{Agent: "claude", SessionID: "s-gone", Cwd: "/repo/work"}},
	})
	launcher := &fakeLauncher{}
	h := newHarness(t, Deps{
		Tasks: store, Sessions: newFakeSessions(t), Herdr: &fakeHerdr{}, Launcher: launcher,
	}, Settings{})
	h.dispatch(snapshotUpdate())
	h.board.snapshot = spacesSnapshot()

	h.key("g")
	if h.board.mode != modeResumeStart {
		t.Fatalf("mode = %v, want modeResumeStart", h.board.mode)
	}
	h.key("right") // 調査 -> 作成行
	h.typeText("再開用")
	h.key("enter")

	if len(launcher.resumes) != 1 {
		t.Fatalf("resumes = %+v, want 1 件", launcher.resumes)
	}
	if got := launcher.resumes[0].space; !got.Create || got.Label != "再開用" {
		t.Errorf("space = %+v, want 作成 + ラベル", got)
	}
}

// The modal grows with the candidate list and the prompt; the three things the user is looking at
// have to survive that on the terminal sizes it actually runs in.
func TestStartModalKeepsCursorPromptAndHelpVisible(t *testing.T) {
	sessions := make([]model.SessionRef, 0, 8)
	for i := range 8 {
		sessions = append(sessions, model.SessionRef{
			Agent: "claude", SessionID: string(rune('a' + i)), Cwd: "/repo/" + strings.Repeat("d", i+1),
		})
	}
	store := newFakeStore(
		model.Task{ID: 1, Title: "履歴", Status: "todo", Sessions: sessions},
		model.Task{ID: 2, Title: "起動対象", Status: "todo"},
	)
	for _, size := range []struct{ width, height int }{{80, 24}, {80, 16}} {
		t.Run(fmt.Sprintf("%dx%d", size.width, size.height), func(t *testing.T) {
			h := newHarness(t, Deps{
				Tasks: store, Sessions: newFakeSessions(t), Herdr: &fakeHerdr{}, Launcher: &fakeLauncher{},
			}, Settings{})
			h.board.snapshot = spacesSnapshot()
			h.dispatch(tea.WindowSizeMsg{Width: size.width, Height: size.height})
			openStartModal(t, h)

			view := h.board.render()
			for _, want := range []string{
				ja.Start.LabelPrompt,
				"enter",              // the key help's own line
				h.board.icons.Cursor, // the row the cursor is on
			} {
				if !strings.Contains(view, want) {
					t.Errorf("%dx%d: %q が描画されていない\n%s", size.width, size.height, want, view)
				}
			}
			for i, line := range strings.Split(view, "\n") {
				if w := lipgloss.Width(line); w > size.width {
					t.Errorf("%dx%d: %d 行目が %d セル幅", size.width, size.height, i+1, w)
				}
			}
		})
	}
}
