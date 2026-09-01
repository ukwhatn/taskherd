package tui

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/ukwhatn/taskherd/internal/herdrc"
	"github.com/ukwhatn/taskherd/internal/i18n"
	"github.com/ukwhatn/taskherd/internal/model"
)

// PickerTaskStore is the tasks.json access the picker needs: the task list to choose from, and
// the mutation that links the chosen one to a session.
type PickerTaskStore interface {
	Load() (*model.File, error)
	Update(ctx context.Context, fn func(*model.File) error) error
}

// PickerHerdrOps are the herdr operations the picker performs: resolving the agent in the target
// pane, and stamping the linked task id back onto it.
type PickerHerdrOps interface {
	Snapshot(ctx context.Context) (*herdrc.Snapshot, error)
	ReportTaskDisplay(ctx context.Context, paneID string, taskID int, title string) error
}

// PickerDeps are the picker's ports on the outside world.
type PickerDeps struct {
	Tasks   PickerTaskStore
	Herdr   PickerHerdrOps
	Columns model.Columns
	// Icons is the glyph vocabulary the popup draws with, shared with the board.
	Icons IconMode
	// Text is the language the popup draws in. Nil falls back to the default catalog.
	Text *i18n.Catalog
	Now  func() time.Time
}

func (d PickerDeps) now() time.Time {
	if d.Now == nil {
		return time.Now()
	}
	return d.Now()
}

// RunPicker starts the picker popup and blocks until it links a task or the user cancels.
func RunPicker(ctx context.Context, deps PickerDeps, targetPane string) error {
	if deps.Tasks == nil {
		return errors.New("no task store is configured")
	}
	if targetPane == "" {
		return errors.New("no target pane was given")
	}

	program := tea.NewProgram(newPicker(ctx, deps, targetPane), tea.WithContext(ctx))
	if _, err := program.Run(); err != nil {
		return fmt.Errorf("cannot run the picker: %w", err)
	}
	return nil
}

// picker is the popup's own small model: a filterable flat list of tasks, one action (link the
// selected task to targetPane), and an error line for what board's detail view would otherwise
// show inline (an undetected agent, a store failure).
type picker struct {
	ctx        context.Context
	deps       PickerDeps
	styles     styles
	icons      IconSet
	text       *i18n.Catalog
	targetPane string

	filter textinput.Model

	tasks    []model.Task // every task, sorted for display
	filtered []int        // indexes into tasks matching the current filter
	cursor   int
	// offset is the first visible row of the list, kept between renders so a list that need not
	// scroll does not jump under the selection.
	offset int

	width, height int

	loaded  bool
	loadErr error

	linking bool
	status  string
	isError bool
	linked  bool
}

func newPicker(ctx context.Context, deps PickerDeps, targetPane string) *picker {
	filter := textinput.New()
	text := i18n.OrDefault(deps.Text)
	filter.Prompt = text.Picker.FilterPrompt
	filter.Focus()

	p := &picker{
		ctx:        ctx,
		deps:       deps,
		styles:     newStyles(),
		icons:      Icons(deps.Icons),
		text:       text,
		targetPane: targetPane,
		filter:     filter,
		width:      60,
		height:     20,
	}
	p.resizeFilter()
	return p
}

// resizeFilter fits the filter to the popup's width.
//
// The value is written back onto the model afterwards because textinput recomputes its horizontal
// scroll window when the value changes, never when the width does: without this, a popup that
// starts or is resized narrower keeps drawing the wider window and overflows the row.
func (p *picker) resizeFilter() {
	p.filter.SetWidth(fieldWidth(p.filter, p.width))
	p.filter.SetValue(p.filter.Value())
}

func (p *picker) Init() tea.Cmd {
	return p.loadTasksCmd()
}

type pickerTasksLoadedMsg struct {
	tasks []model.Task
	err   error
}

type pickerLinkedMsg struct {
	task *model.Task
	err  error
}

func (p *picker) loadTasksCmd() tea.Cmd {
	return func() tea.Msg {
		file, err := p.deps.Tasks.Load()
		if err != nil {
			return pickerTasksLoadedMsg{err: err}
		}
		return pickerTasksLoadedMsg{tasks: file.Tasks}
	}
}

func (p *picker) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.width, p.height = msg.Width, msg.Height
		p.resizeFilter()
		return p, nil

	case tea.KeyPressMsg:
		return p.handleKey(msg)

	case tea.PasteMsg:
		// A paste is not a key press, so it has to be routed to the filter explicitly or it is
		// dropped on the floor.
		if p.linking {
			return p, nil
		}
		updated, cmd := p.filter.Update(msg)
		p.filter = updated
		p.applyFilter()
		return p, cmd

	case pickerTasksLoadedMsg:
		if msg.err != nil {
			p.loadErr = msg.err
			return p, nil
		}
		p.loaded = true
		p.tasks = sortedForPicker(msg.tasks, p.deps.Columns)
		p.applyFilter()
		return p, nil

	case pickerLinkedMsg:
		p.linking = false
		if msg.err != nil {
			p.status, p.isError = p.message(msg.err), true
			return p, nil
		}
		p.linked = true
		p.status = fmt.Sprintf(p.text.Picker.Attached, msg.task.ID)
		return p, tea.Quit
	}
	return p, nil
}

func (p *picker) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// The filter is always focused, so command keys are matched only on text-less events: an IME
	// commit arrives as text and belongs in the filter, not in a binding.
	if !isTextKey(msg) {
		switch msg.String() {
		case "ctrl+c", "esc":
			return p, tea.Quit
		case "up":
			if p.cursor > 0 {
				p.cursor--
			}
			return p, nil
		case "down":
			if p.cursor < len(p.filtered)-1 {
				p.cursor++
			}
			return p, nil
		case "enter":
			return p, p.linkSelectedCmd()
		}
	}

	if p.linking {
		// The filter keeps rendering while a link is in flight, but editing it while the
		// result is still pending would move the selection out from under it.
		return p, nil
	}

	updated, cmd := p.filter.Update(msg)
	p.filter = updated
	p.applyFilter()
	return p, cmd
}

// applyFilter recomputes which tasks match the filter text and clamps the cursor onto them.
func (p *picker) applyFilter() {
	query := strings.ToLower(strings.TrimSpace(p.filter.Value()))
	p.filtered = p.filtered[:0]
	for i, task := range p.tasks {
		if query == "" || matchesQuery(task, query) {
			p.filtered = append(p.filtered, i)
		}
	}
	if p.cursor >= len(p.filtered) {
		p.cursor = len(p.filtered) - 1
	}
	if p.cursor < 0 {
		p.cursor = 0
	}
}

func matchesQuery(task model.Task, query string) bool {
	if strings.Contains(strings.ToLower(task.Title), query) {
		return true
	}
	return strings.Contains(strconv.Itoa(task.ID), strings.TrimPrefix(query, "#"))
}

func (p *picker) selected() (model.Task, bool) {
	if p.cursor < 0 || p.cursor >= len(p.filtered) {
		return model.Task{}, false
	}
	return p.tasks[p.filtered[p.cursor]], true
}

// linkSelectedCmd resolves the agent herdr currently detects in targetPane and links its session
// to the selected task. This mirrors `session link --pane` (internal/cli/session_cmd.go's
// resolveSession): the picker cannot import the cli package, so the short pane-to-session
// resolution is repeated here against the same public herdrc.Snapshot API.
func (p *picker) linkSelectedCmd() tea.Cmd {
	task, ok := p.selected()
	if !ok {
		return nil
	}
	if p.linking {
		return nil
	}
	p.linking = true
	p.status, p.isError = "", false

	taskID := task.ID
	targetPane := p.targetPane
	now := p.deps.now()

	return func() tea.Msg {
		if p.deps.Herdr == nil {
			return pickerLinkedMsg{err: errors.New(p.text.Common.HerdrUnreachable)}
		}
		snapshot, err := p.deps.Herdr.Snapshot(p.ctx)
		if err != nil {
			return pickerLinkedMsg{err: fmt.Errorf(p.text.Picker.HerdrError, err)}
		}
		agent, ok := snapshot.AgentByPaneID(targetPane)
		if !ok {
			return pickerLinkedMsg{err: fmt.Errorf(p.text.Picker.NoAgent, targetPane)}
		}
		if agent.SessionID() == "" {
			return pickerLinkedMsg{err: fmt.Errorf(p.text.Picker.NoSessionID, targetPane)}
		}

		ref := model.SessionRef{Agent: agent.Agent, SessionID: agent.SessionID(), Cwd: agent.Cwd}
		var updated *model.Task
		err = p.deps.Tasks.Update(p.ctx, func(f *model.File) error {
			t, err := f.Task(taskID)
			if err != nil {
				return err
			}
			if _, err := t.AddSession(ref, now); err != nil {
				return err
			}
			updated = t
			return nil
		})
		if err != nil {
			return pickerLinkedMsg{err: err}
		}
		// A failed stamp is only a missing convenience in herdr's own UI (§7.5), never a
		// reason to report the link itself as failed.
		_ = p.deps.Herdr.ReportTaskDisplay(p.ctx, targetPane, updated.ID, updated.Title)
		return pickerLinkedMsg{task: updated}
	}
}

// sortedForPicker orders tasks by column position (config order), then id, so the list reads the
// same way the board's columns do. A task whose status is not a defined column sorts last.
func sortedForPicker(tasks []model.Task, columns model.Columns) []model.Task {
	sorted := append([]model.Task(nil), tasks...)
	sort.SliceStable(sorted, func(i, j int) bool {
		oi, oj := columnOrder(sorted[i].Status, columns), columnOrder(sorted[j].Status, columns)
		if oi != oj {
			return oi < oj
		}
		return sorted[i].ID < sorted[j].ID
	})
	return sorted
}

func columnOrder(status string, columns model.Columns) int {
	if idx := columns.Index(status); idx >= 0 {
		return idx
	}
	return len(columns)
}

// message renders err in the popup's language, dropping the advice it carries: the popup has one
// line to say what went wrong.
func (p *picker) message(err error) string {
	text, _ := i18n.Message(p.text, err)
	return text
}

// pickerFixedLines is what the popup spends on everything but the task list: the title, the
// filter, the blank line under it, the blank line above the status, the status itself, and the
// key help. The list gets whatever is left.
const pickerFixedLines = 6

func (p *picker) View() tea.View {
	return tea.NewView(p.render())
}

func (p *picker) render() string {
	width := maxInt(p.width, 1)

	lines := []string{
		p.styles.heading.Render(truncate(fmt.Sprintf(p.text.Picker.Title, p.targetPane), width)),
		p.filter.View(),
		"",
	}

	lines = append(lines, p.listLines(width)...)

	lines = append(lines, "", p.statusLine(width))
	lines = append(lines, p.styles.footer.Render(truncate(fmt.Sprintf(p.text.Picker.Help, p.icons.verticalKeys()), width)))

	return strings.Join(clampLines(lines, p.height), "\n")
}

// listLines draws the task list into the rows left over after pickerFixedLines, windowed so the
// selection stays visible. Rows are trimmed as plain text and styled afterwards, since cutting an
// already-styled row would land inside an escape sequence.
func (p *picker) listLines(width int) []string {
	budget := p.height - pickerFixedLines
	if budget < 1 {
		return nil
	}

	switch {
	case p.loadErr != nil:
		return []string{p.styles.alert.Render(truncate(p.message(p.loadErr), width))}
	case !p.loaded:
		return []string{p.styles.dim.Render(truncate(p.text.Picker.Loading, width))}
	case len(p.filtered) == 0:
		return []string{p.styles.dim.Render(truncate(p.text.Picker.NoMatch, width))}
	}

	start, visible, before, after := listWindow(p.offset, p.cursor, len(p.filtered), budget)
	p.offset = start

	lines := make([]string, 0, budget)
	if before {
		lines = append(lines, p.styles.dim.Render(truncateMark))
	}
	for i := start; i < start+visible; i++ {
		task := p.tasks[p.filtered[i]]
		row := truncate(fmt.Sprintf("#%-4d [%s] %s", task.ID, columnLabel(task.Status, p.deps.Columns), task.Title), width)
		if i == p.cursor {
			row = p.styles.cardTitleSelected.Render(row)
		}
		lines = append(lines, row)
	}
	if after {
		lines = append(lines, p.styles.dim.Render(truncateMark))
	}
	return lines
}

// statusLine is always a line, blank included: the list's budget is measured against a fixed
// number of surrounding rows, and a status that disappears would change it.
func (p *picker) statusLine(width int) string {
	switch {
	case p.linking:
		return p.styles.dim.Render(truncate(p.text.Picker.Attaching, width))
	case p.status != "" && p.isError:
		return p.styles.alert.Render(truncate(p.status, width))
	case p.status != "":
		return p.styles.status.Render(truncate(p.status, width))
	}
	return ""
}

// clampLines is the last guard on the popup's height, for the sizes where even the fixed rows do
// not fit. It mirrors renderModal's own cut for the board's dialogs.
func clampLines(lines []string, height int) []string {
	if height <= 0 {
		return nil
	}
	if len(lines) <= height {
		return lines
	}
	return append(lines[:height-1:height-1], truncateMark)
}

func columnLabel(status string, columns model.Columns) string {
	if col, ok := columns.Find(status); ok {
		return col.Label
	}
	return status
}
