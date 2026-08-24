package cli_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ukwhatn/taskherd/internal/fetch"
)

// seedCache writes cache entries into the harness's state directory the same way a refresh
// cycle would, so `show` reads them through the real cache file.
func (h *harness) seedCache(t *testing.T, mutate func(*fetch.CacheFile)) {
	t.Helper()
	if err := fetch.NewCache(h.stateDir).Update(context.Background(), mutate); err != nil {
		t.Fatalf("cache を書けない: %v", err)
	}
}

// `show` reports what the cache holds; going out to GitHub or Jira is `refresh`'s job.
func TestShowDisplaysCachedLinkState(t *testing.T) {
	url := "https://github.com/o/r/pull/1"
	h := newHarness(t)
	h.mustRun(t, "add", "実装", "--link", url)
	h.seedCache(t, func(cf *fetch.CacheFile) {
		if err := cf.SetSuccess(url, fetch.GitHubData{
			State:  "OPEN",
			Checks: "pass",
			Title:  "本体実装",
		}, h.now.Add(-time.Minute)); err != nil {
			t.Fatalf("SetSuccess: %v", err)
		}
	})

	res := h.mustRun(t, "show", "1")

	if !strings.Contains(res.stdout, "live:") {
		t.Fatalf("stdout にライブ状態が無い:\n%s", res.stdout)
	}
	for _, want := range []string{"open", "checks=pass", "本体実装", "1m前"} {
		if !strings.Contains(res.stdout, want) {
			t.Errorf("stdout に %q が無い:\n%s", want, res.stdout)
		}
	}
}

func TestShowMarksStaleLinkState(t *testing.T) {
	url := "https://github.com/o/r/pull/1"
	h := newHarness(t)
	h.writeConfig(t, "[board]\ncache_ttl_minutes = 5\n")
	h.mustRun(t, "add", "実装", "--link", url)
	h.seedCache(t, func(cf *fetch.CacheFile) {
		if err := cf.SetSuccess(url, fetch.GitHubData{State: "OPEN", Checks: "none"}, h.now.Add(-time.Hour)); err != nil {
			t.Fatalf("SetSuccess: %v", err)
		}
	})

	res := h.mustRun(t, "show", "1")

	if !strings.Contains(res.stdout, "TTL 超過") {
		t.Errorf("stdout に stale の注記が無い:\n%s", res.stdout)
	}
}

// A link with no cache entry must say so rather than looking like a link with no state.
func TestShowReportsUnfetchedLink(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "add", "実装", "--link", "https://github.com/o/r/pull/1")

	res := h.mustRun(t, "show", "1")

	if !strings.Contains(res.stdout, "未取得") {
		t.Errorf("stdout に未取得の注記が無い:\n%s", res.stdout)
	}
}

// The displayed value stays the last success while a refresh keeps failing; the failure is
// reported next to it instead of blanking it out.
func TestShowKeepsLastSuccessAlongsideError(t *testing.T) {
	url := "https://github.com/o/r/pull/1"
	h := newHarness(t)
	h.mustRun(t, "add", "実装", "--link", url)
	h.seedCache(t, func(cf *fetch.CacheFile) {
		if err := cf.SetSuccess(url, fetch.GitHubData{State: "MERGED", Checks: "pass"}, h.now.Add(-time.Minute)); err != nil {
			t.Fatalf("SetSuccess: %v", err)
		}
		cf.SetFailure(url, errFetch("gh: not authenticated"), h.now)
	})

	res := h.mustRun(t, "show", "1")

	if !strings.Contains(res.stdout, "merged") {
		t.Errorf("最終成功値が消えている:\n%s", res.stdout)
	}
	if !strings.Contains(res.stdout, "not authenticated") {
		t.Errorf("直近の失敗が報告されていない:\n%s", res.stdout)
	}
}

// An "other" link has no live state at all, so no live line is printed for it.
func TestShowOmitsLiveLineForOtherLink(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "add", "参考", "--link", "https://example.com/doc")

	res := h.mustRun(t, "show", "1")

	if strings.Contains(res.stdout, "live:") {
		t.Errorf("other リンクにライブ状態が出ている:\n%s", res.stdout)
	}
}

func TestShowJSONCarriesLinkStates(t *testing.T) {
	url := "https://github.com/o/r/pull/1"
	h := newHarness(t)
	h.mustRun(t, "add", "実装", "--link", url)
	h.seedCache(t, func(cf *fetch.CacheFile) {
		if err := cf.SetSuccess(url, fetch.GitHubData{State: "OPEN", Checks: "fail"}, h.now); err != nil {
			t.Fatalf("SetSuccess: %v", err)
		}
	})

	res := h.mustRun(t, "show", "1", "--json")

	var payload struct {
		LinkStates map[string]fetch.LinkState `json:"link_states"`
	}
	if err := json.Unmarshal([]byte(res.stdout), &payload); err != nil {
		t.Fatalf("JSON を解析できない: %v\n%s", err, res.stdout)
	}
	state, ok := payload.LinkStates[url]
	if !ok {
		t.Fatalf("link_states に %s が無い: %+v", url, payload.LinkStates)
	}
	if !state.Fetched || state.GitHub == nil || state.GitHub.Checks != "fail" {
		t.Errorf("state = %+v, want checks=fail", state)
	}
}

type errFetch string

func (e errFetch) Error() string { return string(e) }
