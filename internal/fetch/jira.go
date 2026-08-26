package fetch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ukwhatn/taskherd/internal/i18n"
)

// JiraCredentials identifies the Jira Cloud site and the account used for Basic auth.
// Token is resolved by the caller from the environment variable or the file named in config.toml
// (jira.token_env / jira.token_file); it never comes from the config file directly.
type JiraCredentials struct {
	Site  string
	Email string
	Token string
	// TokenReason says why Token is empty when the caller tried and failed to resolve one. It is
	// what turns "Jira is not configured" from a dead end into something the user can act on, so it
	// names the path and the failure but never the token.
	TokenReason string
}

// Configured reports whether enough is set to attempt a fetch.
func (c JiraCredentials) Configured() bool {
	return c.Site != "" && c.Email != "" && c.Token != ""
}

// JiraData is the normalized status of a Jira issue.
type JiraData struct {
	StatusName     string `json:"status_name"`
	StatusCategory string `json:"status_category"`
	Summary        string `json:"summary"`
	UpdatedAt      string `json:"updated_at"`
}

// JiraAuthError reports a 401: the API token is invalid, or has expired.
type JiraAuthError struct{}

func (e *JiraAuthError) Error() string {
	text, _ := e.Localize(i18n.For(i18n.LangEN))
	return text
}

// Localize states the problem and points at the token reissue flow, since Atlassian began
// expiring long-lived tokens in 2026.
func (e *JiraAuthError) Localize(t *i18n.Catalog) (string, string) {
	entry := i18n.OrDefault(t).Err.Live.JiraAuth
	return entry.Msg, entry.Hint
}

// JiraRateLimitError reports a 429. RetryAfter is 0 when the server sent no usable header.
type JiraRateLimitError struct {
	RetryAfter time.Duration
}

func (e *JiraRateLimitError) Error() string {
	text, _ := e.Localize(i18n.For(i18n.LangEN))
	return text
}

// Localize states the problem and tells the caller to respect Retry-After.
func (e *JiraRateLimitError) Localize(t *i18n.Catalog) (string, string) {
	entry := i18n.OrDefault(t).Err.Live.JiraRateLimited
	return entry.Msg, entry.Hint
}

// JiraStatusError reports any other non-2xx response.
type JiraStatusError struct {
	StatusCode int
	Body       string
}

func (e *JiraStatusError) Error() string {
	text, _ := e.Localize(i18n.For(i18n.LangEN))
	return text
}

// Localize states the status and shows Jira's own body under it.
func (e *JiraStatusError) Localize(t *i18n.Catalog) (string, string) {
	entry := i18n.OrDefault(t).Err.Live.JiraStatus
	return fmt.Sprintf(entry.Msg, e.StatusCode, e.Body), entry.Hint
}

// JiraNotConfiguredError reports that jira.site/email is unset, or that no token could be resolved.
type JiraNotConfiguredError struct {
	// Reason is the caller's explanation when it tried to resolve a token and could not.
	Reason string
}

func (e *JiraNotConfiguredError) Error() string {
	text, _ := e.Localize(i18n.For(i18n.LangEN))
	return text
}

// Localize states what is missing and points at the config keys that must be filled in.
func (e *JiraNotConfiguredError) Localize(t *i18n.Catalog) (string, string) {
	live := i18n.OrDefault(t).Err.Live
	if e.Reason == "" {
		return live.JiraNotConfigured.Msg, live.JiraNotConfigured.Hint
	}
	return fmt.Sprintf(live.JiraNotConfiguredWhy, e.Reason), live.JiraNotConfigured.Hint
}

// JiraFetcher fetches issue status from Jira Cloud's REST API via net/http directly:
// the only call needed is a single field-scoped GET, which does not justify depending
// on jira-cli or go-jira.
type JiraFetcher struct {
	Client *http.Client
}

// NewJiraFetcher returns a JiraFetcher using http.DefaultClient.
func NewJiraFetcher() *JiraFetcher {
	return &JiraFetcher{Client: http.DefaultClient}
}

// FetchIssue fetches summary/status/updated for key using HTTP Basic auth.
func (f *JiraFetcher) FetchIssue(ctx context.Context, creds JiraCredentials, key string) (*JiraData, error) {
	if !creds.Configured() {
		return nil, &JiraNotConfiguredError{Reason: creds.TokenReason}
	}

	endpoint := jiraIssueEndpoint(creds.Site, key)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("cannot build the request to Jira: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.SetBasicAuth(creds.Email, creds.Token)

	resp, err := f.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cannot reach Jira: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("cannot read Jira's response: %w", err)
	}

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized:
		return nil, &JiraAuthError{}
	case http.StatusTooManyRequests:
		return nil, &JiraRateLimitError{RetryAfter: parseRetryAfterSeconds(resp.Header.Get("Retry-After"))}
	default:
		return nil, &JiraStatusError{StatusCode: resp.StatusCode, Body: strings.TrimSpace(string(body))}
	}

	var raw jiraIssueResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("cannot parse Jira's response: %w", err)
	}
	return &JiraData{
		StatusName:     raw.Fields.Status.Name,
		StatusCategory: raw.Fields.Status.StatusCategory.Key,
		Summary:        raw.Fields.Summary,
		UpdatedAt:      raw.Fields.Updated,
	}, nil
}

type jiraIssueResponse struct {
	Fields struct {
		Summary string `json:"summary"`
		Status  struct {
			Name           string `json:"name"`
			StatusCategory struct {
				Key string `json:"key"`
			} `json:"statusCategory"`
		} `json:"status"`
		Updated string `json:"updated"`
	} `json:"fields"`
}

// jiraIssueEndpoint builds the GET URL. site is a bare host in config.toml (e.g.
// "example.atlassian.net"); tests point it at an httptest server, whose URL already
// carries a scheme, so an existing scheme is left as-is rather than doubled up.
func jiraIssueEndpoint(site, key string) string {
	if !strings.Contains(site, "://") {
		site = "https://" + site
	}
	site = strings.TrimSuffix(site, "/")
	return fmt.Sprintf("%s/rest/api/3/issue/%s?fields=summary,status,updated", site, key)
}

// parseRetryAfterSeconds handles the delta-seconds form of Retry-After. The HTTP-date
// form is rare for APIs (Jira's own examples use delta-seconds) and is treated as absent.
func parseRetryAfterSeconds(v string) time.Duration {
	secs, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || secs < 0 {
		return 0
	}
	return time.Duration(secs) * time.Second
}
