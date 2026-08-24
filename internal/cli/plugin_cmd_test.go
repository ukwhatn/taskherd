package cli_test

import (
	"strings"
	"testing"
)

func TestPluginOpenBoardExecsPaneOpenWithoutPlacement(t *testing.T) {
	h := newHarness(t)
	h.herdr = newFakeHerdr()

	h.mustRun(t, "plugin", "open-board")

	if !h.herdr.called("plugin pane open --plugin taskherd --entrypoint board") {
		t.Errorf("呼び出し = %+v", h.herdr.calls)
	}
}

func TestPluginLinkPaneReadsPaneFromContextJSON(t *testing.T) {
	h := newHarness(t)
	h.herdr = newFakeHerdr()
	h.env["HERDR_PLUGIN_CONTEXT_JSON"] = `{"pane":{"pane_id":"wS:p7"}}`

	h.mustRun(t, "plugin", "link-pane")

	call := h.herdr.call("plugin pane open")
	if call == nil {
		t.Fatal("plugin pane open が呼ばれていない")
	}
	got := strings.Join(call, " ")
	want := "plugin pane open --plugin taskherd --entrypoint picker --env TASKHERD_TARGET_PANE=wS:p7"
	if got != want {
		t.Errorf("呼び出し = %q, want %q", got, want)
	}
}

func TestPluginLinkPaneFallsBackToPaneIDEnv(t *testing.T) {
	h := newHarness(t)
	h.herdr = newFakeHerdr()
	// No HERDR_PLUGIN_CONTEXT_JSON at all: only the direct env var is available.
	h.env["HERDR_PANE_ID"] = "wS:p8"

	h.mustRun(t, "plugin", "link-pane")

	call := h.herdr.call("plugin pane open")
	if call == nil {
		t.Fatal("plugin pane open が呼ばれていない")
	}
	if !strings.Contains(strings.Join(call, " "), "TASKHERD_TARGET_PANE=wS:p8") {
		t.Errorf("呼び出し = %q, want wS:p8 を対象にする", strings.Join(call, " "))
	}
}

func TestPluginLinkPaneWithoutPaneContextFails(t *testing.T) {
	h := newHarness(t)
	h.herdr = newFakeHerdr()

	res := h.run(t, "plugin", "link-pane")

	if res.code == 0 {
		t.Fatal("exit = 0, want 非 0（対象 pane が無い）")
	}
	if h.herdr.called("plugin pane open") {
		t.Error("対象 pane が無いのに plugin pane open を呼んでいる")
	}
}
