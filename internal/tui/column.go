// Package tui renders the taskherd kanban board.
//
// The board's model layer (column layout, badge folding, card text) is kept free of bubbletea
// so it can be tested as plain functions; board.go holds the Update/View shell that drives it.
package tui

import (
	"sort"

	"github.com/ukwhatn/taskherd/internal/model"
)

// unknownColumnKey identifies the synthetic column in per-column state maps. It uses a NUL
// byte so it can never collide with a config column id, which is validated as non-empty text.
const unknownColumnKey = "\x00unknown"

// unknownColumnLabel is what the synthetic column is called on screen.
const unknownColumnLabel = "(unknown)"

// Column is one rendered board column: either a column from config, or the synthetic
// (unknown) column collecting tasks whose status no longer exists in config.
type Column struct {
	ID    string
	Label string
	Color string
	// Terminal marks a column that is collapsed by default and hidden from `list`.
	Terminal bool
	// Unknown marks the synthetic column. Its tasks need a `move` to a real column.
	Unknown bool
	// Collapsed folds this column into the stack at the board's right edge, where it shows as a
	// label and a count and is skipped by the cursor.
	Collapsed bool
	Tasks     []model.Task
}

// Key returns the stable identity used to remember per-column selection across rebuilds.
func (c Column) Key() string {
	if c.Unknown {
		return unknownColumnKey
	}
	return c.ID
}

// BuildColumns lays out the board: open columns in config order, then the terminal columns on
// the right, then the (unknown) column when some task still points at a deleted column.
//
// Terminal columns are pushed right rather than left in place because that is where they are
// collapsed to, and a collapsed column in the middle of the board would split the open ones.
func BuildColumns(tasks []model.Task, columns model.Columns, collapseTerminal bool) []Column {
	byStatus := make(map[string][]model.Task, len(columns))
	var unknown []model.Task
	for _, task := range tasks {
		if _, ok := columns.Find(task.Status); ok {
			byStatus[task.Status] = append(byStatus[task.Status], task)
			continue
		}
		unknown = append(unknown, task)
	}

	built := make([]Column, 0, len(columns)+1)
	appendKind := func(kind model.ColumnKind) {
		for _, col := range columns {
			if col.Kind != kind {
				continue
			}
			built = append(built, Column{
				ID:        col.ID,
				Label:     col.Label,
				Color:     col.Color,
				Terminal:  kind == model.ColumnKindTerminal,
				Collapsed: kind == model.ColumnKindTerminal && collapseTerminal,
				Tasks:     sortByID(byStatus[col.ID]),
			})
		}
	}
	appendKind(model.ColumnKindOpen)
	appendKind(model.ColumnKindTerminal)

	if len(unknown) > 0 {
		built = append(built, Column{
			ID:      unknownColumnLabel,
			Label:   unknownColumnLabel,
			Unknown: true,
			Tasks:   sortByID(unknown),
		})
	}
	return built
}

func sortByID(tasks []model.Task) []model.Task {
	sorted := make([]model.Task, len(tasks))
	copy(sorted, tasks)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })
	return sorted
}

// expandedColumns are the columns that get a column of their own on the board, each paired with
// its index in the whole set.
//
// The board's focus is an index into all the columns, folded ones included, while the layout only
// ever sees the expanded ones. The pairing is what translates between the two, and keeping it here
// rather than in the layout leaves the layout a pure width calculation.
func expandedColumns(columns []Column) (cols []Column, index []int) {
	for i, col := range columns {
		if col.Collapsed {
			continue
		}
		cols = append(cols, col)
		index = append(index, i)
	}
	return cols, index
}

// collapsedColumns are the folded ones, in board order.
func collapsedColumns(columns []Column) []Column {
	var cols []Column
	for _, col := range columns {
		if col.Collapsed {
			cols = append(cols, col)
		}
	}
	return cols
}

// positionOf finds where a board index sits in an expandedColumns index, and falls back to the
// first column when it is not among them.
func positionOf(index []int, boardIdx int) int {
	for i, idx := range index {
		if idx == boardIdx {
			return i
		}
	}
	return 0
}

// nearestExpanded is the expanded column closest to idx, which is where the cursor goes when the
// column it was on folds away under it.
//
// A tie goes to the left, so that the cursor ends up on the open columns rather than on the
// synthetic (unknown) one that sits past the folded ones.
func nearestExpanded(columns []Column, idx int) int {
	if len(columns) == 0 {
		return 0
	}
	idx = clampIndex(idx, len(columns))
	if !columns[idx].Collapsed {
		return idx
	}
	for d := 1; d < len(columns); d++ {
		if i := idx - d; i >= 0 && !columns[i].Collapsed {
			return i
		}
		if i := idx + d; i < len(columns) && !columns[i].Collapsed {
			return i
		}
	}
	return idx
}

// findTask locates a task by id across the built columns.
func findTask(columns []Column, id int) (col, row int, ok bool) {
	for c := range columns {
		for r := range columns[c].Tasks {
			if columns[c].Tasks[r].ID == id {
				return c, r, true
			}
		}
	}
	return 0, 0, false
}

// moveTarget returns the column a card moves into when shifted by delta.
//
// The (unknown) column is a valid source but never a destination: it is not a status a task can
// hold, only the absence of one. Collapsed terminal columns stay reachable, since moving a task
// to Done is the most common move on the board.
func moveTarget(columns []Column, from, delta int) (Column, bool) {
	for i := from + delta; i >= 0 && i < len(columns); i += delta {
		if columns[i].Unknown {
			continue
		}
		return columns[i], true
	}
	return Column{}, false
}
