// Package store persists tasks.json.
//
// Every write goes through Update, which bundles flock exclusion, one generation of backup
// and an atomic rename into a single transaction. A future sync adapter plugs in at this
// boundary only; no other layer persists data.
package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"
	"github.com/ukwhatn/taskherd/internal/i18n"
	"github.com/ukwhatn/taskherd/internal/model"
)

const (
	tasksFileName = "tasks.json"
	bakFileName   = "tasks.json.bak"
	lockFileName  = "tasks.lock"

	defaultLockTimeout = 10 * time.Second
	lockRetryDelay     = 20 * time.Millisecond

	dirPerm  = 0o700
	filePerm = 0o600
)

// CorruptError reports tasks.json as unparsable or invalid. Such a file is never overwritten.
type CorruptError struct {
	Path    string
	BakPath string
	Err     error
}

func (e *CorruptError) Error() string {
	text, _ := e.Localize(i18n.For(i18n.LangEN))
	return text
}

func (e *CorruptError) Unwrap() error { return e.Err }

// Localize states the problem and how to recover from it. The cause is put through Message too:
// a parse failure is an English diagnostic, but a version mismatch is not, and only Message knows
// which one it is holding.
func (e *CorruptError) Localize(t *i18n.Catalog) (string, string) {
	entry := i18n.OrDefault(t).Err.Data.Corrupt
	cause, _ := i18n.Message(t, e.Err)
	return fmt.Sprintf(entry.Msg, e.Path, cause), fmt.Sprintf(entry.Hint, e.BakPath)
}

// UnsupportedVersionError reports a tasks.json written for a different taskherd version.
// It is kept apart from CorruptError because the file is intact; only this binary cannot handle it.
type UnsupportedVersionError struct {
	Path string
	Err  error
}

func (e *UnsupportedVersionError) Error() string {
	text, _ := e.Localize(i18n.For(i18n.LangEN))
	return text
}

func (e *UnsupportedVersionError) Unwrap() error { return e.Err }

// Localize states the problem and how to recover from it.
func (e *UnsupportedVersionError) Localize(t *i18n.Catalog) (string, string) {
	entry := i18n.OrDefault(t).Err.Data.Version
	cause, _ := i18n.Message(t, e.Err)
	return fmt.Sprintf(entry.Msg, e.Path, cause), entry.Hint
}

// LockError reports that waiting for the lock timed out.
type LockError struct {
	Path    string
	Timeout time.Duration
	Err     error
}

func (e *LockError) Error() string {
	text, _ := e.Localize(i18n.For(i18n.LangEN))
	return text
}

func (e *LockError) Unwrap() error { return e.Err }

// Localize states the problem and how to recover from it.
func (e *LockError) Localize(t *i18n.Catalog) (string, string) {
	entry := i18n.OrDefault(t).Err.Data.Lock
	return fmt.Sprintf(entry.Msg, e.Path, e.Timeout, e.Err), entry.Hint
}

// Store reads and writes one data directory.
type Store struct {
	dir         string
	lockTimeout time.Duration
}

// Option configures a Store.
type Option func(*Store)

// WithLockTimeout changes how long Update waits for the lock.
func WithLockTimeout(d time.Duration) Option {
	return func(s *Store) { s.lockTimeout = d }
}

// New returns a Store over the data directory dir.
func New(dir string, opts ...Option) *Store {
	s := &Store{dir: dir, lockTimeout: defaultLockTimeout}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *Store) Dir() string       { return s.dir }
func (s *Store) TasksPath() string { return filepath.Join(s.dir, tasksFileName) }
func (s *Store) BakPath() string   { return filepath.Join(s.dir, bakFileName) }
func (s *Store) LockPath() string  { return filepath.Join(s.dir, lockFileName) }

// Load reads tasks.json, returning empty data when the file does not exist.
// Reads take no lock: the file only ever changes by atomic rename, so no partial state is visible.
func (s *Store) Load() (*model.File, error) {
	_, f, err := s.read()
	return f, err
}

// Update runs re-read, validate, mutate, back up and atomic write as one transaction under the lock.
// fn receives data read after the lock was taken, so no content read outside the lock is written back.
func (s *Store) Update(ctx context.Context, fn func(*model.File) error) error {
	if err := s.ensureDir(); err != nil {
		return err
	}

	lock := flock.New(s.LockPath())
	if err := s.acquire(ctx, lock); err != nil {
		return err
	}
	defer func() {
		_ = lock.Unlock()
	}()

	raw, f, err := s.read()
	if err != nil {
		return err
	}
	if err := fn(f); err != nil {
		return err
	}
	// The mutated result is re-validated: a buggy caller must not be able to persist
	// duplicate ids or a stale next_id into the file every other command trusts.
	if err := model.Validate(f); err != nil {
		return fmt.Errorf("write aborted: the mutated content does not validate: %w", err)
	}

	data, err := model.MarshalFile(f)
	if err != nil {
		return err
	}
	// The backup must land before the rename; afterwards it would hold the new content and recover nothing.
	if raw != nil {
		if err := writeFileAtomic(s.BakPath(), raw); err != nil {
			return fmt.Errorf("cannot write the backup: %w", err)
		}
	}
	if err := writeFileAtomic(s.TasksPath(), data); err != nil {
		return fmt.Errorf("cannot write %s: %w", s.TasksPath(), err)
	}
	return nil
}

func (s *Store) acquire(ctx context.Context, lock *flock.Flock) error {
	lockCtx := ctx
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		lockCtx, cancel = context.WithTimeout(ctx, s.lockTimeout)
		defer cancel()
	}

	locked, err := lock.TryLockContext(lockCtx, lockRetryDelay)
	if err != nil || !locked {
		return &LockError{Path: s.LockPath(), Timeout: s.lockTimeout, Err: err}
	}
	return nil
}

// read returns the raw bytes and the parsed file, or raw=nil and empty data when the file is absent.
func (s *Store) read() ([]byte, *model.File, error) {
	raw, err := os.ReadFile(s.TasksPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil, model.NewFile(), nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("cannot read %s: %w", s.TasksPath(), err)
	}

	f, err := model.ParseFile(raw)
	if err != nil {
		var mismatch *model.VersionMismatchError
		if errors.As(err, &mismatch) {
			return nil, nil, &UnsupportedVersionError{Path: s.TasksPath(), Err: err}
		}
		return nil, nil, &CorruptError{Path: s.TasksPath(), BakPath: s.BakPath(), Err: err}
	}
	return raw, f, nil
}

func (s *Store) ensureDir() error {
	if err := os.MkdirAll(s.dir, dirPerm); err != nil {
		return fmt.Errorf("cannot create %s: %w", s.dir, err)
	}
	// MkdirAll applies umask, so the mode is set explicitly here (this also tightens an existing directory).
	if err := os.Chmod(s.dir, dirPerm); err != nil {
		return fmt.Errorf("cannot set the mode on %s: %w", s.dir, err)
	}
	return nil
}
