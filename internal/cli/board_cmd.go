package cli

import (
	"context"
	"time"

	"github.com/spf13/cobra"
	"github.com/ukwhatn/taskherd/internal/config"
	"github.com/ukwhatn/taskherd/internal/model"
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
			// Checked before boardDeps, which is what starts the file and herdr watchers: a refusal
			// after that point would return without the deferred Close ever being registered.
			if err := requireOpenColumn(cfg.Columns); err != nil {
				return err
			}
			return tui.Run(cmd.Context(), a.boardDeps(cmd.Context(), cfg), a.boardSettings(cfg))
		},
	}
}

// requireOpenColumn refuses to open a board that would have no column to put a cursor on.
//
// Terminal columns are folded into the stack at the board's right edge and are skipped by the
// cursor, so a column set that is all terminal leaves the board with nothing to focus. This is a
// rule about the board and not about the config: Columns.Validate runs for every command, and
// list, show and add all work perfectly well with such a column set.
func requireOpenColumn(columns model.Columns) error {
	for _, col := range columns {
		if col.Kind == model.ColumnKindOpen {
			return nil
		}
	}
	return &UserError{
		Msg:      `board を開くには kind = "open" の列が最低 1 つ必要`,
		HintText: "config.toml の [[columns]] に open の列を足す（場所は taskherd config path）",
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
	if launcher, err := newDetachedLauncher(a.env.Paths.StateDir, a.env.Now); err == nil {
		deps.Launcher = launcher
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
		SessionStart:    cfg.SessionStart,
	}
}
