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
