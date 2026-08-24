package fetch

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"
)

const (
	cacheFileName     = "cache.json"
	cacheLockFileName = "cache.lock"
	cacheVersion      = 1

	dirPerm  = 0o700
	filePerm = 0o600

	defaultCacheLockTimeout = 10 * time.Second
	cacheLockRetryDelay     = 20 * time.Millisecond
)

// CacheFile is the whole cache.json document: one entry per link URL.
type CacheFile struct {
	Version int                   `json:"version"`
	Entries map[string]CacheEntry `json:"entries"`
}

// CacheEntry is the last known state of one link.
//
// FetchedAt is nil until the first successful fetch. A later failure leaves FetchedAt and
// Data at their last-success values while OK/Error report the failure, so callers can show
// a stale-but-known value instead of blanking out on a transient error.
type CacheEntry struct {
	FetchedAt *string         `json:"fetched_at"`
	OK        bool            `json:"ok"`
	Error     string          `json:"error"`
	Data      json.RawMessage `json:"data"`
}

func newCacheFile() *CacheFile {
	return &CacheFile{Version: cacheVersion, Entries: map[string]CacheEntry{}}
}

// Get returns the cached entry for url.
func (f *CacheFile) Get(url string) (CacheEntry, bool) {
	e, ok := f.Entries[url]
	return e, ok
}

// SetSuccess records a successful fetch: fetched_at advances to now and data is replaced.
func (f *CacheFile) SetSuccess(url string, data any, now time.Time) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("%s の取得結果を直列化できない: %w", url, err)
	}
	ts := now.Format(time.RFC3339)
	f.Entries[url] = CacheEntry{FetchedAt: &ts, OK: true, Error: "", Data: raw}
	return nil
}

// SetFailure records a failed fetch without touching fetched_at or data: those still hold
// whatever the last success was (or remain nil if there never was one).
func (f *CacheFile) SetFailure(url string, fetchErr error) {
	existing := f.Entries[url]
	existing.OK = false
	existing.Error = fetchErr.Error()
	f.Entries[url] = existing
}

// IsStale reports whether this entry's last successful fetch is older than ttl, treating a
// never-successful or unparsable fetched_at as stale too. now and ttl are both explicit so
// the caller (a background refresh loop) controls the clock and cadence; `refresh`/`r`/`R`
// bypass this entirely and always fetch (§8.3 draws that line at "manual" vs. "background").
func (e CacheEntry) IsStale(now time.Time, ttl time.Duration) bool {
	if e.FetchedAt == nil {
		return true
	}
	fetchedAt, err := time.Parse(time.RFC3339, *e.FetchedAt)
	if err != nil {
		return true
	}
	return now.Sub(fetchedAt) >= ttl
}

// Cache reads and writes one data directory's cache.json. Unlike store.Store (tasks.json),
// it keeps no .bak generation: every entry is a point-in-time snapshot of an external
// service that a corrupt file loses nothing by rebuilding from scratch.
type Cache struct {
	dir         string
	lockTimeout time.Duration
}

// NewCache returns a Cache over the data directory dir.
func NewCache(dir string) *Cache {
	return &Cache{dir: dir, lockTimeout: defaultCacheLockTimeout}
}

func (c *Cache) Dir() string      { return c.dir }
func (c *Cache) Path() string     { return filepath.Join(c.dir, cacheFileName) }
func (c *Cache) LockPath() string { return filepath.Join(c.dir, cacheLockFileName) }

// Load reads cache.json, returning an empty cache when the file is absent, unparsable or
// written by an incompatible version. A corrupt tasks.json is refused for manual recovery;
// a corrupt cache.json is simply rebuilt on the next fetch, so no error is surfaced here.
func (c *Cache) Load() *CacheFile {
	data, err := os.ReadFile(c.Path())
	if err != nil {
		return newCacheFile()
	}
	var f CacheFile
	if err := json.Unmarshal(data, &f); err != nil || f.Version != cacheVersion {
		return newCacheFile()
	}
	if f.Entries == nil {
		f.Entries = map[string]CacheEntry{}
	}
	return &f
}

// Update runs read, mutate and atomic write as one transaction under the lock, mirroring
// store.Store.Update so concurrent refreshes (goroutines or processes) never clobber each
// other's entries: each Update re-reads the latest file before fn mutates it.
func (c *Cache) Update(ctx context.Context, fn func(*CacheFile)) error {
	if err := os.MkdirAll(c.dir, dirPerm); err != nil {
		return fmt.Errorf("%s を作成できない: %w", c.dir, err)
	}

	lock := flock.New(c.LockPath())
	lockCtx := ctx
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		lockCtx, cancel = context.WithTimeout(ctx, c.lockTimeout)
		defer cancel()
	}
	locked, err := lock.TryLockContext(lockCtx, cacheLockRetryDelay)
	if err != nil || !locked {
		return fmt.Errorf("%s のロックを取得できなかった: %w", c.LockPath(), err)
	}
	defer func() {
		_ = lock.Unlock()
	}()

	f := c.Load()
	fn(f)

	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("cache.json を生成できない: %w", err)
	}
	data = append(data, '\n')

	if err := writeFileAtomic(c.Path(), data); err != nil {
		return fmt.Errorf("%s を書けない: %w", c.Path(), err)
	}
	return nil
}
