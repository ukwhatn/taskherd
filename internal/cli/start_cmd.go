package cli

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/ukwhatn/taskherd/internal/herdrc"
	"github.com/ukwhatn/taskherd/internal/model"
)

// Session-start stages, reported in startResult.Stage. Each names the last step that completed,
// not the one that was attempted: a stage earlier than the one that failed is what lets a caller
// tell how far the launch got and what is safe to retry (§6 of the design).
const (
	stageStarted  = "started"
	stageWaited   = "waited"
	stageLinked   = "linked"
	stagePrompted = "prompted"
)

// sessionStartWaitTimeout bounds agent wait once StartAgent itself has returned. StartAgent's own
// timeout covers herdr's trust-folder gate; this one covers the shorter settle time between a
// freshly started agent process and herdr's integration hook reporting its session id.
const sessionStartWaitTimeout = 30 * time.Second

// partialResultError signals that RunE already wrote a (possibly partial) result to stdout and
// only the exit code remains open. cli.Run skips report() for this type: printing report()'s own
// {error,hint} envelope on top of an already-emitted object would give a machine reader two
// different things to parse for one invocation.
type partialResultError struct{}

func (e *partialResultError) Error() string {
	return "start が起動を開始した後に失敗した（結果は stdout に出力済み）"
}

// startResult is a session-start attempt's outcome, reported in the same shape whether it fully
// succeeded or stopped partway through: Stage says how far it got, and Error/Hint are empty on
// success. One shape for both means --json output is always a single object regardless of where
// the sequence stopped, and a caller tells success from partial failure by Stage/Linked/PromptSent
// rather than by which stream the output landed on.
type startResult struct {
	TaskID     int    `json:"task_id"`
	Stage      string `json:"stage"`
	PaneID     string `json:"pane_id,omitempty"`
	SessionID  string `json:"session_id,omitempty"`
	Linked     bool   `json:"linked"`
	PromptSent bool   `json:"prompt_sent"`
	Error      string `json:"error,omitempty"`
	Hint       string `json:"hint,omitempty"`
}

func (a *app) startCmd() *cobra.Command {
	var (
		cwdFlag    string
		promptFlag string
	)

	cmd := &cobra.Command{
		Use:   "start <id>",
		Short: "タスクに新しいエージェントセッションを起こす",
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

			cwd, err := a.resolveStartCwd(*f, cwdFlag, cmd.Flags().Changed("cwd"))
			if err != nil {
				return err
			}

			prompt := promptFlag
			if !cmd.Flags().Changed("prompt") {
				cfg, err := a.config()
				if err != nil {
					return err
				}
				prompt = model.RenderPrompt(cfg.SessionStart.TemplateFor(task.Status), *task)
			}

			return a.startSession(cmd.Context(), task, cwd, prompt)
		},
	}

	cmd.Flags().StringVar(&cwdFlag, "cwd", "", "起動する作業ディレクトリ（候補が定まらなければ必須）")
	cmd.Flags().StringVar(&promptFlag, "prompt", "",
		"起動直後に送るプロンプト（省略時は config のテンプレートを使う。空文字を明示すると送らない）")
	return cmd
}

// resolveStartCwd picks the cwd a new session starts in.
//
// An explicit --cwd wins outright, blank or not: a blank one is rejected here rather than treated
// as "not given", since silently falling through to a candidate would start the agent somewhere
// the caller did not ask for. Without --cwd the ranked candidates (frequency, then recency, then
// name — model.RankSessionCwds) decide: none means there is nothing to default to and --cwd is
// required, exactly one is unambiguous and used outright, and several are resolved the same way
// jump resolves an ambiguous session — an explicit flag in --json mode, an interactive pick
// otherwise.
func (a *app) resolveStartCwd(f model.File, flag string, changed bool) (string, error) {
	if changed {
		cwd := strings.TrimSpace(flag)
		if cwd == "" {
			return "", &UserError{
				Msg:      "--cwd が空白だけ",
				HintText: "作業ディレクトリを指定するか、--cwd 自体を省略して候補から選ぶ",
			}
		}
		return cwd, nil
	}

	candidates := model.RankSessionCwds(f)
	switch len(candidates) {
	case 0:
		return "", &UserError{
			Msg:      "cwd の候補が無い（このタスクに紐づくセッションがまだ無い）",
			HintText: "--cwd <path> で作業ディレクトリを指定する",
		}
	case 1:
		return candidates[0], nil
	}

	if a.jsonOut {
		return "", &UserError{
			Msg:      "cwd の候補が複数ある",
			HintText: "--cwd <path> で作業ディレクトリを指定する（候補: " + strings.Join(candidates, ", ") + "）",
		}
	}
	return a.promptStartCwd(candidates)
}

func (a *app) promptStartCwd(candidates []string) (string, error) {
	fmt.Fprintln(a.env.Out, "作業ディレクトリの候補:")
	for i, cwd := range candidates {
		fmt.Fprintf(a.env.Out, "  %d) %s\n", i+1, cwd)
	}

	choice, err := a.readLine("番号か、パスを直接入力")
	if err != nil {
		return "", err
	}
	if index, convErr := strconv.Atoi(choice); convErr == nil {
		if index < 1 || index > len(candidates) {
			return "", &UserError{
				Msg:      fmt.Sprintf("番号が不正: %q", choice),
				HintText: fmt.Sprintf("1〜%d の番号か、パスを直接入力する", len(candidates)),
			}
		}
		return candidates[index-1], nil
	}
	cwd := strings.TrimSpace(choice)
	if cwd == "" {
		return "", &UserError{Msg: "作業ディレクトリが空", HintText: "パスを入力するか --cwd で指定する"}
	}
	return cwd, nil
}

// startSession runs the launch sequence: tab, agent start, wait for a session id, save the link,
// then the prompt. Each step's failure is reported with whatever the result holds so far (§6).
func (a *app) startSession(ctx context.Context, task *model.Task, cwd, prompt string) error {
	client := a.herdr()
	result := startResult{TaskID: task.ID}

	tab, err := client.CreateTab(ctx, herdrc.TabSpec{Cwd: cwd, Label: task.Title})
	if err != nil {
		// Nothing was created: a plain error, not a partial result.
		return err
	}
	result.PaneID = tab.PaneID

	started, err := client.StartAgent(ctx, herdrc.AgentSpec{
		Name:   fmt.Sprintf("taskherd-%d", task.ID),
		Kind:   resumeAgent,
		PaneID: tab.PaneID,
	})
	if err != nil {
		return a.emitStart(result, err, fmt.Sprintf("pane %s を確認する（起動に失敗した）", tab.PaneID))
	}
	result.PaneID = started.PaneID
	result.Stage = stageStarted
	if started.NeedsAttention {
		return a.emitStart(result,
			fmt.Errorf("起動直後に入力待ちになっている（%s）", started.Code),
			fmt.Sprintf("pane %s を開いて応答してから、セッション picker から後で紐づける", started.PaneID))
	}

	agent, err := client.WaitForAgentState(ctx, started.PaneID,
		[]string{herdrc.StateIdle, herdrc.StateBlocked}, sessionStartWaitTimeout)
	if err != nil {
		return a.emitStart(result, err,
			fmt.Sprintf("pane %s を確認し、セッション picker から後で紐づける", started.PaneID))
	}
	sessionID := agent.SessionID()
	if sessionID == "" {
		return a.emitStart(result, errors.New("herdr がセッション id を報告しなかった"),
			fmt.Sprintf("pane %s を確認し、セッション picker から後で紐づける", started.PaneID))
	}
	result.SessionID = sessionID
	result.Stage = stageWaited

	now := a.env.Now()
	err = a.tasks().Update(ctx, func(f *model.File) error {
		t, err := f.Task(task.ID)
		if err != nil {
			return err
		}
		_, err = t.AddSession(model.SessionRef{Agent: resumeAgent, SessionID: sessionID, Cwd: cwd}, now)
		return err
	})
	if err != nil {
		return a.emitStart(result, err,
			fmt.Sprintf("pane %s / session %s を taskherd session link で手動で紐づける", started.PaneID, sessionID))
	}
	result.Linked = true
	result.Stage = stageLinked
	a.stampTaskToken(ctx, started.PaneID, task.ID)

	if prompt != "" {
		if err := client.SendAgentPrompt(ctx, started.PaneID, prompt); err != nil {
			return a.emitStart(result, err, "起動と紐づけは済んでいる。プロンプトの送信だけ失敗した")
		}
		result.PromptSent = true
		result.Stage = stagePrompted
	}

	return a.emitStart(result, nil, "")
}

// emitStart reports a session-start outcome, success or partial failure, in a single --json shape
// (see startResult and partialResultError). Text mode prints the same information as two lines: a
// summary of what exists so far, then the error and its hint when there is one.
func (a *app) emitStart(result startResult, err error, hint string) error {
	if err != nil {
		result.Error, result.Hint = err.Error(), hint
	}

	if a.jsonOut {
		if emitErr := a.emitJSON(result); emitErr != nil {
			return emitErr
		}
		if err != nil {
			return &partialResultError{}
		}
		return nil
	}

	fmt.Fprintf(a.env.Out, "#%d", result.TaskID)
	if result.PaneID != "" {
		fmt.Fprintf(a.env.Out, " pane %s", result.PaneID)
	}
	if result.SessionID != "" {
		fmt.Fprintf(a.env.Out, " session %s", result.SessionID)
	}
	switch {
	case result.PromptSent:
		fmt.Fprintln(a.env.Out, " まで起動した（プロンプト送信済み）")
	case result.Linked:
		fmt.Fprintln(a.env.Out, " まで起動した（紐づけ済み、プロンプトは送っていない）")
	default:
		fmt.Fprintln(a.env.Out)
	}
	if err == nil {
		return nil
	}
	fmt.Fprintf(a.env.Err, "エラー: %v\n", err)
	if hint != "" {
		fmt.Fprintf(a.env.Err, "ヒント: %s\n", hint)
	}
	return &partialResultError{}
}
