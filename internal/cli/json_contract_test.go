package cli_test

import (
	"encoding/json"
	"strings"
	"testing"
)

// With --json nothing may prompt: a situation that needs input exits non-zero with a JSON error on stderr.
func TestJSONModeNeverPrompts(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "rm は --yes が必須", args: []string{"rm", "1", "--json"}},
		{name: "note は --set / --append が必須", args: []string{"note", "1", "--json"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			h.mustRun(t, "add", "残るべきタスク")
			h.stdinContent = "y\n"

			res := h.run(t, tt.args...)

			if res.code == 0 {
				t.Fatalf("exit = 0, want 非 0\nstdout: %s", res.stdout)
			}
			if h.stdin.read {
				t.Error("--json 指定なのに stdin を読んだ")
			}
			if res.stdout != "" {
				t.Errorf("stdout = %q, want 空（結果は出さない）", res.stdout)
			}
			payload := decodeError(t, res.stderr)
			if payload.Error == "" {
				t.Error("error が空")
			}
			if payload.Hint == "" {
				t.Error("hint が空（指定すべきフラグを案内していない）")
			}
			if got := len(h.tasks(t).Tasks); got != 1 {
				t.Errorf("tasks = %d, want 1（変更していない）", got)
			}
		})
	}
}

func TestJSONModeSucceedsWithExplicitFlags(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "add", "a")

	res := h.mustRun(t, "note", "1", "--set", "非対話で書いた", "--json")
	if res.stderr != "" {
		t.Errorf("stderr = %q, want 空", res.stderr)
	}
	if task := decodeTask(t, res.stdout); task.Note != "非対話で書いた" {
		t.Errorf("note = %q", task.Note)
	}

	res = h.mustRun(t, "rm", "1", "--yes", "--json")
	if h.stdin.read {
		t.Error("--yes 指定なのに stdin を読んだ")
	}
	if got := len(h.tasks(t).Tasks); got != 0 {
		t.Errorf("tasks = %d, want 0", got)
	}
}

func TestJSONOutputIsSingleObject(t *testing.T) {
	h := newHarness(t)

	for _, args := range [][]string{
		{"add", "a", "--json"},
		{"list", "--json"},
		{"show", "1", "--json"},
		{"edit", "1", "--title", "b", "--json"},
		{"move", "1", "review", "--json"},
		{"link", "1", "https://github.com/o/r/pull/1", "--json"},
		{"unlink", "1", "https://github.com/o/r/pull/1", "--json"},
		{"note", "1", "--set", "x", "--json"},
		{"config", "path", "--json"},
	} {
		res := h.mustRun(t, args...)
		var payload map[string]any
		if err := json.Unmarshal([]byte(res.stdout), &payload); err != nil {
			t.Errorf("taskherd %s の stdout が単一 JSON オブジェクトでない: %v\n%s", strings.Join(args, " "), err, res.stdout)
		}
		if res.stderr != "" {
			t.Errorf("taskherd %s の stderr = %q, want 空", strings.Join(args, " "), res.stderr)
		}
	}
}

func TestErrorOutputIsTextWithoutJSONFlag(t *testing.T) {
	h := newHarness(t)

	res := h.run(t, "show", "42")

	if res.code == 0 {
		t.Fatal("exit = 0, want 非 0")
	}
	if strings.HasPrefix(strings.TrimSpace(res.stderr), "{") {
		t.Errorf("--json なしで JSON を返している: %q", res.stderr)
	}
}
