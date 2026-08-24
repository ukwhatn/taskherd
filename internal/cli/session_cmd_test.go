package cli_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ukwhatn/taskherd/internal/herdrc"
	"github.com/ukwhatn/taskherd/internal/model"
)

const (
	sessionA = "33274acc-7d02-494d-bb06-dd907cbb6d0a"
	sessionB = "6f66ca9a-96fe-4e35-a97c-f9726a48077f"
)

// inHerdr puts the harness inside a herdr-managed pane holding the given session.
func (h *harness) inHerdr(paneID, sessionID, cwd string) *fakeHerdr {
	h.env["HERDR_ENV"] = "1"
	h.env["HERDR_PANE_ID"] = paneID
	h.herdr = newFakeHerdr().withAgent(sessionID, fakeAgent{PaneID: paneID, Cwd: cwd})
	return h.herdr
}

func decodeJump(t *testing.T, stdout string) struct {
	TaskID         int    `json:"task_id"`
	SessionID      string `json:"session_id"`
	Action         string `json:"action"`
	PaneID         string `json:"pane_id"`
	NeedsAttention bool   `json:"needs_attention"`
} {
	t.Helper()
	var payload struct {
		TaskID         int    `json:"task_id"`
		SessionID      string `json:"session_id"`
		Action         string `json:"action"`
		PaneID         string `json:"pane_id"`
		NeedsAttention bool   `json:"needs_attention"`
	}
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("jump の JSON を解析できない: %v\n%s", err, stdout)
	}
	return payload
}

func TestSessionLinkCurrentResolvesFromSnapshot(t *testing.T) {
	h := newHarness(t)
	fake := h.inHerdr("wS:p1", sessionA, "/repo/herdr")
	h.mustRun(t, "add", "設計")

	res := h.mustRun(t, "session", "link", "1", "--current", "--label", "設計セッション", "--json")

	task := decodeTask(t, res.stdout)
	if len(task.Sessions) != 1 {
		t.Fatalf("sessions = %+v", task.Sessions)
	}
	session := task.Sessions[0]
	if session.SessionID != sessionA || session.Agent != "claude" || session.Cwd != "/repo/herdr" {
		t.Errorf("session = %+v, want herdr から解決した値", session)
	}
	if session.Label != "設計セッション" {
		t.Errorf("label = %q", session.Label)
	}
	if session.LinkedAt != model.NewTimestamp(baseTime) {
		t.Errorf("linked_at = %q, want %q", session.LinkedAt, model.NewTimestamp(baseTime))
	}
	// The task id is stamped back onto the pane so herdr's own UI can show it.
	stamp := fake.call("pane report-metadata")
	if stamp == nil {
		t.Fatal("pane report-metadata を呼んでいない")
	}
	if got := strings.Join(stamp, " "); !strings.Contains(got, "--token task=1") || !strings.Contains(got, "--source plugin:taskherd") {
		t.Errorf("report-metadata の引数 = %q", got)
	}
}

func TestSessionLinkCurrentOutsideHerdr(t *testing.T) {
	h := newHarness(t)
	h.herdr = newFakeHerdr()
	h.mustRun(t, "add", "設計")

	res := h.run(t, "session", "link", "1", "--current", "--json")

	if res.code == 0 {
		t.Fatal("exit = 0, want 非 0（HERDR_PANE_ID が無い）")
	}
	payload := decodeError(t, res.stderr)
	if !strings.Contains(payload.Hint, "--session-id") {
		t.Errorf("hint = %q, want 明示指定の案内", payload.Hint)
	}
	if len(h.tasks(t).Tasks[0].Sessions) != 0 {
		t.Error("失敗したのにセッションが紐づいている")
	}
}

func TestSessionLinkCurrentWithoutSessionID(t *testing.T) {
	h := newHarness(t)
	h.env["HERDR_ENV"] = "1"
	h.env["HERDR_PANE_ID"] = "wS:p1"
	h.herdr = newFakeHerdr().withPaneWithoutSession("wS:p1", "/repo")
	h.mustRun(t, "add", "設計")

	res := h.run(t, "session", "link", "1", "--current", "--json")

	if res.code == 0 {
		t.Fatal("exit = 0, want 非 0（agent_session が null）")
	}
	payload := decodeError(t, res.stderr)
	if !strings.Contains(payload.Hint, "herdr integration install claude") {
		t.Errorf("hint = %q, want integration 導入の案内", payload.Hint)
	}
}

func TestSessionLinkRequiresExactlyOneSelector(t *testing.T) {
	h := newHarness(t)
	h.inHerdr("wS:p1", sessionA, "/repo")
	h.mustRun(t, "add", "設計")

	for _, args := range [][]string{
		{"session", "link", "1", "--json"},
		{"session", "link", "1", "--current", "--session-id", sessionA, "--json"},
		{"session", "link", "1", "--current", "--pane", "wS:p1", "--json"},
	} {
		res := h.run(t, args...)
		if res.code == 0 {
			t.Errorf("%v が成功した", args)
		}
	}
}

// A session herdr cannot resolve needs an explicit cwd: without it the resume path is impossible.
func TestSessionLinkSessionIDFallsBackToCwd(t *testing.T) {
	h := newHarness(t)
	h.herdr = newFakeHerdr()
	h.mustRun(t, "add", "設計")

	res := h.run(t, "session", "link", "1", "--session-id", sessionB, "--json")
	if res.code == 0 {
		t.Fatal("exit = 0, want 非 0（--cwd なし）")
	}
	if payload := decodeError(t, res.stderr); !strings.Contains(payload.Hint, "--cwd") {
		t.Errorf("hint = %q, want --cwd の案内", payload.Hint)
	}

	res = h.mustRun(t, "session", "link", "1", "--session-id", sessionB, "--cwd", "/repo/old", "--json")
	session := decodeTask(t, res.stdout).Sessions[0]
	if session.Cwd != "/repo/old" || session.Agent != "claude" {
		t.Errorf("session = %+v, want cwd 指定値 + agent=claude 固定", session)
	}
}

func TestSessionLinkSessionIDPrefersSnapshot(t *testing.T) {
	h := newHarness(t)
	h.herdr = newFakeHerdr().withAgent(sessionB, fakeAgent{PaneID: "wD:p8", Agent: "cursor", Cwd: "/repo/live"})
	h.mustRun(t, "add", "設計")

	res := h.mustRun(t, "session", "link", "1", "--session-id", sessionB, "--cwd", "/無視される", "--json")

	session := decodeTask(t, res.stdout).Sessions[0]
	if session.Cwd != "/repo/live" || session.Agent != "cursor" {
		t.Errorf("session = %+v, want herdr の解決結果を優先", session)
	}
}

func TestSessionLinkRejectsDuplicateOnSameTaskButAllowsAnotherTask(t *testing.T) {
	h := newHarness(t)
	h.inHerdr("wS:p1", sessionA, "/repo")
	h.mustRun(t, "add", "タスク1")
	h.mustRun(t, "add", "タスク2")

	h.mustRun(t, "session", "link", "1", "--current")

	if res := h.run(t, "session", "link", "1", "--current"); res.code == 0 {
		t.Error("同一タスクへの同一セッション重複が成功した")
	}
	// The same session on another task is allowed: one session can serve several tasks.
	h.mustRun(t, "session", "link", "2", "--current")

	f := h.tasks(t)
	if len(f.Tasks[0].Sessions) != 1 || len(f.Tasks[1].Sessions) != 1 {
		t.Errorf("sessions = %+v / %+v", f.Tasks[0].Sessions, f.Tasks[1].Sessions)
	}
}

func TestSessionUnlink(t *testing.T) {
	h := newHarness(t)
	h.inHerdr("wS:p1", sessionA, "/repo")
	h.mustRun(t, "add", "設計")
	h.mustRun(t, "session", "link", "1", "--current")

	if res := h.run(t, "session", "unlink", "1", sessionB); res.code == 0 {
		t.Error("紐づいていないセッションの unlink が成功した")
	}

	res := h.mustRun(t, "session", "unlink", "1", sessionA, "--json")
	if task := decodeTask(t, res.stdout); len(task.Sessions) != 0 {
		t.Errorf("unlink 後の sessions = %+v", task.Sessions)
	}
}

func TestAddWithSessionCurrent(t *testing.T) {
	h := newHarness(t)
	fake := h.inHerdr("wS:p1", sessionA, "/repo/herdr")

	res := h.mustRun(t, "add", "実装", "--session", "current", "--json")

	task := decodeTask(t, res.stdout)
	if len(task.Sessions) != 1 || task.Sessions[0].SessionID != sessionA {
		t.Fatalf("sessions = %+v", task.Sessions)
	}
	if !fake.called("pane report-metadata") {
		t.Error("作成時に pane へタスク id を刻んでいない")
	}
}

func TestAddWithSessionUUIDRequiresCwd(t *testing.T) {
	h := newHarness(t)
	h.herdr = newFakeHerdr()

	res := h.run(t, "add", "実装", "--session", sessionB, "--json")

	if res.code == 0 {
		t.Fatal("exit = 0, want 非 0")
	}
	if got := len(h.tasks(t).Tasks); got != 0 {
		t.Errorf("tasks = %d, want 0（セッション解決に失敗したらタスクも作らない）", got)
	}
}

func TestJumpFocusesLivePane(t *testing.T) {
	h := newHarness(t)
	fake := h.inHerdr("wS:p1", sessionA, "/repo/herdr")
	h.mustRun(t, "add", "設計")
	h.mustRun(t, "session", "link", "1", "--current")

	res := h.mustRun(t, "jump", "1", "--json")

	got := decodeJump(t, res.stdout)
	if got.Action != "focus" || got.PaneID != "wS:p1" || got.SessionID != sessionA {
		t.Errorf("jump = %+v", got)
	}
	focus := fake.call("agent focus")
	if focus == nil || strings.Join(focus, " ") != "agent focus wS:p1" {
		t.Errorf("agent focus の呼び出し = %v", focus)
	}
	// A single focus call is enough: it moves workspace, tab and pane together.
	if fake.called("tab focus") || fake.called("workspace focus") {
		t.Error("tab focus / workspace focus を併用している")
	}
}

func TestJumpResumesWhenPaneIsGone(t *testing.T) {
	h := newHarness(t)
	h.herdr = newFakeHerdr()
	h.mustRun(t, "add", "設計")
	h.mustRun(t, "session", "link", "1", "--session-id", sessionB, "--cwd", "/repo/old")
	fake := h.herdr

	res := h.mustRun(t, "jump", "1", "--yes", "--json")

	got := decodeJump(t, res.stdout)
	if got.Action != "resume" || got.PaneID != "wS:p9" {
		t.Errorf("jump = %+v", got)
	}

	tab := fake.call("tab create")
	if tab == nil || !strings.Contains(strings.Join(tab, " "), "--cwd /repo/old") {
		t.Errorf("tab create = %v, want 保存済み cwd を使う", tab)
	}
	if !strings.Contains(strings.Join(tab, " "), "--label 設計") {
		t.Errorf("tab create = %v, want タスクタイトルを label にする", tab)
	}

	start := fake.call("agent start")
	if start == nil {
		t.Fatal("agent start を呼んでいない")
	}
	joined := strings.Join(start, " ")
	if !strings.Contains(joined, "--kind claude") || !strings.Contains(joined, "--pane wS:p9") {
		t.Errorf("agent start = %q", joined)
	}
	if !strings.Contains(joined, "-- --resume "+sessionB) {
		t.Errorf("agent start = %q, want -- の後に --resume <uuid>", joined)
	}
}

func TestJumpResumeConfirmsBeforeStarting(t *testing.T) {
	h := newHarness(t)
	h.herdr = newFakeHerdr()
	h.mustRun(t, "add", "設計")
	h.mustRun(t, "session", "link", "1", "--session-id", sessionB, "--cwd", "/repo/old")
	h.stdinContent = "n\n"

	res := h.mustRun(t, "jump", "1")

	if !h.stdin.read {
		t.Error("確認プロンプトで stdin を読んでいない")
	}
	if h.herdr.called("tab create") {
		t.Error("中止したのに tab を作った")
	}
	if !strings.Contains(res.stdout, "中止") {
		t.Errorf("stdout = %q, want 中止の表示", res.stdout)
	}
}

// With --json nothing may prompt, so the resume confirmation has to come from --yes.
func TestJumpResumeRequiresYesInJSONMode(t *testing.T) {
	h := newHarness(t)
	h.herdr = newFakeHerdr()
	h.mustRun(t, "add", "設計")
	h.mustRun(t, "session", "link", "1", "--session-id", sessionB, "--cwd", "/repo/old")
	h.stdinContent = "y\n"

	res := h.run(t, "jump", "1", "--json")

	if res.code == 0 {
		t.Fatal("exit = 0, want 非 0")
	}
	if h.stdin.read {
		t.Error("--json 指定なのに stdin を読んだ")
	}
	if h.herdr.called("tab create") {
		t.Error("確認前に tab を作った")
	}
	if payload := decodeError(t, res.stderr); !strings.Contains(payload.Hint, "--yes") {
		t.Errorf("hint = %q, want --yes の案内", payload.Hint)
	}
}

// Resuming is only defined for claude; other agents get pointed at their own cwd.
func TestJumpDoesNotResumeOtherAgents(t *testing.T) {
	h := newHarness(t)
	h.herdr = newFakeHerdr().withAgent(sessionB, fakeAgent{PaneID: "wD:p8", Agent: "cursor", Cwd: "/repo/live"})
	h.mustRun(t, "add", "設計")
	h.mustRun(t, "session", "link", "1", "--session-id", sessionB)
	// The pane is gone by the time jump runs.
	h.herdr = newFakeHerdr()

	res := h.run(t, "jump", "1", "--yes", "--json")

	if res.code == 0 {
		t.Fatal("exit = 0, want 非 0")
	}
	payload := decodeError(t, res.stderr)
	if !strings.Contains(payload.Error, "cursor") {
		t.Errorf("error = %q, want agent 名を含む", payload.Error)
	}
	if !strings.Contains(payload.Hint, "/repo/live") {
		t.Errorf("hint = %q, want cwd での手動再開案内", payload.Hint)
	}
	if h.herdr.called("tab create") {
		t.Error("claude 以外なのに pane を作った")
	}
}

func TestJumpMultipleSessionsNeedsSelection(t *testing.T) {
	h := newHarness(t)
	h.herdr = newFakeHerdr().
		withAgent(sessionA, fakeAgent{PaneID: "wS:p1", Cwd: "/repo/a"}).
		withAgent(sessionB, fakeAgent{PaneID: "wD:p8", Cwd: "/repo/b"})
	h.mustRun(t, "add", "設計")
	h.mustRun(t, "session", "link", "1", "--session-id", sessionA)
	h.mustRun(t, "session", "link", "1", "--session-id", sessionB)

	res := h.run(t, "jump", "1", "--json")
	if res.code == 0 {
		t.Fatal("exit = 0, want 非 0（複数セッション）")
	}
	if h.stdin.read {
		t.Error("--json 指定なのに stdin を読んだ")
	}
	payload := decodeError(t, res.stderr)
	if !strings.Contains(payload.Hint, "--session") || !strings.Contains(payload.Hint, sessionB) {
		t.Errorf("hint = %q, want --session と候補一覧", payload.Hint)
	}

	res = h.mustRun(t, "jump", "1", "--session", sessionB, "--json")
	if got := decodeJump(t, res.stdout); got.PaneID != "wD:p8" {
		t.Errorf("jump = %+v, want 指定したセッションの pane", got)
	}
}

func TestJumpPromptsForSessionInTextMode(t *testing.T) {
	h := newHarness(t)
	h.herdr = newFakeHerdr().
		withAgent(sessionA, fakeAgent{PaneID: "wS:p1", Cwd: "/repo/a"}).
		withAgent(sessionB, fakeAgent{PaneID: "wD:p8", Cwd: "/repo/b"})
	h.mustRun(t, "add", "設計")
	h.mustRun(t, "session", "link", "1", "--session-id", sessionA)
	h.mustRun(t, "session", "link", "1", "--session-id", sessionB)
	h.stdinContent = "2\n"

	res := h.mustRun(t, "jump", "1")

	if !strings.Contains(res.stdout, sessionB) {
		t.Errorf("stdout = %q, want 選択肢の一覧", res.stdout)
	}
	focus := h.herdr.call("agent focus")
	if focus == nil || strings.Join(focus, " ") != "agent focus wD:p8" {
		t.Errorf("agent focus = %v, want 2 番目の選択", focus)
	}
}

func TestJumpWithoutSessionsIsRejected(t *testing.T) {
	h := newHarness(t)
	h.herdr = newFakeHerdr()
	h.mustRun(t, "add", "設計")

	res := h.run(t, "jump", "1", "--json")

	if res.code == 0 {
		t.Fatal("exit = 0, want 非 0")
	}
	if payload := decodeError(t, res.stderr); !strings.Contains(payload.Hint, "session link") {
		t.Errorf("hint = %q, want 紐づけの案内", payload.Hint)
	}
}

// Outside herdr the jump cannot happen, so the user gets the command to run by hand.
func TestJumpOfflineShowsManualCommand(t *testing.T) {
	h := newHarness(t)
	h.herdr = newFakeHerdr()
	h.mustRun(t, "add", "設計")
	h.mustRun(t, "session", "link", "1", "--session-id", sessionB, "--cwd", "/repo/old")
	h.herdr.unavailable = true

	res := h.run(t, "jump", "1", "--json")

	if res.code == 0 {
		t.Fatal("exit = 0, want 非 0")
	}
	payload := decodeError(t, res.stderr)
	if !strings.Contains(payload.Hint, "claude --resume "+sessionB) {
		t.Errorf("hint = %q, want 具体的な resume コマンド", payload.Hint)
	}
	if !strings.Contains(payload.Hint, "/repo/old") {
		t.Errorf("hint = %q, want cwd を含む", payload.Hint)
	}
}

func TestJumpReportsStartupBlockAsNeedingAttention(t *testing.T) {
	h := newHarness(t)
	h.herdr = newFakeHerdr()
	h.mustRun(t, "add", "設計")
	h.mustRun(t, "session", "link", "1", "--session-id", sessionB, "--cwd", "/repo/old")
	h.herdr.startErr = &herdrc.APIError{Code: herdrc.CodeAgentNotReady, Message: "blocked during startup"}

	res := h.mustRun(t, "jump", "1", "--yes", "--json")

	got := decodeJump(t, res.stdout)
	if got.Action != "resume" || !got.NeedsAttention {
		t.Errorf("jump = %+v, want resume 成功 + 要対応", got)
	}
}

func TestListShowsSessionState(t *testing.T) {
	h := newHarness(t)
	h.herdr = newFakeHerdr().
		withAgent(sessionA, fakeAgent{PaneID: "wS:p1", Status: "blocked", Cwd: "/repo/a"}).
		withAgent(sessionB, fakeAgent{PaneID: "wD:p8", Status: "idle", Cwd: "/repo/b"})
	h.mustRun(t, "add", "設計")
	h.mustRun(t, "session", "link", "1", "--session-id", sessionA)
	h.mustRun(t, "session", "link", "1", "--session-id", sessionB)

	res := h.mustRun(t, "list")
	if !strings.Contains(res.stdout, herdrc.StateBlocked) {
		t.Errorf("list = %q, want blocked（最も注意すべき状態を集約）", res.stdout)
	}

	res = h.mustRun(t, "list", "--json")
	var payload struct {
		Herdr struct {
			Available bool `json:"available"`
		} `json:"herdr"`
		SessionStates map[string]struct {
			State  string `json:"state"`
			PaneID string `json:"pane_id"`
		} `json:"session_states"`
	}
	if err := json.Unmarshal([]byte(res.stdout), &payload); err != nil {
		t.Fatalf("list --json を解析できない: %v\n%s", err, res.stdout)
	}
	if !payload.Herdr.Available {
		t.Error("herdr.available = false")
	}
	if got := payload.SessionStates[sessionA]; got.State != "blocked" || got.PaneID != "wS:p1" {
		t.Errorf("session_states[A] = %+v", got)
	}
	if got := payload.SessionStates[sessionB].State; got != "idle" {
		t.Errorf("session_states[B].state = %q", got)
	}
}

func TestListMarksVanishedSessionOffline(t *testing.T) {
	h := newHarness(t)
	h.herdr = newFakeHerdr()
	h.mustRun(t, "add", "設計")
	h.mustRun(t, "session", "link", "1", "--session-id", sessionB, "--cwd", "/repo/old")

	res := h.mustRun(t, "list")

	if !strings.Contains(res.stdout, herdrc.StateOffline) {
		t.Errorf("list = %q, want offline", res.stdout)
	}
}

// The core of the tool works without herdr; the degradation is reported once, on stderr.
func TestListDegradesWhenHerdrUnreachable(t *testing.T) {
	h := newHarness(t)
	h.herdr = newFakeHerdr()
	h.mustRun(t, "add", "設計")
	h.mustRun(t, "session", "link", "1", "--session-id", sessionB, "--cwd", "/repo/old")
	h.herdr.unavailable = true

	res := h.mustRun(t, "list")
	if !strings.Contains(res.stdout, "設計") {
		t.Errorf("stdout = %q, want タスク一覧は表示する", res.stdout)
	}
	if !strings.Contains(res.stderr, "herdr") {
		t.Errorf("stderr = %q, want 縮退の 1 行注記", res.stderr)
	}

	// With --json the same fact belongs in the payload, and stdout stays a single object.
	res = h.mustRun(t, "list", "--json")
	if res.stderr != "" {
		t.Errorf("stderr = %q, want 空（--json では stdout に載せる）", res.stderr)
	}
	var payload struct {
		Herdr struct {
			Available bool   `json:"available"`
			Error     string `json:"error"`
		} `json:"herdr"`
	}
	if err := json.Unmarshal([]byte(res.stdout), &payload); err != nil {
		t.Fatalf("list --json を解析できない: %v", err)
	}
	if payload.Herdr.Available || payload.Herdr.Error == "" {
		t.Errorf("herdr = %+v, want available=false + 理由", payload.Herdr)
	}
}

// A user with no linked sessions must not be told about a feature they are not using.
func TestListStaysSilentWithoutLinkedSessions(t *testing.T) {
	h := newHarness(t)
	h.herdr = newFakeHerdr()
	h.herdr.unavailable = true
	h.mustRun(t, "add", "設計")

	res := h.mustRun(t, "list")

	if res.stderr != "" {
		t.Errorf("stderr = %q, want 空", res.stderr)
	}
	if h.herdr.called("api snapshot") {
		t.Error("紐づけが無いのに herdr へ問い合わせている")
	}
}

func TestShowShowsSessionState(t *testing.T) {
	h := newHarness(t)
	h.herdr = newFakeHerdr().withAgent(sessionA, fakeAgent{PaneID: "wS:p1", Status: "working", Cwd: "/repo/a"})
	h.mustRun(t, "add", "設計")
	h.mustRun(t, "session", "link", "1", "--session-id", sessionA)

	res := h.mustRun(t, "show", "1")

	for _, want := range []string{sessionA, "working", "wS:p1", "/repo/a"} {
		if !strings.Contains(res.stdout, want) {
			t.Errorf("show の出力に %q が無い:\n%s", want, res.stdout)
		}
	}
}
