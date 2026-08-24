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

func TestWrapTitleBreaksByCell(t *testing.T) {
	tests := []struct {
		name     string
		in       string
		width    int
		maxLines int
		want     []string
	}{
		{"収まるなら 1 行", "abcdef", 6, 2, []string{"abcdef"}},
		{"1 セル超えたら 2 行目へ", "abcdefg", 6, 2, []string{"abcdef", "g"}},
		{"全角は 2 セルとして折る", "ab設計cd", 7, 2, []string{"ab設計c", "d"}},
		{"最終行に入らない分は切る", "abcdefghij", 4, 2, []string{"abcd", "efg~"}},
		{"改行と連続空白は 1 つの空白へ", "a\nb  c", 3, 2, []string{"a b", "c"}},
		{"幅 1 でも ASCII なら折れる", "abc", 1, 2, []string{"a", "~"}},
		{"幅 1 に全角は 1 文字も入らない", "設計", 1, 2, []string{"~"}},
		{"幅 0", "abc", 0, 2, nil},
		{"行数 0", "abc", 10, 0, nil},
		{"1 行しか使えないなら切る", "abcdefg", 4, 1, []string{"abc~"}},
		{"空のタイトルでも 1 行は返す", "   ", 10, 2, []string{""}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := wrapTitle(tc.in, tc.width, tc.maxLines)
			if len(got) != len(tc.want) {
				t.Fatalf("wrapTitle(%q,%d,%d) = %q, want %q", tc.in, tc.width, tc.maxLines, got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Fatalf("wrapTitle(%q,%d,%d) = %q, want %q", tc.in, tc.width, tc.maxLines, got, tc.want)
				}
			}
			for _, line := range got {
				if w := lipgloss.Width(line); w > tc.width {
					t.Errorf("行 %q の表示幅 = %d, want <= %d", line, w, tc.width)
				}
			}
		})
	}
}

// Whatever the title and the width, the wrap fits: every line inside the column and no more lines
// than the card budgeted for.
func TestWrapTitleStaysWithinBudget(t *testing.T) {
	titles := []string{
		"#12 設計する",
		"#3 board のレイアウトを可読性から組み直す",
		"#456 mixed 日本語 and ascii title that keeps going",
		"#7 aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}
	for _, title := range titles {
		for width := 1; width <= 40; width++ {
			got := wrapTitle(title, width, maxTitleLines)
			if len(got) == 0 || len(got) > maxTitleLines {
				t.Fatalf("wrapTitle(%q,%d) = %q, want 1..%d 行", title, width, got, maxTitleLines)
			}
			for _, line := range got {
				if w := lipgloss.Width(line); w > width {
					t.Fatalf("wrapTitle(%q,%d): 行 %q の幅 = %d", title, width, line, w)
				}
			}
		}
	}
}
