package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/ukwhatn/taskherd/internal/herdrc"
	"github.com/ukwhatn/taskherd/internal/model"
)

// currentSessionKeyword is the value of --session that means "the session in this pane".
const currentSessionKeyword = "current"

// resumeAgent is the only agent taskherd knows how to resume, so it is also the agent assumed
// for a session herdr cannot resolve.
const resumeAgent = "claude"

// sessionSpec is how the user identified a session on the command line.
type sessionSpec struct {
	current   bool
	sessionID string
	paneID    string
	cwd       string
	label     string
}

// sessionSpecFromFlag reads the shorthand accepted by `add --session`: either the keyword
// current or a session UUID.
func sessionSpecFromFlag(value, cwd string) sessionSpec {
	value = strings.TrimSpace(value)
	if value == currentSessionKeyword {
		return sessionSpec{current: true}
	}
	return sessionSpec{sessionID: value, cwd: cwd}
}

func (a *app) sessionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "session",
		Short: "タスクとエージェントセッションの紐づけを操作する",
	}
	cmd.AddCommand(a.sessionLinkCmd(), a.sessionUnlinkCmd())
	return cmd
}

func (a *app) sessionLinkCmd() *cobra.Command {
	var spec sessionSpec

	cmd := &cobra.Command{
		Use:   "link <id>",
		Short: "エージェントセッションをタスクに紐づける",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseID(args[0])
			if err != nil {
				return err
			}
			count := 0
			for _, set := range []bool{spec.current, spec.sessionID != "", spec.paneID != ""} {
				if set {
					count++
				}
			}
			if count != 1 {
				return &UserError{
					Msg:      "セッションの指定方法が 1 つに定まらない",
					HintText: "--current / --session-id <uuid> / --pane <pane_id> のいずれか 1 つを指定する",
				}
			}

			ref, err := a.resolveSession(cmd.Context(), spec)
			if err != nil {
				return err
			}

			now := a.env.Now()
			var updated *model.Task
			err = a.tasks().Update(cmd.Context(), func(f *model.File) error {
				task, err := f.Task(id)
				if err != nil {
					return err
				}
				if _, err := task.AddSession(ref.SessionRef, now); err != nil {
					return err
				}
				updated = task
				return nil
			})
			if err != nil {
				return err
			}

			if ref.PaneID != "" {
				a.stampTaskToken(cmd.Context(), ref.PaneID, updated.ID, updated.Title)
			}
			return a.emitTask(updated, fmt.Sprintf("#%d に %s セッション %s を紐づけた", updated.ID, ref.Agent, ref.SessionID))
		},
	}

	cmd.Flags().BoolVar(&spec.current, "current", false, "この pane で動いているセッションを紐づける")
	cmd.Flags().StringVar(&spec.sessionID, "session-id", "", "セッション UUID を明示指定する")
	cmd.Flags().StringVar(&spec.paneID, "pane", "", "対象 pane_id を明示指定する")
	cmd.Flags().StringVar(&spec.cwd, "cwd", "", "セッションの作業ディレクトリ（herdr が解決できない場合は必須）")
	cmd.Flags().StringVar(&spec.label, "label", "", "セッションの表示名")
	return cmd
}

func (a *app) sessionUnlinkCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unlink <id> <uuid>",
		Short: "セッションの紐づけを外す",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseID(args[0])
			if err != nil {
				return err
			}
			sessionID := strings.TrimSpace(args[1])

			now := a.env.Now()
			var updated *model.Task
			err = a.tasks().Update(cmd.Context(), func(f *model.File) error {
				task, err := f.Task(id)
				if err != nil {
					return err
				}
				if _, err := task.RemoveSession(sessionID, now); err != nil {
					return err
				}
				updated = task
				return nil
			})
			if err != nil {
				return err
			}
			return a.emitTask(updated, fmt.Sprintf("#%d から セッション %s の紐づけを外した", updated.ID, sessionID))
		},
	}
}

// sessionRef is a resolved session plus the pane it was resolved from, if any.
// The pane is not stored on the task: it is volatile, and only used to stamp the task id back.
type sessionRef struct {
	model.SessionRef
	PaneID string
}

// resolveSession turns a command line specification into a session to store.
//
// A session found in the snapshot carries its agent and cwd from herdr. One that is not there
// (the pane is gone, or herdr is unreachable) needs an explicit cwd and is assumed to be claude:
// without a cwd the resume path cannot work, so an empty one is never stored.
func (a *app) resolveSession(ctx context.Context, spec sessionSpec) (sessionRef, error) {
	client := a.herdr()

	if spec.current {
		spec.paneID = client.CurrentPaneID()
		if spec.paneID == "" {
			return sessionRef{}, &UserError{
				Msg:      "現在の pane を特定できない（HERDR_PANE_ID が無い）",
				HintText: "herdr 管理 pane の外では --session-id <uuid> --cwd <path> で明示指定する",
			}
		}
	}

	if spec.paneID != "" {
		snapshot, err := client.Snapshot(ctx)
		if err != nil {
			return sessionRef{}, err
		}
		agent, ok := snapshot.AgentByPaneID(spec.paneID)
		if !ok {
			return sessionRef{}, &UserError{
				Msg:      fmt.Sprintf("pane %s でエージェントが検出されていない", spec.paneID),
				HintText: "エージェントが動いている pane を指定するか、--session-id <uuid> --cwd <path> で明示指定する",
			}
		}
		if agent.SessionID() == "" {
			return sessionRef{}, &UserError{
				Msg:      fmt.Sprintf("pane %s ではセッション ID を検出できない", spec.paneID),
				HintText: "--session-id <uuid> で明示指定するか、herdr integration install claude を実行して再試行する",
			}
		}
		return sessionRef{
			SessionRef: model.SessionRef{
				Agent:     agent.Agent,
				SessionID: agent.SessionID(),
				Cwd:       agent.Cwd,
				Label:     spec.label,
			},
			PaneID: agent.PaneID,
		}, nil
	}

	// An explicit session id: prefer herdr's own answer, fall back to the given cwd.
	if snapshot, err := client.Snapshot(ctx); err == nil {
		if agent, ok := snapshot.AgentBySessionID(spec.sessionID); ok {
			return sessionRef{
				SessionRef: model.SessionRef{
					Agent:     agent.Agent,
					SessionID: agent.SessionID(),
					Cwd:       agent.Cwd,
					Label:     spec.label,
				},
				PaneID: agent.PaneID,
			}, nil
		}
	}
	if spec.cwd == "" {
		return sessionRef{}, &UserError{
			Msg:      fmt.Sprintf("セッション %s を herdr 上で解決できない", spec.sessionID),
			HintText: "--cwd <path> でセッションの作業ディレクトリを指定する（resume に必要なため省略できない）",
		}
	}
	return sessionRef{
		SessionRef: model.SessionRef{
			Agent:     resumeAgent,
			SessionID: spec.sessionID,
			Cwd:       spec.cwd,
			Label:     spec.label,
		},
	}, nil
}

// stampTaskToken records the task id and display name on the pane. It is a display convenience in
// herdr's own UI, so a failure is reported but never fails the command that triggered it.
func (a *app) stampTaskToken(ctx context.Context, paneID string, taskID int, title string) {
	if err := a.herdr().ReportTaskDisplay(ctx, paneID, taskID, title); err != nil && !a.jsonOut {
		fmt.Fprintf(a.env.Err, "注記: pane %s へのタスク id 記録に失敗した: %v\n", paneID, err)
	}
}

// jumpAction is what jump did, reported in the --json output.
const (
	jumpActionFocus  = "focus"
	jumpActionResume = "resume"
)

func (a *app) jumpCmd() *cobra.Command {
	var (
		sessionID string
		yes       bool
	)

	cmd := &cobra.Command{
		Use:   "jump <id>",
		Short: "紐づいたセッションへ移動する（消滅している場合は resume 起動）",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseID(args[0])
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
			target, err := a.pickSession(task, sessionID)
			if err != nil {
				return err
			}
			return a.jumpTo(cmd.Context(), task, target, yes)
		},
	}

	cmd.Flags().StringVar(&sessionID, "session", "", "移動先のセッション UUID（複数紐づいている場合に指定）")
	cmd.Flags().BoolVar(&yes, "yes", false, "resume 起動の確認プロンプトを省略する")
	return cmd
}

// pickSession chooses which linked session to jump to, prompting only when it is safe to do so.
func (a *app) pickSession(task *model.Task, sessionID string) (*model.SessionRef, error) {
	if len(task.Sessions) == 0 {
		return nil, &UserError{
			Msg:      fmt.Sprintf("#%d にセッションが紐づいていない", task.ID),
			HintText: fmt.Sprintf("taskherd session link %d --current で紐づける", task.ID),
		}
	}
	if sessionID != "" {
		session, ok := task.Session(strings.TrimSpace(sessionID))
		if !ok {
			return nil, &UserError{
				Msg:      fmt.Sprintf("#%d にセッション %s は紐づいていない", task.ID, sessionID),
				HintText: "taskherd show " + fmt.Sprint(task.ID) + " で紐づいているセッションを確認する",
			}
		}
		return session, nil
	}
	if len(task.Sessions) == 1 {
		return &task.Sessions[0], nil
	}

	if a.jsonOut {
		ids := make([]string, 0, len(task.Sessions))
		for _, session := range task.Sessions {
			ids = append(ids, session.SessionID)
		}
		return nil, &UserError{
			Msg:      fmt.Sprintf("#%d には %d 件のセッションが紐づいている", task.ID, len(task.Sessions)),
			HintText: "--session <uuid> で移動先を指定する（候補: " + strings.Join(ids, ", ") + "）",
		}
	}
	return a.promptSession(task)
}

func (a *app) promptSession(task *model.Task) (*model.SessionRef, error) {
	fmt.Fprintf(a.env.Out, "#%d に紐づくセッション:\n", task.ID)
	for i, session := range task.Sessions {
		label := session.Label
		if label == "" {
			label = session.Cwd
		}
		fmt.Fprintf(a.env.Out, "  %d) %s %s  %s\n", i+1, session.Agent, session.SessionID, label)
	}

	choice, err := a.readLine("移動先の番号")
	if err != nil {
		return nil, err
	}
	index := 0
	if _, err := fmt.Sscanf(choice, "%d", &index); err != nil || index < 1 || index > len(task.Sessions) {
		return nil, &UserError{
			Msg:      fmt.Sprintf("番号が不正: %q", choice),
			HintText: fmt.Sprintf("1〜%d の番号を入力する（--session <uuid> でも指定できる）", len(task.Sessions)),
		}
	}
	return &task.Sessions[index-1], nil
}

func (a *app) jumpTo(ctx context.Context, task *model.Task, target *model.SessionRef, yes bool) error {
	client := a.herdr()

	snapshot, err := client.Snapshot(ctx)
	if err != nil {
		return a.offlineJumpError(target, err)
	}

	if agent, ok := snapshot.AgentBySessionID(target.SessionID); ok {
		// One focus call moves workspace, tab and pane together.
		if err := client.FocusAgent(ctx, agent.PaneID); err != nil {
			return err
		}
		a.stampTaskToken(ctx, agent.PaneID, task.ID, task.Title)
		return a.emitJump(task, target, jumpActionFocus, agent.PaneID, false)
	}

	if target.Agent != resumeAgent {
		return &UserError{
			Msg:      fmt.Sprintf("%s セッションの pane が消滅している。この agent の resume には未対応", target.Agent),
			HintText: fmt.Sprintf("%s で手動再開する", target.Cwd),
		}
	}

	if !yes {
		if a.jsonOut {
			return &UserError{
				Msg:      "pane が消滅しているため resume 起動の確認が必要",
				HintText: "--yes を指定する（--json では確認プロンプトを出さない）",
			}
		}
		confirmed, err := a.confirm(fmt.Sprintf("pane が消滅している。%s で claude --resume を起動する", target.Cwd))
		if err != nil {
			return err
		}
		if !confirmed {
			fmt.Fprintln(a.env.Out, "中止した")
			return nil
		}
	}

	// Focused as it is created, matching the live-pane branch above: jump means "take me there"
	// either way, and a resumed session left in a background tab would be the one case where it
	// silently does not.
	tab, err := client.CreateTab(ctx, herdrc.TabSpec{Cwd: target.Cwd, Label: task.Title, Focus: true})
	if err != nil {
		return err
	}
	started, err := client.StartAgent(ctx, herdrc.AgentSpec{
		Name:   fmt.Sprintf("taskherd-%d", task.ID),
		Kind:   resumeAgent,
		PaneID: tab.PaneID,
		Args:   []string{"--resume", target.SessionID},
	})
	if err != nil {
		return err
	}
	a.stampTaskToken(ctx, started.PaneID, task.ID, task.Title)
	return a.emitJump(task, target, jumpActionResume, started.PaneID, started.NeedsAttention)
}

// offlineJumpError turns an unreachable herdr into the command the user can run by hand.
func (a *app) offlineJumpError(target *model.SessionRef, cause error) error {
	if target.Agent == resumeAgent {
		return &UserError{
			Msg:      fmt.Sprintf("herdr に接続できないため jump できない: %v", cause),
			HintText: fmt.Sprintf("cd %s && claude --resume %s を実行する", target.Cwd, target.SessionID),
		}
	}
	return &UserError{
		Msg:      fmt.Sprintf("herdr に接続できないため jump できない: %v", cause),
		HintText: fmt.Sprintf("%s で %s セッションを手動再開する", target.Cwd, target.Agent),
	}
}

func (a *app) emitJump(task *model.Task, target *model.SessionRef, action, paneID string, needsAttention bool) error {
	if a.jsonOut {
		return a.emitJSON(struct {
			TaskID         int    `json:"task_id"`
			SessionID      string `json:"session_id"`
			Action         string `json:"action"`
			PaneID         string `json:"pane_id"`
			NeedsAttention bool   `json:"needs_attention"`
		}{
			TaskID:         task.ID,
			SessionID:      target.SessionID,
			Action:         action,
			PaneID:         paneID,
			NeedsAttention: needsAttention,
		})
	}

	if action == jumpActionFocus {
		fmt.Fprintf(a.env.Out, "#%d のセッションへ移動した（pane %s）\n", task.ID, paneID)
		return nil
	}
	fmt.Fprintf(a.env.Out, "#%d のセッションを pane %s で resume 起動した\n", task.ID, paneID)
	if needsAttention {
		fmt.Fprintln(a.env.Out, "起動直後に入力待ちになっている。pane を開いて応答する")
	}
	return nil
}
