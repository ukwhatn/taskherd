package herdrc

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// runnerFunc adapts a plain function to the Runner interface, for a test that needs to inspect
// the context WaitForAgentState builds rather than just the args (fakeRunner in the external test
// package drops the context, which every other test here has no need of).
type runnerFunc func(ctx context.Context, args ...string) ([]byte, error)

func (f runnerFunc) Run(ctx context.Context, args ...string) ([]byte, error) { return f(ctx, args...) }

// writeFakeBin writes a shell script standing in for the herdr binary, so execRunner's own
// subprocess handling (as opposed to the Runner interface tests elsewhere) can be exercised
// against a real *exec.ExitError with Stderr populated.
func writeFakeBin(t *testing.T, script string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("シェルスクリプトを直接実行できない")
	}
	path := filepath.Join(t.TempDir(), "fake-herdr")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		t.Fatalf("フェイクバイナリを書けない: %v", err)
	}
	return path
}

func TestExecRunnerRestoresAPIErrorFromStderr(t *testing.T) {
	bin := writeFakeBin(t, `echo '{"id":"x","error":{"code":"agent_not_ready","message":"blocked during startup"}}' 1>&2
exit 1`)
	runner := &execRunner{bin: bin}

	_, err := runner.Run(context.Background(), "agent", "start", "n", "--kind", "claude", "--pane", "p")

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v, want APIError（stderr の封筒から復元される）", err)
	}
	if apiErr.Code != CodeAgentNotReady {
		t.Errorf("code = %q, want %q", apiErr.Code, CodeAgentNotReady)
	}
}

// stdout carrying a non-error diagnostic must not stop the stderr envelope from being read: the
// branch is "stdout から解析できなかったら stderr を見る", not "stdout が空なら".
func TestExecRunnerFallsBackToStderrWhenStdoutIsNotAnEnvelope(t *testing.T) {
	bin := writeFakeBin(t, `echo 'some non-json diagnostic on stdout'
echo '{"id":"x","error":{"code":"agent_blocked","message":"blocked"}}' 1>&2
exit 1`)
	runner := &execRunner{bin: bin}

	_, err := runner.Run(context.Background(), "agent", "focus", "wS:p9")

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v, want APIError", err)
	}
	if apiErr.Code != CodeAgentBlocked {
		t.Errorf("code = %q, want %q", apiErr.Code, CodeAgentBlocked)
	}
}

func TestExecRunnerPrefersStdoutEnvelopeOverStderr(t *testing.T) {
	bin := writeFakeBin(t, `echo '{"id":"x","error":{"code":"pane_not_found","message":"stdout"}}'
echo '{"id":"x","error":{"code":"agent_blocked","message":"stderr"}}' 1>&2
exit 1`)
	runner := &execRunner{bin: bin}

	_, err := runner.Run(context.Background(), "agent", "focus", "wS:p9")

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v, want APIError", err)
	}
	if apiErr.Code != "pane_not_found" {
		t.Errorf("code = %q, want stdout の内容が優先される", apiErr.Code)
	}
}

// The text `agent prompt` sends is a bare positional with no flag following it, so it is the
// sharpest case for the no-argument-values rule: nothing about its shape marks it apart from a
// real subcommand word.
func TestExecRunnerErrorNeverLeaksArgumentValues(t *testing.T) {
	bin := writeFakeBin(t, `echo 'plain failure, no envelope' 1>&2
exit 1`)
	runner := &execRunner{bin: bin}
	const secret = "SENSITIVE-PROMPT-TEXT-must-not-leak"

	_, err := runner.Run(context.Background(), "agent", "prompt", "wS:p9", secret)

	if err == nil {
		t.Fatal("err = nil, want エラー")
	}
	if strings.Contains(err.Error(), secret) {
		t.Errorf("err = %v, want TEXT を含まない", err)
	}
	if !strings.Contains(err.Error(), "agent prompt") {
		t.Errorf("err = %v, want 操作名 agent prompt を含む", err)
	}
}

func TestExecRunnerSucceedsWithoutEnvelope(t *testing.T) {
	bin := writeFakeBin(t, `echo '{"id":"x","result":{"ok":true}}'`)
	runner := &execRunner{bin: bin}

	out, err := runner.Run(context.Background(), "api", "snapshot")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(string(out), `"ok":true`) {
		t.Errorf("out = %q", out)
	}
}

// The context WaitForAgentState builds must outlive the timeout it hands herdr itself: herdr's own
// wait is bounded by --timeout, and cliTimeout on top of that is the round-trip margin, the same
// shape StartAgent already uses. A context cut to exactly the wait timeout would cancel the request
// out from under a herdr that answers right at the deadline.
func TestWaitForAgentStateContextOutlivesTheWaitTimeout(t *testing.T) {
	const timeout = 50 * time.Millisecond
	var deadline time.Time
	var hasDeadline bool
	runner := runnerFunc(func(ctx context.Context, args ...string) ([]byte, error) {
		deadline, hasDeadline = ctx.Deadline()
		return []byte(`{"id":"x","result":{"agent":{"pane_id":"p"}}}`), nil
	})
	client := &Client{runner: runner}

	if _, err := client.WaitForAgentState(context.Background(), "p", []string{StateIdle}, timeout); err != nil {
		t.Fatalf("WaitForAgentState: %v", err)
	}
	if !hasDeadline {
		t.Fatal("context に締切が付いていない")
	}
	if remaining := time.Until(deadline); remaining <= timeout {
		t.Errorf("残り時間 = %s, want %s（cliTimeout の余裕）より大きい", remaining, timeout)
	}
}

func TestOperationNameStopsAtKnownSubcommands(t *testing.T) {
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"agent", "prompt", "wS:p9", "any text at all"}, "agent prompt"},
		{[]string{"agent", "start", "taskherd-1", "--kind", "claude"}, "agent start"},
		{[]string{"plugin", "pane", "open", "--plugin", "x"}, "plugin pane open"},
		{[]string{"pane", "report-metadata", "wS:p1", "--source", "s"}, "pane report-metadata"},
		{[]string{"unknown-command", "x", "y"}, "コマンド"},
		{nil, "コマンド"},
	}
	for _, tt := range tests {
		if got := operationName(tt.args); got != tt.want {
			t.Errorf("operationName(%v) = %q, want %q", tt.args, got, tt.want)
		}
	}
}
