package tui

import (
	"testing"

	"github.com/ukwhatn/taskherd/internal/model"
)

func testColumns() model.Columns {
	return model.Columns{
		{ID: "todo", Label: "ToDo", Kind: model.ColumnKindOpen},
		{ID: "done", Label: "Done", Kind: model.ColumnKindTerminal},
		{ID: "working", Label: "Working", Kind: model.ColumnKindOpen},
	}
}

func task(id int, status string) model.Task {
	return model.Task{ID: id, Title: "t", Status: status}
}

func columnIDs(columns []Column) []string {
	ids := make([]string, len(columns))
	for i, col := range columns {
		ids[i] = col.ID
	}
	return ids
}

// Terminal columns sit on the right whatever their position in config, because that is where
// they collapse to; a collapsed column in the middle would split the open ones.
func TestBuildColumnsPutsTerminalColumnsLast(t *testing.T) {
	columns := BuildColumns(nil, testColumns(), true)

	got := columnIDs(columns)
	want := []string{"todo", "working", "done"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("列順 = %v, want %v", got, want)
		}
	}
}

func TestBuildColumnsCollapsesTerminalOnlyWhenAsked(t *testing.T) {
	collapsed := BuildColumns(nil, testColumns(), true)
	if !collapsed[2].Collapsed || collapsed[0].Collapsed {
		t.Errorf("collapsed = %v/%v, want terminal のみ true", collapsed[2].Collapsed, collapsed[0].Collapsed)
	}

	expanded := BuildColumns(nil, testColumns(), false)
	if expanded[2].Collapsed {
		t.Error("t で展開したのに terminal 列が折り畳まれている")
	}
}

func TestBuildColumnsGroupsTasksByStatus(t *testing.T) {
	columns := BuildColumns([]model.Task{
		task(3, "working"),
		task(1, "todo"),
		task(2, "working"),
	}, testColumns(), true)

	if len(columns[0].Tasks) != 1 || columns[0].Tasks[0].ID != 1 {
		t.Errorf("todo = %+v, want #1 のみ", columns[0].Tasks)
	}
	// Cards are ordered by id so the board does not reshuffle when tasks.json is rewritten.
	if len(columns[1].Tasks) != 2 || columns[1].Tasks[0].ID != 2 || columns[1].Tasks[1].ID != 3 {
		t.Errorf("working = %+v, want #2,#3 の順", columns[1].Tasks)
	}
}

func TestBuildColumnsCollectsUnknownStatusLast(t *testing.T) {
	columns := BuildColumns([]model.Task{
		task(1, "todo"),
		task(2, "retired"),
	}, testColumns(), true)

	last := columns[len(columns)-1]
	if !last.Unknown {
		t.Fatalf("末尾列 = %+v, want (unknown)", last)
	}
	if last.Label != unknownColumnLabel {
		t.Errorf("Label = %q, want %q", last.Label, unknownColumnLabel)
	}
	if len(last.Tasks) != 1 || last.Tasks[0].ID != 2 {
		t.Errorf("Tasks = %+v, want #2", last.Tasks)
	}
}

// The synthetic column is only worth a slot when something is actually in it.
func TestBuildColumnsOmitsUnknownWhenEmpty(t *testing.T) {
	columns := BuildColumns([]model.Task{task(1, "todo")}, testColumns(), true)

	for _, col := range columns {
		if col.Unknown {
			t.Fatalf("(unknown) 列が出ている: %+v", columns)
		}
	}
}

func TestColumnKeyDistinguishesUnknown(t *testing.T) {
	columns := BuildColumns([]model.Task{task(1, "retired")}, testColumns(), true)

	if key := columns[len(columns)-1].Key(); key != unknownColumnKey {
		t.Errorf("Key = %q, want %q", key, unknownColumnKey)
	}
}

// A task cannot be moved *into* (unknown): it is the absence of a status, not one you can set.
func TestMoveTargetSkipsUnknownColumn(t *testing.T) {
	columns := BuildColumns([]model.Task{task(1, "retired")}, testColumns(), true)

	if _, ok := moveTarget(columns, 2, 1); ok {
		t.Error("(unknown) 列が移動先になった")
	}
	target, ok := moveTarget(columns, 3, -1)
	if !ok || target.ID != "done" {
		t.Errorf("target = %+v ok=%v, want done", target, ok)
	}
}

func TestMoveTargetStopsAtEdges(t *testing.T) {
	columns := BuildColumns(nil, testColumns(), true)

	if _, ok := moveTarget(columns, 0, -1); ok {
		t.Error("左端から更に左へ移動できた")
	}
	if _, ok := moveTarget(columns, len(columns)-1, 1); ok {
		t.Error("右端から更に右へ移動できた")
	}
}

func TestFindTask(t *testing.T) {
	columns := BuildColumns([]model.Task{task(1, "todo"), task(5, "working")}, testColumns(), true)

	col, row, ok := findTask(columns, 5)
	if !ok || col != 1 || row != 0 {
		t.Errorf("findTask = (%d,%d,%v), want (1,0,true)", col, row, ok)
	}
	if _, _, ok := findTask(columns, 99); ok {
		t.Error("存在しない id が見つかった")
	}
}
