package model

import (
	"fmt"
	"strings"
	"time"

	"github.com/ukwhatn/taskherd/internal/i18n"
)

// Violation is one validation failure. Path locates it in the document (e.g. tasks[0].due), Code
// says which rule was broken, and Args carries the values that rule's message names.
//
// The code is kept instead of the finished sentence because a violation can be raised while writing
// and shown much later — by another command, in another language.
type Violation struct {
	Path string
	Code i18n.ViolationCode
	Args []any
}

// Text renders the violation in the catalog's language.
func (v Violation) Text(t *i18n.Catalog) string {
	return i18n.OrDefault(t).ViolationText(v.Code, v.Args...)
}

// ValidationError collects every violation, so a refused write can report all of them.
type ValidationError struct {
	Subject    string
	Violations []Violation
}

func (e *ValidationError) Error() string {
	text, _ := e.Localize(i18n.For(i18n.LangEN))
	return text
}

// Localize renders the whole list in the catalog's language.
func (e *ValidationError) Localize(t *i18n.Catalog) (string, string) {
	t = i18n.OrDefault(t)
	subject := e.Subject
	if subject == "" {
		subject = t.Err.Data.InvalidSubject
	}
	lines := make([]string, 0, len(e.Violations)+1)
	lines = append(lines, fmt.Sprintf(t.Err.Data.Invalid, subject, len(e.Violations)))
	for _, v := range e.Violations {
		lines = append(lines, fmt.Sprintf(t.Err.Data.Violation, v.Path, v.Text(t)))
	}
	return strings.Join(lines, "\n"), ""
}

// VersionMismatchError reports a version this binary does not handle.
type VersionMismatchError struct {
	Got  int
	Want int
}

func (e *VersionMismatchError) Error() string {
	text, _ := e.Localize(i18n.For(i18n.LangEN))
	return text
}

// Localize renders the mismatch in the catalog's language.
func (e *VersionMismatchError) Localize(t *i18n.Catalog) (string, string) {
	return fmt.Sprintf(i18n.OrDefault(t).Err.Data.VersionMismatch, e.Got, e.Want), ""
}

// addViolation is the shape every check uses to record a failure.
type addViolation func(path string, code i18n.ViolationCode, args ...any)

// Validate applies the read-time rules. Unknown status values pass; they render as an (unknown) column.
func Validate(f *File) error {
	if f.Version != CurrentVersion {
		return &VersionMismatchError{Got: f.Version, Want: CurrentVersion}
	}

	var violations []Violation
	add := func(path string, code i18n.ViolationCode, args ...any) {
		violations = append(violations, Violation{Path: path, Code: code, Args: args})
	}

	maxID := 0
	for _, task := range f.Tasks {
		if task.ID > maxID {
			maxID = task.ID
		}
	}
	if f.NextID <= maxID || f.NextID < 1 {
		add("next_id", i18n.ViolationNextIDTooSmall, maxID, f.NextID)
	}

	seen := make(map[int]int, len(f.Tasks))
	for i, task := range f.Tasks {
		switch prev, dup := seen[task.ID]; {
		case task.ID < 1:
			add(fmt.Sprintf("tasks[%d].id", i), i18n.ViolationTaskIDNotPositive, task.ID)
		case dup:
			add(fmt.Sprintf("tasks[%d].id", i), i18n.ViolationTaskIDDuplicate, task.ID, prev)
		default:
			seen[task.ID] = i
		}

		checkTimestamp(add, fmt.Sprintf("tasks[%d].created_at", i), task.CreatedAt)
		checkTimestamp(add, fmt.Sprintf("tasks[%d].updated_at", i), task.UpdatedAt)
		if task.Due != nil {
			if _, err := time.Parse(dateLayout, string(*task.Due)); err != nil {
				add(fmt.Sprintf("tasks[%d].due", i), i18n.ViolationTaskDueFormat, string(*task.Due))
			}
		}
		for j, link := range task.Links {
			checkTimestamp(add, fmt.Sprintf("tasks[%d].links[%d].added_at", i, j), link.AddedAt)
		}
		for j, session := range task.Sessions {
			checkTimestamp(add, fmt.Sprintf("tasks[%d].sessions[%d].linked_at", i, j), session.LinkedAt)
		}
	}

	if len(violations) > 0 {
		return &ValidationError{Subject: "tasks.json", Violations: violations}
	}
	return nil
}

func checkTimestamp(add addViolation, path string, ts Timestamp) {
	if _, err := ts.Time(); err != nil {
		add(path, i18n.ViolationTimestampFormat, string(ts))
	}
}
