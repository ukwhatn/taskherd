package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/ukwhatn/taskherd/internal/fetch"
	"github.com/ukwhatn/taskherd/internal/model"
)

var testClassifier = model.URLClassifier{
	GHESHosts: []string{"github.example.com"},
	JiraSite:  "x.atlassian.net",
}

func testStyle(icons IconSet) CardStyle {
	return CardStyle{Icons: icons, Classifier: testClassifier}
}

func linkTask(links ...model.Link) model.Task {
	return model.Task{ID: 1, Title: "t", Status: "todo", Links: links}
}

func ghState(url string, data fetch.GitHubData, kind model.LinkKind) fetch.LinkState {
	return fetch.LinkState{URL: url, Kind: kind, Cached: true, Fetched: true, GitHub: &data}
}

// rowText flattens a row the way the renderer joins it, so a test can assert on one string.
func rowText(row LinkRow) string {
	parts := []string{}
	if row.Icon.Text != "" {
		parts = append(parts, row.Icon.Text)
	}
	parts = append(parts, row.Refs[0])
	for _, segment := range row.Status {
		parts = append(parts, segment.Text)
	}
	return strings.Join(parts, " ")
}

// The reference is parsed out of the URL, so a card names its PR before anything has been fetched
// and keeps naming it while the network is down.
func TestLinkRowNamesPRWithoutCache(t *testing.T) {
	url := "https://github.com/owner/repo/pull/123"

	rows := BuildLinkRows(linkTask(model.Link{URL: url, Kind: model.LinkKindGitHubPR}), nil, testStyle(testIcons))

	if len(rows) != 1 {
		t.Fatalf("rows = %+v, want 1 件", rows)
	}
	want := []string{"owner/repo#123", "repo#123", "#123"}
	if len(rows[0].Refs) != len(want) {
		t.Fatalf("Refs = %q, want %q", rows[0].Refs, want)
	}
	for i := range want {
		if rows[0].Refs[i] != want[i] {
			t.Errorf("Refs[%d] = %q, want %q", i, rows[0].Refs[i], want[i])
		}
	}
	if rows[0].URL != url {
		t.Errorf("URL = %q, want %q", rows[0].URL, url)
	}
}

func TestLinkRowPullRequestStates(t *testing.T) {
	tests := []struct {
		name  string
		data  fetch.GitHubData
		want  string
		phase SegmentKind
	}{
		{"open + green ci", fetch.GitHubData{State: "OPEN", Checks: "pass"}, "PR owner/repo#1 open CI+", SegLinkOpen},
		{"open + red ci", fetch.GitHubData{State: "OPEN", Checks: "fail"}, "PR owner/repo#1 open CI!", SegLinkOpen},
		{"open + 進行中 ci", fetch.GitHubData{State: "OPEN", Checks: "pending"}, "PR owner/repo#1 open CI*", SegLinkOpen},
		{"ci 未設定", fetch.GitHubData{State: "OPEN", Checks: "none"}, "PR owner/repo#1 open", SegLinkOpen},
		{"draft", fetch.GitHubData{State: "OPEN", IsDraft: true, Checks: "none"}, "PR owner/repo#1 draft", SegLinkDraft},
		{"merged", fetch.GitHubData{State: "MERGED", Checks: "pass"}, "PR owner/repo#1 merged CI+", SegLinkMerged},
		{"closed", fetch.GitHubData{State: "CLOSED", Checks: "none"}, "PR owner/repo#1 closed", SegLinkClosed},
		{"approved", fetch.GitHubData{State: "OPEN", ReviewDecision: "APPROVED", Checks: "none"}, "PR owner/repo#1 open rv+", SegLinkOpen},
		{"変更要求", fetch.GitHubData{State: "OPEN", ReviewDecision: "CHANGES_REQUESTED", Checks: "none"}, "PR owner/repo#1 open rv!", SegLinkOpen},
		{"レビュー待ちは表示しない", fetch.GitHubData{State: "OPEN", ReviewDecision: "REVIEW_REQUIRED", Checks: "none"}, "PR owner/repo#1 open", SegLinkOpen},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			url := "https://github.com/owner/repo/pull/1"
			rows := BuildLinkRows(
				linkTask(model.Link{URL: url, Kind: model.LinkKindGitHubPR}),
				map[string]fetch.LinkState{url: ghState(url, tc.data, model.LinkKindGitHubPR)},
				testStyle(testIcons),
			)
			if got := rowText(rows[0]); got != tc.want {
				t.Errorf("row = %q, want %q", got, tc.want)
			}
			if rows[0].Icon.Kind != tc.phase {
				t.Errorf("Icon.Kind = %v, want %v", rows[0].Icon.Kind, tc.phase)
			}
		})
	}
}

// The nerd set has a glyph per PR state, so the row does not repeat the state as a word; the
// ascii set has one glyph for every PR, so it must.
func TestLinkRowNerdIconCarriesStateInsteadOfWord(t *testing.T) {
	url := "https://github.com/owner/repo/pull/1"
	rows := BuildLinkRows(
		linkTask(model.Link{URL: url, Kind: model.LinkKindGitHubPR}),
		map[string]fetch.LinkState{url: ghState(url, fetch.GitHubData{State: "MERGED", Checks: "none"}, model.LinkKindGitHubPR)},
		testStyle(Icons(IconNerd)),
	)

	if len(rows[0].Status) != 0 {
		t.Errorf("Status = %+v, want 空（アイコンが merged を表す）", rows[0].Status)
	}
	if rows[0].Icon.Text != nfOctGitMerge {
		t.Errorf("Icon = %q, want nf-oct-git_merge", rows[0].Icon.Text)
	}
}

func TestLinkRowIssueAndJira(t *testing.T) {
	issue := "https://github.com/owner/repo/issues/45"
	jira := "https://x.atlassian.net/browse/ABC-123"

	rows := BuildLinkRows(
		linkTask(
			model.Link{URL: issue, Kind: model.LinkKindGitHubIssue},
			model.Link{URL: jira, Kind: model.LinkKindJira},
		),
		map[string]fetch.LinkState{
			issue: ghState(issue, fetch.GitHubData{State: "OPEN"}, model.LinkKindGitHubIssue),
			jira: {URL: jira, Kind: model.LinkKindJira, Cached: true, Fetched: true,
				Jira: &fetch.JiraData{StatusName: "In Progress", StatusCategory: "indeterminate"}},
		},
		testStyle(testIcons),
	)

	want := []string{"IS owner/repo#45 open", "JR ABC-123 In Progress"}
	for i := range want {
		if got := rowText(rows[i]); got != want[i] {
			t.Errorf("rows[%d] = %q, want %q", i, got, want[i])
		}
	}
}

// An "other" link has no number and nothing to fetch, so it is named by its host.
func TestLinkRowOtherKindShowsHost(t *testing.T) {
	url := "https://example.com/some/page"

	rows := BuildLinkRows(
		linkTask(model.Link{URL: url, Kind: model.LinkKindOther}),
		map[string]fetch.LinkState{url: {URL: url, Kind: model.LinkKindOther}},
		testStyle(testIcons),
	)

	if got := rowText(rows[0]); got != "LN example.com" {
		t.Errorf("row = %q, want %q", got, "LN example.com")
	}
}

// Rows keep the order the links were attached in, which is the order the detail modal lists them.
func TestLinkRowKeepsAttachOrder(t *testing.T) {
	jira := "https://x.atlassian.net/browse/A-1"
	pr := "https://github.com/o/r/pull/1"

	rows := BuildLinkRows(
		linkTask(
			model.Link{URL: jira, Kind: model.LinkKindJira},
			model.Link{URL: pr, Kind: model.LinkKindGitHubPR},
		),
		nil,
		testStyle(testIcons),
	)

	if rows[0].URL != jira || rows[1].URL != pr {
		t.Errorf("順序 = %q, %q, want %q, %q", rows[0].URL, rows[1].URL, jira, pr)
	}
}

func TestLinkRowStaleCarriesAge(t *testing.T) {
	url := "https://github.com/o/r/pull/1"
	state := ghState(url, fetch.GitHubData{State: "OPEN", Checks: "pass"}, model.LinkKindGitHubPR)
	state.Stale = true
	state.Age = 12 * time.Minute

	rows := BuildLinkRows(
		linkTask(model.Link{URL: url, Kind: model.LinkKindGitHubPR}),
		map[string]fetch.LinkState{url: state},
		testStyle(testIcons),
	)

	last := rows[0].Status[len(rows[0].Status)-1]
	if last.Text != "12m" {
		t.Errorf("末尾 = %q, want 12m", last.Text)
	}
	for i, segment := range rows[0].Status {
		if segment.Kind != SegLinkStale {
			t.Errorf("Status[%d].Kind = %v, want SegLinkStale", i, segment.Kind)
		}
	}
	if rows[0].RefKind != SegLinkStale || rows[0].Icon.Kind != SegLinkStale {
		t.Errorf("stale 行が dim になっていない: %+v", rows[0])
	}
}

// A link the first cycle has not reached yet reads as unfetched; one that keeps failing reads as
// something the user should look at.
func TestLinkRowUnfetchedStates(t *testing.T) {
	url := "https://github.com/o/r/pull/1"
	link := model.Link{URL: url, Kind: model.LinkKindGitHubPR}

	pending := BuildLinkRows(linkTask(link), map[string]fetch.LinkState{
		url: {URL: url, Kind: model.LinkKindGitHubPR},
	}, testStyle(testIcons))
	if pending[0].Status[0].Text != "未取得" || pending[0].Icon.Kind != SegLinkUnfetched {
		t.Errorf("pending = %+v, want 未取得 / SegLinkUnfetched", pending[0])
	}

	failing := BuildLinkRows(linkTask(link), map[string]fetch.LinkState{
		url: {URL: url, Kind: model.LinkKindGitHubPR, Cached: true, Err: "認証されていない"},
	}, testStyle(testIcons))
	if failing[0].Status[0].Text != "取得失敗" || failing[0].Icon.Kind != SegLinkAttention {
		t.Errorf("failing = %+v, want 取得失敗 / SegLinkAttention", failing[0])
	}
}

// A missing entry is the same situation as an unfetched one: the row must still appear, or a
// freshly added link would look like no link at all.
func TestLinkRowMissingStateStillDrawsRow(t *testing.T) {
	url := "https://github.com/o/r/pull/1"

	rows := BuildLinkRows(linkTask(model.Link{URL: url, Kind: model.LinkKindGitHubPR}), nil, testStyle(testIcons))

	if len(rows) != 1 || rows[0].Status[0].Text != "未取得" {
		t.Errorf("rows = %+v, want 未取得 の 1 行", rows)
	}
}

// A card is a summary: past the cap the remaining links fold into one row rather than pushing the
// card past the height of its column.
func TestLinkRowCapsAtMaxWithSummaryRow(t *testing.T) {
	links := []model.Link{}
	for i := 1; i <= maxCardLinkRows+2; i++ {
		links = append(links, model.Link{URL: "https://github.com/o/r/pull/" + string(rune('0'+i)), Kind: model.LinkKindGitHubPR})
	}

	rows := BuildLinkRows(linkTask(links...), nil, testStyle(testIcons))

	if len(rows) != maxCardLinkRows+1 {
		t.Fatalf("rows = %d 行, want %d 行", len(rows), maxCardLinkRows+1)
	}
	summary := rows[len(rows)-1]
	if !summary.Overflow || summary.Refs[0] != "他 2 件" {
		t.Errorf("summary = %+v, want 他 2 件", summary)
	}
	if summary.URL != "" {
		t.Errorf("summary.URL = %q, want 空（開く先がない）", summary.URL)
	}
}

func TestLinkRowGHESHostIsParsed(t *testing.T) {
	url := "https://github.example.com/team/svc/pull/7"

	rows := BuildLinkRows(linkTask(model.Link{URL: url, Kind: model.LinkKindGitHubPR}), nil, testStyle(testIcons))

	if rows[0].Refs[0] != "team/svc#7" {
		t.Errorf("Refs[0] = %q, want team/svc#7", rows[0].Refs[0])
	}
}
