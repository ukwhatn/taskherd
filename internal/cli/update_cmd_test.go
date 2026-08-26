package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ukwhatn/taskherd/internal/buildinfo"
	"github.com/ukwhatn/taskherd/internal/i18n"
	"github.com/ukwhatn/taskherd/internal/update"
)

// writeUpdateRecord plants what a previous check would have left behind.
func writeUpdateRecord(t *testing.T, stateDir string, state update.State) {
	t.Helper()
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatalf("state ディレクトリを作れない: %v", err)
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("記録を組み立てられない: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "update.json"), data, 0o600); err != nil {
		t.Fatalf("記録を書けない: %v", err)
	}
}

// A build from source has no counterpart on the releases page, so nothing is announced and nothing
// is offered. Every test binary is such a build, which is what makes this the default case here.
func TestNoUpdateNoticeForADevelopmentBuild(t *testing.T) {
	h := newHarness(t)
	writeUpdateRecord(t, h.stateDir, update.State{
		CheckedAt: baseTime,
		LatestTag: "v99.0.0",
	})

	res := h.run(t, "list")

	if strings.Contains(res.stderr, "taskherd update") {
		t.Errorf("開発ビルドに更新を勧めている:\n%s", res.stderr)
	}
}

func TestUpdateRefusesToReplaceADevelopmentBuild(t *testing.T) {
	h := newHarness(t)

	res := h.run(t, "update")

	if res.code == 0 {
		t.Fatalf("update が成功している: %s", res.stdout)
	}
	if !strings.Contains(res.stderr, ja.CLI.Update.NotReleased.Msg) {
		t.Errorf("stderr = %q, want リリース版でない旨", res.stderr)
	}
	if !strings.Contains(res.stderr, ja.CLI.Update.NotReleased.Hint) {
		t.Errorf("stderr にヒントが無い:\n%s", res.stderr)
	}
}

// The check has to be switchable off for good: not "asked and ignored", but never asked.
func TestUpdateCheckIsDisabledByTheEnvironment(t *testing.T) {
	h := newHarness(t)
	h.env["TASKHERD_NO_UPDATE_CHECK"] = "1"
	writeUpdateRecord(t, h.stateDir, update.State{CheckedAt: baseTime, LatestTag: "v99.0.0"})

	res := h.run(t, "list")

	if strings.Contains(res.stderr, "v99.0.0") {
		t.Errorf("無効化しているのに通知が出ている:\n%s", res.stderr)
	}
}

// --json output is parsed by other programs; a notice on stderr is fine, but it must never reach
// the document on stdout.
func TestUpdateNoticeStaysOutOfJSON(t *testing.T) {
	h := newHarness(t)
	writeUpdateRecord(t, h.stateDir, update.State{CheckedAt: baseTime, LatestTag: "v99.0.0"})

	res := h.run(t, "--json", "list")

	var payload map[string]any
	if err := json.Unmarshal([]byte(res.stdout), &payload); err != nil {
		t.Fatalf("stdout が JSON ではない (%v):\n%s", err, res.stdout)
	}
	if strings.Contains(res.stderr, "v99.0.0") {
		t.Errorf("--json で通知が出ている:\n%s", res.stderr)
	}
}

// The record is a cache; a damaged one must not take a command down with it.
func TestACorruptUpdateRecordIsIgnored(t *testing.T) {
	h := newHarness(t)
	if err := os.MkdirAll(h.stateDir, 0o700); err != nil {
		t.Fatalf("state ディレクトリを作れない: %v", err)
	}
	if err := os.WriteFile(filepath.Join(h.stateDir, "update.json"), []byte("{broken"), 0o600); err != nil {
		t.Fatalf("壊れた記録を書けない: %v", err)
	}

	if res := h.run(t, "list"); res.code != 0 {
		t.Errorf("壊れた記録で list が失敗した: %s%s", res.stdout, res.stderr)
	}
}

// The notice wording lives in the catalog like everything else the user reads.
func TestUpdateNoticeIsTranslated(t *testing.T) {
	en := i18n.For(i18n.LangEN)
	if ja.CLI.Update.Notice == en.CLI.Update.Notice {
		t.Error("ja と en の通知文言が同じ")
	}
	for _, text := range []string{ja.CLI.Update.Notice, en.CLI.Update.Notice} {
		if !strings.Contains(text, "taskherd update") {
			t.Errorf("通知 %q が実行すべきコマンドを示していない", text)
		}
	}
}

// asReleased makes the test binary claim to be a released build, which is the only state in which
// any of the update machinery does anything.
func asReleased(t *testing.T, version string) {
	t.Helper()
	buildinfo.Version = version
	t.Cleanup(func() { buildinfo.Version = "" })
}

func TestUpdateNoticeAppearsOnceForAReleasedBuild(t *testing.T) {
	asReleased(t, "v1.0.0")

	h := newHarness(t)
	writeUpdateRecord(t, h.stateDir, update.State{CheckedAt: baseTime, LatestTag: "v1.3.0"})

	first := h.run(t, "list")
	if !strings.Contains(first.stderr, "v1.3.0") {
		t.Errorf("新しい版が通知されていない:\n%s", first.stderr)
	}
	if !strings.Contains(first.stderr, "taskherd update") {
		t.Errorf("通知が次の一手を示していない:\n%s", first.stderr)
	}

	// Repeating the same news on every command is how a notice stops being read.
	second := h.run(t, "list")
	if strings.Contains(second.stderr, "v1.3.0") {
		t.Errorf("同じ版が二度通知されている:\n%s", second.stderr)
	}
}

func TestNoUpdateNoticeWhenTheRecordIsNotNewer(t *testing.T) {
	asReleased(t, "v1.3.0")

	h := newHarness(t)
	writeUpdateRecord(t, h.stateDir, update.State{CheckedAt: baseTime, LatestTag: "v1.3.0"})

	if res := h.run(t, "list"); strings.Contains(res.stderr, "v1.3.0") {
		t.Errorf("最新なのに通知が出ている:\n%s", res.stderr)
	}
}

// The notice must not push itself in front of the error the command was reporting.
func TestUpdateNoticeComesAfterTheError(t *testing.T) {
	asReleased(t, "v1.0.0")

	h := newHarness(t)
	writeUpdateRecord(t, h.stateDir, update.State{CheckedAt: baseTime, LatestTag: "v1.3.0"})

	res := h.run(t, "show", "99")

	errIdx := strings.Index(res.stderr, ja.Err.Task.TaskNotFound)
	noticeIdx := strings.Index(res.stderr, "v1.3.0")
	if errIdx < 0 || noticeIdx < 0 {
		t.Fatalf("エラーと通知の両方が出ていない:\n%s", res.stderr)
	}
	if noticeIdx < errIdx {
		t.Errorf("通知がエラーより先に出ている:\n%s", res.stderr)
	}
}
