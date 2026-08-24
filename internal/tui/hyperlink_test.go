package tui

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/ukwhatn/taskherd/internal/model"
)

func TestHyperlinkWrapsWithoutChangingWidth(t *testing.T) {
	url := "https://github.com/owner/repo/pull/123"
	text := "owner/repo#123"

	wrapped := hyperlink(url, text)

	if !strings.Contains(wrapped, url) {
		t.Errorf("URL が埋め込まれていない: %q", wrapped)
	}
	if !strings.HasPrefix(wrapped, osc8Open) || !strings.HasSuffix(wrapped, osc8Open+osc8Close) {
		t.Errorf("OSC 8 で囲まれていない: %q", wrapped)
	}
	if got, want := lipgloss.Width(wrapped), lipgloss.Width(text); got != want {
		t.Errorf("幅 = %d, want %d（エスケープは幅に数えない）", got, want)
	}
}

// A styled row wrapped in OSC 8 still has to measure as its visible text, or a card's border goes
// ragged the moment a link is drawn inside it.
func TestHyperlinkSurvivesStylingAndBorders(t *testing.T) {
	styled := lipgloss.NewStyle().Foreground(lipgloss.Green).Render("owner/repo#123")
	wrapped := hyperlink("https://example.com/x", styled)

	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Width(24).MaxWidth(24).Render("title\n" + wrapped)
	for i, line := range strings.Split(box, "\n") {
		if got := lipgloss.Width(line); got != 24 {
			t.Errorf("ボックス %d 行目の幅 = %d, want 24: %q", i, got, line)
		}
	}
}

// A URL is user input. A control byte inside one would be read by the terminal as a command rather
// than printed, so it never reaches the escape sequence.
func TestHyperlinkSanitizesURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{"制御文字を落とす", "https://example.com/\x1b]0;pwned\x07", "https://example.com/]0;pwned"},
		{"改行を落とす", "https://example.com/a\nb", "https://example.com/ab"},
		{"前後の空白を落とす", "  https://example.com/a  ", "https://example.com/a"},
		{"通常の URL はそのまま", "https://example.com/a?b=c#d", "https://example.com/a?b=c#d"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := sanitizeURI(tc.url); got != tc.want {
				t.Errorf("sanitizeURI(%q) = %q, want %q", tc.url, got, tc.want)
			}
			wrapped := hyperlink(tc.url, "x")
			if strings.Count(wrapped, "\x1b") != 4 {
				t.Errorf("エスケープが %d 個: %q", strings.Count(wrapped, "\x1b"), wrapped)
			}
		})
	}
}

func TestHyperlinkLeavesEmptyInputAlone(t *testing.T) {
	if got := hyperlink("", "text"); got != "text" {
		t.Errorf("URL 無し = %q, want text", got)
	}
	if got := hyperlink("https://example.com", ""); got != "" {
		t.Errorf("テキスト無し = %q, want 空", got)
	}
	if got := hyperlink("\x00\x01", "text"); got != "text" {
		t.Errorf("制御文字だけの URL = %q, want text（リンクにしない）", got)
	}
}

func TestBoardHyperlinksFollowConfig(t *testing.T) {
	url := "https://github.com/owner/repo/pull/123"
	task := model.Task{ID: 1, Title: "task", Status: "todo",
		Links: []model.Link{{URL: url, Kind: model.LinkKindGitHubPR}}}

	for _, enabled := range []bool{true, false} {
		t.Run(fmt.Sprintf("hyperlinks=%v", enabled), func(t *testing.T) {
			h := newHarness(t, Deps{Tasks: newFakeStore(task)}, Settings{
				Columns: model.DefaultColumns(), Classifier: testClassifier,
				Icons: IconASCII, Hyperlinks: enabled,
			})
			view := h.board.render()

			if strings.Contains(view, url) != enabled {
				t.Errorf("URL の埋め込み = %v, want %v", strings.Contains(view, url), enabled)
			}
			if strings.Contains(view, osc8Open) != enabled {
				t.Errorf("OSC 8 の有無 = %v, want %v", strings.Contains(view, osc8Open), enabled)
			}
			for _, line := range strings.Split(view, "\n") {
				if got := lipgloss.Width(line); got > h.board.width {
					t.Fatalf("行幅 = %d, want <= %d: %q", got, h.board.width, line)
				}
			}
		})
	}
}

// The overflow row summarizes links rather than being one, so it must not become a link to
// whatever URL happened to precede it.
func TestOverflowRowIsNotHyperlinked(t *testing.T) {
	links := []model.Link{}
	for i := 1; i <= maxCardLinkRows+1; i++ {
		links = append(links, model.Link{URL: fmt.Sprintf("https://github.com/o/r/pull/%d", i), Kind: model.LinkKindGitHubPR})
	}
	h := newHarness(t, Deps{Tasks: newFakeStore(model.Task{ID: 1, Title: "task", Status: "todo", Links: links})},
		Settings{Columns: model.DefaultColumns(), Classifier: testClassifier, Icons: IconASCII, Hyperlinks: true})

	view := h.board.render()

	if strings.Contains(view, "https://github.com/o/r/pull/4") {
		t.Errorf("上限を超えたリンクの URL が埋め込まれている:\n%q", view)
	}
	if !strings.Contains(view, "他 1 件") {
		t.Errorf("集約行が描画されていない:\n%s", view)
	}
}
