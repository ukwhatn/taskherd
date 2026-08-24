package model_test

import (
	"errors"
	"testing"
	"time"

	"github.com/ukwhatn/taskherd/internal/model"
)

func at(t *testing.T, rfc3339 string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, rfc3339)
	if err != nil {
		t.Fatalf("テスト時刻の解析に失敗: %v", err)
	}
	return parsed
}

func TestFileAddTask(t *testing.T) {
	now := at(t, "2026-08-24T16:00:00+09:00")
	f := model.NewFile()

	first, err := f.AddTask(model.TaskInput{Title: "設計", Status: "todo"}, now)
	if err != nil {
		t.Fatalf("AddTask() error = %v", err)
	}
	if first.ID != 1 {
		t.Errorf("1 件目の id = %d, want 1", first.ID)
	}
	if f.NextID != 2 {
		t.Errorf("next_id = %d, want 2", f.NextID)
	}
	if first.CreatedAt != "2026-08-24T16:00:00+09:00" || first.UpdatedAt != first.CreatedAt {
		t.Errorf("created_at/updated_at = %q/%q, want 2026-08-24T16:00:00+09:00", first.CreatedAt, first.UpdatedAt)
	}
	if first.Links == nil || first.Sessions == nil {
		t.Error("links/sessions が nil で初期化されている")
	}

	second, err := f.AddTask(model.TaskInput{Title: "実装", Status: "working"}, now)
	if err != nil {
		t.Fatalf("AddTask() error = %v", err)
	}
	if second.ID != 2 || f.NextID != 3 {
		t.Errorf("2 件目の id = %d / next_id = %d, want 2 / 3", second.ID, f.NextID)
	}
}

func TestFileAddTaskUsesNextIDEvenWithGaps(t *testing.T) {
	now := at(t, "2026-08-24T16:00:00+09:00")
	f := model.NewFile()
	f.NextID = 10

	task, err := f.AddTask(model.TaskInput{Title: "a", Status: "todo"}, now)
	if err != nil {
		t.Fatalf("AddTask() error = %v", err)
	}
	if task.ID != 10 || f.NextID != 11 {
		t.Errorf("id = %d / next_id = %d, want 10 / 11", task.ID, f.NextID)
	}
}

func TestFileAddTaskRejectsEmptyTitle(t *testing.T) {
	now := at(t, "2026-08-24T16:00:00+09:00")
	f := model.NewFile()

	for _, title := range []string{"", "   ", "\t\n"} {
		if _, err := f.AddTask(model.TaskInput{Title: title, Status: "todo"}, now); !errors.Is(err, model.ErrEmptyTitle) {
			t.Errorf("AddTask(%q) error = %v, want ErrEmptyTitle", title, err)
		}
	}
	if len(f.Tasks) != 0 || f.NextID != 1 {
		t.Errorf("拒否時にファイルが変更されている: tasks=%d next_id=%d", len(f.Tasks), f.NextID)
	}
}

func TestFileRemoveTaskKeepsNextID(t *testing.T) {
	now := at(t, "2026-08-24T16:00:00+09:00")
	f := model.NewFile()
	if _, err := f.AddTask(model.TaskInput{Title: "a", Status: "todo"}, now); err != nil {
		t.Fatalf("AddTask() error = %v", err)
	}

	removed, err := f.RemoveTask(1)
	if err != nil {
		t.Fatalf("RemoveTask() error = %v", err)
	}
	if removed.Title != "a" {
		t.Errorf("removed.Title = %q, want a", removed.Title)
	}
	if len(f.Tasks) != 0 {
		t.Errorf("tasks = %d, want 0", len(f.Tasks))
	}
	if f.NextID != 2 {
		t.Errorf("next_id = %d, want 2（削除で id を再利用しない）", f.NextID)
	}

	if _, err := f.RemoveTask(1); !errors.Is(err, model.ErrTaskNotFound) {
		t.Errorf("2 回目の RemoveTask() error = %v, want ErrTaskNotFound", err)
	}
}

func TestFileTaskLookup(t *testing.T) {
	now := at(t, "2026-08-24T16:00:00+09:00")
	f := model.NewFile()
	if _, err := f.AddTask(model.TaskInput{Title: "a", Status: "todo"}, now); err != nil {
		t.Fatalf("AddTask() error = %v", err)
	}

	task, err := f.Task(1)
	if err != nil {
		t.Fatalf("Task() error = %v", err)
	}
	task.Title = "書き換え"
	if f.Tasks[0].Title != "書き換え" {
		t.Error("Task() がコピーを返しており、変更がファイルに反映されない")
	}

	if _, err := f.Task(99); !errors.Is(err, model.ErrTaskNotFound) {
		t.Errorf("Task(99) error = %v, want ErrTaskNotFound", err)
	}
}

func TestTaskSetters(t *testing.T) {
	created := at(t, "2026-08-24T16:00:00+09:00")
	updated := at(t, "2026-08-25T09:30:00+09:00")
	f := model.NewFile()
	task, err := f.AddTask(model.TaskInput{Title: "a", Status: "todo"}, created)
	if err != nil {
		t.Fatalf("AddTask() error = %v", err)
	}

	if err := task.SetTitle("b", updated); err != nil {
		t.Fatalf("SetTitle() error = %v", err)
	}
	if task.Title != "b" {
		t.Errorf("Title = %q, want b", task.Title)
	}
	if task.UpdatedAt != "2026-08-25T09:30:00+09:00" {
		t.Errorf("UpdatedAt = %q, want 2026-08-25T09:30:00+09:00", task.UpdatedAt)
	}
	if task.CreatedAt != "2026-08-24T16:00:00+09:00" {
		t.Errorf("CreatedAt が書き換わっている: %q", task.CreatedAt)
	}

	if err := task.SetTitle("  ", updated); !errors.Is(err, model.ErrEmptyTitle) {
		t.Errorf("SetTitle(空白) error = %v, want ErrEmptyTitle", err)
	}

	due := model.Date("2026-08-31")
	task.SetDue(&due, updated)
	if task.Due == nil || *task.Due != due {
		t.Errorf("Due = %v, want %q", task.Due, due)
	}
	task.SetDue(nil, updated)
	if task.Due != nil {
		t.Errorf("Due = %v, want nil", task.Due)
	}

	if err := task.SetStatus("review", updated); err != nil {
		t.Fatalf("SetStatus() error = %v", err)
	}
	if task.Status != "review" {
		t.Errorf("Status = %q, want review", task.Status)
	}
	if err := task.SetStatus("", updated); !errors.Is(err, model.ErrEmptyStatus) {
		t.Errorf("SetStatus(空) error = %v, want ErrEmptyStatus", err)
	}
}

func TestTaskNoteEditing(t *testing.T) {
	now := at(t, "2026-08-24T16:00:00+09:00")
	f := model.NewFile()
	task, err := f.AddTask(model.TaskInput{Title: "a", Status: "todo"}, now)
	if err != nil {
		t.Fatalf("AddTask() error = %v", err)
	}

	task.AppendNote("1 行目", now)
	if task.Note != "1 行目" {
		t.Errorf("空 note への追記 = %q, want 1 行目", task.Note)
	}

	task.AppendNote("2 行目", now)
	if task.Note != "1 行目\n2 行目" {
		t.Errorf("追記結果 = %q, want 1 行目\\n2 行目", task.Note)
	}

	task.SetNote("上書き", now)
	if task.Note != "上書き" {
		t.Errorf("SetNote 後 = %q, want 上書き", task.Note)
	}

	task.SetNote("", now)
	if task.Note != "" {
		t.Errorf("SetNote(空) 後 = %q, want 空", task.Note)
	}
}

func TestTaskLinks(t *testing.T) {
	now := at(t, "2026-08-24T16:00:00+09:00")
	later := at(t, "2026-08-24T17:00:00+09:00")
	f := model.NewFile()
	task, err := f.AddTask(model.TaskInput{Title: "a", Status: "todo"}, now)
	if err != nil {
		t.Fatalf("AddTask() error = %v", err)
	}

	const prURL = "https://github.com/owner/repo/pull/1"
	link, err := task.AddLink(prURL, model.LinkKindGitHubPR, "本体実装", now)
	if err != nil {
		t.Fatalf("AddLink() error = %v", err)
	}
	if link.Kind != model.LinkKindGitHubPR || link.Note != "本体実装" || link.AddedAt != "2026-08-24T16:00:00+09:00" {
		t.Errorf("link = %+v", link)
	}

	if _, err := task.AddLink(prURL, model.LinkKindGitHubPR, "", later); !errors.Is(err, model.ErrLinkExists) {
		t.Errorf("同一 URL の再追加 error = %v, want ErrLinkExists", err)
	}
	if len(task.Links) != 1 {
		t.Fatalf("links = %d, want 1", len(task.Links))
	}

	if _, err := task.RemoveLink("https://github.com/owner/repo/pull/2", later); !errors.Is(err, model.ErrLinkNotFound) {
		t.Errorf("未登録 URL の RemoveLink error = %v, want ErrLinkNotFound", err)
	}

	removed, err := task.RemoveLink(prURL, later)
	if err != nil {
		t.Fatalf("RemoveLink() error = %v", err)
	}
	if removed.URL != prURL {
		t.Errorf("removed.URL = %q, want %q", removed.URL, prURL)
	}
	if len(task.Links) != 0 {
		t.Errorf("links = %d, want 0", len(task.Links))
	}
	if task.UpdatedAt != "2026-08-24T17:00:00+09:00" {
		t.Errorf("UpdatedAt = %q, want 2026-08-24T17:00:00+09:00", task.UpdatedAt)
	}
}

func TestParseDate(t *testing.T) {
	tests := []struct {
		in      string
		wantErr bool
	}{
		{in: "2026-08-31"},
		{in: "2024-02-29"},
		{in: "2026-02-30", wantErr: true},
		{in: "2026-8-3", wantErr: true},
		{in: "2026/08/31", wantErr: true},
		{in: "2026-08-31T00:00:00+09:00", wantErr: true},
		{in: "", wantErr: true},
		{in: "きょう", wantErr: true},
		{in: "2026-08-31 ", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := model.ParseDate(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseDate(%q) error = nil, want エラー", tt.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseDate(%q) error = %v", tt.in, err)
			}
			if string(got) != tt.in {
				t.Errorf("ParseDate(%q) = %q", tt.in, got)
			}
		})
	}
}

func TestFileMarshalRoundTrip(t *testing.T) {
	now := at(t, "2026-08-24T16:00:00+09:00")
	f := model.NewFile()
	task, err := f.AddTask(model.TaskInput{Title: "設計", Status: "working", Note: "メモ"}, now)
	if err != nil {
		t.Fatalf("AddTask() error = %v", err)
	}
	due := model.Date("2026-08-31")
	task.SetDue(&due, now)
	if _, err := task.AddLink("https://github.com/o/r/pull/1", model.LinkKindGitHubPR, "", now); err != nil {
		t.Fatalf("AddLink() error = %v", err)
	}

	data, err := model.MarshalFile(f)
	if err != nil {
		t.Fatalf("MarshalFile() error = %v", err)
	}
	if data[len(data)-1] != '\n' {
		t.Error("MarshalFile() の出力が改行で終わっていない")
	}

	parsed, err := model.ParseFile(data)
	if err != nil {
		t.Fatalf("ParseFile() error = %v", err)
	}
	if parsed.NextID != f.NextID || len(parsed.Tasks) != 1 {
		t.Fatalf("round trip 後 = %+v", parsed)
	}
	got := parsed.Tasks[0]
	if got.Title != "設計" || got.Status != "working" || got.Note != "メモ" || got.Due == nil || *got.Due != due {
		t.Errorf("round trip 後のタスク = %+v", got)
	}
	if len(got.Links) != 1 || got.Links[0].Kind != model.LinkKindGitHubPR {
		t.Errorf("round trip 後の links = %+v", got.Links)
	}
}
