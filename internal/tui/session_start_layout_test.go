package tui

import (
	"testing"

	"github.com/ukwhatn/taskherd/internal/pathcomp"
)

// The prompt gives way before the list does, and neither goes below its floor: the order is what
// decides which of the two the user loses first on a short terminal.
func TestSessionStartHeights(t *testing.T) {
	for _, tc := range []struct {
		name       string
		height     int
		listWant   int
		fixed      int
		listFloor  int
		wantList   int
		wantPrompt int
	}{
		{name: "両方収まる", height: 40, listWant: 5, fixed: 5, listFloor: 1, wantList: 5, wantPrompt: 6},
		{name: "候補が少なければ要求分だけ", height: 40, listWant: 2, fixed: 5, listFloor: 1, wantList: 2, wantPrompt: 6},
		{name: "上限で頭打ち", height: 40, listWant: 30, fixed: 5, listFloor: 1, wantList: maxCwdListRows, wantPrompt: 6},
		// 24 行なら body は 20、fixed 5 を引いて 15 なので 6+5 はまだ収まる。
		{name: "80x24 は無傷", height: 24, listWant: 5, fixed: 5, listFloor: 1, wantList: 5, wantPrompt: 6},
		// body 16 - fixed 5 = 11 に 6+5 がちょうど収まる。
		{name: "境界ちょうど", height: 20, listWant: 5, fixed: 5, listFloor: 1, wantList: 5, wantPrompt: 6},
		// body 14 - 5 = 9。まずプロンプトが 6→3 まで削れ、次に候補が 5→... の順。
		{name: "先にプロンプトが削れる", height: 18, listWant: 5, fixed: 5, listFloor: 1, wantList: 5, wantPrompt: 4},
		{name: "プロンプトの下限で候補が削れる", height: 15, listWant: 5, fixed: 5, listFloor: 1, wantList: 3, wantPrompt: 3},
		// 候補一覧はカーソル行の 1 行を必ず残す。
		{name: "候補は 1 行を残す", height: 11, listWant: 5, fixed: 5, listFloor: 1, wantList: 1, wantPrompt: 3},
		// サジェストは選択が無いので 0 まで消える。
		{name: "サジェストは 0 まで消える", height: 11, listWant: 5, fixed: 5, listFloor: 0, wantList: 0, wantPrompt: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := &Board{height: tc.height}
			list, prompt := b.sessionStartHeights(tc.listWant, tc.fixed, tc.listFloor)
			if list != tc.wantList || prompt != tc.wantPrompt {
				t.Errorf("= (%d, %d), want (%d, %d)", list, prompt, tc.wantList, tc.wantPrompt)
			}
		})
	}
}

// The completion list asks for the row it spends on the count of what it is not showing, and for
// nothing extra when it is showing everything.
func TestSuggestionRows(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   pathcomp.Suggestions
		want int
	}{
		{name: "空", in: pathcomp.Suggestions{}, want: 0},
		{name: "全部出せる", in: pathcomp.Suggestions{Items: []string{"a", "b"}, Total: 2}, want: 2},
		{name: "省いた分の行を足す", in: pathcomp.Suggestions{Items: []string{"a", "b"}, Total: 9}, want: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := suggestionRows(tc.in); got != tc.want {
				t.Errorf("= %d, want %d", got, tc.want)
			}
		})
	}
}
