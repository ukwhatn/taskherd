package store

import (
	"fmt"
	"path/filepath"
	"sync"

	"github.com/fsnotify/fsnotify"
)

// Watcher reports changes to tasks.json. Events are coalesced: a receiver learns only that
// something changed and re-reads the content with Load.
type Watcher struct {
	fsw    *fsnotify.Watcher
	events chan struct{}
	errs   chan error
	once   sync.Once
}

// Watch observes tasks.json for changes.
// It watches the parent directory rather than the file, because an atomic rename swaps the inode
// and a watch on the file cannot follow the new one.
func (s *Store) Watch() (*Watcher, error) {
	if err := s.ensureDir(); err != nil {
		return nil, err
	}

	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("cannot start the file watcher: %w", err)
	}
	if err := fsw.Add(s.dir); err != nil {
		_ = fsw.Close()
		return nil, fmt.Errorf("cannot watch %s: %w", s.dir, err)
	}

	w := &Watcher{
		fsw:    fsw,
		events: make(chan struct{}, 1),
		errs:   make(chan error, 1),
	}
	go w.loop()
	return w, nil
}

// Events signals that tasks.json changed. It is closed after Close.
func (w *Watcher) Events() <-chan struct{} { return w.events }

// Errors reports failures from the underlying watcher.
func (w *Watcher) Errors() <-chan error { return w.errs }

// Close stops watching. It is safe to call more than once.
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
			// Remove counts too: on some platforms a rename over the file arrives as the old inode's deletion.
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
