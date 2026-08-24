package tui

import (
	"testing"
	"time"

	"github.com/ukwhatn/taskherd/internal/fetch"
	"github.com/ukwhatn/taskherd/internal/herdrc"
	"github.com/ukwhatn/taskherd/internal/model"
)

func sessionTask(ids ...string) model.Task {
	t := model.Task{ID: 1, Title: "t", Status: "todo"}
	for _, id := range ids {
		t.Sessions = append(t.Sessions, model.SessionRef{Agent: "claude", SessionID: id, Cwd: "/tmp"})
	}
	return t
}

func liveStates(pairs map[string]string) SessionStates {
	return SessionStates{Available: true, State: pairs, Pane: map[string]string{}, Agent: map[string]string{}}
}

func TestSessionBadgeEmptyWithoutSessions(t *testing.T) {
	badge := BuildSessionBadge(model.Task{ID: 1}, liveStates(nil))

	if badge.Text != "" {
		t.Errorf("Text = %q, want 空（報告するものがない）", badge.Text)
	}
}

// herdr being unreachable and a pane having disappeared are both "no live state", and the card
// says so the same way.
func TestSessionBadgeOfflineWhenHerdrUnavailable(t *testing.T) {
	badge := BuildSessionBadge(sessionTask("a"), UnavailableSessions(nil))

	if badge.Text != offlineBadge {
		t.Errorf("Text = %q, want %q", badge.Text, offlineBadge)
	}
	if badge.State != herdrc.StateOffline {
		t.Errorf("State = %q, want %q", badge.State, herdrc.StateOffline)
	}
}

func TestSessionBadgeUsesMostAttentionWorthyState(t *testing.T) {
	tests := []struct {
		name   string
		states map[string]string
		want   string
	}{
		{"blocked が working に勝つ", map[string]string{"a": herdrc.StateWorking, "b": herdrc.StateBlocked}, "■blocked×2"},
		{"working が done に勝つ", map[string]string{"a": herdrc.StateDone, "b": herdrc.StateWorking}, "●working×2"},
		{"done が idle に勝つ", map[string]string{"a": herdrc.StateIdle, "b": herdrc.StateDone}, "✓done×2"},
		{"idle だけ", map[string]string{"a": herdrc.StateIdle, "b": herdrc.StateIdle}, "◌idle×2"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			badge := BuildSessionBadge(sessionTask("a", "b"), liveStates(tc.states))
			if badge.Text != tc.want {
				t.Errorf("Text = %q, want %q", badge.Text, tc.want)
			}
		})
	}
}

// A session herdr does not know about has lost its pane, which is offline rather than unknown.
func TestSessionBadgeMissingSessionIsOffline(t *testing.T) {
	badge := BuildSessionBadge(sessionTask("gone"), liveStates(map[string]string{}))

	if badge.Text != offlineBadge {
		t.Errorf("Text = %q, want %q", badge.Text, offlineBadge)
	}
}

func TestSessionBadgeSingleSessionHasNoCount(t *testing.T) {
	badge := BuildSessionBadge(sessionTask("a"), liveStates(map[string]string{"a": herdrc.StateWorking}))

	if badge.Text != "●working" {
		t.Errorf("Text = %q, want ●working", badge.Text)
	}
}

func linkTask(links ...model.Link) model.Task {
	return model.Task{ID: 1, Title: "t", Status: "todo", Links: links}
}

func ghState(url string, data fetch.GitHubData, kind model.LinkKind) fetch.LinkState {
	return fetch.LinkState{URL: url, Kind: kind, Cached: true, Fetched: true, GitHub: &data}
}

func TestLinkBadgePullRequest(t *testing.T) {
	tests := []struct {
		name string
		data fetch.GitHubData
		want string
	}{
		{"open + green ci", fetch.GitHubData{State: "OPEN", Checks: "pass"}, "PR:open✓ci"},
		{"open + red ci", fetch.GitHubData{State: "OPEN", Checks: "fail"}, "PR:open✗ci"},
		{"open + 進行中 ci", fetch.GitHubData{State: "OPEN", Checks: "pending"}, "PR:open…ci"},
		{"ci 未設定", fetch.GitHubData{State: "OPEN", Checks: "none"}, "PR:open"},
		{"draft", fetch.GitHubData{State: "OPEN", IsDraft: true, Checks: "none"}, "PR:draft"},
		{"merged", fetch.GitHubData{State: "MERGED", Checks: "pass"}, "PR:merged✓ci"},
		{"approved", fetch.GitHubData{State: "OPEN", ReviewDecision: "APPROVED", Checks: "none"}, "PR:open✓rv"},
		{"変更要求", fetch.GitHubData{State: "OPEN", ReviewDecision: "CHANGES_REQUESTED", Checks: "none"}, "PR:open✗rv"},
		{"レビュー待ちは表示しない", fetch.GitHubData{State: "OPEN", ReviewDecision: "REVIEW_REQUIRED", Checks: "none"}, "PR:open"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			url := "https://github.com/o/r/pull/1"
			badges := BuildLinkBadges(
				linkTask(model.Link{URL: url, Kind: model.LinkKindGitHubPR}),
				map[string]fetch.LinkState{url: ghState(url, tc.data, model.LinkKindGitHubPR)},
			)
			if len(badges) != 1 {
				t.Fatalf("badges = %+v, want 1 件", badges)
			}
			if badges[0].Text != tc.want {
				t.Errorf("Text = %q, want %q", badges[0].Text, tc.want)
			}
		})
	}
}

// Several links of one kind fold into one badge so a card cannot grow a row of PR badges;
// the one that needs attention is the one that shows.
func TestLinkBadgeFoldsSameKindOntoWorst(t *testing.T) {
	green := "https://github.com/o/r/pull/1"
	red := "https://github.com/o/r/pull/2"

	badges := BuildLinkBadges(
		linkTask(
			model.Link{URL: green, Kind: model.LinkKindGitHubPR},
			model.Link{URL: red, Kind: model.LinkKindGitHubPR},
		),
		map[string]fetch.LinkState{
			green: ghState(green, fetch.GitHubData{State: "OPEN", Checks: "pass"}, model.LinkKindGitHubPR),
			red:   ghState(red, fetch.GitHubData{State: "OPEN", Checks: "fail"}, model.LinkKindGitHubPR),
		},
	)

	if len(badges) != 1 {
		t.Fatalf("badges = %+v, want 1 件に集約", badges)
	}
	if badges[0].Text != "PR×2:open✗ci" {
		t.Errorf("Text = %q, want PR×2:open✗ci", badges[0].Text)
	}
	if !badges[0].Attention {
		t.Error("Attention = false, want true（ci 失敗）")
	}
}

func TestLinkBadgeOrderIsFixedByKind(t *testing.T) {
	jira := "https://x.atlassian.net/browse/A-1"
	issue := "https://github.com/o/r/issues/9"
	pr := "https://github.com/o/r/pull/1"

	// Links deliberately added in the reverse of the display order.
	badges := BuildLinkBadges(
		linkTask(
			model.Link{URL: jira, Kind: model.LinkKindJira},
			model.Link{URL: issue, Kind: model.LinkKindGitHubIssue},
			model.Link{URL: pr, Kind: model.LinkKindGitHubPR},
		),
		map[string]fetch.LinkState{
			jira:  {URL: jira, Kind: model.LinkKindJira, Cached: true, Fetched: true, Jira: &fetch.JiraData{StatusName: "In Progress", StatusCategory: "indeterminate"}},
			issue: ghState(issue, fetch.GitHubData{State: "OPEN"}, model.LinkKindGitHubIssue),
			pr:    ghState(pr, fetch.GitHubData{State: "OPEN", Checks: "none"}, model.LinkKindGitHubPR),
		},
	)

	want := []string{"PR:open", "Issue:open", "Jira:In Progress"}
	if len(badges) != len(want) {
		t.Fatalf("badges = %+v, want %d 件", badges, len(want))
	}
	for i := range want {
		if badges[i].Text != want[i] {
			t.Errorf("badges[%d] = %q, want %q", i, badges[i].Text, want[i])
		}
	}
}

func TestLinkBadgeStaleCarriesAge(t *testing.T) {
	url := "https://github.com/o/r/pull/1"
	state := ghState(url, fetch.GitHubData{State: "OPEN", Checks: "pass"}, model.LinkKindGitHubPR)
	state.Stale = true
	state.Age = 12 * time.Minute

	badges := BuildLinkBadges(
		linkTask(model.Link{URL: url, Kind: model.LinkKindGitHubPR}),
		map[string]fetch.LinkState{url: state},
	)

	if !badges[0].Stale || badges[0].Age != 12*time.Minute {
		t.Errorf("badge = %+v, want Stale=true Age=12m", badges[0])
	}
}

// A link the first cycle has not reached yet reads as pending; one that keeps failing reads as
// an error the user should look at.
func TestLinkBadgeUnfetchedStates(t *testing.T) {
	url := "https://github.com/o/r/pull/1"
	link := model.Link{URL: url, Kind: model.LinkKindGitHubPR}

	pending := BuildLinkBadges(linkTask(link), map[string]fetch.LinkState{
		url: {URL: url, Kind: model.LinkKindGitHubPR},
	})
	if pending[0].Text != "PR:…" || pending[0].Attention {
		t.Errorf("pending = %+v, want PR:… で Attention=false", pending[0])
	}

	failing := BuildLinkBadges(linkTask(link), map[string]fetch.LinkState{
		url: {URL: url, Kind: model.LinkKindGitHubPR, Cached: true, Err: "認証されていない"},
	})
	if failing[0].Text != "PR:!" || !failing[0].Attention {
		t.Errorf("failing = %+v, want PR:! で Attention=true", failing[0])
	}
}

// A missing entry is the same situation as an unfetched one: the board must not drop the badge,
// or a freshly added link would look like no link at all.
func TestLinkBadgeMissingStateIsPending(t *testing.T) {
	url := "https://github.com/o/r/pull/1"

	badges := BuildLinkBadges(linkTask(model.Link{URL: url, Kind: model.LinkKindGitHubPR}), nil)

	if len(badges) != 1 || badges[0].Text != "PR:…" {
		t.Errorf("badges = %+v, want PR:…", badges)
	}
}

func TestLinkBadgeSkipsOtherKind(t *testing.T) {
	url := "https://example.com/x"

	badges := BuildLinkBadges(
		linkTask(model.Link{URL: url, Kind: model.LinkKindOther}),
		map[string]fetch.LinkState{url: {URL: url, Kind: model.LinkKindOther}},
	)

	if len(badges) != 0 {
		t.Errorf("badges = %+v, want 0 件（other はライブ取得対象外）", badges)
	}
}

func TestLinkBadgeJiraPicksActiveOverDone(t *testing.T) {
	done := "https://x.atlassian.net/browse/A-1"
	active := "https://x.atlassian.net/browse/A-2"

	badges := BuildLinkBadges(
		linkTask(
			model.Link{URL: done, Kind: model.LinkKindJira},
			model.Link{URL: active, Kind: model.LinkKindJira},
		),
		map[string]fetch.LinkState{
			done:   {URL: done, Kind: model.LinkKindJira, Cached: true, Fetched: true, Jira: &fetch.JiraData{StatusName: "Done", StatusCategory: "done"}},
			active: {URL: active, Kind: model.LinkKindJira, Cached: true, Fetched: true, Jira: &fetch.JiraData{StatusName: "In Progress", StatusCategory: "indeterminate"}},
		},
	)

	if len(badges) != 1 || badges[0].Text != "Jira×2:In Progress" {
		t.Errorf("badges = %+v, want Jira×2:In Progress", badges)
	}
}

func TestFormatAge(t *testing.T) {
	tests := []struct {
		in   time.Duration
		want string
	}{
		{30 * time.Second, "now"},
		{90 * time.Second, "1m"},
		{2 * time.Hour, "2h"},
		{50 * time.Hour, "2d"},
	}
	for _, tc := range tests {
		if got := FormatAge(tc.in); got != tc.want {
			t.Errorf("FormatAge(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
