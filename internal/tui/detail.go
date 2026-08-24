package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/ukwhatn/taskherd/internal/fetch"
	"github.com/ukwhatn/taskherd/internal/herdrc"
	"github.com/ukwhatn/taskherd/internal/model"
)

const detailHelp = "↑↓ 項目  enter 編集  ←→ ステータス  delete 解除  g jump  r 取得  esc 戻る"

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
	due := "(なし)"
	if task.Due != nil {
		due = string(*task.Due)
		if isOverdue(*task.Due, b.deps.now()) {
			due += " 超過"
		}
	}
	note := "(なし)"
	if task.Note != "" {
		note = fmt.Sprintf("%d 行", len(strings.Split(task.Note, "\n")))
	}

	items := []detailItem{
		{kind: itemTitle, label: "タイトル", value: task.Title},
		{kind: itemStatus, label: "ステータス", value: fmt.Sprintf("%s (%s)", statusLabel, task.Status)},
		{kind: itemDue, label: "期限", value: due},
		{kind: itemNote, label: "note", value: note},
	}

	for _, link := range task.Links {
		value := fmt.Sprintf("[%s] %s", link.Kind, link.URL)
		if link.Note != "" {
			value += "  — " + link.Note
		}
		items = append(items, detailItem{kind: itemLink, ref: link.URL, label: "リンク", value: value})
	}
	items = append(items, detailItem{kind: itemAddLink, label: "リンク", value: "＋リンクを追加"})

	for _, session := range task.Sessions {
		items = append(items, detailItem{
			kind:  itemSession,
			ref:   session.SessionID,
			label: "セッション",
			value: b.sessionRow(session),
		})
	}
	sessionAdd := detailItem{kind: itemAddSession, label: "セッション", value: "＋セッションを紐づける"}
	if b.deps.Herdr == nil || !b.sessions.Available {
		sessionAdd.disabled = true
		sessionAdd.value += "（herdr 不達）"
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
	state := offlineBadge
	if b.sessions.Available {
		if live, ok := b.sessions.State[session.SessionID]; ok {
			state = live
		} else {
			state = herdrc.StateOffline
		}
	}
	row := fmt.Sprintf("%s %s  %s", session.Agent, shortID(session.SessionID), state)
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

	task := b.activeTask()
	if task == nil {
		b.mode = modeBoard
		return b, nil
	}
	items := b.detailItems(*task)
	b.detail.clamp(len(items))
	item := b.currentDetailItem(items)

	switch msg.String() {
	case "esc":
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
		return status("列が定義されていない", true)
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
			return status("リンクが見つからない", true)
		}
		b.beginDetailEdit(editLinkNote, link.URL, link.Note)
	case itemAddLink:
		b.beginDetailEdit(editAddLink, "", "")
	case itemSession:
		session, ok := task.Session(item.ref)
		if !ok {
			return status("セッションが見つからない", true)
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
			prompt: fmt.Sprintf("#%d から %s を解除する", task.ID, item.ref),
			taskID: task.ID,
			ref:    item.ref,
		})
	case itemSession:
		b.openConfirm(confirmState{
			kind:   confirmUnlinkSession,
			prompt: fmt.Sprintf("#%d から セッション %s を解除する", task.ID, shortID(item.ref)),
			taskID: task.ID,
			ref:    item.ref,
		})
	default:
		return status("この項目は delete で解除できない（リンク行・セッション行を選ぶ）", true)
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
		urls, err := parseLinkURLs(value)
		if err != nil {
			return status(err.Error(), true)
		}
		return b.addLinksCmd(taskID, urls)
	}
	return nil
}

// --- rendering ---------------------------------------------------------------

func (b *Board) renderDetail(overlay string) string {
	task := b.activeTask()
	if task == nil {
		b.mode = modeBoard
		return b.renderBoard("")
	}
	items := b.detailItems(*task)
	b.detail.clamp(len(items))

	footer := overlay
	if footer == "" {
		if b.detail.editing {
			footer = b.renderDetailEdit()
		} else {
			footer = b.styles.footer.Render(truncate(detailHelp, b.width))
		}
	}
	if b.status != "" {
		footer += "\n" + b.statusLine()
	}

	header := b.styles.heading.Render(truncate(fmt.Sprintf("#%d %s", task.ID, task.Title), b.width))
	body := b.height - 1 - lipgloss.Height(footer)
	if body < 1 {
		body = 1
	}

	noteLines := b.detailNoteBlock(*task, body)
	listHeight := body - len(noteLines)
	if listHeight < 1 {
		listHeight = 1
	}

	b.detail.offset = scrollOffset(b.detail.offset, b.detail.cursor, listHeight, len(items))
	end := b.detail.offset + listHeight
	if end > len(items) {
		end = len(items)
	}

	lines := []string{header}
	for i := b.detail.offset; i < end; i++ {
		lines = append(lines, b.renderDetailItem(items[i], i == b.detail.cursor))
	}
	lines = append(lines, noteLines...)
	lines = append(lines, footer)
	return strings.Join(lines, "\n")
}

// detailNoteBlock renders the note underneath the item list. The note is the one field with no
// useful one-line form, so it is shown in full rather than only as a row, clipped to a third of
// the body so it never crowds the list out.
func (b *Board) detailNoteBlock(task model.Task, body int) []string {
	if task.Note == "" {
		return nil
	}
	budget := body / 3
	if budget < 2 {
		return nil
	}

	lines := []string{b.styles.dim.Render(truncate("── note ──", b.width))}
	for _, line := range strings.Split(task.Note, "\n") {
		if len(lines) >= budget {
			lines = append(lines, b.styles.dim.Render("…"))
			break
		}
		lines = append(lines, b.styles.dim.Render(truncate(line, b.width)))
	}
	return lines
}

func (b *Board) renderDetailItem(item detailItem, focused bool) string {
	marker := "  "
	if focused {
		marker = "▌ "
	}

	value := item.value
	if item.kind == itemLink {
		value = b.decorateLinkRow(item.ref, value)
	}

	line := truncate(marker+padLabel(item.label)+value, b.width)
	switch {
	case focused:
		return b.styles.cardTitleSelected.Render(line)
	case item.disabled:
		return b.styles.dim.Render(line)
	default:
		return line
	}
}

// decorateLinkRow appends the cached live state to a link row, so the row says what the badge on
// the card says without opening anything further.
func (b *Board) decorateLinkRow(url, value string) string {
	state, ok := b.links[url]
	if !ok || !state.Fetchable() {
		return value
	}
	if !state.Fetched {
		if state.Err != "" {
			return value + "  " + b.styles.alert.Render("取得失敗")
		}
		return value + "  " + b.styles.dim.Render("未取得")
	}
	summary := DescribeLink(state)
	if state.Stale {
		return value + "  " + b.styles.linkStale.Render(fmt.Sprintf("%s（%s前 / TTL 超過）", summary, FormatAge(state.Age)))
	}
	return value + "  " + summary
}

func (b *Board) renderDetailEdit() string {
	return b.styles.prompt.Render(detailEditPrompt(b.detail.editKind)) + "\n" +
		b.detail.input.View() + "\n" +
		b.styles.dim.Render("enter 確定 / esc 取消")
}

func detailEditPrompt(kind detailEditKind) string {
	switch kind {
	case editTitle:
		return "タイトル"
	case editDue:
		return "期限（YYYY-MM-DD。空で削除）"
	case editLinkNote:
		return "リンクメモ（空で削除）"
	case editAddLink:
		return "追加するリンクの URL（空白・改行区切りで複数可）"
	default:
		return ""
	}
}

// DescribeLink spells out a fetched link's state in full, for the detail view and `show`.
func DescribeLink(state fetch.LinkState) string {
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
		return "不明"
	}
}

func titleSuffix(title string) string {
	if title == "" {
		return ""
	}
	return " — " + title
}
