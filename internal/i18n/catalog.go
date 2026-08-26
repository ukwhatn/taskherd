package i18n

// Catalog is every string the UI can show, grouped by the screen it belongs to.
//
// Fields whose name ends in a verb-like word hold a format string; the doc comment on each names
// its arguments. Where the two languages want the arguments in different orders, the entry uses
// explicit indexes (%[1]s), which is why catalog_test.go compares the *set* of verbs rather than
// their sequence.
type Catalog struct {
	Common Common
	Board  Board
	Detail Detail
	Add    Add
	Start  Start
	Jump   Jump
	Select Select
	Picker Picker
}

// Common is the vocabulary shared across screens. A word only belongs here when the two uses would
// always want to change together — a value that reads the same in two places by coincidence is
// two entries, not one.
type Common struct {
	// None stands in for an empty field value.
	None string
	// Unknown is a live state that could not be classified.
	Unknown string
	// NotFetched is a link whose live state has never been read.
	NotFetched string
	// Failed is the word icon sets without a glyph for it spell out on a failing link.
	Failed string
	// NoCardSelected is the refusal shared by every action that needs a card under the cursor.
	NoCardSelected string
	// NoColumns is the refusal shared by every action that needs at least one defined column.
	NoColumns string
	// HerdrUnreachable is herdr being unreachable, stated without saying what it blocks.
	HerdrUnreachable string
	// Cancelled acknowledges a confirmation answered with no.
	Cancelled string
	// ConfirmTitle is the confirmation dialog's title.
	ConfirmTitle string
	// ConfirmHelp is the confirmation dialog's key line.
	ConfirmHelp string
	// LinkNeedsScheme rejects a URL written without one. Takes the offending field (%q).
	LinkNeedsScheme string
}

// Board is the board screen: its footer, its placeholders and its status line.
type Board struct {
	// Help is the footer's key list. Takes the horizontal and vertical arrow glyphs.
	Help string
	// AllCollapsed replaces the columns when every one of them is folded.
	AllCollapsed string
	// TooNarrow replaces the columns when not even one fits.
	TooNarrow string
	// ColumnWindow says which slice of the columns is on screen. Takes the ellipsis mark, the
	// first and last visible column numbers, the total, and the horizontal arrow glyphs.
	ColumnWindow string
	// MoreCount labels the cards scrolled out of a column. Takes the count.
	MoreCount string
	// MoreLinks labels the links that did not fit on a card. Takes the count.
	MoreLinks string
	// EmptyColumn is the placeholder inside a column holding nothing.
	EmptyColumn string
	// HerdrDisabled / HerdrOffline / HerdrConnecting are the footer's herdr state words.
	HerdrDisabled   string
	HerdrOffline    string
	HerdrConnecting string
	// LiveRefreshing / LiveNotFetched are the footer's live-fetch state words. Separate from
	// Refreshing and Common.NotFetched because the footer is a state label and the status line is
	// a sentence, which English capitalises differently.
	LiveRefreshing string
	LiveNotFetched string
	// NextRefresh is appended to the footer while a backoff is in effect. Takes the wait.
	NextRefresh string
	// Refreshing tells the user a fetch is already running.
	Refreshing string
	// RefreshDisabled is the refusal when no fetcher is configured.
	RefreshDisabled string
	// TaskHasNoLinks / NoLinksAtAll are the refusals for r and R with nothing to fetch.
	TaskHasNoLinks string
	NoLinksAtAll   string
	// RateLimited reports a cycle cut short by a rate limit. Takes the wait before the next one.
	RateLimited string
	// RefreshedSome reports a cycle with failures. Takes the succeeded count, the failed count,
	// and the first failure's reason.
	RefreshedSome string
	// Refreshed reports a clean manual cycle. Takes the count.
	Refreshed string
	// EditorFailed / EditorReadFailed report an external editor that did not work out. Each takes
	// the underlying error.
	EditorFailed     string
	EditorReadFailed string
	// NoEditor is the refusal when no editor is configured anywhere.
	NoEditor string
	// ConfirmDelete asks before deleting a task. Takes the id and the title.
	ConfirmDelete string
	// TitleEmpty rejects an empty title.
	TitleEmpty string
	// The remaining entries acknowledge a completed mutation. Each takes the task id first.
	NoteUpdated     string
	Created         string
	CreatedMany     string // takes the count and the status
	LinksAdded      string // takes the id and the count
	LinksAddedSome  string // takes the id, the added count and the already-present count
	TitleUpdated    string
	DueUpdated      string
	Moved           string // takes the id and the status
	LinkNoteUpdated string
	Deleted         string
	LinkRemoved     string
	SessionDetached string
	SessionAttached string // takes the id and the agent name
}

// Detail is the detail modal.
type Detail struct {
	// Help is the modal's key list. Takes the vertical and horizontal arrow glyphs.
	Help string
	// HelpEditing replaces Help while a field is open for editing.
	HelpEditing string
	// The Label fields name the rows of the item list.
	LabelTitle   string
	LabelStatus  string
	LabelDue     string
	LabelNote    string
	LabelLink    string
	LabelSession string
	// Overdue is appended to a due date already past.
	Overdue string
	// NoteLines summarises a note by its length. Takes the line count.
	NoteLines string
	// AddLink / AddSession are the two rows that open something rather than showing a value.
	AddLink    string
	AddSession string
	// HerdrSuffix marks the AddSession row as unusable while herdr is unreachable.
	HerdrSuffix string
	// LinkNotFound / SessionNotFound are raised when the row's target vanished under the cursor.
	LinkNotFound    string
	SessionNotFound string
	// ConfirmRemoveLink takes the task id and the URL; ConfirmDetachSession the id and short id.
	ConfirmRemoveLink    string
	ConfirmDetachSession string
	// OnlyLinkOrSession explains what delete can act on.
	OnlyLinkOrSession string
	// StaleMark labels a cached value past its TTL. Takes the age.
	StaleMark string
	// The Prompt fields are the label above the text field each row opens.
	PromptTitle    string
	PromptDue      string
	PromptLinkNote string
	PromptAddLink  string
}

// Add is the task-creation modal.
type Add struct {
	// Help is the modal's key list. Takes the vertical arrows, the horizontal arrows and the key
	// that inserts a line break on this terminal.
	Help string
	// Title is the modal's title.
	Title string
	// The Label fields name the rows.
	LabelTitle  string
	LabelStatus string
	LabelDue    string
	LabelNote   string
	LabelLink   string
	// NoColumns is the refusal when there is nowhere to create a task.
	NoColumns string
	// NeedTitle rejects a submission with no title.
	NeedTitle string
	// ChangeHint sits beside the status row. Takes the horizontal arrow glyphs.
	ChangeHint string
	// CreateHint appears once a multi-line title means several tasks. Takes the count.
	CreateHint string
}

// Start is the session launch modal.
type Start struct {
	// Help is the modal's key list. Takes the vertical arrows and the line-break key.
	Help string
	// Title is the modal's title. Takes the task id and title.
	Title string
	// NoLauncher is the refusal when the board has no way to launch anything.
	NoLauncher string
	// HerdrDown is the refusal while herdr is unreachable.
	HerdrDown string
	// ProbingCwd is shown while the recoverable-cwd probe is still out.
	ProbingCwd string
	// Copied acknowledges an OSC 52 copy, which cannot be confirmed.
	Copied string
	// NeedCwd rejects a submission with no working directory.
	NeedCwd string
	// StartFailed reports a launch that could not be handed off. Takes the id and the error.
	StartFailed string
	// LabelCwd / LabelCustom / LabelPrompt name the modal's three regions.
	LabelCwd    string
	LabelCustom string
	LabelPrompt string
}

// Jump is the g flow: moving to a live pane, or resuming one that is gone.
type Jump struct {
	// HerdrDown is the refusal while herdr is unreachable.
	HerdrDown string
	// ResumeManually tells the user the exact command to run. Takes the cwd and the session id.
	ResumeManually string
	// ResumeManuallyAgent is the same for an agent taskherd cannot resume. Takes the cwd and the
	// agent name.
	ResumeManuallyAgent string
	// PaneGoneUnsupported reports a dead pane of an agent with no resume path. Takes the agent
	// name and the cwd.
	PaneGoneUnsupported string
	// ConfirmResume asks before resuming into a new pane. Takes the cwd.
	ConfirmResume string
	// FocusFailed reports a focus call herdr refused. Takes the pane id and the error.
	FocusFailed string
	// NoLauncher is the refusal when the board has no way to resume anything.
	NoLauncher string
	// ResumeFailed reports a resume that could not be handed off. Takes the id and the error.
	ResumeFailed string
	// TargetTitle is the session picker's title. Takes the task id.
	TargetTitle string
	// TargetHelp is the session picker's key line. Takes the vertical arrow glyphs.
	TargetHelp string
}

// Select is the two selectors: Tab's status picker and the detail modal's agent picker.
type Select struct {
	// NoTargetColumn is the refusal when there is nowhere to move a task.
	NoTargetColumn string
	// StatusTitle is the status picker's title. Takes the task id.
	StatusTitle string
	// StatusHelp is the status picker's key line. Takes the horizontal arrow glyphs.
	StatusHelp string
	// AttachHerdrDown is the refusal to open the agent picker while herdr is unreachable.
	AttachHerdrDown string
	// HerdrError reports a failed snapshot. Takes the error.
	HerdrError string
	// NoAgentsFound is the error line when herdr answered with no agents.
	NoAgentsFound string
	// NoSessionID reports an agent whose session id herdr never saw. Takes the pane id.
	NoSessionID string
	// Querying is shown while the snapshot is out.
	Querying string
	// NoAgents is the empty-list placeholder inside the picker.
	NoAgents string
	// NotDetected stands in for a missing session id in the list.
	NotDetected string
	// AttachTitle is the agent picker's title. Takes the task id.
	AttachTitle string
	// AttachHelp is the agent picker's key line. Takes the vertical arrow glyphs.
	AttachHelp string
}

// Picker is the standalone pane-to-task popup (`taskherd picker`).
type Picker struct {
	// FilterPrompt is the prefix of the filter field.
	FilterPrompt string
	// Attached acknowledges the link. Takes the task id.
	Attached string
	// HerdrError reports a failed snapshot. Takes the error.
	HerdrError string
	// NoAgent reports a pane herdr sees no agent in. Takes the pane id.
	NoAgent string
	// NoSessionID reports an agent whose session id herdr never saw. Takes the pane id.
	NoSessionID string
	// Title is the popup's heading. Takes the pane id.
	Title string
	// Loading / NoMatch / Attaching are the list's three transient states.
	Loading   string
	NoMatch   string
	Attaching string
	// Help is the popup's key line. Takes the vertical arrow glyphs.
	Help string
}
