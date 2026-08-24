package cli_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"

	"github.com/ukwhatn/taskherd/internal/herdrc"
)

// fakeHerdr stands in for the herdr CLI and records how the commands invoked it.
//
// The socket is deliberately left unreachable so that every request travels the CLI path,
// which keeps these tests about command behaviour; the socket protocol itself is covered by
// the herdrc package tests.
type fakeHerdr struct {
	mu    sync.Mutex
	calls [][]string

	// agents are the panes herdr reports, keyed by session id.
	agents map[string]fakeAgent
	// unavailable makes every request fail, which is the degraded mode.
	unavailable bool
	// newPaneID is the pane returned by tab create.
	newPaneID string
	// startErr is returned by agent start instead of a success payload.
	startErr error
}

type fakeAgent struct {
	PaneID string
	Agent  string
	Status string
	Cwd    string
}

func newFakeHerdr() *fakeHerdr {
	return &fakeHerdr{agents: map[string]fakeAgent{}, newPaneID: "wS:p9"}
}

func (f *fakeHerdr) withAgent(sessionID string, agent fakeAgent) *fakeHerdr {
	if agent.Agent == "" {
		agent.Agent = "claude"
	}
	if agent.Status == "" {
		agent.Status = "idle"
	}
	f.agents[sessionID] = agent
	return f
}

// withPaneWithoutSession registers a pane whose agent reports no session id.
func (f *fakeHerdr) withPaneWithoutSession(paneID, cwd string) *fakeHerdr {
	f.agents[""] = fakeAgent{PaneID: paneID, Agent: "claude", Status: "idle", Cwd: cwd}
	return f
}

func (f *fakeHerdr) client(getenv func(string) string) *herdrc.Client {
	return herdrc.New(herdrc.Options{
		Getenv: getenv,
		Dialer: func(context.Context, string) (net.Conn, error) {
			return nil, errors.New("テストでは socket を使わない")
		},
		Runner: f,
	})
}

func (f *fakeHerdr) Run(_ context.Context, args ...string) ([]byte, error) {
	f.mu.Lock()
	f.calls = append(f.calls, append([]string(nil), args...))
	f.mu.Unlock()

	joined := strings.Join(args, " ")
	switch {
	case joined == "api snapshot":
		if f.unavailable {
			return nil, errors.New("herdr に到達できない")
		}
		return []byte(`{"id":"cli:api:snapshot","result":{"type":"session_snapshot","snapshot":` + f.snapshotJSON() + `}}`), nil

	case strings.HasPrefix(joined, "agent focus"):
		if f.unavailable {
			return nil, errors.New("herdr に到達できない")
		}
		return []byte(`{"id":"cli:agent:focus","result":{"type":"ok"}}`), nil

	case strings.HasPrefix(joined, "tab create"):
		return []byte(fmt.Sprintf(`{"id":"cli:tab:create","result":{"type":"tab_created",`+
			`"tab":{"tab_id":"wS:t9","workspace_id":"wS"},`+
			`"root_pane":{"pane_id":%q,"cwd":"/repo"}}}`, f.newPaneID)), nil

	case strings.HasPrefix(joined, "agent start"):
		if f.startErr != nil {
			return nil, f.startErr
		}
		sessionID := ""
		for i, arg := range args {
			if arg == "--resume" && i+1 < len(args) {
				sessionID = args[i+1]
			}
		}
		return []byte(fmt.Sprintf(`{"id":"cli:agent:start","result":{"type":"agent_started",`+
			`"argv":["claude","--resume",%q],`+
			`"agent":{"pane_id":%q,"agent_session":{"agent":"claude","kind":"id","source":"herdr:claude","value":%q}}}}`,
			sessionID, f.newPaneID, sessionID)), nil

	case strings.HasPrefix(joined, "pane report-metadata"):
		return nil, nil

	case strings.HasPrefix(joined, "plugin pane open"):
		if f.unavailable {
			return nil, errors.New("herdr に到達できない")
		}
		return nil, nil
	}
	return nil, fmt.Errorf("想定外の呼び出し: %s", joined)
}

func (f *fakeHerdr) snapshotJSON() string {
	entries := make([]string, 0, len(f.agents))
	for sessionID, agent := range f.agents {
		session := "null"
		if sessionID != "" {
			session = fmt.Sprintf(`{"agent":%q,"kind":"id","source":"herdr:claude","value":%q}`, agent.Agent, sessionID)
		}
		entries = append(entries, fmt.Sprintf(
			`{"pane_id":%q,"tab_id":"wS:t1","workspace_id":"wS","agent":%q,"agent_status":%q,"cwd":%q,"agent_session":%s}`,
			agent.PaneID, agent.Agent, agent.Status, agent.Cwd, session))
	}
	return `{"version":"0.8.2","protocol":20,"focused_workspace_id":"wS","focused_tab_id":"wS:t1",` +
		`"focused_pane_id":"wS:p1","panes":[],"agents":[` + strings.Join(entries, ",") + `]}`
}

// called reports whether a command starting with prefix was invoked.
func (f *fakeHerdr) called(prefix string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, call := range f.calls {
		if strings.HasPrefix(strings.Join(call, " "), prefix) {
			return true
		}
	}
	return false
}

// call returns the first invocation starting with prefix.
func (f *fakeHerdr) call(prefix string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		if strings.HasPrefix(strings.Join(c, " "), prefix) {
			return c
		}
	}
	return nil
}
