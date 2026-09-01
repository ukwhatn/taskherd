package cli_test

import (
	"strings"
	"testing"
)

// A quoted --cwd reaches the process with its ~ intact, and so does one the board hands over. herdr
// is given a real path either way, and the SessionRef records the same one so the next launch's
// candidate list offers something that exists.
func TestStartExpandsTildeInTheCwdFlag(t *testing.T) {
	h := newHarness(t)
	fake := newFakeHerdr()
	fake.waitSessionID = "s-new"
	h.herdr = fake
	h.env["HOME"] = "/home/u"
	h.mustRun(t, "add", "a")

	h.mustRun(t, "start", "1", "--cwd", "~/dev/taskherd", "--json")

	tab := fake.call("tab create")
	if tab == nil || !strings.Contains(strings.Join(tab, " "), "--cwd /home/u/dev/taskherd") {
		t.Errorf("tab create = %v, want 展開済みの cwd", tab)
	}
	if got := h.tasks(t).Tasks[0].Sessions[0].Cwd; got != "/home/u/dev/taskherd" {
		t.Errorf("記録された cwd = %q, want /home/u/dev/taskherd", got)
	}
}

// A --cwd that is only a ~ names the home directory itself rather than a directory called "~".
func TestStartExpandsABareTildeCwd(t *testing.T) {
	h := newHarness(t)
	fake := newFakeHerdr()
	fake.waitSessionID = "s-new"
	h.herdr = fake
	h.env["HOME"] = "/home/u"
	h.mustRun(t, "add", "a")

	h.mustRun(t, "start", "1", "--cwd", "~", "--json")

	tab := fake.call("tab create")
	if tab == nil || !strings.Contains(strings.Join(tab, " "), "--cwd /home/u") {
		t.Errorf("tab create = %v, want HOME そのもの", tab)
	}
}
