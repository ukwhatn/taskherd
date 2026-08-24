package fetch_test

import (
	"context"
	"errors"
	"os/exec"
	"testing"

	"github.com/ukwhatn/taskherd/internal/fetch"
)

func TestFoldStatusCheckRollup(t *testing.T) {
	tests := []struct {
		name string
		json string
		want fetch.CheckStatus
	}{
		{name: "空配列は none", json: `[]`, want: fetch.CheckNone},
		{name: "null は none", json: `null`, want: fetch.CheckNone},

		// CheckRun
		{name: "CheckRun SUCCESS/COMPLETED は pass", json: `[{"__typename":"CheckRun","status":"COMPLETED","conclusion":"SUCCESS"}]`, want: fetch.CheckPass},
		{name: "CheckRun SKIPPED/COMPLETED は pass", json: `[{"__typename":"CheckRun","status":"COMPLETED","conclusion":"SKIPPED"}]`, want: fetch.CheckPass},
		{name: "CheckRun NEUTRAL/COMPLETED は pass", json: `[{"__typename":"CheckRun","status":"COMPLETED","conclusion":"NEUTRAL"}]`, want: fetch.CheckPass},
		{name: "CheckRun FAILURE は fail", json: `[{"__typename":"CheckRun","status":"COMPLETED","conclusion":"FAILURE"}]`, want: fetch.CheckFail},
		{name: "CheckRun TIMED_OUT は fail", json: `[{"__typename":"CheckRun","status":"COMPLETED","conclusion":"TIMED_OUT"}]`, want: fetch.CheckFail},
		{name: "CheckRun CANCELLED は fail", json: `[{"__typename":"CheckRun","status":"COMPLETED","conclusion":"CANCELLED"}]`, want: fetch.CheckFail},
		{name: "CheckRun ACTION_REQUIRED は fail", json: `[{"__typename":"CheckRun","status":"COMPLETED","conclusion":"ACTION_REQUIRED"}]`, want: fetch.CheckFail},
		{name: "CheckRun STARTUP_FAILURE は fail", json: `[{"__typename":"CheckRun","status":"COMPLETED","conclusion":"STARTUP_FAILURE"}]`, want: fetch.CheckFail},
		{name: "CheckRun QUEUED は pending", json: `[{"__typename":"CheckRun","status":"QUEUED","conclusion":""}]`, want: fetch.CheckPending},
		{name: "CheckRun IN_PROGRESS は pending", json: `[{"__typename":"CheckRun","status":"IN_PROGRESS","conclusion":""}]`, want: fetch.CheckPending},

		// StatusContext (legacy commit status)
		{name: "StatusContext SUCCESS は pass", json: `[{"__typename":"StatusContext","state":"SUCCESS"}]`, want: fetch.CheckPass},
		{name: "StatusContext FAILURE は fail", json: `[{"__typename":"StatusContext","state":"FAILURE"}]`, want: fetch.CheckFail},
		{name: "StatusContext ERROR は fail", json: `[{"__typename":"StatusContext","state":"ERROR"}]`, want: fetch.CheckFail},
		{name: "StatusContext PENDING は pending", json: `[{"__typename":"StatusContext","state":"PENDING"}]`, want: fetch.CheckPending},
		{name: "StatusContext EXPECTED は pending", json: `[{"__typename":"StatusContext","state":"EXPECTED"}]`, want: fetch.CheckPending},

		// Aggregation priority: fail > pending > pass
		{name: "pass と fail の混在は fail 優先", json: `[{"__typename":"CheckRun","status":"COMPLETED","conclusion":"SUCCESS"},{"__typename":"CheckRun","status":"COMPLETED","conclusion":"FAILURE"}]`, want: fetch.CheckFail},
		{name: "pass と pending の混在は pending 優先", json: `[{"__typename":"CheckRun","status":"COMPLETED","conclusion":"SUCCESS"},{"__typename":"CheckRun","status":"IN_PROGRESS","conclusion":""}]`, want: fetch.CheckPending},
		{name: "pending と fail の混在は fail 優先", json: `[{"__typename":"CheckRun","status":"IN_PROGRESS","conclusion":""},{"__typename":"CheckRun","status":"COMPLETED","conclusion":"FAILURE"}]`, want: fetch.CheckFail},
		{name: "CheckRun と StatusContext の混在（両方 pass）", json: `[{"__typename":"CheckRun","status":"COMPLETED","conclusion":"SUCCESS"},{"__typename":"StatusContext","state":"SUCCESS"}]`, want: fetch.CheckPass},

		// Unknown / malformed
		{name: "未知の __typename は pending 扱い（安全側）", json: `[{"__typename":"FutureCheckType","status":"COMPLETED","conclusion":"SUCCESS"}]`, want: fetch.CheckPending},
		{name: "__typename 欠落も pending 扱い", json: `[{"status":"COMPLETED","conclusion":"SUCCESS"}]`, want: fetch.CheckPending},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := fetch.FoldStatusCheckRollupJSON([]byte(tt.json))
			if err != nil {
				t.Fatalf("FoldStatusCheckRollupJSON(%s) error = %v", tt.json, err)
			}
			if got != tt.want {
				t.Errorf("FoldStatusCheckRollupJSON(%s) = %q, want %q", tt.json, got, tt.want)
			}
		})
	}
}

// fakeRun lets tests drive GitHubFetcher without spawning a real gh process.
type fakeRun struct {
	stdout []byte
	stderr []byte
	err    error
	args   []string
}

func (f *fakeRun) run(_ context.Context, args ...string) ([]byte, []byte, error) {
	f.args = args
	return f.stdout, f.stderr, f.err
}

func TestGitHubFetcherFetchPR(t *testing.T) {
	fake := &fakeRun{stdout: []byte(`{"state":"OPEN","isDraft":true,"reviewDecision":"REVIEW_REQUIRED","statusCheckRollup":[{"__typename":"CheckRun","status":"COMPLETED","conclusion":"SUCCESS"}],"title":"feat: x","updatedAt":"2026-08-24T09:00:00Z"}`)}
	f := &fetch.GitHubFetcher{Run: fake.run}

	data, err := f.FetchPR(context.Background(), "https://github.com/o/r/pull/1")
	if err != nil {
		t.Fatalf("FetchPR() error = %v", err)
	}
	if data.State != "OPEN" || !data.IsDraft || data.ReviewDecision != "REVIEW_REQUIRED" || data.Checks != string(fetch.CheckPass) {
		t.Errorf("data = %+v", data)
	}
	if data.Title != "feat: x" || data.UpdatedAt != "2026-08-24T09:00:00Z" {
		t.Errorf("data = %+v", data)
	}
	if fake.args[0] != "pr" || fake.args[1] != "view" {
		t.Errorf("args = %v, want pr view ...", fake.args)
	}
}

func TestGitHubFetcherFetchIssue(t *testing.T) {
	fake := &fakeRun{stdout: []byte(`{"state":"CLOSED","title":"bug","updatedAt":"2026-08-24T09:00:00Z"}`)}
	f := &fetch.GitHubFetcher{Run: fake.run}

	data, err := f.FetchIssue(context.Background(), "https://github.com/o/r/issues/1")
	if err != nil {
		t.Fatalf("FetchIssue() error = %v", err)
	}
	if data.State != "CLOSED" || data.Checks != string(fetch.CheckNone) {
		t.Errorf("data = %+v, want checks=none（issue に statusCheckRollup はない）", data)
	}
	if fake.args[0] != "issue" || fake.args[1] != "view" {
		t.Errorf("args = %v, want issue view ...", fake.args)
	}
}

func TestGitHubFetcherNotFound(t *testing.T) {
	fake := &fakeRun{err: &exec.Error{Name: "gh", Err: exec.ErrNotFound}}
	f := &fetch.GitHubFetcher{Run: fake.run}

	_, err := f.FetchPR(context.Background(), "https://github.com/o/r/pull/1")
	var notFound *fetch.GHNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("err = %v (%T), want *GHNotFoundError", err, err)
	}
	if notFound.Hint() == "" {
		t.Error("Hint() が空")
	}
}

func TestGitHubFetcherRateLimitDetection(t *testing.T) {
	fake := &fakeRun{
		stderr: []byte("gh: API rate limit exceeded for user ID 123."),
		err:    errors.New("exit status 1"),
	}
	f := &fetch.GitHubFetcher{Run: fake.run}

	_, err := f.FetchPR(context.Background(), "https://github.com/o/r/pull/1")
	var rateLimit *fetch.GHRateLimitError
	if !errors.As(err, &rateLimit) {
		t.Fatalf("err = %v (%T), want *GHRateLimitError", err, err)
	}
}

func TestGitHubFetcherCommandError(t *testing.T) {
	fake := &fakeRun{
		stderr: []byte("gh: To use GitHub CLI in a GitHub Actions workflow, set the GH_TOKEN environment variable."),
		err:    errors.New("exit status 4"),
	}
	f := &fetch.GitHubFetcher{Run: fake.run}

	_, err := f.FetchPR(context.Background(), "https://github.com/o/r/pull/1")
	var cmdErr *fetch.GHCommandError
	if !errors.As(err, &cmdErr) {
		t.Fatalf("err = %v (%T), want *GHCommandError", err, err)
	}
	if cmdErr.Hint() == "" {
		t.Error("Hint() が空")
	}
	if cmdErr.Error() == "" {
		t.Error("Error() が空（stderr をそのまま提示すべき）")
	}
}
