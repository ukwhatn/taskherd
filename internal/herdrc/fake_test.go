package herdrc_test

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// fakeHerdr is a Unix socket server that speaks herdr's NDJSON protocol.
//
// It reproduces the two behaviours the real server was observed to have: a connection is closed
// once a non-subscription request is answered, and a subscription keeps its connection open for
// pushed event lines.
type fakeHerdr struct {
	t    *testing.T
	path string
	ln   net.Listener

	mu          sync.Mutex
	snapshot    string
	subscribes  [][]byte
	subscribers []net.Conn
	closed      bool
}

func newFakeHerdr(t *testing.T, snapshot string) *fakeHerdr {
	t.Helper()
	// A Unix socket path is capped near 104 bytes, which the test-name-derived t.TempDir()
	// path exceeds, so the directory and file names are kept short here.
	dir, err := os.MkdirTemp("", "th")
	if err != nil {
		t.Fatalf("一時ディレクトリを作れない: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	path := filepath.Join(dir, "h.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("フェイク socket を作れない: %v", err)
	}

	f := &fakeHerdr{t: t, path: path, ln: ln, snapshot: snapshot}
	go f.serve()
	t.Cleanup(f.Close)
	return f
}

func (f *fakeHerdr) Path() string { return f.path }

func (f *fakeHerdr) serve() {
	for {
		conn, err := f.ln.Accept()
		if err != nil {
			return
		}
		go f.handle(conn)
	}
}

func (f *fakeHerdr) handle(conn net.Conn) {
	reader := bufio.NewReader(conn)
	line, err := reader.ReadBytes('\n')
	if err != nil && len(line) == 0 {
		conn.Close()
		return
	}

	var req struct {
		ID     string          `json:"id"`
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(line, &req); err != nil {
		conn.Close()
		return
	}

	switch req.Method {
	case "events.subscribe":
		f.mu.Lock()
		f.subscribes = append(f.subscribes, req.Params)
		f.subscribers = append(f.subscribers, conn)
		f.mu.Unlock()
		writeLine(conn, `{"id":"`+req.ID+`","result":{"type":"subscription_started"}}`)
		// The connection stays open; pushes and Close end it.
		return
	case "session.snapshot":
		f.mu.Lock()
		snapshot := f.snapshot
		f.mu.Unlock()
		writeLine(conn, `{"id":"`+req.ID+`","result":{"type":"session_snapshot","snapshot":`+snapshot+`}}`)
	case "ping":
		writeLine(conn, `{"id":"`+req.ID+`","result":{"type":"pong","version":"0.8.2","protocol":20}}`)
	default:
		writeLine(conn, `{"id":"`+req.ID+`","error":{"code":"unknown_method","message":"`+req.Method+`"}}`)
	}
	// herdr answers one request per connection and then closes it.
	conn.Close()
}

func writeLine(conn net.Conn, line string) {
	_, _ = conn.Write(append([]byte(line), '\n'))
}

// SetSnapshot changes what the next snapshot request returns.
func (f *fakeHerdr) SetSnapshot(snapshot string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.snapshot = snapshot
}

// Push sends an event line to every open subscription.
func (f *fakeHerdr) Push(line string) {
	f.mu.Lock()
	conns := append([]net.Conn(nil), f.subscribers...)
	f.mu.Unlock()
	for _, conn := range conns {
		writeLine(conn, line)
	}
}

// DropSubscribers closes the open subscriptions, simulating a herdr restart.
func (f *fakeHerdr) DropSubscribers() {
	f.mu.Lock()
	conns := f.subscribers
	f.subscribers = nil
	f.mu.Unlock()
	for _, conn := range conns {
		conn.Close()
	}
}

// Subscribes returns the params of every subscribe request received so far.
func (f *fakeHerdr) Subscribes() [][]byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][]byte(nil), f.subscribes...)
}

func (f *fakeHerdr) Close() {
	f.mu.Lock()
	if f.closed {
		f.mu.Unlock()
		return
	}
	f.closed = true
	conns := f.subscribers
	f.subscribers = nil
	f.mu.Unlock()

	for _, conn := range conns {
		conn.Close()
	}
	f.ln.Close()
}

// fakeRunner stands in for the herdr CLI and records how it was called.
type fakeRunner struct {
	mu      sync.Mutex
	calls   [][]string
	handler func(args []string) ([]byte, error)
}

func (r *fakeRunner) Run(_ context.Context, args ...string) ([]byte, error) {
	r.mu.Lock()
	r.calls = append(r.calls, append([]string(nil), args...))
	handler := r.handler
	r.mu.Unlock()

	if handler == nil {
		return nil, nil
	}
	return handler(args)
}

func (r *fakeRunner) Calls() [][]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([][]string(nil), r.calls...)
}

// snapshotJSON builds a snapshot whose shape matches the real session.snapshot payload:
// entity ids are pane_id/tab_id/workspace_id and the session reference is agent_session.
func snapshotJSON(agents ...string) string {
	body := `{"version":"0.8.2","protocol":20,` +
		`"focused_workspace_id":"wS","focused_tab_id":"wS:t1","focused_pane_id":"wS:p1",` +
		`"panes":[],"agents":[`
	for i, agent := range agents {
		if i > 0 {
			body += ","
		}
		body += agent
	}
	return body + `]}`
}

func agentJSON(paneID, sessionID, status, cwd string) string {
	return agentJSONNamed("", paneID, sessionID, status, cwd)
}

// agentJSONNamed is agentJSON plus the identifier `agent start` registered the agent under,
// which is what AgentByName resolves.
func agentJSONNamed(name, paneID, sessionID, status, cwd string) string {
	session := "null"
	if sessionID != "" {
		session = `{"agent":"claude","kind":"id","source":"herdr:claude","value":"` + sessionID + `"}`
	}
	return `{"pane_id":"` + paneID + `","tab_id":"wS:t1","workspace_id":"wS","agent":"claude",` +
		`"name":"` + name + `",` +
		`"agent_status":"` + status + `","cwd":"` + cwd + `","agent_session":` + session +
		`,"terminal_title_stripped":"作業中"}`
}
