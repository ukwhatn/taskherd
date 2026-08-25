package cli_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ukwhatn/taskherd/internal/herdrc"
	"github.com/ukwhatn/taskherd/internal/model"
	"github.com/ukwhatn/taskherd/internal/store"
)

type startResult struct {
	TaskID     int    `json:"task_id"`
	Stage      string `json:"stage"`
	PaneID     string `json:"pane_id"`
	SessionID  string `json:"session_id"`
	Linked     bool   `json:"linked"`
	PromptSent bool   `json:"prompt_sent"`
	Error      string `json:"error"`
	Hint       string `json:"hint"`
}

func decodeStart(t *testing.T, stdout string) startResult {
	t.Helper()
	var payload startResult
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("start の JSON を解析できない: %v\n%s", err, stdout)
	}
	return payload
}

func TestStartFullSuccessSendsTemplatedPromptAndLinksSession(t *testing.T) {
	h := newHarness(t)
	fake := newFakeHerdr()
	fake.waitSessionID = "s-new"
	h.herdr = fake
	h.writeConfig(t, `
[session_start]
prompt_template = "#{{id}} {{title}} を進める"
`)
	h.mustRun(t, "add", "設計する", "--status", "working")

	res := h.mustRun(t, "start", "1", "--cwd", "/repo/work", "--json")

	got := decodeStart(t, res.stdout)
	if got.Stage != "prompted" || !got.Linked || !got.PromptSent {
		t.Fatalf("start = %+v, want 完走", got)
	}
	if got.PaneID != "wS:p9" || got.SessionID != "s-new" {
		t.Errorf("start = %+v", got)
	}
	if got.Error != "" || got.Hint != "" {
		t.Errorf("成功したのに error/hint が付いている: %+v", got)
	}
	if res.stderr != "" {
		t.Errorf("stderr = %q, want 空", res.stderr)
	}

	tab := fake.call("tab create")
	if tab == nil || !strings.Contains(strings.Join(tab, " "), "--cwd /repo/work") {
		t.Errorf("tab create = %v, want 指定 cwd", tab)
	}
	prompt, ok := fake.promptSent()
	if !ok || prompt.Text != "#1 設計する を進める" || prompt.PaneID != "wS:p9" {
		t.Errorf("送信されたプロンプト = %+v, want テンプレート展開結果", prompt)
	}

	task := h.tasks(t).Tasks[0]
	if len(task.Sessions) != 1 || task.Sessions[0].SessionID != "s-new" || task.Sessions[0].Cwd != "/repo/work" {
		t.Errorf("sessions = %+v", task.Sessions)
	}
}

func TestStartExplicitEmptyPromptSendsNothing(t *testing.T) {
	h := newHarness(t)
	fake := newFakeHerdr()
	h.herdr = fake
	h.mustRun(t, "add", "a")

	res := h.mustRun(t, "start", "1", "--cwd", "/repo", "--prompt", "", "--json")

	got := decodeStart(t, res.stdout)
	if got.Stage != "linked" || !got.Linked || got.PromptSent {
		t.Errorf("start = %+v, want linked で止まる（プロンプト無し）", got)
	}
	if fake.called("agent prompt") {
		t.Error("--prompt \"\" なのに agent prompt を呼んでいる")
	}
}

func TestStartOmittedPromptUsesConfiguredTemplate(t *testing.T) {
	h := newHarness(t)
	fake := newFakeHerdr()
	h.herdr = fake
	h.writeConfig(t, "[session_start]\nprompt_template = \"既定テンプレート\"\n")
	h.mustRun(t, "add", "a")

	h.mustRun(t, "start", "1", "--cwd", "/repo", "--json")

	prompt, ok := fake.promptSent()
	if !ok || prompt.Text != "既定テンプレート" {
		t.Errorf("送信されたプロンプト = %+v, want 既定テンプレート", prompt)
	}
}

func TestStartExplicitPromptOverridesTemplate(t *testing.T) {
	h := newHarness(t)
	fake := newFakeHerdr()
	h.herdr = fake
	h.mustRun(t, "add", "a")

	h.mustRun(t, "start", "1", "--cwd", "/repo", "--prompt", "手で書いた指示", "--json")

	prompt, ok := fake.promptSent()
	if !ok || prompt.Text != "手で書いた指示" {
		t.Errorf("送信されたプロンプト = %+v, want 明示指定値", prompt)
	}
}

// A failure before anything was created is a plain error, not a partial result.
func TestStartFailsPlainlyWhenTabCreateFails(t *testing.T) {
	h := newHarness(t)
	fake := newFakeHerdr()
	fake.createTabErr = herdrcUnavailableErr
	h.herdr = fake
	h.mustRun(t, "add", "a")

	res := h.run(t, "start", "1", "--cwd", "/repo", "--json")

	if res.code == 0 {
		t.Fatal("exit = 0, want 非 0")
	}
	if res.stdout != "" {
		t.Errorf("stdout = %q, want 空（何も作られていない）", res.stdout)
	}
	payload := decodeError(t, res.stderr)
	if payload.Error == "" {
		t.Error("error が空")
	}
	if len(h.tasks(t).Tasks[0].Sessions) != 0 {
		t.Error("失敗したのにセッションが紐づいている")
	}
}

// agent start failing outright (not NeedsAttention) still leaves the pane CreateTab made: the
// result must say so even though "started" itself never completed.
func TestStartReportsPartialResultWhenAgentStartFails(t *testing.T) {
	h := newHarness(t)
	fake := newFakeHerdr()
	fake.startErr = herdrcUnavailableErr
	h.herdr = fake
	h.mustRun(t, "add", "a")

	res := h.run(t, "start", "1", "--cwd", "/repo", "--json")

	if res.code == 0 {
		t.Fatal("exit = 0, want 非 0")
	}
	if res.stderr != "" {
		t.Errorf("stderr = %q, want 空（結果は stdout に出す）", res.stderr)
	}
	got := decodeStart(t, res.stdout)
	if got.PaneID != "wS:p9" {
		t.Errorf("pane_id = %q, want tab create が作った pane が残る", got.PaneID)
	}
	if got.Stage != "" {
		t.Errorf("stage = %q, want 空（agent start 自体が失敗した）", got.Stage)
	}
	if got.Linked || got.PromptSent {
		t.Errorf("start = %+v, want linked/prompt_sent ともに false", got)
	}
	if got.Error == "" || got.Hint == "" {
		t.Errorf("start = %+v, want error/hint が両方入っている", got)
	}
}

func TestStartReportsNeedsAttentionAsStartedStage(t *testing.T) {
	h := newHarness(t)
	fake := newFakeHerdr()
	fake.startErr = &herdrc.APIError{Code: herdrc.CodeAgentNotReady, Message: "blocked during startup"}
	h.herdr = fake
	h.mustRun(t, "add", "a")

	res := h.run(t, "start", "1", "--cwd", "/repo", "--json")

	if res.code == 0 {
		t.Fatal("exit = 0, want 非 0")
	}
	got := decodeStart(t, res.stdout)
	if got.Stage != "started" {
		t.Errorf("stage = %q, want started（pane 作成 + agent start は完了している）", got.Stage)
	}
	if got.PaneID == "" {
		t.Error("pane_id が空")
	}
	if !strings.Contains(got.Hint, "picker") {
		t.Errorf("hint = %q, want picker からの後づけ紐づけを案内", got.Hint)
	}
}

func TestStartReportsWaitTimeoutKeepingStartedStage(t *testing.T) {
	h := newHarness(t)
	fake := newFakeHerdr()
	fake.waitErr = errStartWait
	h.herdr = fake
	h.mustRun(t, "add", "a")

	res := h.run(t, "start", "1", "--cwd", "/repo", "--json")

	got := decodeStart(t, res.stdout)
	if got.Stage != "started" {
		t.Errorf("stage = %q, want started（wait が失敗した）", got.Stage)
	}
	if got.SessionID != "" {
		t.Errorf("session_id = %q, want 空", got.SessionID)
	}
	if res.code == 0 {
		t.Fatal("exit = 0, want 非 0")
	}
}

func TestStartReportsMissingSessionIDAfterWait(t *testing.T) {
	h := newHarness(t)
	fake := newFakeHerdr()
	fake.waitSessionID = ""
	h.herdr = fake
	h.mustRun(t, "add", "a")

	res := h.run(t, "start", "1", "--cwd", "/repo", "--json")

	got := decodeStart(t, res.stdout)
	if got.Stage != "started" || got.Linked {
		t.Errorf("start = %+v, want started で止まる", got)
	}
	if res.code == 0 {
		t.Fatal("exit = 0, want 非 0")
	}
}

// blocked is what an untrusted cwd's agent settles into (herdr's own trust-folder gate), and it
// never carries a session id — the single most common way TestStartReportsMissingSessionIDAfterWait
// above's "no session id" wait ends. The message must name that instead of the generic one.
func TestStartReportsBlockedStateAfterWait(t *testing.T) {
	h := newHarness(t)
	fake := newFakeHerdr()
	fake.waitStatus = herdrc.StateBlocked
	fake.waitSessionID = ""
	h.herdr = fake
	h.mustRun(t, "add", "a")

	res := h.run(t, "start", "1", "--cwd", "/repo", "--json")

	got := decodeStart(t, res.stdout)
	if got.Stage != "started" || got.Linked {
		t.Errorf("start = %+v, want started で止まる", got)
	}
	if !strings.Contains(got.Error, "入力待ち") {
		t.Errorf("error = %q, want blocked を示す文言", got.Error)
	}
	if res.code == 0 {
		t.Fatal("exit = 0, want 非 0")
	}
}

// The same session already linked to the task makes AddSession fail (ErrSessionExists), which is
// how "SessionRef の保存失敗" is exercised without needing to fake the store itself.
func TestStartReportsSaveFailureKeepingSessionIDButNotLinked(t *testing.T) {
	h := newHarness(t)
	h.inHerdr("wS:p1", sessionA, "/repo/herdr")
	h.mustRun(t, "add", "a")
	h.mustRun(t, "session", "link", "1", "--current")
	fake := newFakeHerdr()
	fake.waitSessionID = sessionA // already linked to task #1
	h.herdr = fake

	res := h.run(t, "start", "1", "--cwd", "/repo", "--json")

	got := decodeStart(t, res.stdout)
	if got.SessionID != sessionA {
		t.Errorf("session_id = %q, want %q（wait までは成功している）", got.SessionID, sessionA)
	}
	if got.Linked {
		t.Error("保存に失敗したのに linked = true")
	}
	if res.code == 0 {
		t.Fatal("exit = 0, want 非 0")
	}
}

func TestStartReportsPromptFailureAfterLinking(t *testing.T) {
	h := newHarness(t)
	fake := newFakeHerdr()
	fake.promptErr = errPromptFailed
	h.herdr = fake
	h.mustRun(t, "add", "a")

	res := h.run(t, "start", "1", "--cwd", "/repo", "--prompt", "何かの指示", "--json")

	got := decodeStart(t, res.stdout)
	if got.Stage != "linked" || !got.Linked || got.PromptSent {
		t.Errorf("start = %+v, want 紐づけ済み・送信は失敗", got)
	}
	if !strings.Contains(got.Hint, "済んで") {
		t.Errorf("hint = %q, want 起動と紐づけは済んでいる旨の案内", got.Hint)
	}
	task := h.tasks(t).Tasks[0]
	if len(task.Sessions) != 1 {
		t.Errorf("sessions = %+v, want 保存は残る", task.Sessions)
	}
}

// agent prompt's TEXT must never appear on any error path, including this one where the send
// itself fails: the whole point of the herdrc-level fix is that it cannot leak through here either.
func TestStartNeverLeaksPromptTextOnSendFailure(t *testing.T) {
	h := newHarness(t)
	fake := newFakeHerdr()
	fake.promptErr = errPromptFailed
	h.herdr = fake
	h.mustRun(t, "add", "a")
	const secret = "SENSITIVE-PROMPT-TEXT-must-not-leak"

	res := h.run(t, "start", "1", "--cwd", "/repo", "--prompt", secret, "--json")

	if strings.Contains(res.stdout, secret) || strings.Contains(res.stderr, secret) {
		t.Errorf("出力に TEXT が漏れている\nstdout: %s\nstderr: %s", res.stdout, res.stderr)
	}
}

func TestStartRequiresCwdWhenNoCandidateExists(t *testing.T) {
	h := newHarness(t)
	h.herdr = newFakeHerdr()
	h.mustRun(t, "add", "a")

	res := h.run(t, "start", "1", "--json")

	if res.code == 0 {
		t.Fatal("exit = 0, want 非 0")
	}
	if res.stdout != "" {
		t.Errorf("stdout = %q, want 空", res.stdout)
	}
	payload := decodeError(t, res.stderr)
	if !strings.Contains(payload.Hint, "--cwd") {
		t.Errorf("hint = %q, want --cwd の案内", payload.Hint)
	}
}

// A change landed through another path (a concurrent taskherd process, say) while agent wait is
// still running must survive the eventual save: AddSession is reached through Store.Update, which
// re-reads under its own lock rather than from the *model.File this command loaded at the start.
func TestStartConcurrentExternalUpdateSurvives(t *testing.T) {
	h := newHarness(t)
	fake := newFakeHerdr()
	fake.waitSessionID = "s-new"
	fake.waitSideEffect = func() {
		err := store.New(h.stateDir).Update(context.Background(), func(f *model.File) error {
			task, err := f.Task(1)
			if err != nil {
				return err
			}
			task.SetNote("外部から更新", time.Now())
			return nil
		})
		if err != nil {
			t.Fatalf("外部更新に失敗: %v", err)
		}
	}
	h.herdr = fake
	h.mustRun(t, "add", "a")

	h.mustRun(t, "start", "1", "--cwd", "/repo", "--json")

	task := h.tasks(t).Tasks[0]
	if task.Note != "外部から更新" {
		t.Errorf("note = %q, want 外部更新が残る", task.Note)
	}
	if len(task.Sessions) != 1 || task.Sessions[0].SessionID != "s-new" {
		t.Errorf("sessions = %+v", task.Sessions)
	}
}

func TestStartAutoPicksSoleCandidateCwd(t *testing.T) {
	h := newHarness(t)
	h.inHerdr("wS:p1", sessionA, "/repo/sole")
	h.mustRun(t, "add", "既存")
	h.mustRun(t, "session", "link", "1", "--current")
	h.mustRun(t, "add", "新規")
	fake := newFakeHerdr()
	h.herdr = fake

	h.mustRun(t, "start", "2", "--json")

	tab := fake.call("tab create")
	if tab == nil || !strings.Contains(strings.Join(tab, " "), "--cwd /repo/sole") {
		t.Errorf("tab create = %v, want 唯一の候補を自動選択", tab)
	}
}

func TestStartJSONModeRejectsAmbiguousCwdWithoutPrompting(t *testing.T) {
	h := newHarness(t)
	h.herdr = newFakeHerdr().
		withAgent(sessionA, fakeAgent{PaneID: "wS:p1", Cwd: "/repo/a"}).
		withAgent(sessionB, fakeAgent{PaneID: "wD:p8", Cwd: "/repo/b"})
	h.mustRun(t, "add", "既存1")
	h.mustRun(t, "session", "link", "1", "--session-id", sessionA)
	h.mustRun(t, "add", "既存2")
	h.mustRun(t, "session", "link", "2", "--session-id", sessionB)
	h.mustRun(t, "add", "新規")
	h.stdinContent = "1\n"

	res := h.run(t, "start", "3", "--json")

	if res.code == 0 {
		t.Fatal("exit = 0, want 非 0（候補が複数）")
	}
	if h.stdin.read {
		t.Error("--json 指定なのに stdin を読んだ")
	}
	payload := decodeError(t, res.stderr)
	if !strings.Contains(payload.Hint, "/repo/a") || !strings.Contains(payload.Hint, "/repo/b") {
		t.Errorf("hint = %q, want 候補一覧", payload.Hint)
	}
}

func TestStartTextModePromptsForAmbiguousCwd(t *testing.T) {
	h := newHarness(t)
	h.herdr = newFakeHerdr().
		withAgent(sessionA, fakeAgent{PaneID: "wS:p1", Cwd: "/repo/a"}).
		withAgent(sessionB, fakeAgent{PaneID: "wD:p8", Cwd: "/repo/b"})
	h.mustRun(t, "add", "既存1")
	h.mustRun(t, "session", "link", "1", "--session-id", sessionA)
	h.mustRun(t, "add", "既存2")
	h.mustRun(t, "session", "link", "2", "--session-id", sessionB)
	h.mustRun(t, "add", "新規")
	h.stdinContent = "2\n"

	h.mustRun(t, "start", "3")

	if !h.stdin.read {
		t.Error("対話モードなのに stdin を読んでいない")
	}
	tab := h.herdr.call("tab create")
	if tab == nil || !strings.Contains(strings.Join(tab, " "), "--cwd /repo/b") {
		t.Errorf("tab create = %v, want 2 番目の候補", tab)
	}
}

// The trimmed value must be rejected before anything is created, exactly like an omitted --cwd
// falling through to "no candidate" would be — not silently treated as if --cwd were never given.
func TestStartRejectsBlankCwdBeforeCreatingAnything(t *testing.T) {
	h := newHarness(t)
	fake := newFakeHerdr()
	h.herdr = fake
	h.mustRun(t, "add", "a")

	res := h.run(t, "start", "1", "--cwd", "   ", "--json")

	if res.code == 0 {
		t.Fatal("exit = 0, want 非 0")
	}
	if fake.called("tab create") {
		t.Error("空白だけの --cwd なのに pane を作った")
	}
}

var (
	herdrcUnavailableErr = &herdrcTestErr{"herdr に到達できない"}
	errStartWait         = &herdrcTestErr{"wait タイムアウト"}
	errPromptFailed      = &herdrcTestErr{"agent prompt に失敗した"}
)

type herdrcTestErr struct{ msg string }

func (e *herdrcTestErr) Error() string { return e.msg }
