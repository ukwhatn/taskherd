package tui

import (
	"testing"
	"time"

	"github.com/ukwhatn/taskherd/internal/herdrc"
	"github.com/ukwhatn/taskherd/internal/model"
)

// testIcons is the ascii vocabulary, used wherever a test asserts on text: its glyphs are
// readable in a failure message, and the nerd set is checked separately for what only it can say.
var testIcons = Icons(IconASCII)

func sessionTask(ids ...string) model.Task {
	t := model.Task{ID: 1, Title: "t", Status: "todo"}
	for _, id := range ids {
		t.Sessions = append(t.Sessions, model.SessionRef{Agent: "claude", SessionID: id, Cwd: "/tmp"})
	}
	return t
}

func liveStates(pairs map[string]string) SessionStates {
	return SessionStates{Available: true, State: pairs, Pane: map[string]string{}, Agent: map[string]string{}}
}

func TestSessionBadgeEmptyWithoutSessions(t *testing.T) {
	badge := BuildSessionBadge(model.Task{ID: 1}, liveStates(nil), testIcons)

	if badge.Text != "" {
		t.Errorf("Text = %q, want 空（報告するものがない）", badge.Text)
	}
}

// herdr being unreachable and a pane having disappeared are both "no live state", and the card
// says so the same way.
func TestSessionBadgeOfflineWhenHerdrUnavailable(t *testing.T) {
	badge := BuildSessionBadge(sessionTask("a"), UnavailableSessions(nil), testIcons)

	if badge.Text != "- offline" {
		t.Errorf("Text = %q, want %q", badge.Text, "- offline")
	}
	if badge.State != herdrc.StateOffline {
		t.Errorf("State = %q, want %q", badge.State, herdrc.StateOffline)
	}
}

func TestSessionBadgeUsesMostAttentionWorthyState(t *testing.T) {
	tests := []struct {
		name   string
		states map[string]string
		want   string
	}{
		{"blocked が working に勝つ", map[string]string{"a": herdrc.StateWorking, "b": herdrc.StateBlocked}, "! blocked x2"},
		{"working が done に勝つ", map[string]string{"a": herdrc.StateDone, "b": herdrc.StateWorking}, "* working x2"},
		{"done が idle に勝つ", map[string]string{"a": herdrc.StateIdle, "b": herdrc.StateDone}, "+ done x2"},
		{"idle だけ", map[string]string{"a": herdrc.StateIdle, "b": herdrc.StateIdle}, ". idle x2"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			badge := BuildSessionBadge(sessionTask("a", "b"), liveStates(tc.states), testIcons)
			if badge.Text != tc.want {
				t.Errorf("Text = %q, want %q", badge.Text, tc.want)
			}
		})
	}
}

// A session herdr does not know about has lost its pane, which is offline rather than unknown.
func TestSessionBadgeMissingSessionIsOffline(t *testing.T) {
	badge := BuildSessionBadge(sessionTask("gone"), liveStates(map[string]string{}), testIcons)

	if badge.Text != "- offline" {
		t.Errorf("Text = %q, want %q", badge.Text, "- offline")
	}
}

func TestSessionBadgeSingleSessionHasNoCount(t *testing.T) {
	badge := BuildSessionBadge(sessionTask("a"), liveStates(map[string]string{"a": herdrc.StateWorking}), testIcons)

	if badge.Text != "* working" {
		t.Errorf("Text = %q, want %q", badge.Text, "* working")
	}
}

// The none mode has no glyph to put in front of a state, and must not leave a stray space where
// one would have gone.
func TestSessionBadgeNoneModeIsWordOnly(t *testing.T) {
	badge := BuildSessionBadge(sessionTask("a"), liveStates(map[string]string{"a": herdrc.StateWorking}), Icons(IconNone))

	if badge.Text != "working" {
		t.Errorf("Text = %q, want %q", badge.Text, "working")
	}
}

func TestFormatAge(t *testing.T) {
	tests := []struct {
		in   time.Duration
		want string
	}{
		{30 * time.Second, "now"},
		{90 * time.Second, "1m"},
		{2 * time.Hour, "2h"},
		{50 * time.Hour, "2d"},
	}
	for _, tc := range tests {
		if got := FormatAge(tc.in); got != tc.want {
			t.Errorf("FormatAge(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
