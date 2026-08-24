package tui

import (
	"context"
	"time"

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

// HerdrOps are the herdr operations the jump flow performs. *herdrc.Client satisfies this.
type HerdrOps interface {
	Snapshot(ctx context.Context) (*herdrc.Snapshot, error)
	FocusAgent(ctx context.Context, paneID string) error
	CreateTab(ctx context.Context, spec herdrc.TabSpec) (herdrc.Tab, error)
	StartAgent(ctx context.Context, spec herdrc.AgentSpec) (herdrc.StartResult, error)
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
	Getenv   func(string) string
	Now      func() time.Time
}

// Settings are the board's configuration, resolved from config.toml before the program starts.
type Settings struct {
	Columns model.Columns
	// Classifier derives a link's kind from its URL, using the configured GHES and Jira hosts.
	Classifier model.URLClassifier
	// CacheTTL is how long a fetched link value counts as current.
	CacheTTL time.Duration
	// RefreshInterval is the background fetch cadence. Zero disables background refresh entirely;
	// r and R still work.
	RefreshInterval time.Duration
}

func (d Deps) now() time.Time {
	if d.Now == nil {
		return time.Now()
	}
	return d.Now()
}

func (d Deps) getenv(key string) string {
	if d.Getenv == nil {
		return ""
	}
	return d.Getenv(key)
}
