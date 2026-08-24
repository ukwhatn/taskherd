package fetch_test

import (
	"context"
	"testing"
	"time"

	"github.com/ukwhatn/taskherd/internal/fetch"
	"github.com/ukwhatn/taskherd/internal/model"
)

const stateTTL = 5 * time.Minute

var stateNow = time.Date(2026, 8, 24, 16, 0, 0, 0, time.UTC)

func prLink(url string) model.Link {
	return model.Link{URL: url, Kind: model.LinkKindGitHubPR}
}

// cacheWith builds a cache file through the public write path so the test exercises the same
// entries the fetcher produces.
func cacheWith(t *testing.T, mutate func(*fetch.CacheFile)) *fetch.CacheFile {
	t.Helper()
	cache := fetch.NewCache(t.TempDir())
	if err := cache.Update(context.Background(), mutate); err != nil {
		t.Fatalf("cache を書けない: %v", err)
	}
	return cache.Load()
}

func TestLinkStateNoEntry(t *testing.T) {
	file := cacheWith(t, func(*fetch.CacheFile) {})

	state := file.LinkState(prLink("https://github.com/o/r/pull/1"), stateNow, stateTTL)

	if state.Cached || state.Fetched {
		t.Errorf("state = %+v, want Cached/Fetched=false", state)
	}
	if state.Stale {
		t.Error("Stale = true, want false（未取得は stale ではない）")
	}
}

func TestLinkStateFresh(t *testing.T) {
	url := "https://github.com/o/r/pull/1"
	file := cacheWith(t, func(cf *fetch.CacheFile) {
		if err := cf.SetSuccess(url, fetch.GitHubData{State: "OPEN", Checks: "pass"}, stateNow.Add(-time.Minute)); err != nil {
			t.Fatalf("SetSuccess: %v", err)
		}
	})

	state := file.LinkState(prLink(url), stateNow, stateTTL)

	if !state.Fetched || state.Stale {
		t.Fatalf("state = %+v, want Fetched=true Stale=false", state)
	}
	if state.Age != time.Minute {
		t.Errorf("Age = %v, want 1m", state.Age)
	}
	if state.GitHub == nil || state.GitHub.State != "OPEN" || state.GitHub.Checks != "pass" {
		t.Errorf("GitHub = %+v, want OPEN/pass", state.GitHub)
	}
}

func TestLinkStateStaleAfterTTL(t *testing.T) {
	url := "https://github.com/o/r/pull/1"
	file := cacheWith(t, func(cf *fetch.CacheFile) {
		if err := cf.SetSuccess(url, fetch.GitHubData{State: "OPEN"}, stateNow.Add(-time.Hour)); err != nil {
			t.Fatalf("SetSuccess: %v", err)
		}
	})

	state := file.LinkState(prLink(url), stateNow, stateTTL)

	if !state.Fetched || !state.Stale {
		t.Fatalf("state = %+v, want Fetched=true Stale=true", state)
	}
	if state.Age != time.Hour {
		t.Errorf("Age = %v, want 1h", state.Age)
	}
}

// A failure after a success must keep showing the success: that is what lets the board stay
// readable while gh or Jira is unreachable.
func TestLinkStateFailureKeepsLastSuccess(t *testing.T) {
	url := "https://github.com/o/r/pull/1"
	file := cacheWith(t, func(cf *fetch.CacheFile) {
		if err := cf.SetSuccess(url, fetch.GitHubData{State: "MERGED"}, stateNow.Add(-time.Minute)); err != nil {
			t.Fatalf("SetSuccess: %v", err)
		}
		cf.SetFailure(url, errString("gh がタイムアウトした"))
	})

	state := file.LinkState(prLink(url), stateNow, stateTTL)

	if !state.Fetched {
		t.Fatalf("state = %+v, want Fetched=true（最終成功値を保持）", state)
	}
	if state.GitHub == nil || state.GitHub.State != "MERGED" {
		t.Errorf("GitHub = %+v, want MERGED", state.GitHub)
	}
	if state.Err == "" {
		t.Error("Err = 空, want 直近の失敗理由")
	}
}

// A link that has only ever failed has no value to fall back on, and must not read as stale
// (there is nothing to be stale about) but as a fetch error.
func TestLinkStateNeverSucceeded(t *testing.T) {
	url := "https://github.com/o/r/pull/1"
	file := cacheWith(t, func(cf *fetch.CacheFile) {
		cf.SetFailure(url, errString("認証されていない"))
	})

	state := file.LinkState(prLink(url), stateNow, stateTTL)

	if !state.Cached {
		t.Error("Cached = false, want true（試行の記録はある）")
	}
	if state.Fetched || state.Stale {
		t.Errorf("state = %+v, want Fetched=false Stale=false", state)
	}
	if state.Err == "" {
		t.Error("Err = 空, want 失敗理由")
	}
}

func TestLinkStateJira(t *testing.T) {
	url := "https://x.atlassian.net/browse/ABC-1"
	file := cacheWith(t, func(cf *fetch.CacheFile) {
		if err := cf.SetSuccess(url, fetch.JiraData{StatusName: "In Progress", StatusCategory: "indeterminate"}, stateNow); err != nil {
			t.Fatalf("SetSuccess: %v", err)
		}
	})

	state := file.LinkState(model.Link{URL: url, Kind: model.LinkKindJira}, stateNow, stateTTL)

	if state.Jira == nil || state.Jira.StatusName != "In Progress" {
		t.Fatalf("Jira = %+v, want In Progress", state.Jira)
	}
	if state.GitHub != nil {
		t.Errorf("GitHub = %+v, want nil", state.GitHub)
	}
}

// An "other" link has no live state at all, so nothing is read for it even if the cache
// happens to hold an entry under that URL.
func TestLinkStateOtherKindIsNotFetchable(t *testing.T) {
	url := "https://example.com/x"
	file := cacheWith(t, func(cf *fetch.CacheFile) {
		if err := cf.SetSuccess(url, fetch.GitHubData{State: "OPEN"}, stateNow); err != nil {
			t.Fatalf("SetSuccess: %v", err)
		}
	})

	state := file.LinkState(model.Link{URL: url, Kind: model.LinkKindOther}, stateNow, stateTTL)

	if state.Fetchable() {
		t.Error("Fetchable = true, want false")
	}
	if state.Cached || state.Fetched {
		t.Errorf("state = %+v, want 何も読まない", state)
	}
}

func TestLinkStatesKeyedByURL(t *testing.T) {
	file := cacheWith(t, func(cf *fetch.CacheFile) {
		if err := cf.SetSuccess("https://github.com/o/r/pull/1", fetch.GitHubData{State: "OPEN"}, stateNow); err != nil {
			t.Fatalf("SetSuccess: %v", err)
		}
	})

	states := file.LinkStates([]model.Link{
		prLink("https://github.com/o/r/pull/1"),
		prLink("https://github.com/o/r/pull/2"),
	}, stateNow, stateTTL)

	if len(states) != 2 {
		t.Fatalf("len = %d, want 2", len(states))
	}
	if !states["https://github.com/o/r/pull/1"].Fetched {
		t.Error("pull/1 が Fetched でない")
	}
	if states["https://github.com/o/r/pull/2"].Cached {
		t.Error("pull/2 が Cached になっている")
	}
}

type errString string

func (e errString) Error() string { return string(e) }
