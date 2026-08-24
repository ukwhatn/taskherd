package tui

// Column geometry. A collapsed column only has to fit its label and count; an expanded one has
// to fit a readable card, and the gap is the single space between two columns.
const (
	collapsedWidth = 12
	minColumnWidth = 18
	columnGap      = 1
)

// Layout is the horizontal geometry of the board for one terminal width: which columns are on
// screen, and how wide each of them is.
type Layout struct {
	// Start is the index of the first visible column.
	Start int
	// Widths holds the width of each visible column, aligned with columns[Start:].
	Widths []int
}

// End returns the index one past the last visible column.
func (l Layout) End() int { return l.Start + len(l.Widths) }

// Visible reports whether the column at index i is on screen.
func (l Layout) Visible(i int) bool { return i >= l.Start && i < l.End() }

// LayoutColumns decides which columns fit in total width and how the space is shared.
//
// When the board is wider than the terminal it scrolls sideways rather than squeezing columns
// below a readable width: the window slides just far enough to keep the focused column visible.
func LayoutColumns(columns []Column, focus, total int) Layout {
	if len(columns) == 0 || total <= 0 {
		return Layout{}
	}
	if focus < 0 {
		focus = 0
	}
	if focus >= len(columns) {
		focus = len(columns) - 1
	}

	start := 0
	var end int
	for {
		end = fitFrom(columns, start, total)
		// end == start means not even one column fits; showing the focused one clipped beats
		// showing nothing at all.
		if end <= start {
			start, end = focus, focus+1
			break
		}
		if end > focus {
			break
		}
		start++
	}

	widths := make([]int, 0, end-start)
	for i := start; i < end; i++ {
		widths = append(widths, minWidth(columns[i]))
	}
	distribute(columns[start:end], widths, total)
	return Layout{Start: start, Widths: widths}
}

// fitFrom returns the exclusive end index of the longest run of columns from start that fits.
func fitFrom(columns []Column, start, total int) int {
	used := 0
	end := start
	for i := start; i < len(columns); i++ {
		need := minWidth(columns[i])
		if i > start {
			need += columnGap
		}
		if used+need > total {
			break
		}
		used += need
		end = i + 1
	}
	return end
}

func minWidth(col Column) int {
	if col.Collapsed {
		return collapsedWidth
	}
	return minColumnWidth
}

// distribute hands the leftover width to the expanded columns, one cell at a time so that the
// widths stay within one cell of each other instead of piling up on the leftmost column.
func distribute(columns []Column, widths []int, total int) {
	expandable := make([]int, 0, len(columns))
	used := 0
	for i := range widths {
		used += widths[i]
		if i > 0 {
			used += columnGap
		}
		if !columns[i].Collapsed {
			expandable = append(expandable, i)
		}
	}
	if len(expandable) == 0 {
		return
	}
	for spare := total - used; spare > 0; {
		for _, i := range expandable {
			if spare == 0 {
				break
			}
			widths[i]++
			spare--
		}
	}
}

// scrollOffset keeps the selected row inside a viewport of the given height, returning the new
// first visible row. The previous offset is respected so the list does not jump when it need not.
func scrollOffset(offset, selected, visible, count int) int {
	if visible <= 0 || count <= 0 {
		return 0
	}
	if offset > count-visible {
		offset = count - visible
	}
	if offset < 0 {
		offset = 0
	}
	if selected < offset {
		return selected
	}
	if selected >= offset+visible {
		return selected - visible + 1
	}
	return offset
}
