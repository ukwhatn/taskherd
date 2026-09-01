package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/ukwhatn/taskherd/internal/herdrc"
	"github.com/ukwhatn/taskherd/internal/model"
)

var pickerNow = time.Date(2026, 8, 24, 16, 0, 0, 0, time.UTC)

type fakePickerHerdr struct {
	agents      map[string]herdrc.Agent // keyed by pane id
	snapshotErr error
	tokens      []struct {
		paneID string
		taskID int
		title  string
	}
}

func (f *fakePickerHerdr) Snapshot(context.Context) (*herdrc.Snapshot, error) {
	if f.snapshotErr != nil {
		return nil, f.snapshotErr
	}
	agents := make([]herdrc.Agent, 0, len(f.agents))
	for _, a := range f.agents {
		agents = append(agents, a)
	}
	return &herdrc.Snapshot{Agents: agents}, nil
}

func (f *fakePickerHerdr) ReportTaskDisplay(_ context.Context, paneID string, taskID int, title string) error {
	f.tokens = append(f.tokens, struct {
		paneID string
		taskID int
		title  string
	}{paneID, taskID, title})
	return nil
}

func agentWithSession(paneID, sessionID, cwd string) herdrc.Agent {
	return herdrc.Agent{
		PaneID:  paneID,
		Agent:   "claude",
		Cwd:     cwd,
		Session: &herdrc.AgentSession{Agent: "claude", Kind: "id", Value: sessionID},
	}
}

// run drives cmd synchronously and feeds the message (and any batched messages) back into p,
// mirroring the board harness's run() without needing a full terminal.
func runPickerCmd(t *testing.T, p *picker, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	msg := cmd()
	switch typed := msg.(type) {
	case nil:
		return
	case tea.BatchMsg:
		for _, inner := range typed {
			runPickerCmd(t, p, inner)
		}
	case tea.QuitMsg:
		return
	default:
		_, next := p.Update(msg)
		runPickerCmd(t, p, next)
	}
}

func newTestPicker(deps PickerDeps, targetPane string) *picker {
	if deps.Now == nil {
		deps.Now = func() time.Time { return pickerNow }
	}
	if deps.Columns == nil {
		deps.Columns = model.DefaultColumns()
	}
	return newPicker(context.Background(), deps, targetPane)
}

func TestPickerLoadsAndFiltersTasks(t *testing.T) {
	store := newFakeStore(
		model.Task{ID: 1, Title: "設計", Status: "todo"},
		model.Task{ID: 2, Title: "実装", Status: "working"},
	)
	p := newTestPicker(PickerDeps{Tasks: store}, "wS:p1")

	runPickerCmd(t, p, p.Init())
	if !p.loaded || len(p.tasks) != 2 {
		t.Fatalf("loaded = %v, tasks = %+v", p.loaded, p.tasks)
	}
	if len(p.filtered) != 2 {
		t.Fatalf("filtered = %v, want 全件", p.filtered)
	}

	p.filter.SetValue("実装")
	p.applyFilter()
	if len(p.filtered) != 1 {
		t.Fatalf("filtered = %v, want 1 件", p.filtered)
	}
	got, ok := p.selected()
	if !ok || got.ID != 2 {
		t.Errorf("selected = %+v, want #2", got)
	}
}

func TestPickerLinksSelectedTaskToTargetPane(t *testing.T) {
	store := newFakeStore(model.Task{ID: 1, Title: "設計", Status: "todo"})
	herdr := &fakePickerHerdr{agents: map[string]herdrc.Agent{
		"wS:p1": agentWithSession("wS:p1", "sess-1", "/repo"),
	}}
	p := newTestPicker(PickerDeps{Tasks: store, Herdr: herdr}, "wS:p1")
	runPickerCmd(t, p, p.Init())

	runPickerCmd(t, p, p.linkSelectedCmd())

	if !p.linked {
		t.Fatalf("linked = false, status = %q", p.status)
	}
	file := store.snapshot()
	if len(file.Tasks[0].Sessions) != 1 || file.Tasks[0].Sessions[0].SessionID != "sess-1" {
		t.Errorf("sessions = %+v", file.Tasks[0].Sessions)
	}
	if len(herdr.tokens) != 1 || herdr.tokens[0].taskID != 1 || herdr.tokens[0].title != "設計" {
		t.Errorf("tokens = %+v, want #1/設計 の刻印", herdr.tokens)
	}
}

func TestPickerShowsErrorWhenPaneHasNoAgent(t *testing.T) {
	store := newFakeStore(model.Task{ID: 1, Title: "設計", Status: "todo"})
	herdr := &fakePickerHerdr{agents: map[string]herdrc.Agent{}}
	p := newTestPicker(PickerDeps{Tasks: store, Herdr: herdr}, "wS:p9")
	runPickerCmd(t, p, p.Init())

	runPickerCmd(t, p, p.linkSelectedCmd())

	if p.linked {
		t.Fatal("linked = true, want false（エージェント未検出）")
	}
	if !p.isError || p.status == "" {
		t.Errorf("status = %q, isError = %v, want エラー表示", p.status, p.isError)
	}
	if file := store.snapshot(); len(file.Tasks[0].Sessions) != 0 {
		t.Errorf("sessions = %+v, want 変更なし", file.Tasks[0].Sessions)
	}
}

func TestPickerShowsErrorWhenHerdrUnreachable(t *testing.T) {
	store := newFakeStore(model.Task{ID: 1, Title: "設計", Status: "todo"})
	herdr := &fakePickerHerdr{snapshotErr: errors.New("接続できない")}
	p := newTestPicker(PickerDeps{Tasks: store, Herdr: herdr}, "wS:p1")
	runPickerCmd(t, p, p.Init())

	runPickerCmd(t, p, p.linkSelectedCmd())

	if p.linked || !p.isError {
		t.Errorf("linked = %v, isError = %v, want 失敗をエラー表示", p.linked, p.isError)
	}
}

// manyPickerTasks is a list long enough that the popup cannot show all of it, with titles wide
// enough that an untrimmed row overflows the popup on its own.
func manyPickerTasks(n int) []model.Task {
	tasks := make([]model.Task, 0, n)
	for i := 1; i <= n; i++ {
		tasks = append(tasks, model.Task{
			ID:     i,
			Status: "todo",
			Title:  fmt.Sprintf("タスク %d 折り返しの原因になるくらい長い日本語のタイトル", i),
		})
	}
	return tasks
}

func pickerViewLines(p *picker) []string {
	return strings.Split(p.render(), "\n")
}

func TestPickerViewFitsThePopup(t *testing.T) {
	sizes := []struct{ width, height int }{
		{48, 14}, // the popup a 80x24 terminal gives a 60% x 60% entrypoint
		{48, 7},  // one row of list left over the fixed rows
		{48, 6},  // exactly the fixed rows
		{48, 3},  // less than the fixed rows
		{20, 10}, // narrow enough that every title is cut
	}
	cursors := []int{0, 38, 76}

	for _, size := range sizes {
		for _, cursor := range cursors {
			p := newTestPicker(PickerDeps{Tasks: newFakeStore(manyPickerTasks(77)...)}, "wS:p1")
			runPickerCmd(t, p, p.Init())
			p.Update(tea.WindowSizeMsg{Width: size.width, Height: size.height})
			p.cursor = cursor

			lines := pickerViewLines(p)
			if len(lines) > size.height {
				t.Errorf("%dx%d cursor=%d: %d 行あり端末高を超えた", size.width, size.height, cursor, len(lines))
			}
			for i, line := range lines {
				if w := lipgloss.Width(line); w > size.width {
					t.Errorf("%dx%d cursor=%d: %d 行目が %d セル幅で端末幅を超えた", size.width, size.height, cursor, i+1, w)
				}
			}
		}
	}
}

func TestPickerViewKeepsTheCursorVisible(t *testing.T) {
	p := newTestPicker(PickerDeps{Tasks: newFakeStore(manyPickerTasks(77)...)}, "wS:p1")
	runPickerCmd(t, p, p.Init())
	p.Update(tea.WindowSizeMsg{Width: 60, Height: 14})

	for _, cursor := range []int{0, 20, 76} {
		p.cursor = cursor
		view := p.render()
		marker := fmt.Sprintf("#%-4d", p.tasks[p.filtered[cursor]].ID)
		if !strings.Contains(view, marker) {
			t.Errorf("cursor=%d: 選択行 %q が描画されていない", cursor, marker)
		}
	}
}

// The filter is a textinput, so it is sized through the model rather than trimmed after the fact:
// trimming its value would hide the cursor, and trimming its View would cut an escape sequence.
func TestPickerFilterStaysInsideTheWidth(t *testing.T) {
	p := newTestPicker(PickerDeps{Tasks: newFakeStore(manyPickerTasks(3)...)}, "wS:p1")
	runPickerCmd(t, p, p.Init())
	p.Update(tea.WindowSizeMsg{Width: 30, Height: 14})
	p.filter.SetValue(strings.Repeat("絞", 40))
	p.applyFilter()

	for i, line := range pickerViewLines(p) {
		if w := lipgloss.Width(line); w > 30 {
			t.Errorf("%d 行目が %d セル幅で端末幅を超えた", i+1, w)
		}
	}
}

func TestPickerEscQuits(t *testing.T) {
	store := newFakeStore(model.Task{ID: 1, Title: "設計", Status: "todo"})
	p := newTestPicker(PickerDeps{Tasks: store}, "wS:p1")
	runPickerCmd(t, p, p.Init())

	_, cmd := p.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd == nil {
		t.Fatal("esc で終了コマンドが返らない")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Error("esc の結果が tea.Quit でない")
	}
}
