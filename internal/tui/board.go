package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
	"unicode/utf8"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
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

// mode is the screen that has the keyboard. modeBoard, modeDetail and modeAdd are full screens;
// the rest are overlays drawn on top of whichever of those opened them (overlayBack).
type mode int

const (
	modeBoard mode = iota
	modeDetail
	modeAdd
	modeStatusSelect
	modeSessionSelect
	modeJump
	modeConfirm
)

// Board is the kanban board program.
type Board struct {
	ctx      context.Context
	deps     Deps
	settings Settings
	styles   styles

	icons IconSet

	file     *model.File
	sessions SessionStates
	cache    *fetch.CacheFile
	links    map[string]fetch.LinkState
	// snapshot is the last herdr report, kept so the session states can be re-derived when the
	// task list changes rather than only when herdr speaks again.
	snapshot *herdrc.Snapshot

	// tasksLoaded and cacheLoaded gate the startup fetch: which links are stale is only
	// answerable once both the task list and the cache are in, and they arrive in either order.
	tasksLoaded    bool
	cacheLoaded    bool
	startupFetched bool

	columns  []Column
	colIdx   int
	selected map[string]int
	offsets  map[string]int
	// focusTaskID pulls the selection onto a specific task after the next rebuild, so a card
	// stays under the cursor after it moves to another column.
	focusTaskID int

	width  int
	height int

	mode mode
	// overlayBack is the screen the open overlay was launched from, and the one it returns to.
	overlayBack mode

	detail     detailState
	add        addState
	statusSel  statusSelectState
	sessionSel sessionSelectState
	jump       jumpState
	confirm    confirmState

	collapseTerminal bool
	fetching         bool
	backoffSteps     int
	nextBackoff      time.Duration
	stamped          bool

	// shiftEnter records that the terminal answered the keyboard-enhancement query, which is what
	// makes Shift+Enter distinguishable from Enter. Without it the modals fall back to ctrl+j.
	shiftEnter bool

	lastHerdrSync time.Time
	lastFetch     time.Time
	status        string
	statusIsError bool
}

// isTextKey reports whether the event carries literal text to insert.
//
// This is the rule for a screen that owns a text field: an IME commit arrives as key events
// carrying the committed characters, and tea.Key.String() reports that text, so a commit could
// otherwise match a binding by name and be swallowed instead of typed.
func isTextKey(msg tea.KeyPressMsg) bool {
	return msg.Text != ""
}

// isCommandKey reports whether an event may be read as a command on a screen with no text field.
//
// Such a screen has to keep its letter bindings (y/n, a, g, q), so it cannot ignore everything
// carrying text. What it must ignore is text committed several characters at once, which
// String() reports whole: without this, a commit reading "delete" or "esc" would fire the binding
// of that name.
func isCommandKey(msg tea.KeyPressMsg) bool {
	return utf8.RuneCountInString(msg.Text) <= 1
}

// newlineKey names the key the footer advertises for inserting a line break.
//
// Shift+Enter reaches the program as its own key only where the terminal answered the
// keyboard-enhancement query. Elsewhere a terminal configured to send ESC+CR for Shift+Enter is
// indistinguishable from Alt+Enter, which is the truthful name to show when it is not.
func (b *Board) newlineKey() string {
	if b.shiftEnter {
		return "shift+enter"
	}
	return "alt+enter"
}

// isNewlineKey accepts every spelling of the newline key at once.
//
// alt+enter is how ESC+CR arrives, which is what a terminal mapping Shift+Enter to `text:\x1b\r`
// sends, and ctrl+j is the fallback for a terminal that offers neither.
func (b *Board) isNewlineKey(msg tea.KeyPressMsg) bool {
	switch msg.String() {
	case "ctrl+j", "alt+enter":
		return true
	case "shift+enter":
		return b.shiftEnter
	default:
		return false
	}
}

// jumpState is the session picker shown when a task has several linked sessions.
type jumpState struct {
	taskID int
	// title labels the tab a resume creates.
	title    string
	sessions []model.SessionRef
	cursor   int
}

// confirmKind is what a yes/no prompt will do when answered yes.
type confirmKind int

const (
	confirmResume confirmKind = iota
	confirmDeleteTask
	confirmUnlinkLink
	confirmUnlinkSession
)

// confirmState is the yes/no prompt shown before an irreversible or pane-creating action.
type confirmState struct {
	kind   confirmKind
	prompt string
	taskID int
	// title labels the tab a resume creates.
	title   string
	session model.SessionRef
	// ref identifies what an unlink acts on: a link URL or a session id.
	ref string
}

// New builds a board over the given ports.
func New(ctx context.Context, deps Deps, settings Settings) *Board {
	return &Board{
		ctx:      ctx,
		deps:     deps,
		settings: settings,
		styles:   newStyles(),
		icons:    Icons(settings.Icons),
		file:     model.NewFile(),
		cache:    &fetch.CacheFile{Version: 1, Entries: map[string]fetch.CacheEntry{}},
		links:    map[string]fetch.LinkState{},
		// Without a cache there is nothing to wait for before the first fetch.
		cacheLoaded:      deps.Cache == nil,
		selected:         map[string]int{},
		offsets:          map[string]int{},
		collapseTerminal: true,
		width:            80,
		height:           24,
	}
}

// cardStyle is the presentation every card on this board is built with.
func (b *Board) cardStyle() CardStyle {
	return CardStyle{Icons: b.icons, Classifier: b.settings.Classifier}
}

// linkText wraps a link row's text in OSC 8 when hyperlinks are enabled, so a terminal that
// understands the escape opens the URL on a click.
func (b *Board) linkText(url, text string) string {
	if !b.settings.Hyperlinks {
		return text
	}
	return hyperlink(url, text)
}

// newFieldInput builds a text field for the modals. The suggestion bindings are cleared because
// the modals hand item navigation the arrow keys and Tab.
func newFieldInput() textinput.Model {
	input := textinput.New()
	input.Prompt = "> "
	input.KeyMap.NextSuggestion = key.NewBinding()
	input.KeyMap.PrevSuggestion = key.NewBinding()
	input.KeyMap.AcceptSuggestion = key.NewBinding()
	return input
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
// never fire while a modal has the keyboard.
func (b *Board) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		b.width, b.height = msg.Width, msg.Height
		return b, nil

	case tea.KeyPressMsg:
		return b.handleKey(msg)

	case tea.PasteMsg:
		return b.handlePaste(msg)

	case tea.KeyboardEnhancementsMsg:
		b.shiftEnter = msg.SupportsKeyDisambiguation()
		return b, nil

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
		b.cacheLoaded = true
		return b, b.startupFetchCmd()

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

	case agentsLoadedMsg:
		b.applyAgents(msg)
		return b, nil

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
	case modeDetail:
		return b.handleDetailKey(msg)
	case modeAdd:
		return b.handleAddKey(msg)
	case modeStatusSelect:
		return b.handleStatusSelectKey(msg)
	case modeSessionSelect:
		return b.handleSessionSelectKey(msg)
	case modeJump:
		return b.handleJumpKey(msg)
	case modeConfirm:
		return b.handleConfirmKey(msg)
	default:
		return b.handleBoardKey(msg)
	}
}

// handlePaste hands bracketed paste to whichever text field has the keyboard. A PasteMsg is not a
// key press, so without this route it never reaches an input and the paste is silently dropped.
func (b *Board) handlePaste(msg tea.PasteMsg) (tea.Model, tea.Cmd) {
	switch b.mode {
	case modeDetail:
		return b.pasteIntoDetail(msg)
	case modeAdd:
		return b.pasteIntoAdd(msg)
	}
	return b, nil
}

func (b *Board) handleBoardKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if !isCommandKey(msg) {
		return b, nil
	}
	switch msg.String() {
	case "q":
		return b, tea.Quit
	case "left":
		b.moveColumn(-1)
	case "right":
		b.moveColumn(1)
	case "down":
		b.moveCard(1)
	case "up":
		b.moveCard(-1)
	case "tab":
		return b, b.beginStatusSelect()
	case "t":
		b.collapseTerminal = !b.collapseTerminal
		b.rebuild()
	case "enter":
		return b, b.openDetail()
	case "a":
		return b, b.beginAdd()
	case "backspace", "delete":
		return b, b.beginDeleteTask()
	case "g":
		return b, b.beginJump()
	case "r":
		return b, b.refreshTaskCmd()
	case "R":
		return b, b.refreshAllCmd()
	}
	return b, nil
}

// --- overlays ----------------------------------------------------------------

// baseMode is the full screen underneath: the current screen when nothing is layered on it, and
// the screen the overlay was opened from otherwise.
func (b *Board) baseMode() mode {
	switch b.mode {
	case modeStatusSelect, modeSessionSelect, modeJump, modeConfirm:
		return b.overlayBack
	default:
		return b.mode
	}
}

func (b *Board) openOverlay(m mode) {
	b.overlayBack = b.baseMode()
	b.mode = m
}

func (b *Board) closeOverlay() {
	b.mode = b.overlayBack
}

func (b *Board) openConfirm(state confirmState) {
	b.confirm = state
	b.openOverlay(modeConfirm)
}

func (b *Board) handleConfirmKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if !isCommandKey(msg) {
		return b, nil
	}
	switch strings.ToLower(msg.String()) {
	case "y":
		state := b.confirm
		b.closeOverlay()
		return b, b.runConfirm(state)
	case "n", "esc":
		b.closeOverlay()
		b.setStatus("中止した", false)
		return b, nil
	}
	return b, nil
}

func (b *Board) runConfirm(state confirmState) tea.Cmd {
	switch state.kind {
	case confirmResume:
		return b.resumeCmd(state)
	case confirmDeleteTask:
		return b.deleteTaskCmd(state.taskID)
	case confirmUnlinkLink:
		return b.removeLinkCmd(state.taskID, state.ref)
	case confirmUnlinkSession:
		return b.removeSessionCmd(state.taskID, state.ref)
	}
	return nil
}

// --- board actions -----------------------------------------------------------

func (b *Board) openDetail() tea.Cmd {
	task := b.currentTask()
	if task == nil {
		return status("カードが選択されていない", true)
	}
	b.mode = modeDetail
	b.detail = newDetailState(task.ID)
	return nil
}

func (b *Board) beginDeleteTask() tea.Cmd {
	task := b.currentTask()
	if task == nil {
		return status("カードが選択されていない", true)
	}
	b.openConfirm(confirmState{
		kind:   confirmDeleteTask,
		prompt: fmt.Sprintf("#%d %s を削除する", task.ID, task.Title),
		taskID: task.ID,
	})
	return nil
}

// --- selection ---------------------------------------------------------------

// moveColumn walks the cursor to the next expanded column in the given direction.
//
// Folded columns are stepped over rather than landed on: they are drawn in the stack at the right
// edge, hold no card to put a cursor on, and stopping the cursor on one would read as the arrow
// keys having stuck.
func (b *Board) moveColumn(delta int) {
	for i := b.colIdx + delta; i >= 0 && i < len(b.columns); i += delta {
		if b.columns[i].Collapsed {
			continue
		}
		b.colIdx = i
		return
	}
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

// taskByID looks a task up in the board's current data.
func (b *Board) taskByID(id int) *model.Task {
	for i := range b.file.Tasks {
		if b.file.Tasks[i].ID == id {
			task := b.file.Tasks[i]
			return &task
		}
	}
	return nil
}

// detailOpen reports whether the detail modal is the screen in play, overlay or not.
func (b *Board) detailOpen() bool {
	return b.baseMode() == modeDetail
}

// activeTask is the task the keyboard acts on: the one the detail modal is pinned to while it is
// open, and the focused card otherwise. Pinning by id keeps the modal on its own task even when
// an edit moves that card to another column.
func (b *Board) activeTask() *model.Task {
	if b.detailOpen() {
		return b.taskByID(b.detail.taskID)
	}
	return b.currentTask()
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
	// The cursor may be pointing at a column that has just folded, or at one that has gone away
	// with the last task that had an unknown status.
	b.colIdx = nearestExpanded(b.columns, b.colIdx)
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
	// The badge states are derived from the task list together with the cache and the last herdr
	// report, so a new task list means re-deriving both: a link or session that was just added has
	// to pick up its live value now rather than at the next event.
	b.rebuildLinks()
	b.rebuildSessions()
	b.tasksLoaded = true

	// The detail modal is pinned to a task id, so a task deleted underneath it (by the CLI, or
	// by another session) has to close it rather than leave it on nothing.
	if b.detailOpen() && b.taskByID(b.detail.taskID) == nil {
		b.mode = modeBoard
	}

	// A change that may have added a link needs a fetch of its own: waiting for the next
	// background cycle would leave the new link blank for minutes.
	if msg.refresh {
		return b.refreshStaleCmd()
	}
	return b.startupFetchCmd()
}

// startupFetchCmd runs the one-off fetch of everything stale, once both sources are in.
func (b *Board) startupFetchCmd() tea.Cmd {
	if b.startupFetched || !b.tasksLoaded || !b.cacheLoaded {
		return nil
	}
	b.startupFetched = true
	return b.refreshStaleCmd()
}

func (b *Board) applySessionUpdate(update herdrc.Update) tea.Cmd {
	cmds := []tea.Cmd{waitSessionUpdate(b.deps.Sessions)}

	if !update.Status.Available {
		b.snapshot = nil
		b.sessions = UnavailableSessions(update.Status.Err)
		return tea.Batch(cmds...)
	}
	b.snapshot = update.Snapshot
	b.rebuildSessions()
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
	b.cache = cache
	b.rebuildLinks()
}

// rebuildLinks re-derives the displayed link states from the cache the board holds.
func (b *Board) rebuildLinks() {
	b.links = b.cache.LinkStates(allLinks(b.file.Tasks), b.deps.now(), b.settings.CacheTTL)
}

// rebuildSessions re-derives the live session states from the last herdr report. The states are
// keyed by the sessions the tasks carry, so a task list that just gained one has to re-derive them
// or the new row reads offline until herdr next speaks.
func (b *Board) rebuildSessions() {
	if b.snapshot == nil {
		return
	}
	b.sessions = BuildSessionStates(b.snapshot, b.file.Tasks)
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
		// The reason is included rather than just the count: a count alone is what let a run of
		// wrong-account 404s look like ordinary noise for as long as it did.
		b.setStatus(fmt.Sprintf("%d 件取得（%d 件失敗）: %s",
			len(msg.result.Outcomes)-failed, failed, firstFailureReason(msg.result)), true)
	case msg.manual:
		b.setStatus(fmt.Sprintf("%d 件取得した", len(msg.result.Outcomes)), false)
	}
	return nil
}

// firstFailureReason is the first line of the first failure in a cycle, which is the part of a gh
// or Jira error that says what went wrong; the rest is the guidance the CLI prints in full.
func firstFailureReason(result *fetch.RefreshResult) string {
	for _, outcome := range result.Outcomes {
		if outcome.Err == nil {
			continue
		}
		line, _, _ := strings.Cut(outcome.Err.Error(), "\n")
		return strings.TrimSpace(line)
	}
	return ""
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

// --- mutations ---------------------------------------------------------------

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

// selectableColumns are the columns a task can actually hold as a status: every built column
// except the synthetic (unknown) one, terminal columns included.
func selectableColumns(columns []Column) []Column {
	targets := make([]Column, 0, len(columns))
	for _, col := range columns {
		if !col.Unknown {
			targets = append(targets, col)
		}
	}
	return targets
}

func statusIndex(targets []Column, status string) int {
	for i, col := range targets {
		if col.ID == status {
			return i
		}
	}
	return -1
}

// addTasksCmd creates every title in one transaction, so a batch paste takes the store lock once
// and either lands whole or not at all. The remaining attributes, links included, apply to all of
// them.
func (b *Board) addTasksCmd(titles []string, in model.TaskInput, urls []string) tea.Cmd {
	if len(titles) == 0 {
		return nil
	}
	now := b.deps.now()
	var created []int
	return b.mutateCmd(mutation{
		apply: func(f *model.File) error {
			created = created[:0]
			for _, title := range titles {
				spec := in
				spec.Title = title
				task, err := f.AddTask(spec, now)
				if err != nil {
					return err
				}
				for _, url := range urls {
					if _, err := task.AddLink(url, b.settings.Classifier.Classify(url), "", now); err != nil {
						return err
					}
				}
				created = append(created, task.ID)
			}
			return nil
		},
		note: func() string {
			if len(created) == 1 {
				return fmt.Sprintf("#%d を %s に作成した", created[0], in.Status)
			}
			return fmt.Sprintf("%d 件のタスクを %s に作成した", len(created), in.Status)
		},
		focus: func() int {
			if len(created) == 0 {
				return 0
			}
			return created[0]
		},
		refresh: true,
	})
}

// addLinksCmd attaches a whole batch of URLs at once. A URL already on the task is skipped rather
// than failing the batch: re-pasting a list that overlaps what is there should not be an error.
func (b *Board) addLinksCmd(taskID int, urls []string) tea.Cmd {
	if len(urls) == 0 {
		return nil
	}
	now := b.deps.now()
	added := 0
	return b.mutateCmd(mutation{
		apply: func(f *model.File) error {
			target, err := f.Task(taskID)
			if err != nil {
				return err
			}
			added = 0
			for _, url := range urls {
				if _, exists := linkByURL(target.Links, url); exists {
					continue
				}
				if _, err := target.AddLink(url, b.settings.Classifier.Classify(url), "", now); err != nil {
					return err
				}
				added++
			}
			return nil
		},
		note: func() string {
			if added == len(urls) {
				return fmt.Sprintf("#%d に %d 件のリンクを追加した", taskID, added)
			}
			return fmt.Sprintf("#%d に %d 件のリンクを追加した（%d 件は登録済み）", taskID, added, len(urls)-added)
		},
		refresh: true,
	})
}

func (b *Board) setTitleCmd(taskID int, title string) tea.Cmd {
	if strings.TrimSpace(title) == "" {
		return status("タイトルは空にできない", true)
	}
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

func (b *Board) setDueCmd(taskID int, raw string) tea.Cmd {
	var due *model.Date
	if trimmed := strings.TrimSpace(raw); trimmed != "" {
		parsed, err := model.ParseDate(trimmed)
		if err != nil {
			return status(err.Error(), true)
		}
		due = &parsed
	}

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

func (b *Board) setStatusCmd(taskID int, statusID string) tea.Cmd {
	now := b.deps.now()
	return b.mutateCmd(mutation{
		apply: func(f *model.File) error {
			target, err := f.Task(taskID)
			if err != nil {
				return err
			}
			return target.SetStatus(statusID, now)
		},
		note:  func() string { return fmt.Sprintf("#%d を %s へ移動した", taskID, statusID) },
		focus: func() int { return taskID },
	})
}

func (b *Board) setLinkNoteCmd(taskID int, url, note string) tea.Cmd {
	now := b.deps.now()
	return b.mutateCmd(mutation{
		apply: func(f *model.File) error {
			target, err := f.Task(taskID)
			if err != nil {
				return err
			}
			return target.SetLinkNote(url, note, now)
		},
		note: func() string { return fmt.Sprintf("#%d のリンクメモを更新した", taskID) },
	})
}

func (b *Board) deleteTaskCmd(taskID int) tea.Cmd {
	return b.mutateCmd(mutation{
		apply: func(f *model.File) error {
			_, err := f.RemoveTask(taskID)
			return err
		},
		note: func() string { return fmt.Sprintf("#%d を削除した", taskID) },
	})
}

func (b *Board) removeLinkCmd(taskID int, url string) tea.Cmd {
	now := b.deps.now()
	return b.mutateCmd(mutation{
		apply: func(f *model.File) error {
			target, err := f.Task(taskID)
			if err != nil {
				return err
			}
			_, err = target.RemoveLink(url, now)
			return err
		},
		note: func() string { return fmt.Sprintf("#%d のリンクを解除した", taskID) },
	})
}

func (b *Board) removeSessionCmd(taskID int, sessionID string) tea.Cmd {
	now := b.deps.now()
	return b.mutateCmd(mutation{
		apply: func(f *model.File) error {
			target, err := f.Task(taskID)
			if err != nil {
				return err
			}
			_, err = target.RemoveSession(sessionID, now)
			return err
		},
		note: func() string { return fmt.Sprintf("#%d のセッション紐づけを解除した", taskID) },
	})
}

func (b *Board) addSessionCmd(taskID int, ref model.SessionRef) tea.Cmd {
	now := b.deps.now()
	return b.mutateCmd(mutation{
		apply: func(f *model.File) error {
			target, err := f.Task(taskID)
			if err != nil {
				return err
			}
			_, err = target.AddSession(ref, now)
			return err
		},
		note: func() string { return fmt.Sprintf("#%d に %s セッションを紐づけた", taskID, ref.Agent) },
	})
}

// editNoteCmd opens the task's note in the configured editor, suspending the board while it runs.
func (b *Board) editNoteCmd() tea.Cmd {
	task := b.activeTask()
	if task == nil {
		return status("カードが選択されていない", true)
	}
	editor := b.settings.Editor
	if editor == "" {
		return status("エディタが設定されていない（config.toml の editor / $VISUAL / $EDITOR）", true)
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

func linkByURL(links []model.Link, url string) (model.Link, bool) {
	for _, link := range links {
		if link.URL == url {
			return link, true
		}
	}
	return model.Link{}, false
}

func allLinks(tasks []model.Task) []model.Link {
	var links []model.Link
	for i := range tasks {
		links = append(links, tasks[i].Links...)
	}
	return links
}
