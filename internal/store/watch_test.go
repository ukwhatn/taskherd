package store_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/ukwhatn/taskherd/internal/store"
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

func TestWatchNotifiesOnExternalProcessWrite(t *testing.T) {
	if testing.Short() {
		t.Skip("プロセス起動を伴うため -short では実行しない")
	}

	bin := buildCLI(t)
	root := t.TempDir()
	stateHome := filepath.Join(root, "state")
	st := store.New(filepath.Join(stateHome, "taskherd"))

	w, err := st.Watch()
	if err != nil {
		t.Fatalf("Watch() error = %v", err)
	}
	defer func() {
		if err := w.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	}()

	for i, title := range []string{"外部プロセス 1 件目", "外部プロセス 2 件目"} {
		cmd := exec.Command(bin, "add", title)
		cmd.Env = []string{
			"HOME=" + root,
			"XDG_STATE_HOME=" + stateHome,
			"TASKHERD_CONFIG=" + filepath.Join(root, "config.toml"),
		}
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("add に失敗した: %v\n%s", err, out)
		}
		waitForEvent(t, w.Events(), fmt.Sprintf("外部プロセスによる %d 回目の書き込み", i+1))
	}

	f, err := st.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(f.Tasks) != 2 {
		t.Errorf("再読込結果の tasks = %d, want 2", len(f.Tasks))
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
