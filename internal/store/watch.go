package store

import (
	"fmt"
	"path/filepath"
	"sync"

	"github.com/fsnotify/fsnotify"
)

// Watcher は tasks.json の変更通知。Events は合体（coalesce）されるため、
// 受信側は「変わった」ことだけを知り、内容は Load で読み直す。
type Watcher struct {
	fsw    *fsnotify.Watcher
	events chan struct{}
	errs   chan error
	once   sync.Once
}

// Watch は tasks.json の変更を監視する。
// 監視対象は tasks.json ではなく親ディレクトリ（原子 rename で inode が入れ替わり、
// ファイル自体に張った監視は新しい inode を追えないため）。
func (s *Store) Watch() (*Watcher, error) {
	if err := s.ensureDir(); err != nil {
		return nil, err
	}

	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("ファイル監視を開始できない: %w", err)
	}
	if err := fsw.Add(s.dir); err != nil {
		_ = fsw.Close()
		return nil, fmt.Errorf("%s を監視できない: %w", s.dir, err)
	}

	w := &Watcher{
		fsw:    fsw,
		events: make(chan struct{}, 1),
		errs:   make(chan error, 1),
	}
	go w.loop()
	return w, nil
}

// Events は tasks.json が変わったことを通知する。Close 後にクローズされる。
func (w *Watcher) Events() <-chan struct{} { return w.events }

// Errors は監視中のエラーを通知する。
func (w *Watcher) Errors() <-chan error { return w.errs }

// Close は監視を終了する。複数回呼んでよい。
func (w *Watcher) Close() error {
	var err error
	w.once.Do(func() {
		err = w.fsw.Close()
	})
	return err
}

func (w *Watcher) loop() {
	defer close(w.events)

	for {
		select {
		case ev, ok := <-w.fsw.Events:
			if !ok {
				return
			}
			if filepath.Base(ev.Name) != tasksFileName {
				continue
			}
			// Remove も拾う: rename での上書きは、旧 inode の削除として届くプラットフォームがある。
			if !ev.Has(fsnotify.Create) && !ev.Has(fsnotify.Write) &&
				!ev.Has(fsnotify.Rename) && !ev.Has(fsnotify.Remove) {
				continue
			}
			select {
			case w.events <- struct{}{}:
			default:
			}
		case err, ok := <-w.fsw.Errors:
			if !ok {
				return
			}
			select {
			case w.errs <- err:
			default:
			}
		}
	}
}
