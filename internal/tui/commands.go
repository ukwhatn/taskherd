package tui

import (
	"errors"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/ukwhatn/taskherd/internal/fetch"
	"github.com/ukwhatn/taskherd/internal/herdrc"
	"github.com/ukwhatn/taskherd/internal/model"
)

// Messages carrying the result of work done off the update loop.
type (
	// tasksLoadedMsg delivers a fresh read of tasks.json.
	tasksLoadedMsg struct {
		file *model.File
		err  error
		// note is shown on the status line once the data is in.
		note string
		// refresh asks for a fetch cycle afterwards, for a change that may have added a link.
		refresh bool
		// focus is the task the selection should follow after the rebuild, or 0 to leave it.
		focus int
	}

	// fileEventMsg reports that tasks.json changed underneath the board.
	fileEventMsg struct{ closed bool }

	// sessionUpdateMsg delivers one live herdr report.
	sessionUpdateMsg struct {
		update herdrc.Update
		closed bool
	}

	// cacheLoadedMsg delivers a read of cache.json.
	cacheLoadedMsg struct {
		cache *fetch.CacheFile
	}

	// refreshDoneMsg reports the outcome of one fetch cycle.
	refreshDoneMsg struct {
		result *fetch.RefreshResult
		cache  *fetch.CacheFile
		err    error
		at     time.Time
		manual bool
	}

	// refreshTickMsg fires the background fetch cadence.
	refreshTickMsg struct{}

	// editorDoneMsg reports that $EDITOR exited, leaving the edited note in path.
	editorDoneMsg struct {
		taskID int
		path   string
		err    error
	}

	// agentsLoadedMsg delivers the agents herdr currently reports, for the session picker.
	agentsLoadedMsg struct {
		agents []herdrc.Agent
		err    error
	}

	// statusMsg puts a line on the footer without touching any data.
	statusMsg struct {
		text    string
		isError bool
	}
)

// fetchAgentsCmd asks herdr which agents exist right now, off the update loop.
func (b *Board) fetchAgentsCmd() tea.Cmd {
	return func() tea.Msg {
		snapshot, err := b.deps.Herdr.Snapshot(b.ctx)
		if err != nil {
			return agentsLoadedMsg{err: err}
		}
		return agentsLoadedMsg{agents: snapshot.Agents}
	}
}

func status(text string, isError bool) tea.Cmd {
	return func() tea.Msg { return statusMsg{text: text, isError: isError} }
}

// mutation is one change to tasks.json plus how to report it. note and focus are evaluated after
// apply has run, so they can mention the id it assigned.
type mutation struct {
	apply   func(*model.File) error
	note    func() string
	focus   func() int
	refresh bool
}

// mutateCmd applies the change under the store's own lock and reloads the board from the result.
// The board never writes back the display model it holds; only apply's edits reach the file.
func (b *Board) mutateCmd(m mutation) tea.Cmd {
	return func() tea.Msg {
		if err := b.deps.Tasks.Update(b.ctx, m.apply); err != nil {
			return tasksLoadedMsg{err: err}
		}
		msg := tasksLoadedMsg{refresh: m.refresh}
		if m.note != nil {
			msg.note = m.note()
		}
		if m.focus != nil {
			msg.focus = m.focus()
		}
		file, err := b.deps.Tasks.Load()
		if err != nil {
			msg.err = err
			return msg
		}
		msg.file = file
		return msg
	}
}

func (b *Board) loadTasksCmd(note string) tea.Cmd {
	return func() tea.Msg {
		file, err := b.deps.Tasks.Load()
		return tasksLoadedMsg{file: file, err: err, note: note}
	}
}

func (b *Board) loadCacheCmd() tea.Cmd {
	return func() tea.Msg {
		return cacheLoadedMsg{cache: b.deps.Cache.Load()}
	}
}

// waitFileEvent turns the next store event into a message. The watcher coalesces events, so one
// message only ever means "re-read".
func waitFileEvent(watcher FileWatcher) tea.Cmd {
	return func() tea.Msg {
		if _, ok := <-watcher.Events(); !ok {
			return fileEventMsg{closed: true}
		}
		return fileEventMsg{}
	}
}

// waitSessionUpdate turns the next herdr report into a message. The source reconnects on its own,
// so an unavailable report is not the end of the stream.
func waitSessionUpdate(source SessionSource) tea.Cmd {
	return func() tea.Msg {
		update, ok := <-source.Updates()
		if !ok {
			return sessionUpdateMsg{closed: true}
		}
		return sessionUpdateMsg{update: update}
	}
}

// tickCmd schedules the next background fetch. It is re-armed after every tick rather than set up
// once, which is what lets a rate limit stretch the interval for the following cycle.
func (b *Board) tickCmd() tea.Cmd {
	interval := b.refreshInterval()
	if interval <= 0 {
		return nil
	}
	return tea.Tick(interval, func(time.Time) tea.Msg { return refreshTickMsg{} })
}

// refreshStaleCmd fetches only the links whose cached value has aged past the TTL. This is the
// background path: startup and the timer both use it, so a board left open does not refetch
// everything on every cycle.
func (b *Board) refreshStaleCmd() tea.Cmd {
	if b.deps.Links == nil || b.fetching {
		return nil
	}
	var urls []string
	seen := map[string]bool{}
	for _, link := range allLinks(b.file.Tasks) {
		if seen[link.URL] {
			continue
		}
		seen[link.URL] = true

		state, ok := b.links[link.URL]
		if !ok {
			state = fetch.LinkState{URL: link.URL, Kind: link.Kind}
		}
		if !state.Fetchable() {
			continue
		}
		if state.Fetched && !state.Stale {
			continue
		}
		urls = append(urls, link.URL)
	}
	if len(urls) == 0 {
		return nil
	}
	b.fetching = true
	return b.fetchCmd(urls, false)
}

// refreshTaskCmd re-fetches the active task's links regardless of the TTL: r is a manual
// request, and the point of it is to bypass the cache.
func (b *Board) refreshTaskCmd() tea.Cmd {
	if b.deps.Links == nil {
		return status("ライブ取得が無効になっている", true)
	}
	task := b.activeTask()
	if task == nil {
		return status("カードが選択されていない", true)
	}
	if b.fetching {
		return status("取得中", false)
	}

	var urls []string
	for _, link := range task.Links {
		urls = append(urls, link.URL)
	}
	if len(urls) == 0 {
		return status("このタスクにリンクがない", true)
	}
	b.fetching = true
	return b.fetchCmd(urls, true)
}

func (b *Board) refreshAllCmd() tea.Cmd {
	if b.deps.Links == nil {
		return status("ライブ取得が無効になっている", true)
	}
	if b.fetching {
		return status("取得中", false)
	}

	var urls []string
	seen := map[string]bool{}
	for _, link := range allLinks(b.file.Tasks) {
		if seen[link.URL] {
			continue
		}
		seen[link.URL] = true
		urls = append(urls, link.URL)
	}
	if len(urls) == 0 {
		return status("リンクが 1 つもない", true)
	}
	b.fetching = true
	return b.fetchCmd(urls, true)
}

// fetchCmd runs one cycle off the update loop, so key handling continues while gh and Jira are
// being waited on.
func (b *Board) fetchCmd(urls []string, manual bool) tea.Cmd {
	return func() tea.Msg {
		result, err := b.deps.Links.RefreshLinks(b.ctx, urls)
		msg := refreshDoneMsg{result: result, err: err, at: b.deps.now(), manual: manual}
		if b.deps.Cache != nil {
			msg.cache = b.deps.Cache.Load()
		}
		return msg
	}
}

// stampTokensCmd re-stamps the task id onto every live pane a task is linked to. The stamp expires
// after 24h on herdr's side, so it is refreshed when the board opens rather than relied upon.
func (b *Board) stampTokensCmd() tea.Cmd {
	type stamp struct {
		paneID string
		taskID int
	}
	var stamps []stamp
	for i := range b.file.Tasks {
		for _, session := range b.file.Tasks[i].Sessions {
			if paneID := b.sessions.Pane[session.SessionID]; paneID != "" {
				stamps = append(stamps, stamp{paneID: paneID, taskID: b.file.Tasks[i].ID})
			}
		}
	}
	if len(stamps) == 0 {
		return nil
	}

	return func() tea.Msg {
		for _, s := range stamps {
			// A failed stamp is only a missing convenience in herdr's own UI, so it never
			// becomes an error the user has to dismiss.
			_ = b.deps.Herdr.ReportTaskToken(b.ctx, s.paneID, s.taskID)
		}
		return nil
	}
}

func jiraRetryAfter(err error) time.Duration {
	var rateLimit *fetch.JiraRateLimitError
	if errors.As(err, &rateLimit) {
		return rateLimit.RetryAfter
	}
	return 0
}
