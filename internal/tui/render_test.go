package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/ukwhatn/taskherd/internal/fetch"
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
	if !strings.Contains(view, h.board.icons.ScrollDown+" "+fmt.Sprintf(ja.Board.MoreCount, 7)) {
		t.Fatalf("下方向のオーバーフローインジケータが無い:\n%s", view)
	}
	if strings.Contains(view, h.board.icons.ScrollUp+" ") {
		t.Errorf("先頭にいるのに上方向のインジケータが出ている:\n%s", view)
	}
	if !strings.Contains(view, "#1 タスク 1") {
		t.Errorf("先頭カードが描画されていない:\n%s", view)
	}

	for i := 0; i < 9; i++ {
		h.key("down")
	}
	view = h.board.render()
	if !strings.Contains(view, h.board.icons.ScrollUp+" "+fmt.Sprintf(ja.Board.MoreCount, 7)) {
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
					model.Task{ID: 12, Title: title, Due: due("2026-08-30"),
						Links: []model.Link{
							{URL: "https://github.com/owner/repo/pull/1234", Kind: model.LinkKindGitHubPR},
							{URL: "https://x.atlassian.net/browse/ABC-123", Kind: model.LinkKindJira},
						}},
					SessionBadge{Text: "* working"}, nil, h.board.cardStyle(), boardNow)
				got := h.board.renderCard(card, Column{Color: "green"}, width, true, density.metrics())

				for _, line := range strings.Split(got, "\n") {
					if w := lipgloss.Width(line); w != width {
						t.Fatalf("density=%v width=%d: 行幅 = %d: %q", density, width, w, line)
					}
				}
				if lines := strings.Count(got, "\n") + 1; lines != cardHeight(card, width, density.metrics()) {
					t.Fatalf("density=%v width=%d: 行数 = %d, want %d",
						density, width, lines, cardHeight(card, width, density.metrics()))
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

	if !strings.Contains(view, ja.Common.ConfirmTitle) {
		t.Errorf("確認ダイアログのタイトルが無い:\n%s", view)
	}
	if !strings.Contains(view, ja.Common.ConfirmHelp) {
		t.Errorf("確認ダイアログのキーヘルプが無い:\n%s", view)
	}
	if !strings.Contains(view, "ToDo (3)") {
		t.Errorf("ダイアログの背後に盤面が残っていない:\n%s", view)
	}
	if !strings.Contains(view, "tab ステータス") {
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
	if !strings.Contains(h.board.render(), fmt.Sprintf(ja.Select.StatusTitle, 1)) {
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

// A narrow column drops the owner, then the repository, but never the number: the number is what
// tells one PR from another, so it is the last thing to go.
// A state too long for the cells left over is cut, not dropped: it was fetched, and a row with
// nothing after the reference reads as a link whose state is unknown.
func TestLinkRowCutsStateRatherThanDroppingIt(t *testing.T) {
	h := newHarness(t, Deps{Tasks: newFakeStore()}, Settings{Classifier: testClassifier, Icons: IconASCII})
	link := model.Link{URL: "https://x.atlassian.net/browse/ABC-1", Kind: model.LinkKindJira}
	states := map[string]fetch.LinkState{
		link.URL: {
			Kind:    model.LinkKindJira,
			Fetched: true,
			Jira:    &fetch.JiraData{StatusName: "開発中(QAデプロイ待ち)", StatusCategory: "indeterminate"},
		},
	}
	rows := BuildLinkRows(linkTask(link), states, h.board.cardStyle())

	// 12 cells of reference plus the icon leave too little for the 22-cell status name.
	whole := stripANSI(h.board.renderLinkRow(rows[0], 40))
	if !strings.Contains(whole, "開発中(QAデプロイ待ち)") {
		t.Fatalf("広い幅で状態が丸ごと出ていない: %q", whole)
	}

	for _, width := range []int{20, 24, 28} {
		t.Run(fmt.Sprintf("width%d", width), func(t *testing.T) {
			got := stripANSI(h.board.renderLinkRow(rows[0], width))
			if !strings.Contains(got, "ABC-1") {
				t.Fatalf("参照が消えている: %q", got)
			}
			if !strings.Contains(got, "開") {
				t.Errorf("状態が丸ごと落ちている: %q", got)
			}
			if w := lipgloss.Width(h.board.renderLinkRow(rows[0], width)); w > width {
				t.Errorf("表示幅 = %d, want <= %d", w, width)
			}
		})
	}

	// Below the point where even one wide character and the cut mark fit, the state is left out
	// entirely rather than drawn as a bare mark that says nothing.
	narrow := stripANSI(h.board.renderLinkRow(rows[0], 16))
	if strings.HasSuffix(narrow, "~") && !strings.Contains(narrow, "開") {
		t.Errorf("意味を持たない切り詰めが出ている: %q", narrow)
	}
}

func TestLinkRowDegradesReferenceByWidth(t *testing.T) {
	h := newHarness(t, Deps{Tasks: newFakeStore()}, Settings{Classifier: testClassifier, Icons: IconASCII})
	rows := BuildLinkRows(
		linkTask(model.Link{URL: "https://github.com/owner/repo/pull/123", Kind: model.LinkKindGitHubPR}),
		nil, h.board.cardStyle())

	tests := []struct {
		width int
		want  string
	}{
		{30, "PR owner/repo#123 未取得"},
		{17, "PR owner/repo#123"},
		// The shorter reference leaves cells over, and a state that does not fit them whole is cut
		// rather than dropped.
		{16, "PR repo#123 未~"},
		{11, "PR repo#123"},
		{10, "PR #123"},
		{7, "PR #123"},
		{6, "PR #1~"},
		// Narrower than the icon and one cell, nothing identifies the link, so the row goes blank
		// rather than printing a reference that is only a truncation mark.
		{3, ""},
	}
	for _, tc := range tests {
		t.Run(fmt.Sprintf("width%d", tc.width), func(t *testing.T) {
			rendered := h.board.renderLinkRow(rows[0], tc.width)
			got := stripANSI(rendered)
			if got != tc.want {
				t.Errorf("renderLinkRow(width=%d) = %q, want %q", tc.width, got, tc.want)
			}
			if w := lipgloss.Width(rendered); w > tc.width {
				t.Errorf("表示幅 = %d, want <= %d", w, tc.width)
			}
		})
	}
}

// stripANSI drops the styling from a rendered line so a test can assert on what the user reads.
func stripANSI(s string) string {
	var (
		out    strings.Builder
		escape bool
	)
	for _, r := range s {
		switch {
		case r == 0x1b:
			escape = true
		case escape && (r == 'm' || r == '\\'):
			escape = false
		case !escape:
			out.WriteRune(r)
		}
	}
	return out.String()
}

// A card grows a row per link, and its box has to grow with it rather than clipping the last one.
func TestCardHeightFollowsLinkCount(t *testing.T) {
	h := newHarness(t, Deps{Tasks: newFakeStore()}, Settings{Classifier: testClassifier, Icons: IconASCII})
	links := []model.Link{
		{URL: "https://github.com/owner/repo/pull/1", Kind: model.LinkKindGitHubPR},
		{URL: "https://x.atlassian.net/browse/ABC-1", Kind: model.LinkKindJira},
	}

	for count := 0; count <= len(links); count++ {
		card := BuildCard(model.Task{ID: 1, Title: "task", Status: "todo", Links: links[:count]},
			SessionBadge{}, nil, h.board.cardStyle(), boardNow)
		for _, density := range []Density{DensityRoomy, DensityTight, DensityCompact} {
			m := density.metrics()
			got := h.board.renderCard(card, Column{Color: "green"}, 40, false, m)
			if lines := strings.Count(got, "\n") + 1; lines != cardHeight(card, 40, m) {
				t.Errorf("links=%d density=%v: 行数 = %d, want %d", count, density, lines, cardHeight(card, 40, m))
			}
		}
	}
}

// A title that does not fit on one line costs the card a second one, which is what the column's
// scrolling has to be measured in.
func TestCardHeightGrowsWithWrappedTitle(t *testing.T) {
	h := newHarness(t, Deps{Tasks: newFakeStore()}, Settings{})
	m := DensityRoomy.metrics()

	short := BuildCard(model.Task{ID: 1, Title: "短い"}, SessionBadge{}, nil, h.board.cardStyle(), boardNow)
	long := BuildCard(model.Task{ID: 2, Title: "折り返しが必要になるくらい長いタイトル"},
		SessionBadge{}, nil, h.board.cardStyle(), boardNow)

	if got, want := cardHeight(long, 30, m), cardHeight(short, 30, m)+1; got != want {
		t.Errorf("折り返したカードの高さ = %d, want %d", got, want)
	}
	// The same title stops wrapping once the column is wide enough for it.
	if got, want := cardHeight(long, 60, m), cardHeight(short, 60, m); got != want {
		t.Errorf("広い列でのカードの高さ = %d, want %d（折り返さない）", got, want)
	}
}

// Cards of differing heights still scroll to the cursor: the window is measured per card, so a
// wrapped title cannot leave the selection off screen.
func TestColumnScrollsWithMixedTitleHeights(t *testing.T) {
	titles := []string{
		"短い",
		"折り返しが要るくらい長いタイトルを持つタスクの設計と実装",
		"短い",
		"こちらも折り返しが要る長さのタイトルでレビューまで含む",
		"短い",
		"三行目には届かないが二行は使うタイトルの実装とレビュー",
		"短い",
		"最後も折り返す長さのタイトルにしておく設計と実装の作業",
	}
	tasks := make([]model.Task, 0, len(titles))
	for i, title := range titles {
		tasks = append(tasks, model.Task{ID: i + 1, Title: title, Status: "todo"})
	}
	h := newHarness(t, Deps{Tasks: newFakeStore(tasks...)}, Settings{})
	h.board.width, h.board.height = 90, 20

	for i := 1; i <= len(titles); i++ {
		view := h.board.render()
		if !strings.Contains(view, fmt.Sprintf("#%d ", i)) {
			t.Fatalf("#%d を選択中なのに描画されていない:\n%s", i, view)
		}
		for _, line := range strings.Split(view, "\n") {
			if got := lipgloss.Width(line); got > h.board.width {
				t.Fatalf("行幅 = %d, want <= %d: %q", got, h.board.width, line)
			}
		}
		if i < len(titles) {
			h.key("down")
		}
	}
}

// Widening the terminal unwraps titles, so more cards fit and the window that had scrolled down
// has to come back rather than leave blank space under the last card.
func TestColumnWindowFollowsResize(t *testing.T) {
	h := boardWithCards(t, 12)
	h.board.width, h.board.height = 60, 20

	for i := 0; i < 11; i++ {
		h.key("down")
	}
	h.board.render()
	narrow := h.board.offsets["todo"]
	if narrow == 0 {
		t.Fatalf("狭い端末で窓が送られていない")
	}

	h.dispatch(tea.WindowSizeMsg{Width: 200, Height: 40})
	h.board.render()

	if wide := h.board.offsets["todo"]; wide >= narrow {
		t.Errorf("リサイズ後の offset = %d, want < %d", wide, narrow)
	}
}

// A terminal too narrow for even one readable column draws no columns at all, and says so without
// pushing the message itself past the edge.
func TestBoardSaysSoWhenNoColumnFits(t *testing.T) {
	h := boardWithCards(t, 3)

	for _, width := range []int{12, 16, 20, 25} {
		t.Run(fmt.Sprintf("width%d", width), func(t *testing.T) {
			h.board.width, h.board.height = width, 24
			view := h.board.render()

			if !strings.Contains(stripANSI(view), truncate(ja.Board.TooNarrow, width)) {
				t.Errorf("狭すぎる旨の表示が無い:\n%s", view)
			}
			if strings.Contains(view, "#1 ") {
				t.Errorf("列を描けない幅なのにカードが出ている:\n%s", view)
			}
			for _, line := range strings.Split(view, "\n") {
				if got := lipgloss.Width(line); got > width {
					t.Fatalf("行幅 = %d, want <= %d: %q", got, width, line)
				}
			}
		})
	}
}

// The footer is subject to the same rule as the rest of the board: nothing it draws may push a
// line past the terminal. Its second line carries two independently styled halves, so the width
// has to be spent before either is rendered.
func TestFooterStaysWithinTheTerminal(t *testing.T) {
	statuses := []struct {
		name   string
		status string
	}{
		{"ステータスなし", ""},
		{"短いステータス", "取得した"},
		{"sync の予算を食い尽くすステータス", strings.Repeat("長い報告", 12)},
	}
	for _, tc := range statuses {
		for _, width := range []int{4, 8, 11, 12, 20, 40} {
			t.Run(fmt.Sprintf("%s/width%d", tc.name, width), func(t *testing.T) {
				h := boardWithCards(t, 3)
				h.board.width, h.board.height = width, 24
				h.board.status = tc.status

				for _, line := range strings.Split(h.board.render(), "\n") {
					if got := lipgloss.Width(line); got > width {
						t.Fatalf("行幅 = %d, want <= %d: %q", got, width, line)
					}
				}
			})
		}
	}
}

// The folded columns are one box at the board's right edge, and the columns beside it keep the
// width the stack left them: nothing the stack draws may push the row past the terminal.
func TestCollapsedStackSitsAtTheRightEdge(t *testing.T) {
	store := newFakeStore(task(1, "todo"), task(2, "done"))
	h := newHarness(t, Deps{Tasks: store}, Settings{})

	for _, width := range []int{60, 83, 120, 162, 246} {
		t.Run(fmt.Sprintf("width%d", width), func(t *testing.T) {
			h.board.width, h.board.height = width, 24
			view := stripANSI(h.board.render())

			if !strings.Contains(view, "Done 1") {
				t.Errorf("スタックに折り畳み列が無い:\n%s", view)
			}
			// The stack is a box, and the row is inside it rather than beside the last column.
			stack := -1
			for _, line := range strings.Split(view, "\n") {
				if i := strings.Index(line, "Done 1"); i >= 0 {
					stack = i
				}
				if got := lipgloss.Width(line); got > width {
					t.Fatalf("行幅 = %d, want <= %d: %q", got, width, line)
				}
			}
			if stack < 0 {
				t.Fatalf("スタックの行が見つからない:\n%s", view)
			}
			if header := strings.Index(view, "ToDo"); stack <= header {
				t.Errorf("スタックの位置 = %d, want 列より右（%d）", stack, header)
			}
		})
	}
}

// Regression (§2.7): an oversized card (FitCards returns one even past the column's own budget)
// must not push the sideways-scroll notice off screen.
func TestNoticeSurvivesWhenCardsOverflowTheColumn(t *testing.T) {
	links := make([]model.Link, 8)
	for i := range links {
		links[i] = model.Link{URL: fmt.Sprintf("https://example.com/x%d", i), Kind: model.LinkKindOther}
	}
	store := newFakeStore(
		model.Task{ID: 1, Title: "too many links to fit in the column's own height", Status: "todo", Links: links},
		model.Task{ID: 2, Title: "working", Status: "working"},
	)
	h := newHarness(t, Deps{Tasks: store}, Settings{})
	// Narrow enough that only one column is on screen (the notice appears), short enough that #1's
	// card cannot fit in the column's own budget (FitCards still returns it whole).
	h.board.width, h.board.height = 40, 11

	view := stripANSI(h.board.render())
	if !strings.Contains(view, "で移動") {
		t.Fatalf("横スクロールの notice が出ていない:\n%s", view)
	}
	rows := strings.Split(view, "\n")
	noticeRow := -1
	for i, row := range rows {
		if strings.Contains(row, "で移動") {
			noticeRow = i
		}
	}
	if noticeRow < 0 || noticeRow >= h.board.height-footerReserve {
		t.Fatalf("notice の行 = %d, want 本文の最終行以内（footer の手前）: \n%s", noticeRow, view)
	}
}

// Same regression, forced through the folded-column stack instead: enough folded columns make the
// stack alone taller than the notice-adjusted body.
func TestNoticeSurvivesWhenCollapsedStackIsTallerThanTheColumns(t *testing.T) {
	tasks := []model.Task{task(1, "todo"), task(2, "working")}
	columns := model.Columns{
		{ID: "todo", Label: "ToDo", Kind: model.ColumnKindOpen},
		{ID: "working", Label: "Working", Kind: model.ColumnKindOpen},
	}
	for i := 0; i < 6; i++ {
		columns = append(columns, model.Column{
			ID: fmt.Sprintf("done%d", i), Label: fmt.Sprintf("Done%d", i), Kind: model.ColumnKindTerminal,
		})
	}
	h := newHarness(t, Deps{Tasks: newFakeStore(tasks...)}, Settings{Columns: columns})
	// Narrow enough for one open column plus the stack (the notice appears), short enough that the
	// stack's 6 rows plus its border do not fit in the notice-adjusted body height.
	h.board.width, h.board.height = 45, 10

	view := stripANSI(h.board.render())
	if !strings.Contains(view, "で移動") {
		t.Fatalf("横スクロールの notice が出ていない:\n%s", view)
	}
}

// The bodyHeight==1 edge case (§2.7): the notice's own line is all there is room for.
func TestNoticeAloneFillsABodyHeightOfOne(t *testing.T) {
	store := newFakeStore(task(1, "todo"), task(2, "working"))
	h := newHarness(t, Deps{Tasks: store}, Settings{})
	h.board.width, h.board.height = 40, 4 // height - footerReserve(3) == 1

	view := stripANSI(h.board.render())
	rows := strings.Split(view, "\n")
	if len(rows) == 0 || !strings.Contains(rows[0], "で移動") {
		t.Fatalf("bodyHeight=1 の本文行に notice が無い: %q\n全体:\n%s", rows[0], view)
	}
}

// A folded column is drawn in the stack instead of taking a column of its own, so the board's
// columns are the expanded ones and the sideways-scroll notice counts only those.
func TestFoldingReturnsWidthToTheOpenColumns(t *testing.T) {
	store := newFakeStore(task(1, "todo"), task(2, "working"), task(3, "done"))
	h := newHarness(t, Deps{Tasks: store}, Settings{})
	h.board.width, h.board.height = 100, 24

	folded := h.board.render()
	h.key("t")
	opened := h.board.render()

	if folded == opened {
		t.Fatalf("t で折り畳みを開いても描画が変わらない")
	}
	if strings.Contains(stripANSI(folded), "t 展開") {
		t.Errorf("折り畳み列がまだ列として描かれている:\n%s", folded)
	}
	if !strings.Contains(stripANSI(opened), "Done (1)") {
		t.Errorf("展開した terminal 列にヘッダが無い:\n%s", opened)
	}
	for _, line := range strings.Split(opened, "\n") {
		if got := lipgloss.Width(line); got > h.board.width {
			t.Fatalf("行幅 = %d, want <= %d: %q", got, h.board.width, line)
		}
	}
}
