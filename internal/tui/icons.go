package tui

import (
	"strings"

	"github.com/ukwhatn/taskherd/internal/model"
)

// IconMode is the glyph vocabulary the board draws with.
type IconMode string

const (
	// IconNerd draws Nerd Font glyphs. It needs a patched font in the terminal, which is the
	// default because the glyphs are the only symbols guaranteed to occupy exactly one cell.
	IconNerd IconMode = "nerd"
	// IconASCII draws compact ASCII stand-ins, for a terminal without a patched font.
	IconASCII IconMode = "ascii"
	// IconNone draws no symbols at all and spells every state out as a word.
	IconNone IconMode = "none"
)

// ParseIconMode resolves a config value, reporting whether it named a known mode.
func ParseIconMode(s string) (IconMode, bool) {
	switch IconMode(s) {
	case IconNerd:
		return IconNerd, true
	case IconASCII:
		return IconASCII, true
	case IconNone:
		return IconNone, true
	default:
		return IconNerd, false
	}
}

// IconSet is one mode's vocabulary. Every glyph in the nerd set is a Nerd Font private-use
// codepoint, which is what makes it exactly one cell wide in a patched monospace font.
type IconSet struct {
	Mode IconMode

	// StateInLinkIcon reports whether the link icon itself distinguishes open from draft, merged
	// and closed. When it does not, the row spells the state out after the reference instead.
	StateInLinkIcon bool

	PROpen, PRDraft, PRMerged, PRClosed string
	IssueOpen, IssueClosed              string
	Jira, Link                          string
	// More opens the row standing in for the links a card had no room to draw.
	More string

	Pass, Fail, Pending string
	// Alert marks a link whose refresh keeps failing, which is the one state that is about the
	// board itself rather than about the thing linked to.
	Alert string

	SessionBlocked, SessionWorking, SessionDone, SessionIdle, SessionOffline string

	// Due prefixes the due date. It is empty in the modes that have no glyph to spare for it.
	Due, DueOverdue string

	ScrollUp, ScrollDown      string
	Cursor, Collapsed         string
	CardEdge, CardEdgeFocused string

	// Arrow* name the arrow keys in the footer's key help.
	ArrowLeft, ArrowRight, ArrowUp, ArrowDown string
}

// horizontalKeys and verticalKeys name a pair of arrow keys the way the key help shows them.
// The none mode spells the keys out, so it needs a separator the glyph modes do not.
func (s IconSet) horizontalKeys() string { return s.arrowPair(s.ArrowLeft, s.ArrowRight) }
func (s IconSet) verticalKeys() string   { return s.arrowPair(s.ArrowUp, s.ArrowDown) }

// tag labels a state mark, as in "CI" plus a pass mark. The none mode spells its marks out as
// words, so it needs the separator that the glyph modes would only waste a cell on.
func (s IconSet) tag(label, mark string) string {
	if s.Mode == IconNone {
		return label + " " + mark
	}
	return label + mark
}

func (s IconSet) arrowPair(first, second string) string {
	if s.Mode == IconNone {
		return first + "/" + second
	}
	return first + second
}

// Nerd Font glyphs, by their glyphnames.json name in Nerd Fonts 3.5.1. Every one of them was
// checked against the codepoints JetBrainsMono Nerd Font actually maps.
const (
	nfOctGitPullRequest       = "\uf407" // nf-oct-git_pull_request
	nfOctGitPullRequestDraft  = "\uf4dd" // nf-oct-git_pull_request_draft
	nfOctGitPullRequestClosed = "\uf4dc" // nf-oct-git_pull_request_closed
	nfOctGitMerge             = "\uf419" // nf-oct-git_merge
	nfOctIssueOpened          = "\uf41b" // nf-oct-issue_opened
	nfOctIssueClosed          = "\uf41d" // nf-oct-issue_closed
	nfFaJira                  = "\uef56" // nf-fa-jira
	nfOctLink                 = "\uf44c" // nf-oct-link
	nfOctCheck                = "\uf42e" // nf-oct-check
	nfOctX                    = "\uf467" // nf-oct-x
	nfOctClock                = "\uf43a" // nf-oct-clock
	nfOctAlert                = "\uf421" // nf-oct-alert
	nfOctDotFill              = "\uf444" // nf-oct-dot_fill
	nfOctCheckCircleFill      = "\uf4a4" // nf-oct-check_circle_fill
	nfOctDot                  = "\uf4c3" // nf-oct-dot
	nfOctCircleSlash          = "\uf468" // nf-oct-circle_slash
	nfOctCalendar             = "\uf455" // nf-oct-calendar
	nfOctChevronUp            = "\uf47b" // nf-oct-chevron_up
	nfOctChevronDown          = "\uf47c" // nf-oct-chevron_down
	nfOctChevronRight         = "\uf460" // nf-oct-chevron_right
	nfOctArrowUp              = "\uf431" // nf-oct-arrow_up
	nfOctArrowRight           = "\uf432" // nf-oct-arrow_right
	nfOctArrowDown            = "\uf433" // nf-oct-arrow_down
	nfOctArrowLeft            = "\uf434" // nf-oct-arrow_left
	nfOctEllipsis             = "\uf475" // nf-oct-ellipsis
)

var nerdIcons = IconSet{
	Mode:            IconNerd,
	StateInLinkIcon: true,

	PROpen:      nfOctGitPullRequest,
	PRDraft:     nfOctGitPullRequestDraft,
	PRMerged:    nfOctGitMerge,
	PRClosed:    nfOctGitPullRequestClosed,
	IssueOpen:   nfOctIssueOpened,
	IssueClosed: nfOctIssueClosed,
	Jira:        nfFaJira,
	Link:        nfOctLink,
	More:        nfOctEllipsis,

	Pass:    nfOctCheck,
	Fail:    nfOctX,
	Pending: nfOctClock,
	Alert:   nfOctAlert,

	SessionBlocked: nfOctAlert,
	SessionWorking: nfOctDotFill,
	SessionDone:    nfOctCheckCircleFill,
	SessionIdle:    nfOctDot,
	SessionOffline: nfOctCircleSlash,

	Due:        nfOctCalendar,
	DueOverdue: nfOctAlert,

	ScrollUp:        nfOctChevronUp,
	ScrollDown:      nfOctChevronDown,
	Cursor:          nfOctChevronRight,
	Collapsed:       nfOctChevronRight,
	CardEdge:        "│",
	CardEdgeFocused: "┃",

	ArrowLeft:  nfOctArrowLeft,
	ArrowRight: nfOctArrowRight,
	ArrowUp:    nfOctArrowUp,
	ArrowDown:  nfOctArrowDown,
}

var asciiIcons = IconSet{
	Mode: IconASCII,

	PROpen:      "PR",
	PRDraft:     "PR",
	PRMerged:    "PR",
	PRClosed:    "PR",
	IssueOpen:   "IS",
	IssueClosed: "IS",
	Jira:        "JR",
	Link:        "LN",
	More:        "..",

	Pass:    "+",
	Fail:    "!",
	Pending: "*",
	Alert:   "!",

	SessionBlocked: "!",
	SessionWorking: "*",
	SessionDone:    "+",
	SessionIdle:    ".",
	SessionOffline: "-",

	ScrollUp:        "^",
	ScrollDown:      "v",
	Cursor:          ">",
	Collapsed:       ">",
	CardEdge:        "|",
	CardEdgeFocused: "|",

	ArrowLeft:  "<",
	ArrowRight: ">",
	ArrowUp:    "^",
	ArrowDown:  "v",
}

var noneIcons = IconSet{
	Mode: IconNone,

	PROpen:      "PR",
	PRDraft:     "PR",
	PRMerged:    "PR",
	PRClosed:    "PR",
	IssueOpen:   "Issue",
	IssueClosed: "Issue",
	Jira:        "Jira",
	Link:        "link",
	More:        "..",

	Pass:    "ok",
	Fail:    "ng",
	Pending: "run",
	// Alert is empty here: failureMark spells the word out in this mode.
	Alert: "",

	SessionBlocked: "",
	SessionWorking: "",
	SessionDone:    "",
	SessionIdle:    "",
	SessionOffline: "",

	// Spelled the same way as the arrow names below: this mode replaces glyphs with the direction's
	// name, and one direction named in Japanese while the four beside it were named in English was
	// an oversight rather than a choice.
	ScrollUp:        "up",
	ScrollDown:      "down",
	Cursor:          ">",
	Collapsed:       ">",
	CardEdge:        "|",
	CardEdgeFocused: "|",

	ArrowLeft:  "left",
	ArrowRight: "right",
	ArrowUp:    "up",
	ArrowDown:  "down",
}

// Icons returns the vocabulary for a mode. An unknown mode falls back to nerd, which is what
// config validation already guarantees; the fallback only keeps a zero value from blanking the UI.
func Icons(mode IconMode) IconSet {
	switch mode {
	case IconASCII:
		return asciiIcons
	case IconNone:
		return noneIcons
	default:
		return nerdIcons
	}
}

// declaredIcons is every glyph the icon sets draw, used by UnsafeWidthRunes to tell a deliberate
// private-use glyph apart from a mistyped codepoint.
var declaredIcons = buildDeclaredIcons()

func buildDeclaredIcons() map[rune]bool {
	declared := map[rune]bool{}
	for _, set := range []IconSet{nerdIcons, asciiIcons, noneIcons} {
		for _, glyph := range set.all() {
			for _, r := range glyph {
				declared[r] = true
			}
		}
	}
	return declared
}

// all lists every glyph in the set, so a new field cannot be forgotten by the checks that walk it.
func (s IconSet) all() []string {
	return []string{
		s.PROpen, s.PRDraft, s.PRMerged, s.PRClosed,
		s.IssueOpen, s.IssueClosed, s.Jira, s.Link, s.More,
		s.Pass, s.Fail, s.Pending, s.Alert,
		s.SessionBlocked, s.SessionWorking, s.SessionDone, s.SessionIdle, s.SessionOffline,
		s.Due, s.DueOverdue,
		s.ScrollUp, s.ScrollDown, s.Cursor, s.Collapsed, s.CardEdge, s.CardEdgeFocused,
		s.ArrowLeft, s.ArrowRight, s.ArrowUp, s.ArrowDown,
	}
}

// linkGlyph is the glyph that opens a link row. The colour it is drawn in comes from linkTone, not
// from here: a glyph and a tone answer to different things, and only the nerd set has a glyph per
// state to answer with at all.
func (s IconSet) linkGlyph(kind model.LinkKind, phase linkPhase) string {
	switch kind {
	case model.LinkKindGitHubPR:
		switch phase {
		case phaseDraft:
			return s.PRDraft
		case phaseMerged:
			return s.PRMerged
		case phaseClosed:
			return s.PRClosed
		default:
			return s.PROpen
		}
	case model.LinkKindGitHubIssue:
		if phase == phaseClosed || phase == phaseMerged {
			return s.IssueClosed
		}
		return s.IssueOpen
	case model.LinkKindJira:
		return s.Jira
	default:
		return s.Link
	}
}

// failureMark says that refreshing a link is failing, and for how long. The nerd glyph carries the
// meaning by itself; the other modes have no glyph that does, so they spell the word out — failed
// is passed in rather than read from a catalog here, because an icon set knows about glyphs and
// nothing about language.
func (s IconSet) failureMark(failed, age string) string {
	parts := make([]string, 0, 3)
	if s.Alert != "" {
		parts = append(parts, s.Alert)
	}
	if s.Mode != IconNerd {
		parts = append(parts, failed)
	}
	if age != "" {
		parts = append(parts, age)
	}
	return strings.Join(parts, " ")
}
