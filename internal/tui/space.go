package tui

import (
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/ukwhatn/taskherd/internal/herdrc"
)

// spaceSelectState is the one-row space selector the launch and resume modals share: the spaces
// herdr reported, then a trailing row that creates one.
//
// It is a row rather than a list because the modals it sits in already spend their height on the
// cwd candidates and the prompt; a vertical space list on top of those overruns the body of an
// 80x24 terminal's modal and costs the prompt its last lines.
type spaceSelectState struct {
	spaces []herdrc.Workspace
	// cursor indexes spaces; len(spaces) is the "create a space" row.
	cursor   int
	newLabel textinput.Model
	// explicit records that the user moved the selection.
	//
	// Leaving it where it opened means "wherever herdr puts it", which is what every launch did
	// before spaces could be chosen — and what keeps a leftover pane in another space recoverable
	// (findReusableAgent refuses to recover across a space the caller named). Sending the focused
	// space just because the cursor started there would turn that recovery into an error nobody
	// asked for.
	explicit bool
}

// newSpaceSelect builds the selector from the board's last herdr snapshot, opening on the space
// herdr currently shows. A snapshot without spaces (herdr unreachable, or an older herdr that does
// not report them) yields an unavailable selector, which the modals simply do not draw.
func newSpaceSelect(snapshot *herdrc.Snapshot) spaceSelectState {
	if snapshot == nil || len(snapshot.Workspaces) == 0 {
		return spaceSelectState{}
	}
	state := spaceSelectState{
		spaces:   snapshot.Workspaces,
		newLabel: newFieldInput(),
	}
	for i := range state.spaces {
		if state.spaces[i].Focused {
			state.cursor = i
			break
		}
	}
	return state
}

func (s *spaceSelectState) available() bool { return len(s.spaces) > 0 }

// creating reports that the selection is on the row that makes a new space, which is the one row
// with a text field on it.
func (s *spaceSelectState) creating() bool {
	return s.available() && s.cursor == len(s.spaces)
}

func (s *spaceSelectState) move(delta int) {
	if !s.available() {
		return
	}
	next := s.cursor + delta
	if next < 0 || next > len(s.spaces) {
		return
	}
	s.cursor = next
	s.explicit = true
	if s.creating() {
		s.newLabel.Focus()
	} else {
		s.newLabel.Blur()
	}
}

// focusRow gives the keyboard to the label field when the selection sits on the create row, and
// takes it away otherwise. Called when the section itself gains or loses focus.
func (s *spaceSelectState) focusRow(focused bool) {
	if focused && s.creating() {
		s.newLabel.Focus()
		return
	}
	s.newLabel.Blur()
}

func (s *spaceSelectState) choice() SpaceChoice {
	switch {
	case !s.available() || !s.explicit:
		return SpaceChoice{}
	case s.creating():
		return SpaceChoice{Create: true, Label: strings.TrimSpace(s.newLabel.Value())}
	default:
		return SpaceChoice{WorkspaceID: s.spaces[s.cursor].WorkspaceID}
	}
}

// update routes a key or paste event to the label field, which only exists on the create row.
func (s *spaceSelectState) update(msg tea.Msg) tea.Cmd {
	if !s.creating() {
		return nil
	}
	updated, cmd := s.newLabel.Update(msg)
	s.newLabel = updated
	return cmd
}

// render draws the selector as one row, windowed to the width available. The cells are windowed as
// plain text and styled afterwards, the same way the status selector does it: trimming a styled
// cell would cut an escape sequence.
func (b *Board) renderSpaceRow(s *spaceSelectState, label string, inner int, focused bool) string {
	marker := padCell("", cursorWidth(b.icons))
	if focused {
		marker = b.icons.Cursor + " "
	}
	head := marker + label
	room := maxInt(inner-lipgloss.Width(head), 1)

	if s.creating() {
		// The label field replaces the cell run rather than sitting after it: a text field is
		// where the eye goes, and the spaces it would be listed beside are not choosable while it
		// has the keyboard anyway.
		prefix := head + b.text.Start.NewSpace + " "
		s.newLabel.SetWidth(fieldWidth(s.newLabel, inner-lipgloss.Width(prefix)))
		return truncate(prefix+s.newLabel.View(), inner)
	}

	cells := make([]string, 0, len(s.spaces)+1)
	for i := range s.spaces {
		cells = append(cells, " "+spaceLabel(s.spaces[i])+" ")
	}
	cells = append(cells, " "+b.text.Start.NewSpace+" ")

	visible, start := visibleCells(cells, s.cursor, room)
	for i := range visible {
		if start+i == s.cursor {
			visible[i] = b.styles.cardTitleSelected.Render(visible[i])
		}
	}
	return truncate(head, inner) + strings.Join(visible, " ")
}

// spaceLabel is how one space reads in the selector. herdr allows an unnamed space, which shows as
// its number there and has to show as something here too.
func spaceLabel(space herdrc.Workspace) string {
	if label := strings.TrimSpace(space.Label); label != "" {
		return label
	}
	return "#" + strconv.Itoa(space.Number)
}
