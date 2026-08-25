package cli_test

import (
	"strings"
	"testing"
)

// Regression: opening a task's detail directly (the pane's session is already linked) must not
// require an open column the way `board` itself does. requireOpenColumn is a rule about a board
// the cursor has to move around on, not about opening one task's detail by id, and
// Columns.Validate accepts a terminal-only config — so before the fix, picker refused this whole
// scenario outright with requireOpenColumn's own UserError, leaving neither a detail nor a picker
// fallback for a config board itself never rejects unless someone tries to open one.
//
// The test cannot observe the board actually opening (there is no TTY in a test run, so
// tui.Run fails at that point regardless), but that failure is itself the signal: it only reaches
// tui.Run — and its distinct "could not open TTY" error — once requireOpenColumn has been skipped.
// Reintroducing the check in this branch makes this test fail (verified below the fix).
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

[[columns]]
id = "wontfix"
label = "Wontfix"
kind = "terminal"
color = "gray"
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
