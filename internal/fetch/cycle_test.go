package fetch_test

import (
	"context"
	"errors"
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
		Classifier: model.URLClassifier{JiraSite: "dena.atlassian.net"},
		Now:        func() time.Time { return time.Date(2026, 8, 24, 16, 0, 0, 0, time.UTC) },
	}, cache
}

type fakeRunFn = fetch.GitHubRunner

func TestFetcherRefreshLinksGitHubSuccess(t *testing.T) {
	calls := 0
	run := func(_ context.Context, _ []string, args ...string) ([]byte, []byte, error) {
		calls++
		return []byte(`{"state":"OPEN","title":"t","updatedAt":"2026-08-24T09:00:00Z"}`), nil, nil
	}
	f, cache := newTestFetcher(t, run)

	urls := []string{"https://github.com/o/r/pull/1", "https://github.com/o/r/issues/2"}
	result, err := f.RefreshLinks(context.Background(), urls)
	if err != nil {
		t.Fatalf("RefreshLinks() error = %v", err)
	}
	if calls != 2 {
		t.Errorf("gh 呼び出し回数 = %d, want 2", calls)
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

func TestFetcherRefreshLinksSkipsUnrecognizedKind(t *testing.T) {
	run := func(_ context.Context, _ []string, args ...string) ([]byte, []byte, error) {
		t.Fatal("other kind の URL で gh が呼ばれた")
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

func TestFetcherRefreshLinksGitHubRateLimitInterruptsRemainingGitHubLinks(t *testing.T) {
	calls := 0
	run := func(_ context.Context, _ []string, args ...string) ([]byte, []byte, error) {
		calls++
		if calls == 1 {
			return nil, []byte("gh: API rate limit exceeded"), errors.New("exit status 1")
		}
		t.Errorf("レート制限後に %d 回目の呼び出しが発生した", calls)
		return []byte(`{}`), nil, nil
	}
	f, _ := newTestFetcher(t, run)

	urls := []string{"https://github.com/o/r/pull/1", "https://github.com/o/r/pull/2", "https://github.com/o/r/pull/3"}
	result, err := f.RefreshLinks(context.Background(), urls)
	if err != nil {
		t.Fatalf("RefreshLinks() error = %v", err)
	}
	if !result.GitHubInterrupted {
		t.Error("GitHubInterrupted = false, want true")
	}
	if len(result.Outcomes) != 1 {
		t.Fatalf("Outcomes = %+v, want 1 件（残りは中断）", result.Outcomes)
	}
	if calls != 1 {
		t.Errorf("gh 呼び出し回数 = %d, want 1", calls)
	}
}

func TestFetcherRefreshLinksJiraNotConfiguredRecordsFailure(t *testing.T) {
	f, cache := newTestFetcher(t, nil)
	const url = "https://dena.atlassian.net/browse/ABC-1"

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

	urls := []string{"https://github.com/o/r/pull/1", "https://dena.atlassian.net/browse/ABC-1"}
	result, err := f.RefreshLinks(context.Background(), urls)
	if err != nil {
		t.Fatalf("RefreshLinks() error = %v", err)
	}
	if !result.GitHubInterrupted {
		t.Error("GitHubInterrupted = false, want true")
	}
	// Jira has its own kind-worker: GitHub's rate limit must not block it.
	if _, ok := cache.Load().Get("https://dena.atlassian.net/browse/ABC-1"); !ok {
		t.Error("GitHub のレート制限で Jira の取得まで止まった")
	}
	if len(result.Outcomes) != 2 {
		t.Errorf("Outcomes = %+v, want GitHub 中断分 1 件 + Jira 1 件の計 2 件", result.Outcomes)
	}
}
