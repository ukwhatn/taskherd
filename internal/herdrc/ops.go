package herdrc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"
)

// Error codes returned by herdr that taskherd branches on.
const (
	// CodeAgentNotFound means the pane holds no detected agent; the pane itself may still exist.
	CodeAgentNotFound = "agent_not_found"
	// CodeAgentNotReady / CodeAgentBlocked mean the agent was launched but stopped for input
	// during startup (a first-run trust prompt, for instance). The pane is usable by the human.
	CodeAgentNotReady = "agent_not_ready"
	CodeAgentBlocked  = "agent_blocked"
)

// agentStartTimeout is passed to herdr's own readiness wait. Resuming a large transcript
// takes noticeably longer than a cold start.
const agentStartTimeout = 60 * time.Second

// cliTimeout bounds the short-lived operations (focus, tab create, metadata).
const cliTimeout = 15 * time.Second

// TabSpec describes a tab to create for a resumed session.
type TabSpec struct {
	Cwd   string
	Label string
}

// Tab is the result of creating a tab: the new tab and the pane an agent can be started in.
type Tab struct {
	TabID       string
	WorkspaceID string
	PaneID      string
	Cwd         string
}

// AgentSpec describes an agent to start in an existing pane. Args are passed through to the
// agent executable after "--", which is how the resume flag reaches Claude Code.
type AgentSpec struct {
	Name   string
	Kind   string
	PaneID string
	Args   []string
}

// StartResult reports how an agent start ended. NeedsAttention is set when herdr launched the
// agent but it stopped for input before becoming ready, which is not a failure of the jump:
// the pane exists with the agent running and only the human can answer the prompt.
type StartResult struct {
	PaneID         string
	SessionID      string
	Argv           []string
	NeedsAttention bool
	Code           string
}

// FocusAgent moves herdr's focus to the pane holding the given agent.
//
// One call moves workspace, tab and pane focus together, so no separate tab or workspace
// focus is needed. It requires a detected agent in the pane and fails with agent_not_found
// for a pane whose agent has exited.
func (c *Client) FocusAgent(ctx context.Context, paneID string) error {
	callCtx, cancel := requestContext(ctx, cliTimeout)
	defer cancel()

	_, err := c.runner.Run(callCtx, "agent", "focus", paneID)
	return err
}

// CreateTab opens a new tab in the focused workspace and returns its root pane.
func (c *Client) CreateTab(ctx context.Context, spec TabSpec) (Tab, error) {
	callCtx, cancel := requestContext(ctx, cliTimeout)
	defer cancel()

	args := []string{"tab", "create"}
	if spec.Cwd != "" {
		args = append(args, "--cwd", spec.Cwd)
	}
	if spec.Label != "" {
		args = append(args, "--label", spec.Label)
	}

	out, err := c.runner.Run(callCtx, args...)
	if err != nil {
		return Tab{}, err
	}

	var payload struct {
		Tab struct {
			TabID       string `json:"tab_id"`
			WorkspaceID string `json:"workspace_id"`
		} `json:"tab"`
		RootPane struct {
			PaneID string `json:"pane_id"`
			Cwd    string `json:"cwd"`
		} `json:"root_pane"`
	}
	if err := decodeResult(out, &payload); err != nil {
		return Tab{}, err
	}
	if payload.RootPane.PaneID == "" {
		return Tab{}, fmt.Errorf("herdr tab create が pane を返さなかった")
	}
	return Tab{
		TabID:       payload.Tab.TabID,
		WorkspaceID: payload.Tab.WorkspaceID,
		PaneID:      payload.RootPane.PaneID,
		Cwd:         payload.RootPane.Cwd,
	}, nil
}

// StartAgent starts an agent in an existing pane and waits for it to become ready.
func (c *Client) StartAgent(ctx context.Context, spec AgentSpec) (StartResult, error) {
	callCtx, cancel := requestContext(ctx, agentStartTimeout+cliTimeout)
	defer cancel()

	args := []string{
		"agent", "start", spec.Name,
		"--kind", spec.Kind,
		"--pane", spec.PaneID,
		"--timeout", strconv.Itoa(int(agentStartTimeout.Milliseconds())),
	}
	if len(spec.Args) > 0 {
		args = append(args, "--")
		args = append(args, spec.Args...)
	}

	out, err := c.runner.Run(callCtx, args...)
	if err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) && (apiErr.Code == CodeAgentNotReady || apiErr.Code == CodeAgentBlocked) {
			return StartResult{PaneID: spec.PaneID, NeedsAttention: true, Code: apiErr.Code}, nil
		}
		return StartResult{}, err
	}

	var payload struct {
		Agent struct {
			PaneID  string        `json:"pane_id"`
			Session *AgentSession `json:"agent_session"`
		} `json:"agent"`
		Argv []string `json:"argv"`
	}
	if err := decodeResult(out, &payload); err != nil {
		return StartResult{}, err
	}
	result := StartResult{PaneID: payload.Agent.PaneID, Argv: payload.Argv}
	if result.PaneID == "" {
		result.PaneID = spec.PaneID
	}
	if payload.Agent.Session != nil {
		result.SessionID = payload.Agent.Session.Value
	}
	return result, nil
}

// WaitForAgentState blocks until the agent in paneID reports one of the given states, or herdr's
// own wait gives up at timeout.
//
// A newly started agent is trust-gated separately from the process starting (an unfamiliar cwd
// stops at a first-run prompt before herdr ever reports a session id), so StartAgent alone cannot
// say when the pane is ready to be linked: this is what waits for that.
func (c *Client) WaitForAgentState(ctx context.Context, paneID string, until []string, timeout time.Duration) (Agent, error) {
	// herdr's own wait is bounded by timeout; cliTimeout is spent on the request/response round
	// trip around it, the same margin StartAgent gives its own timeout.
	callCtx, cancel := requestContext(ctx, timeout+cliTimeout)
	defer cancel()

	args := []string{"agent", "wait", paneID}
	for _, state := range until {
		args = append(args, "--until", state)
	}
	args = append(args, "--timeout", strconv.Itoa(int(timeout.Milliseconds())))

	out, err := c.runner.Run(callCtx, args...)
	if err != nil {
		return Agent{}, err
	}

	var payload struct {
		Agent Agent `json:"agent"`
	}
	if err := decodeResult(out, &payload); err != nil {
		return Agent{}, err
	}
	return payload.Agent, nil
}

// sessionPollInterval is how often WaitForAgentSession re-fetches the snapshot once the agent is
// idle but has not yet reported a session id.
const sessionPollInterval = 200 * time.Millisecond

// WaitForAgentSession waits for the agent in paneID to become idle (or blocked) and, when idle,
// for its native session id to appear.
//
// `agent start` succeeding and the session id being reported are two separate events with no
// ordering guarantee between them: the agent can be detected and ready for input before the
// integration hook that reports the session id has run. herdr's own wait has no way to block on
// the session id itself (--until only takes agent_status values), so once WaitForAgentState
// reports idle with no session yet, this re-fetches the snapshot every sessionPollInterval until
// one appears, the agent turns blocked, or timeout — the same single budget WaitForAgentState was
// given, not a second one stacked on top of it — runs out.
func (c *Client) WaitForAgentSession(ctx context.Context, paneID string, timeout time.Duration) (Agent, error) {
	deadline := time.Now().Add(timeout)

	agent, err := c.WaitForAgentState(ctx, paneID, []string{StateIdle, StateBlocked}, timeout)
	if err != nil {
		return Agent{}, err
	}

	for agent.SessionID() == "" && agent.AgentStatus != StateBlocked {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return agent, nil
		}
		wait := sessionPollInterval
		if wait > remaining {
			wait = remaining
		}
		if !sleep(ctx, wait) {
			return Agent{}, ctx.Err()
		}

		snapshot, err := c.Snapshot(ctx)
		if err != nil {
			return Agent{}, err
		}
		if found, ok := snapshot.AgentByPaneID(paneID); ok {
			agent = *found
		}
	}
	return agent, nil
}

// SendAgentPrompt sends text to an already-started agent's input.
//
// The text is never echoed into an error: execRunner's own error messages carry only the
// operation name (§3.2), and this method adds nothing of its own on top of what runner.Run returns.
func (c *Client) SendAgentPrompt(ctx context.Context, paneID, text string) error {
	callCtx, cancel := requestContext(ctx, cliTimeout)
	defer cancel()

	_, err := c.runner.Run(callCtx, "agent", "prompt", paneID, text)
	return err
}

// Notify raises a herdr notification. It is how a process with no one watching its output — a
// launch the board detached from itself before quitting — reports that it failed.
//
// body is optional; an empty one sends the title alone. Position and sound are left at herdr's
// defaults: this is a report, not something to be dismissed in a particular corner.
func (c *Client) Notify(ctx context.Context, title, body string) error {
	callCtx, cancel := requestContext(ctx, cliTimeout)
	defer cancel()

	args := []string{"notification", "show", title}
	if body != "" {
		args = append(args, "--body", body)
	}
	_, err := c.runner.Run(callCtx, args...)
	return err
}

// ReportTaskDisplay stamps the task id onto the pane so herdr's own UI can show it, and sets the
// sidebar's display name to "#<id> <title>" via --display-agent. Unlike the task id token, the
// display name has no uniqueness constraint (it is not the agent's herdr-internal identifier), so
// it carries the title as-is; herdr truncates it for display.
// herdr caps the token TTL at 24h, so the stamp is a display convenience and never a source of truth.
func (c *Client) ReportTaskDisplay(ctx context.Context, paneID string, taskID int, title string) error {
	callCtx, cancel := requestContext(ctx, cliTimeout)
	defer cancel()

	_, err := c.runner.Run(callCtx,
		"pane", "report-metadata", paneID,
		"--source", Source,
		"--token", "task="+strconv.Itoa(taskID),
		"--ttl-ms", strconv.Itoa(taskTokenTTL),
		"--display-agent", fmt.Sprintf("#%d %s", taskID, title),
	)
	return err
}

// decodeResult unwraps the {"id":...,"result":{...}} envelope herdr's CLI prints.
func decodeResult(out []byte, target any) error {
	var env envelope
	if err := json.Unmarshal(out, &env); err != nil {
		return fmt.Errorf("herdr の出力を解析できない: %w", err)
	}
	if env.Error != nil {
		return &APIError{Code: env.Error.Code, Message: env.Error.Message}
	}
	if len(env.Result) == 0 {
		return fmt.Errorf("herdr の出力に result が無い")
	}
	if err := json.Unmarshal(env.Result, target); err != nil {
		return fmt.Errorf("herdr の result を解析できない: %w", err)
	}
	return nil
}

// parseCLIError returns the API error carried by herdr's stdout, if any.
func parseCLIError(out []byte) *APIError {
	if len(out) == 0 {
		return nil
	}
	var env envelope
	if err := json.Unmarshal(out, &env); err != nil || env.Error == nil {
		return nil
	}
	return &APIError{Code: env.Error.Code, Message: env.Error.Message}
}
