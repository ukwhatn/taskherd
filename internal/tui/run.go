package tui

import (
	"context"
	"fmt"

	tea "charm.land/bubbletea/v2"
)

// Run starts the board and blocks until the user quits.
//
// Every live source is closed on the way out: the board holds a herdr subscription and a file
// watcher only while it is open, and nothing of taskherd survives it.
func Run(ctx context.Context, deps Deps, settings Settings) error {
	if deps.Tasks == nil {
		return fmt.Errorf("タスクストアが設定されていない")
	}
	if deps.Files != nil {
		defer func() {
			_ = deps.Files.Close()
		}()
	}
	if deps.Sessions != nil {
		defer deps.Sessions.Close()
	}

	program := tea.NewProgram(New(ctx, deps, settings), tea.WithContext(ctx))
	if _, err := program.Run(); err != nil {
		return fmt.Errorf("board を実行できない: %w", err)
	}
	return nil
}
