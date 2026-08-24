package fetch

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ukwhatn/taskherd/internal/model"
)

// Fetcher runs one refresh cycle over a set of link URLs, writing results into Cache.
// GitHub and Jira links are each worked through serially within their own kind, so a rate
// limit on one provider never touches the other's remaining requests (§5.3/§8.3 of the
// implementation master). This is the cycle-execution API PR-4's board is expected to call
// on a timer; PR-3 only wires it to the `refresh` command.
type Fetcher struct {
	GitHub     *GitHubFetcher
	Jira       *JiraFetcher
	Cache      *Cache
	Classifier model.URLClassifier
	JiraCreds  JiraCredentials // zero value means Jira is not configured
	Now        func() time.Time
}

// RefreshOutcome is the per-link result of one refresh cycle. Err is nil on success.
type RefreshOutcome struct {
	URL string
	Err error
}

// RefreshResult summarizes a refresh cycle. *Interrupted is set when a rate limit stopped
// that kind's remaining links partway through; links never attempted have no Outcome entry.
type RefreshResult struct {
	Outcomes          []RefreshOutcome
	GitHubInterrupted bool
	JiraInterrupted   bool
}

// RefreshLinks fetches the current status of each url and stores it in Cache. URLs whose
// kind is not github_pr/github_issue/jira are silently skipped: there is nothing to fetch
// for an "other" link.
func (f *Fetcher) RefreshLinks(ctx context.Context, urls []string) (*RefreshResult, error) {
	githubURLs, jiraURLs := f.partitionByKind(urls)
	result := &RefreshResult{}

	for _, u := range githubURLs {
		outcome := f.refreshOne(ctx, u, f.fetchGitHub)
		result.Outcomes = append(result.Outcomes, outcome)
		if isGHRateLimit(outcome.Err) {
			result.GitHubInterrupted = true
			break
		}
	}

	for _, u := range jiraURLs {
		outcome := f.refreshOne(ctx, u, f.fetchJira)
		result.Outcomes = append(result.Outcomes, outcome)
		if isJiraRateLimit(outcome.Err) {
			result.JiraInterrupted = true
			break
		}
	}

	return result, nil
}

func (f *Fetcher) partitionByKind(urls []string) (github, jira []string) {
	for _, u := range urls {
		switch f.Classifier.Classify(u) {
		case model.LinkKindGitHubPR, model.LinkKindGitHubIssue:
			github = append(github, u)
		case model.LinkKindJira:
			jira = append(jira, u)
		}
	}
	return github, jira
}

// refreshOne fetches url and writes the outcome into Cache under one lock/read/write
// transaction, so a failed fetch cannot leave a half-updated entry.
func (f *Fetcher) refreshOne(ctx context.Context, url string, doFetch func(context.Context, string) (any, error)) RefreshOutcome {
	data, fetchErr := doFetch(ctx, url)
	now := f.Now()

	updateErr := f.Cache.Update(ctx, func(cf *CacheFile) {
		if fetchErr != nil {
			cf.SetFailure(url, fetchErr, now)
			return
		}
		if err := cf.SetSuccess(url, data, now); err != nil {
			fetchErr = err
		}
	})
	if updateErr != nil {
		return RefreshOutcome{URL: url, Err: updateErr}
	}
	return RefreshOutcome{URL: url, Err: fetchErr}
}

func (f *Fetcher) fetchGitHub(ctx context.Context, url string) (any, error) {
	if f.Classifier.Classify(url) == model.LinkKindGitHubIssue {
		return f.GitHub.FetchIssue(ctx, url)
	}
	return f.GitHub.FetchPR(ctx, url)
}

func (f *Fetcher) fetchJira(ctx context.Context, url string) (any, error) {
	key, ok := f.Classifier.JiraKey(url)
	if !ok {
		return nil, fmt.Errorf("%s から Jira issue key を取り出せない", url)
	}
	return f.Jira.FetchIssue(ctx, f.JiraCreds, key)
}

func isGHRateLimit(err error) bool {
	var rateLimit *GHRateLimitError
	return errors.As(err, &rateLimit)
}

func isJiraRateLimit(err error) bool {
	var rateLimit *JiraRateLimitError
	return errors.As(err, &rateLimit)
}
