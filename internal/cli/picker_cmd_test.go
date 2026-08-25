package cli

import (
	"context"
	"errors"
	"testing"

	"github.com/ukwhatn/taskherd/internal/herdrc"
	"github.com/ukwhatn/taskherd/internal/model"
)

// The two functions the picker/board branch actually decides with (resolvePaneSessionID,
// taskForSession) are tested directly, in an internal (package cli) test file, rather than through
// the picker command end to end: RunPicker and tui.Run both block on a real bubbletea program, so
// there is no way to run the full command past this branch in a test without hanging.

// fakePaneHerdr stands in for tui.PickerHerdrOps: only the Snapshot half of the interface matters
// to resolvePaneSessionID, so ReportTaskToken is a no-op.
type fakePaneHerdr struct {
	snapshot *herdrc.Snapshot
	err      error
}

func (f *fakePaneHerdr) Snapshot(context.Context) (*herdrc.Snapshot, error) {
	return f.snapshot, f.err
}

func (f *fakePaneHerdr) ReportTaskToken(context.Context, string, int) error { return nil }

func TestResolvePaneSessionID(t *testing.T) {
	tests := []struct {
		name       string
		herdr      *fakePaneHerdr
		targetPane string
		wantID     string
		wantOK     bool
	}{
		{
			name: "エージェントがセッションを持つ",
			herdr: &fakePaneHerdr{snapshot: &herdrc.Snapshot{Agents: []herdrc.Agent{
				{PaneID: "pane-1", Agent: "claude", Session: &herdrc.AgentSession{Value: "sess-1"}},
			}}},
			targetPane: "pane-1",
			wantID:     "sess-1",
			wantOK:     true,
		},
		{
			name:       "snapshot 取得に失敗する",
			herdr:      &fakePaneHerdr{err: errors.New("herdr に到達できない")},
			targetPane: "pane-1",
			wantOK:     false,
		},
		{
			name:       "pane にエージェントが検出されていない",
			herdr:      &fakePaneHerdr{snapshot: &herdrc.Snapshot{}},
			targetPane: "pane-1",
			wantOK:     false,
		},
		{
			name: "エージェントにセッション id が無い",
			herdr: &fakePaneHerdr{snapshot: &herdrc.Snapshot{Agents: []herdrc.Agent{
				{PaneID: "pane-1", Agent: "claude"},
			}}},
			targetPane: "pane-1",
			wantOK:     false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotID, gotOK := resolvePaneSessionID(context.Background(), tc.herdr, tc.targetPane)
			if gotOK != tc.wantOK {
				t.Fatalf("ok = %v, want %v", gotOK, tc.wantOK)
			}
			if gotOK && gotID != tc.wantID {
				t.Errorf("sessionID = %q, want %q", gotID, tc.wantID)
			}
		})
	}
}

func TestTaskForSession(t *testing.T) {
	tests := []struct {
		name    string
		tasks   []model.Task
		session string
		wantID  int
		wantOK  bool
	}{
		{
			name:    "突合あり",
			tasks:   []model.Task{{ID: 1, Sessions: []model.SessionRef{{SessionID: "sess-1"}}}},
			session: "sess-1",
			wantID:  1,
			wantOK:  true,
		},
		{
			name:    "突合なし",
			tasks:   []model.Task{{ID: 1, Sessions: []model.SessionRef{{SessionID: "sess-2"}}}},
			session: "sess-1",
			wantOK:  false,
		},
		{
			name:    "タスクにセッションが無い",
			tasks:   []model.Task{{ID: 1}},
			session: "sess-1",
			wantOK:  false,
		},
		{
			name: "同一セッションが id 降順で保存されていても最小 id を選ぶ",
			tasks: []model.Task{
				{ID: 5, Sessions: []model.SessionRef{{SessionID: "sess-1"}}},
				{ID: 2, Sessions: []model.SessionRef{{SessionID: "sess-1"}}},
				{ID: 9, Sessions: []model.SessionRef{{SessionID: "sess-1"}}},
			},
			session: "sess-1",
			wantID:  2,
			wantOK:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotID, gotOK := taskForSession(tc.tasks, tc.session)
			if gotOK != tc.wantOK {
				t.Fatalf("ok = %v, want %v", gotOK, tc.wantOK)
			}
			if gotOK && gotID != tc.wantID {
				t.Errorf("taskID = %d, want %d", gotID, tc.wantID)
			}
		})
	}
}
