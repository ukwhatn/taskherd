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

// The board folds terminal columns into a stack the cursor cannot reach, so a column set with no
// open column leaves it with nothing to focus. Only the board minds: the config is valid, and the
// commands that just read it keep working.
func TestBoardRequiresAnOpenColumn(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "add", "移行漏れ")
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

	res := h.run(t, "board")

	if res.code == 0 {
		t.Fatalf("exit = 0, want 非 0\nstdout: %s", res.stdout)
	}
	if !strings.Contains(res.stderr, "open") {
		t.Errorf("stderr = %q, want open の列が要る旨", res.stderr)
	}

	// The same config still lists, which is why the check is not in Columns.Validate.
	list := h.mustRun(t, "list", "--all")
	if !strings.Contains(list.stdout, "移行漏れ") {
		t.Errorf("list が動いていない:\n%s", list.stdout)
	}
}
