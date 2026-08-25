package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ukwhatn/taskherd/internal/model"
)

func TestCardRegionContains(t *testing.T) {
	r := cardRegion{x: 10, y: 4, w: 6, h: 3}

	inside := []struct{ x, y int }{{10, 4}, {15, 6}, {12, 5}}
	for _, p := range inside {
		if !r.contains(p.x, p.y) {
			t.Errorf("contains(%d,%d) = false, want true", p.x, p.y)
		}
	}
	// The far edges are exclusive: x+w and y+h are the first cell past the card.
	outside := []struct{ x, y int }{{9, 4}, {16, 4}, {10, 3}, {10, 7}, {16, 7}}
	for _, p := range outside {
		if r.contains(p.x, p.y) {
			t.Errorf("contains(%d,%d) = true, want false", p.x, p.y)
		}
	}
}

func TestHitCard(t *testing.T) {
	regions := []cardRegion{
		{taskID: 1, x: 0, y: 0, w: 5, h: 2},
		{taskID: 2, x: 10, y: 0, w: 5, h: 2},
	}

	if got, ok := hitCard(regions, 2, 1); !ok || got.taskID != 1 {
		t.Errorf("hitCard(2,1) = (%+v, %v), want #1", got, ok)
	}
	if got, ok := hitCard(regions, 12, 1); !ok || got.taskID != 2 {
		t.Errorf("hitCard(12,1) = (%+v, %v), want #2", got, ok)
	}
	if _, ok := hitCard(regions, 6, 0); ok {
		t.Error("列の間の隙間がヒットした")
	}
	if _, ok := hitCard(nil, 0, 0); ok {
		t.Error("矩形が無いのにヒットした")
	}
}

// assertRegionMatchesScreen checks a recorded region against the actual rendered screen, using the
// region's own x/y/w/h to slice it rather than an independently computed position (that recomputed
// position is exactly what could drift from the renderer's own arithmetic and still show green).
//
// It also finds the longest run of renderCard()'s own lines that the screen still carries at that
// position, from the top, and requires region.h to equal that run exactly: a region cut a line
// short of what is actually on screen would pass a "does it match" check but fail this one, and so
// would a region reaching a line further than the screen still agrees with it.
func assertRegionMatchesScreen(t *testing.T, h *harness, screen string, region cardRegion) {
	t.Helper()
	rows := strings.Split(screen, "\n")

	colIdx, rowIdx, ok := findTask(h.board.columns, region.taskID)
	if !ok {
		t.Fatalf("region の taskID #%d がどの列にも見つからない", region.taskID)
	}
	col := h.board.columns[colIdx]
	task := col.Tasks[rowIdx]

	// Density and metrics are shared, already independently tested pure functions
	// (ChooseDensity/layout_test.go) — reusing them is not the position arithmetic the docstring
	// above warns against, which is specifically the boardPad+widths+gap accumulation that placed
	// the region in the first place.
	expanded, _ := expandedColumns(h.board.columns)
	stackWidth := collapsedStackWidth(collapsedColumns(h.board.columns))
	m := ChooseDensity(expanded, h.board.width, stackWidth).metrics()

	card := BuildCard(task, BuildSessionBadge(task, h.board.sessions, h.board.icons), h.board.links, h.board.cardStyle(), h.board.deps.now())
	focused := colIdx == h.board.colIdx && rowIdx == h.board.selectedIndex(col)
	want := strings.Split(stripANSI(h.board.renderCard(card, col, region.w, focused, m)), "\n")

	visible := 0
	for visible < len(want) && region.y+visible < len(rows) {
		got := sliceCells(stripANSI(rows[region.y+visible]), region.x, region.w)
		if got != want[visible] {
			break
		}
		visible++
	}
	if region.h != visible {
		t.Errorf("#%d 矩形の高さ = %d, want %d（画面に残っている renderCard 行数）\nrenderCard:\n%s",
			region.taskID, region.h, visible, strings.Join(want, "\n"))
	}
}

// sliceCells returns the width cells starting at x. Sliced by rune rather than by byte: the box
// border every card draws (╭─│╰╯) is multi-byte UTF-8 even though the fixtures in this file keep
// their titles ASCII-only, and every rune the board draws — border or ASCII title alike — is one
// display cell wide, so a rune offset lines up with a cell offset where a byte offset would not.
func sliceCells(row string, x, width int) string {
	runes := []rune(row)
	if x < 0 || x >= len(runes) {
		return ""
	}
	end := x + width
	if end > len(runes) {
		end = len(runes)
	}
	return string(runes[x:end])
}

func asciiTasks(n int, status string) []model.Task {
	tasks := make([]model.Task, 0, n)
	for i := 1; i <= n; i++ {
		tasks = append(tasks, model.Task{ID: i, Title: fmt.Sprintf("card number %d", i), Status: status})
	}
	return tasks
}

func TestCardRegionsMatchRenderedScreenSingleColumn(t *testing.T) {
	h := newHarness(t, Deps{Tasks: newFakeStore(asciiTasks(3, "todo")...)}, Settings{})
	h.board.width, h.board.height = 80, 24

	screen := h.board.render()
	if len(h.board.cardRegions) != 3 {
		t.Fatalf("cardRegions = %+v, want 3 件", h.board.cardRegions)
	}
	for _, region := range h.board.cardRegions {
		assertRegionMatchesScreen(t, h, screen, region)
	}
}

// A second column, one of them focused, checks that a region's task resolves to the right column
// board-wide (findTask, the same lookup a click resolves through) and that only the truly focused
// card's region is built from a focused render.
func TestCardRegionsMatchAcrossColumnsWithFocus(t *testing.T) {
	tasks := append(asciiTasks(2, "todo"), model.Task{ID: 3, Title: "working card", Status: "working"})
	h := newHarness(t, Deps{Tasks: newFakeStore(tasks...)}, Settings{})
	h.board.width, h.board.height = 80, 24
	h.key("down") // select task #2 within the todo column

	screen := h.board.render()
	if len(h.board.cardRegions) != 3 {
		t.Fatalf("cardRegions = %+v, want 3 件", h.board.cardRegions)
	}
	for _, region := range h.board.cardRegions {
		assertRegionMatchesScreen(t, h, screen, region)
	}

	if _, ok := findRegion(h.board.cardRegions, 3); !ok {
		t.Fatal("working 列のカードの矩形が無い")
	}
	colIdx, _, ok := findTask(h.board.columns, 3)
	if !ok || h.board.columns[colIdx].ID != "working" {
		t.Errorf("findTask(3) の列 = %d, want working 列", colIdx)
	}
}

// Folded columns draw their row in the collapsed stack, not through renderCards, so they hold no
// card of their own to record a region for.
func TestCardRegionsExcludeCollapsedColumns(t *testing.T) {
	tasks := []model.Task{task(1, "todo"), task(2, "done")}
	h := newHarness(t, Deps{Tasks: newFakeStore(tasks...)}, Settings{})
	h.board.width, h.board.height = 80, 24

	h.board.render()
	if _, ok := findRegion(h.board.cardRegions, 2); ok {
		t.Error("折り畳まれた列のカードに矩形が記録されている")
	}
	if _, ok := findRegion(h.board.cardRegions, 1); !ok {
		t.Error("展開されている列のカードに矩形が無い")
	}
}

// Enough cards to scroll: the overflow indicators take a line each and are not cards, so they must
// not appear as regions, and the cards actually drawn still have to check out.
func TestCardRegionsWithScrollOffset(t *testing.T) {
	h := newHarness(t, Deps{Tasks: newFakeStore(asciiTasks(10, "todo")...)}, Settings{})
	h.board.width, h.board.height = 80, 16
	for i := 0; i < 9; i++ {
		h.key("down")
	}

	screen := h.board.render()
	if len(h.board.cardRegions) == 0 {
		t.Fatal("スクロール後にカードの矩形が無い")
	}
	for _, region := range h.board.cardRegions {
		if region.taskID < 1 || region.taskID > 10 {
			t.Fatalf("矩形が overflow インジケータの行を指している可能性がある: %+v", region)
		}
		assertRegionMatchesScreen(t, h, screen, region)
	}
}

// A region must not reach into the sideways-scroll notice's row (§2.7).
func TestCardRegionsWithSidewaysScrollNotice(t *testing.T) {
	tasks := []model.Task{task(1, "todo"), task(2, "working")}
	h := newHarness(t, Deps{Tasks: newFakeStore(tasks...)}, Settings{})
	h.board.width, h.board.height = 40, 20

	screen := h.board.render()
	if !strings.Contains(stripANSI(screen), "で移動") {
		t.Fatalf("横スクロールの notice が出ていない前提が崩れている:\n%s", screen)
	}
	if len(h.board.cardRegions) != 1 {
		t.Fatalf("cardRegions = %+v, want 表示中の 1 列ぶんのみ", h.board.cardRegions)
	}
	assertRegionMatchesScreen(t, h, screen, h.board.cardRegions[0])
}

// A region must shrink to the card's clipped height (§2.6), not its full unclipped one.
func TestCardRegionsClippedWhenCardOverflowsColumn(t *testing.T) {
	links := make([]model.Link, 8)
	for i := range links {
		links[i] = model.Link{URL: fmt.Sprintf("https://example.com/x%d", i), Kind: model.LinkKindOther}
	}
	store := newFakeStore(model.Task{ID: 1, Title: "card with many links", Status: "todo", Links: links})
	h := newHarness(t, Deps{Tasks: store}, Settings{})
	h.board.width, h.board.height = 80, 10 // short enough that #1's card cannot fit its own column

	screen := h.board.render()
	if len(h.board.cardRegions) != 1 {
		t.Fatalf("cardRegions = %+v, want 1 件", h.board.cardRegions)
	}
	region := h.board.cardRegions[0]

	m := ChooseDensity(mustExpanded(h), h.board.width, 0).metrics()
	card := BuildCard(h.board.file.Tasks[0], SessionBadge{}, nil, h.board.cardStyle(), h.board.deps.now())
	full := cardHeight(card, region.w, m)
	if region.h >= full {
		t.Fatalf("この検証はカードが切られる場合に限る（region.h=%d, full=%d）", region.h, full)
	}
	assertRegionMatchesScreen(t, h, screen, region)
}

// §2.7 combined with §2.6's clip: both the notice and a correctly clipped region must survive
// together.
func TestCardRegionsClippedWithNoticePresent(t *testing.T) {
	links := make([]model.Link, 6)
	for i := range links {
		links[i] = model.Link{URL: fmt.Sprintf("https://example.com/y%d", i), Kind: model.LinkKindOther}
	}
	tasks := []model.Task{
		{ID: 1, Title: "tall card forcing a clip", Status: "todo", Links: links},
		task(2, "working"),
	}
	h := newHarness(t, Deps{Tasks: newFakeStore(tasks...)}, Settings{})
	h.board.width, h.board.height = 40, 10

	screen := h.board.render()
	if !strings.Contains(stripANSI(screen), "で移動") {
		t.Fatalf("notice が出ていない前提が崩れている:\n%s", screen)
	}
	if len(h.board.cardRegions) != 1 {
		t.Fatalf("cardRegions = %+v, want 1 件", h.board.cardRegions)
	}
	assertRegionMatchesScreen(t, h, screen, h.board.cardRegions[0])
}

// renderColumns clears cardRegions at its own top (§4.2) rather than only ever appending to it, so
// a board that had regions from an earlier render must not still report them once its columns and
// tasks are both gone: a fresh Board's cardRegions being nil would pass this even without that
// reset line, since it never held anything to clear in the first place.
func TestCardRegionsResetWhenColumnsBecomeEmpty(t *testing.T) {
	h := newHarness(t, Deps{Tasks: newFakeStore(task(1, "todo"))}, Settings{})
	h.board.width, h.board.height = 80, 24

	h.board.render()
	if len(h.board.cardRegions) == 0 {
		t.Fatal("前提: 列がある状態でカードの矩形が記録されていること")
	}

	h.board.settings.Columns = model.Columns{}
	h.board.file.Tasks = nil
	h.board.rebuild()
	h.board.render()

	if len(h.board.cardRegions) != 0 {
		t.Errorf("cardRegions = %+v, want 空（列が定義されなくなった後の再描画）", h.board.cardRegions)
	}
	if _, ok := hitCard(h.board.cardRegions, 10, 10); ok {
		t.Error("列が無いのにヒットした")
	}
}

func findRegion(regions []cardRegion, taskID int) (cardRegion, bool) {
	for _, r := range regions {
		if r.taskID == taskID {
			return r, true
		}
	}
	return cardRegion{}, false
}

func mustExpanded(h *harness) []Column {
	expanded, _ := expandedColumns(h.board.columns)
	return expanded
}
