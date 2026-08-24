package cli

import (
	"context"
	"time"

	"github.com/spf13/cobra"
	"github.com/ukwhatn/taskherd/internal/config"
	"github.com/ukwhatn/taskherd/internal/tui"
)

func (a *app) boardCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "board",
		Short: "kanban ボード（TUI）を開く",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if a.jsonOut {
				return &UserError{
					Msg:      "board は対話 TUI のため --json では起動できない",
					HintText: "機械可読な出力は list --json / show --json を使う",
				}
			}
			cfg, err := a.config()
			if err != nil {
				return err
			}
			return tui.Run(cmd.Context(), a.boardDeps(cmd.Context(), cfg), a.boardSettings(cfg))
		},
	}
}

// boardDeps wires the board to the real store, herdr and fetch. A source that cannot be started
// is left nil, which disables that feature on the board instead of refusing to open it: the task
// management core has to work without herdr, and without a usable watcher.
func (a *app) boardDeps(ctx context.Context, cfg *config.Config) tui.Deps {
	deps := tui.Deps{
		Tasks: a.tasks(),
		Cache: a.cache(),
		Links: a.fetcher(cfg),
		Now:   a.env.Now,
	}

	if watcher, err := a.tasks().Watch(); err == nil {
		deps.Files = watcher
	}
	client := a.herdr()
	deps.Herdr = client
	deps.Sessions = client.Watch(ctx)
	return deps
}

func (a *app) boardSettings(cfg *config.Config) tui.Settings {
	return tui.Settings{
		Columns:         cfg.Columns,
		Editor:          cfg.ResolveEditor(a.env.Getenv),
		Classifier:      cfg.Classifier(),
		CacheTTL:        time.Duration(cfg.Board.CacheTTLMinutes) * time.Minute,
		RefreshInterval: time.Duration(cfg.Board.RefreshIntervalMinutes) * time.Minute,
		Icons:           tui.IconMode(cfg.Board.Icons),
		Hyperlinks:      cfg.Board.Hyperlinks,
	}
}
