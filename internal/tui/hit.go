package tui

// cardRegion is the on-screen rectangle one card was drawn in, recorded by renderCards while it
// draws the card rather than recomputed from a click: rebuilding it independently would mean
// reproducing ChooseDensity, LayoutColumns, FitCards and cardHeight a second time, and the two
// copies could drift apart silently the moment only one of them changed.
//
// It carries taskID rather than a column, so the click handler looks the task up in the board's
// current columns instead of trusting where it was drawn: a card can change column between the
// render that placed it and the click reaching Update.
type cardRegion struct {
	taskID     int
	x, y, w, h int
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
