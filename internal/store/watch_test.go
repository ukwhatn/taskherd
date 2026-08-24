package store_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func waitForEvent(t *testing.T, events <-chan struct{}, what string) {
	t.Helper()
	select {
	case _, ok := <-events:
		if !ok {
			t.Fatalf("%s: イベントチャネルが閉じている", what)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("%s: イベントが届かない", what)
	}
}

func TestWatchNotifiesAfterAtomicRename(t *testing.T) {
	st := newStore(t)

	w, err := st.Watch()
	if err != nil {
		t.Fatalf("Watch() error = %v", err)
	}
	defer func() {
		if err := w.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	}()

	if err := st.Update(context.Background(), addTask("1 件目")); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	waitForEvent(t, w.Events(), "tasks.json の新規作成")

	// From the second write on, the rename replaces an existing file and swaps the inode.
	if err := st.Update(context.Background(), addTask("2 件目")); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	waitForEvent(t, w.Events(), "rename 上書きによる更新")

	f, err := st.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(f.Tasks) != 2 {
		t.Errorf("再読込結果の tasks = %d, want 2", len(f.Tasks))
	}

	select {
	case err := <-w.Errors():
		t.Fatalf("watch エラー: %v", err)
	default:
	}
}

func TestWatchIgnoresUnrelatedFiles(t *testing.T) {
	st := newStore(t)
	if err := os.MkdirAll(st.Dir(), 0o700); err != nil {
		t.Fatalf("ディレクトリを作れない: %v", err)
	}

	w, err := st.Watch()
	if err != nil {
		t.Fatalf("Watch() error = %v", err)
	}
	defer func() {
		if err := w.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	}()

	if err := os.WriteFile(filepath.Join(st.Dir(), "cache.json"), []byte("{}"), 0o600); err != nil {
		t.Fatalf("cache.json を書けない: %v", err)
	}

	select {
	case <-w.Events():
		t.Error("tasks.json 以外の変更でイベントが届いた")
	case <-time.After(500 * time.Millisecond):
	}
}

func TestWatchClosesEventChannel(t *testing.T) {
	st := newStore(t)

	w, err := st.Watch()
	if err != nil {
		t.Fatalf("Watch() error = %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	select {
	case _, ok := <-w.Events():
		if ok {
			t.Error("Close() 後にイベントが届いた")
		}
	case <-time.After(2 * time.Second):
		t.Error("Close() 後もイベントチャネルが閉じない")
	}

	if err := w.Close(); err != nil {
		t.Errorf("2 回目の Close() error = %v, want nil", err)
	}
}
