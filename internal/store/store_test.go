package store_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gofrs/flock"
	"github.com/ukwhatn/taskherd/internal/model"
	"github.com/ukwhatn/taskherd/internal/store"
)

var testNow = time.Date(2026, 8, 24, 16, 0, 0, 0, time.FixedZone("JST", 9*60*60))

func addTask(title string) func(*model.File) error {
	return func(f *model.File) error {
		_, err := f.AddTask(model.TaskInput{Title: title, Status: "todo"}, testNow)
		return err
	}
}

func newStore(t *testing.T) *store.Store {
	t.Helper()
	return store.New(filepath.Join(t.TempDir(), "taskherd"))
}

func TestLoadMissingFileReturnsEmptyFile(t *testing.T) {
	st := newStore(t)

	f, err := st.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if f.Version != model.CurrentVersion || f.NextID != 1 || len(f.Tasks) != 0 {
		t.Errorf("Load() = %+v, want 空ファイル相当", f)
	}
	if _, err := os.Stat(st.TasksPath()); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Load() が tasks.json を作成している: %v", err)
	}
}

func TestUpdateCreatesFilesWithRestrictedPermissions(t *testing.T) {
	st := newStore(t)

	if err := st.Update(context.Background(), addTask("設計")); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	dirInfo, err := os.Stat(st.Dir())
	if err != nil {
		t.Fatalf("データディレクトリを stat できない: %v", err)
	}
	if perm := dirInfo.Mode().Perm(); perm != 0o700 {
		t.Errorf("データディレクトリの権限 = %o, want 700", perm)
	}

	for _, path := range []string{st.TasksPath(), st.LockPath()} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("%s を stat できない: %v", path, err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("%s の権限 = %o, want 600", filepath.Base(path), perm)
		}
	}

	f, err := st.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(f.Tasks) != 1 || f.Tasks[0].Title != "設計" || f.NextID != 2 {
		t.Errorf("Load() = %+v", f)
	}
}

func TestUpdateBacksUpPreviousContent(t *testing.T) {
	st := newStore(t)

	if err := st.Update(context.Background(), addTask("1 件目")); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if _, err := os.Stat(st.BakPath()); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("初回書き込みで .bak が作られている: %v", err)
	}
	firstGen, err := os.ReadFile(st.TasksPath())
	if err != nil {
		t.Fatalf("tasks.json を読めない: %v", err)
	}

	if err := st.Update(context.Background(), addTask("2 件目")); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	bak, err := os.ReadFile(st.BakPath())
	if err != nil {
		t.Fatalf(".bak を読めない: %v", err)
	}
	if string(bak) != string(firstGen) {
		t.Errorf(".bak の内容が書き込み前の tasks.json と一致しない:\n%s", bak)
	}
	info, err := os.Stat(st.BakPath())
	if err != nil {
		t.Fatalf(".bak を stat できない: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf(".bak の権限 = %o, want 600", perm)
	}

	f, err := st.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(f.Tasks) != 2 {
		t.Errorf("tasks = %d, want 2", len(f.Tasks))
	}
}

func TestUpdateRefusesWriteOnInvalidFile(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{
			name:    "version 不一致",
			content: `{"version":2,"next_id":1,"tasks":[]}`,
		},
		{
			name: "id 重複",
			content: `{"version":1,"next_id":9,"tasks":[
				{"id":3,"title":"a","status":"todo","due":null,"note":"","links":[],"sessions":[],"created_at":"2026-08-24T16:00:00+09:00","updated_at":"2026-08-24T16:00:00+09:00"},
				{"id":3,"title":"b","status":"todo","due":null,"note":"","links":[],"sessions":[],"created_at":"2026-08-24T16:00:00+09:00","updated_at":"2026-08-24T16:00:00+09:00"}]}`,
		},
		{
			name: "next_id が max(id) 以下",
			content: `{"version":1,"next_id":1,"tasks":[
				{"id":4,"title":"a","status":"todo","due":null,"note":"","links":[],"sessions":[],"created_at":"2026-08-24T16:00:00+09:00","updated_at":"2026-08-24T16:00:00+09:00"}]}`,
		},
		{
			name:    "JSON として壊れている",
			content: `{"version":1,`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			if err := os.MkdirAll(st.Dir(), 0o700); err != nil {
				t.Fatalf("ディレクトリを作れない: %v", err)
			}
			if err := os.WriteFile(st.TasksPath(), []byte(tt.content), 0o600); err != nil {
				t.Fatalf("tasks.json を書けない: %v", err)
			}

			if err := st.Update(context.Background(), addTask("追加")); err == nil {
				t.Fatal("Update() error = nil, want 書き込み拒否")
			}
			if _, err := st.Load(); err == nil {
				t.Error("Load() error = nil, want 検証エラー")
			}

			after, err := os.ReadFile(st.TasksPath())
			if err != nil {
				t.Fatalf("tasks.json を読めない: %v", err)
			}
			if string(after) != tt.content {
				t.Errorf("拒否したのに tasks.json が書き換わっている:\n%s", after)
			}
			if _, err := os.Stat(st.BakPath()); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("拒否したのに .bak が作られている: %v", err)
			}
		})
	}
}

func TestLoadReportsRecoveryHintOnCorruptFile(t *testing.T) {
	st := newStore(t)
	if err := os.MkdirAll(st.Dir(), 0o700); err != nil {
		t.Fatalf("ディレクトリを作れない: %v", err)
	}
	if err := os.WriteFile(st.TasksPath(), []byte(`{"version":1,`), 0o600); err != nil {
		t.Fatalf("tasks.json を書けない: %v", err)
	}

	_, err := st.Load()

	var corrupt *store.CorruptError
	if !errors.As(err, &corrupt) {
		t.Fatalf("Load() error = %v, want *CorruptError", err)
	}
	if corrupt.Hint() == "" {
		t.Error("Hint() が空（.bak からの復旧手順を案内していない）")
	}
}

func TestUpdateRefusesWriteWhenCallbackBreaksInvariants(t *testing.T) {
	st := newStore(t)
	for _, title := range []string{"1 件目", "2 件目"} {
		if err := st.Update(context.Background(), addTask(title)); err != nil {
			t.Fatalf("Update() error = %v", err)
		}
	}
	tasksBefore, err := os.ReadFile(st.TasksPath())
	if err != nil {
		t.Fatalf("tasks.json を読めない: %v", err)
	}
	bakBefore, err := os.ReadFile(st.BakPath())
	if err != nil {
		t.Fatalf(".bak を読めない: %v", err)
	}

	err = st.Update(context.Background(), func(f *model.File) error {
		f.Tasks[1].ID = f.Tasks[0].ID
		return nil
	})

	var invalid *model.ValidationError
	if !errors.As(err, &invalid) {
		t.Fatalf("Update() error = %v, want *ValidationError", err)
	}
	if len(invalid.Violations) != 1 || invalid.Violations[0].Path != "tasks[1].id" {
		t.Errorf("違反 = %v, want tasks[1].id の 1 件", invalid.Violations)
	}

	tasksAfter, err := os.ReadFile(st.TasksPath())
	if err != nil {
		t.Fatalf("tasks.json を読めない: %v", err)
	}
	if string(tasksAfter) != string(tasksBefore) {
		t.Errorf("不正な変更が書き込まれている:\n%s", tasksAfter)
	}
	bakAfter, err := os.ReadFile(st.BakPath())
	if err != nil {
		t.Fatalf(".bak を読めない: %v", err)
	}
	if string(bakAfter) != string(bakBefore) {
		t.Error("書き込みを拒否したのに .bak が更新されている（検証は .bak 退避より前に行う）")
	}
}

func TestLoadVersionMismatchHintDoesNotAdviseBackupRecovery(t *testing.T) {
	st := newStore(t)
	if err := os.MkdirAll(st.Dir(), 0o700); err != nil {
		t.Fatalf("ディレクトリを作れない: %v", err)
	}
	if err := os.WriteFile(st.TasksPath(), []byte(`{"version":2,"next_id":1,"tasks":[]}`), 0o600); err != nil {
		t.Fatalf("tasks.json を書けない: %v", err)
	}

	_, err := st.Load()

	var unsupported *store.UnsupportedVersionError
	if !errors.As(err, &unsupported) {
		t.Fatalf("Load() error = %v, want *UnsupportedVersionError", err)
	}
	var corrupt *store.CorruptError
	if errors.As(err, &corrupt) {
		t.Error("version 不一致が CorruptError として扱われている（.bak 復旧を誤案内する）")
	}
	var mismatch *model.VersionMismatchError
	if !errors.As(err, &mismatch) {
		t.Errorf("Unwrap で *VersionMismatchError に到達できない: %v", err)
	}

	hint := unsupported.Hint()
	if hint == "" {
		t.Fatal("Hint() が空")
	}
	if strings.Contains(hint, "tasks.json.bak") {
		t.Errorf("hint = %q, want .bak 復旧を案内しない（新しいファイルを古いバイナリで読んでいる状況）", hint)
	}
	if !strings.Contains(hint, "taskherd") {
		t.Errorf("hint = %q, want taskherd の更新・移行の案内", hint)
	}
}

func TestUpdatePropagatesCallbackErrorWithoutWriting(t *testing.T) {
	st := newStore(t)
	if err := st.Update(context.Background(), addTask("既存")); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	before, err := os.ReadFile(st.TasksPath())
	if err != nil {
		t.Fatalf("tasks.json を読めない: %v", err)
	}

	sentinel := errors.New("呼び出し側の都合で中断")
	err = st.Update(context.Background(), func(f *model.File) error {
		if _, addErr := f.AddTask(model.TaskInput{Title: "書かれてはいけない", Status: "todo"}, testNow); addErr != nil {
			return addErr
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("Update() error = %v, want %v", err, sentinel)
	}

	after, err := os.ReadFile(st.TasksPath())
	if err != nil {
		t.Fatalf("tasks.json を読めない: %v", err)
	}
	if string(after) != string(before) {
		t.Errorf("コールバック失敗時に書き込まれている:\n%s", after)
	}
}

func TestUpdateLeavesNoTemporaryFiles(t *testing.T) {
	st := newStore(t)
	for _, title := range []string{"1", "2", "3"} {
		if err := st.Update(context.Background(), addTask(title)); err != nil {
			t.Fatalf("Update() error = %v", err)
		}
	}

	entries, err := os.ReadDir(st.Dir())
	if err != nil {
		t.Fatalf("ディレクトリを読めない: %v", err)
	}
	got := make([]string, 0, len(entries))
	for _, e := range entries {
		got = append(got, e.Name())
	}
	sort.Strings(got)
	want := []string{"tasks.json", "tasks.json.bak", "tasks.lock"}
	if len(got) != len(want) {
		t.Fatalf("ディレクトリ内 = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("ディレクトリ内 = %v, want %v", got, want)
			break
		}
	}
}

func TestUpdateSerializesConcurrentUpdates(t *testing.T) {
	st := newStore(t)
	const workers = 20

	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs <- st.Update(context.Background(), addTask("並行"+string(rune('A'+i))))
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("Update() error = %v", err)
		}
	}

	f, err := st.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(f.Tasks) != workers {
		t.Fatalf("tasks = %d, want %d（更新の消失）", len(f.Tasks), workers)
	}
	seen := make(map[int]bool, workers)
	maxID := 0
	for _, task := range f.Tasks {
		if seen[task.ID] {
			t.Fatalf("id %d が重複している", task.ID)
		}
		seen[task.ID] = true
		if task.ID > maxID {
			maxID = task.ID
		}
	}
	if f.NextID <= maxID {
		t.Errorf("next_id = %d, want > %d", f.NextID, maxID)
	}
}

func TestUpdateFailsWhenLockHeldWhileLoadStaysAvailable(t *testing.T) {
	st := store.New(filepath.Join(t.TempDir(), "taskherd"), store.WithLockTimeout(100*time.Millisecond))
	if err := st.Update(context.Background(), addTask("既存")); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	holder := flock.New(st.LockPath())
	locked, err := holder.TryLock()
	if err != nil || !locked {
		t.Fatalf("テスト側でロックを取得できない: locked=%v err=%v", locked, err)
	}
	defer func() {
		if err := holder.Unlock(); err != nil {
			t.Errorf("Unlock() error = %v", err)
		}
	}()

	if err := st.Update(context.Background(), addTask("追加")); err == nil {
		t.Error("Update() error = nil, want ロック待ちタイムアウト")
	}

	f, err := st.Load()
	if err != nil {
		t.Fatalf("ロック保持中の Load() error = %v（読み込みは lock-free でなければならない）", err)
	}
	if len(f.Tasks) != 1 {
		t.Errorf("tasks = %d, want 1", len(f.Tasks))
	}
}
