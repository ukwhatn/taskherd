// Package model holds the taskherd task data model with its validation and mutations.
// Persistence (file IO, locking) belongs to the store package.
package model

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// CurrentVersion is the tasks.json version this binary can read and write.
const CurrentVersion = 1

const dateLayout = "2006-01-02"

var (
	ErrTaskNotFound = errors.New("タスクが見つからない")
	ErrLinkNotFound = errors.New("リンクが見つからない")
	ErrLinkExists   = errors.New("同じ URL が既に紐づいている")
	ErrEmptyTitle   = errors.New("タイトルが空")
	ErrEmptyStatus  = errors.New("ステータスが空")
)

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
		return "", fmt.Errorf("日付は YYYY-MM-DD 形式で指定する: %q", s)
	}
	return Date(s), nil
}

// Time converts the Timestamp to time.Time.
func (t Timestamp) Time() (time.Time, error) {
	return time.Parse(time.RFC3339, string(t))
}

// Time converts the Date to midnight local time.
func (d Date) Time() (time.Time, error) {
	return time.ParseInLocation(dateLayout, string(d), time.Local)
}

// ParseFile parses tasks.json bytes and applies the validation rules.
func ParseFile(data []byte) (*File, error) {
	var f File
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("tasks.json を解析できない: %w", err)
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
		return nil, fmt.Errorf("tasks.json を生成できない: %w", err)
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
	return nil, fmt.Errorf("#%d: %w", id, ErrTaskNotFound)
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
	return nil, fmt.Errorf("#%d: %w", id, ErrTaskNotFound)
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
			return nil, fmt.Errorf("%s: %w", url, ErrLinkExists)
		}
	}
	t.Links = append(t.Links, Link{URL: url, Kind: kind, Note: note, AddedAt: NewTimestamp(now)})
	t.touch(now)
	return &t.Links[len(t.Links)-1], nil
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
	return nil, fmt.Errorf("%s: %w", url, ErrLinkNotFound)
}

func (t *Task) touch(now time.Time) {
	t.UpdatedAt = NewTimestamp(now)
}
