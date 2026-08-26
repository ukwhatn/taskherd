// Package model holds the taskherd task data model with its validation and mutations.
// Persistence (file IO, locking) belongs to the store package.
package model

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ukwhatn/taskherd/internal/i18n"
)

// CurrentVersion is the tasks.json version this binary can read and write.
const CurrentVersion = 1

const dateLayout = "2006-01-02"

var (
	ErrTaskNotFound    = &sentinel{func(t *i18n.ErrTask) string { return t.TaskNotFound }}
	ErrLinkNotFound    = &sentinel{func(t *i18n.ErrTask) string { return t.LinkNotFound }}
	ErrLinkExists      = &sentinel{func(t *i18n.ErrTask) string { return t.LinkExists }}
	ErrEmptyTitle      = &sentinel{func(t *i18n.ErrTask) string { return t.EmptyTitle }}
	ErrEmptyStatus     = &sentinel{func(t *i18n.ErrTask) string { return t.EmptyStatus }}
	ErrSessionNotFound = &sentinel{func(t *i18n.ErrTask) string { return t.SessionNotFound }}
	ErrSessionExists   = &sentinel{func(t *i18n.ErrTask) string { return t.SessionExists }}
	ErrEmptySessionID  = &sentinel{func(t *i18n.ErrTask) string { return t.EmptySessionID }}
	ErrEmptySessionCwd = &sentinel{func(t *i18n.ErrTask) string { return t.EmptySessionCwd }}
	ErrEmptyAgent      = &sentinel{func(t *i18n.ErrTask) string { return t.EmptyAgent }}
)

// sentinel is a fixed error that reads its text out of the catalog.
//
// The pointer is the identity errors.Is compares, exactly as with errors.New, so callers are
// unaffected. Error() renders the English entry: an error's own text is what lands in a log or a
// bug report, where one searchable wording beats a familiar one.
type sentinel struct {
	text func(*i18n.ErrTask) string
}

func (e *sentinel) Error() string { return e.text(&i18n.For(i18n.LangEN).Err.Task) }

func (e *sentinel) Localize(t *i18n.Catalog) (string, string) {
	return e.text(&i18n.OrDefault(t).Err.Task), ""
}

// subjectError says which task, link or session an error is about.
//
// It replaces wrapping with fmt.Errorf, which would hide the sentinel's translation behind a prefix
// fixed in one language. The subject itself is a value — an id, a URL — and needs no translating.
type subjectError struct {
	subject string
	err     error
}

func withSubject(subject string, err error) error {
	return &subjectError{subject: subject, err: err}
}

func (e *subjectError) Error() string { return e.subject + ": " + e.err.Error() }

func (e *subjectError) Unwrap() error { return e.err }

func (e *subjectError) Localize(t *i18n.Catalog) (string, string) {
	text, hint := i18n.Message(t, e.err)
	return e.subject + ": " + text, hint
}

// Timestamp is a point in time in RFC 3339 notation.
type Timestamp string

// Date is a calendar date in YYYY-MM-DD notation.
type Date string

// LinkKind is the kind of an external link, derived from its URL.
type LinkKind string

const (
	LinkKindGitHubPR    LinkKind = "github_pr"
	LinkKindGitHubIssue LinkKind = "github_issue"
	LinkKindJira        LinkKind = "jira"
	LinkKindOther       LinkKind = "other"
)

// File is the whole tasks.json document.
type File struct {
	Version int    `json:"version"`
	NextID  int    `json:"next_id"`
	Tasks   []Task `json:"tasks"`
}

// Task is a single task.
type Task struct {
	ID        int          `json:"id"`
	Title     string       `json:"title"`
	Status    string       `json:"status"`
	Due       *Date        `json:"due"`
	Note      string       `json:"note"`
	Sessions  []SessionRef `json:"sessions"`
	Links     []Link       `json:"links"`
	CreatedAt Timestamp    `json:"created_at"`
	UpdatedAt Timestamp    `json:"updated_at"`
}

// SessionRef is an agent session linked to a task. pane_id is volatile, so it is not stored.
type SessionRef struct {
	Agent     string    `json:"agent"`
	SessionID string    `json:"session_id"`
	Cwd       string    `json:"cwd"`
	Label     string    `json:"label"`
	LinkedAt  Timestamp `json:"linked_at"`
}

// Link is an external link attached to a task.
type Link struct {
	URL     string    `json:"url"`
	Kind    LinkKind  `json:"kind"`
	Note    string    `json:"note"`
	AddedAt Timestamp `json:"added_at"`
}

// TaskInput carries the attributes of a task to create.
type TaskInput struct {
	Title  string
	Status string
	Due    *Date
	Note   string
}

// NewFile returns the empty equivalent of tasks.json.
func NewFile() *File {
	return &File{Version: CurrentVersion, NextID: 1, Tasks: []Task{}}
}

// NewTimestamp formats t as RFC 3339 with its local offset.
func NewTimestamp(t time.Time) Timestamp {
	return Timestamp(t.Format(time.RFC3339))
}

// ParseDate validates YYYY-MM-DD notation and returns it as a Date.
func ParseDate(s string) (Date, error) {
	if _, err := time.Parse(dateLayout, s); err != nil {
		return "", i18n.Errorf(func(t *i18n.Catalog) string { return t.Err.Task.BadDate }, s)
	}
	return Date(s), nil
}

// Time converts the Timestamp to time.Time.
func (t Timestamp) Time() (time.Time, error) {
	return time.Parse(time.RFC3339, string(t))
}

// ParseFile parses tasks.json bytes and applies the validation rules.
func ParseFile(data []byte) (*File, error) {
	var f File
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("cannot parse tasks.json: %w", err)
	}
	if err := Validate(&f); err != nil {
		return nil, err
	}
	f.Normalize()
	return &f, nil
}

// MarshalFile renders the file as tasks.json bytes.
func MarshalFile(f *File) ([]byte, error) {
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("cannot build tasks.json: %w", err)
	}
	return append(data, '\n'), nil
}

// Normalize replaces omitted arrays with empty slices so JSON output shows [] instead of null.
func (f *File) Normalize() {
	for i := range f.Tasks {
		if f.Tasks[i].Sessions == nil {
			f.Tasks[i].Sessions = []SessionRef{}
		}
		if f.Tasks[i].Links == nil {
			f.Tasks[i].Links = []Link{}
		}
	}
	if f.Tasks == nil {
		f.Tasks = []Task{}
	}
}

// Task returns a pointer to the task with the given id; mutations through it reach the File.
func (f *File) Task(id int) (*Task, error) {
	for i := range f.Tasks {
		if f.Tasks[i].ID == id {
			return &f.Tasks[i], nil
		}
	}
	return nil, withSubject(fmt.Sprintf("#%d", id), ErrTaskNotFound)
}

// AddTask appends a task using next_id as its id.
func (f *File) AddTask(in TaskInput, now time.Time) (*Task, error) {
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return nil, ErrEmptyTitle
	}
	if strings.TrimSpace(in.Status) == "" {
		return nil, ErrEmptyStatus
	}

	ts := NewTimestamp(now)
	// next_id > max(id) is validated on read, which is what makes this id unique.
	task := Task{
		ID:        f.NextID,
		Title:     title,
		Status:    in.Status,
		Due:       in.Due,
		Note:      in.Note,
		Sessions:  []SessionRef{},
		Links:     []Link{},
		CreatedAt: ts,
		UpdatedAt: ts,
	}
	f.Tasks = append(f.Tasks, task)
	f.NextID++
	return &f.Tasks[len(f.Tasks)-1], nil
}

// RemoveTask deletes the task outright. next_id is never lowered, so ids are not reused.
func (f *File) RemoveTask(id int) (*Task, error) {
	for i := range f.Tasks {
		if f.Tasks[i].ID != id {
			continue
		}
		removed := f.Tasks[i]
		f.Tasks = append(f.Tasks[:i], f.Tasks[i+1:]...)
		return &removed, nil
	}
	return nil, withSubject(fmt.Sprintf("#%d", id), ErrTaskNotFound)
}

func (t *Task) SetTitle(title string, now time.Time) error {
	trimmed := strings.TrimSpace(title)
	if trimmed == "" {
		return ErrEmptyTitle
	}
	t.Title = trimmed
	t.touch(now)
	return nil
}

func (t *Task) SetStatus(status string, now time.Time) error {
	if strings.TrimSpace(status) == "" {
		return ErrEmptyStatus
	}
	t.Status = status
	t.touch(now)
	return nil
}

func (t *Task) SetDue(due *Date, now time.Time) {
	t.Due = due
	t.touch(now)
}

func (t *Task) SetNote(note string, now time.Time) {
	t.Note = note
	t.touch(now)
}

// AppendNote appends to the existing note on a new line.
func (t *Task) AppendNote(note string, now time.Time) {
	if t.Note == "" {
		t.Note = note
	} else {
		t.Note = strings.TrimRight(t.Note, "\n") + "\n" + note
	}
	t.touch(now)
}

// AddLink attaches an external link. Duplicate URLs are rejected because unlink identifies links by URL.
func (t *Task) AddLink(url string, kind LinkKind, note string, now time.Time) (*Link, error) {
	for _, existing := range t.Links {
		if existing.URL == url {
			return nil, withSubject(url, ErrLinkExists)
		}
	}
	t.Links = append(t.Links, Link{URL: url, Kind: kind, Note: note, AddedAt: NewTimestamp(now)})
	t.touch(now)
	return &t.Links[len(t.Links)-1], nil
}

// AddSession links an agent session to the task.
//
// The same session may be linked to several tasks, but not twice to the same one: unlink
// identifies a session by its id. cwd is required because it is what makes a resume possible
// once the pane is gone.
func (t *Task) AddSession(ref SessionRef, now time.Time) (*SessionRef, error) {
	ref.Agent = strings.TrimSpace(ref.Agent)
	ref.SessionID = strings.TrimSpace(ref.SessionID)
	ref.Cwd = strings.TrimSpace(ref.Cwd)

	switch {
	case ref.SessionID == "":
		return nil, ErrEmptySessionID
	case ref.Agent == "":
		return nil, ErrEmptyAgent
	case ref.Cwd == "":
		return nil, ErrEmptySessionCwd
	}
	for _, existing := range t.Sessions {
		if existing.SessionID == ref.SessionID {
			return nil, withSubject(ref.SessionID, ErrSessionExists)
		}
	}

	ref.LinkedAt = NewTimestamp(now)
	t.Sessions = append(t.Sessions, ref)
	t.touch(now)
	return &t.Sessions[len(t.Sessions)-1], nil
}

func (t *Task) RemoveSession(sessionID string, now time.Time) (*SessionRef, error) {
	sessionID = strings.TrimSpace(sessionID)
	for i := range t.Sessions {
		if t.Sessions[i].SessionID != sessionID {
			continue
		}
		removed := t.Sessions[i]
		t.Sessions = append(t.Sessions[:i], t.Sessions[i+1:]...)
		t.touch(now)
		return &removed, nil
	}
	return nil, withSubject(sessionID, ErrSessionNotFound)
}

// Session returns the linked session with the given id.
func (t *Task) Session(sessionID string) (*SessionRef, bool) {
	for i := range t.Sessions {
		if t.Sessions[i].SessionID == sessionID {
			return &t.Sessions[i], true
		}
	}
	return nil, false
}

// SetLinkNote replaces the memo on an already-attached link.
func (t *Task) SetLinkNote(url, note string, now time.Time) error {
	for i := range t.Links {
		if t.Links[i].URL != url {
			continue
		}
		t.Links[i].Note = strings.TrimSpace(note)
		t.touch(now)
		return nil
	}
	return withSubject(url, ErrLinkNotFound)
}

func (t *Task) RemoveLink(url string, now time.Time) (*Link, error) {
	for i := range t.Links {
		if t.Links[i].URL != url {
			continue
		}
		removed := t.Links[i]
		t.Links = append(t.Links[:i], t.Links[i+1:]...)
		t.touch(now)
		return &removed, nil
	}
	return nil, withSubject(url, ErrLinkNotFound)
}

func (t *Task) touch(now time.Time) {
	t.UpdatedAt = NewTimestamp(now)
}
