package herdrc_test

import (
	"context"
	"strings"
	"testing"

	"github.com/ukwhatn/taskherd/internal/herdrc"
)

func TestOpenPluginPaneBuildsArgvWithoutPlacement(t *testing.T) {
	runner := &fakeRunner{}
	client := newClient(t, newFakeHerdr(t, snapshotJSON()), runner)

	if err := client.OpenPluginPane(context.Background(), "taskherd", "board", nil); err != nil {
		t.Fatalf("OpenPluginPane: %v", err)
	}

	got := strings.Join(runner.Calls()[0], " ")
	// --placement is deliberately never passed: herdr 0.8.2's CLI does not accept "popup" as an
	// override, so the manifest's own placement on the entrypoint has to apply instead.
	want := "plugin pane open --plugin taskherd --entrypoint board"
	if got != want {
		t.Errorf("呼び出し = %q, want %q", got, want)
	}
}

func TestOpenPluginPaneSortsEnvForDeterminism(t *testing.T) {
	runner := &fakeRunner{}
	client := newClient(t, newFakeHerdr(t, snapshotJSON()), runner)

	err := client.OpenPluginPane(context.Background(), "taskherd", "picker", map[string]string{
		"TASKHERD_TARGET_PANE": "wS:p2",
		"AAA":                  "1",
	})
	if err != nil {
		t.Fatalf("OpenPluginPane: %v", err)
	}

	got := strings.Join(runner.Calls()[0], " ")
	want := "plugin pane open --plugin taskherd --entrypoint picker --env AAA=1 --env TASKHERD_TARGET_PANE=wS:p2"
	if got != want {
		t.Errorf("呼び出し = %q, want %q", got, want)
	}
}

func TestParsePluginContextReadsPaneID(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "focused_pane_id を持つ（実機 herdr 0.8.2 の実測形状）", raw: `{"workspace_id":"wS","tab_id":"wS:t1","focused_pane_id":"wS:p3","invocation_source":"cli"}`, want: "wS:p3"},
		{name: "空文字列", raw: "", want: ""},
		{name: "パース不能", raw: "{not json", want: ""},
		{name: "focused_pane_id が無い", raw: `{"workspace_id":"wS"}`, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := herdrc.ParsePluginContext(tt.raw).PaneID(); got != tt.want {
				t.Errorf("PaneID() = %q, want %q", got, tt.want)
			}
		})
	}
}
