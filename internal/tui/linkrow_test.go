package tui

import (
	"fmt"
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
		{"open + green ci", fetch.GitHubData{State: "OPEN", Checks: "pass"}, "PR owner/repo#1 open CI+", SegGood},
		{"open + red ci", fetch.GitHubData{State: "OPEN", Checks: "fail"}, "PR owner/repo#1 open CI!", SegGood},
		{"open + 進行中 ci", fetch.GitHubData{State: "OPEN", Checks: "pending"}, "PR owner/repo#1 open CI*", SegGood},
		{"ci 未設定", fetch.GitHubData{State: "OPEN", Checks: "none"}, "PR owner/repo#1 open", SegGood},
		{"draft", fetch.GitHubData{State: "OPEN", IsDraft: true, Checks: "none"}, "PR owner/repo#1 draft", SegMuted},
		{"merged", fetch.GitHubData{State: "MERGED", Checks: "pass"}, "PR owner/repo#1 merged CI+", SegDone},
		{"closed", fetch.GitHubData{State: "CLOSED", Checks: "none"}, "PR owner/repo#1 closed", SegAlert},
		{"approved", fetch.GitHubData{State: "OPEN", ReviewDecision: "APPROVED", Checks: "none"}, "PR owner/repo#1 open rv+", SegGood},
		{"変更要求", fetch.GitHubData{State: "OPEN", ReviewDecision: "CHANGES_REQUESTED", Checks: "none"}, "PR owner/repo#1 open rv!", SegGood},
		{"レビュー待ちも表示する", fetch.GitHubData{State: "OPEN", ReviewDecision: "REVIEW_REQUIRED", Checks: "none"}, "PR owner/repo#1 open rv*", SegGood},
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
	if last.Text != "12m" || last.Kind != SegDim {
		t.Errorf("末尾 = %+v, want 12m / SegDim", last)
	}
	// Only the age is dimmed. The state keeps its own tone, which is the whole point: a dim row
	// made a passing build and a failing one look the same.
	if rows[0].Icon.Kind != SegGood {
		t.Errorf("Icon.Kind = %v, want SegGood（stale でも状態色を保つ）", rows[0].Icon.Kind)
	}
	if rows[0].Status[0].Kind == SegDim {
		t.Errorf("状態セグメントが dim になっている: %+v", rows[0].Status[0])
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
	if pending[0].Status[0].Text != "未取得" || pending[0].Icon.Kind != SegMuted {
		t.Errorf("pending = %+v, want 未取得 / SegMuted", pending[0])
	}

	failing := BuildLinkRows(linkTask(link), map[string]fetch.LinkState{
		url: {URL: url, Kind: model.LinkKindGitHubPR, Cached: true, Err: "認証されていない"},
	}, testStyle(testIcons))
	if failing[0].Status[0].Text != "! 失敗" || failing[0].Icon.Kind != SegAlert {
		t.Errorf("failing = %+v, want 失敗 / SegAlert", failing[0])
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
	if want := fmt.Sprintf(ja.Board.MoreLinks, 2); !summary.Overflow || summary.Refs[0] != want {
		t.Errorf("summary = %+v, want %q", summary, want)
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

// The none mode spells its marks out as words, so "CI" and the mark need a separator that the
// glyph modes would only spend a cell on.
func TestLinkRowNoneModeSeparatesLabelFromMark(t *testing.T) {
	url := "https://github.com/o/r/pull/1"
	states := map[string]fetch.LinkState{
		url: ghState(url, fetch.GitHubData{State: "OPEN", Checks: "pass", ReviewDecision: "APPROVED"}, model.LinkKindGitHubPR),
	}

	none := BuildLinkRows(linkTask(model.Link{URL: url, Kind: model.LinkKindGitHubPR}), states, testStyle(Icons(IconNone)))
	if got := rowText(none[0]); got != "PR o/r#1 open CI ok rv ok" {
		t.Errorf("none = %q, want %q", got, "PR o/r#1 open CI ok rv ok")
	}

	ascii := BuildLinkRows(linkTask(model.Link{URL: url, Kind: model.LinkKindGitHubPR}), states, testStyle(testIcons))
	if got := rowText(ascii[0]); got != "PR o/r#1 open CI+ rv+" {
		t.Errorf("ascii = %q, want %q", got, "PR o/r#1 open CI+ rv+")
	}
}

// A value that is still on the card while every refresh fails is the case the board used to hide:
// the state stayed coloured as if it were current and nothing said the fetch was broken.
func TestLinkRowFailingRefreshKeepsStateAndSaysSoInRed(t *testing.T) {
	url := "https://github.com/o/r/pull/1"
	state := ghState(url, fetch.GitHubData{State: "OPEN", Checks: "pass"}, model.LinkKindGitHubPR)
	state.Stale = true
	state.Age = 26 * time.Minute
	state.Err = "gh: Could not resolve to a Repository"
	state.FailingSince = time.Now().Add(-26 * time.Minute)
	state.FailingFor = 26 * time.Minute

	rows := BuildLinkRows(
		linkTask(model.Link{URL: url, Kind: model.LinkKindGitHubPR}),
		map[string]fetch.LinkState{url: state},
		testStyle(testIcons),
	)

	if got := rowText(rows[0]); got != "PR o/r#1 open CI+ 26m ! 失敗 26m" {
		t.Errorf("row = %q, want 状態 + 経過時間 + 失敗マーク", got)
	}
	last := rows[0].Status[len(rows[0].Status)-1]
	if last.Kind != SegAlert {
		t.Errorf("失敗マークの Kind = %v, want SegAlert", last.Kind)
	}
	if rows[0].Status[0].Kind != SegGood {
		t.Errorf("状態セグメントの Kind = %v, want SegGood（失敗中でも状態色は保つ）", rows[0].Status[0].Kind)
	}
}

// The failure mark is drawn even when nothing timed the run: "failing" is the part that must not be
// silent, and how long it has been failing is what the cache may not know.
func TestLinkRowFailureWithoutRecordedStartStillShows(t *testing.T) {
	url := "https://github.com/o/r/pull/1"
	state := ghState(url, fetch.GitHubData{State: "MERGED"}, model.LinkKindGitHubPR)
	state.Err = "gh: Not Found"

	rows := BuildLinkRows(
		linkTask(model.Link{URL: url, Kind: model.LinkKindGitHubPR}),
		map[string]fetch.LinkState{url: state},
		testStyle(testIcons),
	)

	last := rows[0].Status[len(rows[0].Status)-1]
	if last.Text != "! 失敗" || last.Kind != SegAlert {
		t.Errorf("末尾 = %+v, want \"! 失敗\" / SegAlert", last)
	}
	if rows[0].Icon.Kind != SegDone {
		t.Errorf("Icon.Kind = %v, want SegDone（merged の状態色を保つ）", rows[0].Icon.Kind)
	}
}

// Each icon mode has to be able to say "failing" with what it has: the nerd glyph alone, and a word
// in the modes that have no such glyph.
func TestFailureMarkPerIconMode(t *testing.T) {
	tests := []struct {
		mode    IconMode
		age     string
		want    string
		wantEnd string
	}{
		{IconNerd, "26m", nfOctAlert + " 26m", "26m"},
		{IconNerd, "", nfOctAlert, nfOctAlert},
		{IconASCII, "26m", "! 失敗 26m", "26m"},
		{IconASCII, "", "! 失敗", "失敗"},
		{IconNone, "26m", "失敗 26m", "26m"},
		{IconNone, "", "失敗", "失敗"},
	}
	for _, tc := range tests {
		t.Run(string(tc.mode)+"/"+tc.age, func(t *testing.T) {
			got := Icons(tc.mode).failureMark(ja.Common.Failed, tc.age)
			if got != tc.want {
				t.Errorf("failureMark(%q) = %q, want %q", tc.age, got, tc.want)
			}
			if strings.HasSuffix(got, " ") || strings.HasPrefix(got, " ") {
				t.Errorf("failureMark(%q) = %q に余分な空白がある", tc.age, got)
			}
		})
	}
}

// Jira and issue tones changed with the colour table, and the row is where they are actually read.
func TestLinkRowIssueAndJiraTones(t *testing.T) {
	issueURL := "https://github.com/o/r/issues/5"
	jiraURL := "https://x.atlassian.net/browse/ABC-1"

	closed := BuildLinkRows(
		linkTask(model.Link{URL: issueURL, Kind: model.LinkKindGitHubIssue}),
		map[string]fetch.LinkState{issueURL: ghState(issueURL, fetch.GitHubData{State: "CLOSED"}, model.LinkKindGitHubIssue)},
		testStyle(testIcons),
	)
	if closed[0].Status[0].Kind != SegDone || closed[0].Icon.Kind != SegDone {
		t.Errorf("closed issue = %+v, want SegDone（紫）", closed[0])
	}

	done := BuildLinkRows(
		linkTask(model.Link{URL: jiraURL, Kind: model.LinkKindJira}),
		map[string]fetch.LinkState{jiraURL: {
			URL: jiraURL, Kind: model.LinkKindJira, Cached: true, Fetched: true,
			Jira: &fetch.JiraData{StatusName: "Done", StatusCategory: "done"},
		}},
		testStyle(testIcons),
	)
	if done[0].Status[0].Kind != SegGood || done[0].Icon.Kind != SegGood {
		t.Errorf("Jira done = %+v, want SegGood（緑）", done[0])
	}
}
