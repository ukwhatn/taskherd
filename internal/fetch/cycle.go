package fetch

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/ukwhatn/taskherd/internal/model"
)

// providerConcurrency is how many links of one kind are fetched at a time.
//
// The ceiling that matters is GitHub's secondary rate limit on concurrent requests, which is far
// above this; the per-hour GraphQL point budget is untouched by a few dozen links. Eight keeps a
// board-sized refresh at a few seconds without being close enough to either limit to need tuning,
// which is why this is a constant rather than a config knob.
const providerConcurrency = 8

// Fetcher runs one refresh cycle over a set of link URLs, writing results into Cache.
// GitHub and Jira run as two independent cycles side by side, each with its own concurrency
// and its own rate-limit stop, so a rate limit on one provider never touches the other's
// remaining requests (§5.3/§8.3 of the implementation master).
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

// attempt is one link's slot in a provider's run. Each worker owns exactly one slot, so the
// slice needs no lock, and the fixed slice order is what keeps Outcomes deterministic even
// though the fetches complete out of order.
type attempt struct {
	url       string
	at        time.Time
	data      any
	err       error
	attempted bool
}

// RefreshLinks fetches the current status of each url and stores it in Cache. URLs whose
// kind is not github_pr/github_issue/jira are silently skipped: there is nothing to fetch
// for an "other" link.
//
// Fetches run concurrently, but the cache is written once at the end of the cycle rather than
// once per link: a per-link write would serialize behind the cache lock everything the
// concurrency just bought. The trade is that a cycle killed partway through leaves nothing
// behind, where the old serial code kept whatever it had already fetched. Losing a cycle costs
// only a repeat of it, so the write is folded into a single transaction.
func (f *Fetcher) RefreshLinks(ctx context.Context, urls []string) (*RefreshResult, error) {
	githubURLs, jiraURLs := f.partitionByKind(urls)
	github := make([]attempt, len(githubURLs))
	jira := make([]attempt, len(jiraURLs))

	var githubStopped, jiraStopped atomic.Bool

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		f.runProvider(ctx, githubURLs, github, f.fetchGitHub, isGHRateLimit, &githubStopped)
	}()
	go func() {
		defer wg.Done()
		f.runProvider(ctx, jiraURLs, jira, f.fetchJira, isJiraRateLimit, &jiraStopped)
	}()
	wg.Wait()

	result := &RefreshResult{
		GitHubInterrupted: githubStopped.Load(),
		JiraInterrupted:   jiraStopped.Load(),
	}
	f.commit(ctx, github, jira, result)
	return result, nil
}

// runProvider fetches one kind's links with a bounded number in flight.
//
// A rate limit stops the run the same way the serial version's break did: the links that have
// not started are never attempted, and so never appear in Outcomes. Requests already in flight
// are left to finish rather than cancelled — they were already spent, and their results are
// worth keeping — so a stopped run reports at most concurrency-1 more links than a serial one.
func (f *Fetcher) runProvider(
	ctx context.Context,
	urls []string,
	attempts []attempt,
	doFetch func(context.Context, string) (any, error),
	isRateLimit func(error) bool,
	stopped *atomic.Bool,
) {
	var g errgroup.Group
	g.SetLimit(providerConcurrency)

	for i, url := range urls {
		g.Go(func() error {
			if stopped.Load() {
				return nil
			}
			data, err := doFetch(ctx, url)
			attempts[i] = attempt{url: url, at: f.Now(), data: data, err: err, attempted: true}
			if isRateLimit(err) {
				stopped.Store(true)
			}
			return nil
		})
	}
	_ = g.Wait()
}

// commit writes every attempted link into the cache under one lock/read/write transaction and
// fills in result.Outcomes. A fetch that failed still gets an entry, so a link that has been
// broken for a while keeps reporting how long.
func (f *Fetcher) commit(ctx context.Context, github, jira []attempt, result *RefreshResult) {
	updateErr := f.Cache.Update(ctx, func(cf *CacheFile) {
		for _, group := range [][]attempt{github, jira} {
			for i := range group {
				a := &group[i]
				if !a.attempted {
					continue
				}
				if a.err != nil {
					cf.SetFailure(a.url, a.err, a.at)
					continue
				}
				if err := cf.SetSuccess(a.url, a.data, a.at); err != nil {
					a.err = err
				}
			}
		}
	})

	for _, group := range [][]attempt{github, jira} {
		for i := range group {
			a := &group[i]
			if !a.attempted {
				continue
			}
			err := a.err
			if updateErr != nil {
				err = updateErr
			}
			result.Outcomes = append(result.Outcomes, RefreshOutcome{URL: a.url, Err: err})
		}
	}
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
