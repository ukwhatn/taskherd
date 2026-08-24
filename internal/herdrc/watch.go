package herdrc

import (
	"bufio"
	"context"
	"fmt"
	"time"
)

const (
	// eventDebounce collapses a burst of events into one snapshot refetch. herdr replays the
	// matching event history when a subscription starts, so bursts are the normal case.
	eventDebounce = 300 * time.Millisecond

	reconnectMin = 500 * time.Millisecond
	reconnectMax = 30 * time.Second

	// maxFilteredSubscriptions bounds the per-pane status subscriptions in one request.
	maxFilteredSubscriptions = 64
)

// broadcastSubscriptions are the unfiltered events that signal a change in the pane set.
var broadcastSubscriptions = []string{
	"pane.created",
	"pane.closed",
	"pane.exited",
	"pane.agent_detected",
}

// Update is one live report from a Watcher. Snapshot is nil while herdr is unreachable.
type Update struct {
	Snapshot *Snapshot
	Status   Status
}

// Watcher keeps a live subscription to herdr and reports whole snapshots.
//
// Events themselves are never surfaced: herdr replays the matching event history whenever a
// subscription starts, so an event only means "something may have changed" and the authoritative
// answer always comes from a fresh snapshot.
type Watcher struct {
	client  *Client
	updates chan Update
	cancel  context.CancelFunc
}

// Watch starts watching herdr. The returned channel closes when ctx is done or Close is called.
func (c *Client) Watch(ctx context.Context) *Watcher {
	ctx, cancel := context.WithCancel(ctx)
	w := &Watcher{
		client:  c,
		updates: make(chan Update, 1),
		cancel:  cancel,
	}
	go w.loop(ctx)
	return w
}

// Updates returns the channel of snapshots and connection states.
func (w *Watcher) Updates() <-chan Update { return w.updates }

// Close stops watching. Updates is drained and closed by the watcher itself.
func (w *Watcher) Close() { w.cancel() }

func (w *Watcher) loop(ctx context.Context) {
	defer close(w.updates)

	backoff := reconnectMin
	var snapshot *Snapshot

	for ctx.Err() == nil {
		if snapshot == nil {
			fetched, status := w.client.Probe(ctx)
			if !w.emit(ctx, Update{Snapshot: fetched, Status: status}) {
				return
			}
			if !status.Available {
				if !sleep(ctx, backoff) {
					return
				}
				backoff = grow(backoff)
				continue
			}
			snapshot = fetched
			backoff = reconnectMin
		}

		next, err := w.subscribeSession(ctx, snapshot)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			if !w.emit(ctx, Update{Status: Status{Err: err}}) {
				return
			}
			snapshot = nil
			if !sleep(ctx, backoff) {
				return
			}
			backoff = grow(backoff)
			continue
		}
		// The pane set changed, so the filtered subscriptions no longer match: herdr has no
		// unsubscribe, which makes a fresh connection the only way to change them.
		snapshot = next
		backoff = reconnectMin
	}
}

// subscribeSession holds one subscription open. It returns the snapshot to resubscribe with
// once the set of agent panes changes, or an error when the connection is lost.
func (w *Watcher) subscribeSession(ctx context.Context, snapshot *Snapshot) (*Snapshot, error) {
	paneIDs := snapshot.AgentPaneIDs()

	conn, err := w.client.dial(ctx)
	if err != nil {
		return nil, &UnavailableError{SocketPath: w.client.socketPath, Err: err}
	}
	defer conn.Close()

	if err := writeRequest(conn, "events.subscribe", subscribeParams(paneIDs)); err != nil {
		return nil, err
	}
	reader := bufio.NewReader(conn)
	if _, err := readResponse(reader); err != nil {
		return nil, err
	}

	lines, readErr := readLines(ctx, reader)

	// A nil channel blocks forever, which is how the debounce timer stays idle between bursts.
	var (
		pending <-chan time.Time
		timer   *time.Timer
	)
	stopTimer := func() {
		if timer != nil {
			timer.Stop()
			timer, pending = nil, nil
		}
	}
	defer stopTimer()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()

		case _, ok := <-lines:
			if !ok {
				if err := <-readErr; err != nil {
					return nil, err
				}
				return nil, fmt.Errorf("herdr のイベント購読が切断された")
			}
			if timer == nil {
				timer = time.NewTimer(eventDebounce)
				pending = timer.C
			}

		case <-pending:
			stopTimer()
			fresh, status := w.client.Probe(ctx)
			if !status.Available {
				return nil, status.Err
			}
			if !w.emit(ctx, Update{Snapshot: fresh, Status: status}) {
				return nil, ctx.Err()
			}
			if !sameStrings(paneIDs, fresh.AgentPaneIDs()) {
				return fresh, nil
			}
		}
	}
}

// readLines pumps the connection into a channel so the caller can also wait on timers.
// Closing the connection unblocks the reader.
func readLines(ctx context.Context, reader *bufio.Reader) (<-chan []byte, <-chan error) {
	lines := make(chan []byte, 16)
	errs := make(chan error, 1)
	go func() {
		defer close(lines)
		defer close(errs)
		for {
			line, err := reader.ReadBytes('\n')
			if len(line) > 0 {
				select {
				case lines <- line:
				case <-ctx.Done():
					return
				}
			}
			if err != nil {
				if ctx.Err() == nil {
					errs <- err
				}
				return
			}
		}
	}()
	return lines, errs
}

func subscribeParams(paneIDs []string) map[string]any {
	subs := make([]map[string]any, 0, len(broadcastSubscriptions)+len(paneIDs))
	for _, kind := range broadcastSubscriptions {
		subs = append(subs, map[string]any{"type": kind})
	}
	for i, paneID := range paneIDs {
		if i >= maxFilteredSubscriptions {
			break
		}
		subs = append(subs, map[string]any{"type": "pane.agent_status_changed", "pane_id": paneID})
	}
	return map[string]any{"subscriptions": subs}
}

// emit delivers an update, dropping the oldest pending one so a slow consumer cannot stall
// the watcher. It reports false when the watcher should stop.
func (w *Watcher) emit(ctx context.Context, update Update) bool {
	for {
		select {
		case w.updates <- update:
			return true
		case <-ctx.Done():
			return false
		default:
		}
		select {
		case <-w.updates:
		case <-ctx.Done():
			return false
		}
	}
}

func sleep(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func grow(d time.Duration) time.Duration {
	next := d * 2
	if next > reconnectMax {
		return reconnectMax
	}
	return next
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := make(map[string]int, len(a))
	for _, v := range a {
		seen[v]++
	}
	for _, v := range b {
		seen[v]--
		if seen[v] < 0 {
			return false
		}
	}
	return true
}
