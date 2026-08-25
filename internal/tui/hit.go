package tui

// cardRegion is the on-screen rectangle one card was drawn in, recorded by renderCards while it
// draws the card rather than recomputed from a click: rebuilding it independently would mean
// reproducing ChooseDensity, LayoutColumns, FitCards and cardHeight a second time, and the two
// copies could drift apart silently the moment only one of them changed.
//
// columnIndex is the column's position among every column the board holds (the same coordinate
// space as Board.colIdx), not just the visible ones: a click has to move the cursor there, and the
// cursor is addressed board-wide.
type cardRegion struct {
	columnIndex int
	taskID      int
	x, y, w, h  int
}

// contains reports whether the screen cell (x, y) falls inside the region.
func (r cardRegion) contains(x, y int) bool {
	return x >= r.x && x < r.x+r.w && y >= r.y && y < r.y+r.h
}

// hitCard finds the region a screen cell lands in, if any.
func hitCard(regions []cardRegion, x, y int) (cardRegion, bool) {
	for _, r := range regions {
		if r.contains(x, y) {
			return r, true
		}
	}
	return cardRegion{}, false
}
