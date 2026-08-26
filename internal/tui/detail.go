package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/ukwhatn/taskherd/internal/fetch"
	"github.com/ukwhatn/taskherd/internal/herdrc"
	"github.com/ukwhatn/taskherd/internal/i18n"
	"github.com/ukwhatn/taskherd/internal/model"
)

// detailHelp is the modal's key list, with the arrow keys named by the icon set in use.
func (b *Board) detailHelp() string {
	return fmt.Sprintf(b.text.Detail.Help, b.icons.verticalKeys(), b.icons.horizontalKeys())
}

// detailItemKind is what one row of the detail modal stands for.
type detailItemKind int

const (
	itemTitle detailItemKind = iota
	itemStatus
	itemDue
	itemNote
	itemLink
	itemAddLink
	itemSession
	itemAddSession
)

// detailItem is one selectable row. ref identifies which link or session the row belongs to.
type detailItem struct {
	kind  detailItemKind
	ref   string
	label string
	value string
	// disabled marks a row that cannot be acted on right now, such as linking a session while
	// herdr is unreachable.
	disabled bool
}

// detailEditKind is the field a text prompt inside the detail modal is editing.
type detailEditKind int

const (
	editNone detailEditKind = iota
	editTitle
	editDue
	editLinkNote
	editAddLink
)

// detailState is the detail modal: a cursor over the task's fields, links and sessions, plus the
// one text prompt that Enter opens on the selected row.
//
// The modal is pinned to a task id rather than to the focused card, so an edit that moves the card
// to another column leaves the modal where it is.
type detailState struct {
	taskID int
	cursor int
	offset int
	// quitOnClose marks a detail opened straight from startup (prefix+t on a pane whose session was
	// already linked to a task): Esc there ends the whole program — the popup herdr opened just for
	// this — rather than falling back to a board the launch was never meant to show. It lives here
	// rather than as a Board-wide flag because detail closes through more paths than Esc (the task
	// disappearing underneath it, most notably), and a flag on Board would still be set the next
	// time a board-opened detail closed, ending the program along with it.
	quitOnClose bool

	editing  bool
	editKind detailEditKind
	// editRef is the link URL a link-note edit belongs to.
	editRef string
	input   textinput.Model
}

func newDetailState(taskID int) detailState {
	return detailState{taskID: taskID, input: newFieldInput()}
}

func (d *detailState) clamp(count int) {
	if d.cursor >= count {
		d.cursor = count - 1
	}
	if d.cursor < 0 {
		d.cursor = 0
	}
}

func (d *detailState) move(delta, count int) {
	next := d.cursor + delta
	if next < 0 || next >= count {
		return
	}
	d.cursor = next
}

// detailItems lays the task out as the flat row list the modal navigates. A group's label is shown
// only on its first row, which keeps the repeated link and session rows readable.
func (b *Board) detailItems(task model.Task) []detailItem {
	statusLabel := unknownColumnLabel
	if col, ok := b.settings.Columns.Find(task.Status); ok {
		statusLabel = col.Label
	}
	due := b.text.Common.None
	if task.Due != nil {
		due = string(*task.Due)
		if isOverdue(*task.Due, b.deps.now()) {
			due += b.text.Detail.Overdue
		}
	}
	note := b.text.Common.None
	if task.Note != "" {
		note = fmt.Sprintf(b.text.Detail.NoteLines, len(strings.Split(task.Note, "\n")))
	}

	items := []detailItem{
		{kind: itemTitle, label: b.text.Detail.LabelTitle, value: task.Title},
		{kind: itemStatus, label: b.text.Detail.LabelStatus, value: fmt.Sprintf("%s (%s)", statusLabel, task.Status)},
		{kind: itemDue, label: b.text.Detail.LabelDue, value: due},
		{kind: itemNote, label: b.text.Detail.LabelNote, value: note},
	}

	for _, link := range task.Links {
		value := fmt.Sprintf("[%s] %s", link.Kind, link.URL)
		if link.Note != "" {
			value += "  - " + link.Note
		}
		items = append(items, detailItem{kind: itemLink, ref: link.URL, label: b.text.Detail.LabelLink, value: value})
	}
	items = append(items, detailItem{kind: itemAddLink, label: b.text.Detail.LabelLink, value: b.text.Detail.AddLink})

	for _, session := range task.Sessions {
		items = append(items, detailItem{
			kind:  itemSession,
			ref:   session.SessionID,
			label: b.text.Detail.LabelSession,
			value: b.sessionRow(session),
		})
	}
	sessionAdd := detailItem{kind: itemAddSession, label: b.text.Detail.LabelSession, value: b.text.Detail.AddSession}
	if b.deps.Herdr == nil || !b.sessions.Available {
		sessionAdd.disabled = true
		sessionAdd.value += b.text.Detail.HerdrSuffix
	}
	items = append(items, sessionAdd)

	// Blank out a label that repeats the row above it.
	for i := len(items) - 1; i > 0; i-- {
		if items[i].label == items[i-1].label {
			items[i].label = ""
		}
	}
	return items
}

// sessionRow is the one-line summary of a linked session shown in the item list.
func (b *Board) sessionRow(session model.SessionRef) string {
	state := herdrc.StateOffline
	if b.sessions.Available {
		if live, ok := b.sessions.State[session.SessionID]; ok {
			state = live
		}
	}
	row := fmt.Sprintf("%s %s  %s", session.Agent, shortID(session.SessionID), sessionStateText(state, b.icons))
	if paneID := b.sessions.Pane[session.SessionID]; paneID != "" {
		row += fmt.Sprintf(" (pane %s)", paneID)
	}
	if session.Label != "" {
		return row + "  " + session.Label
	}
	return row + "  " + session.Cwd
}

func (b *Board) currentDetailItem(items []detailItem) detailItem {
	if b.detail.cursor < 0 || b.detail.cursor >= len(items) {
		return detailItem{}
	}
	return items[b.detail.cursor]
}

// --- key handling ------------------------------------------------------------

func (b *Board) handleDetailKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if b.detail.editing {
		return b.handleDetailEditKey(msg)
	}
	if !isCommandKey(msg) {
		return b, nil
	}

	task := b.activeTask()
	if task == nil {
		b.mode = modeBoard
		return b, nil
	}
	items := b.detailItems(*task)
	b.detail.clamp(len(items))
	item := b.currentDetailItem(items)

	switch msg.String() {
	case "q":
		if b.detail.quitOnClose {
			return b, tea.Quit
		}
		b.mode = modeBoard
	case "up":
		b.detail.move(-1, len(items))
	case "down":
		b.detail.move(1, len(items))
	case "left":
		if item.kind == itemStatus {
			return b, b.shiftStatus(*task, -1)
		}
	case "right":
		if item.kind == itemStatus {
			return b, b.shiftStatus(*task, 1)
		}
	case "enter":
		return b, b.activateDetailItem(*task, item)
	case "backspace", "delete":
		return b, b.unlinkDetailItem(*task, item)
	case "g":
		return b, b.beginJump()
	case "r":
		return b, b.refreshTaskCmd()
	}
	return b, nil
}

// shiftStatus steps the task one column along without leaving the modal. A task sitting in the
// (unknown) column has no position to step from, so either direction lands it on a real column.
func (b *Board) shiftStatus(task model.Task, delta int) tea.Cmd {
	targets := selectableColumns(b.columns)
	if len(targets) == 0 {
		return status(b.text.Common.NoColumns, true)
	}
	idx := statusIndex(targets, task.Status)
	if idx < 0 {
		if delta < 0 {
			return b.setStatusCmd(task.ID, targets[len(targets)-1].ID)
		}
		return b.setStatusCmd(task.ID, targets[0].ID)
	}
	next := idx + delta
	if next < 0 || next >= len(targets) {
		return nil
	}
	return b.setStatusCmd(task.ID, targets[next].ID)
}

func (b *Board) activateDetailItem(task model.Task, item detailItem) tea.Cmd {
	switch item.kind {
	case itemTitle:
		b.beginDetailEdit(editTitle, "", task.Title)
	case itemStatus:
		return b.beginStatusSelect()
	case itemDue:
		due := ""
		if task.Due != nil {
			due = string(*task.Due)
		}
		b.beginDetailEdit(editDue, "", due)
	case itemNote:
		return b.editNoteCmd()
	case itemLink:
		link, ok := linkByURL(task.Links, item.ref)
		if !ok {
			return status(b.text.Detail.LinkNotFound, true)
		}
		b.beginDetailEdit(editLinkNote, link.URL, link.Note)
	case itemAddLink:
		b.beginDetailEdit(editAddLink, "", "")
	case itemSession:
		session, ok := task.Session(item.ref)
		if !ok {
			return status(b.text.Detail.SessionNotFound, true)
		}
		return b.jumpTo(task.ID, task.Title, *session)
	case itemAddSession:
		return b.beginSessionSelect(task.ID)
	}
	return nil
}

func (b *Board) unlinkDetailItem(task model.Task, item detailItem) tea.Cmd {
	switch item.kind {
	case itemLink:
		b.openConfirm(confirmState{
			kind:   confirmUnlinkLink,
			prompt: fmt.Sprintf(b.text.Detail.ConfirmRemoveLink, task.ID, item.ref),
			taskID: task.ID,
			ref:    item.ref,
		})
	case itemSession:
		b.openConfirm(confirmState{
			kind:   confirmUnlinkSession,
			prompt: fmt.Sprintf(b.text.Detail.ConfirmDetachSession, task.ID, shortID(item.ref)),
			taskID: task.ID,
			ref:    item.ref,
		})
	default:
		return status(b.text.Detail.OnlyLinkOrSession, true)
	}
	return nil
}

func (b *Board) beginDetailEdit(kind detailEditKind, ref, value string) {
	b.detail.editing = true
	b.detail.editKind = kind
	b.detail.editRef = ref
	b.detail.input.SetValue(value)
	b.detail.input.CursorEnd()
	b.detail.input.Focus()
}

func (b *Board) endDetailEdit() {
	b.detail.editing = false
	b.detail.editKind = editNone
	b.detail.editRef = ""
	b.detail.input.Blur()
	b.detail.input.Reset()
}

func (b *Board) handleDetailEditKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// Text-carrying events are always insertion, so an IME commit cannot match a binding.
	if !isTextKey(msg) {
		switch msg.String() {
		case "esc":
			b.endDetailEdit()
			return b, nil
		case "enter":
			kind, ref, value := b.detail.editKind, b.detail.editRef, b.detail.input.Value()
			b.endDetailEdit()
			return b, b.submitDetailEdit(kind, ref, value)
		}
	}

	updated, cmd := b.detail.input.Update(msg)
	b.detail.input = updated
	return b, cmd
}

func (b *Board) pasteIntoDetail(msg tea.PasteMsg) (tea.Model, tea.Cmd) {
	if !b.detail.editing {
		return b, nil
	}
	updated, cmd := b.detail.input.Update(msg)
	b.detail.input = updated
	return b, cmd
}

func (b *Board) submitDetailEdit(kind detailEditKind, ref, value string) tea.Cmd {
	taskID := b.detail.taskID
	switch kind {
	case editTitle:
		return b.setTitleCmd(taskID, value)
	case editDue:
		return b.setDueCmd(taskID, value)
	case editLinkNote:
		return b.setLinkNoteCmd(taskID, ref, value)
	case editAddLink:
		urls, err := parseLinkURLs(b.text, value)
		if err != nil {
			return status(err.Error(), true)
		}
		return b.addLinksCmd(taskID, urls)
	}
	return nil
}

// --- rendering ---------------------------------------------------------------

// renderDetail draws the detail modal as one centred box. focused is false while a picker of its
// own is open over it, which is what dims its border and hands the accent to the picker.
func (b *Board) renderDetail(focused bool) string {
	task := b.activeTask()
	if task == nil {
		b.mode = modeBoard
		return ""
	}
	items := b.detailItems(*task)
	b.detail.clamp(len(items))

	width := b.modalWidth(96)
	inner := modalInner(width)

	prompt := b.detailPromptLines(inner)
	noteLines := b.detailNoteBlock(*task, b.modalBody(1), inner)
	listHeight := b.modalBody(1) - len(noteLines) - len(prompt)
	if listHeight < 1 {
		listHeight = 1
	}

	b.detail.offset = scrollOffset(b.detail.offset, b.detail.cursor, listHeight, len(items))
	end := b.detail.offset + listHeight
	if end > len(items) {
		end = len(items)
	}

	lines := make([]string, 0, listHeight+len(noteLines)+len(prompt))
	for i := b.detail.offset; i < end; i++ {
		lines = append(lines, b.renderDetailItem(items[i], i == b.detail.cursor, inner))
	}
	lines = append(lines, noteLines...)
	lines = append(lines, prompt...)

	help := b.detailHelp()
	if b.detail.editing {
		help = b.text.Detail.HelpEditing
	}
	return b.renderModal(modal{
		title:   fmt.Sprintf("#%d %s", task.ID, task.Title),
		body:    lines,
		help:    help,
		width:   width,
		focused: focused,
	})
}

// detailPromptLines are the text field the modal opens on a row, drawn in the box under the list.
func (b *Board) detailPromptLines(inner int) []string {
	if !b.detail.editing {
		return nil
	}
	// The field scrolls its own text rather than spilling out of the box.
	b.detail.input.SetWidth(inner - 2)
	return []string{
		"",
		b.styles.prompt.Render(truncate(detailEditPrompt(b.text, b.detail.editKind), inner)),
		b.detail.input.View(),
	}
}

// detailNoteBlock renders the note underneath the item list. The note is the one field with no
// useful one-line form, so it is shown in full rather than only as a row, clipped to a third of
// the body so it never crowds the list out.
func (b *Board) detailNoteBlock(task model.Task, body, inner int) []string {
	if task.Note == "" {
		return nil
	}
	budget := body / 3
	if budget < 2 {
		return nil
	}

	lines := []string{b.styles.dim.Render(truncate("── note ──", inner))}
	for _, line := range strings.Split(task.Note, "\n") {
		if len(lines) >= budget {
			lines = append(lines, b.styles.dim.Render(truncateMark))
			break
		}
		lines = append(lines, b.styles.dim.Render(truncate(line, inner)))
	}
	return lines
}

func (b *Board) renderDetailItem(item detailItem, focused bool, inner int) string {
	marker := padCell("", cursorWidth(b.icons))
	if focused {
		marker = b.icons.Cursor + " "
	}

	// The live suffix is styled, so the row is trimmed to width before it is appended rather than
	// after: trimming afterwards would cut an escape sequence in half.
	line := truncate(marker+padLabel(item.label)+item.value, inner)
	if item.kind == itemLink {
		line += b.decorateLinkRow(item.ref, inner-lipgloss.Width(line))
	}
	switch {
	case focused:
		line = b.styles.cardTitleSelected.Render(padCell(line, inner))
	case item.disabled:
		line = b.styles.dim.Render(line)
	}
	// The hyperlink goes on last, around the finished row: an escape inside the styled text would
	// be measured and cut with it.
	if item.kind == itemLink {
		line = b.linkText(item.ref, line)
	}
	return line
}

// decorateLinkRow is the cached live state appended to a link row, so the row says what the badge
// on the card says without opening anything further. room is what is left of the row's width; the
// suffix is trimmed to it as plain text, before it is styled.
func (b *Board) decorateLinkRow(url string, room int) string {
	state, ok := b.links[url]
	if !ok || !state.Fetchable() || room < 4 {
		return ""
	}
	room -= 2

	if !state.Fetched {
		if state.Err != "" {
			mark := b.icons.failureMark(b.text.Common.Failed, failingAge(state))
			return "  " + b.styles.segment(SegAlert).Render(truncate(mark, room))
		}
		return "  " + b.styles.segment(SegMuted).Render(truncate(b.text.Common.NotFetched, room))
	}

	// The marks are measured before the summary is trimmed, so a long PR title gives way to them
	// rather than pushing them off the row: how old the value is, and whether anything is still
	// able to refresh it, outrank the title here.
	var marks []Segment
	if state.Stale {
		marks = append(marks, Segment{Text: fmt.Sprintf(b.text.Detail.StaleMark, FormatAge(state.Age)), Kind: SegDim})
	}
	if state.Err != "" {
		marks = append(marks, Segment{Text: b.icons.failureMark(b.text.Common.Failed, failingAge(state)), Kind: SegAlert})
	}
	rendered, used := "", 0
	for _, mark := range marks {
		used += lipgloss.Width(mark.Text) + 1
		rendered += " " + b.styles.segment(mark.Kind).Render(mark.Text)
	}
	return "  " + b.styles.segment(linkTone(state)).Render(truncate(DescribeLink(b.text, state), room-used)) + rendered
}

func detailEditPrompt(text *i18n.Catalog, kind detailEditKind) string {
	switch kind {
	case editTitle:
		return text.Detail.PromptTitle
	case editDue:
		return text.Detail.PromptDue
	case editLinkNote:
		return text.Detail.PromptLinkNote
	case editAddLink:
		return text.Detail.PromptAddLink
	default:
		return ""
	}
}

// DescribeLink spells out a fetched link's state in full, for the detail view and `show`.
//
// The state words themselves (open, draft, review=…) are GitHub's and Jira's own vocabulary and
// stay as they are in every language; only the fallback for an unclassifiable state is translated.
func DescribeLink(text *i18n.Catalog, state fetch.LinkState) string {
	switch {
	case state.GitHub != nil && state.Kind == model.LinkKindGitHubPR:
		parts := []string{strings.ToLower(state.GitHub.State)}
		if state.GitHub.IsDraft {
			parts = append(parts, "draft")
		}
		if state.GitHub.ReviewDecision != "" {
			parts = append(parts, "review="+strings.ToLower(state.GitHub.ReviewDecision))
		}
		if state.GitHub.Checks != "" {
			parts = append(parts, "checks="+state.GitHub.Checks)
		}
		return strings.Join(parts, " ") + titleSuffix(state.GitHub.Title)
	case state.GitHub != nil:
		return strings.ToLower(state.GitHub.State) + titleSuffix(state.GitHub.Title)
	case state.Jira != nil:
		return fmt.Sprintf("%s (%s)%s", state.Jira.StatusName, state.Jira.StatusCategory, titleSuffix(state.Jira.Summary))
	default:
		return i18n.OrDefault(text).Common.Unknown
	}
}

func titleSuffix(title string) string {
	if title == "" {
		return ""
	}
	return " - " + title
}
