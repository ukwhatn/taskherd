package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/ukwhatn/taskherd/internal/i18n"
	"github.com/ukwhatn/taskherd/internal/model"
)

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

// addFieldLabels names the rows in the language the board is drawing in.
func addFieldLabels(text *i18n.Catalog) [addFieldCount]string {
	return [addFieldCount]string{
		addTitle:  text.Add.LabelTitle,
		addStatus: text.Add.LabelStatus,
		addDue:    text.Add.LabelDue,
		addNote:   text.Add.LabelNote,
		addLink:   text.Add.LabelLink,
	}
}

// multiline reports whether several lines mean something on this field: the title makes one task
// per line, and a note keeps the line breaks it was written with.
func (f addField) multiline() bool {
	return f == addTitle || f == addNote
}

// addState is the task-creation modal.
//
// Unlike the detail modal every text row is live: the field under the cursor accepts typing with
// no Enter to open it, and Enter creates the task from wherever the cursor happens to be. That is
// what makes "type a title, press Enter" the shortest path through it.
type addState struct {
	inputs [addFieldCount]textinput.Model
	// lines are the lines already finished on a multi-line field; the input edits the line that
	// follows them. A single-line text field cannot hold a line break, so the breaks live here.
	lines  [addFieldCount][]string
	status string
	cursor addField
	err    string
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

// fieldLines is everything typed into a field: the finished lines plus the one being edited.
func (a *addState) fieldLines(field addField) []string {
	return append(append([]string(nil), a.lines[field]...), a.value(field))
}

// breakLine ends the line being edited and starts a new one.
func (a *addState) breakLine() {
	if !a.cursor.multiline() {
		return
	}
	a.lines[a.cursor] = append(a.lines[a.cursor], a.value(a.cursor))
	a.inputs[a.cursor].SetValue("")
}

// titles are the tasks the modal would create, one per non-blank title line.
func (a *addState) titles() []string {
	return splitTitleLines(strings.Join(a.fieldLines(addTitle), "\n"))
}

func (a *addState) note() string {
	return strings.TrimSpace(strings.Join(a.fieldLines(addNote), "\n"))
}

func (b *Board) beginAdd() tea.Cmd {
	col, ok := b.targetColumn()
	if !ok {
		return status(b.text.Add.NoColumns, true)
	}
	b.add = newAddState(col.ID)
	b.mode = modeAdd
	return nil
}

func (b *Board) handleAddKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// An IME commit arrives as a key event carrying text, so only text-less events are read as
	// commands: otherwise a committed string could match a binding and be swallowed instead of
	// typed into the field.
	if !isTextKey(msg) {
		if b.isNewlineKey(msg) {
			b.add.breakLine()
			return b, nil
		}
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
	}

	if b.add.cursor == addStatus {
		return b, nil
	}
	updated, cmd := b.add.inputs[b.add.cursor].Update(msg)
	b.add.inputs[b.add.cursor] = updated
	return b, cmd
}

func (b *Board) pasteIntoAdd(msg tea.PasteMsg) (tea.Model, tea.Cmd) {
	field := b.add.cursor
	if field == addStatus {
		return b, nil
	}

	// A text field folds a pasted line break into a space, so on a field where the breaks carry
	// meaning the block is split here instead: the head goes in at the cursor, the middle lines
	// are finished as they stand, and the tail is left being edited.
	if lines := strings.Split(msg.Content, "\n"); field.multiline() && len(lines) > 1 {
		b.add.inputs[field], _ = b.add.inputs[field].Update(tea.PasteMsg{Content: lines[0]})
		b.add.lines[field] = append(b.add.lines[field], b.add.value(field))
		b.add.lines[field] = append(b.add.lines[field], lines[1:len(lines)-1]...)
		b.add.inputs[field].SetValue(lines[len(lines)-1])
		b.add.inputs[field].CursorEnd()
		return b, nil
	}

	updated, cmd := b.add.inputs[field].Update(msg)
	b.add.inputs[field] = updated
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
		b.add.err = b.text.Add.NeedTitle
		return nil
	}
	urls, err := parseLinkURLs(b.text, b.add.value(addLink))
	if err != nil {
		b.add.err = b.message(err)
		return nil
	}
	var due *model.Date
	if raw := strings.TrimSpace(b.add.value(addDue)); raw != "" {
		parsed, parseErr := model.ParseDate(raw)
		if parseErr != nil {
			b.add.err = b.message(parseErr)
			return nil
		}
		due = &parsed
	}

	in := model.TaskInput{Status: b.add.status, Due: due, Note: b.add.note()}
	b.mode = modeBoard
	return b.addTasksCmd(titles, in, urls)
}

func (b *Board) renderAdd() string {
	statusLabel := b.add.status
	if col, ok := b.settings.Columns.Find(b.add.status); ok {
		statusLabel = fmt.Sprintf("%s (%s)", col.Label, col.ID)
	}

	// Wide enough for the key help to land whole, which is where the modal says which key inserts
	// a line break on this terminal.
	width := b.modalWidth(84)
	inner := modalInner(width)

	labels := addFieldLabels(b.text)
	var lines []string
	for field := addTitle; field < addFieldCount; field++ {
		// The finished lines sit above the field, so a multi-line entry reads as what it is.
		for _, done := range b.add.lines[field] {
			lines = append(lines, b.styles.dim.Render(truncate("  "+padLabel("")+done, inner)))
		}

		marker := padCell("", cursorWidth(b.icons))
		if field == b.add.cursor {
			marker = b.icons.Cursor + " "
		}
		// The text field styles its own cursor, so its row is assembled from parts already the
		// right width rather than trimmed after the fact.
		label := truncate(marker+padLabel(labels[field]), inner)
		if field == addStatus {
			value := truncate(statusLabel, inner-lipgloss.Width(label))
			if field == b.add.cursor {
				hint := fmt.Sprintf(b.text.Add.ChangeHint, b.icons.horizontalKeys())
				value += "   " + b.styles.dim.Render(hint)
			}
			lines = append(lines, label+value)
			continue
		}
		// The field scrolls its own text rather than spilling out of the box.
		b.add.inputs[field].SetWidth(inner - lipgloss.Width(label) - 2)
		lines = append(lines, label+b.add.inputs[field].View())
	}

	if titles := b.add.titles(); len(titles) > 1 {
		lines = append(lines, b.styles.status.Render(truncate(
			fmt.Sprintf(b.text.Add.CreateHint, len(titles)), inner)))
	}
	if b.add.err != "" {
		lines = append(lines, b.styles.alert.Render(truncate(b.add.err, inner)))
	}

	return b.renderModal(modal{
		title:   b.text.Add.Title,
		body:    lines,
		help:    b.addHelp(),
		width:   width,
		focused: true,
	})
}

// addHelp names the key that actually inserts a line break, which depends on what the terminal
// turned out to support.
func (b *Board) addHelp() string {
	return fmt.Sprintf(b.text.Add.Help, b.icons.verticalKeys(), b.icons.horizontalKeys(), b.newlineKey())
}
