package model

import (
	"fmt"
	"strings"
	"time"
)

// Violation is one validation failure. Path locates it in the document (e.g. tasks[0].due).
type Violation struct {
	Path    string
	Message string
}

// ValidationError collects every violation, so a refused write can report all of them.
type ValidationError struct {
	Subject    string
	Violations []Violation
}

func (e *ValidationError) Error() string {
	subject := e.Subject
	if subject == "" {
		subject = "入力"
	}
	lines := make([]string, 0, len(e.Violations)+1)
	lines = append(lines, fmt.Sprintf("%s の検証に失敗した（%d 件）:", subject, len(e.Violations)))
	for _, v := range e.Violations {
		lines = append(lines, fmt.Sprintf("  %s: %s", v.Path, v.Message))
	}
	return strings.Join(lines, "\n")
}

// VersionMismatchError reports a version this binary does not handle.
type VersionMismatchError struct {
	Got  int
	Want int
}

func (e *VersionMismatchError) Error() string {
	return fmt.Sprintf("tasks.json の version が %d（このバイナリの対応は %d）", e.Got, e.Want)
}

// Validate applies the read-time rules. Unknown status values pass; they render as an (unknown) column.
func Validate(f *File) error {
	if f.Version != CurrentVersion {
		return &VersionMismatchError{Got: f.Version, Want: CurrentVersion}
	}

	var violations []Violation
	add := func(path, format string, args ...any) {
		violations = append(violations, Violation{Path: path, Message: fmt.Sprintf(format, args...)})
	}

	maxID := 0
	for _, task := range f.Tasks {
		if task.ID > maxID {
			maxID = task.ID
		}
	}
	if f.NextID <= maxID || f.NextID < 1 {
		add("next_id", "next_id は max(id)=%d より大きい正の整数でなければならない（実際: %d）", maxID, f.NextID)
	}

	seen := make(map[int]int, len(f.Tasks))
	for i, task := range f.Tasks {
		switch prev, dup := seen[task.ID]; {
		case task.ID < 1:
			add(fmt.Sprintf("tasks[%d].id", i), "id は正の整数でなければならない（実際: %d）", task.ID)
		case dup:
			add(fmt.Sprintf("tasks[%d].id", i), "id %d が tasks[%d] と重複している", task.ID, prev)
		default:
			seen[task.ID] = i
		}

		checkTimestamp(add, fmt.Sprintf("tasks[%d].created_at", i), task.CreatedAt)
		checkTimestamp(add, fmt.Sprintf("tasks[%d].updated_at", i), task.UpdatedAt)
		if task.Due != nil {
			if _, err := time.Parse(dateLayout, string(*task.Due)); err != nil {
				add(fmt.Sprintf("tasks[%d].due", i), "YYYY-MM-DD 形式でなければならない（実際: %q）", string(*task.Due))
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

func checkTimestamp(add func(path, format string, args ...any), path string, ts Timestamp) {
	if _, err := ts.Time(); err != nil {
		add(path, "RFC 3339 形式でなければならない（実際: %q）", string(ts))
	}
}
