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
)

// JiraCredentials identifies the Jira Cloud site and the account used for Basic auth.
// Token is resolved by the caller from the environment variable named in config.toml
// (jira.token_env); it never comes from the config file directly.
type JiraCredentials struct {
	Site  string
	Email string
	Token string
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

func (e *JiraAuthError) Error() string { return "Jira API token が無効" }

// Hint points at the token reissue flow, since Atlassian began expiring long-lived tokens in 2026.
func (e *JiraAuthError) Hint() string {
	return "Atlassian は 2026 年からトークンを 1 年で失効させる。https://id.atlassian.com/manage-profile/security/api-tokens で再発行し、環境変数を更新する"
}

// JiraRateLimitError reports a 429. RetryAfter is 0 when the server sent no usable header.
type JiraRateLimitError struct {
	RetryAfter time.Duration
}

func (e *JiraRateLimitError) Error() string { return "Jira のレート制限に達した" }

// Hint tells the caller to respect Retry-After.
func (e *JiraRateLimitError) Hint() string {
	return "Retry-After の時間だけ待ってから再試行する（このサイクルの残りの Jira 取得は中断した）"
}

// JiraStatusError reports any other non-2xx response.
type JiraStatusError struct {
	StatusCode int
	Body       string
}

func (e *JiraStatusError) Error() string {
	return fmt.Sprintf("Jira API が %d を返した: %s", e.StatusCode, e.Body)
}

// JiraNotConfiguredError reports that jira.site/email or the token environment variable is unset.
type JiraNotConfiguredError struct{}

func (e *JiraNotConfiguredError) Error() string { return "Jira の設定がない" }

// Hint points at the config keys that must be filled in.
func (e *JiraNotConfiguredError) Hint() string {
	return "config.toml の [jira] site/email、および token_env が指す環境変数を設定する"
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
		return nil, &JiraNotConfiguredError{}
	}

	endpoint := jiraIssueEndpoint(creds.Site, key)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("Jira へのリクエストを作れない: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.SetBasicAuth(creds.Email, creds.Token)

	resp, err := f.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Jira への接続に失敗した: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("Jira の応答を読めない: %w", err)
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
		return nil, fmt.Errorf("Jira の応答を解析できない: %w", err)
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
// "dena.atlassian.net"); tests point it at an httptest server, whose URL already
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
