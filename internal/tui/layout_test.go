package tui

import "testing"

func cols(n int, collapsed ...int) []Column {
	built := make([]Column, n)
	for i := range built {
		built[i] = Column{ID: string(rune('a' + i))}
	}
	for _, i := range collapsed {
		built[i].Collapsed = true
	}
	return built
}

func TestLayoutColumnsFitsAllWhenWideEnough(t *testing.T) {
	m := DensityRoomy.metrics()
	layout := LayoutColumns(cols(3), 0, 200, 0, DensityRoomy)

	if layout.Start != 0 || len(layout.Widths) != 3 {
		t.Fatalf("layout = %+v, want 3 列すべて", layout)
	}
	total := 0
	for _, w := range layout.Widths {
		if w < m.minColumn() {
			t.Errorf("width = %d, want >= %d", w, m.minColumn())
		}
		total += w
	}
	if want := columnViewportWidth(200, m, 0); total+2*m.gap != want {
		t.Errorf("合計幅 = %d, want %d（余白を配分しきる）", total+2*m.gap, want)
	}
}

func TestLayoutColumnsSpreadsSpareEvenly(t *testing.T) {
	m := DensityRoomy.metrics()
	layout := LayoutColumns(cols(2), 0, 60+m.gap+2*m.boardPad, 0, DensityRoomy)

	// 60 cells across two columns once the gutter is taken out: 30 each.
	if layout.Widths[0] != 30 || layout.Widths[1] != 30 {
		t.Errorf("Widths = %v, want [30 30]", layout.Widths)
	}
}

// Every column the board draws has to be readable: the width comes from what a card needs, and
// the columns that do not get it are scrolled to rather than squeezed.
func TestLayoutColumnsNeverGoesBelowReadableWidth(t *testing.T) {
	board := cols(8)

	for width := 1; width <= 250; width++ {
		density := ChooseDensity(board, width, 0)
		m := density.metrics()
		layout := LayoutColumns(board, 3, width, 0, density)
		if len(layout.Widths) == 0 {
			continue
		}

		used := 2 * m.boardPad
		for i, w := range layout.Widths {
			if got := cardInner(w, m); got < readableCardWidth {
				t.Fatalf("width=%d density=%v: 列の内容幅 = %d, want >= %d",
					width, density, got, readableCardWidth)
			}
			if i > 0 {
				used += m.gap
			}
			used += w
		}
		if used > width {
			t.Fatalf("width=%d density=%v: 行幅 = %d, want <= %d", width, density, used, width)
		}
	}
}

// A board wider than the terminal scrolls sideways rather than squeezing columns below a
// readable width, and the window slides just far enough to keep the focus visible.
func TestLayoutColumnsWindowFollowsFocus(t *testing.T) {
	all := cols(6)

	left := LayoutColumns(all, 0, 50, 0, DensityRoomy)
	if left.Start != 0 || !left.Visible(0) {
		t.Fatalf("layout = %+v, want 先頭列が見える", left)
	}
	if left.End() >= len(all) {
		t.Fatalf("layout = %+v, want 一部の列だけ", left)
	}

	right := LayoutColumns(all, 5, 50, 0, DensityRoomy)
	if !right.Visible(5) {
		t.Errorf("layout = %+v, want フォーカス列 5 が見える", right)
	}
	if right.Start == 0 {
		t.Errorf("layout = %+v, want 窓が右へずれている", right)
	}
}

// A terminal too narrow for even one readable column gets no board at all: a column clipped in
// half says less than the message the caller puts in its place.
func TestLayoutColumnsShowsNothingWhenTooNarrow(t *testing.T) {
	layout := LayoutColumns(cols(4), 2, 5, 0, DensityRoomy)

	if len(layout.Widths) != 0 {
		t.Errorf("layout = %+v, want 0 列", layout)
	}
}

func TestLayoutColumnsEmpty(t *testing.T) {
	if layout := LayoutColumns(nil, 0, 100, 0, DensityRoomy); len(layout.Widths) != 0 {
		t.Errorf("layout = %+v, want 空", layout)
	}
	if layout := LayoutColumns(cols(2), 0, 0, 0, DensityRoomy); len(layout.Widths) != 0 {
		t.Errorf("layout = %+v, want 空", layout)
	}
}

// Decoration is what the board spends to buy columns: every column keeps the same readable width,
// so giving up the gutters and then the boxes is only worth it when it puts one more column on
// screen. When several densities fit the same number, the roomiest of them wins.
func TestChooseDensityBuysColumnsWithDecoration(t *testing.T) {
	board := cols(6)

	tests := []struct {
		name    string
		width   int
		want    Density
		columns int
	}{
		{"全列が入る幅では余白を取る", 200, DensityRoomy, 6},
		{"装飾を削っても列数が変わらない幅では厚い方を選ぶ", 100, DensityRoomy, 3},
		{"ボーダーを削れば 1 列増える幅では削る", 80, DensityCompact, 3},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ChooseDensity(board, tc.width, 0)
			if got != tc.want {
				t.Fatalf("ChooseDensity(_, %d, 0) = %v, want %v", tc.width, got, tc.want)
			}
			if layout := LayoutColumns(board, 0, tc.width, 0, got); len(layout.Widths) != tc.columns {
				t.Errorf("列数 = %d, want %d", len(layout.Widths), tc.columns)
			}
		})
	}
}

func TestFitCards(t *testing.T) {
	// Cards four lines tall with a blank line between them: the first costs 4, each next 5.
	four := []int{4, 4, 4, 4, 4}

	tests := []struct {
		name                       string
		heights                    []int
		gap, avail, selected, prev int
		start, end, above, below   int
	}{
		{"全部入るなら全部見せる", four[:2], 1, 14, 0, 0, 0, 2, 0, 0},
		{"入りきらない分はインジケータに出す", four, 1, 14, 0, 0, 0, 2, 0, 3},
		{"カーソルを追って下へ送る", four, 1, 14, 4, 0, 3, 5, 3, 0},
		{"カーソルが戻れば窓も戻る", four, 1, 14, 0, 3, 0, 2, 0, 3},
		{"列が縮んだら窓を詰める", four[:3], 1, 14, 0, 5, 0, 3, 0, 0},
		{"高さが不揃いでも詰められる", []int{3, 4, 3, 4}, 1, 12, 0, 0, 0, 2, 0, 2},
		{"1 枚も入らない高さでも 1 枚は出す", four[:1], 1, 2, 0, 0, 0, 1, 0, 0},
		{"空の列", nil, 1, 14, 0, 0, 0, 0, 0, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := FitCards(tc.heights, tc.gap, tc.avail, tc.selected, tc.prev)
			want := CardWindow{Start: tc.start, End: tc.end, Above: tc.above, Below: tc.below}
			if got != want {
				t.Errorf("FitCards(%v, %d, %d, %d, %d) = %+v, want %+v",
					tc.heights, tc.gap, tc.avail, tc.selected, tc.prev, got, want)
			}
		})
	}
}

// Whatever the window is, the selected card has to be inside it: the cursor may never sit on a
// card the column is not drawing.
func TestFitCardsAlwaysShowsSelection(t *testing.T) {
	heights := []int{3, 4, 4, 3, 5, 4, 4}
	for avail := 3; avail <= 30; avail++ {
		for selected := range heights {
			for prev := range heights {
				got := FitCards(heights, 1, avail, selected, prev)
				if selected < got.Start || selected >= got.End {
					t.Fatalf("avail=%d selected=%d prev=%d: window = %+v が選択を含まない",
						avail, selected, prev, got)
				}
				if got.Above != got.Start || got.Below != len(heights)-got.End {
					t.Fatalf("avail=%d selected=%d prev=%d: window = %+v の件数が窓と合わない",
						avail, selected, prev, got)
				}
			}
		}
	}
}

func TestScrollOffset(t *testing.T) {
	tests := []struct {
		name                            string
		offset, selected, visible, size int
		want                            int
	}{
		{"全部入るなら動かさない", 0, 3, 10, 5, 0},
		{"下へはみ出したら追う", 0, 5, 3, 10, 3},
		{"上へはみ出したら戻る", 5, 2, 3, 10, 2},
		{"範囲内なら維持する", 2, 3, 3, 10, 2},
		{"末尾が縮んだら詰める", 8, 4, 3, 6, 3},
		{"空", 0, 0, 3, 0, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := scrollOffset(tc.offset, tc.selected, tc.visible, tc.size)
			if got != tc.want {
				t.Errorf("scrollOffset(%d,%d,%d,%d) = %d, want %d",
					tc.offset, tc.selected, tc.visible, tc.size, got, tc.want)
			}
		})
	}
}
