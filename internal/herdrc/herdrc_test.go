package herdrc_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ukwhatn/taskherd/internal/herdrc"
)

func envFunc(pairs map[string]string) func(string) string {
	return func(key string) string { return pairs[key] }
}

func TestResolveSocketPathFollowsHerdrOrder(t *testing.T) {
	home := "/home/u"
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{
			name: "HERDR_SOCKET_PATH が最優先",
			env:  map[string]string{"HOME": home, "HERDR_SOCKET_PATH": "/tmp/custom.sock", "HERDR_SESSION": "work"},
			want: "/tmp/custom.sock",
		},
		{
			name: "HERDR_SESSION は named session のパスになる",
			env:  map[string]string{"HOME": home, "HERDR_SESSION": "work"},
			want: filepath.Join(home, ".config", "herdr", "sessions", "work", "herdr.sock"),
		},
		{
			name: "既定は既定セッションの socket",
			env:  map[string]string{"HOME": home},
			want: filepath.Join(home, ".config", "herdr", "herdr.sock"),
		},
		{
			name: "XDG_CONFIG_HOME を尊重する",
			env:  map[string]string{"HOME": home, "XDG_CONFIG_HOME": "/xdg"},
			want: "/xdg/herdr/herdr.sock",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := herdrc.ResolveSocketPath(envFunc(tt.env)); got != tt.want {
				t.Errorf("ResolveSocketPath = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSnapshotDecodesAgentsAndSessions(t *testing.T) {
	fake := newFakeHerdr(t, snapshotJSON(
		agentJSON("wS:p1", "33274acc-7d02-494d-bb06-dd907cbb6d0a", "blocked", "/repo/a"),
		agentJSON("wD:p4", "", "idle", "/repo/b"),
	))
	client := newClient(t, fake, nil)

	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	if snapshot.Protocol != 20 || snapshot.Version != "0.8.2" {
		t.Errorf("version/protocol = %q / %d", snapshot.Version, snapshot.Protocol)
	}
	if snapshot.FocusedPaneID != "wS:p1" {
		t.Errorf("focused_pane_id = %q", snapshot.FocusedPaneID)
	}
	agent, ok := snapshot.AgentBySessionID("33274acc-7d02-494d-bb06-dd907cbb6d0a")
	if !ok {
		t.Fatal("session UUID から agent を引けない")
	}
	if agent.PaneID != "wS:p1" || agent.Cwd != "/repo/a" || agent.AgentStatus != "blocked" {
		t.Errorf("agent = %+v", agent)
	}
	if _, ok := snapshot.AgentBySessionID(""); ok {
		t.Error("空の session id が一致した")
	}
	// An agent without a session reference must not be reachable by session lookup.
	byPane, ok := snapshot.AgentByPaneID("wD:p4")
	if !ok || byPane.SessionID() != "" {
		t.Errorf("agent_session が null の agent = %+v (ok=%v)", byPane, ok)
	}
}

// Each request needs its own connection: the real server closes the connection after answering.
func TestSnapshotWorksAcrossSuccessiveCalls(t *testing.T) {
	fake := newFakeHerdr(t, snapshotJSON(agentJSON("wS:p1", "s-1", "idle", "/repo")))
	client := newClient(t, fake, nil)

	for i := 0; i < 3; i++ {
		if _, err := client.Snapshot(context.Background()); err != nil {
			t.Fatalf("%d 回目の Snapshot: %v", i+1, err)
		}
	}
}

func TestSnapshotUnreachableIsReportedAsUnavailable(t *testing.T) {
	client := herdrc.New(herdrc.Options{
		Getenv: envFunc(map[string]string{"HERDR_SOCKET_PATH": filepath.Join(t.TempDir(), "missing.sock")}),
		Runner: &fakeRunner{handler: func([]string) ([]byte, error) { return nil, errors.New("herdr が無い") }},
	})

	snapshot, status := client.Probe(context.Background())

	if status.Available || snapshot != nil {
		t.Fatalf("status = %+v, snapshot = %v, want 不達", status, snapshot)
	}
	var unavailable *herdrc.UnavailableError
	if !errors.As(status.Err, &unavailable) {
		t.Errorf("err = %v, want UnavailableError", status.Err)
	}
	if unavailable.Hint() == "" {
		t.Error("Hint が空（縮退の案内がない）")
	}
}

// The CLI resolves the socket by herdr's own rules, so it is tried when the derived path fails.
func TestSnapshotFallsBackToCLI(t *testing.T) {
	runner := &fakeRunner{handler: func(args []string) ([]byte, error) {
		if strings.Join(args, " ") != "api snapshot" {
			return nil, errors.New("想定外の呼び出し: " + strings.Join(args, " "))
		}
		body := `{"id":"cli:api:snapshot","result":{"type":"session_snapshot","snapshot":` +
			snapshotJSON(agentJSON("wX:p9", "s-cli", "working", "/repo")) + `}}`
		return []byte(body), nil
	}}
	client := herdrc.New(herdrc.Options{
		Getenv: envFunc(map[string]string{"HERDR_SOCKET_PATH": filepath.Join(t.TempDir(), "missing.sock")}),
		Runner: runner,
	})

	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if agent, ok := snapshot.AgentBySessionID("s-cli"); !ok || agent.PaneID != "wX:p9" {
		t.Errorf("CLI 経由の snapshot = %+v", snapshot.Agents)
	}
}

// The task id stamped by ReportTaskToken comes back on the pane's tokens map.
func TestSnapshotDecodesPaneTokens(t *testing.T) {
	body := `{"version":"0.8.2","protocol":20,"focused_workspace_id":"wS","focused_tab_id":"wS:t1",` +
		`"focused_pane_id":"wS:p1","agents":[],"panes":[` +
		`{"pane_id":"wS:p1","tab_id":"wS:t1","workspace_id":"wS","cwd":"/repo","focused":true,` +
		`"tokens":{"task":"12"}},` +
		`{"pane_id":"wS:p2","tab_id":"wS:t1","workspace_id":"wS","cwd":"/repo","focused":false,"tokens":null}]}`

	snapshot := parseSnapshot(t, body)

	if len(snapshot.Panes) != 2 {
		t.Fatalf("panes = %+v", snapshot.Panes)
	}
	if got := snapshot.Panes[0].Tokens["task"]; got != "12" {
		t.Errorf("tokens[task] = %q, want 12", got)
	}
	if snapshot.Panes[1].Tokens != nil {
		t.Errorf("tokens = %v, want nil（未設定）", snapshot.Panes[1].Tokens)
	}
}

func TestAggregateStatePrefersAttention(t *testing.T) {
	tests := []struct {
		name   string
		states []string
		want   string
	}{
		{name: "blocked が最優先", states: []string{"idle", "working", "blocked"}, want: herdrc.StateBlocked},
		{name: "working は done より優先", states: []string{"done", "working"}, want: herdrc.StateWorking},
		{name: "done は idle より優先", states: []string{"idle", "done"}, want: herdrc.StateDone},
		{name: "idle は unknown より優先", states: []string{"unknown", "idle"}, want: herdrc.StateIdle},
		{name: "offline のみなら offline", states: []string{herdrc.StateOffline}, want: herdrc.StateOffline},
		{name: "空なら offline", states: nil, want: herdrc.StateOffline},
		{name: "生存が 1 つでもあればそれを出す", states: []string{herdrc.StateOffline, "idle"}, want: herdrc.StateIdle},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := herdrc.AggregateState(tt.states); got != tt.want {
				t.Errorf("AggregateState(%v) = %q, want %q", tt.states, got, tt.want)
			}
		})
	}
}

func TestSessionStateReportsOfflineForMissingPane(t *testing.T) {
	fake := newFakeHerdr(t, snapshotJSON(agentJOnlyIdle()))
	client := newClient(t, fake, nil)

	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if got := snapshot.SessionState("s-live"); got != herdrc.StateIdle {
		t.Errorf("生存セッションの state = %q, want idle", got)
	}
	if got := snapshot.SessionState("s-gone"); got != herdrc.StateOffline {
		t.Errorf("消滅セッションの state = %q, want offline", got)
	}
}

func agentJOnlyIdle() string { return agentJSON("wS:p1", "s-live", "idle", "/repo") }

func TestFocusAgentUsesOneCall(t *testing.T) {
	runner := &fakeRunner{}
	client := newClient(t, newFakeHerdr(t, snapshotJSON()), runner)

	if err := client.FocusAgent(context.Background(), "wD:p8"); err != nil {
		t.Fatalf("FocusAgent: %v", err)
	}

	calls := runner.Calls()
	if len(calls) != 1 {
		t.Fatalf("呼び出し = %v, want 1 回（tab/workspace focus を併用しない）", calls)
	}
	if got := strings.Join(calls[0], " "); got != "agent focus wD:p8" {
		t.Errorf("呼び出し = %q", got)
	}
}

func TestCreateTabReturnsRootPane(t *testing.T) {
	runner := &fakeRunner{handler: func([]string) ([]byte, error) {
		return []byte(`{"id":"cli:tab:create","result":{"type":"tab_created",` +
			`"tab":{"tab_id":"wS:t9","workspace_id":"wS"},` +
			`"root_pane":{"pane_id":"wS:p9","cwd":"/private/tmp/work"}}}`), nil
	}}
	client := newClient(t, newFakeHerdr(t, snapshotJSON()), runner)

	tab, err := client.CreateTab(context.Background(), herdrc.TabSpec{Cwd: "/tmp/work", Label: "設計タスク"})
	if err != nil {
		t.Fatalf("CreateTab: %v", err)
	}
	if tab.PaneID != "wS:p9" || tab.TabID != "wS:t9" {
		t.Errorf("tab = %+v", tab)
	}
	if got := strings.Join(runner.Calls()[0], " "); got != "tab create --cwd /tmp/work --label 設計タスク" {
		t.Errorf("呼び出し = %q", got)
	}
}

func TestStartAgentPassesResumeAfterSeparator(t *testing.T) {
	runner := &fakeRunner{handler: func([]string) ([]byte, error) {
		return []byte(`{"id":"cli:agent:start","result":{"type":"agent_started",` +
			`"argv":["claude","--resume","s-1"],` +
			`"agent":{"pane_id":"wS:p9","agent_session":{"agent":"claude","kind":"id","source":"herdr:claude","value":"s-1"}}}}`), nil
	}}
	client := newClient(t, newFakeHerdr(t, snapshotJSON()), runner)

	got, err := client.StartAgent(context.Background(), herdrc.AgentSpec{
		Name: "taskherd-12", Kind: "claude", PaneID: "wS:p9", Args: []string{"--resume", "s-1"},
	})
	if err != nil {
		t.Fatalf("StartAgent: %v", err)
	}
	if got.SessionID != "s-1" {
		t.Errorf("session_id = %q, want resume 対象と同一", got.SessionID)
	}
	if got.NeedsAttention {
		t.Error("NeedsAttention = true, want false")
	}

	args := runner.Calls()[0]
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "agent start taskherd-12 --kind claude --pane wS:p9") {
		t.Errorf("呼び出し = %q", joined)
	}
	sep := -1
	for i, arg := range args {
		if arg == "--" {
			sep = i
		}
	}
	if sep < 0 || strings.Join(args[sep+1:], " ") != "--resume s-1" {
		t.Errorf("resume 引数が -- の後に無い: %v", args)
	}
}

// A startup prompt (the first-run trust prompt, for instance) leaves the pane usable, so it is
// reported as needing attention rather than as a failed jump.
func TestStartAgentTreatsStartupBlockAsNeedingAttention(t *testing.T) {
	for _, code := range []string{herdrc.CodeAgentNotReady, herdrc.CodeAgentBlocked} {
		t.Run(code, func(t *testing.T) {
			runner := &fakeRunner{handler: func([]string) ([]byte, error) {
				return nil, &herdrc.APIError{Code: code, Message: "blocked during startup"}
			}}
			client := newClient(t, newFakeHerdr(t, snapshotJSON()), runner)

			got, err := client.StartAgent(context.Background(), herdrc.AgentSpec{
				Name: "taskherd-1", Kind: "claude", PaneID: "wS:p9", Args: []string{"--resume", "s-1"},
			})
			if err != nil {
				t.Fatalf("StartAgent = %v, want エラーにしない", err)
			}
			if !got.NeedsAttention || got.PaneID != "wS:p9" || got.Code != code {
				t.Errorf("result = %+v", got)
			}
		})
	}
}

func TestStartAgentPropagatesOtherErrors(t *testing.T) {
	runner := &fakeRunner{handler: func([]string) ([]byte, error) {
		return nil, &herdrc.APIError{Code: "pane_not_found", Message: "pane not found"}
	}}
	client := newClient(t, newFakeHerdr(t, snapshotJSON()), runner)

	if _, err := client.StartAgent(context.Background(), herdrc.AgentSpec{Name: "n", Kind: "claude", PaneID: "x"}); err == nil {
		t.Fatal("err = nil, want pane_not_found を伝播")
	}
}

func TestWaitForAgentStateRepeatsUntilFlagAndDecodesAgent(t *testing.T) {
	runner := &fakeRunner{handler: func([]string) ([]byte, error) {
		return []byte(`{"id":"cli:agent:wait","result":{"agent":` +
			agentJSON("wS:p9", "s-9", "idle", "/repo") + `}}`), nil
	}}
	client := newClient(t, newFakeHerdr(t, snapshotJSON()), runner)

	got, err := client.WaitForAgentState(context.Background(), "wS:p9",
		[]string{herdrc.StateIdle, herdrc.StateBlocked}, 30*time.Second)
	if err != nil {
		t.Fatalf("WaitForAgentState: %v", err)
	}
	if got.SessionID() != "s-9" || got.AgentStatus != "idle" || got.PaneID != "wS:p9" {
		t.Errorf("agent = %+v", got)
	}

	joined := strings.Join(runner.Calls()[0], " ")
	if !strings.HasPrefix(joined, "agent wait wS:p9 ") {
		t.Errorf("呼び出し = %q", joined)
	}
	if got := strings.Count(joined, "--until"); got != 2 {
		t.Errorf("--until の回数 = %d, want 2（状態ごとに繰り返す）", got)
	}
	if !strings.Contains(joined, "--until idle") || !strings.Contains(joined, "--until blocked") {
		t.Errorf("呼び出し = %q, want 各状態が --until で渡る", joined)
	}
	if !strings.Contains(joined, "--timeout 30000") {
		t.Errorf("呼び出し = %q, want --timeout 30000", joined)
	}
}

func TestWaitForAgentStatePropagatesTimeoutError(t *testing.T) {
	runner := &fakeRunner{handler: func([]string) ([]byte, error) {
		return nil, &herdrc.APIError{Code: "wait_timeout", Message: "timed out"}
	}}
	client := newClient(t, newFakeHerdr(t, snapshotJSON()), runner)

	if _, err := client.WaitForAgentState(context.Background(), "wS:p9", []string{herdrc.StateIdle}, time.Second); err == nil {
		t.Fatal("err = nil, want timeout エラーを伝播")
	}
}

func TestSendAgentPromptSendsExactText(t *testing.T) {
	runner := &fakeRunner{handler: func([]string) ([]byte, error) { return nil, nil }}
	client := newClient(t, newFakeHerdr(t, snapshotJSON()), runner)

	if err := client.SendAgentPrompt(context.Background(), "wS:p9", "実装してください"); err != nil {
		t.Fatalf("SendAgentPrompt: %v", err)
	}

	got := strings.Join(runner.Calls()[0], " ")
	if got != "agent prompt wS:p9 実装してください" {
		t.Errorf("呼び出し = %q", got)
	}
}

func TestReportTaskTokenStampsWithTTL(t *testing.T) {
	runner := &fakeRunner{}
	client := newClient(t, newFakeHerdr(t, snapshotJSON()), runner)

	if err := client.ReportTaskToken(context.Background(), "wS:p1", 12); err != nil {
		t.Fatalf("ReportTaskToken: %v", err)
	}

	got := strings.Join(runner.Calls()[0], " ")
	want := "pane report-metadata wS:p1 --source plugin:taskherd --token task=12 --ttl-ms 86400000"
	if got != want {
		t.Errorf("呼び出し = %q, want %q", got, want)
	}
}

func newClient(t *testing.T, fake *fakeHerdr, runner herdrc.Runner) *herdrc.Client {
	t.Helper()
	if runner == nil {
		runner = &fakeRunner{}
	}
	return herdrc.New(herdrc.Options{
		Getenv: envFunc(map[string]string{"HERDR_SOCKET_PATH": fake.Path(), "HERDR_ENV": "1", "HERDR_PANE_ID": "wS:p1"}),
		Runner: runner,
	})
}

func waitUpdate(t *testing.T, updates <-chan herdrc.Update) herdrc.Update {
	t.Helper()
	select {
	case update, ok := <-updates:
		if !ok {
			t.Fatal("Updates が閉じられた")
		}
		return update
	case <-time.After(5 * time.Second):
		t.Fatal("Update が届かない")
		return herdrc.Update{}
	}
}

func TestWatchEmitsSnapshotThenSubscribes(t *testing.T) {
	fake := newFakeHerdr(t, snapshotJSON(agentJSON("wS:p1", "s-1", "idle", "/repo")))
	client := newClient(t, fake, nil)

	watcher := client.Watch(context.Background())
	defer watcher.Close()

	first := waitUpdate(t, watcher.Updates())
	if !first.Status.Available || first.Snapshot == nil {
		t.Fatalf("初回 update = %+v, want 到達 + snapshot", first)
	}

	// The subscription carries the broadcast set plus one filtered subscription per agent pane.
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

	kinds, paneIDs := parseSubscriptions(t, params)
	for _, want := range []string{"pane.created", "pane.closed", "pane.exited", "pane.agent_detected"} {
		if !contains(kinds, want) {
			t.Errorf("購読に %q が無い: %v", want, kinds)
		}
	}
	if !contains(paneIDs, "wS:p1") {
		t.Errorf("pane.agent_status_changed の pane_id = %v, want wS:p1 を含む", paneIDs)
	}
}

// An event only means "something may have changed": herdr replays matching history on subscribe,
// so the state always comes from a fresh snapshot.
func TestWatchRefetchesSnapshotOnEvent(t *testing.T) {
	fake := newFakeHerdr(t, snapshotJSON(agentJSON("wS:p1", "s-1", "idle", "/repo")))
	client := newClient(t, fake, nil)

	watcher := client.Watch(context.Background())
	defer watcher.Close()

	first := waitUpdate(t, watcher.Updates())
	if got := first.Snapshot.SessionState("s-1"); got != herdrc.StateIdle {
		t.Fatalf("初回 state = %q, want idle", got)
	}
	waitForSubscribe(t, fake, 1)

	fake.SetSnapshot(snapshotJSON(agentJSON("wS:p1", "s-1", "working", "/repo")))
	fake.Push(`{"event":"pane_agent_status_changed","data":{"pane_id":"wS:p1","type":"pane_agent_status_changed","agent_status":"working"}}`)

	update := waitUpdate(t, watcher.Updates())
	if !update.Status.Available || update.Snapshot == nil {
		t.Fatalf("update = %+v", update)
	}
	if got := update.Snapshot.SessionState("s-1"); got != herdrc.StateWorking {
		t.Errorf("再取得後の state = %q, want working", got)
	}
}

// A replayed burst must neither cause an endless resubscribe loop nor a redundant update:
// the connection is rebuilt only when the set of agent panes differs, and an unchanged snapshot
// is not passed on.
func TestWatchKeepsSubscriptionWhenPaneSetIsUnchanged(t *testing.T) {
	fake := newFakeHerdr(t, snapshotJSON(agentJSON("wS:p1", "s-1", "idle", "/repo")))
	client := newClient(t, fake, nil)

	watcher := client.Watch(context.Background())
	defer watcher.Close()

	waitUpdate(t, watcher.Updates())
	waitForSubscribe(t, fake, 1)

	for i := 0; i < 5; i++ {
		fake.Push(`{"event":"pane_created","data":{"type":"pane_created","pane":{"pane_id":"wS:p1"}}}`)
	}

	// Long enough for the debounce to fire and a resubscribe to show up if one were coming.
	time.Sleep(1500 * time.Millisecond)
	if got := len(fake.Subscribes()); got != 1 {
		t.Errorf("subscribe 回数 = %d, want 1（pane 集合が変わらないなら張り直さない）", got)
	}
	select {
	case update := <-watcher.Updates():
		t.Errorf("状態が変わっていないのに update が届いた: %+v", update.Snapshot)
	default:
	}
}

// herdr has no unsubscribe, so changing the filtered subscriptions means a new connection.
func TestWatchResubscribesWhenPaneSetChanges(t *testing.T) {
	fake := newFakeHerdr(t, snapshotJSON(agentJSON("wS:p1", "s-1", "idle", "/repo")))
	client := newClient(t, fake, nil)

	watcher := client.Watch(context.Background())
	defer watcher.Close()

	waitUpdate(t, watcher.Updates())
	waitForSubscribe(t, fake, 1)

	fake.SetSnapshot(snapshotJSON(
		agentJSON("wS:p1", "s-1", "idle", "/repo"),
		agentJSON("wD:p8", "s-2", "working", "/other"),
	))
	fake.Push(`{"event":"pane_created","data":{"type":"pane_created","pane":{"pane_id":"wD:p8"}}}`)

	waitUpdate(t, watcher.Updates())
	subs := waitForSubscribe(t, fake, 2)

	_, paneIDs := parseSubscriptions(t, subs[1])
	if !contains(paneIDs, "wS:p1") || !contains(paneIDs, "wD:p8") {
		t.Errorf("張り直し後の pane_id = %v, want 両方", paneIDs)
	}
}

func TestFingerprintTracksRenderedFields(t *testing.T) {
	base := parseSnapshot(t, snapshotJSON(agentJSON("wS:p1", "s-1", "idle", "/repo")))

	tests := []struct {
		name  string
		other string
		same  bool
	}{
		{name: "同一内容", other: snapshotJSON(agentJSON("wS:p1", "s-1", "idle", "/repo")), same: true},
		{name: "status が変わる", other: snapshotJSON(agentJSON("wS:p1", "s-1", "working", "/repo"))},
		{name: "pane が変わる", other: snapshotJSON(agentJSON("wS:p2", "s-1", "idle", "/repo"))},
		{name: "session が変わる", other: snapshotJSON(agentJSON("wS:p1", "s-2", "idle", "/repo"))},
		{name: "cwd が変わる", other: snapshotJSON(agentJSON("wS:p1", "s-1", "idle", "/other"))},
		{name: "agent が増える", other: snapshotJSON(
			agentJSON("wS:p1", "s-1", "idle", "/repo"),
			agentJSON("wD:p8", "s-2", "idle", "/other"),
		)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			other := parseSnapshot(t, tt.other)
			if got := base.Fingerprint() == other.Fingerprint(); got != tt.same {
				t.Errorf("同一判定 = %v, want %v", got, tt.same)
			}
		})
	}
}

func parseSnapshot(t *testing.T, body string) *herdrc.Snapshot {
	t.Helper()
	var snapshot herdrc.Snapshot
	if err := json.Unmarshal([]byte(body), &snapshot); err != nil {
		t.Fatalf("snapshot を解析できない: %v", err)
	}
	return &snapshot
}

func TestWatchReconnectsAfterDisconnect(t *testing.T) {
	fake := newFakeHerdr(t, snapshotJSON(agentJSON("wS:p1", "s-1", "idle", "/repo")))
	client := newClient(t, fake, nil)

	watcher := client.Watch(context.Background())
	defer watcher.Close()

	waitUpdate(t, watcher.Updates())
	waitForSubscribe(t, fake, 1)

	fake.DropSubscribers()

	// The disconnect is reported, then the watcher comes back with a fresh snapshot.
	sawOffline := false
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		update := waitUpdate(t, watcher.Updates())
		if !update.Status.Available {
			sawOffline = true
			continue
		}
		if sawOffline && update.Snapshot != nil {
			waitForSubscribe(t, fake, 2)
			return
		}
	}
	t.Fatalf("再接続しなかった（オフライン検知 = %v）", sawOffline)
}

func TestWatchClosesUpdatesOnClose(t *testing.T) {
	fake := newFakeHerdr(t, snapshotJSON(agentJSON("wS:p1", "s-1", "idle", "/repo")))
	client := newClient(t, fake, nil)

	watcher := client.Watch(context.Background())
	waitUpdate(t, watcher.Updates())
	watcher.Close()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := <-watcher.Updates(); !ok {
			return
		}
	}
	t.Fatal("Close 後も Updates が閉じられない")
}

func waitForSubscribe(t *testing.T, fake *fakeHerdr, want int) [][]byte {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if subs := fake.Subscribes(); len(subs) >= want {
			return subs
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("subscribe が %d 回に達しない（実際: %d）", want, len(fake.Subscribes()))
	return nil
}

func parseSubscriptions(t *testing.T, params []byte) (kinds, paneIDs []string) {
	t.Helper()
	var decoded struct {
		Subscriptions []struct {
			Type   string `json:"type"`
			PaneID string `json:"pane_id"`
		} `json:"subscriptions"`
	}
	if err := json.Unmarshal(params, &decoded); err != nil {
		t.Fatalf("subscribe params を解析できない: %v\n%s", err, params)
	}
	for _, sub := range decoded.Subscriptions {
		if sub.Type == "pane.agent_status_changed" {
			paneIDs = append(paneIDs, sub.PaneID)
			continue
		}
		kinds = append(kinds, sub.Type)
	}
	return kinds, paneIDs
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
