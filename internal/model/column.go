package model

import (
	"fmt"
	"strings"
)

// ColumnKind は列の性質。terminal 列は board で折り畳み、list の既定表示から除く。
type ColumnKind string

const (
	ColumnKindOpen     ColumnKind = "open"
	ColumnKindTerminal ColumnKind = "terminal"
)

// Column は kanban の 1 列。config で定義され、Task.Status がこの ID を指す。
type Column struct {
	ID    string     `toml:"id" json:"id"`
	Label string     `toml:"label" json:"label"`
	Kind  ColumnKind `toml:"kind" json:"kind"`
	Color string     `toml:"color" json:"color"`
}

// Columns は列の定義列。並び順が board / list の表示順になる。
type Columns []Column

// DefaultColumns は config 未設定時に使う既定 6 列。
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

// Validate は列定義の重複・空定義・未知の kind を検出する。
func (cs Columns) Validate() error {
	var violations []Violation
	add := func(path, format string, args ...any) {
		violations = append(violations, Violation{Path: path, Message: fmt.Sprintf(format, args...)})
	}

	if len(cs) == 0 {
		add("columns", "列が 1 つも定義されていない")
	}

	seen := make(map[string]int, len(cs))
	for i, col := range cs {
		path := fmt.Sprintf("columns[%d]", i)
		switch prev, dup := seen[col.ID]; {
		case strings.TrimSpace(col.ID) == "":
			add(path+".id", "id が空")
		case dup:
			add(path+".id", "id %q が columns[%d] と重複している", col.ID, prev)
		default:
			seen[col.ID] = i
		}
		if strings.TrimSpace(col.Label) == "" {
			add(path+".label", "label が空")
		}
		if col.Kind != ColumnKindOpen && col.Kind != ColumnKindTerminal {
			add(path+".kind", "kind は %q か %q（実際: %q）", ColumnKindOpen, ColumnKindTerminal, col.Kind)
		}
	}

	if len(violations) > 0 {
		return &ValidationError{Violations: violations}
	}
	return nil
}

// Find は id に一致する列を返す。
func (cs Columns) Find(id string) (Column, bool) {
	for _, col := range cs {
		if col.ID == id {
			return col, true
		}
	}
	return Column{}, false
}

// Index は id の並び順を返す。未定義の列 id は -1（board 末尾の (unknown) 列相当）。
func (cs Columns) Index(id string) int {
	for i, col := range cs {
		if col.ID == id {
			return i
		}
	}
	return -1
}

// IDs は定義順の列 id を返す。
func (cs Columns) IDs() []string {
	ids := make([]string, 0, len(cs))
	for _, col := range cs {
		ids = append(ids, col.ID)
	}
	return ids
}

// OpenIDs は kind=open の列 id を定義順で返す（list の既定フィルタ）。
func (cs Columns) OpenIDs() []string {
	ids := make([]string, 0, len(cs))
	for _, col := range cs {
		if col.Kind == ColumnKindOpen {
			ids = append(ids, col.ID)
		}
	}
	return ids
}
