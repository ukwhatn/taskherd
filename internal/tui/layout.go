package tui

// Density is how much decoration the board can afford at the terminal's current width.
//
// Every column the board draws is at least readableCardWidth cells of card text wide, whatever the
// density: a column narrower than that cannot say enough of a title to tell one card from the next,
// so it is worth less than the scroll it saves. What a density buys is how many such columns fit —
// giving up the gutters, then the card boxes, frees a few cells per column — and the columns that
// still do not fit are reached by scrolling sideways.
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

// readableCardWidth is the cells every drawn card keeps for its own text.
//
// Two lines of 24 cells carry 21 or 22 Japanese characters once `#id ` is taken out of them, which
// is enough to tell one task from another at a glance. It is not enough for a whole title and is
// not meant to be: the point is to hold the width at which a column stops being readable, in one
// place, so that the board spends a scroll rather than the words.
const readableCardWidth = 24

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
	// minCardContentWidth is the narrowest a card's own text may get in this density.
	minCardContentWidth int
}

func (d Density) metrics() metrics {
	switch d {
	case DensityTight:
		return metrics{boxed: true, padX: 0, gap: 1, cardGap: 1, boardPad: 1, minCardContentWidth: readableCardWidth}
	case DensityCompact:
		return metrics{boxed: false, padX: 0, gap: 1, cardGap: 1, boardPad: 0, minCardContentWidth: readableCardWidth}
	default:
		return metrics{boxed: true, padX: 1, gap: 2, cardGap: 1, boardPad: 1, minCardContentWidth: readableCardWidth}
	}
}

// minColumn is the narrowest a column may get: the readable card width plus whatever this density
// spends on decoration around it. It is the inverse of cardInner.
func (m metrics) minColumn() int {
	if m.boxed {
		return m.minCardContentWidth + 2 + 2*m.padX
	}
	return m.minCardContentWidth + 2
}

// columnViewportWidth is the cells the columns themselves have at a terminal of total width: the
// board's own margins and the folded-column stack at the right edge come out of it first.
//
// Both ChooseDensity and LayoutColumns measure from here, so the width a density was chosen for
// cannot drift from the width the columns are then laid out in.
func columnViewportWidth(total int, m metrics, stackWidth int) int {
	return total - 2*m.boardPad - stackWidth - collapsedStackGap(stackWidth, m)
}

// collapsedStackGap is the gutter between the rightmost column and the folded-column stack.
// lipgloss joins blocks flush against each other, so the board spends those cells itself, and only
// when there is a stack to separate.
func collapsedStackGap(stackWidth int, m metrics) int {
	if stackWidth <= 0 {
		return 0
	}
	return m.gap
}

// ChooseDensity picks the decoration level for a terminal of the given width: whichever puts the
// most columns on screen, and the roomiest of those when several manage the same number.
//
// The count is always taken from the leftmost column rather than from the visible window, so that
// scrolling sideways cannot change how the whole board is drawn under the user.
func ChooseDensity(columns []Column, total, stackWidth int) Density {
	best := DensityRoomy
	bestFit := densityFit(columns, total, stackWidth, best)
	for _, d := range []Density{DensityTight, DensityCompact} {
		if fit := densityFit(columns, total, stackWidth, d); fit > bestFit {
			best, bestFit = d, fit
		}
	}
	return best
}

func densityFit(columns []Column, total, stackWidth int, d Density) int {
	m := d.metrics()
	return fitFrom(columns, 0, columnViewportWidth(total, m, stackWidth), m)
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

// LayoutColumns decides which columns fit at a terminal of total width and how the space left over
// is shared between them. columns are the expanded ones only; stackWidth is what the folded ones
// take at the right edge.
//
// A column is never squeezed below minColumn: the ones that do not fit are reached by scrolling
// sideways, and the window slides just far enough to keep the focused column visible. A terminal
// too narrow for even one readable column gets no columns at all, which the caller turns into a
// message rather than a board with a column cut in half.
func LayoutColumns(columns []Column, focus, total, stackWidth int, d Density) Layout {
	m := d.metrics()
	viewport := columnViewportWidth(total, m, stackWidth)
	if len(columns) == 0 || viewport <= 0 {
		return Layout{Gap: m.gap}
	}
	focus = clampIndex(focus, len(columns))

	start := 0
	var end int
	for {
		end = fitFrom(columns, start, viewport, m)
		if end <= start {
			return Layout{Gap: m.gap}
		}
		if end > focus {
			break
		}
		start++
	}

	widths := make([]int, 0, end-start)
	for i := start; i < end; i++ {
		widths = append(widths, m.minColumn())
	}
	distribute(widths, viewport, m)
	return Layout{Start: start, Widths: widths, Gap: m.gap}
}

// fitFrom returns the exclusive end index of the longest run of columns from start that fits in
// total cells with every one of them at its readable minimum.
func fitFrom(columns []Column, start, total int, m metrics) int {
	end := start
	for used := 0; end < len(columns); end++ {
		need := m.minColumn()
		if end > start {
			need += m.gap
		}
		if used+need > total {
			break
		}
		used += need
	}
	return end
}

// distribute hands the leftover width to the columns, one cell at a time so that the widths stay
// within one cell of each other instead of piling up on the leftmost column.
func distribute(widths []int, total int, m metrics) {
	if len(widths) == 0 {
		return
	}
	used := m.gap * (len(widths) - 1)
	for _, w := range widths {
		used += w
	}
	for spare := total - used; spare > 0; {
		for i := range widths {
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

// listWindow fits count rows into budget lines of a scrolling list. It returns the window to draw
// plus whether an ellipsis line fits above and below it, so a caller can spend its whole budget
// without counting the markers itself.
//
// A window that hides rows spends a line on each side it hides, and which sides those are is only
// known once the window has landed — so the shapes are tried from the roomiest down until one
// whose markers fit is found. When the budget is too small for both a row and its markers, the
// cursor's row wins and the markers are dropped: a list that scrolls out from under the selection
// is worse than one that does not say it was cut.
func listWindow(offset, cursor, count, budget int) (start, visible int, before, after bool) {
	if budget <= 0 || count <= 0 {
		return 0, 0, false, false
	}
	if count <= budget {
		return 0, count, false, false
	}
	for reserve := 0; reserve <= 2; reserve++ {
		visible = budget - reserve
		if visible < 1 {
			break
		}
		start = scrollOffset(offset, cursor, visible, count)
		before, after = start > 0, start+visible < count
		if markerLines(before, after) <= reserve {
			return start, visible, before, after
		}
	}
	return scrollOffset(offset, cursor, budget, count), budget, false, false
}

func markerLines(before, after bool) int {
	lines := 0
	if before {
		lines++
	}
	if after {
		lines++
	}
	return lines
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
