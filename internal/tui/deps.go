package tui

import (
	"context"
	"time"

	"github.com/ukwhatn/taskherd/internal/config"
	"github.com/ukwhatn/taskherd/internal/fetch"
	"github.com/ukwhatn/taskherd/internal/herdrc"
	"github.com/ukwhatn/taskherd/internal/model"
)

// TaskStore is the tasks.json access the board needs. Every change goes through Update, so the
// board never writes back the display model it holds. *store.Store satisfies this.
type TaskStore interface {
	Load() (*model.File, error)
	Update(ctx context.Context, fn func(*model.File) error) error
}

// FileWatcher signals that tasks.json changed under the board. Events are coalesced: they only
// mean "re-read", never what changed. *store.Watcher satisfies this.
type FileWatcher interface {
	Events() <-chan struct{}
	Close() error
}

// SessionSource is the live herdr feed. Each update carries a whole snapshot, or reports that
// herdr is unreachable while the source keeps reconnecting on its own. *herdrc.Watcher satisfies this.
type SessionSource interface {
	Updates() <-chan herdrc.Update
	Close()
}

// HerdrOps are the herdr operations the jump and session-start flows perform. *herdrc.Client
// satisfies this.
type HerdrOps interface {
	Snapshot(ctx context.Context) (*herdrc.Snapshot, error)
	FocusAgent(ctx context.Context, paneID string) error
	CreateTab(ctx context.Context, spec herdrc.TabSpec) (herdrc.Tab, error)
	StartAgent(ctx context.Context, spec herdrc.AgentSpec) (herdrc.StartResult, error)
	WaitForAgentState(ctx context.Context, paneID string, until []string, timeout time.Duration) (herdrc.Agent, error)
	SendAgentPrompt(ctx context.Context, paneID, text string) error
	ReportTaskToken(ctx context.Context, paneID string, taskID int) error
}

// CacheLoader reads the live-status cache. *fetch.Cache satisfies this.
type CacheLoader interface {
	Load() *fetch.CacheFile
}

// LinkRefresher runs one fetch cycle over the given link URLs. *fetch.Fetcher satisfies this.
type LinkRefresher interface {
	RefreshLinks(ctx context.Context, urls []string) (*fetch.RefreshResult, error)
}

// Deps are the board's ports on the outside world. A nil port disables the feature that uses it
// rather than failing the board, which is how the board stays usable without herdr, without gh
// and without a writable cache.
type Deps struct {
	Tasks    TaskStore
	Files    FileWatcher
	Sessions SessionSource
	Herdr    HerdrOps
	Cache    CacheLoader
	Links    LinkRefresher
	Now      func() time.Time
}

// Settings are the board's configuration, resolved from config.toml before the program starts.
type Settings struct {
	Columns model.Columns
	// Editor is the command note editing opens, already resolved against the environment. Empty
	// means no editor is configured anywhere, and note editing reports that rather than guessing.
	Editor string
	// Classifier derives a link's kind from its URL, using the configured GHES and Jira hosts.
	Classifier model.URLClassifier
	// CacheTTL is how long a fetched link value counts as current.
	CacheTTL time.Duration
	// RefreshInterval is the background fetch cadence. Zero disables background refresh entirely;
	// r and R still work.
	RefreshInterval time.Duration
	// Icons is the glyph vocabulary the board draws with.
	Icons IconMode
	// Hyperlinks wraps link rows in OSC 8 so a terminal that understands it opens them on a click.
	Hyperlinks bool
	// SessionStart configures the prompt a session started from a task opens with. Held as-is
	// (rather than pre-resolved) because which template applies depends on the task being started,
	// decided at launch time rather than once when the board opens.
	SessionStart config.SessionStart
	// DetailTaskID opens the board straight into that task's detail modal once the first task list
	// arrives, for prefix+t launched from a pane whose session is already linked to a task. Zero
	// leaves the board on the ordinary board screen.
	DetailTaskID int
}

func (d Deps) now() time.Time {
	if d.Now == nil {
		return time.Now()
	}
	return d.Now()
}
