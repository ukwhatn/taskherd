package herdrc_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ukwhatn/taskherd/internal/herdrc"
)

const workspaceCreatedJSON = `{"id":"cli:workspace:create","result":{"type":"workspace_created",` +
	`"workspace":{"workspace_id":"wN","label":"新しい space"},` +
	`"tab":{"tab_id":"wN:t1","workspace_id":"wN"},` +
	`"root_pane":{"pane_id":"wN:p1","cwd":"/private/tmp/work"}}}`

const tabCreatedJSON = `{"id":"cli:tab:create","result":{"type":"tab_created",` +
	`"tab":{"tab_id":"wG:t9","workspace_id":"wG"},` +
	`"root_pane":{"pane_id":"wG:p9","cwd":"/private/tmp/work"}}}`

func TestCreateTabTargetsAWorkspaceWhenOneIsGiven(t *testing.T) {
	tests := []struct {
		name        string
		workspaceID string
		want        string
	}{
		{name: "指定あり", workspaceID: "wG", want: "tab create --workspace wG --cwd /tmp/work --label t"},
		{name: "指定なしは現在の space", workspaceID: "", want: "tab create --cwd /tmp/work --label t"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			runner := &fakeRunner{handler: func([]string) ([]byte, error) {
				return []byte(tabCreatedJSON), nil
			}}
			client := newClient(t, newFakeHerdr(t, snapshotJSON()), runner)

			if _, err := client.CreateTab(context.Background(),
				herdrc.TabSpec{WorkspaceID: tc.workspaceID, Cwd: "/tmp/work", Label: "t"}); err != nil {
				t.Fatalf("CreateTab: %v", err)
			}
			if got := strings.Join(runner.Calls()[0], " "); got != tc.want {
				t.Errorf("呼び出し = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCreateWorkspaceReturnsItsRootPane(t *testing.T) {
	runner := &fakeRunner{handler: func([]string) ([]byte, error) {
		return []byte(workspaceCreatedJSON), nil
	}}
	client := newClient(t, newFakeHerdr(t, snapshotJSON()), runner)

	tab, err := client.CreateWorkspace(context.Background(),
		herdrc.WorkspaceSpec{Cwd: "/tmp/work", Label: "新しい space", Focus: true})
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	if tab.WorkspaceID != "wN" || tab.TabID != "wN:t1" || tab.PaneID != "wN:p1" {
		t.Errorf("tab = %+v", tab)
	}
	want := "workspace create --cwd /tmp/work --label 新しい space --focus"
	if got := strings.Join(runner.Calls()[0], " "); got != want {
		t.Errorf("呼び出し = %q, want %q", got, want)
	}
}

// An unlabelled space is herdr's own to name, so the flag is left off entirely rather than sent
// empty: `--label ""` would set the label to the empty string.
func TestCreateWorkspaceOmitsAnEmptyLabel(t *testing.T) {
	runner := &fakeRunner{handler: func([]string) ([]byte, error) {
		return []byte(workspaceCreatedJSON), nil
	}}
	client := newClient(t, newFakeHerdr(t, snapshotJSON()), runner)

	if _, err := client.CreateWorkspace(context.Background(), herdrc.WorkspaceSpec{Cwd: "/tmp/work"}); err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	if got := strings.Join(runner.Calls()[0], " "); got != "workspace create --cwd /tmp/work" {
		t.Errorf("呼び出し = %q", got)
	}
}

func TestCreateWorkspaceReportsHerdrsOwnError(t *testing.T) {
	runner := &fakeRunner{handler: func([]string) ([]byte, error) {
		return []byte(`{"id":"cli:workspace:create","error":{"code":"trust_required","message":"folder is not trusted"}}`), nil
	}}
	client := newClient(t, newFakeHerdr(t, snapshotJSON()), runner)

	_, err := client.CreateWorkspace(context.Background(), herdrc.WorkspaceSpec{Cwd: "/tmp/work"})
	if err == nil {
		t.Fatal("エラー封筒が握り潰されている")
	}
	if !strings.Contains(err.Error(), "trust_required") {
		t.Errorf("err = %v, want herdr のコードを含む", err)
	}
}

func TestSnapshotDecodesWorkspaces(t *testing.T) {
	client := newClient(t, newFakeHerdr(t, snapshotJSON()), nil)

	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(snapshot.Workspaces) != 2 {
		t.Fatalf("workspaces = %+v, want 2 件", snapshot.Workspaces)
	}
	focused, ok := snapshot.FocusedWorkspace()
	if !ok || focused.WorkspaceID != "wS" || focused.Label != "作業" {
		t.Errorf("FocusedWorkspace = %+v, ok = %v", focused, ok)
	}
}

func TestFocusedWorkspaceIsAbsentWhenNoneIsMarked(t *testing.T) {
	snapshot := &herdrc.Snapshot{Workspaces: []herdrc.Workspace{{WorkspaceID: "wA"}}}
	if _, ok := snapshot.FocusedWorkspace(); ok {
		t.Error("focused = false の space が focused として返った")
	}
}

// The board reads its space list off the last snapshot, so a change the fingerprint cannot see is
// a change the launch modal keeps showing the old version of.
func TestFingerprintFollowsWorkspaceChanges(t *testing.T) {
	base := func() *herdrc.Snapshot {
		return &herdrc.Snapshot{Workspaces: []herdrc.Workspace{
			{WorkspaceID: "wA", Label: "das", Number: 1, Focused: true},
			{WorkspaceID: "wB", Label: "taskherd", Number: 2},
		}}
	}
	tests := []struct {
		name   string
		mutate func(*herdrc.Snapshot)
	}{
		{"改名", func(s *herdrc.Snapshot) { s.Workspaces[1].Label = "別名" }},
		{"並べ替え", func(s *herdrc.Snapshot) { s.Workspaces[0].Number, s.Workspaces[1].Number = 2, 1 }},
		{"作成", func(s *herdrc.Snapshot) {
			s.Workspaces = append(s.Workspaces, herdrc.Workspace{WorkspaceID: "wC", Label: "新規", Number: 3})
		}},
		{"閉じる", func(s *herdrc.Snapshot) { s.Workspaces = s.Workspaces[:1] }},
		{"フォーカス移動", func(s *herdrc.Snapshot) {
			s.Workspaces[0].Focused, s.Workspaces[1].Focused = false, true
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			before := base()
			after := base()
			tc.mutate(after)
			if before.Fingerprint() == after.Fingerprint() {
				t.Error("fingerprint が変わらないため board に届かない")
			}
		})
	}
}

func TestWatchSubscribesToWorkspaceEvents(t *testing.T) {
	fake := newFakeHerdr(t, snapshotJSON(agentJSON("wS:p1", "s-1", "idle", "/repo")))
	client := newClient(t, fake, nil)

	watcher := client.Watch(context.Background())
	defer watcher.Close()
	waitUpdate(t, watcher.Updates())

	deadline := time.Now().Add(5 * time.Second)
	var params []byte
	for time.Now().Before(deadline) {
		if subs := fake.Subscribes(); len(subs) > 0 {
			params = subs[0]
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if params == nil {
		t.Fatal("events.subscribe が届かない")
	}

	kinds, _ := parseSubscriptions(t, params)
	for _, want := range []string{
		"workspace.created", "workspace.renamed", "workspace.closed",
		"workspace.focused", "workspace.moved", "workspace.reordered",
	} {
		if !contains(kinds, want) {
			t.Errorf("購読に %q が無い: %v", want, kinds)
		}
	}
}
