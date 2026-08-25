package tui

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/ukwhatn/taskherd/internal/herdrc"
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
	Now   func() time.Time
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
		return fmt.Errorf("タスクストアが設定されていない")
	}
	if targetPane == "" {
		return fmt.Errorf("対象 pane が指定されていない")
	}

	program := tea.NewProgram(newPicker(ctx, deps, targetPane), tea.WithContext(ctx))
	if _, err := program.Run(); err != nil {
		return fmt.Errorf("picker を実行できない: %w", err)
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
	targetPane string

	filter textinput.Model

	tasks    []model.Task // every task, sorted for display
	filtered []int        // indexes into tasks matching the current filter
	cursor   int

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
	filter.Prompt = "絞り込み: "
	filter.Focus()

	return &picker{
		ctx:        ctx,
		deps:       deps,
		styles:     newStyles(),
		icons:      Icons(deps.Icons),
		targetPane: targetPane,
		filter:     filter,
		width:      60,
		height:     20,
	}
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
			p.status, p.isError = msg.err.Error(), true
			return p, nil
		}
		p.linked = true
		p.status = fmt.Sprintf("#%d に紐づけた", msg.task.ID)
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
			return pickerLinkedMsg{err: fmt.Errorf("herdr に接続できない")}
		}
		snapshot, err := p.deps.Herdr.Snapshot(p.ctx)
		if err != nil {
			return pickerLinkedMsg{err: fmt.Errorf("herdr に接続できない: %w", err)}
		}
		agent, ok := snapshot.AgentByPaneID(targetPane)
		if !ok {
			return pickerLinkedMsg{err: fmt.Errorf("pane %s でエージェントが検出されていない", targetPane)}
		}
		if agent.SessionID() == "" {
			return pickerLinkedMsg{err: fmt.Errorf(
				"pane %s ではセッション ID を検出できない。herdr integration install claude を実行して再試行する", targetPane)}
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

func (p *picker) View() tea.View {
	var b strings.Builder

	fmt.Fprintf(&b, "%s\n", p.styles.heading.Render(fmt.Sprintf("pane %s をタスクに紐づける", p.targetPane)))
	b.WriteString(p.filter.View())
	b.WriteString("\n\n")

	switch {
	case p.loadErr != nil:
		b.WriteString(p.styles.alert.Render(p.loadErr.Error()))
		b.WriteString("\n")
	case !p.loaded:
		b.WriteString(p.styles.dim.Render("読み込み中..."))
		b.WriteString("\n")
	case len(p.filtered) == 0:
		b.WriteString(p.styles.dim.Render("一致するタスクがない"))
		b.WriteString("\n")
	default:
		for i, idx := range p.filtered {
			task := p.tasks[idx]
			label := columnLabel(task.Status, p.deps.Columns)
			line := fmt.Sprintf("#%-4d [%s] %s", task.ID, label, task.Title)
			if i == p.cursor {
				line = p.styles.cardTitleSelected.Render(line)
			}
			b.WriteString(line)
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")
	switch {
	case p.linking:
		b.WriteString(p.styles.dim.Render("紐づけ中..."))
	case p.status != "" && p.isError:
		b.WriteString(p.styles.alert.Render(p.status))
	case p.status != "":
		b.WriteString(p.styles.status.Render(p.status))
	}
	b.WriteString("\n")
	b.WriteString(p.styles.footer.Render(fmt.Sprintf("%s 選択  enter 紐づけ  esc 中止", p.icons.verticalKeys())))

	return tea.NewView(b.String())
}

func columnLabel(status string, columns model.Columns) string {
	if col, ok := columns.Find(status); ok {
		return col.Label
	}
	return status
}
