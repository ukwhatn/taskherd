package model_test

import (
	"errors"
	"testing"

	"github.com/ukwhatn/taskherd/internal/model"
)

func TestDefaultColumns(t *testing.T) {
	cols := model.DefaultColumns()

	wantIDs := []string{"todo", "planning", "working", "review", "done", "wontfix"}
	if len(cols) != len(wantIDs) {
		t.Fatalf("既定列数 = %d, want %d", len(cols), len(wantIDs))
	}
	for i, id := range wantIDs {
		if cols[i].ID != id {
			t.Errorf("columns[%d].ID = %q, want %q", i, cols[i].ID, id)
		}
		if cols[i].Label == "" || cols[i].Color == "" {
			t.Errorf("columns[%d] に label/color が無い: %+v", i, cols[i])
		}
	}
	if err := cols.Validate(); err != nil {
		t.Fatalf("既定列が検証を通らない: %v", err)
	}

	terminal := map[string]bool{"done": true, "wontfix": true}
	for _, col := range cols {
		wantKind := model.ColumnKindOpen
		if terminal[col.ID] {
			wantKind = model.ColumnKindTerminal
		}
		if col.Kind != wantKind {
			t.Errorf("%s の kind = %q, want %q", col.ID, col.Kind, wantKind)
		}
	}
}

func TestColumnsValidate(t *testing.T) {
	tests := []struct {
		name      string
		columns   model.Columns
		wantPaths []string
	}{
		{
			name:      "列が空",
			columns:   model.Columns{},
			wantPaths: []string{"columns"},
		},
		{
			name:      "id が空",
			columns:   model.Columns{{ID: "", Label: "ToDo", Kind: model.ColumnKindOpen}},
			wantPaths: []string{"columns[0].id"},
		},
		{
			name: "id が重複",
			columns: model.Columns{
				{ID: "todo", Label: "ToDo", Kind: model.ColumnKindOpen},
				{ID: "todo", Label: "やること", Kind: model.ColumnKindOpen},
			},
			wantPaths: []string{"columns[1].id"},
		},
		{
			name:      "label が空",
			columns:   model.Columns{{ID: "todo", Label: "", Kind: model.ColumnKindOpen}},
			wantPaths: []string{"columns[0].label"},
		},
		{
			name:      "kind が未知の値",
			columns:   model.Columns{{ID: "todo", Label: "ToDo", Kind: "opened"}},
			wantPaths: []string{"columns[0].kind"},
		},
		{
			name:      "kind が空",
			columns:   model.Columns{{ID: "todo", Label: "ToDo", Kind: ""}},
			wantPaths: []string{"columns[0].kind"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.columns.Validate()

			var invalid *model.ValidationError
			if !errors.As(err, &invalid) {
				t.Fatalf("Validate() error = %v, want *ValidationError", err)
			}
			if len(invalid.Violations) != len(tt.wantPaths) {
				t.Fatalf("違反 = %v, want %v", invalid.Violations, tt.wantPaths)
			}
			for i, want := range tt.wantPaths {
				if invalid.Violations[i].Path != want {
					t.Errorf("Violations[%d].Path = %q, want %q", i, invalid.Violations[i].Path, want)
				}
			}
		})
	}
}

func TestColumnsFindAndIndex(t *testing.T) {
	cols := model.DefaultColumns()

	col, ok := cols.Find("working")
	if !ok || col.Label != "Working" {
		t.Errorf("Find(working) = %+v, %v", col, ok)
	}
	if _, ok := cols.Find("消えた列"); ok {
		t.Error("Find(未知) が見つかったと報告した")
	}

	if got := cols.Index("todo"); got != 0 {
		t.Errorf("Index(todo) = %d, want 0", got)
	}
	if got := cols.Index("wontfix"); got != 5 {
		t.Errorf("Index(wontfix) = %d, want 5", got)
	}
	if got := cols.Index("消えた列"); got != -1 {
		t.Errorf("Index(未知) = %d, want -1", got)
	}
}
