package tui

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/ukwhatn/taskherd/internal/fetch"
	"github.com/ukwhatn/taskherd/internal/herdrc"
	"github.com/ukwhatn/taskherd/internal/i18n"
	"github.com/ukwhatn/taskherd/internal/model"
)

var boardNow = time.Date(2026, 8, 24, 16, 0, 0, 0, time.UTC)

// ja and en are the catalogs the tests assert against. Comparing rendered output to a catalog
// field rather than to a literal is what keeps a wording change from being a test change too;
// the wording itself is checked in internal/i18n.
var (
	ja = i18n.For(i18n.LangJA)
	en = i18n.For(i18n.LangEN)
)

// fakeStore stands in for the real tasks.json store. Load hands out a deep copy, which is what
// makes the test able to catch the board writing back its own display model.
type fakeStore struct {
	mu       sync.Mutex
	file     *model.File
	updates  int
	loadErr  error
	failWith error
}

func newFakeStore(tasks ...model.Task) *fakeStore {
	file := model.NewFile()
	for _, task := range tasks {
		file.Tasks = append(file.Tasks, task)
		if task.ID >= file.NextID {
			file.NextID = task.ID + 1
		}
	}
	return &fakeStore{file: file}
}

func (s *fakeStore) Load() (*model.File, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loadErr != nil {
		return nil, s.loadErr
	}
	return clone(s.file), nil
}

func (s *fakeStore) Update(_ context.Context, fn func(*model.File) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failWith != nil {
		return s.failWith
	}
	working := clone(s.file)
	if err := fn(working); err != nil {
		return err
	}
	s.file = working
	s.updates++
	return nil
}

func (s *fakeStore) snapshot() *model.File {
	s.mu.Lock()
	defer s.mu.Unlock()
	return clone(s.file)
}

func clone(f *model.File) *model.File {
	data, err := json.Marshal(f)
	if err != nil {
		panic(err)
	}
	var out model.File
	if err := json.Unmarshal(data, &out); err != nil {
		panic(err)
	}
	out.Normalize()
	return &out
}

type fakeFiles struct {
	events chan struct{}
	closed bool
}

// newFakeFiles registers a cleanup that closes the channel, so any command still parked on it
// when the test ends unblocks instead of leaking a goroutine.
func newFakeFiles(t *testing.T) *fakeFiles {
	t.Helper()
	f := &fakeFiles{events: make(chan struct{}, 1)}
	t.Cleanup(func() { close(f.events) })
	return f
}

func (f *fakeFiles) Events() <-chan struct{} { return f.events }
func (f *fakeFiles) Close() error            { f.closed = true; return nil }

type fakeSessions struct {
	updates chan herdrc.Update
	closed  bool
}

func newFakeSessions(t *testing.T) *fakeSessions {
	t.Helper()
	f := &fakeSessions{updates: make(chan herdrc.Update, 1)}
	t.Cleanup(func() { close(f.updates) })
	return f
}

func (f *fakeSessions) Updates() <-chan herdrc.Update { return f.updates }
func (f *fakeSessions) Close()                        { f.closed = true }

type fakeHerdr struct {
	focused  []string
	tokens   []tokenStamp
	focusErr error
	// snapshot is what Snapshot answers with; nil means an empty herdr.
	snapshot    *herdrc.Snapshot
	snapshotErr error
}

type tokenStamp struct {
	paneID string
	taskID int
	title  string
}

func (f *fakeHerdr) Snapshot(context.Context) (*herdrc.Snapshot, error) {
	if f.snapshotErr != nil {
		return nil, f.snapshotErr
	}
	if f.snapshot != nil {
		return f.snapshot, nil
	}
	return &herdrc.Snapshot{}, nil
}

func (f *fakeHerdr) FocusAgent(_ context.Context, paneID string) error {
	if f.focusErr != nil {
		return f.focusErr
	}
	f.focused = append(f.focused, paneID)
	return nil
}

func (f *fakeHerdr) ReportTaskDisplay(_ context.Context, paneID string, taskID int, title string) error {
	f.tokens = append(f.tokens, tokenStamp{paneID: paneID, taskID: taskID, title: title})
	return nil
}

// fakeLauncher records what the board handed off instead of doing itself. Both methods return as
// soon as they are called, which is what the real one does too: a detached launch reports through
// its own log, never back to the board.
type fakeLauncher struct {
	starts  []startCall
	resumes []resumeCall
	err     error
}

type startCall struct {
	taskID int
	cwd    string
	prompt string
	space  SpaceChoice
}

type resumeCall struct {
	taskID    int
	sessionID string
	space     SpaceChoice
}

func (f *fakeLauncher) StartSession(taskID int, cwd, prompt string, space SpaceChoice) error {
	if f.err != nil {
		return f.err
	}
	f.starts = append(f.starts, startCall{taskID: taskID, cwd: cwd, prompt: prompt, space: space})
	return nil
}

func (f *fakeLauncher) ResumeSession(taskID int, sessionID string, space SpaceChoice) error {
	if f.err != nil {
		return f.err
	}
	f.resumes = append(f.resumes, resumeCall{taskID: taskID, sessionID: sessionID, space: space})
	return nil
}

type fakeCache struct {
	file *fetch.CacheFile
}

func (f *fakeCache) Load() *fetch.CacheFile {
	if f.file == nil {
		return &fetch.CacheFile{Version: 1, Entries: map[string]fetch.CacheEntry{}}
	}
	return f.file
}

type fakeLinks struct {
	calls  [][]string
	result *fetch.RefreshResult
	err    error
}

func (f *fakeLinks) RefreshLinks(_ context.Context, urls []string) (*fetch.RefreshResult, error) {
	f.calls = append(f.calls, urls)
	if f.err != nil {
		return nil, f.err
	}
	if f.result != nil {
		return f.result, nil
	}
	result := &fetch.RefreshResult{}
	for _, u := range urls {
		result.Outcomes = append(result.Outcomes, fetch.RefreshOutcome{URL: u})
	}
	return result, nil
}

// harness drives the board without a terminal.
//
// Commands are executed explicitly rather than by draining every command a message produced: the
// board's live sources are commands that block on a channel or a timer on purpose, and running
// those in a test would simply hang.
type harness struct {
	t     *testing.T
	board *Board
	store *fakeStore
	// quit records that a command asked the program to exit. The board closes itself after
	// handing work to a pane, so several tests turn on this having happened.
	quit bool
}

func newHarness(t *testing.T, deps Deps, settings Settings) *harness {
	t.Helper()
	if deps.Now == nil {
		deps.Now = func() time.Time { return boardNow }
	}
	if len(settings.Columns) == 0 {
		settings.Columns = testColumns()
	}
	if settings.CacheTTL == 0 {
		settings.CacheTTL = 5 * time.Minute
	}

	board := New(context.Background(), deps, settings)
	board.width, board.height = 200, 30

	h := &harness{t: t, board: board}
	if store, ok := deps.Tasks.(*fakeStore); ok {
		h.store = store
		h.reload()
	}
	return h
}

// reload pulls the store's current content into the board, the way the initial load does.
func (h *harness) reload() {
	h.t.Helper()
	file, err := h.store.Load()
	if err != nil {
		h.t.Fatalf("Load: %v", err)
	}
	h.dispatch(tasksLoadedMsg{file: file})
}

// dispatch feeds one message and returns the command the board produced, without running it.
func (h *harness) dispatch(msg tea.Msg) tea.Cmd {
	h.t.Helper()
	_, cmd := h.board.Update(msg)
	return cmd
}

// key presses one key and runs whatever the board asked for, feeding the results back in.
func (h *harness) key(name string) {
	h.t.Helper()
	h.run(h.dispatch(keyMsg(name)))
}

// typeText types a string into the focused prompt. The commands a text input returns drive its
// cursor blink, so they are dropped rather than run: a test has no cursor to blink.
func (h *harness) typeText(s string) {
	h.t.Helper()
	for _, r := range s {
		h.dispatch(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

// paste delivers one bracketed paste, the way a terminal does: a single PasteMsg rather than a
// burst of key presses. As with typeText the cursor-blink command is dropped.
func (h *harness) paste(content string) {
	h.t.Helper()
	h.dispatch(tea.PasteMsg{Content: content})
}

// run executes cmd and feeds every message it produces back into the board, following batches.
// Commands whose whole purpose is to wait (the live source readers, the refresh timer) are
// skipped: they never return on their own.
func (h *harness) run(cmd tea.Cmd) {
	h.t.Helper()
	if cmd == nil {
		return
	}
	msg := runWithTimeout(h.t, cmd)
	switch typed := msg.(type) {
	case nil:
		return
	case tea.BatchMsg:
		for _, inner := range typed {
			h.run(inner)
		}
	case tea.QuitMsg:
		h.quit = true
		return
	default:
		h.run(h.dispatch(msg))
	}
}

// runWithTimeout runs cmd, treating one that does not return as a waiter to be skipped rather
// than a failure: the board deliberately parks commands on channels and timers.
func runWithTimeout(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case msg := <-done:
		return msg
	case <-time.After(100 * time.Millisecond):
		return nil
	}
}

func keyMsg(name string) tea.KeyPressMsg {
	switch name {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "left":
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	case "ctrl+c":
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	case "delete":
		return tea.KeyPressMsg{Code: tea.KeyDelete}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	}

	runes := []rune(name)
	if len(runes) != 1 {
		panic("未対応のキー: " + name)
	}
	msg := tea.KeyPressMsg{Code: runes[0], Text: name}
	if runes[0] >= 'A' && runes[0] <= 'Z' {
		msg.Code = runes[0] + ('a' - 'A')
		msg.Mod = tea.ModShift
	}
	return msg
}

// snapshotUpdate is one available herdr report placing the given sessions in panes.
func snapshotUpdate(agents ...herdrc.Agent) sessionUpdateMsg {
	return sessionUpdateMsg{update: herdrc.Update{
		Snapshot: &herdrc.Snapshot{Agents: agents},
		Status:   herdrc.Status{Available: true},
	}}
}

func agent(paneID, sessionID, state string) herdrc.Agent {
	return herdrc.Agent{
		PaneID:      paneID,
		Agent:       "claude",
		AgentStatus: state,
		Cwd:         "/tmp/work",
		Session:     &herdrc.AgentSession{Agent: "claude", Kind: "id", Value: sessionID},
	}
}

var errUnavailable = errors.New("herdr に接続できない")
