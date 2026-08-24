package model

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// jiraKeyPattern matches a Jira issue key (PROJ-123). Case is ignored so real keys are not rejected.
var jiraKeyPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*-[0-9]+$`)

const githubHost = "github.com"

// URLClassifier decides the kind of a URL from the configured hosts.
type URLClassifier struct {
	GHESHosts []string
	JiraSite  string
}

// Classify returns the LinkKind of raw. Anything unrecognized is LinkKindOther.
func (c URLClassifier) Classify(raw string) LinkKind {
	u, err := url.Parse(raw)
	if err != nil {
		return LinkKindOther
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	default:
		return LinkKindOther
	}

	host := strings.ToLower(u.Hostname())
	if host == "" {
		return LinkKindOther
	}
	segments := pathSegments(u.Path)

	if host == githubHost || c.isGHESHost(host) {
		return classifyGitHubPath(segments)
	}
	if site := normalizeHost(c.JiraSite); site != "" && host == site {
		if len(segments) == 2 && segments[0] == "browse" && jiraKeyPattern.MatchString(segments[1]) {
			return LinkKindJira
		}
	}
	return LinkKindOther
}

// JiraKey extracts the issue key from a URL that Classify would report as LinkKindJira.
// Case is preserved: Jira's REST API accepts either case, so no canonical form is imposed here.
func (c URLClassifier) JiraKey(raw string) (string, bool) {
	if c.Classify(raw) != LinkKindJira {
		return "", false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", false
	}
	segments := pathSegments(u.Path)
	return segments[1], true
}

func (c URLClassifier) isGHESHost(host string) bool {
	for _, candidate := range c.GHESHosts {
		if normalizeHost(candidate) == host {
			return true
		}
	}
	return false
}

// classifyGitHubPath matches <owner>/<repo>/pull|issues/<n>.
// Trailing segments (/files and friends) still point at the same PR or issue, so they are ignored.
func classifyGitHubPath(segments []string) LinkKind {
	if len(segments) < 4 || !isPositiveInt(segments[3]) {
		return LinkKindOther
	}
	switch segments[2] {
	case "pull":
		return LinkKindGitHubPR
	case "issues":
		return LinkKindGitHubIssue
	default:
		return LinkKindOther
	}
}

// normalizeHost strips scheme, path and port for comparison,
// because config values may be written as "https://example.atlassian.net/".
func normalizeHost(raw string) string {
	host := strings.TrimSpace(strings.ToLower(raw))
	if host == "" {
		return ""
	}
	if idx := strings.Index(host, "://"); idx >= 0 {
		host = host[idx+3:]
	}
	host = strings.TrimSuffix(host, "/")
	if idx := strings.IndexAny(host, "/:"); idx >= 0 {
		host = host[:idx]
	}
	return host
}

func pathSegments(path string) []string {
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "/")
}

func isPositiveInt(s string) bool {
	n, err := strconv.Atoi(s)
	return err == nil && n > 0
}

// GitHubRef is the owner, repository and number a GitHub PR or issue URL points at.
type GitHubRef struct {
	Owner  string
	Repo   string
	Number int
}

// GitHubRef parses a GitHub PR or issue URL into its parts, reporting whether it was one.
//
// The board identifies a link by this rather than by anything it fetched, so a card can name the
// PR it links to before — or without ever — reaching the network.
func (c URLClassifier) GitHubRef(raw string) (GitHubRef, bool) {
	switch c.Classify(raw) {
	case LinkKindGitHubPR, LinkKindGitHubIssue:
	default:
		return GitHubRef{}, false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return GitHubRef{}, false
	}
	segments := pathSegments(u.Path)
	number, err := strconv.Atoi(segments[3])
	if err != nil {
		return GitHubRef{}, false
	}
	return GitHubRef{Owner: segments[0], Repo: segments[1], Number: number}, true
}

// LinkHost is the host a URL addresses, for links whose kind carries no reference of its own.
// It is empty when raw is not a URL with a host.
func LinkHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Hostname()
}
