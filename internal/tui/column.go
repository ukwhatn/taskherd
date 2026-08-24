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
	// Collapsed hides this column's cards, leaving only its header.
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
