package store_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	"github.com/ukwhatn/taskherd/internal/model"
	"github.com/ukwhatn/taskherd/internal/store"
)

// buildCLI builds the real binary so the lock can be exercised across process boundaries;
// goroutines in one process would only prove the in-process path.
func buildCLI(t *testing.T) string {
	t.Helper()

	bin := filepath.Join(t.TempDir(), "taskherd")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/taskherd")
	cmd.Dir = "../.."
	cmd.Env = os.Environ()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("バイナリをビルドできない: %v\n%s", err, out)
	}
	return bin
}

func TestConcurrentAddAcrossProcesses(t *testing.T) {
	if testing.Short() {
		t.Skip("プロセス起動を伴うため -short では実行しない")
	}

	bin := buildCLI(t)
	root := t.TempDir()
	stateHome := filepath.Join(root, "state")
	env := []string{
		"HOME=" + root,
		"XDG_STATE_HOME=" + stateHome,
		"TASKHERD_CONFIG=" + filepath.Join(root, "config.toml"),
	}

	const processes = 16
	type outcome struct {
		id     int
		stderr string
		err    error
	}
	outcomes := make([]outcome, processes)

	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range processes {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			cmd := exec.Command(bin, "add", fmt.Sprintf("並行タスク %d", i), "--json")
			cmd.Env = env
			var stderr []byte
			<-start
			stdout, err := cmd.Output()
			if exitErr, ok := err.(*exec.ExitError); ok {
				stderr = exitErr.Stderr
			}
			if err != nil {
				outcomes[i] = outcome{err: err, stderr: string(stderr)}
				return
			}
			var payload struct {
				Task model.Task `json:"task"`
			}
			if jsonErr := json.Unmarshal(stdout, &payload); jsonErr != nil {
				outcomes[i] = outcome{err: fmt.Errorf("JSON を解析できない: %w (%s)", jsonErr, stdout)}
				return
			}
			outcomes[i] = outcome{id: payload.Task.ID}
		}(i)
	}
	close(start)
	wg.Wait()

	reportedIDs := make(map[int]int, processes)
	for i, got := range outcomes {
		if got.err != nil {
			t.Fatalf("プロセス %d が失敗した: %v\nstderr: %s", i, got.err, got.stderr)
		}
		if prev, dup := reportedIDs[got.id]; dup {
			t.Fatalf("プロセス %d と %d が同じ id %d を報告した", prev, i, got.id)
		}
		reportedIDs[got.id] = i
	}

	f, err := store.New(filepath.Join(stateHome, "taskherd")).Load()
	if err != nil {
		t.Fatalf("tasks.json を読めない: %v", err)
	}
	if len(f.Tasks) != processes {
		t.Fatalf("tasks = %d, want %d（更新の消失）", len(f.Tasks), processes)
	}

	seen := make(map[int]bool, processes)
	titles := make(map[string]bool, processes)
	maxID := 0
	for _, task := range f.Tasks {
		if seen[task.ID] {
			t.Errorf("tasks.json 内で id %d が重複している", task.ID)
		}
		seen[task.ID] = true
		titles[task.Title] = true
		if task.ID > maxID {
			maxID = task.ID
		}
	}
	if f.NextID <= maxID {
		t.Errorf("next_id = %d, want > %d", f.NextID, maxID)
	}
	for i := range processes {
		title := fmt.Sprintf("並行タスク %d", i)
		if !titles[title] {
			t.Errorf("%q が保存されていない", title)
		}
		if !seen[i+1] {
			t.Errorf("id %d が採番されていない（%d プロセスで 1..%d を採番するはず）", i+1, processes, processes)
		}
	}
}
