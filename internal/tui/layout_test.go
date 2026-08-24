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
	layout := LayoutColumns(cols(3), 0, 200)

	if layout.Start != 0 || len(layout.Widths) != 3 {
		t.Fatalf("layout = %+v, want 3 列すべて", layout)
	}
	total := 0
	for _, w := range layout.Widths {
		if w < minColumnWidth {
			t.Errorf("width = %d, want >= %d", w, minColumnWidth)
		}
		total += w
	}
	if total+2*columnGap != 200 {
		t.Errorf("合計幅 = %d, want 200（余白を配分しきる）", total+2*columnGap)
	}
}

func TestLayoutColumnsSpreadsSpareEvenly(t *testing.T) {
	layout := LayoutColumns(cols(2), 0, 61)

	// 61 = 2 columns + 1 gap, so 60 cells across two columns: 30 each.
	if layout.Widths[0] != 30 || layout.Widths[1] != 30 {
		t.Errorf("Widths = %v, want [30 30]", layout.Widths)
	}
}

func TestLayoutColumnsKeepsCollapsedNarrow(t *testing.T) {
	layout := LayoutColumns(cols(2, 1), 0, 100)

	if layout.Widths[1] != collapsedWidth {
		t.Errorf("折り畳み列の幅 = %d, want %d", layout.Widths[1], collapsedWidth)
	}
	if layout.Widths[0] <= collapsedWidth {
		t.Errorf("展開列の幅 = %d, want 余白が回っている", layout.Widths[0])
	}
}

// A board wider than the terminal scrolls sideways rather than squeezing columns below a
// readable width, and the window slides just far enough to keep the focus visible.
func TestLayoutColumnsWindowFollowsFocus(t *testing.T) {
	all := cols(6)

	left := LayoutColumns(all, 0, 40)
	if left.Start != 0 || !left.Visible(0) {
		t.Fatalf("layout = %+v, want 先頭列が見える", left)
	}
	if left.End() >= len(all) {
		t.Fatalf("layout = %+v, want 一部の列だけ", left)
	}

	right := LayoutColumns(all, 5, 40)
	if !right.Visible(5) {
		t.Errorf("layout = %+v, want フォーカス列 5 が見える", right)
	}
	if right.Start == 0 {
		t.Errorf("layout = %+v, want 窓が右へずれている", right)
	}
}

func TestLayoutColumnsShowsFocusEvenWhenTooNarrow(t *testing.T) {
	layout := LayoutColumns(cols(4), 2, 5)

	if len(layout.Widths) != 1 || layout.Start != 2 {
		t.Errorf("layout = %+v, want フォーカス列のみ", layout)
	}
}

func TestLayoutColumnsEmpty(t *testing.T) {
	if layout := LayoutColumns(nil, 0, 100); len(layout.Widths) != 0 {
		t.Errorf("layout = %+v, want 空", layout)
	}
	if layout := LayoutColumns(cols(2), 0, 0); len(layout.Widths) != 0 {
		t.Errorf("layout = %+v, want 空", layout)
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
