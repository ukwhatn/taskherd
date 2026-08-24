package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/ukwhatn/taskherd/internal/model"
)

const addHelp = "↑↓ 項目  ←→ ステータス  enter 作成  esc 取消"

// addField is one row of the add modal.
type addField int

const (
	addTitle addField = iota
	addStatus
	addDue
	addNote
	addLink
	addFieldCount
)

var addFieldLabels = [addFieldCount]string{
	addTitle:  "タイトル",
	addStatus: "ステータス",
	addDue:    "期限",
	addNote:   "note",
	addLink:   "リンク",
}

// addState is the task-creation modal.
//
// Unlike the detail modal every text row is live: the field under the cursor accepts typing with
// no Enter to open it, and Enter creates the task from wherever the cursor happens to be. That is
// what makes "type a title, press Enter" the shortest path through it.
type addState struct {
	inputs [addFieldCount]textinput.Model
	status string
	cursor addField
	// extraTitles are the trailing lines of a multi-line paste into the title field. Each one
	// becomes a task of its own alongside the title still in the field.
	extraTitles []string
	err         string
}

func newAddState(statusID string) addState {
	state := addState{status: statusID}
	for i := range state.inputs {
		state.inputs[i] = newFieldInput()
	}
	state.focus()
	return state
}

// focus keeps exactly the field under the cursor accepting text.
func (a *addState) focus() {
	for i := range a.inputs {
		if addField(i) == a.cursor && addField(i) != addStatus {
			a.inputs[i].Focus()
			a.inputs[i].CursorEnd()
			continue
		}
		a.inputs[i].Blur()
	}
}

func (a *addState) move(delta int) {
	next := addField(int(a.cursor) + delta)
	if next < 0 || next >= addFieldCount {
		return
	}
	a.cursor = next
	a.focus()
}

func (a *addState) value(field addField) string {
	return a.inputs[field].Value()
}

// titles are every task the modal would create: the title field plus the extra lines a multi-line
// paste left behind.
func (a *addState) titles() []string {
	return append(splitTitleLines(a.value(addTitle)), a.extraTitles...)
}

func (b *Board) beginAdd() tea.Cmd {
	col, ok := b.targetColumn()
	if !ok {
		return status("列が定義されていないためタスクを作成できない", true)
	}
	b.add = newAddState(col.ID)
	b.mode = modeAdd
	return nil
}

func (b *Board) handleAddKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		b.mode = modeBoard
		return b, nil
	case "up":
		b.add.move(-1)
		return b, nil
	case "down":
		b.add.move(1)
		return b, nil
	case "enter":
		return b, b.submitAdd()
	case "left":
		if b.add.cursor == addStatus {
			b.shiftAddStatus(-1)
			return b, nil
		}
	case "right":
		if b.add.cursor == addStatus {
			b.shiftAddStatus(1)
			return b, nil
		}
	}

	if b.add.cursor == addStatus {
		return b, nil
	}
	updated, cmd := b.add.inputs[b.add.cursor].Update(msg)
	b.add.inputs[b.add.cursor] = updated
	return b, cmd
}

func (b *Board) pasteIntoAdd(msg tea.PasteMsg) (tea.Model, tea.Cmd) {
	if b.add.cursor == addStatus {
		return b, nil
	}
	if b.add.cursor == addTitle {
		// The field is single-line and would fold a multi-line paste into one string, so the
		// trailing lines are set aside here: that is what turns them into one task per line.
		if lines := splitTitleLines(msg.Content); len(lines) > 1 {
			b.add.extraTitles = append(b.add.extraTitles, lines[1:]...)
			msg.Content = lines[0]
		}
	}
	updated, cmd := b.add.inputs[b.add.cursor].Update(msg)
	b.add.inputs[b.add.cursor] = updated
	return b, cmd
}

func (b *Board) shiftAddStatus(delta int) {
	targets := selectableColumns(b.columns)
	if len(targets) == 0 {
		return
	}
	next := statusIndex(targets, b.add.status) + delta
	if next < 0 || next >= len(targets) {
		return
	}
	b.add.status = targets[next].ID
}

// submitAdd validates every field before creating anything, so a bad date or URL leaves the modal
// open with what was typed still in it.
func (b *Board) submitAdd() tea.Cmd {
	titles := b.add.titles()
	if len(titles) == 0 {
		b.add.err = "タイトルを入力する"
		return nil
	}
	urls, err := parseLinkURLs(b.add.value(addLink))
	if err != nil {
		b.add.err = err.Error()
		return nil
	}
	var due *model.Date
	if raw := strings.TrimSpace(b.add.value(addDue)); raw != "" {
		parsed, parseErr := model.ParseDate(raw)
		if parseErr != nil {
			b.add.err = parseErr.Error()
			return nil
		}
		due = &parsed
	}

	in := model.TaskInput{
		Status: b.add.status,
		Due:    due,
		Note:   strings.TrimSpace(b.add.value(addNote)),
	}
	b.mode = modeBoard
	return b.addTasksCmd(titles, in, urls)
}

func (b *Board) renderAdd() string {
	statusLabel := b.add.status
	if col, ok := b.settings.Columns.Find(b.add.status); ok {
		statusLabel = fmt.Sprintf("%s (%s)", col.Label, col.ID)
	}

	lines := []string{b.styles.heading.Render("新しいタスク")}
	for field := addTitle; field < addFieldCount; field++ {
		marker := "  "
		if field == b.add.cursor {
			marker = "▌ "
		}
		value := b.add.inputs[field].View()
		if field == addStatus {
			value = statusLabel
			if field == b.add.cursor {
				value += "   " + b.styles.dim.Render("←→ で変更")
			}
		}
		lines = append(lines, truncate(marker+padLabel(addFieldLabels[field])+value, b.width))
	}

	if len(b.add.extraTitles) > 0 {
		lines = append(lines, b.styles.status.Render(
			fmt.Sprintf("複数行を貼り付け: enter で %d 件のタスクを作成する", len(b.add.titles()))))
	}
	if b.add.err != "" {
		lines = append(lines, b.styles.alert.Render(truncate(b.add.err, b.width)))
	}
	lines = append(lines, b.styles.footer.Render(truncate(addHelp, b.width)))
	return strings.Join(lines, "\n")
}
