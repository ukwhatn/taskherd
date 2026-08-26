package model_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/ukwhatn/taskherd/internal/model"
)

func TestParseFileVersion(t *testing.T) {
	tests := []struct {
		name    string
		version string
		wantGot int
	}{
		{name: "未来の version は拒否する", version: "2", wantGot: 2},
		{name: "version 0 は拒否する", version: "0", wantGot: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := []byte(`{"version": ` + tt.version + `, "next_id": 1, "tasks": []}`)

			_, err := model.ParseFile(data)

			var mismatch *model.VersionMismatchError
			if !errors.As(err, &mismatch) {
				t.Fatalf("ParseFile() error = %v, want *VersionMismatchError", err)
			}
			if mismatch.Got != tt.wantGot || mismatch.Want != model.CurrentVersion {
				t.Errorf("mismatch = %+v, want Got=%d Want=%d", mismatch, tt.wantGot, model.CurrentVersion)
			}
		})
	}
}

func TestParseFileValidationViolations(t *testing.T) {
	tests := []struct {
		name      string
		data      string
		wantPaths []string
	}{
		{
			name: "id の重複",
			data: `{"version":1,"next_id":9,"tasks":[
				{"id":3,"title":"a","status":"todo","due":null,"note":"","created_at":"2026-08-24T16:00:00+09:00","updated_at":"2026-08-24T16:00:00+09:00"},
				{"id":3,"title":"b","status":"todo","due":null,"note":"","created_at":"2026-08-24T16:00:00+09:00","updated_at":"2026-08-24T16:00:00+09:00"}]}`,
			wantPaths: []string{"tasks[1].id"},
		},
		{
			name: "id が 0",
			data: `{"version":1,"next_id":9,"tasks":[
				{"id":0,"title":"a","status":"todo","due":null,"note":"","created_at":"2026-08-24T16:00:00+09:00","updated_at":"2026-08-24T16:00:00+09:00"}]}`,
			wantPaths: []string{"tasks[0].id"},
		},
		{
			name: "id が負",
			data: `{"version":1,"next_id":9,"tasks":[
				{"id":-1,"title":"a","status":"todo","due":null,"note":"","created_at":"2026-08-24T16:00:00+09:00","updated_at":"2026-08-24T16:00:00+09:00"}]}`,
			wantPaths: []string{"tasks[0].id"},
		},
		{
			name: "next_id が max(id) 以下",
			data: `{"version":1,"next_id":3,"tasks":[
				{"id":3,"title":"a","status":"todo","due":null,"note":"","created_at":"2026-08-24T16:00:00+09:00","updated_at":"2026-08-24T16:00:00+09:00"}]}`,
			wantPaths: []string{"next_id"},
		},
		{
			name:      "タスクが空でも next_id は正でなければならない",
			data:      `{"version":1,"next_id":0,"tasks":[]}`,
			wantPaths: []string{"next_id"},
		},
		{
			name: "created_at が RFC 3339 でない",
			data: `{"version":1,"next_id":2,"tasks":[
				{"id":1,"title":"a","status":"todo","due":null,"note":"","created_at":"2026-08-24","updated_at":"2026-08-24T16:00:00+09:00"}]}`,
			wantPaths: []string{"tasks[0].created_at"},
		},
		{
			name: "updated_at が空",
			data: `{"version":1,"next_id":2,"tasks":[
				{"id":1,"title":"a","status":"todo","due":null,"note":"","created_at":"2026-08-24T16:00:00+09:00","updated_at":""}]}`,
			wantPaths: []string{"tasks[0].updated_at"},
		},
		{
			name: "due が RFC 3339 タイムスタンプ",
			data: `{"version":1,"next_id":2,"tasks":[
				{"id":1,"title":"a","status":"todo","due":"2026-08-31T00:00:00+09:00","note":"","created_at":"2026-08-24T16:00:00+09:00","updated_at":"2026-08-24T16:00:00+09:00"}]}`,
			wantPaths: []string{"tasks[0].due"},
		},
		{
			name: "due の日付が存在しない",
			data: `{"version":1,"next_id":2,"tasks":[
				{"id":1,"title":"a","status":"todo","due":"2026-02-30","note":"","created_at":"2026-08-24T16:00:00+09:00","updated_at":"2026-08-24T16:00:00+09:00"}]}`,
			wantPaths: []string{"tasks[0].due"},
		},
		{
			name: "links[].added_at が不正",
			data: `{"version":1,"next_id":2,"tasks":[
				{"id":1,"title":"a","status":"todo","due":null,"note":"","created_at":"2026-08-24T16:00:00+09:00","updated_at":"2026-08-24T16:00:00+09:00",
				 "links":[{"url":"https://example.com","kind":"other","note":"","added_at":"きのう"}]}]}`,
			wantPaths: []string{"tasks[0].links[0].added_at"},
		},
		{
			name: "sessions[].linked_at が不正",
			data: `{"version":1,"next_id":2,"tasks":[
				{"id":1,"title":"a","status":"todo","due":null,"note":"","created_at":"2026-08-24T16:00:00+09:00","updated_at":"2026-08-24T16:00:00+09:00",
				 "sessions":[{"agent":"claude","session_id":"u-1","cwd":"/tmp","label":"","linked_at":"2026-08-24 16:00:00"}]}]}`,
			wantPaths: []string{"tasks[0].sessions[0].linked_at"},
		},
		{
			name: "違反が複数あれば全件報告する",
			data: `{"version":1,"next_id":1,"tasks":[
				{"id":5,"title":"a","status":"todo","due":"31/08/2026","note":"","created_at":"2026-08-24T16:00:00+09:00","updated_at":"2026-08-24T16:00:00+09:00"}]}`,
			wantPaths: []string{"next_id", "tasks[0].due"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := model.ParseFile([]byte(tt.data))

			var invalid *model.ValidationError
			if !errors.As(err, &invalid) {
				t.Fatalf("ParseFile() error = %v, want *ValidationError", err)
			}
			if len(invalid.Violations) != len(tt.wantPaths) {
				t.Fatalf("違反件数 = %d (%v), want %d (%v)", len(invalid.Violations), invalid.Violations, len(tt.wantPaths), tt.wantPaths)
			}
			for i, want := range tt.wantPaths {
				if invalid.Violations[i].Path != want {
					t.Errorf("Violations[%d].Path = %q, want %q", i, invalid.Violations[i].Path, want)
				}
				if invalid.Violations[i].Text(nil) == "" {
					t.Errorf("Violations[%d] のメッセージが空", i)
				}
				if !strings.Contains(err.Error(), want) {
					t.Errorf("Error() = %q に違反箇所 %q が含まれていない", err.Error(), want)
				}
			}
		})
	}
}

func TestParseFileAccepts(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{name: "空のタスク配列", data: `{"version":1,"next_id":1,"tasks":[]}`},
		{
			name: "未知の status も読み込みは通す",
			data: `{"version":1,"next_id":2,"tasks":[
				{"id":1,"title":"a","status":"消えた列","due":null,"note":"","created_at":"2026-08-24T16:00:00+09:00","updated_at":"2026-08-24T16:00:00+09:00"}]}`,
		},
		{
			name: "UTC 表記・秒未満を含むタイムスタンプ",
			data: `{"version":1,"next_id":2,"tasks":[
				{"id":1,"title":"a","status":"todo","due":"2024-02-29","note":"","created_at":"2026-08-24T07:00:00Z","updated_at":"2026-08-24T07:00:00.512Z"}]}`,
		},
		{
			name: "CJK を含む title・note",
			data: `{"version":1,"next_id":2,"tasks":[
				{"id":1,"title":"herdr タスク管理","status":"todo","due":null,"note":"改行\nを含む自由記述","created_at":"2026-08-24T16:00:00+09:00","updated_at":"2026-08-24T16:00:00+09:00"}]}`,
		},
		{
			name: "links / sessions を省略した最小形",
			data: `{"version":1,"next_id":2,"tasks":[
				{"id":1,"title":"a","status":"todo","note":"","created_at":"2026-08-24T16:00:00+09:00","updated_at":"2026-08-24T16:00:00+09:00"}]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := model.ParseFile([]byte(tt.data))
			if err != nil {
				t.Fatalf("ParseFile() error = %v, want nil", err)
			}
			for i, task := range f.Tasks {
				if task.Links == nil {
					t.Errorf("tasks[%d].Links = nil, want 空スライス", i)
				}
				if task.Sessions == nil {
					t.Errorf("tasks[%d].Sessions = nil, want 空スライス", i)
				}
			}
		})
	}
}

func TestParseFileRejectsBrokenJSON(t *testing.T) {
	if _, err := model.ParseFile([]byte("{ this is not json")); err == nil {
		t.Fatal("ParseFile() error = nil, want パースエラー")
	}
}
