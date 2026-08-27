package fetch_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ukwhatn/taskherd/internal/fetch"
)

func argsOf(call scriptedCall) string {
	return strings.Join(call.args, " ")
}

// A configured account is named to gh, so the token is that account's rather than whichever one
// happens to be signed in.
func TestHostTokenUsesTheConfiguredAccount(t *testing.T) {
	gh := &scriptedGH{reply: func([]string) ([]byte, []byte, error) {
		return []byte("gho_configured\n"), nil, nil
	}}
	f := &fetch.GitHubFetcher{Run: gh.run, Accounts: map[string]string{"github.com": "someone"}}

	if got := f.HostToken(context.Background(), "github.com"); got != "gho_configured" {
		t.Errorf("HostToken() = %q, want gho_configured", got)
	}
	if got, want := argsOf(gh.call(t, 0)), "auth token --hostname github.com --user someone"; got != want {
		t.Errorf("gh args = %q, want %q", got, want)
	}
}

// With no entry for the host there is no account to name, and gh answers for the one it is signed
// in as. Naming an empty --user instead would make gh fail on a machine that works fine.
func TestHostTokenFallsBackToTheActiveAccount(t *testing.T) {
	gh := &scriptedGH{reply: func([]string) ([]byte, []byte, error) {
		return []byte("gho_active\n"), nil, nil
	}}
	f := &fetch.GitHubFetcher{Run: gh.run}

	if got := f.HostToken(context.Background(), "github.com"); got != "gho_active" {
		t.Errorf("HostToken() = %q, want gho_active", got)
	}
	if got, want := argsOf(gh.call(t, 0)), "auth token --hostname github.com"; got != want {
		t.Errorf("gh args = %q, want %q", got, want)
	}
}

// Every way of having no token has to look the same to the caller, whose fallback is the same in
// all of them: ask unauthenticated.
func TestHostTokenAnswersEmptyWhenGHWillNot(t *testing.T) {
	for _, tc := range []struct {
		name  string
		reply func([]string) ([]byte, []byte, error)
		host  string
	}{
		{"gh fails", func([]string) ([]byte, []byte, error) {
			return nil, []byte("not logged in"), errors.New("exit 1")
		}, "github.com"},
		{"gh answers nothing", func([]string) ([]byte, []byte, error) {
			return []byte("  \n"), nil, nil
		}, "github.com"},
		{"no host", func([]string) ([]byte, []byte, error) {
			t.Error("gh should not run for an empty host")
			return nil, nil, nil
		}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fetch.GitHubFetcher{Run: (&scriptedGH{reply: tc.reply}).run}
			if got := f.HostToken(context.Background(), tc.host); got != "" {
				t.Errorf("HostToken() = %q, want empty", got)
			}
		})
	}
}

// Resolution is cached per credential, so a board asking on every check does not spawn a gh
// process every time.
func TestHostTokenResolvesOnce(t *testing.T) {
	gh := &scriptedGH{reply: func([]string) ([]byte, []byte, error) {
		return []byte("gho_once\n"), nil, nil
	}}
	f := &fetch.GitHubFetcher{Run: gh.run}

	for i := 0; i < 3; i++ {
		if got := f.HostToken(context.Background(), "github.com"); got != "gho_once" {
			t.Fatalf("HostToken() = %q, want gho_once", got)
		}
	}
	if len(gh.calls) != 1 {
		t.Errorf("gh ran %d times, want 1", len(gh.calls))
	}
}

// The host is what a caller hands over verbatim, and a config file or a URL supplies it in
// whatever case and spacing it was written in.
func TestHostTokenNormalizesTheHost(t *testing.T) {
	gh := &scriptedGH{reply: func([]string) ([]byte, []byte, error) {
		return []byte("gho_normalized\n"), nil, nil
	}}
	f := &fetch.GitHubFetcher{Run: gh.run, Accounts: map[string]string{"github.com": "someone"}}

	if got := f.HostToken(context.Background(), "  GitHub.com "); got != "gho_normalized" {
		t.Errorf("HostToken() = %q, want gho_normalized", got)
	}
	if got, want := argsOf(gh.call(t, 0)), "auth token --hostname github.com --user someone"; got != want {
		t.Errorf("gh args = %q, want %q", got, want)
	}
}
