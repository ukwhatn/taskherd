package model

import (
	"fmt"
	"strings"

	"github.com/ukwhatn/taskherd/internal/i18n"
)

// ColumnKind marks how a column behaves. Terminal columns collapse on the board and are hidden from list by default.
type ColumnKind string

const (
	ColumnKindOpen     ColumnKind = "open"
	ColumnKindTerminal ColumnKind = "terminal"
)

// Column is one kanban column, defined in config and referenced by Task.Status.
type Column struct {
	ID    string     `toml:"id" json:"id"`
	Label string     `toml:"label" json:"label"`
	Kind  ColumnKind `toml:"kind" json:"kind"`
	Color string     `toml:"color" json:"color"`
}

// Columns is the ordered column set; the order drives board and list display.
type Columns []Column

// DefaultColumns returns the six columns used when config defines none.
func DefaultColumns() Columns {
	return Columns{
		{ID: "todo", Label: "ToDo", Kind: ColumnKindOpen, Color: "gray"},
		{ID: "planning", Label: "Planning", Kind: ColumnKindOpen, Color: "blue"},
		{ID: "working", Label: "Working", Kind: ColumnKindOpen, Color: "green"},
		{ID: "review", Label: "Review", Kind: ColumnKindOpen, Color: "magenta"},
		{ID: "done", Label: "Done", Kind: ColumnKindTerminal, Color: "purple"},
		{ID: "wontfix", Label: "Wontfix", Kind: ColumnKindTerminal, Color: "gray"},
	}
}

// Validate detects duplicate ids, empty definitions and unknown kinds.
func (cs Columns) Validate() error {
	var violations []Violation
	add := func(path string, code i18n.ViolationCode, args ...any) {
		violations = append(violations, Violation{Path: path, Code: code, Args: args})
	}

	if len(cs) == 0 {
		add("columns", i18n.ViolationColumnsEmpty)
	}

	seen := make(map[string]int, len(cs))
	for i, col := range cs {
		path := fmt.Sprintf("columns[%d]", i)
		switch prev, dup := seen[col.ID]; {
		case strings.TrimSpace(col.ID) == "":
			add(path+".id", i18n.ViolationColumnIDEmpty)
		case dup:
			add(path+".id", i18n.ViolationColumnIDDuplicate, col.ID, prev)
		default:
			seen[col.ID] = i
		}
		if strings.TrimSpace(col.Label) == "" {
			add(path+".label", i18n.ViolationColumnLabelEmpty)
		}
		if col.Kind != ColumnKindOpen && col.Kind != ColumnKindTerminal {
			add(path+".kind", i18n.ViolationColumnKindInvalid, ColumnKindOpen, ColumnKindTerminal, col.Kind)
		}
	}

	if len(violations) > 0 {
		return &ValidationError{Violations: violations}
	}
	return nil
}

// Find returns the column with the given id.
func (cs Columns) Find(id string) (Column, bool) {
	for _, col := range cs {
		if col.ID == id {
			return col, true
		}
	}
	return Column{}, false
}

// Index returns the display position of id, or -1 for an undefined column.
func (cs Columns) Index(id string) int {
	for i, col := range cs {
		if col.ID == id {
			return i
		}
	}
	return -1
}

// IDs returns the column ids in definition order.
func (cs Columns) IDs() []string {
	ids := make([]string, 0, len(cs))
	for _, col := range cs {
		ids = append(ids, col.ID)
	}
	return ids
}
