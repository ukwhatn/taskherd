// Package model は taskherd のタスクデータモデルと、その検証・変更操作を提供する。
// 永続化（ファイル入出力・排他制御）は store パッケージが担う。
package model

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// CurrentVersion は本バイナリが読み書きできる tasks.json の version。
const CurrentVersion = 1

const dateLayout = "2006-01-02"

var (
	ErrTaskNotFound = errors.New("タスクが見つからない")
	ErrLinkNotFound = errors.New("リンクが見つからない")
	ErrLinkExists   = errors.New("同じ URL が既に紐づいている")
	ErrEmptyTitle   = errors.New("タイトルが空")
	ErrEmptyStatus  = errors.New("ステータスが空")
)

// Timestamp は RFC 3339 表記の時刻。
type Timestamp string

// Date は YYYY-MM-DD 表記のカレンダー日付。
type Date string

// LinkKind は外部リンクの種別。URL から自動判別する。
type LinkKind string

const (
	LinkKindGitHubPR    LinkKind = "github_pr"
	LinkKindGitHubIssue LinkKind = "github_issue"
	LinkKindJira        LinkKind = "jira"
	LinkKindOther       LinkKind = "other"
)

// File は tasks.json 全体。
type File struct {
	Version int    `json:"version"`
	NextID  int    `json:"next_id"`
	Tasks   []Task `json:"tasks"`
}

// Task は 1 件のタスク。
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

// SessionRef はタスクに紐づくエージェントセッション。pane_id は可変キーのため保存しない。
type SessionRef struct {
	Agent     string    `json:"agent"`
	SessionID string    `json:"session_id"`
	Cwd       string    `json:"cwd"`
	Label     string    `json:"label"`
	LinkedAt  Timestamp `json:"linked_at"`
}

// Link はタスクに紐づく外部リンク。
type Link struct {
	URL     string    `json:"url"`
	Kind    LinkKind  `json:"kind"`
	Note    string    `json:"note"`
	AddedAt Timestamp `json:"added_at"`
}

// TaskInput は新規タスクの属性。
type TaskInput struct {
	Title  string
	Status string
	Due    *Date
	Note   string
}

// NewFile は空の tasks.json 相当を返す。
func NewFile() *File {
	return &File{Version: CurrentVersion, NextID: 1, Tasks: []Task{}}
}

// NewTimestamp は t を RFC 3339（ローカルオフセット付き）の Timestamp にする。
func NewTimestamp(t time.Time) Timestamp {
	return Timestamp(t.Format(time.RFC3339))
}

// ParseDate は YYYY-MM-DD 表記を検証して Date にする。
func ParseDate(s string) (Date, error) {
	if _, err := time.Parse(dateLayout, s); err != nil {
		return "", fmt.Errorf("日付は YYYY-MM-DD 形式で指定する: %q", s)
	}
	return Date(s), nil
}

// Time は Timestamp を time.Time に変換する。
func (t Timestamp) Time() (time.Time, error) {
	return time.Parse(time.RFC3339, string(t))
}

// Time は Date を、その日の 0 時（ローカル）の time.Time に変換する。
func (d Date) Time() (time.Time, error) {
	return time.ParseInLocation(dateLayout, string(d), time.Local)
}

// ParseFile は tasks.json のバイト列を解析し、§3.1 の検証規則を適用する。
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

// MarshalFile は tasks.json のバイト列を生成する。
func MarshalFile(f *File) ([]byte, error) {
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("tasks.json を生成できない: %w", err)
	}
	return append(data, '\n'), nil
}

// Normalize は省略された配列を空スライスに揃える（JSON 出力を null でなく [] にする）。
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

// Task は id に対応するタスクへのポインタを返す。返り値への変更は File に反映される。
func (f *File) Task(id int) (*Task, error) {
	for i := range f.Tasks {
		if f.Tasks[i].ID == id {
			return &f.Tasks[i], nil
		}
	}
	return nil, fmt.Errorf("#%d: %w", id, ErrTaskNotFound)
}

// AddTask は next_id を採番して新規タスクを追加する。
func (f *File) AddTask(in TaskInput, now time.Time) (*Task, error) {
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return nil, ErrEmptyTitle
	}
	if strings.TrimSpace(in.Status) == "" {
		return nil, ErrEmptyStatus
	}

	ts := NewTimestamp(now)
	// next_id > max(id) は読込時に検証済みのため、この採番で id は一意になる。
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

// RemoveTask は id のタスクを物理削除する。next_id は減らさない（id を再利用しない）。
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

// AppendNote は既存 note の末尾に改行区切りで追記する。
func (t *Task) AppendNote(note string, now time.Time) {
	if t.Note == "" {
		t.Note = note
	} else {
		t.Note = strings.TrimRight(t.Note, "\n") + "\n" + note
	}
	t.touch(now)
}

// AddLink は外部リンクを追加する。同一 URL の重複は unlink の指定が曖昧になるため拒否する。
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
