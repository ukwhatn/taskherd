package cli_test

import (
	"strings"
	"testing"
)

// The board is an interactive TUI, so --json has nothing to give: it is refused with the
// commands that do have machine-readable output, rather than starting a program whose output
// would be escape codes.
func TestBoardRejectsJSON(t *testing.T) {
	h := newHarness(t)

	res := h.run(t, "board", "--json")

	if res.code == 0 {
		t.Fatal("exit = 0, want 非 0")
	}
	if !strings.Contains(res.stderr, "list --json") {
		t.Errorf("stderr = %q, want 代替コマンドの案内", res.stderr)
	}
}

func TestBoardIsRegistered(t *testing.T) {
	h := newHarness(t)

	res := h.mustRun(t, "--help")

	if !strings.Contains(res.stdout, "board") {
		t.Errorf("help に board が無い:\n%s", res.stdout)
	}
}
