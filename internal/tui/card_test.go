package tui

import (
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/ukwhatn/taskherd/internal/model"
)

var cardNow = time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)

func due(s string) *model.Date {
	d := model.Date(s)
	return &d
}

func TestBuildCardTitleCarriesID(t *testing.T) {
	card := BuildCard(model.Task{ID: 12, Title: "設計する"}, SessionBadge{}, nil, cardNow)

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
		{"過去", "2026-08-23", SegDueOverdue},
		{"当日はまだ超過でない", "2026-08-24", SegDue},
		{"未来", "2026-08-25", SegDue},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			card := BuildCard(model.Task{ID: 1, Title: "t", Due: due(tc.due)}, SessionBadge{}, nil, cardNow)
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
	card := BuildCard(model.Task{ID: 1, Title: "t"}, SessionBadge{}, nil, cardNow)

	if len(card.Meta) != 0 {
		t.Errorf("Meta = %+v, want 空", card.Meta)
	}
}

func TestBuildCardSegmentOrder(t *testing.T) {
	card := BuildCard(
		model.Task{ID: 1, Title: "t", Due: due("2026-09-01")},
		SessionBadge{Text: "●working"},
		[]LinkBadge{{Text: "PR:open✓ci"}},
		cardNow,
	)

	want := []string{"2026-09-01", "●working", "PR:open✓ci"}
	if len(card.Meta) != len(want) {
		t.Fatalf("Meta = %+v, want %d 件", card.Meta, len(want))
	}
	for i := range want {
		if card.Meta[i].Text != want[i] {
			t.Errorf("Meta[%d] = %q, want %q", i, card.Meta[i].Text, want[i])
		}
	}
}

func TestBuildCardStaleLinkShowsAge(t *testing.T) {
	card := BuildCard(
		model.Task{ID: 1, Title: "t"},
		SessionBadge{},
		[]LinkBadge{{Text: "PR:open", Stale: true, Age: 12 * time.Minute}},
		cardNow,
	)

	if card.Meta[0].Kind != SegLinkStale {
		t.Errorf("Kind = %v, want SegLinkStale", card.Meta[0].Kind)
	}
	if card.Meta[0].Text != "PR:open 12m" {
		t.Errorf("Text = %q, want 経過時間付き", card.Meta[0].Text)
	}
}

// An offline session badge is styled apart from a live one so a dash does not read as a state.
func TestBuildCardOfflineSessionSegment(t *testing.T) {
	card := BuildCard(model.Task{ID: 1, Title: "t"}, SessionBadge{Text: offlineBadge}, nil, cardNow)

	if card.Meta[0].Kind != SegSessionOffline {
		t.Errorf("Kind = %v, want SegSessionOffline", card.Meta[0].Kind)
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
		{"切る", "abcdef", 5, "abcd…"},
		{"幅 1", "abc", 1, "…"},
		{"幅 0", "abc", 0, ""},
		{"全角は 2 セル", "設計する", 5, "設計…"},
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
