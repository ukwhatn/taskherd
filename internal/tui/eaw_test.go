package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ukwhatn/taskherd/internal/fetch"
	"github.com/ukwhatn/taskherd/internal/herdrc"
	"github.com/ukwhatn/taskherd/internal/model"
)

func TestUnsafeWidthRunes(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"ascii は安全", "PR owner/repo#1 open CI+", ""},
		{"日本語は全角として正しく数えられる", "設計する", ""},
		{"曖昧幅の記号を検出する", "●working", "●"},
		{"Neutral でも和文フォントが全角に描く記号を検出する", "✓done", "✓"},
		{"省略記号は曖昧幅", "続き…", "…"},
		{"全角ダッシュ類", "a–b—c×d", "–—×"},
		{"矢印", "↑↓←→", "↑↓←→"},
		{"重複は 1 回だけ報告する", "●●●", "●"},
		{"罫線は許容する", "╭──╮│╰╯┃", ""},
		{"宣言済みの Nerd Font グリフは許容する", nfOctGitPullRequest + nfOctCheck, ""},
		{"宣言していない私用領域は検出する", "\ue001", "\ue001"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := string(UnsafeWidthRunes(tc.in))
			if got != tc.want {
				t.Errorf("UnsafeWidthRunes(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// The whole point of the icon work: no screen, in any icon mode, may draw a character whose
// painted width depends on which font the terminal falls back to.
//
// Every fixture below is ASCII, so anything the scan reports came from the board itself rather
// than from the task data.
// stateFailStart is when the fixture's broken link started failing, fixed so the rendered age is
// stable across runs.
var stateFailStart = time.Date(2026, 8, 24, 16, 0, 0, 0, time.UTC)

func TestRenderedScreensUseOnlyWidthSafeCharacters(t *testing.T) {
	for _, mode := range []IconMode{IconNerd, IconASCII, IconNone} {
		t.Run(string(mode), func(t *testing.T) {
			for name, screen := range widthSafetyScreens(t, mode) {
				for _, r := range UnsafeWidthRunes(screen) {
					t.Errorf("%s: 幅が不安定な文字 U+%04X %q が描画されている\n%s", name, r, string(r), screen)
				}
			}
		})
	}
}

// widthSafetyScreens renders one of every screen the board can show, so the scan covers borders,
// badges, indicators, link rows, modals and the key help in one pass.
func widthSafetyScreens(t *testing.T, mode IconMode) map[string]string {
	t.Helper()

	prURL := "https://github.com/owner/repo/pull/123"
	issueURL := "https://github.com/owner/repo/issues/45"
	jiraURL := "https://x.atlassian.net/browse/ABC-123"
	otherURL := "https://example.com/doc"
	failURL := "https://github.com/owner/repo/pull/9"
	staleFailURL := "https://github.com/owner/repo/pull/10"

	tasks := []model.Task{
		{ID: 1, Title: "design the board", Status: "todo", Due: due("2026-08-20"),
			Links: []model.Link{
				{URL: prURL, Kind: model.LinkKindGitHubPR},
				{URL: issueURL, Kind: model.LinkKindGitHubIssue},
				{URL: jiraURL, Kind: model.LinkKindJira},
				{URL: otherURL, Kind: model.LinkKindOther},
			},
			Sessions: []model.SessionRef{{Agent: "claude", SessionID: "s-1", Cwd: "/tmp/work"}},
			Note:     "a note line"},
		{ID: 2, Title: "implement", Status: "working",
			Links: []model.Link{
				{URL: failURL, Kind: model.LinkKindGitHubPR},
				{URL: staleFailURL, Kind: model.LinkKindGitHubPR},
			}},
		{ID: 3, Title: "retired status", Status: "gone"},
	}

	settings := Settings{
		Columns:    model.DefaultColumns(),
		Classifier: testClassifier,
		Icons:      mode,
		Hyperlinks: true,
		CacheTTL:   5 * time.Minute,
	}
	h := newHarness(t, Deps{Tasks: newFakeStore(tasks...), Herdr: &fakeHerdr{}}, settings)

	h.board.links = map[string]fetch.LinkState{
		prURL:    ghState(prURL, fetch.GitHubData{State: "OPEN", Checks: "pending", ReviewDecision: "CHANGES_REQUESTED"}, model.LinkKindGitHubPR),
		issueURL: ghState(issueURL, fetch.GitHubData{State: "CLOSED"}, model.LinkKindGitHubIssue),
		jiraURL: {URL: jiraURL, Kind: model.LinkKindJira, Cached: true, Fetched: true, Stale: true,
			Age: 30 * time.Minute, Jira: &fetch.JiraData{StatusName: "In Progress", StatusCategory: "indeterminate"}},
		otherURL: {URL: otherURL, Kind: model.LinkKindOther},
		failURL:  {URL: failURL, Kind: model.LinkKindGitHubPR, Cached: true, Err: "not authenticated"},
		// A last-success value whose refresh keeps failing: the one row that draws the state tone,
		// the stale age and the alert mark all at once.
		staleFailURL: {URL: staleFailURL, Kind: model.LinkKindGitHubPR, Cached: true, Fetched: true,
			Stale: true, Age: 26 * time.Minute, Err: "gh: Could not resolve to a Repository",
			FailingSince: stateFailStart, FailingFor: 26 * time.Minute,
			GitHub: &fetch.GitHubData{State: "OPEN", Checks: "fail", ReviewDecision: "REVIEW_REQUIRED"}},
	}
	h.dispatch(snapshotUpdate(agent("pane-1", "s-1", herdrc.StateWorking)))

	screens := map[string]string{}
	// A narrow board degrades through every density, and each one is its own drawing code.
	for _, width := range []int{40, 60, 120} {
		h.board.width, h.board.height = width, 16
		h.board.mode = modeBoard
		screens[fmt.Sprintf("board/w%d", width)] = h.board.render()
	}

	h.board.width, h.board.height = 120, 30
	for name, open := range map[string]func(){
		"detail":  func() { h.key("enter") },
		"add":     func() { h.key("a") },
		"confirm": func() { h.key("delete") },
		"status":  func() { h.key("tab") },
	} {
		h.board.mode = modeBoard
		open()
		screens[name] = h.board.render()
	}

	h.board.mode = modeBoard
	h.key("enter")
	h.key("down")
	h.key("down")
	h.key("down")
	h.key("down")
	screens["detail/link-row"] = h.board.render()

	h.board.mode = modeBoard
	h.board.collapseTerminal = false
	screens["board/expanded"] = h.board.render()
	h.board.collapseTerminal = true

	// A column with more cards than it has room for draws its overflow indicators.
	crowded := boardWithMode(t, mode, 12)
	crowded.board.height = 18
	screens["board/overflow"] = crowded.board.render()

	sessionStart := boardWithSessionStart(t, mode)
	screens["session-start"] = sessionStart.board.render()

	fromStart := boardWithDetailFromStart(t, mode)
	screens["detail/from-start"] = fromStart.board.render()

	return screens
}

// boardWithDetailFromStart opens straight into a task's detail the way prefix+t does for a pane
// whose session is already linked to one.
func boardWithDetailFromStart(t *testing.T, mode IconMode) *harness {
	t.Helper()
	tasks := []model.Task{{ID: 1, Title: "task with a linked session", Status: "todo",
		Sessions: []model.SessionRef{{Agent: "claude", SessionID: "s-1", Cwd: "/tmp/work"}}}}
	return newHarness(t, Deps{Tasks: newFakeStore(tasks...), Herdr: &fakeHerdr{}},
		Settings{Columns: model.DefaultColumns(), Icons: mode, Classifier: testClassifier, DetailTaskID: 1})
}

// boardWithSessionStart opens the launch modal (g on a task with no linked session) over a board
// that also has one existing session, so the cwd candidate list is not empty.
func boardWithSessionStart(t *testing.T, mode IconMode) *harness {
	t.Helper()
	tasks := []model.Task{
		{ID: 1, Title: "existing session task", Status: "todo",
			Sessions: []model.SessionRef{{Agent: "claude", SessionID: "s-1", Cwd: "/tmp/existing", LinkedAt: "2026-08-20T10:00:00+09:00"}}},
		{ID: 2, Title: "task without a session yet", Status: "working"},
	}
	h := newHarness(t, Deps{Tasks: newFakeStore(tasks...), Herdr: &fakeHerdr{}, Launcher: &fakeLauncher{}},
		Settings{Columns: model.DefaultColumns(), Icons: mode, Classifier: testClassifier})
	h.key("right") // todo -> planning
	h.key("right") // planning -> working, selecting the task with no session
	h.key("g")
	h.board.sessionStart.cwdInput.SetValue("/typed/by/hand")
	h.board.sessionStart.prompt.SetValue("multi\nline prompt text")
	return h
}

func boardWithMode(t *testing.T, mode IconMode, n int) *harness {
	t.Helper()
	tasks := make([]model.Task, 0, n)
	for i := 1; i <= n; i++ {
		tasks = append(tasks, model.Task{ID: i, Title: fmt.Sprintf("task %d", i), Status: "todo", Due: due("2026-08-30")})
	}
	return newHarness(t, Deps{Tasks: newFakeStore(tasks...)},
		Settings{Columns: model.DefaultColumns(), Icons: mode, Classifier: testClassifier})
}

// The ambiguous table is generated, so a spot check that it did not land shifted by a codepoint is
// worth more than restating it.
func TestEastAsianAmbiguousTableBoundaries(t *testing.T) {
	inside := []rune{0x00A1, 0x2013, 0x2026, 0x25CF, 0x2500, 0x256D, 0x2592, 0xE000, 0xF8FF, 0xFFFD}
	outside := []rune{'a', 'あ', 0x2713, 0x25B8, 0x0020, 0x2E80, 0xFF0B}

	for _, r := range inside {
		if !isEastAsianAmbiguous(r) {
			t.Errorf("U+%04X が Ambiguous と判定されない", r)
		}
	}
	for _, r := range outside {
		if isEastAsianAmbiguous(r) {
			t.Errorf("U+%04X が誤って Ambiguous と判定されている", r)
		}
	}
	for i := 1; i < len(eastAsianAmbiguous); i++ {
		if eastAsianAmbiguous[i-1][1] >= eastAsianAmbiguous[i][0] {
			t.Fatalf("表が昇順でないか重複している: %+v / %+v", eastAsianAmbiguous[i-1], eastAsianAmbiguous[i])
		}
	}
}

// The board's own strings are checked by rendering, but the constants are worth naming: these are
// exactly the characters the v0.2 board drew, and reintroducing one is the regression to catch.
func TestRetiredSymbolsAreReported(t *testing.T) {
	for _, symbol := range strings.Split("✓ ✗ × ● ◌ ■ ▌ ▏ ↑ ↓ ▸ … – —", " ") {
		if len(UnsafeWidthRunes(symbol)) == 0 {
			t.Errorf("%q が安全と判定されている", symbol)
		}
	}
}
