package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStartSessionArgs(t *testing.T) {
	got := startSessionArgs(42, "/repo/work", "これをやって\n2 行目")
	want := []string{
		"start", "42",
		"--cwd", "/repo/work",
		"--prompt", "これをやって\n2 行目",
		"--notify-error", "#42 の起動",
	}
	assertArgs(t, got, want)
}

// An empty prompt still travels as an explicit --prompt "": omitting the flag would make start
// fall back to the config template, which is the opposite of what an emptied prompt field means.
func TestStartSessionArgsKeepsEmptyPromptExplicit(t *testing.T) {
	got := startSessionArgs(7, "/repo", "")
	want := []string{
		"start", "7",
		"--cwd", "/repo",
		"--prompt", "",
		"--notify-error", "#7 の起動",
	}
	assertArgs(t, got, want)
}

func TestResumeSessionArgs(t *testing.T) {
	got := resumeSessionArgs(3, "s-gone")
	want := []string{
		"jump", "3",
		"--session", "s-gone",
		"--yes",
		"--notify-error", "#3 の resume",
	}
	assertArgs(t, got, want)
}

func assertArgs(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("args = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("args[%d] = %q, want %q（全体: %q）", i, got[i], want[i], got)
		}
	}
}

// The header the parent writes before handing over is the only part of the log that is there
// synchronously, and it is what tells two runs apart. Every argument is quoted because the prompt
// is multi-line.
func TestDetachedLauncherWritesLogHeader(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "state")
	stamp := time.Date(2026, 8, 26, 13, 9, 20, 0, time.FixedZone("JST", 9*60*60))
	l := &detachedLauncher{exePath: "/bin/echo", stateDir: stateDir, now: func() time.Time { return stamp }}

	if err := l.StartSession(42, "/repo/work", "1 行目\n2 行目"); err != nil {
		t.Fatalf("StartSession: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(stateDir, detachedLogName))
	if err != nil {
		t.Fatalf("ログを読めない: %v", err)
	}
	header := strings.SplitN(string(data), "\n", 2)[0]
	if !strings.HasPrefix(header, "=== 2026-08-26T13:09:20+09:00 taskherd ") {
		t.Errorf("header = %q, want 時刻付きの見出し", header)
	}
	if !strings.Contains(header, `"1 行目\n2 行目"`) {
		t.Errorf("header = %q, want 改行をエスケープしたプロンプト", header)
	}
}

// The log is created 0600 like every other file under the state directory: a prompt can carry
// anything the task's note does.
func TestDetachedLauncherLogIsPrivate(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "state")
	l := &detachedLauncher{exePath: "/bin/echo", stateDir: stateDir}

	if err := l.ResumeSession(1, "s-1"); err != nil {
		t.Fatalf("ResumeSession: %v", err)
	}

	info, err := os.Stat(filepath.Join(stateDir, detachedLogName))
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("mode = %o, want 600", perm)
	}
}

// A hand-off that cannot even start the process has to be reported, not swallowed: the board
// stays open on this and it is the only signal the user gets.
func TestDetachedLauncherReportsUnstartableExecutable(t *testing.T) {
	dir := t.TempDir()
	l := &detachedLauncher{exePath: filepath.Join(dir, "does-not-exist"), stateDir: filepath.Join(dir, "state")}

	err := l.StartSession(1, "/repo", "")

	if err == nil {
		t.Fatal("err = nil, want 起動できない旨のエラー")
	}
	if !strings.Contains(err.Error(), "起こせない") {
		t.Errorf("err = %v, want 起こせない旨", err)
	}
}
