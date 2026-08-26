package fetch_test

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ukwhatn/taskherd/internal/fetch"
)

func TestJiraFetcherFetchIssueSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/3/issue/ABC-123" {
			t.Errorf("path = %q, want /rest/api/3/issue/ABC-123", r.URL.Path)
		}
		if got := r.URL.Query().Get("fields"); got != "summary,status,updated" {
			t.Errorf("fields = %q", got)
		}
		user, pass, ok := r.BasicAuth()
		if !ok || user != "me@example.com" || pass != "token-123" {
			t.Errorf("BasicAuth = (%q, %q, %v), want (me@example.com, token-123, true)", user, pass, ok)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"fields":{"summary":"実装する","status":{"name":"In Progress","statusCategory":{"key":"indeterminate"}},"updated":"2026-08-24T09:00:00.000+0900"}}`))
	}))
	defer srv.Close()

	f := &fetch.JiraFetcher{Client: srv.Client()}
	// Site carries its own scheme here (http, from httptest); production config values
	// are bare hosts, which JiraFetcher defaults to https.
	creds := fetch.JiraCredentials{Site: srv.URL, Email: "me@example.com", Token: "token-123"}

	data, err := f.FetchIssue(context.Background(), creds, "ABC-123")
	if err != nil {
		t.Fatalf("FetchIssue() error = %v", err)
	}
	if data.Summary != "実装する" || data.StatusName != "In Progress" || data.StatusCategory != "indeterminate" {
		t.Errorf("data = %+v", data)
	}
	if data.UpdatedAt != "2026-08-24T09:00:00.000+0900" {
		t.Errorf("UpdatedAt = %q", data.UpdatedAt)
	}
}

func TestJiraFetcherUnauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"errorMessages":["client must be authenticated"]}`))
	}))
	defer srv.Close()

	f := &fetch.JiraFetcher{Client: srv.Client()}
	creds := fetch.JiraCredentials{Site: srv.URL, Email: "me@example.com", Token: "expired"}

	_, err := f.FetchIssue(context.Background(), creds, "ABC-1")
	var authErr *fetch.JiraAuthError
	if !errors.As(err, &authErr) {
		t.Fatalf("err = %v (%T), want *JiraAuthError", err, err)
	}
	if _, hint := authErr.Localize(nil); !strings.Contains(hint, "id.atlassian.com") {
		t.Errorf("hint = %q, want トークン再発行 URL を含む", hint)
	}
}

func TestJiraFetcherRateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	f := &fetch.JiraFetcher{Client: srv.Client()}
	creds := fetch.JiraCredentials{Site: srv.URL, Email: "me@example.com", Token: "t"}

	_, err := f.FetchIssue(context.Background(), creds, "ABC-1")
	var rateLimit *fetch.JiraRateLimitError
	if !errors.As(err, &rateLimit) {
		t.Fatalf("err = %v (%T), want *JiraRateLimitError", err, err)
	}
	if rateLimit.RetryAfter.Seconds() != 30 {
		t.Errorf("RetryAfter = %v, want 30s", rateLimit.RetryAfter)
	}
}

func TestJiraFetcherRateLimitedWithoutRetryAfterHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	f := &fetch.JiraFetcher{Client: srv.Client()}
	creds := fetch.JiraCredentials{Site: srv.URL, Email: "me@example.com", Token: "t"}

	_, err := f.FetchIssue(context.Background(), creds, "ABC-1")
	var rateLimit *fetch.JiraRateLimitError
	if !errors.As(err, &rateLimit) {
		t.Fatalf("err = %v (%T), want *JiraRateLimitError", err, err)
	}
	if rateLimit.RetryAfter != 0 {
		t.Errorf("RetryAfter = %v, want 0", rateLimit.RetryAfter)
	}
}

func TestJiraFetcherOtherStatusCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"errorMessages":["Issue does not exist"]}`))
	}))
	defer srv.Close()

	f := &fetch.JiraFetcher{Client: srv.Client()}
	creds := fetch.JiraCredentials{Site: srv.URL, Email: "me@example.com", Token: "t"}

	_, err := f.FetchIssue(context.Background(), creds, "ABC-404")
	var statusErr *fetch.JiraStatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("err = %v (%T), want *JiraStatusError", err, err)
	}
	if statusErr.StatusCode != http.StatusNotFound {
		t.Errorf("StatusCode = %d, want 404", statusErr.StatusCode)
	}
}

// basicAuthHeader is a sanity check that SetBasicAuth encodes what the test expects to decode.
func TestBasicAuthEncodingSanityCheck(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)
	req.SetBasicAuth("me@example.com", "token-123")
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("me@example.com:token-123"))
	if got := req.Header.Get("Authorization"); got != want {
		t.Errorf("Authorization = %q, want %q", got, want)
	}
}
