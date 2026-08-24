package tui

import (
	"fmt"
	"strings"

	"github.com/ukwhatn/taskherd/internal/fetch"
	"github.com/ukwhatn/taskherd/internal/herdrc"
	"github.com/ukwhatn/taskherd/internal/model"
)

func (b *Board) renderDetail() string {
	task := b.currentTask()
	if task == nil {
		b.mode = modeBoard
		return b.render()
	}

	b.detail.SetContent(b.detailContent(*task))
	header := b.styles.heading.Render(truncate(fmt.Sprintf("#%d %s", task.ID, task.Title), b.width))
	help := b.styles.footer.Render(truncate(
		"j/k スクロール e タイトル d 期日 n note x リンク r 取得 g jump esc 戻る", b.width))

	sections := []string{header, b.detail.View(), help}
	if b.status != "" {
		style := b.styles.status
		if b.statusIsError {
			style = b.styles.alert
		}
		sections = append(sections, style.Render(truncate(b.status, b.width)))
	}
	return strings.Join(sections, "\n")
}

// detailContent is the scrollable body: the task's own attributes, every link's live state,
// every session's live state, and the note in full.
func (b *Board) detailContent(task model.Task) string {
	var out strings.Builder

	statusLabel := unknownColumnLabel
	if col, ok := b.settings.Columns.Find(task.Status); ok {
		statusLabel = col.Label
	}
	fmt.Fprintf(&out, "status:  %s (%s)\n", task.Status, statusLabel)
	if task.Due != nil {
		due := string(*task.Due)
		if isOverdue(*task.Due, b.deps.now()) {
			due = b.styles.dueOverdue.Render(due + " 超過")
		}
		fmt.Fprintf(&out, "due:     %s\n", due)
	}
	fmt.Fprintf(&out, "created: %s\nupdated: %s\n", task.CreatedAt, task.UpdatedAt)

	fmt.Fprintf(&out, "\n%s\n", b.styles.heading.Render(fmt.Sprintf("links (%d)", len(task.Links))))
	for _, link := range task.Links {
		fmt.Fprintf(&out, "  - [%s] %s\n", link.Kind, link.URL)
		if link.Note != "" {
			fmt.Fprintf(&out, "    note:  %s\n", link.Note)
		}
		fmt.Fprintf(&out, "    live:  %s\n", b.linkDetail(link))
	}

	fmt.Fprintf(&out, "\n%s\n", b.styles.heading.Render(fmt.Sprintf("sessions (%d)", len(task.Sessions))))
	for _, session := range task.Sessions {
		fmt.Fprintf(&out, "  - %s %s\n", session.Agent, session.SessionID)
		fmt.Fprintf(&out, "    state: %s\n", b.sessionDetail(session))
		fmt.Fprintf(&out, "    cwd:   %s\n", session.Cwd)
		if session.Label != "" {
			fmt.Fprintf(&out, "    label: %s\n", session.Label)
		}
	}

	fmt.Fprintf(&out, "\n%s\n", b.styles.heading.Render("note"))
	if task.Note == "" {
		out.WriteString("  (なし)\n")
		return out.String()
	}
	for _, line := range strings.Split(task.Note, "\n") {
		fmt.Fprintf(&out, "  %s\n", line)
	}
	return out.String()
}

// linkDetail spells out one link's cached state, including why it is missing when it is.
func (b *Board) linkDetail(link model.Link) string {
	state, ok := b.links[link.URL]
	if !ok {
		state = fetch.LinkState{URL: link.URL, Kind: link.Kind}
	}
	if !state.Fetchable() {
		return b.styles.dim.Render("ライブ取得の対象外")
	}
	if !state.Fetched {
		if state.Err != "" {
			return b.styles.alert.Render("取得失敗: " + state.Err)
		}
		return b.styles.dim.Render("未取得")
	}

	summary := DescribeLink(state)
	age := fmt.Sprintf("%s前", FormatAge(state.Age))
	if state.Stale {
		summary = b.styles.linkStale.Render(summary)
		age = b.styles.linkStale.Render(age + " / TTL 超過")
	}
	line := fmt.Sprintf("%s（%s）", summary, age)
	if state.Err != "" {
		// The value shown is the last success; the current attempt is failing.
		line += "\n           " + b.styles.alert.Render("最新の取得は失敗: "+state.Err)
	}
	return line
}

// DescribeLink spells out a fetched link's state in full, for the detail view and `show`.
func DescribeLink(state fetch.LinkState) string {
	switch {
	case state.GitHub != nil && state.Kind == model.LinkKindGitHubPR:
		parts := []string{strings.ToLower(state.GitHub.State)}
		if state.GitHub.IsDraft {
			parts = append(parts, "draft")
		}
		if state.GitHub.ReviewDecision != "" {
			parts = append(parts, "review="+strings.ToLower(state.GitHub.ReviewDecision))
		}
		if state.GitHub.Checks != "" {
			parts = append(parts, "checks="+state.GitHub.Checks)
		}
		return strings.Join(parts, " ") + titleSuffix(state.GitHub.Title)
	case state.GitHub != nil:
		return strings.ToLower(state.GitHub.State) + titleSuffix(state.GitHub.Title)
	case state.Jira != nil:
		return fmt.Sprintf("%s (%s)%s", state.Jira.StatusName, state.Jira.StatusCategory, titleSuffix(state.Jira.Summary))
	default:
		return "不明"
	}
}

func titleSuffix(title string) string {
	if title == "" {
		return ""
	}
	return " — " + title
}

// sessionDetail spells out one session's live state, with the pane it is in when it has one.
func (b *Board) sessionDetail(session model.SessionRef) string {
	if !b.sessions.Available {
		return b.styles.sessionOffline.Render("herdr 不達（オフライン）")
	}
	state, ok := b.sessions.State[session.SessionID]
	if !ok {
		state = herdrc.StateOffline
	}
	if paneID := b.sessions.Pane[session.SessionID]; paneID != "" {
		return fmt.Sprintf("%s (pane %s)", state, paneID)
	}
	return fmt.Sprintf("%s（pane なし。g で resume 起動）", state)
}
