package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/ukwhatn/taskherd/internal/buildinfo"
)

// updateFoundMsg carries the tag of a newer release back to the board.
type updateFoundMsg struct {
	tag string
}

// checkUpdateCmd asks the releases page whether there is a newer taskherd, at most once a day.
//
// The board is the only place this happens: it is the one part of taskherd that lives long enough
// for a network round trip to cost nothing, and every short-lived command reads what it recorded
// instead of asking again. A failure produces no message at all — not knowing is the normal state
// of a machine that is offline, and it is not news.
func (b *Board) checkUpdateCmd() tea.Cmd {
	checker := b.deps.UpdateChecker
	if checker == nil {
		return nil
	}
	info := buildinfo.Get()
	if !info.Released() {
		return nil
	}

	return func() tea.Msg {
		state := checker.Load()
		if checker.Due(state) {
			state, _ = checker.Refresh(b.ctx)
		}
		tag := checker.Notice(state, info.Version)
		if tag == "" {
			return nil
		}
		checker.MarkNoticed(state, tag)
		return updateFoundMsg{tag: tag}
	}
}

// announceUpdate puts the news on the status line, where it sits until the next action replaces
// it. It is not an error, and it does not interrupt anything.
func (b *Board) announceUpdate(tag string) {
	b.setStatus(fmt.Sprintf(b.text.Board.UpdateAvailable, tag, buildinfo.Get().Version), false)
}
