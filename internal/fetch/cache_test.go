package fetch_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ukwhatn/taskherd/internal/fetch"
)

func TestCacheUpdateSetSuccessThenLoad(t *testing.T) {
	dir := t.TempDir()
	c := fetch.NewCache(dir)
	now := time.Date(2026, 8, 24, 16, 40, 0, 0, time.UTC)

	err := c.Update(context.Background(), func(f *fetch.CacheFile) {
		if setErr := f.SetSuccess("https://github.com/o/r/pull/1", fetch.GitHubData{State: "OPEN"}, now); setErr != nil {
			t.Fatalf("SetSuccess() error = %v", setErr)
		}
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	f := c.Load()
	entry, ok := f.Get("https://github.com/o/r/pull/1")
	if !ok {
		t.Fatal("エントリが無い")
	}
	if !entry.OK || entry.Error != "" {
		t.Errorf("entry = %+v, want OK=true error=\"\"", entry)
	}
	if entry.FetchedAt == nil || *entry.FetchedAt != now.Format(time.RFC3339) {
		t.Errorf("FetchedAt = %v, want %s", entry.FetchedAt, now.Format(time.RFC3339))
	}
	var data fetch.GitHubData
	if err := json.Unmarshal(entry.Data, &data); err != nil {
		t.Fatalf("Data を解析できない: %v", err)
	}
	if data.State != "OPEN" {
		t.Errorf("data.State = %q, want OPEN", data.State)
	}
}

func TestCacheNeverFetchedHasNullFetchedAt(t *testing.T) {
	dir := t.TempDir()
	c := fetch.NewCache(dir)
	failedAt := time.Date(2026, 8, 24, 16, 40, 0, 0, time.UTC)

	err := c.Update(context.Background(), func(f *fetch.CacheFile) {
		f.SetFailure("https://github.com/o/r/pull/1", fmt.Errorf("network error"), failedAt)
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	entry, ok := c.Load().Get("https://github.com/o/r/pull/1")
	if !ok {
		t.Fatal("エントリが無い")
	}
	if entry.OK {
		t.Error("OK = true, want false")
	}
	if entry.Error == "" {
		t.Error("Error が空")
	}
	if entry.FetchedAt != nil {
		t.Errorf("FetchedAt = %v, want nil（一度も成功していない）", entry.FetchedAt)
	}
	if !isJSONNull(entry.Data) {
		t.Errorf("Data = %s, want JSON null", entry.Data)
	}
}

// isJSONNull reports whether raw is absent or serializes as JSON null: json.RawMessage
// round-tripped through Marshal/Unmarshal becomes the 4-byte literal "null", not a nil slice.
func isJSONNull(raw json.RawMessage) bool {
	return raw == nil || string(raw) == "null"
}

func TestCacheFailureAfterSuccessKeepsLastSuccessValue(t *testing.T) {
	dir := t.TempDir()
	c := fetch.NewCache(dir)
	successAt := time.Date(2026, 8, 24, 16, 0, 0, 0, time.UTC)
	const url = "https://github.com/o/r/pull/1"

	if err := c.Update(context.Background(), func(f *fetch.CacheFile) {
		_ = f.SetSuccess(url, fetch.GitHubData{State: "OPEN", Title: "旧タイトル"}, successAt)
	}); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	if err := c.Update(context.Background(), func(f *fetch.CacheFile) {
		f.SetFailure(url, fmt.Errorf("timeout"), successAt.Add(time.Minute))
	}); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	entry, _ := c.Load().Get(url)
	if entry.OK {
		t.Error("OK = true, want false（直近の取得は失敗）")
	}
	if entry.Error == "" {
		t.Error("Error が空")
	}
	if entry.FetchedAt == nil || *entry.FetchedAt != successAt.Format(time.RFC3339) {
		t.Errorf("FetchedAt = %v, want 最終成功時刻 %s のまま", entry.FetchedAt, successAt.Format(time.RFC3339))
	}
	var data fetch.GitHubData
	if err := json.Unmarshal(entry.Data, &data); err != nil {
		t.Fatalf("Data を解析できない: %v", err)
	}
	if data.Title != "旧タイトル" {
		t.Errorf("Data.Title = %q, want 最終成功値のまま", data.Title)
	}
}

func TestCacheConcurrentUpdatesDoNotClobberOtherEntries(t *testing.T) {
	dir := t.TempDir()
	c := fetch.NewCache(dir)
	now := time.Date(2026, 8, 24, 16, 0, 0, 0, time.UTC)

	const n = 16
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			url := fmt.Sprintf("https://github.com/o/r/pull/%d", i)
			errs[i] = c.Update(context.Background(), func(f *fetch.CacheFile) {
				_ = f.SetSuccess(url, fetch.GitHubData{State: "OPEN"}, now)
			})
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d の Update() error = %v", i, err)
		}
	}

	loaded := c.Load()
	if len(loaded.Entries) != n {
		t.Fatalf("entries = %d, want %d（並行更新で他エントリが失われた）", len(loaded.Entries), n)
	}
	for i := range n {
		url := fmt.Sprintf("https://github.com/o/r/pull/%d", i)
		if _, ok := loaded.Get(url); !ok {
			t.Errorf("%s のエントリが無い", url)
		}
	}
}

func TestCacheEntryIsStale(t *testing.T) {
	now := time.Date(2026, 8, 24, 16, 0, 0, 0, time.UTC)
	ttl := 5 * time.Minute

	tests := []struct {
		name      string
		fetchedAt *string
		now       time.Time
		want      bool
	}{
		{name: "一度も成功していない（nil）は stale", fetchedAt: nil, now: now, want: true},
		{name: "TTL 内は stale でない", fetchedAt: strPtr(now.Add(-4 * time.Minute).Format(time.RFC3339)), now: now, want: false},
		{name: "TTL ちょうどは stale（境界は超過側に倒す）", fetchedAt: strPtr(now.Add(-5 * time.Minute).Format(time.RFC3339)), now: now, want: true},
		{name: "TTL 超過は stale", fetchedAt: strPtr(now.Add(-6 * time.Minute).Format(time.RFC3339)), now: now, want: true},
		{name: "壊れた fetched_at は stale 扱い", fetchedAt: strPtr("not-a-timestamp"), now: now, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := fetch.CacheEntry{FetchedAt: tt.fetchedAt}
			if got := e.IsStale(tt.now, ttl); got != tt.want {
				t.Errorf("IsStale() = %v, want %v", got, tt.want)
			}
		})
	}
}

func strPtr(s string) *string { return &s }

func TestCacheLoadOnMissingFileReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	c := fetch.NewCache(dir)

	f := c.Load()
	if f == nil || len(f.Entries) != 0 {
		t.Errorf("Load() = %+v, want 空", f)
	}
}

// A corrupt or version-mismatched cache.json is volatile data, unlike tasks.json: it is
// simply rebuilt on the next fetch rather than rejected for manual recovery.
func TestCacheLoadOnCorruptFileReturnsEmptyAndUpdateRebuildsIt(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "cache.json"), []byte(`{"version":1,`), 0o600); err != nil {
		t.Fatalf("cache.json を壊せない: %v", err)
	}
	c := fetch.NewCache(dir)

	if f := c.Load(); len(f.Entries) != 0 {
		t.Errorf("Load() = %+v, want 空", f)
	}

	now := time.Date(2026, 8, 24, 16, 0, 0, 0, time.UTC)
	err := c.Update(context.Background(), func(f *fetch.CacheFile) {
		_ = f.SetSuccess("https://github.com/o/r/pull/1", fetch.GitHubData{State: "OPEN"}, now)
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if _, ok := c.Load().Get("https://github.com/o/r/pull/1"); !ok {
		t.Error("破損ファイルからの再構築後にエントリが無い")
	}
}

// A run of failures is timed from the failure that started it, not from the latest one: a link
// failing every cycle has to be able to say it has been failing for an hour.
func TestCacheFailedSinceMarksStartOfRun(t *testing.T) {
	dir := t.TempDir()
	c := fetch.NewCache(dir)
	const url = "https://github.com/o/r/pull/1"
	start := time.Date(2026, 8, 24, 16, 0, 0, 0, time.UTC)

	for i := 0; i < 3; i++ {
		at := start.Add(time.Duration(i) * 10 * time.Minute)
		if err := c.Update(context.Background(), func(f *fetch.CacheFile) {
			f.SetFailure(url, fmt.Errorf("attempt %d", i), at)
		}); err != nil {
			t.Fatalf("Update() error = %v", err)
		}
	}

	entry, _ := c.Load().Get(url)
	if entry.FailedSince == nil {
		t.Fatal("FailedSince = nil, want 最初の失敗時刻")
	}
	if *entry.FailedSince != start.Format(time.RFC3339) {
		t.Errorf("FailedSince = %q, want %q（最初の失敗のまま）", *entry.FailedSince, start.Format(time.RFC3339))
	}
}

// A success ends the run, so the next failure starts timing again rather than reporting a gap that
// had already been repaired.
func TestCacheSuccessClearsFailedSince(t *testing.T) {
	dir := t.TempDir()
	c := fetch.NewCache(dir)
	const url = "https://github.com/o/r/pull/1"
	start := time.Date(2026, 8, 24, 16, 0, 0, 0, time.UTC)

	steps := []func(*fetch.CacheFile){
		func(f *fetch.CacheFile) { f.SetFailure(url, fmt.Errorf("down"), start) },
		func(f *fetch.CacheFile) {
			_ = f.SetSuccess(url, fetch.GitHubData{State: "OPEN"}, start.Add(time.Minute))
		},
	}
	for _, step := range steps {
		if err := c.Update(context.Background(), step); err != nil {
			t.Fatalf("Update() error = %v", err)
		}
	}

	entry, _ := c.Load().Get(url)
	if entry.FailedSince != nil {
		t.Errorf("FailedSince = %q, want nil（成功で解消）", *entry.FailedSince)
	}

	if err := c.Update(context.Background(), func(f *fetch.CacheFile) {
		f.SetFailure(url, fmt.Errorf("down again"), start.Add(2*time.Minute))
	}); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	entry, _ = c.Load().Get(url)
	if entry.FailedSince == nil || *entry.FailedSince != start.Add(2*time.Minute).Format(time.RFC3339) {
		t.Errorf("FailedSince = %v, want 再開後の失敗時刻", entry.FailedSince)
	}
}

// failed_since is a new field, not a new cache version: a cache.json written before it existed has
// to keep loading, or the first run after an upgrade would blank every card.
func TestCacheWithoutFailedSinceStillLoads(t *testing.T) {
	dir := t.TempDir()
	c := fetch.NewCache(dir)
	legacy := `{"version":1,"entries":{"https://github.com/o/r/pull/1":{` +
		`"fetched_at":"2026-08-24T16:00:00Z","ok":false,"error":"boom","data":{"state":"OPEN"}}}}`
	if err := os.WriteFile(c.Path(), []byte(legacy), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	entry, ok := c.Load().Get("https://github.com/o/r/pull/1")
	if !ok {
		t.Fatal("旧形式の cache.json が読めていない")
	}
	if entry.FailedSince != nil {
		t.Errorf("FailedSince = %q, want nil", *entry.FailedSince)
	}
	if entry.Error != "boom" {
		t.Errorf("Error = %q, want boom", entry.Error)
	}
}
