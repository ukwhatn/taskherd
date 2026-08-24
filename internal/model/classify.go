package model

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// jiraKeyPattern は Jira の課題キー（PROJ-123）。実在するキーを弾かないため大小文字を問わない。
var jiraKeyPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*-[0-9]+$`)

const githubHost = "github.com"

// URLClassifier は config の設定値に基づいて URL の種別を判別する。
type URLClassifier struct {
	GHESHosts []string
	JiraSite  string
}

// Classify は URL から LinkKind を判別する。判別できない URL は LinkKindOther。
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

func (c URLClassifier) isGHESHost(host string) bool {
	for _, candidate := range c.GHESHosts {
		if normalizeHost(candidate) == host {
			return true
		}
	}
	return false
}

// classifyGitHubPath は <owner>/<repo>/pull|issues/<n> を判別する。
// 後続セグメント（/files 等）は同じ PR/Issue を指すため無視する。
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

// normalizeHost はスキーム・パス・ポートを落として比較用のホスト名にする。
// config には "https://dena.atlassian.net/" のような表記も書かれうる。
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
