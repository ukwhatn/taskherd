package cli_test

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ukwhatn/taskherd/internal/cli"
	"github.com/ukwhatn/taskherd/internal/config"
	"github.com/ukwhatn/taskherd/internal/model"
	"github.com/ukwhatn/taskherd/internal/store"
)

var (
	baseTime  = time.Date(2026, 8, 24, 16, 0, 0, 0, time.FixedZone("JST", 9*60*60))
	laterTime = time.Date(2026, 8, 25, 9, 30, 0, 0, time.FixedZone("JST", 9*60*60))
)

// trackedReader records whether stdin was read at all, which is how the no-prompt contract is checked.
type trackedReader struct {
	content string
	read    bool
	offset  int
}

func (r *trackedReader) Read(p []byte) (int, error) {
	r.read = true
	if r.offset >= len(r.content) {
		return 0, io.EOF
	}
	n := copy(p, r.content[r.offset:])
	r.offset += n
	return n, nil
}

type harness struct {
	stateDir     string
	configPath   string
	now          time.Time
	env          map[string]string
	stdinContent string
	stdin        *trackedReader
	herdr        *fakeHerdr
}

type result struct {
	code   int
	stdout string
	stderr string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	root := t.TempDir()
	return &harness{
		stateDir:   filepath.Join(root, "state", "taskherd"),
		configPath: filepath.Join(root, "config", "config.toml"),
		now:        baseTime,
		env:        map[string]string{},
	}
}

func (h *harness) writeConfig(t *testing.T, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(h.configPath), 0o700); err != nil {
		t.Fatalf("config ディレクトリを作れない: %v", err)
	}
	if err := os.WriteFile(h.configPath, []byte(content), 0o600); err != nil {
		t.Fatalf("config を書けない: %v", err)
	}
}

func (h *harness) run(t *testing.T, args ...string) result {
	t.Helper()
	var out, errOut bytes.Buffer
	h.stdin = &trackedReader{content: h.stdinContent}
	env := cli.Env{
		Paths:  config.Paths{StateDir: h.stateDir, ConfigPath: h.configPath},
		Out:    &out,
		Err:    &errOut,
		In:     h.stdin,
		Now:    func() time.Time { return h.now },
		Getenv: func(key string) string { return h.env[key] },
	}
	if h.herdr != nil {
		env.Herdr = h.herdr.client(func(key string) string { return h.env[key] })
	}
	code := cli.Run(env, args)
	return result{code: code, stdout: out.String(), stderr: errOut.String()}
}

func (h *harness) mustRun(t *testing.T, args ...string) result {
	t.Helper()
	res := h.run(t, args...)
	if res.code != 0 {
		t.Fatalf("taskherd %s = exit %d\nstdout: %s\nstderr: %s", strings.Join(args, " "), res.code, res.stdout, res.stderr)
	}
	return res
}

func (h *harness) tasks(t *testing.T) *model.File {
	t.Helper()
	f, err := store.New(h.stateDir).Load()
	if err != nil {
		t.Fatalf("tasks.json を読めない: %v", err)
	}
	return f
}

func decodeTask(t *testing.T, stdout string) model.Task {
	t.Helper()
	var payload struct {
		Task model.Task `json:"task"`
	}
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("JSON 出力を解析できない: %v\n出力: %s", err, stdout)
	}
	return payload.Task
}

func decodeTasks(t *testing.T, stdout string) []model.Task {
	t.Helper()
	var payload struct {
		Tasks []model.Task `json:"tasks"`
	}
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("JSON 出力を解析できない: %v\n出力: %s", err, stdout)
	}
	return payload.Tasks
}

func decodeError(t *testing.T, stderr string) struct {
	Error string `json:"error"`
	Hint  string `json:"hint"`
} {
	t.Helper()
	var payload struct {
		Error string `json:"error"`
		Hint  string `json:"hint"`
	}
	if err := json.Unmarshal([]byte(stderr), &payload); err != nil {
		t.Fatalf("stderr が JSON でない: %v\nstderr: %s", err, stderr)
	}
	return payload
}

func TestAddCreatesTaskWithDefaults(t *testing.T) {
	h := newHarness(t)

	res := h.mustRun(t, "add", "herdr タスク管理ツールの設計")

	if !strings.Contains(res.stdout, "#1") {
		t.Errorf("stdout に採番結果が無い: %q", res.stdout)
	}
	f := h.tasks(t)
	if len(f.Tasks) != 1 {
		t.Fatalf("tasks = %d, want 1", len(f.Tasks))
	}
	task := f.Tasks[0]
	if task.ID != 1 || task.Title != "herdr タスク管理ツールの設計" {
		t.Errorf("task = %+v", task)
	}
	if task.Status != "todo" {
		t.Errorf("status = %q, want todo（config の先頭列）", task.Status)
	}
	if task.CreatedAt != model.NewTimestamp(baseTime) || task.UpdatedAt != task.CreatedAt {
		t.Errorf("timestamps = %q / %q", task.CreatedAt, task.UpdatedAt)
	}
	if task.Due != nil || task.Note != "" {
		t.Errorf("due/note = %v / %q, want 未設定", task.Due, task.Note)
	}
}

func TestAddWithAllAttributes(t *testing.T) {
	h := newHarness(t)
	h.writeConfig(t, `
[github]
ghes_hosts = ["github.example.com"]

[jira]
site = "example.atlassian.net"
`)

	res := h.mustRun(t, "add", "実装", "--status", "working", "--due", "2026-08-31", "--note", "メモ",
		"--link", "https://github.example.com/o/r/pull/7",
		"--link", "https://example.atlassian.net/browse/ABC-1",
		"--link", "https://example.com/docs",
		"--json")

	task := decodeTask(t, res.stdout)
	if task.Status != "working" || task.Note != "メモ" {
		t.Errorf("task = %+v", task)
	}
	if task.Due == nil || *task.Due != model.Date("2026-08-31") {
		t.Errorf("due = %v, want 2026-08-31", task.Due)
	}
	wantKinds := []model.LinkKind{model.LinkKindGitHubPR, model.LinkKindJira, model.LinkKindOther}
	if len(task.Links) != len(wantKinds) {
		t.Fatalf("links = %+v, want %d 件", task.Links, len(wantKinds))
	}
	for i, want := range wantKinds {
		if task.Links[i].Kind != want {
			t.Errorf("links[%d].Kind = %q, want %q", i, task.Links[i].Kind, want)
		}
		if task.Links[i].AddedAt != model.NewTimestamp(baseTime) {
			t.Errorf("links[%d].AddedAt = %q", i, task.Links[i].AddedAt)
		}
	}
}

func TestAddRejectsInvalidInputWithoutWriting(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "未定義の status", args: []string{"add", "a", "--status", "存在しない列"}},
		{name: "不正な due", args: []string{"add", "a", "--due", "2026/08/31"}},
		{name: "存在しない日付の due", args: []string{"add", "a", "--due", "2026-02-30"}},
		{name: "URL でない link", args: []string{"add", "a", "--link", "これは URL ではない"}},
		{name: "空のタイトル", args: []string{"add", "   "}},
		{name: "同一 URL の link を 2 回", args: []string{"add", "a", "--link", "https://example.com/x", "--link", "https://example.com/x"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)

			res := h.run(t, tt.args...)

			if res.code == 0 {
				t.Fatalf("exit = 0, want 非 0\nstdout: %s", res.stdout)
			}
			if res.stderr == "" {
				t.Error("stderr が空")
			}
			if f := h.tasks(t); len(f.Tasks) != 0 {
				t.Errorf("拒否したのにタスクが作られている: %+v", f.Tasks)
			}
		})
	}
}

func TestAddInvalidStatusHintListsColumns(t *testing.T) {
	h := newHarness(t)

	res := h.run(t, "add", "a", "--status", "存在しない列", "--json")

	if res.code == 0 {
		t.Fatal("exit = 0, want 非 0")
	}
	payload := decodeError(t, res.stderr)
	if !strings.Contains(payload.Hint, "working") || !strings.Contains(payload.Hint, "wontfix") {
		t.Errorf("hint = %q, want 有効な列 id の一覧", payload.Hint)
	}
}

func TestListFiltersTerminalColumnsByDefault(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "add", "作業中", "--status", "working")
	h.mustRun(t, "add", "完了", "--status", "done")
	h.mustRun(t, "add", "やらない", "--status", "wontfix")

	res := h.mustRun(t, "list", "--json")
	got := decodeTasks(t, res.stdout)
	if len(got) != 1 || got[0].Title != "作業中" {
		t.Errorf("既定の list = %+v, want 作業中のみ", got)
	}

	res = h.mustRun(t, "list", "--all", "--json")
	if got := decodeTasks(t, res.stdout); len(got) != 3 {
		t.Errorf("--all の list = %d 件, want 3", len(got))
	}

	res = h.mustRun(t, "list", "--status", "done", "--status", "wontfix", "--json")
	got = decodeTasks(t, res.stdout)
	if len(got) != 2 {
		t.Fatalf("--status 指定の list = %+v, want 2 件", got)
	}
	if got[0].Status != "done" || got[1].Status != "wontfix" {
		t.Errorf("並び順 = %q, %q, want 列定義順", got[0].Status, got[1].Status)
	}
}

func TestListOrdersByColumnThenID(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "add", "review 1", "--status", "review")
	h.mustRun(t, "add", "todo 1", "--status", "todo")
	h.mustRun(t, "add", "review 2", "--status", "review")

	res := h.mustRun(t, "list", "--json")

	got := decodeTasks(t, res.stdout)
	wantTitles := []string{"todo 1", "review 1", "review 2"}
	if len(got) != len(wantTitles) {
		t.Fatalf("list = %+v", got)
	}
	for i, want := range wantTitles {
		if got[i].Title != want {
			t.Errorf("list[%d] = %q, want %q", i, got[i].Title, want)
		}
	}
}

func TestListTextOutputShowsUnknownStatus(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "add", "移行漏れ")
	h.writeConfig(t, `
[[columns]]
id = "backlog"
label = "Backlog"
kind = "open"
color = "gray"
`)

	res := h.mustRun(t, "list", "--all")

	if !strings.Contains(res.stdout, "todo") {
		t.Errorf("未定義列のタスクが表示されていない: %q", res.stdout)
	}
}

func TestListEmpty(t *testing.T) {
	h := newHarness(t)

	res := h.mustRun(t, "list")
	if res.stdout == "" {
		t.Error("空一覧で何も出力していない")
	}

	res = h.mustRun(t, "list", "--json")
	if got := decodeTasks(t, res.stdout); len(got) != 0 {
		t.Errorf("--json = %+v, want 空配列", got)
	}
	if !strings.Contains(res.stdout, "[]") {
		t.Errorf("--json = %q, want tasks: []", res.stdout)
	}
}

func TestShow(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "add", "設計", "--note", "1 行目\n2 行目", "--link", "https://github.com/o/r/pull/1")

	res := h.mustRun(t, "show", "1")
	for _, want := range []string{"#1", "設計", "1 行目", "2 行目", "https://github.com/o/r/pull/1", "github_pr"} {
		if !strings.Contains(res.stdout, want) {
			t.Errorf("show の出力に %q が無い:\n%s", want, res.stdout)
		}
	}

	res = h.mustRun(t, "show", "#1", "--json")
	task := decodeTask(t, res.stdout)
	if task.ID != 1 || len(task.Links) != 1 {
		t.Errorf("task = %+v", task)
	}
}

func TestShowUnknownID(t *testing.T) {
	h := newHarness(t)

	res := h.run(t, "show", "42")

	if res.code == 0 {
		t.Fatal("exit = 0, want 非 0")
	}
	if !strings.Contains(res.stderr, "42") {
		t.Errorf("stderr に id が無い: %q", res.stderr)
	}
}

func TestInvalidIDArgument(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "add", "a")

	for _, id := range []string{"abc", "0", "-1", "#", "1.5", "＃1"} {
		res := h.run(t, "show", id)
		if res.code == 0 {
			t.Errorf("show %q が成功した", id)
		}
	}
}

func TestEdit(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "add", "旧タイトル", "--due", "2026-08-31")
	h.now = laterTime

	res := h.mustRun(t, "edit", "1", "--title", "新タイトル", "--status", "review", "--json")

	task := decodeTask(t, res.stdout)
	if task.Title != "新タイトル" || task.Status != "review" {
		t.Errorf("task = %+v", task)
	}
	if task.CreatedAt != model.NewTimestamp(baseTime) {
		t.Errorf("created_at = %q, want 作成時刻のまま", task.CreatedAt)
	}
	if task.UpdatedAt != model.NewTimestamp(laterTime) {
		t.Errorf("updated_at = %q, want %q", task.UpdatedAt, model.NewTimestamp(laterTime))
	}
	if task.Due == nil {
		t.Fatal("due が消えている")
	}

	res = h.mustRun(t, "edit", "1", "--due", "", "--json")
	if task := decodeTask(t, res.stdout); task.Due != nil {
		t.Errorf("--due \"\" 後の due = %v, want nil", task.Due)
	}
}

func TestEditRequiresAtLeastOneFlag(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "add", "a")

	res := h.run(t, "edit", "1", "--json")

	if res.code == 0 {
		t.Fatal("exit = 0, want 非 0")
	}
	payload := decodeError(t, res.stderr)
	if payload.Hint == "" {
		t.Error("hint が空（指定できるフラグを案内していない）")
	}
}

func TestMoveAndDoneAlias(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "add", "a")

	h.mustRun(t, "move", "1", "review")
	if got := h.tasks(t).Tasks[0].Status; got != "review" {
		t.Errorf("status = %q, want review", got)
	}

	h.mustRun(t, "done", "1")
	if got := h.tasks(t).Tasks[0].Status; got != "done" {
		t.Errorf("done alias 後の status = %q, want done", got)
	}

	res := h.run(t, "move", "1", "存在しない列")
	if res.code == 0 {
		t.Error("未定義列への move が成功した")
	}
}

func TestNoteSetAndAppend(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "add", "a")

	h.mustRun(t, "note", "1", "--set", "1 行目")
	if got := h.tasks(t).Tasks[0].Note; got != "1 行目" {
		t.Errorf("note = %q, want 1 行目", got)
	}

	h.mustRun(t, "note", "1", "--append", "2 行目")
	if got := h.tasks(t).Tasks[0].Note; got != "1 行目\n2 行目" {
		t.Errorf("note = %q, want 1 行目\\n2 行目", got)
	}

	res := h.run(t, "note", "1", "--set", "x", "--append", "y")
	if res.code == 0 {
		t.Error("--set と --append の同時指定が成功した")
	}
}

func TestNoteWithoutEditorEnv(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "add", "a")

	res := h.run(t, "note", "1")

	if res.code == 0 {
		t.Fatal("exit = 0, want 非 0（EDITOR 未設定）")
	}
	if !strings.Contains(res.stderr, "EDITOR") {
		t.Errorf("stderr = %q, want EDITOR の案内", res.stderr)
	}
}

// config.toml の editor は環境変数より優先される（herdr の pane には環境変数が届かないことがあるため）。
func TestNoteUsesConfiguredEditorOverEnv(t *testing.T) {
	h := newHarness(t)
	h.writeConfig(t, "editor = \"taskherd-no-such-editor\"\n")
	h.env["EDITOR"] = "true"
	h.mustRun(t, "add", "a")

	res := h.run(t, "note", "1")

	if res.code == 0 {
		t.Fatal("exit = 0, want 非 0（config の editor が起動を試みて失敗する）")
	}
	if !strings.Contains(res.stderr, "taskherd-no-such-editor") {
		t.Errorf("stderr = %q, want config の editor 名", res.stderr)
	}
}

func TestLinkAndUnlink(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "add", "a")
	const url = "https://github.com/o/r/pull/1"

	res := h.mustRun(t, "link", "1", url, "--note", "本体実装", "--json")
	task := decodeTask(t, res.stdout)
	if len(task.Links) != 1 || task.Links[0].Kind != model.LinkKindGitHubPR || task.Links[0].Note != "本体実装" {
		t.Fatalf("links = %+v", task.Links)
	}

	if res := h.run(t, "link", "1", url); res.code == 0 {
		t.Error("同一 URL の再追加が成功した")
	}
	if res := h.run(t, "unlink", "1", "https://github.com/o/r/pull/999"); res.code == 0 {
		t.Error("未登録 URL の unlink が成功した")
	}

	res = h.mustRun(t, "unlink", "1", url, "--json")
	if task := decodeTask(t, res.stdout); len(task.Links) != 0 {
		t.Errorf("unlink 後の links = %+v", task.Links)
	}
}

func TestRemoveWithYes(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "add", "消す")

	res := h.mustRun(t, "rm", "1", "--yes", "--json")

	if task := decodeTask(t, res.stdout); task.ID != 1 {
		t.Errorf("削除したタスク = %+v", task)
	}
	f := h.tasks(t)
	if len(f.Tasks) != 0 {
		t.Errorf("tasks = %+v, want 空", f.Tasks)
	}
	if f.NextID != 2 {
		t.Errorf("next_id = %d, want 2（id を再利用しない）", f.NextID)
	}
}

func TestRemovePrompt(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantTasks int
	}{
		{name: "y で削除する", input: "y\n", wantTasks: 0},
		{name: "yes で削除する", input: "yes\n", wantTasks: 0},
		{name: "n で中止する", input: "n\n", wantTasks: 1},
		{name: "空入力で中止する", input: "\n", wantTasks: 1},
		{name: "EOF で中止する", input: "", wantTasks: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			h.mustRun(t, "add", "消す")
			h.stdinContent = tt.input

			res := h.run(t, "rm", "1")

			if res.code != 0 {
				t.Fatalf("exit = %d, want 0（中止も正常終了）\nstderr: %s", res.code, res.stderr)
			}
			if !h.stdin.read {
				t.Error("確認プロンプトで stdin を読んでいない")
			}
			if got := len(h.tasks(t).Tasks); got != tt.wantTasks {
				t.Errorf("tasks = %d, want %d", got, tt.wantTasks)
			}
		})
	}
}

func TestCorruptTasksFileReportsRecoveryHint(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "add", "a")
	if err := os.WriteFile(filepath.Join(h.stateDir, "tasks.json"), []byte(`{"version":1,`), 0o600); err != nil {
		t.Fatalf("tasks.json を壊せない: %v", err)
	}

	res := h.run(t, "list", "--json")

	if res.code == 0 {
		t.Fatal("exit = 0, want 非 0")
	}
	payload := decodeError(t, res.stderr)
	if !strings.Contains(payload.Hint, "tasks.json.bak") {
		t.Errorf("hint = %q, want .bak からの復旧案内", payload.Hint)
	}
}

func TestVersionMismatchIsRejected(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "add", "a")
	if err := os.WriteFile(filepath.Join(h.stateDir, "tasks.json"), []byte(`{"version":2,"next_id":1,"tasks":[]}`), 0o600); err != nil {
		t.Fatalf("tasks.json を書けない: %v", err)
	}

	res := h.run(t, "add", "b", "--json")

	if res.code == 0 {
		t.Fatal("exit = 0, want 非 0")
	}
	payload := decodeError(t, res.stderr)
	if !strings.Contains(payload.Error, "version") {
		t.Errorf("error = %q, want version 不一致の説明", payload.Error)
	}
	if !strings.Contains(payload.Hint, "taskherd") {
		t.Errorf("hint = %q, want taskherd の更新・移行の案内", payload.Hint)
	}
	if strings.Contains(payload.Hint, "tasks.json.bak") {
		t.Errorf("hint = %q, want .bak 復旧を案内しない", payload.Hint)
	}
}
