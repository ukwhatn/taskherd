package fetch_test

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/ukwhatn/taskherd/internal/fetch"
)

// scriptedGH answers a sequence of gh invocations and records what each one was run with.
type scriptedGH struct {
	calls []scriptedCall
	reply func(args []string) ([]byte, []byte, error)
}

type scriptedCall struct {
	args []string
	env  []string
}

func (s *scriptedGH) run(_ context.Context, env []string, args ...string) ([]byte, []byte, error) {
	s.calls = append(s.calls, scriptedCall{args: args, env: env})
	return s.reply(args)
}

func (s *scriptedGH) call(t *testing.T, i int) scriptedCall {
	t.Helper()
	if i >= len(s.calls) {
		t.Fatalf("gh 呼び出しが %d 回しかない: %+v", len(s.calls), s.calls)
	}
	return s.calls[i]
}

const prJSON = `{"state":"OPEN","title":"t","updatedAt":"2026-08-24T09:00:00Z"}`

func hasEnv(env []string, key, value string) bool {
	for _, entry := range env {
		if entry == key+"="+value {
			return true
		}
	}
	return false
}

// The configured account's token is resolved once and handed to gh as an environment variable, so
// which account gh has active does not decide what the board can read.
func TestGitHubFetcherUsesConfiguredAccountToken(t *testing.T) {
	gh := &scriptedGH{}
	gh.reply = func(args []string) ([]byte, []byte, error) {
		if args[0] == "auth" {
			return []byte("gho_secret\n"), nil, nil
		}
		return []byte(prJSON), nil, nil
	}
	f := &fetch.GitHubFetcher{Run: gh.run, Accounts: map[string]string{"github.com": "ukwhatn"}}

	if _, err := f.FetchPR(context.Background(), "https://github.com/o/r/pull/1"); err != nil {
		t.Fatalf("FetchPR() error = %v", err)
	}

	token := gh.call(t, 0)
	want := []string{"auth", "token", "--hostname", "github.com", "--user", "ukwhatn"}
	if strings.Join(token.args, " ") != strings.Join(want, " ") {
		t.Errorf("トークン取得コマンド = %v, want %v", token.args, want)
	}
	if token.env != nil {
		t.Errorf("トークン取得を既存トークン付きで実行している: %v", token.env)
	}

	view := gh.call(t, 1)
	if !hasEnv(view.env, "GH_TOKEN", "gho_secret") || !hasEnv(view.env, "GH_ENTERPRISE_TOKEN", "gho_secret") {
		t.Errorf("gh に渡した env = %v, want GH_TOKEN と GH_ENTERPRISE_TOKEN", view.env)
	}
}

// Resolving a token costs a gh process, so it happens once per host and not once per link.
func TestGitHubFetcherResolvesTokenOncePerHost(t *testing.T) {
	gh := &scriptedGH{}
	gh.reply = func(args []string) ([]byte, []byte, error) {
		if args[0] == "auth" {
			return []byte("gho_secret"), nil, nil
		}
		return []byte(prJSON), nil, nil
	}
	f := &fetch.GitHubFetcher{Run: gh.run, Accounts: map[string]string{"github.com": "ukwhatn"}}

	for i := 0; i < 3; i++ {
		if _, err := f.FetchPR(context.Background(), "https://github.com/o/r/pull/1"); err != nil {
			t.Fatalf("FetchPR() error = %v", err)
		}
	}

	auths := 0
	for _, call := range gh.calls {
		if call.args[0] == "auth" {
			auths++
		}
	}
	if auths != 1 {
		t.Errorf("gh auth token の実行回数 = %d, want 1", auths)
	}
}

// A host with no entry keeps the old behaviour: gh resolves the account itself from the URL.
func TestGitHubFetcherLeavesUnconfiguredHostToGH(t *testing.T) {
	gh := &scriptedGH{reply: func([]string) ([]byte, []byte, error) { return []byte(prJSON), nil, nil }}
	f := &fetch.GitHubFetcher{Run: gh.run, Accounts: map[string]string{"github.example.com": "someone"}}

	if _, err := f.FetchPR(context.Background(), "https://github.com/o/r/pull/1"); err != nil {
		t.Fatalf("FetchPR() error = %v", err)
	}

	if len(gh.calls) != 1 {
		t.Fatalf("gh 呼び出し = %+v, want 1 回（トークン取得なし）", gh.calls)
	}
	if gh.calls[0].env != nil {
		t.Errorf("env = %v, want nil", gh.calls[0].env)
	}
}

func TestGitHubFetcherMatchesHostCaseInsensitively(t *testing.T) {
	gh := &scriptedGH{}
	gh.reply = func(args []string) ([]byte, []byte, error) {
		if args[0] == "auth" {
			return []byte("gho_secret"), nil, nil
		}
		return []byte(prJSON), nil, nil
	}
	f := &fetch.GitHubFetcher{Run: gh.run, Accounts: map[string]string{" GitHub.EXAMPLE.com ": "someone"}}

	if _, err := f.FetchIssue(context.Background(), "https://github.example.com/o/r/issues/1"); err != nil {
		t.Fatalf("FetchIssue() error = %v", err)
	}
	if gh.call(t, 0).args[0] != "auth" {
		t.Errorf("大文字小文字・空白違いのホストが一致していない: %+v", gh.calls)
	}
}

// A token that cannot be resolved is not fatal: gh still has its active account, so the fetch is
// tried anyway and the reason is only told if that attempt fails too.
func TestGitHubFetcherFallsBackToActiveAccount(t *testing.T) {
	gh := &scriptedGH{}
	gh.reply = func(args []string) ([]byte, []byte, error) {
		if args[0] == "auth" {
			return nil, []byte("no oauth token found for github.com account ukwhatn"), errors.New("exit 1")
		}
		return []byte(prJSON), nil, nil
	}
	f := &fetch.GitHubFetcher{Run: gh.run, Accounts: map[string]string{"github.com": "ukwhatn"}}

	data, err := f.FetchPR(context.Background(), "https://github.com/o/r/pull/1")
	if err != nil {
		t.Fatalf("FetchPR() error = %v, want 成功（active account へフォールバック）", err)
	}
	if data.State != "OPEN" {
		t.Errorf("State = %q, want OPEN", data.State)
	}
	if gh.call(t, 1).env != nil {
		t.Errorf("env = %v, want nil（トークンが無いので付けない）", gh.call(t, 1).env)
	}
}

// When the fallback fetch also fails, the message says both what gh reported and that the
// configured account was not the one used.
func TestGitHubFetcherReportsAccountFallbackOnFailure(t *testing.T) {
	gh := &scriptedGH{}
	gh.reply = func(args []string) ([]byte, []byte, error) {
		if args[0] == "auth" {
			return nil, []byte("no oauth token found"), errors.New("exit 1")
		}
		return nil, []byte("gh: Not Found"), errors.New("exit 1")
	}
	f := &fetch.GitHubFetcher{Run: gh.run, Accounts: map[string]string{"github.com": "ukwhatn"}}

	_, err := f.FetchPR(context.Background(), "https://github.com/o/r/pull/1")
	if err == nil {
		t.Fatal("FetchPR() error = nil, want エラー")
	}
	msg := err.Error()
	for _, want := range []string{"gh: Not Found", "github.accounts", "ukwhatn"} {
		if !strings.Contains(msg, want) {
			t.Errorf("メッセージに %q が無い: %q", want, msg)
		}
	}
}

// A failure is written to cache.json, which is a file on disk, so a token must never reach one
// even if gh echoes its environment back.
func TestGitHubFetcherScrubsTokenFromErrors(t *testing.T) {
	gh := &scriptedGH{}
	gh.reply = func(args []string) ([]byte, []byte, error) {
		if args[0] == "auth" {
			return []byte("gho_secret"), nil, nil
		}
		return nil, []byte("gh failed with GH_TOKEN=gho_secret in env"), errors.New("exit 1")
	}
	f := &fetch.GitHubFetcher{Run: gh.run, Accounts: map[string]string{"github.com": "ukwhatn"}}

	_, err := f.FetchPR(context.Background(), "https://github.com/o/r/pull/1")
	if err == nil {
		t.Fatal("FetchPR() error = nil, want エラー")
	}
	if strings.Contains(err.Error(), "gho_secret") {
		t.Errorf("エラーメッセージにトークンが残っている: %q", err.Error())
	}
	if !strings.Contains(err.Error(), "***") {
		t.Errorf("トークンが伏せ字に置き換わっていない: %q", err.Error())
	}
}

// gh missing from PATH is still reported as gh missing, not as an account problem.
func TestGitHubFetcherStillDetectsMissingGH(t *testing.T) {
	gh := &scriptedGH{reply: func([]string) ([]byte, []byte, error) { return nil, nil, exec.ErrNotFound }}
	f := &fetch.GitHubFetcher{Run: gh.run, Accounts: map[string]string{"github.com": "ukwhatn"}}

	_, err := f.FetchPR(context.Background(), "https://github.com/o/r/pull/1")
	var notFound *fetch.GHNotFoundError
	if !errors.As(err, &notFound) {
		t.Errorf("err = %v, want GHNotFoundError", err)
	}
}

// The failure this exists for: one host holding both a personal and an organization owner, where a
// single host-wide account can only read one of them.
func TestGitHubFetcherPicksAccountPerOwner(t *testing.T) {
	accounts := map[string]string{
		"github.com/some-org": "work-account",
		"github.com/me":       "personal",
	}
	tests := []struct {
		url  string
		want string
	}{
		{"https://github.com/some-org/server/pull/1", "work-account"},
		{"https://github.com/me/tool/pull/2", "personal"},
	}
	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			gh := &scriptedGH{}
			gh.reply = func(args []string) ([]byte, []byte, error) {
				if args[0] == "auth" {
					return []byte("gho_" + args[5]), nil, nil
				}
				return []byte(prJSON), nil, nil
			}
			f := &fetch.GitHubFetcher{Run: gh.run, Accounts: accounts}

			if _, err := f.FetchPR(context.Background(), tc.url); err != nil {
				t.Fatalf("FetchPR() error = %v", err)
			}
			token := gh.call(t, 0)
			if token.args[5] != tc.want {
				t.Errorf("--user = %q, want %q", token.args[5], tc.want)
			}
			if !hasEnv(gh.call(t, 1).env, "GH_TOKEN", "gho_"+tc.want) {
				t.Errorf("gh に渡した env = %v, want gho_%s", gh.call(t, 1).env, tc.want)
			}
		})
	}
}

// An owner entry beats a host entry for the same host: the host form is the older, coarser answer.
func TestGitHubFetcherOwnerEntryBeatsHostEntry(t *testing.T) {
	gh := &scriptedGH{}
	gh.reply = func(args []string) ([]byte, []byte, error) {
		if args[0] == "auth" {
			return []byte("gho_" + args[5]), nil, nil
		}
		return []byte(prJSON), nil, nil
	}
	f := &fetch.GitHubFetcher{Run: gh.run, Accounts: map[string]string{
		"github.com":          "host-wide",
		"github.com/some-org": "work-account",
	}}

	if _, err := f.FetchPR(context.Background(), "https://github.com/some-org/server/pull/1"); err != nil {
		t.Fatalf("FetchPR() error = %v", err)
	}
	if got := gh.call(t, 0).args[5]; got != "work-account" {
		t.Errorf("--user = %q, want work-account（owner 完全一致が優先）", got)
	}
}

// An owner with no entry of its own still falls back to the host entry, which is what keeps a
// config written before the owner form kept working.
func TestGitHubFetcherFallsBackToHostEntry(t *testing.T) {
	gh := &scriptedGH{}
	gh.reply = func(args []string) ([]byte, []byte, error) {
		if args[0] == "auth" {
			return []byte("gho_" + args[5]), nil, nil
		}
		return []byte(prJSON), nil, nil
	}
	f := &fetch.GitHubFetcher{Run: gh.run, Accounts: map[string]string{
		"github.com":          "host-wide",
		"github.com/some-org": "work-account",
	}}

	if _, err := f.FetchPR(context.Background(), "https://github.com/elsewhere/repo/pull/1"); err != nil {
		t.Fatalf("FetchPR() error = %v", err)
	}
	if got := gh.call(t, 0).args[5]; got != "host-wide" {
		t.Errorf("--user = %q, want host-wide（owner 指定がないのでホスト単位へフォールバック）", got)
	}
}

func TestGitHubFetcherMatchesOwnerCaseInsensitively(t *testing.T) {
	gh := &scriptedGH{}
	gh.reply = func(args []string) ([]byte, []byte, error) {
		if args[0] == "auth" {
			return []byte("gho_secret"), nil, nil
		}
		return []byte(prJSON), nil, nil
	}
	f := &fetch.GitHubFetcher{Run: gh.run, Accounts: map[string]string{" GitHub.COM/Some-Org/ ": "work-account"}}

	if _, err := f.FetchPR(context.Background(), "https://github.com/SOME-ORG/server/pull/1"); err != nil {
		t.Fatalf("FetchPR() error = %v", err)
	}
	if gh.call(t, 0).args[0] != "auth" {
		t.Errorf("大文字小文字・空白・末尾スラッシュ違いの owner キーが一致していない: %+v", gh.calls)
	}
}

// The token cache is keyed by the credential, not by the host, or a second owner on the same host
// would be fetched with the first owner's token.
func TestGitHubFetcherResolvesTokenPerAccountNotPerHost(t *testing.T) {
	gh := &scriptedGH{}
	gh.reply = func(args []string) ([]byte, []byte, error) {
		if args[0] == "auth" {
			return []byte("gho_" + args[5]), nil, nil
		}
		return []byte(prJSON), nil, nil
	}
	f := &fetch.GitHubFetcher{Run: gh.run, Accounts: map[string]string{
		"github.com/org-a": "account-a",
		"github.com/org-b": "account-b",
		"github.com/org-c": "account-a",
	}}

	for _, url := range []string{
		"https://github.com/org-a/r/pull/1",
		"https://github.com/org-b/r/pull/2",
		"https://github.com/org-c/r/pull/3",
		"https://github.com/org-a/r/pull/4",
	} {
		if _, err := f.FetchPR(context.Background(), url); err != nil {
			t.Fatalf("FetchPR(%s) error = %v", url, err)
		}
	}

	var users []string
	for _, call := range gh.calls {
		if call.args[0] == "auth" {
			users = append(users, call.args[5])
		}
	}
	// Two accounts, so two tokens: org-c reuses account-a's and org-a's second link reuses its own.
	if strings.Join(users, ",") != "account-a,account-b" {
		t.Errorf("gh auth token の呼び出し = %v, want [account-a account-b]", users)
	}

	// Every fetch still runs with the token belonging to its own owner's account.
	wantTokens := map[string]string{
		"https://github.com/org-a/r/pull/1": "gho_account-a",
		"https://github.com/org-b/r/pull/2": "gho_account-b",
		"https://github.com/org-c/r/pull/3": "gho_account-a",
		"https://github.com/org-a/r/pull/4": "gho_account-a",
	}
	for _, call := range gh.calls {
		if call.args[0] == "auth" {
			continue
		}
		want := wantTokens[call.args[2]]
		if !hasEnv(call.env, "GH_TOKEN", want) {
			t.Errorf("%s の env = %v, want GH_TOKEN=%s", call.args[2], call.env, want)
		}
	}
}

// A 404 from GraphQL and a repository the account cannot see are the same message, so the failure
// has to name the account it used and point at the owner form of the setting. Without that, the
// board just says every link failed.
func TestGitHubFetcherDiagnosesWrongAccountOn404(t *testing.T) {
	gh := &scriptedGH{}
	gh.reply = func(args []string) ([]byte, []byte, error) {
		if args[0] == "auth" {
			return []byte("gho_secret"), nil, nil
		}
		return nil, []byte("GraphQL: Could not resolve to a Repository with the name 'some-org/server'."), errors.New("exit 1")
	}
	f := &fetch.GitHubFetcher{Run: gh.run, Accounts: map[string]string{"github.com": "personal"}}

	_, err := f.FetchPR(context.Background(), "https://github.com/some-org/server/pull/1")
	if err == nil {
		t.Fatal("FetchPR() error = nil, want エラー")
	}
	msg := err.Error()
	for _, want := range []string{
		"Could not resolve to a Repository",
		`取得に使ったアカウント: "personal"`,
		`"github.com"`,
		`"<host>/<owner>"`,
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("メッセージに %q が無い:\n%s", want, msg)
		}
	}
	if strings.Contains(msg, "gho_secret") {
		t.Errorf("メッセージにトークンが残っている:\n%s", msg)
	}
}

// With no entry at all the message still says which account answered, because "gh's active one" is
// itself the thing to check.
func TestGitHubFetcher404WithoutConfiguredAccount(t *testing.T) {
	gh := &scriptedGH{reply: func([]string) ([]byte, []byte, error) {
		return nil, []byte("GraphQL: Could not resolve to a Repository"), errors.New("exit 1")
	}}
	f := &fetch.GitHubFetcher{Run: gh.run}

	_, err := f.FetchPR(context.Background(), "https://github.com/some-org/server/pull/1")
	if err == nil {
		t.Fatal("FetchPR() error = nil, want エラー")
	}
	if !strings.Contains(err.Error(), "gh の active account") {
		t.Errorf("メッセージが使用アカウントに触れていない:\n%s", err.Error())
	}
}

// A failure that is not a 404 keeps the message it had: the owner guidance is only relevant to the
// one error it explains.
func TestGitHubFetcherLeavesOtherFailuresAlone(t *testing.T) {
	gh := &scriptedGH{}
	gh.reply = func(args []string) ([]byte, []byte, error) {
		if args[0] == "auth" {
			return []byte("gho_secret"), nil, nil
		}
		return nil, []byte("gh: connection refused"), errors.New("exit 1")
	}
	f := &fetch.GitHubFetcher{Run: gh.run, Accounts: map[string]string{"github.com": "personal"}}

	_, err := f.FetchPR(context.Background(), "https://github.com/some-org/server/pull/1")
	if err == nil {
		t.Fatal("FetchPR() error = nil, want エラー")
	}
	if strings.Contains(err.Error(), "<host>/<owner>") {
		t.Errorf("無関係な失敗に owner 指定の案内が付いている:\n%s", err.Error())
	}
}
