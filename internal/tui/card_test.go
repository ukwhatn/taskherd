package tui

import (
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/ukwhatn/taskherd/internal/fetch"
	"github.com/ukwhatn/taskherd/internal/herdrc"
	"github.com/ukwhatn/taskherd/internal/model"
)

var cardNow = time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)

func due(s string) *model.Date {
	d := model.Date(s)
	return &d
}

func TestBuildCardTitleCarriesID(t *testing.T) {
	card := BuildCard(model.Task{ID: 12, Title: "設計する"}, SessionBadge{}, nil, testStyle(testIcons), cardNow)

	if card.Title != "#12 設計する" {
		t.Errorf("Title = %q, want #12 設計する", card.Title)
	}
	if card.TaskID != 12 {
		t.Errorf("TaskID = %d, want 12", card.TaskID)
	}
}

func TestBuildCardMarksOverdueDue(t *testing.T) {
	tests := []struct {
		name string
		due  string
		want SegmentKind
	}{
		{"過去", "2026-08-23", SegAlert},
		{"当日はまだ超過でない", "2026-08-24", SegCaution},
		{"翌日は近い", "2026-08-25", SegCaution},
		{"それ以降は既定色", "2026-08-26", SegPlain},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			card := BuildCard(model.Task{ID: 1, Title: "t", Due: due(tc.due)}, SessionBadge{}, nil, testStyle(testIcons), cardNow)
			if len(card.Meta) != 1 {
				t.Fatalf("Meta = %+v, want 1 件", card.Meta)
			}
			if card.Meta[0].Kind != tc.want {
				t.Errorf("Kind = %v, want %v", card.Meta[0].Kind, tc.want)
			}
		})
	}
}

func TestBuildCardOmitsEmptyBadges(t *testing.T) {
	card := BuildCard(model.Task{ID: 1, Title: "t"}, SessionBadge{}, nil, testStyle(testIcons), cardNow)

	if len(card.Meta) != 0 {
		t.Errorf("Meta = %+v, want 空", card.Meta)
	}
}

// The meta line carries the due date and the session badge; links get rows of their own under it.
func TestBuildCardSegmentOrder(t *testing.T) {
	url := "https://github.com/owner/repo/pull/1"
	card := BuildCard(
		model.Task{ID: 1, Title: "t", Due: due("2026-09-01"),
			Links: []model.Link{{URL: url, Kind: model.LinkKindGitHubPR}}},
		SessionBadge{Text: "* working", State: herdrc.StateWorking},
		map[string]fetch.LinkState{url: ghState(url, fetch.GitHubData{State: "OPEN", Checks: "none"}, model.LinkKindGitHubPR)},
		testStyle(testIcons),
		cardNow,
	)

	want := []string{"2026-09-01", "* working"}
	if len(card.Meta) != len(want) {
		t.Fatalf("Meta = %+v, want %d 件", card.Meta, len(want))
	}
	for i := range want {
		if card.Meta[i].Text != want[i] {
			t.Errorf("Meta[%d] = %q, want %q", i, card.Meta[i].Text, want[i])
		}
	}
	if len(card.Links) != 1 || card.Links[0].Refs[0] != "owner/repo#1" {
		t.Errorf("Links = %+v, want owner/repo#1 の 1 行", card.Links)
	}
}

// The nerd set prefixes the due date with a calendar glyph; the modes without one must not leave
// a stray space in front of the date.
func TestBuildCardDueIconOnlyWhereThereIsOne(t *testing.T) {
	task := model.Task{ID: 1, Title: "t", Due: due("2026-09-01")}

	ascii := BuildCard(task, SessionBadge{}, nil, testStyle(testIcons), cardNow)
	if ascii.Meta[0].Text != "2026-09-01" {
		t.Errorf("ascii = %q, want 日付のみ", ascii.Meta[0].Text)
	}
	nerd := BuildCard(task, SessionBadge{}, nil, testStyle(Icons(IconNerd)), cardNow)
	if nerd.Meta[0].Text != nfOctCalendar+" 2026-09-01" {
		t.Errorf("nerd = %q, want カレンダーグリフ付き", nerd.Meta[0].Text)
	}
}

// An offline session badge is styled apart from a live one so it does not read as a live state.
func TestBuildCardOfflineSessionSegment(t *testing.T) {
	card := BuildCard(model.Task{ID: 1, Title: "t"},
		SessionBadge{Text: "- offline", State: herdrc.StateOffline}, nil, testStyle(testIcons), cardNow)

	if card.Meta[0].Kind != SegDim {
		t.Errorf("Kind = %v, want SegDim", card.Meta[0].Kind)
	}
}

func TestTruncateCountsDisplayWidth(t *testing.T) {
	tests := []struct {
		name  string
		in    string
		width int
		want  string
	}{
		{"収まるならそのまま", "abc", 5, "abc"},
		{"ちょうど", "abcde", 5, "abcde"},
		{"切る", "abcdef", 5, "abcd~"},
		{"幅 1", "abc", 1, "~"},
		{"幅 0", "abc", 0, ""},
		{"全角は 2 セル", "設計する", 5, "設計~"},
		{"全角がちょうど収まる", "設計", 4, "設計"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := truncate(tc.in, tc.width)
			if got != tc.want {
				t.Errorf("truncate(%q,%d) = %q, want %q", tc.in, tc.width, got, tc.want)
			}
			if w := lipgloss.Width(got); w > tc.width {
				t.Errorf("表示幅 = %d, want <= %d", w, tc.width)
			}
		})
	}
}
