package i18n

import (
	"errors"
	"fmt"
)

// Localizer is an error that can say itself in a given language.
//
// The interface lives here but is implemented next to each error type, which keeps the dependency
// pointing the right way: model, store, fetch and herdrc all know about i18n, and i18n knows about
// none of them. An error's own Error() stays in English — it is what lands in a log or an issue,
// where one searchable wording beats a familiar one.
type Localizer interface {
	// Localize returns the message and, when there is one, what to do about it.
	Localize(*Catalog) (text, hint string)
}

// hinter is the pre-existing way an error carries recovery advice. Kept so that an error which has
// a hint but no translation still surfaces it.
type hinter interface {
	Hint() string
}

// Message renders err for a human: its text in the catalog's language, and any hint.
//
// An error nothing recognises comes back as its own Error(), which for the diagnostics that stay
// in English (a failed rename, an unparseable body) is exactly what should be shown.
func Message(t *Catalog, err error) (text, hint string) {
	if err == nil {
		return "", ""
	}
	t = OrDefault(t)

	var local Localizer
	if errors.As(err, &local) {
		return local.Localize(t)
	}

	var h hinter
	if errors.As(err, &h) {
		return err.Error(), h.Hint()
	}
	return err.Error(), ""
}

// Errorf returns an error whose text is read from the catalog when it is displayed, not when it is
// raised. It is for the one-off failures that need no type of their own: pick names the entry and
// args fills its verbs.
func Errorf(pick func(*Catalog) string, args ...any) error {
	return &deferred{text: pick, args: args}
}

// Problemf is Errorf for an entry that also carries advice. The hint takes no arguments; an entry
// whose advice depends on the failure belongs to a type that holds those values.
func Problemf(pick func(*Catalog) Problem, args ...any) error {
	return &deferred{
		text: func(t *Catalog) string { return pick(t).Msg },
		hint: func(t *Catalog) string { return pick(t).Hint },
		args: args,
	}
}

type deferred struct {
	text func(*Catalog) string
	hint func(*Catalog) string
	args []any
}

func (e *deferred) Error() string {
	text, _ := e.Localize(For(LangEN))
	return text
}

func (e *deferred) Localize(t *Catalog) (string, string) {
	t = OrDefault(t)
	hint := ""
	if e.hint != nil {
		hint = e.hint(t)
	}
	return fmt.Sprintf(e.text(t), e.args...), hint
}

// Err is the text of the errors taskherd raises as types rather than as prose: the ones a user can
// act on. Each entry's comment names the values its format string takes.
type Err struct {
	Task ErrTask
	Data ErrData
	Live ErrLive
	Herd ErrHerdr
}

// ErrTask is the vocabulary of an operation on one task, all of which are sentinels.
type ErrTask struct {
	TaskNotFound    string
	LinkNotFound    string
	LinkExists      string
	EmptyTitle      string
	EmptyStatus     string
	SessionNotFound string
	SessionExists   string
	EmptySessionID  string
	EmptySessionCwd string
	EmptyAgent      string
	// BadDate rejects a date that is not YYYY-MM-DD. Takes the value.
	BadDate string
}

// ErrData is what can be wrong with the files taskherd reads: tasks.json and config.toml.
type ErrData struct {
	// Invalid heads a list of violations. Takes the subject and the count.
	Invalid string
	// InvalidSubject is what stands in when the subject is unnamed.
	InvalidSubject string
	// Violation is one line of that list. Takes the path and the message.
	Violation string
	// VersionMismatch reports a tasks.json this binary cannot read. Takes the file's version and
	// the supported one.
	VersionMismatch string

	// Corrupt reports an unreadable tasks.json. Text takes the path and the cause, Hint the
	// backup's path.
	Corrupt Problem
	// Version reports a tasks.json written by another version. Text takes the path and the cause.
	Version Problem
	// NoHome reports that no home directory could be found to place the config and data under.
	NoHome Problem
	// Lock reports a write lock that never came free. Text takes the path, the timeout and the
	// cause.
	Lock Problem

	// The Violation entries below are the checks Columns.Validate and Config.Validate run. Each
	// takes whatever its code carries.
	ColumnsEmpty      string
	ColumnIDEmpty     string
	ColumnIDDuplicate string
	ColumnLabelEmpty  string
	ColumnKindInvalid string
	NextIDTooSmall    string
	TaskIDNotPositive string
	TaskIDDuplicate   string
	TaskDueFormat     string
	TimestampFormat   string
	IntervalNegative  string
	CacheTTLNegative  string
	IconModeInvalid   string
	LanguageInvalid   string
	AccountIncomplete string
	AccountKeyFormat  string
}

// ErrLive is what can go wrong reading a link's live state.
type ErrLive struct {
	// GHNotFound reports that gh is not on PATH.
	GHNotFound Problem
	// GHRateLimited reports a rate limit. Text takes gh's stderr.
	GHRateLimited Problem
	// GHFailed reports any other gh failure. Text is only the fallback for an empty stderr.
	GHFailed Problem
	// GHAccountUsed names the account a fetch ran as, in the three shapes it can take: the active
	// account with no matching config, the active account after a config entry failed to resolve
	// (takes the key and the account), and a named account (takes the account and the key).
	GHAccountActive       string
	GHAccountActiveFailed string
	GHAccountNamed        string
	// GHAccountOwnerHint is the way out of a repository the authenticated account cannot see.
	GHAccountOwnerHint string
	// GHTokenFailed and GHTokenEmpty report a [github.accounts] entry gh would not resolve. Each
	// takes the host and the account; the first also takes gh's stderr.
	GHTokenFailed string
	GHTokenEmpty  string

	// JiraAuth reports an invalid or expired API token.
	JiraAuth Problem
	// JiraRateLimited reports a 429.
	JiraRateLimited Problem
	// JiraStatus reports any other non-2xx. Text takes the status code and the body.
	JiraStatus Problem
	// JiraNotConfigured reports missing Jira settings. Text is used bare, or with the caller's
	// reason appended through JiraNotConfiguredWhy.
	JiraNotConfigured    Problem
	JiraNotConfiguredWhy string
	// NoJiraKey reports a URL no Jira key could be read from. Takes the URL.
	NoJiraKey string
}

// ErrHerdr is what can go wrong talking to herdr.
type ErrHerdr struct {
	// API reports an error herdr itself returned. Text takes the code, or the code and a message.
	APICode    string
	APIMessage string
	// Unavailable reports a socket that would not answer. Text takes the socket path and the cause.
	Unavailable Problem
}

// ViolationCode names one validation rule.
//
// The code travels with the violation instead of the prose does, so a file validated while writing
// can be reported later in whatever language is reading. A code is part of this package because the
// text is: adding a rule means adding both, and keeping them together is what makes that obvious.
type ViolationCode string

const (
	ViolationColumnsEmpty      ViolationCode = "columns.empty"
	ViolationColumnIDEmpty     ViolationCode = "column.id.empty"
	ViolationColumnIDDuplicate ViolationCode = "column.id.duplicate"
	ViolationColumnLabelEmpty  ViolationCode = "column.label.empty"
	ViolationColumnKindInvalid ViolationCode = "column.kind.invalid"
	ViolationNextIDTooSmall    ViolationCode = "next_id.too_small"
	ViolationTaskIDNotPositive ViolationCode = "task.id.not_positive"
	ViolationTaskIDDuplicate   ViolationCode = "task.id.duplicate"
	ViolationTaskDueFormat     ViolationCode = "task.due.format"
	ViolationTimestampFormat   ViolationCode = "timestamp.format"
	ViolationIntervalNegative  ViolationCode = "interval.negative"
	ViolationCacheTTLNegative  ViolationCode = "cache_ttl.negative"
	ViolationIconModeInvalid   ViolationCode = "icons.invalid"
	ViolationLanguageInvalid   ViolationCode = "language.invalid"
	ViolationAccountIncomplete ViolationCode = "github.accounts.incomplete"
	ViolationAccountKeyFormat  ViolationCode = "github.accounts.key_format"
)

// ViolationText renders one violation. An unknown code comes back as the code itself: that is a
// bug in the caller, and a visible identifier is more use than a blank line.
func (t *Catalog) ViolationText(code ViolationCode, args ...any) string {
	t = OrDefault(t)
	var format string
	switch code {
	case ViolationColumnsEmpty:
		format = t.Err.Data.ColumnsEmpty
	case ViolationColumnIDEmpty:
		format = t.Err.Data.ColumnIDEmpty
	case ViolationColumnIDDuplicate:
		format = t.Err.Data.ColumnIDDuplicate
	case ViolationColumnLabelEmpty:
		format = t.Err.Data.ColumnLabelEmpty
	case ViolationColumnKindInvalid:
		format = t.Err.Data.ColumnKindInvalid
	case ViolationNextIDTooSmall:
		format = t.Err.Data.NextIDTooSmall
	case ViolationTaskIDNotPositive:
		format = t.Err.Data.TaskIDNotPositive
	case ViolationTaskIDDuplicate:
		format = t.Err.Data.TaskIDDuplicate
	case ViolationTaskDueFormat:
		format = t.Err.Data.TaskDueFormat
	case ViolationTimestampFormat:
		format = t.Err.Data.TimestampFormat
	case ViolationIntervalNegative:
		format = t.Err.Data.IntervalNegative
	case ViolationCacheTTLNegative:
		format = t.Err.Data.CacheTTLNegative
	case ViolationIconModeInvalid:
		format = t.Err.Data.IconModeInvalid
	case ViolationLanguageInvalid:
		format = t.Err.Data.LanguageInvalid
	case ViolationAccountIncomplete:
		format = t.Err.Data.AccountIncomplete
	case ViolationAccountKeyFormat:
		format = t.Err.Data.AccountKeyFormat
	default:
		return string(code)
	}
	return fmt.Sprintf(format, args...)
}
