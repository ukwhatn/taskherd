package cli_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// These tests exercise the refresh command's wiring (argument validation, id resolution,
// --json contract) without ever invoking gh or Jira: every task here has zero links, so
// Fetcher.RefreshLinks has nothing to fetch and never shells out.
func TestRefreshRequiresIDOrAll(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "add", "a")

	res := h.run(t, "refresh")

	if res.code == 0 {
		t.Fatal("exit = 0, want 非 0")
	}
	if !strings.Contains(res.stderr, "id") && !strings.Contains(res.stderr, "--all") {
		t.Errorf("stderr = %q, want id/--all の案内", res.stderr)
	}
}

func TestRefreshRejectsIDAndAllTogether(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "add", "a")

	res := h.run(t, "refresh", "1", "--all")

	if res.code == 0 {
		t.Fatal("exit = 0, want 非 0")
	}
}

func TestRefreshUnknownID(t *testing.T) {
	h := newHarness(t)

	res := h.run(t, "refresh", "42")

	if res.code == 0 {
		t.Fatal("exit = 0, want 非 0")
	}
	if !strings.Contains(res.stderr, "42") {
		t.Errorf("stderr に id が無い: %q", res.stderr)
	}
}

func TestRefreshTaskWithNoLinksSucceeds(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "add", "リンク無し")

	res := h.mustRun(t, "refresh", "1", "--json")

	var payload struct {
		Updated           []string `json:"updated"`
		Failed            []any    `json:"failed"`
		GitHubInterrupted bool     `json:"github_interrupted"`
		JiraInterrupted   bool     `json:"jira_interrupted"`
	}
	if err := json.Unmarshal([]byte(res.stdout), &payload); err != nil {
		t.Fatalf("JSON を解析できない: %v\n%s", err, res.stdout)
	}
	if len(payload.Updated) != 0 || len(payload.Failed) != 0 {
		t.Errorf("payload = %+v, want 両方空（リンクが無い）", payload)
	}
	if payload.GitHubInterrupted || payload.JiraInterrupted {
		t.Errorf("payload = %+v, want interrupted=false", payload)
	}
}

func TestRefreshAllWithNoTasksSucceeds(t *testing.T) {
	h := newHarness(t)

	res := h.mustRun(t, "refresh", "--all")

	if want := fmt.Sprintf(ja.CLI.Refresh.Refreshed, 0); !strings.Contains(res.stdout, want) {
		t.Errorf("stdout = %q, want %q", res.stdout, want)
	}
}

func TestRefreshSkipsOtherKindLinksWithoutShellingOut(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "add", "a", "--link", "https://example.com/docs")

	res := h.mustRun(t, "refresh", "1", "--json")

	var payload struct {
		Updated []string `json:"updated"`
	}
	if err := json.Unmarshal([]byte(res.stdout), &payload); err != nil {
		t.Fatalf("JSON を解析できない: %v\n%s", err, res.stdout)
	}
	if len(payload.Updated) != 0 {
		t.Errorf("updated = %+v, want 空（other kind は fetch 対象外）", payload.Updated)
	}
}

func TestConfigPathIncludesCache(t *testing.T) {
	h := newHarness(t)

	res := h.mustRun(t, "config", "path", "--json")

	var payload struct {
		Cache     string `json:"cache"`
		CacheLock string `json:"cache_lock"`
	}
	if err := json.Unmarshal([]byte(res.stdout), &payload); err != nil {
		t.Fatalf("JSON を解析できない: %v\n%s", err, res.stdout)
	}
	if !strings.HasSuffix(payload.Cache, "cache.json") {
		t.Errorf("cache = %q, want cache.json で終わる", payload.Cache)
	}
	if !strings.HasSuffix(payload.CacheLock, "cache.lock") {
		t.Errorf("cache_lock = %q, want cache.lock で終わる", payload.CacheLock)
	}
}
