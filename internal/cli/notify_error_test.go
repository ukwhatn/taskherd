package cli_test

import (
	"strings"
	"testing"
)

// --notify-error is how a launch the board detached from itself reports a failure. The board is
// gone by then and the process writes to a log nobody is watching, so without this a launch that
// stopped at a trust-folder prompt is indistinguishable from one still running.
func TestNotifyErrorRaisesNotificationOnPlainFailure(t *testing.T) {
	h := newHarness(t)
	fake := newFakeHerdr()
	h.herdr = fake

	res := h.run(t, "start", "999", "--cwd", "/repo", "--notify-error", "#999 の起動")

	if res.code == 0 {
		t.Fatal("exit = 0, want 非 0")
	}
	call, ok := fake.notification()
	if !ok {
		t.Fatalf("通知が送られていない: %v", fake.calls)
	}
	if len(call) < 3 || call[2] != "taskherd: #999 の起動に失敗" {
		t.Errorf("title = %q, want ラベル入りのタイトル", call)
	}
	if !containsFlag(call, "--body") {
		t.Errorf("call = %q, want --body 付き", call)
	}
}

// A partial failure writes its result to stdout and never goes through report(), so the message
// has to reach the notification off the returned error instead — the reason partialResultError
// carries the text at all.
func TestNotifyErrorRaisesNotificationOnPartialFailure(t *testing.T) {
	h := newHarness(t)
	fake := newFakeHerdr()
	fake.startErr = herdrcUnavailableErr
	h.herdr = fake
	h.mustRun(t, "add", "a")

	res := h.run(t, "start", "1", "--cwd", "/repo", "--notify-error", "#1 の起動")

	if res.code == 0 {
		t.Fatal("exit = 0, want 非 0")
	}
	call, ok := fake.notification()
	if !ok {
		t.Fatalf("通知が送られていない: %v", fake.calls)
	}
	body, ok := flagValue(call, "--body")
	if !ok {
		t.Fatalf("call = %q, want --body 付き", call)
	}
	if body == "" || strings.Contains(body, "stdout に出力済み") {
		t.Errorf("body = %q, want 実際の失敗内容（汎用の言い回しではなく）", body)
	}
}

func TestNotifyErrorStaysQuietOnSuccess(t *testing.T) {
	h := newHarness(t)
	fake := newFakeHerdr()
	fake.waitSessionID = "s-new"
	h.herdr = fake
	h.mustRun(t, "add", "a")

	h.mustRun(t, "start", "1", "--cwd", "/repo/work", "--notify-error", "#1 の起動")

	if call, ok := fake.notification(); ok {
		t.Errorf("成功したのに通知が送られた: %q", call)
	}
}

// Without the flag nothing is raised: an ordinary invocation has someone reading stderr.
func TestFailureWithoutNotifyErrorRaisesNothing(t *testing.T) {
	h := newHarness(t)
	fake := newFakeHerdr()
	h.herdr = fake

	h.run(t, "start", "999", "--cwd", "/repo")

	if call, ok := fake.notification(); ok {
		t.Errorf("--notify-error 無しなのに通知が送られた: %q", call)
	}
}

func containsFlag(call []string, flag string) bool {
	_, ok := flagValue(call, flag)
	return ok
}

func flagValue(call []string, flag string) (string, bool) {
	for i, arg := range call {
		if arg == flag && i+1 < len(call) {
			return call[i+1], true
		}
	}
	return "", false
}
