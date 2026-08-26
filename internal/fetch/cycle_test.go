package fetch_test

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ukwhatn/taskherd/internal/fetch"
	"github.com/ukwhatn/taskherd/internal/model"
)

func newTestFetcher(t *testing.T, runGH fakeRunFn) (*fetch.Fetcher, *fetch.Cache) {
	t.Helper()
	dir := t.TempDir()
	cache := fetch.NewCache(dir)
	return &fetch.Fetcher{
		GitHub:     &fetch.GitHubFetcher{Run: runGH},
		Jira:       &fetch.JiraFetcher{Client: nil}, // overridden per test via JiraCreds when needed
		Cache:      cache,
		Classifier: model.URLClassifier{JiraSite: "example.atlassian.net"},
		Now:        func() time.Time { return time.Date(2026, 8, 24, 16, 0, 0, 0, time.UTC) },
	}, cache
}

type fakeRunFn = fetch.GitHubRunner

func TestFetcherRefreshLinksGitHubSuccess(t *testing.T) {
	var calls atomic.Int64
	run := func(_ context.Context, _ []string, args ...string) ([]byte, []byte, error) {
		calls.Add(1)
		return []byte(`{"state":"OPEN","title":"t","updatedAt":"2026-08-24T09:00:00Z"}`), nil, nil
	}
	f, cache := newTestFetcher(t, run)

	urls := []string{"https://github.com/o/r/pull/1", "https://github.com/o/r/issues/2"}
	result, err := f.RefreshLinks(context.Background(), urls)
	if err != nil {
		t.Fatalf("RefreshLinks() error = %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("gh 呼び出し回数 = %d, want 2", got)
	}
	if len(result.Outcomes) != 2 {
		t.Fatalf("Outcomes = %+v", result.Outcomes)
	}
	for _, o := range result.Outcomes {
		if o.Err != nil {
			t.Errorf("outcome %s = %v, want nil", o.URL, o.Err)
		}
	}
	for _, u := range urls {
		if _, ok := cache.Load().Get(u); !ok {
			t.Errorf("%s がキャッシュに無い", u)
		}
	}
}

// Outcomes follows the order the links were given, not the order the fetches happened to finish,
// so callers that print or diff a cycle's result see a stable list.
func TestFetcherRefreshLinksOutcomesKeepInputOrder(t *testing.T) {
	var started atomic.Int64
	run := func(_ context.Context, _ []string, args ...string) ([]byte, []byte, error) {
		// Finish in the reverse of the start order, so a result-ordered implementation cannot pass.
		time.Sleep(time.Duration(8-started.Add(1)) * 10 * time.Millisecond)
		return []byte(`{"state":"OPEN","title":"t","updatedAt":"2026-08-24T09:00:00Z"}`), nil, nil
	}
	f, _ := newTestFetcher(t, run)

	urls := []string{
		"https://github.com/o/r/pull/1",
		"https://github.com/o/r/pull/2",
		"https://github.com/o/r/pull/3",
		"https://github.com/o/r/pull/4",
	}
	result, err := f.RefreshLinks(context.Background(), urls)
	if err != nil {
		t.Fatalf("RefreshLinks() error = %v", err)
	}
	if len(result.Outcomes) != len(urls) {
		t.Fatalf("Outcomes = %+v, want %d 件", result.Outcomes, len(urls))
	}
	for i, want := range urls {
		if got := result.Outcomes[i].URL; got != want {
			t.Errorf("Outcomes[%d].URL = %s, want %s", i, got, want)
		}
	}
}

// A serial implementation deadlocks this test: every fetch waits for n of them to be in flight
// at once, which only happens if they actually run concurrently.
func TestFetcherRefreshLinksFetchesConcurrently(t *testing.T) {
	const n = 4
	var (
		mu       sync.Mutex
		inFlight int
		peak     int
		once     sync.Once
	)
	allInFlight := make(chan struct{})

	run := func(_ context.Context, _ []string, args ...string) ([]byte, []byte, error) {
		mu.Lock()
		inFlight++
		if inFlight > peak {
			peak = inFlight
		}
		reached := inFlight >= n
		mu.Unlock()

		if reached {
			once.Do(func() { close(allInFlight) })
		}
		select {
		case <-allInFlight:
		case <-time.After(2 * time.Second): // a serial run never opens the gate; do not hang the suite
		}

		mu.Lock()
		inFlight--
		mu.Unlock()
		return []byte(`{"state":"OPEN","title":"t","updatedAt":"2026-08-24T09:00:00Z"}`), nil, nil
	}
	f, _ := newTestFetcher(t, run)

	urls := make([]string, n)
	for i := range urls {
		urls[i] = "https://github.com/o/r/pull/" + strconv.Itoa(i+1)
	}
	if _, err := f.RefreshLinks(context.Background(), urls); err != nil {
		t.Fatalf("RefreshLinks() error = %v", err)
	}

	mu.Lock()
	got := peak
	mu.Unlock()
	if got < n {
		t.Errorf("同時に走った最大数 = %d, want %d（逐次実行になっている）", got, n)
	}
}

func TestFetcherRefreshLinksSkipsUnrecognizedKind(t *testing.T) {
	run := func(_ context.Context, _ []string, args ...string) ([]byte, []byte, error) {
		t.Error("other kind の URL で gh が呼ばれた")
		return nil, nil, nil
	}
	f, _ := newTestFetcher(t, run)

	result, err := f.RefreshLinks(context.Background(), []string{"https://example.com/docs"})
	if err != nil {
		t.Fatalf("RefreshLinks() error = %v", err)
	}
	if len(result.Outcomes) != 0 {
		t.Errorf("Outcomes = %+v, want 空（fetch 対象外の kind）", result.Outcomes)
	}
}

// A rate limit stops the links that have not started yet. Requests already in flight are left to
// finish, so the check is that the run stopped short — not that it stopped at an exact count.
func TestFetcherRefreshLinksGitHubRateLimitStopsUnstartedLinks(t *testing.T) {
	const total = 64 // comfortably more than the concurrency limit
	var calls atomic.Int64
	run := func(_ context.Context, _ []string, args ...string) ([]byte, []byte, error) {
		calls.Add(1)
		return nil, []byte("gh: API rate limit exceeded"), errors.New("exit status 1")
	}
	f, _ := newTestFetcher(t, run)

	urls := make([]string, total)
	for i := range urls {
		urls[i] = "https://github.com/o/r/pull/" + strconv.Itoa(i+1)
	}

	result, err := f.RefreshLinks(context.Background(), urls)
	if err != nil {
		t.Fatalf("RefreshLinks() error = %v", err)
	}
	if !result.GitHubInterrupted {
		t.Error("GitHubInterrupted = false, want true")
	}

	attempted := int(calls.Load())
	if attempted >= total {
		t.Errorf("gh 呼び出し回数 = %d, want %d 未満（残りは中断される）", attempted, total)
	}
	// Links that were never attempted must not show up as outcomes: that absence is what tells a
	// caller the run stopped rather than that those links failed.
	if len(result.Outcomes) != attempted {
		t.Errorf("Outcomes = %d 件, want %d 件（試行した数と一致する）", len(result.Outcomes), attempted)
	}
}

func TestFetcherRefreshLinksJiraNotConfiguredRecordsFailure(t *testing.T) {
	f, cache := newTestFetcher(t, nil)
	const url = "https://example.atlassian.net/browse/ABC-1"

	result, err := f.RefreshLinks(context.Background(), []string{url})
	if err != nil {
		t.Fatalf("RefreshLinks() error = %v", err)
	}
	if len(result.Outcomes) != 1 || result.Outcomes[0].Err == nil {
		t.Fatalf("Outcomes = %+v, want 1 件の失敗", result.Outcomes)
	}
	var notConfigured *fetch.JiraNotConfiguredError
	if !errors.As(result.Outcomes[0].Err, &notConfigured) {
		t.Errorf("err = %v (%T), want *JiraNotConfiguredError", result.Outcomes[0].Err, result.Outcomes[0].Err)
	}
	entry, ok := cache.Load().Get(url)
	if !ok || entry.OK {
		t.Errorf("entry = %+v, want ok:false で記録される", entry)
	}
}

func TestFetcherRefreshLinksGitHubAndJiraAreIndependentKinds(t *testing.T) {
	run := func(_ context.Context, _ []string, args ...string) ([]byte, []byte, error) {
		return nil, []byte("gh: API rate limit exceeded"), errors.New("exit status 1")
	}
	f, cache := newTestFetcher(t, run)

	urls := []string{"https://github.com/o/r/pull/1", "https://example.atlassian.net/browse/ABC-1"}
	result, err := f.RefreshLinks(context.Background(), urls)
	if err != nil {
		t.Fatalf("RefreshLinks() error = %v", err)
	}
	if !result.GitHubInterrupted {
		t.Error("GitHubInterrupted = false, want true")
	}
	// Jira has its own kind-worker: GitHub's rate limit must not block it.
	if _, ok := cache.Load().Get("https://example.atlassian.net/browse/ABC-1"); !ok {
		t.Error("GitHub のレート制限で Jira の取得まで止まった")
	}
	if len(result.Outcomes) != 2 {
		t.Errorf("Outcomes = %+v, want GitHub 中断分 1 件 + Jira 1 件の計 2 件", result.Outcomes)
	}
}
