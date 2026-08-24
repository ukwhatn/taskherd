// Package fetch retrieves live status for external links (GitHub PRs/issues via the gh
// CLI, Jira issues via REST) and caches the results in cache.json.
package fetch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
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
	return "`gh auth switch --hostname <host>` でアカウントを切り替えるか、認証・権限を確認する"
}

// GitHubRunner executes gh and returns its stdout, stderr and process error.
// It is a function type (not a struct method) so GitHubFetcher.Run can be swapped
// wholesale in tests without spawning a real gh process.
type GitHubRunner func(ctx context.Context, args ...string) (stdout, stderr []byte, err error)

// GitHubFetcher fetches PR/issue status by shelling out to gh. gh resolves the host and
// auth from the URL itself, so no --repo or GH_HOST handling is needed here.
type GitHubFetcher struct {
	Run GitHubRunner
}

// NewGitHubFetcher returns a GitHubFetcher that runs the real gh binary.
func NewGitHubFetcher() *GitHubFetcher {
	return &GitHubFetcher{Run: runGH}
}

func runGH(ctx context.Context, args ...string) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, "gh", args...)
	stdout, err := cmd.Output()
	var stderr []byte
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		stderr = exitErr.Stderr
	}
	return stdout, stderr, err
}

// FetchPR fetches PR status. url is passed straight to gh: gh parses the host and repo
// from it and resolves the matching authenticated account itself.
// The --json field lists match the implementation master (§8.1) verbatim, including
// mergedAt/closedAt/stateReason that GitHubData does not surface today: keeping the gh
// invocation itself faithful to the master leaves room for PR-4 to extend GitHubData
// without having to first notice the fetch call was under-requesting fields.
func (f *GitHubFetcher) FetchPR(ctx context.Context, url string) (*GitHubData, error) {
	stdout, stderr, err := f.Run(ctx, "pr", "view", url, "--json",
		"state,isDraft,mergedAt,closedAt,reviewDecision,statusCheckRollup,title,updatedAt")
	if err != nil {
		return nil, classifyGHError(err, stderr)
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
	stdout, stderr, err := f.Run(ctx, "issue", "view", url, "--json", "state,stateReason,closedAt,title,updatedAt")
	if err != nil {
		return nil, classifyGHError(err, stderr)
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

func classifyGHError(err error, stderr []byte) error {
	if errors.Is(err, exec.ErrNotFound) {
		return &GHNotFoundError{}
	}
	msg := string(stderr)
	if strings.Contains(strings.ToLower(msg), "rate limit") {
		return &GHRateLimitError{Stderr: msg}
	}
	return &GHCommandError{Stderr: msg}
}
