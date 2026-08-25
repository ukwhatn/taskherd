package cli_test

import (
	"strings"
	"testing"
)

// Regression for the requireOpenColumn skip in picker_cmd.go: a terminal-only column config must
// not stop the detail-direct-launch path.
//
// The test cannot observe the board actually opening (there is no TTY in a test run, so tui.Run
// fails at that point regardless), but that failure is itself the signal: it only reaches
// tui.Run — and its distinct "could not open TTY" error — once requireOpenColumn has been skipped.
// Reintroducing the check in that branch makes this test fail on the "open" text below instead.
func TestPickerOpensDetailEvenWithoutAnOpenColumn(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "add", "task with a linked session")
	h.inHerdr("wS:p1", sessionA, "/repo/a")
	h.mustRun(t, "session", "link", "1", "--current")

	h.writeConfig(t, `
[[columns]]
id = "done"
label = "Done"
kind = "terminal"
color = "purple"
`)
	h.env["TASKHERD_TARGET_PANE"] = "wS:p1"

	res := h.run(t, "picker")

	if res.code == 0 {
		t.Fatalf("exit = 0, want 非 0（テスト環境に TTY が無く board 自体は起動できない）\nstdout: %s", res.stdout)
	}
	if strings.Contains(res.stderr, `kind = "open"`) {
		t.Fatalf("stderr = %q, want requireOpenColumn を経由していないこと（detail 直接起動は列構成を問わない）", res.stderr)
	}
	if !strings.Contains(res.stderr, "TTY") {
		t.Fatalf("stderr = %q, want board の起動を試みたことを示す TTY エラー", res.stderr)
	}
}
