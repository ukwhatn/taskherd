// Package fetch retrieves live status for external links (GitHub PRs/issues via the gh
// CLI, Jira issues via REST) and caches the results in cache.json.
package fetch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/ukwhatn/taskherd/internal/model"
)

// CheckStatus is the folded result of GitHub's statusCheckRollup.
type CheckStatus string

const (
	CheckPass    CheckStatus = "pass"
	CheckFail    CheckStatus = "fail"
	CheckPending CheckStatus = "pending"
	CheckNone    CheckStatus = "none"
)

// checkRollupItem is the union of CheckRun and StatusContext returned by
// `gh pr view --json statusCheckRollup`; __typename discriminates between the two shapes.
type checkRollupItem struct {
	Typename   string `json:"__typename"`
	Status     string `json:"status"`     // CheckRun only
	Conclusion string `json:"conclusion"` // CheckRun only
	State      string `json:"state"`      // StatusContext only
}

// FoldStatusCheckRollupJSON parses raw statusCheckRollup JSON and folds it to one CheckStatus.
func FoldStatusCheckRollupJSON(raw []byte) (CheckStatus, error) {
	var items []checkRollupItem
	if err := json.Unmarshal(raw, &items); err != nil {
		return "", fmt.Errorf("statusCheckRollup を解析できない: %w", err)
	}
	return foldStatusCheckRollup(items), nil
}

// foldStatusCheckRollup aggregates the normalized items with fail > pending > pass priority;
// an empty rollup means no checks are configured at all.
func foldStatusCheckRollup(items []checkRollupItem) CheckStatus {
	if len(items) == 0 {
		return CheckNone
	}
	sawPending := false
	for _, item := range items {
		switch classifyCheckItem(item) {
		case CheckFail:
			return CheckFail
		case CheckPending:
			sawPending = true
		}
	}
	if sawPending {
		return CheckPending
	}
	return CheckPass
}

func classifyCheckItem(item checkRollupItem) CheckStatus {
	switch item.Typename {
	case "CheckRun":
		switch item.Conclusion {
		case "FAILURE", "TIMED_OUT", "CANCELLED", "ACTION_REQUIRED", "STARTUP_FAILURE":
			return CheckFail
		case "SUCCESS", "SKIPPED", "NEUTRAL":
			if item.Status == "COMPLETED" {
				return CheckPass
			}
			return CheckPending
		default:
			return CheckPending
		}
	case "StatusContext":
		switch item.State {
		case "FAILURE", "ERROR":
			return CheckFail
		case "SUCCESS":
			return CheckPass
		default: // PENDING, EXPECTED and anything else still running
			return CheckPending
		}
	default:
		// An unrecognized __typename (a future GraphQL type, or a malformed entry) is
		// folded as pending rather than pass, so it never reads as "all green" by default.
		return CheckPending
	}
}

// GitHubData is the normalized status of a GitHub PR or issue.
type GitHubData struct {
	State          string `json:"state"`
	IsDraft        bool   `json:"is_draft"`
	ReviewDecision string `json:"review_decision"`
	Checks         string `json:"checks"`
	Title          string `json:"title"`
	UpdatedAt      string `json:"updated_at"`
}

// GHNotFoundError reports that the gh binary itself could not be found on PATH.
type GHNotFoundError struct{}

func (e *GHNotFoundError) Error() string { return "gh コマンドが見つからない" }

// Hint tells the user how to install gh.
func (e *GHNotFoundError) Hint() string {
	return "https://cli.github.com/ から GitHub CLI (gh) を導入する"
}

// GHRateLimitError reports that gh's stderr indicated a GitHub rate limit.
type GHRateLimitError struct {
	Stderr string
}

func (e *GHRateLimitError) Error() string {
	return "GitHub のレート制限に達した: " + strings.TrimSpace(e.Stderr)
}

// Hint tells the user to back off.
func (e *GHRateLimitError) Hint() string {
	return "しばらく待ってから再試行する（このサイクルの残りの GitHub 取得は中断した）"
}

// GHCommandError reports that gh ran but exited non-zero for a reason other than a rate
// limit (missing auth, no permission, unknown host, ...). stderr is shown verbatim because
// which account or host needs attention is gh's call, not this program's guess.
type GHCommandError struct {
	Stderr string
}

func (e *GHCommandError) Error() string {
	msg := strings.TrimSpace(e.Stderr)
	if msg == "" {
		msg = "gh コマンドが失敗した"
	}
	return msg
}

// Hint suggests the account-switch command without guessing which account to use.
func (e *GHCommandError) Hint() string {
	return "`gh auth switch --hostname <host>` でアカウントを切り替えるか、config の [github.accounts] に \"<host>/<owner>\" 形式でアカウントを指定する"
}

// GitHubRunner executes gh and returns its stdout, stderr and process error. env holds extra
// environment entries for that one invocation, which is how a per-host token is handed over
// without touching the rest of the process environment.
//
// It is a function type (not a struct method) so GitHubFetcher.Run can be swapped
// wholesale in tests without spawning a real gh process.
type GitHubRunner func(ctx context.Context, env []string, args ...string) (stdout, stderr []byte, err error)

// GitHubFetcher fetches PR/issue status by shelling out to gh. gh resolves the host and
// auth from the URL itself, so no --repo or GH_HOST handling is needed here.
type GitHubFetcher struct {
	Run GitHubRunner
	// Accounts names the gh account to use, keyed either by host ("github.com") or by host and
	// owner ("github.com/some-org"), from config's [github.accounts]. A link matching no entry is
	// fetched with whichever account gh has active, which is the old behaviour.
	Accounts map[string]string

	// tokens caches one resolution per credential — a host and an account name — for the life of
	// the process, so two owners served by the same account cost one gh process rather than two
	// while two owners on different accounts still get their own token. A token is held in memory
	// only: it is never written to config, to cache.json, or to any message.
	mu     sync.Mutex
	tokens map[string]tokenLookup
}

// tokenLookup is one link's resolved credential: the environment to run gh with, or the reason
// there is none and gh's active account is standing in.
//
// key and account name what was used without naming the token, which is what makes a failed fetch
// diagnosable: the common failure is a link read with an account that cannot see its repository.
type tokenLookup struct {
	env     []string
	token   string
	key     string
	account string
	err     error
}

// NewGitHubFetcher returns a GitHubFetcher that runs the real gh binary.
func NewGitHubFetcher(accounts map[string]string) *GitHubFetcher {
	return &GitHubFetcher{Run: runGH, Accounts: accounts}
}

func runGH(ctx context.Context, env []string, args ...string) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, "gh", args...)
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	stdout, err := cmd.Output()
	var stderr []byte
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		stderr = exitErr.Stderr
	}
	return stdout, stderr, err
}

// credentials resolves the account configured for the URL into the environment gh should run with.
//
// A failure here is not fatal: gh still has its active account, so the fetch is attempted anyway
// and the reason is carried along to be told only if that attempt also fails.
func (f *GitHubFetcher) credentials(ctx context.Context, url string) tokenLookup {
	host := strings.ToLower(model.LinkHost(url))
	if host == "" {
		return tokenLookup{}
	}
	key, account := f.lookupAccount(host, strings.ToLower(model.LinkOwner(url)))
	if account == "" {
		return tokenLookup{}
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	cacheKey := host + "\x00" + account
	lookup, ok := f.tokens[cacheKey]
	if !ok {
		lookup = f.resolveToken(ctx, host, account)
		if f.tokens == nil {
			f.tokens = map[string]tokenLookup{}
		}
		f.tokens[cacheKey] = lookup
	}
	// The key is stamped on the returned copy rather than cached with the token, so a message
	// names the entry this link matched even when the token came from another owner's entry.
	lookup.key, lookup.account = key, account
	return lookup
}

// lookupAccount picks the configured account for a link, most specific first: an entry naming
// "<host>/<owner>" wins over one naming just "<host>", and matching neither leaves the link to
// gh's active account. It returns the config key as written, for messages to quote.
//
// Owner beats host because one host carries both personal and organization repositories and no
// single account can read both; a host-only entry is the older form and stays the fallback.
func (f *GitHubFetcher) lookupAccount(host, owner string) (key, account string) {
	var hostKey, hostAccount string
	for configured, name := range f.Accounts {
		switch normalizeAccountKey(configured) {
		case host + "/" + owner:
			if owner != "" {
				return configured, strings.TrimSpace(name)
			}
		case host:
			hostKey, hostAccount = configured, strings.TrimSpace(name)
		}
	}
	return hostKey, hostAccount
}

// normalizeAccountKey folds a [github.accounts] key to the form host and owner are compared in.
// Surrounding whitespace and a trailing slash are what a hand-written config actually varies by.
func normalizeAccountKey(key string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(key), "/"))
}

// resolveToken asks gh for the named account's token. gh is run with no added environment, so an
// already-resolved token for another host cannot influence the answer.
func (f *GitHubFetcher) resolveToken(ctx context.Context, host, account string) tokenLookup {
	stdout, stderr, err := f.Run(ctx, nil, "auth", "token", "--hostname", host, "--user", account)
	if err != nil {
		return tokenLookup{err: fmt.Errorf("config の github.accounts が指定する %s のアカウント %q のトークンを取得できない（gh の active account で続行）: %s",
			host, account, strings.TrimSpace(string(stderr)))}
	}
	token := strings.TrimSpace(string(stdout))
	if token == "" {
		return tokenLookup{err: fmt.Errorf("config の github.accounts が指定する %s のアカウント %q に対して gh がトークンを返さなかった（gh の active account で続行）", host, account)}
	}
	// GH_TOKEN is what gh reads for github.com and GH_ENTERPRISE_TOKEN for a GHES host. Both are
	// set to the same host-specific token because each invocation addresses exactly one host, so
	// the fetcher does not have to decide which of the two gh will consult.
	return tokenLookup{
		env:   []string{"GH_TOKEN=" + token, "GH_ENTERPRISE_TOKEN=" + token},
		token: token,
	}
}

// FetchPR fetches PR status. url is passed straight to gh: gh parses the host and repo
// from it and resolves the matching authenticated account itself.
// The --json field lists match the implementation master (§8.1) verbatim, including
// mergedAt/closedAt/stateReason that GitHubData does not surface today: keeping the gh
// invocation itself faithful to the master leaves room for PR-4 to extend GitHubData
// without having to first notice the fetch call was under-requesting fields.
func (f *GitHubFetcher) FetchPR(ctx context.Context, url string) (*GitHubData, error) {
	creds := f.credentials(ctx, url)
	stdout, stderr, err := f.Run(ctx, creds.env, "pr", "view", url, "--json",
		"state,isDraft,mergedAt,closedAt,reviewDecision,statusCheckRollup,title,updatedAt")
	if err != nil {
		return nil, classifyGHError(err, stderr, creds)
	}

	var raw struct {
		State             string            `json:"state"`
		IsDraft           bool              `json:"isDraft"`
		MergedAt          string            `json:"mergedAt"`
		ClosedAt          string            `json:"closedAt"`
		ReviewDecision    string            `json:"reviewDecision"`
		StatusCheckRollup []checkRollupItem `json:"statusCheckRollup"`
		Title             string            `json:"title"`
		UpdatedAt         string            `json:"updatedAt"`
	}
	if err := json.Unmarshal(stdout, &raw); err != nil {
		return nil, fmt.Errorf("gh pr view の出力を解析できない: %w", err)
	}

	return &GitHubData{
		State:          raw.State,
		IsDraft:        raw.IsDraft,
		ReviewDecision: raw.ReviewDecision,
		Checks:         string(foldStatusCheckRollup(raw.StatusCheckRollup)),
		Title:          raw.Title,
		UpdatedAt:      raw.UpdatedAt,
	}, nil
}

// FetchIssue fetches issue status. Issues have no statusCheckRollup, so Checks is always none.
func (f *GitHubFetcher) FetchIssue(ctx context.Context, url string) (*GitHubData, error) {
	creds := f.credentials(ctx, url)
	stdout, stderr, err := f.Run(ctx, creds.env, "issue", "view", url, "--json", "state,stateReason,closedAt,title,updatedAt")
	if err != nil {
		return nil, classifyGHError(err, stderr, creds)
	}

	var raw struct {
		State       string `json:"state"`
		StateReason string `json:"stateReason"`
		ClosedAt    string `json:"closedAt"`
		Title       string `json:"title"`
		UpdatedAt   string `json:"updatedAt"`
	}
	if err := json.Unmarshal(stdout, &raw); err != nil {
		return nil, fmt.Errorf("gh issue view の出力を解析できない: %w", err)
	}

	return &GitHubData{
		State:     raw.State,
		Checks:    string(CheckNone),
		Title:     raw.Title,
		UpdatedAt: raw.UpdatedAt,
	}, nil
}

// ghRepoNotFound is what gh's GraphQL error looks like when the account it authenticated as
// cannot see the repository at all. A link fetched with the wrong account is indistinguishable
// from a repository that does not exist, so the message has to say which account was used.
const ghRepoNotFound = "could not resolve to a repository"

// accountOwnerHint is the way out of the failure above, spelled out because the host-form config
// that causes it looks correct: it is the owner, not the host, that decides the account.
const accountOwnerHint = `リポジトリが見えないアカウントで取得している可能性がある。` +
	`config の [github.accounts] に "<host>/<owner>" 形式で owner ごとのアカウントを指定する（例: "github.com/some-org" = "work-account"）。` +
	`同一ホストに個人と組織が混在する場合、ホスト単位の指定では解決できない`

func classifyGHError(err error, stderr []byte, creds tokenLookup) error {
	if errors.Is(err, exec.ErrNotFound) {
		return &GHNotFoundError{}
	}
	msg := scrubToken(string(stderr), creds.token)
	if strings.Contains(strings.ToLower(msg), "rate limit") {
		return &GHRateLimitError{Stderr: msg}
	}
	// The account resolution failure is told here rather than when it happened: on its own it is
	// not a problem, because gh's active account may well have been able to read the link.
	if creds.err != nil {
		msg = strings.TrimSpace(msg) + "\n" + creds.err.Error()
	}
	if strings.Contains(strings.ToLower(msg), ghRepoNotFound) {
		msg = strings.TrimSpace(msg) + "\n" + creds.describe() + "\n" + accountOwnerHint
	}
	return &GHCommandError{Stderr: msg}
}

// describe names the account a fetch ran as, and never the token: this text is written to
// cache.json and shown on screen.
func (l tokenLookup) describe() string {
	switch {
	case l.account == "":
		return "取得に使ったアカウント: gh の active account（config の [github.accounts] に一致する指定がない）"
	case l.token == "":
		return fmt.Sprintf("取得に使ったアカウント: gh の active account（config の [github.accounts] の %q = %q はトークンを解決できなかった）", l.key, l.account)
	default:
		return fmt.Sprintf("取得に使ったアカウント: %q（config の [github.accounts] の %q）", l.account, l.key)
	}
}

// scrubToken removes a resolved token from text on its way to a message, because a failure is
// written to cache.json and cache.json is a file on disk.
func scrubToken(text, token string) string {
	if token == "" {
		return text
	}
	return strings.ReplaceAll(text, token, "***")
}
