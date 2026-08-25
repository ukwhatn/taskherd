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

	// Derived rather than passed straight through: cobra's own context lives until the process
	// exits, so without a cancel here anything the board still has in flight when the user quits
	// — the wait step of a session-start launch, most visibly — keeps running past the point where
	// nothing is left to report its result to. Every per-operation context the board creates is
	// derived from this one (Board.ctx, in turn Board.launch.ctx), so cancelling it here reaches
	// all of them without each cancellation path having to call its own cancel explicitly.
	boardCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	program := tea.NewProgram(New(boardCtx, deps, settings), tea.WithContext(boardCtx))
	if _, err := program.Run(); err != nil {
		return fmt.Errorf("board を実行できない: %w", err)
	}
	return nil
}
