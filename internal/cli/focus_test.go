package cli_test

import (
	"strings"
	"testing"
)

// Starting a session means going to work on it, so the pane is focused as it is created rather
// than left in a background tab. This is what makes the board's g land on the new tab: the board
// closes the moment it hands the launch off, and the launch is what moves the user.
func TestStartFocusesTheNewTabByDefault(t *testing.T) {
	h := newHarness(t)
	fake := newFakeHerdr()
	fake.waitSessionID = "s-new"
	h.herdr = fake
	h.mustRun(t, "add", "a")

	h.mustRun(t, "start", "1", "--cwd", "/repo/work")

	tab := fake.call("tab create")
	if !containsArg(tab, "--focus") {
		t.Errorf("tab create = %v, want --focus", tab)
	}
}

// --no-focus is for a launch nobody is waiting on: started for a task other than the one at hand,
// or from a script that must not move the user.
func TestStartNoFocusLeavesTheTabInTheBackground(t *testing.T) {
	h := newHarness(t)
	fake := newFakeHerdr()
	fake.waitSessionID = "s-new"
	h.herdr = fake
	h.mustRun(t, "add", "a")

	h.mustRun(t, "start", "1", "--cwd", "/repo/work", "--no-focus")

	tab := fake.call("tab create")
	if containsArg(tab, "--focus") {
		t.Errorf("tab create = %v, want --focus 無し", tab)
	}
	if fake.called("agent focus") {
		t.Error("--no-focus なのに agent focus を呼んでいる")
	}
}

// A recovered pane creates no tab, so the focus has to come from agent focus instead — otherwise
// the one path that reuses a pane would be the one path that does not take the user there.
func TestStartFocusesRecoveredPane(t *testing.T) {
	h := newHarness(t)
	fake := newFakeHerdr().withAgent("s-prev", fakeAgent{PaneID: "wS:p9", Name: "taskherd-1", Cwd: "/repo/first-attempt"})
	h.herdr = fake
	h.mustRun(t, "add", "a")

	res := h.mustRun(t, "start", "1", "--json")

	if got := decodeStart(t, res.stdout); !got.Reused {
		t.Fatalf("reused = %v, want true（前提: 回収パスを通ること）", got.Reused)
	}
	if got := fake.call("agent focus"); strings.Join(got, " ") != "agent focus wS:p9" {
		t.Errorf("呼び出し = %v, want agent focus wS:p9", got)
	}
}

func TestStartNoFocusSkipsFocusingRecoveredPane(t *testing.T) {
	h := newHarness(t)
	fake := newFakeHerdr().withAgent("s-prev", fakeAgent{PaneID: "wS:p9", Name: "taskherd-1", Cwd: "/repo/first-attempt"})
	h.herdr = fake
	h.mustRun(t, "add", "a")

	h.mustRun(t, "start", "1", "--no-focus", "--json")

	if fake.called("agent focus") {
		t.Error("--no-focus なのに agent focus を呼んでいる")
	}
}

// jump means "take me there" whichever branch it goes down. The live-pane branch already focused;
// a resumed session used to land in a background tab, which was the one silent exception.
func TestJumpResumeFocusesTheNewTab(t *testing.T) {
	h := newHarness(t)
	fake := newFakeHerdr()
	h.herdr = fake
	h.mustRun(t, "add", "a")
	h.mustRun(t, "session", "link", "1", "--session-id", "s-gone", "--cwd", "/repo/work")

	h.mustRun(t, "jump", "1", "--yes")

	tab := fake.call("tab create")
	if !containsArg(tab, "--focus") {
		t.Errorf("tab create = %v, want --focus", tab)
	}
}

func containsArg(call []string, want string) bool {
	for _, arg := range call {
		if arg == want {
			return true
		}
	}
	return false
}
