package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ukwhatn/taskherd/internal/config"
)

func TestConfigPath(t *testing.T) {
	h := newHarness(t)

	res := h.mustRun(t, "config", "path")
	for _, want := range []string{h.configPath, h.stateDir, "tasks.json", "tasks.json.bak", "tasks.lock"} {
		if !strings.Contains(res.stdout, want) {
			t.Errorf("config path の出力に %q が無い:\n%s", want, res.stdout)
		}
	}

	res = h.mustRun(t, "config", "path", "--json")
	var payload struct {
		Config   string `json:"config"`
		StateDir string `json:"state_dir"`
		Tasks    string `json:"tasks"`
		Backup   string `json:"backup"`
		Lock     string `json:"lock"`
	}
	if err := json.Unmarshal([]byte(res.stdout), &payload); err != nil {
		t.Fatalf("JSON を解析できない: %v\n%s", err, res.stdout)
	}
	if payload.Config != h.configPath || payload.StateDir != h.stateDir {
		t.Errorf("payload = %+v", payload)
	}
	if payload.Tasks != filepath.Join(h.stateDir, "tasks.json") {
		t.Errorf("tasks = %q", payload.Tasks)
	}
	if payload.Backup != filepath.Join(h.stateDir, "tasks.json.bak") || payload.Lock != filepath.Join(h.stateDir, "tasks.lock") {
		t.Errorf("backup/lock = %q / %q", payload.Backup, payload.Lock)
	}
}

func TestConfigInit(t *testing.T) {
	h := newHarness(t)

	res := h.mustRun(t, "config", "init")
	if !strings.Contains(res.stdout, h.configPath) {
		t.Errorf("生成先を報告していない: %q", res.stdout)
	}

	info, err := os.Stat(h.configPath)
	if err != nil {
		t.Fatalf("config.toml が作られていない: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("config.toml の権限 = %o, want 600", perm)
	}
	dirInfo, err := os.Stat(filepath.Dir(h.configPath))
	if err != nil {
		t.Fatalf("config ディレクトリを stat できない: %v", err)
	}
	if perm := dirInfo.Mode().Perm(); perm != 0o700 {
		t.Errorf("config ディレクトリの権限 = %o, want 700", perm)
	}

	cfg, err := config.Load(h.configPath)
	if err != nil {
		t.Fatalf("生成した config を読めない: %v", err)
	}
	if got := cfg.Columns.IDs(); len(got) != 6 || got[0] != "todo" {
		t.Errorf("生成した config の columns = %v", got)
	}

	if res := h.run(t, "config", "init"); res.code == 0 {
		t.Error("既存 config を上書きしてしまった")
	}
}

func TestConfigInitJSON(t *testing.T) {
	h := newHarness(t)

	res := h.mustRun(t, "config", "init", "--json")

	var payload struct {
		Created string `json:"created"`
	}
	if err := json.Unmarshal([]byte(res.stdout), &payload); err != nil {
		t.Fatalf("JSON を解析できない: %v\n%s", err, res.stdout)
	}
	if payload.Created != h.configPath {
		t.Errorf("created = %q, want %q", payload.Created, h.configPath)
	}
}

func TestGeneratedConfigDrivesStatusValidation(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "config", "init")

	h.mustRun(t, "add", "a", "--status", "planning")
	if res := h.run(t, "add", "b", "--status", "backlog"); res.code == 0 {
		t.Error("生成した config に無い列が受理された")
	}
}

func TestInvalidConfigIsReported(t *testing.T) {
	h := newHarness(t)
	h.writeConfig(t, `
[[columns]]
id = "todo"
label = "ToDo"
kind = "open"

[[columns]]
id = "todo"
label = "重複"
kind = "open"
`)

	res := h.run(t, "list", "--json")

	if res.code == 0 {
		t.Fatal("exit = 0, want 非 0")
	}
	payload := decodeError(t, res.stderr)
	if !strings.Contains(payload.Error, "columns[1].id") {
		t.Errorf("error = %q, want 違反箇所 columns[1].id", payload.Error)
	}
}
