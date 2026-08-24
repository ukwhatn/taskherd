package cli

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/ukwhatn/taskherd/internal/fetch"
	"github.com/ukwhatn/taskherd/internal/model"
	"github.com/ukwhatn/taskherd/internal/tui"
)

func (a *app) addCmd() *cobra.Command {
	var (
		status  string
		due     string
		note    string
		links   []string
		session string
		cwd     string
	)

	cmd := &cobra.Command{
		Use:   "add <title>",
		Short: "タスクを作成する",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := a.config()
			if err != nil {
				return err
			}
			if status == "" {
				status = cfg.DefaultStatus()
			}
			if err := requireColumn(cfg, status); err != nil {
				return err
			}
			duePtr, err := parseDueFlag(due)
			if err != nil {
				return err
			}
			urls := make([]string, 0, len(links))
			for _, raw := range links {
				parsed, err := parseLinkURL(raw)
				if err != nil {
					return err
				}
				urls = append(urls, parsed)
			}

			// The session is resolved before the store transaction so that herdr calls
			// never happen while the write lock is held.
			var ref sessionRef
			if session != "" {
				ref, err = a.resolveSession(cmd.Context(), sessionSpecFromFlag(session, cwd))
				if err != nil {
					return err
				}
			}

			classifier := cfg.Classifier()
			now := a.env.Now()
			var created *model.Task
			err = a.tasks().Update(cmd.Context(), func(f *model.File) error {
				task, err := f.AddTask(model.TaskInput{Title: args[0], Status: status, Due: duePtr, Note: note}, now)
				if err != nil {
					return err
				}
				for _, u := range urls {
					if _, err := task.AddLink(u, classifier.Classify(u), "", now); err != nil {
						return err
					}
				}
				if ref.SessionID != "" {
					if _, err := task.AddSession(ref.SessionRef, now); err != nil {
						return err
					}
				}
				created = task
				return nil
			})
			if err != nil {
				return err
			}
			if ref.PaneID != "" {
				a.stampTaskToken(cmd.Context(), ref.PaneID, created.ID)
			}
			return a.emitTask(created, fmt.Sprintf("#%d を作成した（%s）: %s", created.ID, created.Status, created.Title))
		},
	}

	cmd.Flags().StringVar(&status, "status", "", "作成時の列 id（既定: config の先頭列）")
	cmd.Flags().StringVar(&due, "due", "", "期日（YYYY-MM-DD）")
	cmd.Flags().StringVar(&note, "note", "", "note の初期値")
	cmd.Flags().StringArrayVar(&links, "link", nil, "紐づける外部リンク URL（複数指定可）")
	cmd.Flags().StringVar(&session, "session", "", "紐づけるセッション（current または UUID）")
	cmd.Flags().StringVar(&cwd, "cwd", "", "セッションの作業ディレクトリ（--session が UUID で herdr が解決できない場合は必須）")
	return cmd
}

func (a *app) listCmd() *cobra.Command {
	var (
		statuses []string
		all      bool
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "タスク一覧を表示する（既定は terminal 列を除く）",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := a.config()
			if err != nil {
				return err
			}
			f, err := a.tasks().Load()
			if err != nil {
				return err
			}

			tasks := filterTasks(f.Tasks, cfg.Columns, statuses, all)
			sortTasks(tasks, cfg.Columns)
			live := a.liveSessions(cmd.Context(), tasks)

			if a.jsonOut {
				return a.emitJSON(struct {
					Tasks         []model.Task            `json:"tasks"`
					Herdr         *herdrStatusJSON        `json:"herdr,omitempty"`
					SessionStates map[string]sessionState `json:"session_states,omitempty"`
				}{Tasks: tasks, Herdr: live.statusJSON(), SessionStates: live.forTasks(tasks)})
			}
			live.note(a)
			if len(tasks) == 0 {
				fmt.Fprintln(a.env.Out, "該当するタスクがない")
				return nil
			}
			for _, task := range tasks {
				fmt.Fprintln(a.env.Out, formatTaskLine(task, live.badge(task)))
			}
			return nil
		},
	}

	cmd.Flags().StringArrayVar(&statuses, "status", nil, "表示する列 id（複数指定可。未定義の列 id も指定できる）")
	cmd.Flags().BoolVar(&all, "all", false, "terminal 列も表示する")
	return cmd
}

func (a *app) showCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <id>",
		Short: "タスクの詳細を表示する",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseID(args[0])
			if err != nil {
				return err
			}
			cfg, err := a.config()
			if err != nil {
				return err
			}
			f, err := a.tasks().Load()
			if err != nil {
				return err
			}
			task, err := f.Task(id)
			if err != nil {
				return err
			}

			live := a.liveSessions(cmd.Context(), []model.Task{*task})
			// Link state comes from the cache only: `show` reports what is known, and `refresh`
			// is what goes and asks the outside world.
			links := a.cache().Load().LinkStates(
				task.Links, a.env.Now(), time.Duration(cfg.Board.CacheTTLMinutes)*time.Minute)

			if a.jsonOut {
				return a.emitJSON(struct {
					Task          *model.Task                `json:"task"`
					Herdr         *herdrStatusJSON           `json:"herdr,omitempty"`
					SessionStates map[string]sessionState    `json:"session_states,omitempty"`
					LinkStates    map[string]fetch.LinkState `json:"link_states,omitempty"`
				}{
					Task:          task,
					Herdr:         live.statusJSON(),
					SessionStates: live.forTasks([]model.Task{*task}),
					LinkStates:    links,
				})
			}
			live.note(a)
			fmt.Fprint(a.env.Out, formatTaskDetail(task, cfg.Columns, live, links))
			return nil
		},
	}
}

func (a *app) editCmd() *cobra.Command {
	var (
		title  string
		due    string
		status string
	)

	cmd := &cobra.Command{
		Use:   "edit <id>",
		Short: "タスクの属性を更新する",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseID(args[0])
			if err != nil {
				return err
			}
			changedTitle := cmd.Flags().Changed("title")
			changedDue := cmd.Flags().Changed("due")
			changedStatus := cmd.Flags().Changed("status")
			if !changedTitle && !changedDue && !changedStatus {
				return &UserError{
					Msg:      "更新する項目が指定されていない",
					HintText: "--title / --due / --status のいずれかを指定する（--due \"\" で期日を消す）",
				}
			}

			cfg, err := a.config()
			if err != nil {
				return err
			}
			if changedStatus {
				if err := requireColumn(cfg, status); err != nil {
					return err
				}
			}
			duePtr, err := parseDueFlag(due)
			if err != nil {
				return err
			}

			now := a.env.Now()
			var edited *model.Task
			err = a.tasks().Update(cmd.Context(), func(f *model.File) error {
				task, err := f.Task(id)
				if err != nil {
					return err
				}
				if changedTitle {
					if err := task.SetTitle(title, now); err != nil {
						return err
					}
				}
				if changedStatus {
					if err := task.SetStatus(status, now); err != nil {
						return err
					}
				}
				if changedDue {
					task.SetDue(duePtr, now)
				}
				edited = task
				return nil
			})
			if err != nil {
				return err
			}
			return a.emitTask(edited, fmt.Sprintf("#%d を更新した（%s）: %s", edited.ID, edited.Status, edited.Title))
		},
	}

	cmd.Flags().StringVar(&title, "title", "", "新しいタイトル")
	cmd.Flags().StringVar(&due, "due", "", "新しい期日（YYYY-MM-DD。空文字で削除）")
	cmd.Flags().StringVar(&status, "status", "", "新しい列 id")
	return cmd
}

func (a *app) moveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "move <id> <status>",
		Short: "タスクを別の列へ移動する",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.move(cmd, args[0], args[1])
		},
	}
}

func (a *app) doneCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "done <id>",
		Short: "タスクを done 列へ移動する（move <id> done の alias）",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.move(cmd, args[0], "done")
		},
	}
}

func (a *app) move(cmd *cobra.Command, idArg, status string) error {
	id, err := parseID(idArg)
	if err != nil {
		return err
	}
	cfg, err := a.config()
	if err != nil {
		return err
	}
	if err := requireColumn(cfg, status); err != nil {
		return err
	}

	now := a.env.Now()
	var moved *model.Task
	err = a.tasks().Update(cmd.Context(), func(f *model.File) error {
		task, err := f.Task(id)
		if err != nil {
			return err
		}
		if err := task.SetStatus(status, now); err != nil {
			return err
		}
		moved = task
		return nil
	})
	if err != nil {
		return err
	}
	return a.emitTask(moved, fmt.Sprintf("#%d を %s へ移動した: %s", moved.ID, moved.Status, moved.Title))
}

func (a *app) rmCmd() *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use:   "rm <id>",
		Short: "タスクを削除する",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseID(args[0])
			if err != nil {
				return err
			}
			if !yes {
				if a.jsonOut {
					return &UserError{
						Msg:      "削除の確認が必要",
						HintText: "--yes を指定する（--json では確認プロンプトを出さない）",
					}
				}
				f, err := a.tasks().Load()
				if err != nil {
					return err
				}
				task, err := f.Task(id)
				if err != nil {
					return err
				}
				confirmed, err := a.confirm(fmt.Sprintf("#%d 「%s」を削除する", task.ID, task.Title))
				if err != nil {
					return err
				}
				if !confirmed {
					fmt.Fprintln(a.env.Out, "中止した")
					return nil
				}
			}

			var removed *model.Task
			err = a.tasks().Update(cmd.Context(), func(f *model.File) error {
				task, err := f.RemoveTask(id)
				if err != nil {
					return err
				}
				removed = task
				return nil
			})
			if err != nil {
				return err
			}
			return a.emitTask(removed, fmt.Sprintf("#%d を削除した: %s", removed.ID, removed.Title))
		},
	}

	cmd.Flags().BoolVar(&yes, "yes", false, "確認プロンプトを省略する")
	return cmd
}

func filterTasks(tasks []model.Task, columns model.Columns, statuses []string, all bool) []model.Task {
	wanted := make(map[string]bool, len(statuses))
	for _, status := range statuses {
		wanted[status] = true
	}

	filtered := make([]model.Task, 0, len(tasks))
	for _, task := range tasks {
		switch {
		case len(wanted) > 0:
			if !wanted[task.Status] {
				continue
			}
		case all:
		default:
			// Tasks whose column was deleted from config are kept: they need attention,
			// and only known terminal columns are hidden by default.
			if col, ok := columns.Find(task.Status); ok && col.Kind == model.ColumnKindTerminal {
				continue
			}
		}
		filtered = append(filtered, task)
	}
	return filtered
}

func sortTasks(tasks []model.Task, columns model.Columns) {
	rank := func(status string) int {
		if idx := columns.Index(status); idx >= 0 {
			return idx
		}
		return len(columns)
	}
	sort.SliceStable(tasks, func(i, j int) bool {
		ri, rj := rank(tasks[i].Status), rank(tasks[j].Status)
		if ri != rj {
			return ri < rj
		}
		return tasks[i].ID < tasks[j].ID
	})
}

// formatLinkState renders one link's cached live state. The value shown is always the last
// success; a failing refresh is reported alongside it rather than replacing it.
func formatLinkState(state fetch.LinkState) string {
	if !state.Fetched {
		if state.Err != "" {
			return "取得失敗: " + state.Err
		}
		return "未取得（refresh で取得する）"
	}

	line := fmt.Sprintf("%s（%s前", tui.DescribeLink(state), tui.FormatAge(state.Age))
	if state.Stale {
		line += " / TTL 超過"
	}
	line += "）"
	if state.Err != "" {
		line += " 最新の取得は失敗: " + state.Err
	}
	return line
}

func formatTaskLine(task model.Task, badge string) string {
	due := "-"
	if task.Due != nil {
		due = string(*task.Due)
	}
	counts := fmt.Sprintf("L%d S%d", len(task.Links), len(task.Sessions))
	if badge == "" {
		return fmt.Sprintf("#%-4d %-10s %-10s %-7s %s", task.ID, task.Status, due, counts, task.Title)
	}
	return fmt.Sprintf("#%-4d %-10s %-10s %-7s %-8s %s", task.ID, task.Status, due, counts, badge, task.Title)
}

func formatTaskDetail(task *model.Task, columns model.Columns, live liveState, links map[string]fetch.LinkState) string {
	var b strings.Builder
	fmt.Fprintf(&b, "#%d %s\n", task.ID, task.Title)

	statusLabel := "未定義の列"
	if col, ok := columns.Find(task.Status); ok {
		statusLabel = col.Label
	}
	fmt.Fprintf(&b, "status:  %s (%s)\n", task.Status, statusLabel)
	if task.Due != nil {
		fmt.Fprintf(&b, "due:     %s\n", string(*task.Due))
	}
	fmt.Fprintf(&b, "created: %s\n", task.CreatedAt)
	fmt.Fprintf(&b, "updated: %s\n", task.UpdatedAt)

	fmt.Fprintf(&b, "\nlinks (%d):\n", len(task.Links))
	for _, link := range task.Links {
		fmt.Fprintf(&b, "  - [%s] %s\n", link.Kind, link.URL)
		if link.Note != "" {
			fmt.Fprintf(&b, "    note: %s\n", link.Note)
		}
		if state, ok := links[link.URL]; ok && state.Fetchable() {
			fmt.Fprintf(&b, "    live: %s\n", formatLinkState(state))
		}
		fmt.Fprintf(&b, "    added: %s\n", link.AddedAt)
	}

	fmt.Fprintf(&b, "\nsessions (%d):\n", len(task.Sessions))
	for _, session := range task.Sessions {
		fmt.Fprintf(&b, "  - %s %s\n", session.Agent, session.SessionID)
		if state, ok := live.states[session.SessionID]; live.available && ok {
			fmt.Fprintf(&b, "    state: %s", state.State)
			if state.PaneID != "" {
				fmt.Fprintf(&b, " (pane %s)", state.PaneID)
			}
			fmt.Fprintln(&b)
		}
		fmt.Fprintf(&b, "    cwd: %s\n", session.Cwd)
		if session.Label != "" {
			fmt.Fprintf(&b, "    label: %s\n", session.Label)
		}
		fmt.Fprintf(&b, "    linked: %s\n", session.LinkedAt)
	}

	fmt.Fprint(&b, "\nnote:\n")
	if task.Note == "" {
		fmt.Fprint(&b, "  (なし)\n")
		return b.String()
	}
	for _, line := range strings.Split(task.Note, "\n") {
		fmt.Fprintf(&b, "  %s\n", line)
	}
	return b.String()
}
