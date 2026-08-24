package herdrc

import (
	"context"
	"encoding/json"
	"sort"
)

// PluginContext is the invocation context herdr injects as HERDR_PLUGIN_CONTEXT_JSON. Only the
// field taskherd's plugin commands need is decoded; herdr documents more (workspace, tab,
// worktree, agent, selection, clicked URL) that this type ignores.
type PluginContext struct {
	Pane *PluginContextPane `json:"pane"`
}

// PluginContextPane identifies the pane an action was invoked against.
type PluginContextPane struct {
	PaneID string `json:"pane_id"`
}

// ParsePluginContext decodes HERDR_PLUGIN_CONTEXT_JSON. An empty or unparsable value yields a
// zero PluginContext rather than an error: a plugin command run outside herdr, or against an
// older herdr that omits a field, should degrade rather than fail on this step alone.
func ParsePluginContext(raw string) PluginContext {
	if raw == "" {
		return PluginContext{}
	}
	var ctx PluginContext
	if err := json.Unmarshal([]byte(raw), &ctx); err != nil {
		return PluginContext{}
	}
	return ctx
}

// PaneID returns the pane this invocation targets, or "" when none is available.
func (c PluginContext) PaneID() string {
	if c.Pane == nil {
		return ""
	}
	return c.Pane.PaneID
}

// OpenPluginPane opens one of this plugin's own pane entrypoints via `herdr plugin pane open`.
// placement is left unspecified so the manifest's own declaration on the entrypoint applies:
// herdr 0.8.2's CLI does not accept "popup" as a --placement override (only overlay/split/
// tab/zoomed), so a popup entrypoint can only be reached by leaving placement to the manifest.
// env sets additional variables for the launched process; herdr-managed variables always win
// over these regardless of what is passed here.
func (c *Client) OpenPluginPane(ctx context.Context, pluginID, entrypoint string, env map[string]string) error {
	callCtx, cancel := requestContext(ctx, cliTimeout)
	defer cancel()

	args := []string{"plugin", "pane", "open", "--plugin", pluginID, "--entrypoint", entrypoint}
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		args = append(args, "--env", k+"="+env[k])
	}

	_, err := c.runner.Run(callCtx, args...)
	return err
}
