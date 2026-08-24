package cli

import (
	"github.com/spf13/cobra"
	"github.com/ukwhatn/taskherd/internal/config"
	"github.com/ukwhatn/taskherd/internal/tui"
)

// pickerCmd is the picker pane entrypoint's entire body: herdr-plugin.toml declares it as a
// popup, launched only by the link-pane action with TASKHERD_TARGET_PANE set.
func (a *app) pickerCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "picker",
		Short:  "pane をタスクに紐づける選択 TUI（herdr-plugin.toml の picker entrypoint 専用）",
		Args:   cobra.NoArgs,
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			targetPane := a.env.Getenv(targetPaneEnv)
			if targetPane == "" {
				return &UserError{
					Msg:      targetPaneEnv + " が設定されていない",
					HintText: "picker は herdr プラグインの link-pane action からのみ起動する",
				}
			}
			cfg, err := a.config()
			if err != nil {
				return err
			}
			return tui.RunPicker(cmd.Context(), a.pickerDeps(cfg), targetPane)
		},
	}
}

func (a *app) pickerDeps(cfg *config.Config) tui.PickerDeps {
	return tui.PickerDeps{
		Tasks:   a.tasks(),
		Herdr:   a.herdr(),
		Columns: cfg.Columns,
		Now:     a.env.Now,
	}
}
