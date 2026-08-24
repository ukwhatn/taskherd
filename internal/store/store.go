// Package store は tasks.json の永続化を担う。
//
// 書き込みは flock による排他・原子 rename・1 世代バックアップを 1 トランザクションに束ねた
// Update に集約する。将来の同期アダプタ（外部サービスへの反映等）の差し込み点もこの境界に限る。
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

// CorruptError は tasks.json が解析不能または検証違反であることを表す。自動上書きはしない。
type CorruptError struct {
	Path    string
	BakPath string
	Err     error
}

func (e *CorruptError) Error() string {
	return fmt.Sprintf("%s を読み込めない: %v", e.Path, e.Err)
}

func (e *CorruptError) Unwrap() error { return e.Err }

// Hint は復旧手順を返す。
func (e *CorruptError) Hint() string {
	return fmt.Sprintf("書き込み前の内容は %s に残っている。内容を確認して手動で復旧する（taskherd は自動上書きしない）", e.BakPath)
}

// LockError はロック待ちが timeout したことを表す。
type LockError struct {
	Path    string
	Timeout time.Duration
	Err     error
}

func (e *LockError) Error() string {
	return fmt.Sprintf("%s のロックを %s 以内に取得できなかった: %v", e.Path, e.Timeout, e.Err)
}

func (e *LockError) Unwrap() error { return e.Err }

// Hint は復旧手順を返す。
func (e *LockError) Hint() string {
	return "他の taskherd プロセスが書き込み中の可能性がある。完了を待ってから再実行する"
}

// Store は 1 つのデータディレクトリに対する読み書きを担う。
type Store struct {
	dir         string
	lockTimeout time.Duration
}

// Option は Store の生成オプション。
type Option func(*Store)

// WithLockTimeout はロック待ちの上限を変更する。
func WithLockTimeout(d time.Duration) Option {
	return func(s *Store) { s.lockTimeout = d }
}

// New は dir をデータディレクトリとする Store を返す。
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

// Load は tasks.json を読み込む。ファイルが無ければ空のデータを返す。
// 原子 rename でのみ更新されるため、ロックを取らずに読んでも中間状態は見えない。
func (s *Store) Load() (*model.File, error) {
	_, f, err := s.read()
	return f, err
}

// Update はロック内で「再読込 → 検証 → 変更適用 → .bak 退避 → 原子書込」を 1 トランザクションで実行する。
// fn はロック取得後に読み直した内容を受け取るため、ロック外で読んだ内容を書き戻す経路は生じない。
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
	// .bak は rename より前に残す（後にすると .bak も新内容になり、誤削除を復旧できない）。
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

// read は tasks.json の生バイト列と解析結果を返す。ファイルが無い場合は raw=nil と空データ。
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
	// MkdirAll は umask 適用後の権限で作るため、既存ディレクトリを含めて明示的に締める。
	if err := os.Chmod(s.dir, dirPerm); err != nil {
		return fmt.Errorf("%s の権限を設定できない: %w", s.dir, err)
	}
	return nil
}
