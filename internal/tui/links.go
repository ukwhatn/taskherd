package tui

import (
	"fmt"
	"strings"
)

// parseLinkURLs splits a batch of URLs separated by spaces or newlines and validates each one.
//
// The batch is all-or-nothing: one malformed entry rejects the whole input rather than quietly
// storing the rest, so what the user is told matches what was written.
func parseLinkURLs(raw string) ([]string, error) {
	seen := map[string]bool{}
	var urls []string
	for _, field := range strings.Fields(raw) {
		if !strings.Contains(field, "://") {
			return nil, fmt.Errorf("URL はスキームを含めて指定する（例: https://github.com/owner/repo/pull/1）: %q", field)
		}
		if seen[field] {
			continue
		}
		seen[field] = true
		urls = append(urls, field)
	}
	return urls, nil
}

// splitTitleLines turns a multi-line value into one title per line, dropping blank lines.
func splitTitleLines(raw string) []string {
	var titles []string
	for _, line := range strings.Split(raw, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			titles = append(titles, trimmed)
		}
	}
	return titles
}
