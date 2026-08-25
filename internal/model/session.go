package model

import (
	"sort"
	"strconv"
	"strings"
	"time"
)

// cwdRank accumulates the ranking inputs for one cwd across every session in a file.
type cwdRank struct {
	cwd      string
	count    int
	lastUsed time.Time
}

// RankSessionCwds lists every cwd an existing linked session used, most useful first.
//
// The order is frequency, then recency (the newest LinkedAt among the sessions at that cwd), then
// the cwd text itself. The third key exists because LinkedAt has only second resolution: two
// sessions linked within the same second tie on the first two keys, and without a final tiebreaker
// the order would depend on map iteration and could put a different cwd under the cursor from one
// run to the next.
//
// A cwd is trimmed before it is counted, and an empty result of that is skipped: a candidate list
// that included it would read as "no --cwd needed" to a caller deciding whether to require one.
func RankSessionCwds(file File) []string {
	ranks := map[string]*cwdRank{}
	for _, task := range file.Tasks {
		for _, session := range task.Sessions {
			cwd := strings.TrimSpace(session.Cwd)
			if cwd == "" {
				continue
			}
			r, ok := ranks[cwd]
			if !ok {
				r = &cwdRank{cwd: cwd}
				ranks[cwd] = r
			}
			r.count++
			if linkedAt, err := session.LinkedAt.Time(); err == nil && linkedAt.After(r.lastUsed) {
				r.lastUsed = linkedAt
			}
		}
	}

	sorted := make([]*cwdRank, 0, len(ranks))
	for _, r := range ranks {
		sorted = append(sorted, r)
	}
	sort.Slice(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		if a.count != b.count {
			return a.count > b.count
		}
		if !a.lastUsed.Equal(b.lastUsed) {
			return a.lastUsed.After(b.lastUsed)
		}
		return a.cwd < b.cwd
	})

	cwds := make([]string, len(sorted))
	for i, r := range sorted {
		cwds[i] = r.cwd
	}
	return cwds
}

// RenderPrompt expands a session-start template's placeholders against one task:
// {{id}} {{title}} {{note}} {{status}} {{links}}.
//
// Expansion runs once per line through a single strings.Replacer rather than one ReplaceAll call
// per placeholder: chaining ReplaceAll would let a placeholder's own substituted value (a title of
// "fix {{note}}", say) be expanded again by a later call. {{links}} is handled apart from the
// others because it does not substitute in place: it turns into one "- <url>" line per link, and a
// template line holding only {{links}} disappears entirely when the task has none, rather than
// leaving a blank line behind.
func RenderPrompt(tmpl string, task Task) string {
	replacer := strings.NewReplacer(
		"{{id}}", strconv.Itoa(task.ID),
		"{{title}}", task.Title,
		"{{note}}", task.Note,
		"{{status}}", task.Status,
	)

	lines := strings.Split(tmpl, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		hasLinks := strings.Contains(line, "{{links}}")
		rendered := replacer.Replace(line)
		if !hasLinks {
			out = append(out, rendered)
			continue
		}
		block := linksBlock(task.Links)
		if block == "" {
			continue
		}
		out = append(out, strings.Replace(rendered, "{{links}}", block, 1))
	}
	return strings.Join(out, "\n")
}

// linksBlock renders a task's links as one "- <url>" line per link.
func linksBlock(links []Link) string {
	if len(links) == 0 {
		return ""
	}
	rows := make([]string, 0, len(links))
	for _, link := range links {
		rows = append(rows, "- "+link.URL)
	}
	return strings.Join(rows, "\n")
}
