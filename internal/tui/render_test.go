package tui

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/ukwhatn/taskherd/internal/model"
)

// boardWithCards fills the todo column with n tasks, each carrying a due date so every card has a
// meta line to draw.
func boardWithCards(t *testing.T, n int) *harness {
	t.Helper()
	tasks := make([]model.Task, 0, n)
	for i := 1; i <= n; i++ {
		tasks = append(tasks, model.Task{
			ID: i, Title: fmt.Sprintf("タスク %d の設計と実装", i), Status: "todo", Due: due("2026-08-30"),
		})
	}
	return newHarness(t, Deps{Tasks: newFakeStore(tasks...)}, Settings{})
}

// Nothing the board draws may push a row past the terminal, at any width it degrades through.
func TestBoardRowsFitTerminalWidth(t *testing.T) {
	h := boardWithCards(t, 12)

	for _, width := range []int{60, 80, 100, 120, 200} {
		t.Run(fmt.Sprintf("width%d", width), func(t *testing.T) {
			h.board.width, h.board.height = width, 24
			for _, line := range strings.Split(h.board.render(), "\n") {
				if got := lipgloss.Width(line); got > width {
					t.Fatalf("行幅 = %d, want <= %d: %q", got, width, line)
				}
			}
		})
	}
}

// A column with more cards than it has room for says so at both ends rather than cutting cards
// off in silence, and the cursor stays on a card that is actually drawn.
func TestColumnScrollsWithOverflowIndicators(t *testing.T) {
	h := boardWithCards(t, 10)
	h.board.height = 20

	view := h.board.render()
	if !strings.Contains(view, "↓ 7件") {
		t.Fatalf("下方向のオーバーフローインジケータが無い:\n%s", view)
	}
	if strings.Contains(view, "↑ ") {
		t.Errorf("先頭にいるのに上方向のインジケータが出ている:\n%s", view)
	}
	if !strings.Contains(view, "#1 タスク 1") {
		t.Errorf("先頭カードが描画されていない:\n%s", view)
	}

	for i := 0; i < 9; i++ {
		h.key("down")
	}
	view = h.board.render()
	if !strings.Contains(view, "↑ 7件") {
		t.Errorf("末尾まで送ったのに上方向のインジケータが無い:\n%s", view)
	}
	if !strings.Contains(view, "#10 タスク 10") {
		t.Errorf("選択中のカードが描画されていない:\n%s", view)
	}
}

// The scroll window is per column, so moving sideways does not carry one column's offset onto
// another.
func TestColumnScrollIsPerColumn(t *testing.T) {
	store := newFakeStore(
		model.Task{ID: 1, Title: "先頭", Status: "todo"},
		model.Task{ID: 2, Title: "二番目", Status: "todo"},
		model.Task{ID: 3, Title: "働く", Status: "working"},
	)
	h := newHarness(t, Deps{Tasks: store}, Settings{})
	h.board.height = 14

	h.key("down")
	h.key("right")
	h.key("right")

	if got := h.board.offsets["working"]; got != 0 {
		t.Errorf("working 列の offset = %d, want 0", got)
	}
}

// A card is a box: that is what the board's visual language rests on, so it is worth pinning.
func TestCardIsDrawnAsBox(t *testing.T) {
	h := boardWithCards(t, 1)

	view := h.board.render()
	for _, want := range []string{"╭", "╰", "│"} {
		if !strings.Contains(view, want) {
			t.Errorf("カードのボーダー %q が描画されていない:\n%s", want, view)
		}
	}
}

// Every line of a card box has to be exactly as wide as the column, or the board's borders go
// ragged the moment a title contains full-width characters.
func TestRenderCardKeepsBorderAligned(t *testing.T) {
	h := newHarness(t, Deps{Tasks: newFakeStore()}, Settings{})
	titles := []string{
		"全角のタイトルを長めに入力しておく設計実装レビュー",
		"ascii title that is long enough to be cut",
		"混在した title と日本語",
	}

	for _, density := range []Density{DensityRoomy, DensityTight, DensityCompact} {
		for _, title := range titles {
			for width := 15; width <= 40; width++ {
				card := BuildCard(
					model.Task{ID: 12, Title: title, Due: due("2026-08-30")},
					SessionBadge{}, nil, boardNow)
				got := h.board.renderCard(card, Column{Color: "green"}, width, true, density.metrics())

				for _, line := range strings.Split(got, "\n") {
					if w := lipgloss.Width(line); w != width {
						t.Fatalf("density=%v width=%d: 行幅 = %d: %q", density, width, w, line)
					}
				}
				if lines := strings.Count(got, "\n") + 1; lines != cardHeight(card, density.metrics()) {
					t.Fatalf("density=%v: 行数 = %d, want %d", density, lines, cardHeight(card, density.metrics()))
				}
			}
		}
	}
}

// A dialog is a titled box laid over the board, and the board stays readable around it.
func TestModalIsBoxOverTheBoard(t *testing.T) {
	h := boardWithCards(t, 3)

	h.key("delete")
	view := h.board.render()

	if !strings.Contains(view, "確認") {
		t.Errorf("確認ダイアログのタイトルが無い:\n%s", view)
	}
	if !strings.Contains(view, "y で実行 / n で中止") {
		t.Errorf("確認ダイアログのキーヘルプが無い:\n%s", view)
	}
	if !strings.Contains(view, "ToDo (3)") {
		t.Errorf("ダイアログの背後に盤面が残っていない:\n%s", view)
	}
	if !strings.Contains(view, boardHelp[:12]) {
		t.Errorf("ダイアログの背後にフッタが残っていない:\n%s", view)
	}
}

func TestModalIsCentred(t *testing.T) {
	h := boardWithCards(t, 3)
	h.key("delete")

	rows := strings.Split(h.board.render(), "\n")
	top := -1
	for i, row := range rows {
		if strings.Contains(row, "確認") {
			top = i
			break
		}
	}
	if top < 0 {
		t.Fatalf("ダイアログが見つからない")
	}
	if top < 2 || top > h.board.height-4 {
		t.Errorf("ダイアログの上端 = %d, want 画面中央付近（高さ %d）", top, h.board.height)
	}
	if !strings.HasPrefix(rows[top], "  ") {
		t.Errorf("ダイアログが左端に貼り付いている: %q", rows[top])
	}
}

// A picker opened from the detail modal owns the accent border, and the detail box behind it goes
// dim, so a stack of dialogs still reads in the order they were opened.
func TestStackedModalDimsTheOneBehind(t *testing.T) {
	store := newFakeStore(model.Task{ID: 1, Title: "設計", Status: "todo"})
	h := newHarness(t, Deps{Tasks: store}, Settings{})

	h.key("enter")
	alone := h.board.renderDetail(true)
	// The status row opens the destination picker over the detail modal.
	h.key("down")
	h.key("enter")
	behind := h.board.renderDetail(false)

	if alone == behind {
		t.Errorf("ピッカーを開いても詳細モーダルの描画が変わっていない")
	}
	if !strings.Contains(h.board.render(), "の移行先") {
		t.Errorf("重ねたピッカーが描画されていない")
	}
}

// A column with nothing in it still reads as a column.
func TestEmptyColumnShowsPlaceholder(t *testing.T) {
	h := newHarness(t, Deps{Tasks: newFakeStore()}, Settings{})

	if view := h.board.render(); !strings.Contains(view, "カードなし") {
		t.Errorf("空列のプレースホルダが無い:\n%s", view)
	}
}
