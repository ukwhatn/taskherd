package cli_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ukwhatn/taskherd/internal/i18n"
)

// spaceResult reads the field both start and jump report the launch's space through.
type spaceResult struct {
	WorkspaceID string `json:"workspace_id"`
}

func decodeSpace(t *testing.T, stdout string) spaceResult {
	t.Helper()
	var payload spaceResult
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("JSON を解析できない: %v\n%s", err, stdout)
	}
	return payload
}

func TestStartCreatesTheTabInTheChosenSpace(t *testing.T) {
	h := newHarness(t)
	h.herdr = newFakeHerdr()
	h.mustRun(t, "add", "設計する")

	res := h.mustRun(t, "start", "1", "--cwd", "/repo/work", "--space", "wG", "--json")

	if got := decodeSpace(t, res.stdout).WorkspaceID; got != "wG" {
		t.Errorf("workspace_id = %q, want wG", got)
	}
	if !h.herdr.called("tab create --workspace wG") {
		t.Errorf("tab create に --workspace が渡っていない: %+v", h.herdr.calls)
	}
}

func TestStartCreatesASpaceWhenAskedTo(t *testing.T) {
	h := newHarness(t)
	h.herdr = newFakeHerdr()
	h.mustRun(t, "add", "設計する")

	res := h.mustRun(t, "start", "1", "--cwd", "/repo/work", "--new-space", "調査用", "--json")

	if got := decodeSpace(t, res.stdout).WorkspaceID; got != "wNEW" {
		t.Errorf("workspace_id = %q, want wNEW", got)
	}
	if !h.herdr.called("workspace create --cwd /repo/work --label 調査用") {
		t.Errorf("workspace create が呼ばれていない: %+v", h.herdr.calls)
	}
	if h.herdr.called("tab create") {
		t.Error("新しい space を作ったのに tab create も呼ばれている")
	}
}

// An unlabelled --new-space still asks for a space; the label is what is left to herdr.
func TestStartNewSpaceWithoutALabelStillCreatesOne(t *testing.T) {
	h := newHarness(t)
	h.herdr = newFakeHerdr()
	h.mustRun(t, "add", "設計する")

	h.mustRun(t, "start", "1", "--cwd", "/repo/work", "--new-space", "", "--json")

	if !h.herdr.called("workspace create --cwd /repo/work") {
		t.Errorf("workspace create が呼ばれていない: %+v", h.herdr.calls)
	}
	for _, call := range h.herdr.calls {
		if strings.Contains(strings.Join(call, " "), "--label") {
			t.Errorf("ラベル未指定なのに --label が送られている: %v", call)
		}
	}
}

func TestStartRefusesBothSpaceFlags(t *testing.T) {
	h := newHarness(t)
	h.herdr = newFakeHerdr()
	h.mustRun(t, "add", "設計する")

	res := h.run(t, "start", "1", "--cwd", "/repo", "--space", "wG", "--new-space", "x")

	if res.code == 0 {
		t.Fatal("--space と --new-space を同時に受け付けた")
	}
	if !strings.Contains(res.stderr, i18n.For(i18n.LangJA).CLI.Start.SpaceConflict.Msg) {
		t.Errorf("stderr = %q", res.stderr)
	}
	if h.herdr.called("tab create") || h.herdr.called("workspace create") {
		t.Error("矛盾した指定で pane を作ってしまった")
	}
}

// Recovering a previous attempt's pane is what makes a failed first launch retriable, so a launch
// that named no space still takes it — wherever it is. Naming a different one is a request the
// recovery cannot honour, and honouring it silently would put the session in the wrong space.
func TestStartRecoveryRespectsTheChosenSpace(t *testing.T) {
	tests := []struct {
		name        string
		agentSpace  string
		args        []string
		wantReused  bool
		wantRefusal bool
	}{
		{name: "space 未指定なら回収する", agentSpace: "wG", args: nil, wantReused: true},
		{name: "同じ space を指定したら回収する", agentSpace: "wG", args: []string{"--space", "wG"}, wantReused: true},
		{name: "別の space を指定したら拒否する", agentSpace: "wG", args: []string{"--space", "wS"}, wantRefusal: true},
		{name: "新しい space を求めたら拒否する", agentSpace: "wG", args: []string{"--new-space", "別"}, wantRefusal: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.herdr = newFakeHerdr().withAgent("s-left", fakeAgent{
				PaneID:      "wG:p3",
				Name:        "taskherd-1",
				WorkspaceID: tc.agentSpace,
				Cwd:         "/repo/work",
			})
			h.mustRun(t, "add", "設計する")

			args := append([]string{"start", "1", "--cwd", "/repo/work"}, tc.args...)
			res := h.run(t, append(args, "--json")...)

			if tc.wantRefusal {
				if res.code == 0 {
					t.Fatalf("別の space を指定したのに通った: %s", res.stdout)
				}
				if h.herdr.called("tab create") || h.herdr.called("workspace create") {
					t.Error("拒否したのに pane を作ってしまった")
				}
				return
			}
			if res.code != 0 {
				t.Fatalf("回収できるはずが失敗した: code=%d / %s", res.code, res.stderr)
			}
			if got := decodeStart(t, res.stdout); got.Reused != tc.wantReused {
				t.Errorf("reused = %v, want %v", got.Reused, tc.wantReused)
			}
		})
	}
}

func TestJumpResumesIntoTheChosenSpace(t *testing.T) {
	h := newHarness(t)
	h.herdr = newFakeHerdr()
	h.mustRun(t, "add", "設計する")
	h.mustRun(t, "session", "link", "1", "--session-id", "s-gone", "--cwd", "/repo/work")

	res := h.mustRun(t, "jump", "1", "--space", "wG", "--yes", "--json")

	if got := decodeSpace(t, res.stdout).WorkspaceID; got != "wG" {
		t.Errorf("workspace_id = %q, want wG", got)
	}
	if !h.herdr.called("tab create --workspace wG") {
		t.Errorf("tab create に --workspace が渡っていない: %+v", h.herdr.calls)
	}
}

// A live pane is focused where it already is, so the report names the space it is in rather than
// one the caller asked for: moving a pane between spaces is a different operation.
func TestJumpToALivePaneReportsItsOwnSpace(t *testing.T) {
	h := newHarness(t)
	h.herdr = newFakeHerdr().withAgent("s-live", fakeAgent{
		PaneID:      "wG:p3",
		WorkspaceID: "wG",
		Cwd:         "/repo/work",
	})
	h.mustRun(t, "add", "設計する")
	h.mustRun(t, "session", "link", "1", "--session-id", "s-live", "--cwd", "/repo/work")

	res := h.mustRun(t, "jump", "1", "--json")

	if got := decodeSpace(t, res.stdout).WorkspaceID; got != "wG" {
		t.Errorf("workspace_id = %q, want wG", got)
	}
}

// A launch that stopped partway still says where its pane is, which is the only way back to it.
func TestStartReportsTheSpaceEvenWhenItFailsLater(t *testing.T) {
	h := newHarness(t)
	fake := newFakeHerdr()
	fake.waitSessionID = ""
	h.herdr = fake
	h.sessionStartWaitTimeout = 50 * time.Millisecond
	h.mustRun(t, "add", "設計する")

	res := h.run(t, "start", "1", "--cwd", "/repo/work", "--space", "wG", "--json")

	if res.code == 0 {
		t.Fatal("セッション id が取れないのに成功した")
	}
	if got := decodeSpace(t, res.stdout).WorkspaceID; got != "wG" {
		t.Errorf("workspace_id = %q, want wG（部分失敗でも space を返す）", got)
	}
}
