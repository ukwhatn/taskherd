package tui

import (
	"image/color"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/ukwhatn/taskherd/internal/fetch"
	"github.com/ukwhatn/taskherd/internal/herdrc"
	"github.com/ukwhatn/taskherd/internal/model"
)

// toneNames label a tone in a failure message. Comparing "SegGood" against "SegDone" says more
// about what broke than comparing 4 against 7 does.
var toneNames = map[SegmentKind]string{
	SegPlain:   "SegPlain",
	SegRef:     "SegRef",
	SegMuted:   "SegMuted",
	SegDim:     "SegDim",
	SegGood:    "SegGood",
	SegCaution: "SegCaution",
	SegAlert:   "SegAlert",
	SegDone:    "SegDone",
}

func toneName(kind SegmentKind) string {
	if name, ok := toneNames[kind]; ok {
		return name
	}
	return "不明なトーン"
}

// The tone of every state the implementation master's colour table names, in one place. This is the
// table the board's colours are, so a change to any of them has to be a change here first.
func TestStateTones(t *testing.T) {
	tests := []struct {
		name string
		got  SegmentKind
		want SegmentKind
	}{
		{"PR open は緑", prStateTone("OPEN", false), SegGood},
		{"PR draft は灰", prStateTone("OPEN", true), SegMuted},
		{"PR merged は紫", prStateTone("MERGED", false), SegDone},
		{"PR closed は赤", prStateTone("CLOSED", false), SegAlert},
		{"merged は draft より優先する", prStateTone("MERGED", true), SegDone},

		{"CI pass は緑", checkTone(fetch.CheckPass), SegGood},
		{"CI fail は赤", checkTone(fetch.CheckFail), SegAlert},
		{"CI pending は黄", checkTone(fetch.CheckPending), SegCaution},
		{"CI none は灰", checkTone(fetch.CheckNone), SegMuted},

		{"Issue open は緑", issueTone("OPEN"), SegGood},
		{"Issue closed は紫", issueTone("CLOSED"), SegDone},

		{"Jira new は灰", jiraTone("new"), SegMuted},
		{"Jira indeterminate は黄", jiraTone("indeterminate"), SegCaution},
		{"Jira done は緑", jiraTone("done"), SegGood},
		{"Jira の未知のカテゴリは灰", jiraTone(""), SegMuted},

		{"session blocked は赤", sessionTone(herdrc.StateBlocked), SegAlert},
		{"session working は緑", sessionTone(herdrc.StateWorking), SegGood},
		{"session done は黄", sessionTone(herdrc.StateDone), SegCaution},
		{"session idle は灰", sessionTone(herdrc.StateIdle), SegMuted},
		{"session offline は dim", sessionTone(herdrc.StateOffline), SegDim},
		{"session unknown は dim", sessionTone(herdrc.StateUnknown), SegDim},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("トーン = %s, want %s", toneName(tc.got), toneName(tc.want))
			}
		})
	}
}

func TestReviewTone(t *testing.T) {
	tests := []struct {
		decision string
		want     SegmentKind
		wantDraw bool
	}{
		{"APPROVED", SegGood, true},
		{"CHANGES_REQUESTED", SegAlert, true},
		{"REVIEW_REQUIRED", SegCaution, true},
		{"", SegPlain, false},
		{"SOMETHING_NEW", SegPlain, false},
		{"approved", SegGood, true},
	}
	for _, tc := range tests {
		t.Run(tc.decision, func(t *testing.T) {
			got, draw := reviewTone(tc.decision)
			if got != tc.want || draw != tc.wantDraw {
				t.Errorf("reviewTone(%q) = (%s, %v), want (%s, %v)",
					tc.decision, toneName(got), draw, toneName(tc.want), tc.wantDraw)
			}
		})
	}
}

// The due date's tone is a function of the calendar day, not of the hours between two timestamps:
// a task due tomorrow is due tomorrow whether it is now morning or midnight.
func TestDueTone(t *testing.T) {
	now := time.Date(2026, 8, 24, 23, 30, 0, 0, time.UTC)
	tests := []struct {
		due  string
		want SegmentKind
	}{
		{"2026-08-01", SegAlert},
		{"2026-08-23", SegAlert},
		{"2026-08-24", SegCaution},
		{"2026-08-25", SegCaution},
		{"2026-08-26", SegPlain},
		{"2027-01-01", SegPlain},
		{"not-a-date", SegPlain},
	}
	for _, tc := range tests {
		t.Run(tc.due, func(t *testing.T) {
			if got := dueTone(model.Date(tc.due), now); got != tc.want {
				t.Errorf("dueTone(%q) = %s, want %s", tc.due, toneName(got), toneName(tc.want))
			}
		})
	}
}

// A link that is failing to refresh keeps the tone of the state it last knew: the failure gets a
// mark of its own, and turning the whole row red would throw away the state the card is for.
func TestLinkToneKeepsLastKnownStateWhileFailing(t *testing.T) {
	url := "https://github.com/o/r/pull/1"
	state := ghState(url, fetch.GitHubData{State: "MERGED"}, model.LinkKindGitHubPR)
	state.Err = "gh: Not Found"

	if got := linkTone(state); got != SegDone {
		t.Errorf("linkTone = %s, want SegDone（最終成功値の状態色を保つ）", toneName(got))
	}
}

func TestLinkToneUnfetched(t *testing.T) {
	url := "https://github.com/o/r/pull/1"
	tests := []struct {
		name  string
		state fetch.LinkState
		want  SegmentKind
	}{
		{"未取得", fetch.LinkState{URL: url, Kind: model.LinkKindGitHubPR}, SegMuted},
		{"一度も成功していない", fetch.LinkState{URL: url, Kind: model.LinkKindGitHubPR, Cached: true, Err: "認証されていない"}, SegAlert},
		{"other はリンク色", fetch.LinkState{URL: url, Kind: model.LinkKindOther}, SegRef},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := linkTone(tc.state); got != tc.want {
				t.Errorf("linkTone = %s, want %s", toneName(got), toneName(tc.want))
			}
		})
	}
}

// The palette is ANSI 16 only, so the board follows the terminal's theme instead of imposing a
// background of its own. A hex colour would look right on the machine it was picked on and wrong
// on the next one.
func TestTonePaletteIsANSIOnly(t *testing.T) {
	// The 16 ANSI colors, which are the only ones the board may draw from.
	ansi16 := []color.Color{
		lipgloss.Black, lipgloss.Red, lipgloss.Green, lipgloss.Yellow,
		lipgloss.Blue, lipgloss.Magenta, lipgloss.Cyan, lipgloss.White,
		lipgloss.BrightBlack, lipgloss.BrightRed, lipgloss.BrightGreen, lipgloss.BrightYellow,
		lipgloss.BrightBlue, lipgloss.BrightMagenta, lipgloss.BrightCyan, lipgloss.BrightWhite,
	}
	isANSI := func(c color.Color) bool {
		for _, candidate := range ansi16 {
			if c == candidate {
				return true
			}
		}
		return false
	}

	want := map[SegmentKind]color.Color{
		SegRef:     lipgloss.Blue,
		SegMuted:   lipgloss.BrightBlack,
		SegDim:     lipgloss.BrightBlack,
		SegGood:    lipgloss.Green,
		SegCaution: lipgloss.Yellow,
		SegAlert:   lipgloss.Red,
		SegDone:    lipgloss.Magenta,
	}
	for kind, c := range want {
		if toneColors[kind] != c {
			t.Errorf("%s の色 = %v, want %v", toneName(kind), toneColors[kind], c)
		}
	}
	if toneColors[SegPlain] != nil {
		t.Errorf("SegPlain に色が付いている: %v", toneColors[SegPlain])
	}
	if len(toneColors) != len(want)+1 {
		t.Errorf("トーン数 = %d, want %d（新しいトーンは本テストにも登録する）", len(toneColors), len(want)+1)
	}
	for kind, c := range toneColors {
		if c != nil && !isANSI(c) {
			t.Errorf("%s の色 %v が ANSI 16 色の外にある（端末テーマ追従のため hex は使わない）", toneName(SegmentKind(kind)), c)
		}
	}
	for _, c := range append([]color.Color{}, columnColors["gray"], columnColors["purple"]) {
		if !isANSI(c) {
			t.Errorf("列色 %v が ANSI 16 色の外にある", c)
		}
	}
}

// The tones a card actually uses have to be visibly apart from each other, or colouring by state
// says nothing. Grey-as-inert and grey-as-metadata deliberately share a colour and differ by faint.
func TestToneStylesAreDistinct(t *testing.T) {
	s := newStyles()
	seen := map[string]SegmentKind{}
	for _, kind := range []SegmentKind{SegPlain, SegRef, SegGood, SegCaution, SegAlert, SegDone} {
		rendered := s.segment(kind).Render("x")
		if prev, ok := seen[rendered]; ok {
			t.Errorf("%s と %s が同じ描画になる: %q", toneName(kind), toneName(prev), rendered)
		}
		seen[rendered] = kind
	}
	if s.segment(SegMuted).Render("x") == s.segment(SegDim).Render("x") {
		t.Error("SegMuted と SegDim が同じ描画になる（dim は faint で区別する）")
	}
}
