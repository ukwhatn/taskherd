package cli

import (
	"context"

	"github.com/spf13/cobra"
	"github.com/ukwhatn/taskherd/internal/config"
	"github.com/ukwhatn/taskherd/internal/model"
	"github.com/ukwhatn/taskherd/internal/tui"
)

// pickerCmd is the picker pane entrypoint's entire body: herdr-plugin.toml declares it as a
// popup, launched only by the link-pane action with TASKHERD_TARGET_PANE set.
//
// A pane whose session is already linked to a task skips the picker entirely and opens straight
// into that task's detail (prefix+t's "edit the task I'm already working from" behaviour): the
// picker's own job is choosing which task a pane belongs to, and that choice is already made.
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

			if taskID, ok := a.resolvePaneDetailTask(cmd.Context(), targetPane); ok {
				// requireOpenColumn is deliberately not checked here: it guards a board the cursor
				// moves around on, not one task's detail opened by id, and Columns.Validate allows a
				// terminal-only config that would otherwise refuse this whole feature outright.
				settings := a.boardSettings(cfg)
				settings.DetailTaskID = taskID
				return tui.Run(cmd.Context(), a.boardDeps(cmd.Context(), cfg), settings)
			}
			return tui.RunPicker(cmd.Context(), a.pickerDeps(cfg), targetPane)
		},
	}
}

// resolvePaneDetailTask decides whether prefix+t should skip the picker: whichever task already
// carries the session herdr currently shows in targetPane, if one does. Every failure along the
// way — herdr unreachable, no session resolved, no task carrying it, tasks.json unreadable — falls
// back to opening the picker rather than refusing to start anything, since deciding this is not
// worth losing the popup over.
func (a *app) resolvePaneDetailTask(ctx context.Context, targetPane string) (int, bool) {
	sessionID, ok := resolvePaneSessionID(ctx, a.herdr(), targetPane)
	if !ok {
		return 0, false
	}
	file, err := a.tasks().Load()
	if err != nil {
		return 0, false
	}
	return taskForSession(file.Tasks, sessionID)
}

// resolvePaneSessionID asks herdr which native session, if any, currently occupies targetPane.
// Kept apart from resolvePaneDetailTask so the decision itself — not just its result — is
// something a test can drive directly: RunPicker and tui.Run both block on a real bubbletea
// program, which rules out exercising this branch through the command end to end.
func resolvePaneSessionID(ctx context.Context, herdr tui.PickerHerdrOps, targetPane string) (string, bool) {
	snapshot, err := herdr.Snapshot(ctx)
	if err != nil {
		return "", false
	}
	agent, ok := snapshot.AgentByPaneID(targetPane)
	if !ok {
		return "", false
	}
	return agent.SessionID(), agent.SessionID() != ""
}

// taskForSession picks which task carries the given session: the lowest id among every task that
// does, so which one wins does not depend on tasks.json's save order (a session can end up linked
// to more than one task by mistake).
func taskForSession(tasks []model.Task, sessionID string) (int, bool) {
	found := false
	best := 0
	for _, task := range tasks {
		for _, session := range task.Sessions {
			if session.SessionID != sessionID {
				continue
			}
			if !found || task.ID < best {
				best, found = task.ID, true
			}
			break
		}
	}
	return best, found
}

func (a *app) pickerDeps(cfg *config.Config) tui.PickerDeps {
	return tui.PickerDeps{
		Tasks:   a.tasks(),
		Herdr:   a.herdr(),
		Columns: cfg.Columns,
		Icons:   tui.IconMode(cfg.Board.Icons),
		Now:     a.env.Now,
	}
}
