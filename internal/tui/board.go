package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/ukwhatn/taskherd/internal/fetch"
	"github.com/ukwhatn/taskherd/internal/herdrc"
	"github.com/ukwhatn/taskherd/internal/model"
)

// Backoff bounds for the background refresh cycle. A rate-limited cycle stretches the interval
// instead of hammering the provider that just refused.
const (
	maxBackoffSteps   = 5
	maxRefreshBackoff = 2 * time.Hour
)

// resumeAgent is the only agent taskherd knows how to resume, matching the CLI's jump.
const resumeAgent = "claude"

type mode int

const (
	modeBoard mode = iota
	modeDetail
	modeInput
	modeJump
	modeConfirm
)

type inputKind int

const (
	inputAddTask inputKind = iota
	inputAddLink
	inputEditTitle
	inputEditDue
)

// Board is the kanban board program.
type Board struct {
	ctx      context.Context
	deps     Deps
	settings Settings
	styles   styles

	file     *model.File
	sessions SessionStates
	links    map[string]fetch.LinkState

	columns  []Column
	colIdx   int
	selected map[string]int
	offsets  map[string]int
	// focusTaskID pulls the selection onto a specific task after the next rebuild, so a card
	// stays under the cursor after it moves to another column.
	focusTaskID int

	width  int
	height int

	mode      mode
	input     textinput.Model
	inputKind inputKind
	detail    viewport.Model
	jump      jumpState
	confirm   confirmState

	collapseTerminal bool
	fetching         bool
	backoffSteps     int
	nextBackoff      time.Duration
	stamped          bool

	lastHerdrSync time.Time
	lastFetch     time.Time
	status        string
	statusIsError bool
}

// jumpState is the session picker shown when a task has several linked sessions.
type jumpState struct {
	taskID int
	// title labels the tab a resume creates.
	title    string
	sessions []model.SessionRef
	cursor   int
}

// confirmState is the yes/no prompt shown before a resume launches a new pane.
type confirmState struct {
	prompt string
	taskID int
	// title labels the tab the resume creates.
	title   string
	session model.SessionRef
}

// New builds a board over the given ports.
func New(ctx context.Context, deps Deps, settings Settings) *Board {
	input := textinput.New()
	input.Prompt = "> "

	return &Board{
		ctx:              ctx,
		deps:             deps,
		settings:         settings,
		styles:           newStyles(),
		file:             model.NewFile(),
		links:            map[string]fetch.LinkState{},
		selected:         map[string]int{},
		offsets:          map[string]int{},
		input:            input,
		detail:           viewport.New(),
		collapseTerminal: true,
		width:            80,
		height:           24,
	}
}

// Init loads the initial data and starts listening to every live source.
func (b *Board) Init() tea.Cmd {
	cmds := []tea.Cmd{b.loadTasksCmd("")}
	if b.deps.Cache != nil {
		cmds = append(cmds, b.loadCacheCmd())
	}
	if b.deps.Files != nil {
		cmds = append(cmds, waitFileEvent(b.deps.Files))
	}
	if b.deps.Sessions != nil {
		cmds = append(cmds, waitSessionUpdate(b.deps.Sessions))
	}
	if b.deps.Links != nil {
		cmds = append(cmds, b.tickCmd())
	}
	return tea.Batch(cmds...)
}

// Update routes a message. Key handling is delegated per mode so that the board's own bindings
// never fire while a prompt has the keyboard.
func (b *Board) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		b.width, b.height = msg.Width, msg.Height
		b.resizeDetail()
		return b, nil

	case tea.KeyPressMsg:
		return b.handleKey(msg)

	case tasksLoadedMsg:
		return b, b.applyTasks(msg)

	case fileEventMsg:
		if msg.closed {
			return b, nil
		}
		return b, tea.Batch(b.loadTasksCmd(""), waitFileEvent(b.deps.Files))

	case sessionUpdateMsg:
		if msg.closed {
			return b, nil
		}
		return b, b.applySessionUpdate(msg.update)

	case cacheLoadedMsg:
		b.applyCache(msg.cache)
		if msg.initial {
			return b, b.refreshStaleCmd()
		}
		return b, nil

	case refreshDoneMsg:
		return b, b.applyRefresh(msg)

	case refreshTickMsg:
		cmds := []tea.Cmd{b.tickCmd()}
		if !b.fetching {
			cmds = append(cmds, b.refreshStaleCmd())
		}
		return b, tea.Batch(cmds...)

	case editorDoneMsg:
		return b, b.applyEditor(msg)

	case statusMsg:
		b.setStatus(msg.text, msg.isError)
		return b, nil
	}
	return b, nil
}

func (b *Board) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" {
		return b, tea.Quit
	}
	switch b.mode {
	case modeInput:
		return b.handleInputKey(msg)
	case modeJump:
		return b.handleJumpKey(msg)
	case modeConfirm:
		return b.handleConfirmKey(msg)
	case modeDetail:
		return b.handleDetailKey(msg)
	default:
		return b.handleBoardKey(msg)
	}
}

func (b *Board) handleBoardKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q":
		return b, tea.Quit
	case "h", "left":
		b.moveColumn(-1)
	case "l", "right":
		b.moveColumn(1)
	case "j", "down":
		b.moveCard(1)
	case "k", "up":
		b.moveCard(-1)
	case "H":
		return b, b.moveTaskCmd(-1)
	case "L":
		return b, b.moveTaskCmd(1)
	case "t":
		b.collapseTerminal = !b.collapseTerminal
		b.rebuild()
	case "enter":
		if b.currentTask() == nil {
			b.setStatus("カードが選択されていない", true)
			return b, nil
		}
		b.mode = modeDetail
		b.detail.SetYOffset(0)
		b.resizeDetail()
	case "a":
		b.beginInput(inputAddTask)
	case "x":
		if b.currentTask() == nil {
			b.setStatus("カードが選択されていない", true)
			return b, nil
		}
		b.beginInput(inputAddLink)
	case "n":
		return b, b.editNoteCmd()
	case "g":
		return b, b.beginJump()
	case "r":
		return b, b.refreshTaskCmd()
	case "R":
		return b, b.refreshAllCmd()
	}
	return b, nil
}

func (b *Board) handleDetailKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		b.mode = modeBoard
		return b, nil
	case "e":
		b.beginInput(inputEditTitle)
		return b, nil
	case "d":
		b.beginInput(inputEditDue)
		return b, nil
	case "x":
		b.beginInput(inputAddLink)
		return b, nil
	case "n":
		return b, b.editNoteCmd()
	case "g":
		return b, b.beginJump()
	case "r":
		return b, b.refreshTaskCmd()
	}

	updated, cmd := b.detail.Update(msg)
	b.detail = updated
	return b, cmd
}

func (b *Board) handleInputKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		b.mode = b.inputReturnMode()
		b.input.Blur()
		return b, nil
	case "enter":
		value := strings.TrimSpace(b.input.Value())
		kind := b.inputKind
		b.mode = b.inputReturnMode()
		b.input.Blur()
		return b, b.submitInput(kind, value)
	}

	updated, cmd := b.input.Update(msg)
	b.input = updated
	return b, cmd
}

func (b *Board) handleJumpKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		b.mode = modeBoard
		return b, nil
	case "j", "down":
		if b.jump.cursor < len(b.jump.sessions)-1 {
			b.jump.cursor++
		}
		return b, nil
	case "k", "up":
		if b.jump.cursor > 0 {
			b.jump.cursor--
		}
		return b, nil
	case "enter":
		target := b.jump
		b.mode = modeBoard
		return b, b.jumpTo(target.taskID, target.title, target.sessions[target.cursor])
	}
	return b, nil
}

func (b *Board) handleConfirmKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch strings.ToLower(msg.String()) {
	case "y":
		state := b.confirm
		b.mode = modeBoard
		return b, b.resumeCmd(state)
	case "n", "esc", "q":
		b.mode = modeBoard
		b.setStatus("中止した", false)
		return b, nil
	}
	return b, nil
}

// inputReturnMode is the mode a prompt returns to: prompts opened from the detail view go back
// to it rather than dropping the user out to the board.
func (b *Board) inputReturnMode() mode {
	switch b.inputKind {
	case inputEditTitle, inputEditDue:
		return modeDetail
	default:
		return modeBoard
	}
}

// --- selection ---------------------------------------------------------------

func (b *Board) moveColumn(delta int) {
	if len(b.columns) == 0 {
		return
	}
	next := b.colIdx + delta
	if next < 0 || next >= len(b.columns) {
		return
	}
	b.colIdx = next
}

func (b *Board) moveCard(delta int) {
	col, ok := b.currentColumn()
	if !ok || len(col.Tasks) == 0 {
		return
	}
	next := b.selectedIndex(col) + delta
	if next < 0 || next >= len(col.Tasks) {
		return
	}
	b.selected[col.Key()] = next
}

func (b *Board) currentColumn() (Column, bool) {
	if b.colIdx < 0 || b.colIdx >= len(b.columns) {
		return Column{}, false
	}
	return b.columns[b.colIdx], true
}

// selectedIndex clamps the remembered selection to the column's current contents: the column may
// have shrunk since the cursor was last there.
func (b *Board) selectedIndex(col Column) int {
	idx := b.selected[col.Key()]
	if idx >= len(col.Tasks) {
		idx = len(col.Tasks) - 1
	}
	if idx < 0 {
		idx = 0
	}
	return idx
}

func (b *Board) currentTask() *model.Task {
	col, ok := b.currentColumn()
	if !ok || len(col.Tasks) == 0 || col.Collapsed {
		return nil
	}
	task := col.Tasks[b.selectedIndex(col)]
	return &task
}

// rebuild recomputes the columns from the current data and re-anchors the selection.
func (b *Board) rebuild() {
	b.columns = BuildColumns(b.file.Tasks, b.settings.Columns, b.collapseTerminal)

	if b.focusTaskID != 0 {
		if col, row, ok := findTask(b.columns, b.focusTaskID); ok {
			b.colIdx = col
			b.selected[b.columns[col].Key()] = row
		}
		b.focusTaskID = 0
	}
	if b.colIdx >= len(b.columns) {
		b.colIdx = len(b.columns) - 1
	}
	if b.colIdx < 0 {
		b.colIdx = 0
	}
}

func (b *Board) resizeDetail() {
	width := b.width - 2
	if width < 10 {
		width = 10
	}
	height := b.height - 4
	if height < 3 {
		height = 3
	}
	b.detail.SetWidth(width)
	b.detail.SetHeight(height)
}

func (b *Board) setStatus(text string, isError bool) {
	b.status, b.statusIsError = text, isError
}

// --- data application --------------------------------------------------------

func (b *Board) applyTasks(msg tasksLoadedMsg) tea.Cmd {
	if msg.err != nil {
		b.setStatus(msg.err.Error(), true)
		return nil
	}
	b.file = msg.file
	if msg.note != "" {
		b.setStatus(msg.note, false)
	}
	if msg.focus != 0 {
		b.focusTaskID = msg.focus
	}
	b.rebuild()

	// A task added or linked since the last cycle has no cached status yet, and a link removed
	// from every task no longer needs one.
	if msg.refresh {
		return b.refreshStaleCmd()
	}
	return nil
}

func (b *Board) applySessionUpdate(update herdrc.Update) tea.Cmd {
	cmds := []tea.Cmd{waitSessionUpdate(b.deps.Sessions)}

	if !update.Status.Available {
		b.sessions = UnavailableSessions(update.Status.Err)
		return tea.Batch(cmds...)
	}
	b.sessions = BuildSessionStates(update.Snapshot, b.file.Tasks)
	b.lastHerdrSync = b.deps.now()

	// The task id stamped onto a pane expires after 24h, so the board re-stamps the panes it
	// finds on the first snapshot it gets (§7.5). tasks.json stays the source of truth.
	if !b.stamped && b.deps.Herdr != nil {
		b.stamped = true
		if cmd := b.stampTokensCmd(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return tea.Batch(cmds...)
}

func (b *Board) applyCache(cache *fetch.CacheFile) {
	if cache == nil {
		return
	}
	b.links = cache.LinkStates(allLinks(b.file.Tasks), b.deps.now(), b.settings.CacheTTL)
}

func (b *Board) applyRefresh(msg refreshDoneMsg) tea.Cmd {
	b.fetching = false
	b.applyCache(msg.cache)

	if msg.err != nil {
		b.setStatus(msg.err.Error(), true)
		return nil
	}
	if msg.result == nil {
		return nil
	}
	b.lastFetch = msg.at

	interrupted := msg.result.GitHubInterrupted || msg.result.JiraInterrupted
	if interrupted {
		b.growBackoff(msg.result)
	} else {
		b.backoffSteps, b.nextBackoff = 0, 0
	}

	failed := 0
	for _, outcome := range msg.result.Outcomes {
		if outcome.Err != nil {
			failed++
		}
	}
	switch {
	case interrupted:
		b.setStatus(fmt.Sprintf("レート制限で中断した。次回取得を %s 後に延ばす", b.refreshInterval()), true)
	case failed > 0:
		b.setStatus(fmt.Sprintf("%d 件取得（%d 件失敗）", len(msg.result.Outcomes)-failed, failed), true)
	case msg.manual:
		b.setStatus(fmt.Sprintf("%d 件取得した", len(msg.result.Outcomes)), false)
	}
	return nil
}

// growBackoff stretches the background interval after a rate limit. Jira's own Retry-After wins
// when it asks for longer than the backoff would wait anyway.
func (b *Board) growBackoff(result *fetch.RefreshResult) {
	if b.backoffSteps < maxBackoffSteps {
		b.backoffSteps++
	}
	b.nextBackoff = 0
	for _, outcome := range result.Outcomes {
		if retryAfter := jiraRetryAfter(outcome.Err); retryAfter > b.nextBackoff {
			b.nextBackoff = retryAfter
		}
	}
}

// refreshInterval is the wait before the next background cycle, including any backoff.
func (b *Board) refreshInterval() time.Duration {
	interval := b.settings.RefreshInterval
	for i := 0; i < b.backoffSteps; i++ {
		interval *= 2
		if interval > maxRefreshBackoff {
			interval = maxRefreshBackoff
			break
		}
	}
	if b.nextBackoff > interval {
		return b.nextBackoff
	}
	return interval
}

func (b *Board) applyEditor(msg editorDoneMsg) tea.Cmd {
	defer func() {
		_ = os.Remove(msg.path)
	}()

	if msg.err != nil {
		b.setStatus(fmt.Sprintf("エディタの起動に失敗した: %v", msg.err), true)
		return nil
	}
	data, err := os.ReadFile(msg.path)
	if err != nil {
		b.setStatus(fmt.Sprintf("編集結果を読めない: %v", err), true)
		return nil
	}
	note := strings.TrimRight(string(data), "\n")

	taskID := msg.taskID
	return b.mutateCmd(mutation{
		apply: func(f *model.File) error {
			task, err := f.Task(taskID)
			if err != nil {
				return err
			}
			task.SetNote(note, b.deps.now())
			return nil
		},
		note: func() string { return fmt.Sprintf("#%d の note を更新した", taskID) },
	})
}

// --- input submission --------------------------------------------------------

func (b *Board) beginInput(kind inputKind) {
	b.inputKind = kind
	b.mode = modeInput
	b.input.Reset()

	task := b.currentTask()
	switch kind {
	case inputEditTitle:
		if task != nil {
			b.input.SetValue(task.Title)
		}
	case inputEditDue:
		if task != nil && task.Due != nil {
			b.input.SetValue(string(*task.Due))
		}
	}
	b.input.CursorEnd()
	b.input.Focus()
}

func (b *Board) inputPrompt() string {
	switch b.inputKind {
	case inputAddTask:
		col, ok := b.targetColumn()
		if !ok {
			return "新しいタスクのタイトル"
		}
		return fmt.Sprintf("新しいタスクのタイトル（%s）", col.Label)
	case inputAddLink:
		return "追加するリンクの URL"
	case inputEditTitle:
		return "タイトル"
	case inputEditDue:
		return "期日（YYYY-MM-DD。空で削除）"
	default:
		return ""
	}
}

// targetColumn is the column a new task lands in. The (unknown) column is not a status a task can
// be created with, so the first real column stands in for it.
func (b *Board) targetColumn() (Column, bool) {
	if col, ok := b.currentColumn(); ok && !col.Unknown {
		return col, true
	}
	for _, col := range b.columns {
		if !col.Unknown {
			return col, true
		}
	}
	return Column{}, false
}

func (b *Board) submitInput(kind inputKind, value string) tea.Cmd {
	switch kind {
	case inputAddTask:
		return b.addTaskCmd(value)
	case inputAddLink:
		return b.addLinkCmd(value)
	case inputEditTitle:
		return b.editTitleCmd(value)
	case inputEditDue:
		return b.editDueCmd(value)
	}
	return nil
}

func (b *Board) addTaskCmd(title string) tea.Cmd {
	if title == "" {
		return nil
	}
	col, ok := b.targetColumn()
	if !ok {
		return status("列が定義されていないためタスクを作成できない", true)
	}

	now := b.deps.now()
	created := 0
	return b.mutateCmd(mutation{
		apply: func(f *model.File) error {
			task, err := f.AddTask(model.TaskInput{Title: title, Status: col.ID}, now)
			if err != nil {
				return err
			}
			created = task.ID
			return nil
		},
		note:  func() string { return fmt.Sprintf("#%d を %s に作成した", created, col.ID) },
		focus: func() int { return created },
	})
}

func (b *Board) addLinkCmd(rawURL string) tea.Cmd {
	if rawURL == "" {
		return nil
	}
	task := b.currentTask()
	if task == nil {
		return status("カードが選択されていない", true)
	}
	if !strings.Contains(rawURL, "://") {
		return status("URL はスキームを含めて指定する（例: https://github.com/owner/repo/pull/1）", true)
	}

	taskID := task.ID
	kind := b.settings.Classifier.Classify(rawURL)
	now := b.deps.now()
	return b.mutateCmd(mutation{
		apply: func(f *model.File) error {
			target, err := f.Task(taskID)
			if err != nil {
				return err
			}
			_, err = target.AddLink(rawURL, kind, "", now)
			return err
		},
		note:    func() string { return fmt.Sprintf("#%d に %s リンクを追加した", taskID, kind) },
		refresh: true,
	})
}

func (b *Board) editTitleCmd(title string) tea.Cmd {
	task := b.currentTask()
	if task == nil || title == "" {
		return nil
	}
	taskID := task.ID
	now := b.deps.now()
	return b.mutateCmd(mutation{
		apply: func(f *model.File) error {
			target, err := f.Task(taskID)
			if err != nil {
				return err
			}
			return target.SetTitle(title, now)
		},
		note: func() string { return fmt.Sprintf("#%d のタイトルを更新した", taskID) },
	})
}

func (b *Board) editDueCmd(raw string) tea.Cmd {
	task := b.currentTask()
	if task == nil {
		return nil
	}

	var due *model.Date
	if raw != "" {
		parsed, err := model.ParseDate(raw)
		if err != nil {
			return status(err.Error(), true)
		}
		due = &parsed
	}

	taskID := task.ID
	now := b.deps.now()
	return b.mutateCmd(mutation{
		apply: func(f *model.File) error {
			target, err := f.Task(taskID)
			if err != nil {
				return err
			}
			target.SetDue(due, now)
			return nil
		},
		note: func() string { return fmt.Sprintf("#%d の期日を更新した", taskID) },
	})
}

// moveTaskCmd shifts the focused card into the neighbouring column, changing its status.
func (b *Board) moveTaskCmd(delta int) tea.Cmd {
	task := b.currentTask()
	if task == nil {
		return nil
	}
	target, ok := moveTarget(b.columns, b.colIdx, delta)
	if !ok {
		return nil
	}

	taskID := task.ID
	now := b.deps.now()
	return b.mutateCmd(mutation{
		apply: func(f *model.File) error {
			moved, err := f.Task(taskID)
			if err != nil {
				return err
			}
			return moved.SetStatus(target.ID, now)
		},
		note:  func() string { return fmt.Sprintf("#%d を %s へ移動した", taskID, target.ID) },
		focus: func() int { return taskID },
	})
}

// editNoteCmd opens the focused task's note in $EDITOR, suspending the board while it runs.
func (b *Board) editNoteCmd() tea.Cmd {
	task := b.currentTask()
	if task == nil {
		return status("カードが選択されていない", true)
	}

	editor := b.deps.getenv("VISUAL")
	if editor == "" {
		editor = b.deps.getenv("EDITOR")
	}
	if editor == "" {
		return status("$EDITOR が設定されていない", true)
	}

	path, err := writeTempNote(task.ID, task.Note)
	if err != nil {
		return status(err.Error(), true)
	}
	argv := strings.Fields(editor)
	taskID := task.ID

	command := exec.Command(argv[0], append(argv[1:], path)...)
	return tea.ExecProcess(command, func(err error) tea.Msg {
		return editorDoneMsg{taskID: taskID, path: path, err: err}
	})
}

func writeTempNote(id int, note string) (string, error) {
	tmp, err := os.CreateTemp("", fmt.Sprintf("taskherd-note-%d-*.md", id))
	if err != nil {
		return "", fmt.Errorf("一時ファイルを作れない: %w", err)
	}
	if _, err := tmp.WriteString(note); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return "", fmt.Errorf("一時ファイルに書けない: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return "", fmt.Errorf("一時ファイルを閉じられない: %w", err)
	}
	return tmp.Name(), nil
}

func allLinks(tasks []model.Task) []model.Link {
	var links []model.Link
	for i := range tasks {
		links = append(links, tasks[i].Links...)
	}
	return links
}
