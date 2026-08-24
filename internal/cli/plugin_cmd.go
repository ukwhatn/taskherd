package cli

import (
	"github.com/spf13/cobra"
	"github.com/ukwhatn/taskherd/internal/herdrc"
)

// herdrPluginID is this plugin's own id, matching herdr-plugin.toml.
const herdrPluginID = "taskherd"

// targetPaneEnv is how link-pane hands the target pane to picker: a popup process never
// receives HERDR_PANE_ID itself (herdr-plugin.toml's picker entrypoint is a popup), so the
// pane the action was invoked against has to be passed through --env instead.
const targetPaneEnv = "TASKHERD_TARGET_PANE"

// pluginCmd groups the internal commands herdr-plugin.toml's actions exec. They are not meant
// to be run by hand, so they stay hidden from --help.
func (a *app) pluginCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "plugin",
		Short:  "herdr プラグインの action から呼ばれる内部コマンド",
		Hidden: true,
	}
	cmd.AddCommand(a.pluginOpenBoardCmd(), a.pluginLinkPaneCmd())
	return cmd
}

// pluginOpenBoardCmd is the open-board action's entire body: hand off to
// `herdr plugin pane open`, which applies the board entrypoint's manifest placement (overlay).
func (a *app) pluginOpenBoardCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "open-board",
		Short:  "board pane を開く（herdr-plugin.toml の open-board action の実体）",
		Args:   cobra.NoArgs,
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.herdr().OpenPluginPane(cmd.Context(), herdrPluginID, "board", nil)
		},
	}
}

// pluginLinkPaneCmd is the link-pane action's entire body. It identifies the pane the action was
// invoked against and opens the picker popup targeting it.
//
// The pane id is read from HERDR_PLUGIN_CONTEXT_JSON's focused_pane_id first (confirmed against
// real herdr 0.8.2: {"focused_pane_id":"...",...}, a flat shape undocumented in herdr's docs),
// falling back to HERDR_PANE_ID directly, which the same real invocation also carried. Both are
// kept because there is no guarantee a future herdr version keeps injecting both for every
// invocation source (link-pane's contexts=["pane"] was only tested via CLI invocation, not a
// real keybinding or link click).
func (a *app) pluginLinkPaneCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "link-pane",
		Short:  "この pane をタスクに紐づける picker を開く（herdr-plugin.toml の link-pane action の実体）",
		Args:   cobra.NoArgs,
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			paneID := herdrc.ParsePluginContext(a.env.Getenv("HERDR_PLUGIN_CONTEXT_JSON")).PaneID()
			if paneID == "" {
				paneID = a.env.Getenv("HERDR_PANE_ID")
			}
			if paneID == "" {
				return &UserError{
					Msg:      "action の呼び出し元 pane を特定できない",
					HintText: "この action は pane を対象にした操作からのみ呼び出せる",
				}
			}
			return a.herdr().OpenPluginPane(cmd.Context(), herdrPluginID, "picker", map[string]string{
				targetPaneEnv: paneID,
			})
		},
	}
}
