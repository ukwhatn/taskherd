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
	return fmt.Sprintf("%s を読み込めない: %v", e.Path, e.Err)
}

func (e *CorruptError) Unwrap() error { return e.Err }

// Hint returns how to recover.
func (e *CorruptError) Hint() string {
	return fmt.Sprintf("書き込み前の内容は %s に残っている。内容を確認して手動で復旧する（taskherd は自動上書きしない）", e.BakPath)
}

// LockError reports that waiting for the lock timed out.
type LockError struct {
	Path    string
	Timeout time.Duration
	Err     error
}

func (e *LockError) Error() string {
	return fmt.Sprintf("%s のロックを %s 以内に取得できなかった: %v", e.Path, e.Timeout, e.Err)
}

func (e *LockError) Unwrap() error { return e.Err }

// Hint returns how to recover.
func (e *LockError) Hint() string {
	return "他の taskherd プロセスが書き込み中の可能性がある。完了を待ってから再実行する"
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

	data, err := model.MarshalFile(f)
	if err != nil {
		return err
	}
	// The backup must land before the rename; afterwards it would hold the new content and recover nothing.
	if raw != nil {
		if err := writeFileAtomic(s.BakPath(), raw); err != nil {
			return fmt.Errorf("バックアップを書けない: %w", err)
		}
	}
	if err := writeFileAtomic(s.TasksPath(), data); err != nil {
		return fmt.Errorf("%s を書けない: %w", s.TasksPath(), err)
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
		return nil, nil, fmt.Errorf("%s を読めない: %w", s.TasksPath(), err)
	}

	f, err := model.ParseFile(raw)
	if err != nil {
		return nil, nil, &CorruptError{Path: s.TasksPath(), BakPath: s.BakPath(), Err: err}
	}
	return raw, f, nil
}

func (s *Store) ensureDir() error {
	if err := os.MkdirAll(s.dir, dirPerm); err != nil {
		return fmt.Errorf("%s を作成できない: %w", s.dir, err)
	}
	// MkdirAll applies umask, so the mode is set explicitly here (this also tightens an existing directory).
	if err := os.Chmod(s.dir, dirPerm); err != nil {
		return fmt.Errorf("%s の権限を設定できない: %w", s.dir, err)
	}
	return nil
}
