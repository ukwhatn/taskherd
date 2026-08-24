package tui

import (
	"testing"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
	"github.com/ukwhatn/taskherd/internal/model"
)

func TestParseIconMode(t *testing.T) {
	tests := []struct {
		in     string
		want   IconMode
		wantOK bool
	}{
		{"nerd", IconNerd, true},
		{"ascii", IconASCII, true},
		{"none", IconNone, true},
		{"", IconNerd, false},
		{"Nerd", IconNerd, false},
		{"emoji", IconNerd, false},
	}
	for _, tc := range tests {
		got, ok := ParseIconMode(tc.in)
		if got != tc.want || ok != tc.wantOK {
			t.Errorf("ParseIconMode(%q) = (%q, %v), want (%q, %v)", tc.in, got, ok, tc.want, tc.wantOK)
		}
	}
}

// Every nerd glyph must be a single private-use codepoint. Private use is the only range a
// Japanese text font never defines, so the patched monospace font is guaranteed to be the one that
// paints it — which is what makes it exactly one cell wide.
func TestNerdIconsAreSinglePrivateUseGlyphs(t *testing.T) {
	for _, glyph := range nerdIcons.all() {
		if glyph == "" {
			t.Error("nerd セットに空のフィールドがある")
			continue
		}
		if boxDrawing[[]rune(glyph)[0]] {
			continue // the card edge bars, which are box drawing on purpose
		}
		if utf8.RuneCountInString(glyph) != 1 {
			t.Errorf("%q は 1 コードポイントでない", glyph)
			continue
		}
		r := []rune(glyph)[0]
		if r < 0xE000 || r > 0xF8FF {
			t.Errorf("U+%04X は BMP 私用領域 (E000-F8FF) の外", r)
		}
		if w := lipgloss.Width(glyph); w != 1 {
			t.Errorf("U+%04X の計算幅 = %d, want 1", r, w)
		}
	}
}

// The ascii and none sets exist for a terminal without a patched font, so nothing in them may need
// one.
func TestFallbackIconsAreASCIIOrJapanese(t *testing.T) {
	for _, set := range []IconSet{asciiIcons, noneIcons} {
		for _, glyph := range set.all() {
			for _, r := range glyph {
				if r >= 0xE000 && r <= 0xF8FF {
					t.Errorf("%s セットに私用領域の文字 U+%04X がある", set.Mode, r)
				}
			}
			if runes := UnsafeWidthRunes(glyph); len(runes) > 0 {
				t.Errorf("%s セットの %q に幅が不安定な文字 %q がある", set.Mode, glyph, string(runes))
			}
		}
	}
}

// Only the nerd set has a glyph per PR state, and it has to actually use four different ones or
// the row would drop the state without anything taking its place.
func TestLinkIconStateCoverage(t *testing.T) {
	seen := map[string]bool{}
	for _, phase := range []linkPhase{phaseOpen, phaseDraft, phaseMerged, phaseClosed} {
		glyph := nerdIcons.linkGlyph(model.LinkKindGitHubPR, phase)
		if seen[glyph] {
			t.Errorf("PR の状態 %v が他と同じグリフ %q を使っている", phase, glyph)
		}
		seen[glyph] = true
	}
	if !nerdIcons.StateInLinkIcon {
		t.Error("nerd セットは状態をアイコンで表すはず")
	}
	for _, set := range []IconSet{asciiIcons, noneIcons} {
		if set.StateInLinkIcon {
			t.Errorf("%s セットは PR の状態ごとのグリフを持たないのに持つと申告している", set.Mode)
		}
	}
}

func TestDeclaredIconsCoversEverySet(t *testing.T) {
	for _, set := range []IconSet{nerdIcons, asciiIcons, noneIcons} {
		for _, glyph := range set.all() {
			for _, r := range glyph {
				if !declaredIcons[r] {
					t.Errorf("%s セットの U+%04X が declaredIcons に無い", set.Mode, r)
				}
			}
		}
	}
}
