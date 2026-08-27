package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// authServer records the Authorization header of the one request it answers.
func authServer(t *testing.T, seen *string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*seen = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name":"v9.0.0"}`))
	}))
	t.Cleanup(server.Close)
	return server
}

func TestFetchLatestSendsTheToken(t *testing.T) {
	var seen string
	server := authServer(t, &seen)
	c := newChecker(t, server.URL, baseTime)
	c.Token = func(context.Context) string { return "s3cret" }

	if _, err := c.FetchLatest(context.Background()); err != nil {
		t.Fatalf("FetchLatest() error = %v", err)
	}
	if seen != "Bearer s3cret" {
		t.Errorf("Authorization = %q, want %q", seen, "Bearer s3cret")
	}
}

// An empty token is the answer of a machine with no gh, no account, or no config, and it has to
// leave the request exactly as it was before tokens existed rather than sending "Bearer ".
func TestFetchLatestOmitsAnEmptyToken(t *testing.T) {
	for _, tc := range []struct {
		name  string
		token func(context.Context) string
	}{
		{"nil", nil},
		{"empty", func(context.Context) string { return "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var seen string
			server := authServer(t, &seen)
			c := newChecker(t, server.URL, baseTime)
			c.Token = tc.token

			if _, err := c.FetchLatest(context.Background()); err != nil {
				t.Fatalf("FetchLatest() error = %v", err)
			}
			if seen != "" {
				t.Errorf("Authorization = %q, want it unset", seen)
			}
		})
	}
}

// The token is read per request rather than once, so a checker held across a long-lived board
// picks up a token gh only starts answering for later.
func TestFetchLatestReadsTheTokenEachTime(t *testing.T) {
	var seen string
	server := authServer(t, &seen)
	c := newChecker(t, server.URL, baseTime)
	calls := 0
	c.Token = func(context.Context) string {
		calls++
		return "token" + string(rune('0'+calls))
	}

	for want := 1; want <= 2; want++ {
		if _, err := c.FetchLatest(context.Background()); err != nil {
			t.Fatalf("FetchLatest() error = %v", err)
		}
	}
	if calls != 2 {
		t.Errorf("Token called %d times, want 2", calls)
	}
	if seen != "Bearer token2" {
		t.Errorf("Authorization = %q, want the second token", seen)
	}
}
