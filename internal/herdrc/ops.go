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

// ReportTaskToken stamps the task id onto the pane so herdr's own UI can show it.
// herdr caps the TTL at 24h, so the stamp is a display convenience and never a source of truth.
func (c *Client) ReportTaskToken(ctx context.Context, paneID string, taskID int) error {
	callCtx, cancel := requestContext(ctx, cliTimeout)
	defer cancel()

	_, err := c.runner.Run(callCtx,
		"pane", "report-metadata", paneID,
		"--source", Source,
		"--token", "task="+strconv.Itoa(taskID),
		"--ttl-ms", strconv.Itoa(taskTokenTTL),
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
