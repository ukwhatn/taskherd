package tui

// Density is how much decoration the board can afford at the terminal's current width.
//
// The board gives up decoration before it gives up columns: squeezing the gutters, then the card
// boxes, keeps more of the board on screen than pushing a column off the edge and making the user
// scroll sideways to reach it.
type Density int

const (
	// DensityRoomy draws padded card boxes with a wide gutter between columns.
	DensityRoomy Density = iota
	// DensityTight keeps the boxes but drops the padding inside them and narrows the gutter.
	DensityTight
	// DensityCompact replaces each box with a single edge bar, the last step before the board
	// starts scrolling sideways.
	DensityCompact
)

// metrics are the cell budgets one density renders with.
type metrics struct {
	// boxed draws a full border around every card; otherwise a card gets a left edge bar.
	boxed bool
	// padX is the horizontal padding inside a card box.
	padX int
	// gap is the number of cells between two columns.
	gap int
	// cardGap is the number of blank lines between two cards.
	cardGap int
	// boardPad is the margin at the board's left and right edge.
	boardPad int
	// minColumn is the narrowest an expanded column may get, collapsed the same for a folded one.
	minColumn int
	collapsed int
}

func (d Density) metrics() metrics {
	switch d {
	case DensityTight:
		return metrics{boxed: true, padX: 0, gap: 1, cardGap: 1, boardPad: 1, minColumn: 17, collapsed: 12}
	case DensityCompact:
		return metrics{boxed: false, padX: 0, gap: 1, cardGap: 1, boardPad: 0, minColumn: 15, collapsed: 10}
	default:
		return metrics{boxed: true, padX: 1, gap: 2, cardGap: 1, boardPad: 1, minColumn: 22, collapsed: 14}
	}
}

// ChooseDensity picks the decoration level for a terminal of the given width: whichever puts the
// most columns on screen, and the roomiest of those when several manage the same number.
//
// The count is always taken from the leftmost column rather than from the visible window, so that
// scrolling sideways cannot change how the whole board is drawn under the user.
func ChooseDensity(columns []Column, total int) Density {
	best := DensityRoomy
	bestFit := densityFit(columns, total, best)
	for _, d := range []Density{DensityTight, DensityCompact} {
		if fit := densityFit(columns, total, d); fit > bestFit {
			best, bestFit = d, fit
		}
	}
	return best
}

func densityFit(columns []Column, total int, d Density) int {
	m := d.metrics()
	return fitFrom(columns, 0, total-2*m.boardPad, m)
}

// Layout is the horizontal geometry of the board for one terminal width: which columns are on
// screen, and how wide each of them is.
type Layout struct {
	// Start is the index of the first visible column.
	Start int
	// Widths holds the width of each visible column, aligned with columns[Start:].
	Widths []int
	// Gap is the number of cells drawn between two columns.
	Gap int
}

// End returns the index one past the last visible column.
func (l Layout) End() int { return l.Start + len(l.Widths) }

// Visible reports whether the column at index i is on screen.
func (l Layout) Visible(i int) bool { return i >= l.Start && i < l.End() }

// LayoutColumns decides which columns fit in total width and how the space is shared.
//
// When the board is wider than the terminal it scrolls sideways rather than squeezing columns
// below a readable width: the window slides just far enough to keep the focused column visible.
func LayoutColumns(columns []Column, focus, total int, d Density) Layout {
	m := d.metrics()
	if len(columns) == 0 || total <= 0 {
		return Layout{Gap: m.gap}
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
		end = fitFrom(columns, start, total, m)
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
		widths = append(widths, minWidth(columns[i], m))
	}
	distribute(columns[start:end], widths, total, m)
	return Layout{Start: start, Widths: widths, Gap: m.gap}
}

// fitFrom returns the exclusive end index of the longest run of columns from start that fits.
func fitFrom(columns []Column, start, total int, m metrics) int {
	used := 0
	end := start
	for i := start; i < len(columns); i++ {
		need := minWidth(columns[i], m)
		if i > start {
			need += m.gap
		}
		if used+need > total {
			break
		}
		used += need
		end = i + 1
	}
	return end
}

func minWidth(col Column, m metrics) int {
	if col.Collapsed {
		return m.collapsed
	}
	return m.minColumn
}

// distribute hands the leftover width to the expanded columns, one cell at a time so that the
// widths stay within one cell of each other instead of piling up on the leftmost column.
func distribute(columns []Column, widths []int, total int, m metrics) {
	expandable := make([]int, 0, len(columns))
	used := 0
	for i := range widths {
		used += widths[i]
		if i > 0 {
			used += m.gap
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

// CardWindow is the run of cards a column has room for, and how many are hidden on each side.
type CardWindow struct {
	Start, End int
	// Above and Below are the counts the overflow indicators report.
	Above, Below int
}

// FitCards picks the cards a column body of avail lines can show.
//
// heights are the rendered heights of the cards in order, gap the blank lines between them. prev
// is the run's previous first index, kept whenever it still leaves the selected card on screen so
// the column does not jump under a cursor that has not moved. Each hidden side costs one line for
// the indicator that says how many cards are there, so cards are never cut off in silence.
func FitCards(heights []int, gap, avail, selected, prev int) CardWindow {
	n := len(heights)
	if n == 0 || avail <= 0 {
		return CardWindow{}
	}
	selected = clampIndex(selected, n)
	start := clampIndex(prev, n)

	// Pull the window back when the column shrank under it, so it never leaves blank space below
	// the last card while cards are still hidden above.
	if last := lastStart(heights, gap, avail, n); start > last {
		start = last
	}
	if selected < start {
		start = selected
	}
	for start < selected && endFrom(heights, gap, avail, start) <= selected {
		start++
	}

	end := endFrom(heights, gap, avail, start)
	if end <= start {
		end = start + 1
	}
	return CardWindow{Start: start, End: end, Above: start, Below: n - end}
}

// endFrom returns the exclusive end of the run starting at start, after reserving a line for each
// overflow indicator that run turns out to need.
func endFrom(heights []int, gap, avail, start int) int {
	budget := avail
	if start > 0 {
		budget--
	}
	end := runEnd(heights, gap, budget, start)
	if end < len(heights) {
		end = runEnd(heights, gap, budget-1, start)
	}
	return end
}

func runEnd(heights []int, gap, budget, start int) int {
	used, end := 0, start
	for i := start; i < len(heights); i++ {
		need := heights[i]
		if i > start {
			need += gap
		}
		if used+need > budget {
			break
		}
		used += need
		end = i + 1
	}
	return end
}

// lastStart is the smallest first index whose run still reaches the last card.
func lastStart(heights []int, gap, avail, n int) int {
	for s := 0; s < n; s++ {
		if endFrom(heights, gap, avail, s) >= n {
			return s
		}
	}
	return n - 1
}

func clampIndex(i, n int) int {
	if i >= n {
		i = n - 1
	}
	if i < 0 {
		i = 0
	}
	return i
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
