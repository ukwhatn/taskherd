package i18n

// Problem is a refusal the user can act on, in the two parts the CLI prints separately: what is
// wrong, and what to do about it. Either half may be a format string; the field comments say which.
type Problem struct {
	Msg  string
	Hint string
}

// CLI is the command line's own text: cobra's help, flag descriptions, the lines a command prints
// when it succeeds, and the refusals it prints when it will not run.
type CLI struct {
	Root    CLIRoot
	Task    CLITask
	Link    CLILink
	Note    CLINote
	Session CLISession
	Jump    CLIJump
	Start   CLIStart
	Refresh CLIRefresh
	Config  CLIConfig
	Board   CLIBoard
	Plugin  CLIPlugin
	Picker  CLIPicker
	Version CLIVersion
}

// CLIRoot is the root command plus everything shared between subcommands.
type CLIRoot struct {
	Short           string
	FlagJSON        string
	FlagNotifyError string
	// ErrorPrefix and HintPrefix head the two lines a failure prints on stderr. Each takes the
	// text that follows it.
	ErrorPrefix string
	HintPrefix  string
	// NotifyTitle is the herdr notification a detached launch raises. Takes the operation's label.
	NotifyTitle string
	// NotifyBody is that notification's body when the failure carries advice. Takes the message
	// and the advice.
	NotifyBody string
	// ConfirmPrompt wraps a question with the answer key. Takes the question.
	ConfirmPrompt string
	// Cancelled acknowledges a confirmation answered with no.
	Cancelled string
	// BadTaskID rejects an argument that is not a task id. Msg takes the argument.
	BadTaskID Problem
	// UnknownColumn rejects a status that no column defines. Msg takes the status, Hint the list
	// of valid ids.
	UnknownColumn Problem
	// BadDueHint is appended to the date parser's own message.
	BadDueHint string
	// BadURL rejects a link that is not a URL. Msg takes the argument.
	BadURL Problem
	// TokenFileUnreadable and TokenFileEmpty explain why a Jira token file yielded nothing. Each
	// takes the path; the first also takes the error.
	TokenFileUnreadable string
	TokenFileEmpty      string
}

// CLITask covers add / list / show / edit / move / done / rm, which all act on one task.
type CLITask struct {
	AddShort string
	// Created takes the id, the status and the title.
	Created        string
	AddFlagStatus  string
	AddFlagDue     string
	AddFlagNote    string
	AddFlagLink    string
	AddFlagSession string
	AddFlagCwd     string

	ListShort      string
	ListEmpty      string
	ListFlagStatus string
	ListFlagAll    string

	ShowShort string

	EditShort string
	// EditNothing refuses an update that names no field.
	EditNothing Problem
	// Edited takes the id, the status and the title.
	Edited         string
	EditFlagTitle  string
	EditFlagDue    string
	EditFlagStatus string

	MoveShort string
	DoneShort string
	// Moved takes the id, the status and the title.
	Moved string

	RmShort string
	// RmNeedsYes refuses a non-interactive delete with no --yes.
	RmNeedsYes Problem
	// RmConfirm asks before deleting. Takes the id and the title.
	RmConfirm string
	// Removed takes the id and the title.
	Removed   string
	RmFlagYes string

	// FetchFailed heads a link whose state has never been read. Takes the error.
	FetchFailed string
	// NotFetched stands in for a link nothing has fetched yet.
	NotFetched string
	// Live and LiveStale render a fetched link. Each takes the description and the age.
	Live      string
	LiveStale string
	// LastFetchFailed is appended when the value shown is a stale success. Takes the error.
	LastFetchFailed string
	// UnknownColumnLabel names a status that no column defines.
	UnknownColumnLabel string
	// EmptyList stands in for a section of the detail output with nothing in it.
	EmptyList string
}

// CLILink covers link / unlink.
type CLILink struct {
	LinkShort string
	FlagNote  string
	// Linked takes the id, the link kind and the URL.
	Linked      string
	UnlinkShort string
	// Unlinked takes the id and the URL.
	Unlinked string
}

// CLINote covers note.
type CLINote struct {
	Short string
	// BothSetAndAppend refuses --set together with --append.
	BothSetAndAppend Problem
	// Updated takes the id.
	Updated    string
	FlagSet    string
	FlagAppend string
	// NothingToSet refuses a non-interactive edit that named no text.
	NothingToSet Problem
	// NoEditor refuses an interactive edit with no editor configured.
	NoEditor Problem
	// EditorFailed takes the editor command and the error.
	EditorFailed string
}

// CLISession covers session link / unlink and the note the CLI prints when herdr is unreachable.
type CLISession struct {
	Short     string
	LinkShort string
	// AmbiguousSpec refuses more than one way of naming the session.
	AmbiguousSpec Problem
	// Linked takes the id, the agent name and the session id.
	Linked        string
	FlagCurrent   string
	FlagSessionID string
	FlagPane      string
	FlagCwd       string
	FlagLabel     string

	UnlinkShort string
	// Unlinked takes the id and the session id.
	Unlinked string

	// NoCurrentPane refuses --current outside a herdr pane.
	NoCurrentPane Problem
	// NoAgentInPane refuses a pane herdr sees no agent in. Msg takes the pane id.
	NoAgentInPane Problem
	// NoSessionID refuses an agent whose session id herdr never saw. Msg takes the pane id.
	NoSessionID Problem
	// Unresolvable refuses a session id herdr cannot place. Msg takes the session id.
	Unresolvable Problem
	// ReportFailed notes a failed pane stamp, which does not fail the command. Takes the pane id
	// and the error.
	ReportFailed string
	// HerdrDownNote explains why session states are missing from the output. Takes the error.
	HerdrDownNote string
}

// CLIJump covers jump, which moves to a live pane or resumes one that is gone.
type CLIJump struct {
	Short       string
	FlagSession string
	FlagYes     string
	// NoSession refuses a task with nothing linked. Msg and Hint each take the id.
	NoSession Problem
	// NotLinked refuses a session the task does not have. Msg takes the id and the session id,
	// Hint the id.
	NotLinked Problem
	// TooMany refuses an ambiguous jump in --json mode. Msg takes the id and the count, Hint the
	// list of session ids.
	TooMany Problem
	// ChooseHeader heads the interactive list. Takes the id.
	ChooseHeader string
	// ChoosePrompt is the question asked under it.
	ChoosePrompt string
	// BadChoice rejects an answer that is not one of the numbers. Msg takes the answer, Hint the
	// count.
	BadChoice Problem
	// UnsupportedResume refuses to resume an agent taskherd cannot restart. Msg takes the agent
	// name, Hint the cwd.
	UnsupportedResume Problem
	// NeedsYes refuses a non-interactive resume with no --yes.
	NeedsYes Problem
	// ConfirmResume asks before resuming. Takes the cwd.
	ConfirmResume string
	// HerdrDownClaude and HerdrDownOther report an unreachable herdr with the manual way out.
	// Msg takes the error; Hint takes the cwd and the session id, or the cwd and the agent name.
	HerdrDownClaude Problem
	HerdrDownOther  Problem
	// Moved and Resumed take the id and the pane id.
	Moved   string
	Resumed string
	// WaitingInput warns that the resumed pane stopped on a prompt.
	WaitingInput string
}

// CLIStart covers start, which creates a pane, starts an agent and links the session it reports.
type CLIStart struct {
	Short            string
	FlagCwd          string
	FlagPrompt       string
	FlagNew          string
	FlagNoFocus      string
	CandidatesHeader string
	ChoosePrompt     string
	// BlankCwd refuses a --cwd of only whitespace.
	BlankCwd Problem
	// NoCandidate refuses a launch with nowhere to run.
	NoCandidate Problem
	// ManyCandidates refuses an ambiguous launch in --json mode. Hint takes the candidate list.
	ManyCandidates Problem
	// BadChoice rejects an answer that is neither a number nor a path. Msg takes the answer, Hint
	// the count.
	BadChoice Problem
	// EmptyCwd refuses an empty answer.
	EmptyCwd Problem
	// ReusableBusy refuses to reuse a pane that is not idle. Msg takes the id and the pane id,
	// Hint the pane id.
	ReusableBusy Problem
	// AlreadyLinked reports that the recovered pane's session is already on the task. Msg and Hint
	// each take the id.
	AlreadyLinked Problem
	// OtherCwd reports a recovered pane running somewhere else. Msg takes the id, the pane's cwd
	// and the pane id; Hint takes the pane id, the task id and the requested cwd.
	OtherCwd Problem
	// PartialLabel names the failure shape where the result is already on stdout.
	PartialLabel string
	// StartFailed and the entries below it are the ways a launch stops partway. Each hint takes
	// the pane id.
	StartFailed       string
	WaitingInput      string
	WaitingInputHint  string
	CheckPaneHint     string
	TrustPrompt       string
	NoSessionReported string
	LinkManuallyHint  string
	LinkFailedHint    string
	PromptFailedHint  string
	DoneWithPrompt    string
	DoneWithoutPrompt string
	// LaunchLabel and ResumeLabel name the operation in a detached launch's notification. Each
	// takes the task id.
	LaunchLabel string
	ResumeLabel string
}

// CLIRefresh covers refresh.
type CLIRefresh struct {
	Short string
	// NoTarget refuses a refresh that names nothing.
	NoTarget Problem
	// BothIDAndAll refuses an id together with --all.
	BothIDAndAll Problem
	FlagAll      string
	// Refreshed takes the count; FailedSuffix takes the failure count.
	Refreshed     string
	FailedSuffix  string
	GitHubLimited string
	JiraLimited   string
}

// CLIVersion is the version command.
type CLIVersion struct {
	Short string
}

// CLIConfig covers config path / init.
type CLIConfig struct {
	Short     string
	PathShort string
	InitShort string
	// Exists refuses to overwrite. Msg takes the path.
	Exists Problem
	// Created takes the path.
	Created string
}

// CLIBoard covers board.
type CLIBoard struct {
	Short string
	// NoTTY refuses the TUI in --json mode.
	NoTTY Problem
	// NoOpenColumn refuses a board with nothing to draw.
	NoOpenColumn Problem
}

// CLIPlugin covers the hidden commands herdr's plugin actions call.
type CLIPlugin struct {
	Short          string
	OpenBoardShort string
	LinkPaneShort  string
	// NoCallerPane refuses an action invoked outside a pane.
	NoCallerPane Problem
}

// CLIPicker covers the hidden picker entrypoint.
type CLIPicker struct {
	Short string
	// NoTargetPane refuses a picker with no pane to attach. Msg takes the variable's name.
	NoTargetPane Problem
}
