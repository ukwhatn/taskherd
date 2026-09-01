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
	"github.com/ukwhatn/taskherd/internal/i18n"
	"github.com/ukwhatn/taskherd/internal/model"
	"github.com/ukwhatn/taskherd/internal/tui"
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
//
// It still carries the failure's own text and hint. Nothing prints them — the emitted result
// already did — but --notify-error reads them off the returned error like it does for every other
// failure, which is what keeps cli.Run's notification path free of a special case for this one.
type partialResultError struct {
	msg  string
	hint string
	// fallback stands in when msg is empty, which no current caller does; it is here so that the
	// zero value still says something a reader of the log can act on.
	fallback string
}

func (e *partialResultError) Error() string {
	if e.msg == "" {
		return e.fallback
	}
	return e.msg
}

// Hint implements the interface i18n.Message reads advice through.
func (e *partialResultError) Hint() string { return e.hint }

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
	// WorkspaceID is the space the launch landed in, filled the moment the pane exists so that a
	// result reporting a later failure still says where to look.
	WorkspaceID string `json:"workspace_id,omitempty"`
	// Reused reports that no new pane was created: a previous launch's agent was found idle with a
	// session already, and that pane was linked and prompted instead (§4 of the design).
	Reused bool   `json:"reused"`
	Error  string `json:"error,omitempty"`
	Hint   string `json:"hint,omitempty"`
}

// spaceFlags are the --space / --new-space pair, shared by start and jump.
type spaceFlags struct {
	id       string
	newLabel string
}

func (a *app) addSpaceFlags(cmd *cobra.Command, flags *spaceFlags, newSpaceHelp string) {
	cmd.Flags().StringVar(&flags.id, "space", "", a.text.CLI.Start.FlagSpace)
	cmd.Flags().StringVar(&flags.newLabel, "new-space", "", newSpaceHelp)
}

// spaceChoice reads the pair into the launch's target space.
//
// The presence of --new-space is what asks for a new space, not its value: an unlabelled space is
// a space herdr names itself, which is a different request from not asking for one. Giving both
// flags is a contradiction rather than a precedence question, so it is refused instead of ranked.
func (a *app) spaceChoice(cmd *cobra.Command, flags spaceFlags) (tui.SpaceChoice, error) {
	create := cmd.Flags().Changed("new-space")
	id := strings.TrimSpace(flags.id)
	if create && id != "" {
		return tui.SpaceChoice{}, &UserError{
			Msg:      a.text.CLI.Start.SpaceConflict.Msg,
			HintText: a.text.CLI.Start.SpaceConflict.Hint,
		}
	}
	if create {
		return tui.SpaceChoice{Create: true, Label: strings.TrimSpace(flags.newLabel)}, nil
	}
	return tui.SpaceChoice{WorkspaceID: id}, nil
}

func (a *app) startCmd() *cobra.Command {
	var (
		cwdFlag    string
		promptFlag string
		newFlag    bool
		noFocus    bool
		space      spaceFlags
	)

	cmd := &cobra.Command{
		Use:   "start <id>",
		Short: a.text.CLI.Start.Short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := a.parseID(args[0])
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

			cwd, err := a.resolveStartCwd(cmd.Context(), *f, cwdFlag, cmd.Flags().Changed("cwd"), agentNameFor(task.ID))
			if err != nil {
				return err
			}
			target, err := a.spaceChoice(cmd, space)
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

			return a.startSession(cmd.Context(), task, cwd, prompt, target, newFlag, !noFocus)
		},
	}

	cmd.Flags().StringVar(&cwdFlag, "cwd", "", a.text.CLI.Start.FlagCwd)
	cmd.Flags().StringVar(&promptFlag, "prompt", "",
		a.text.CLI.Start.FlagPrompt)
	cmd.Flags().BoolVar(&newFlag, "new", false,
		a.text.CLI.Start.FlagNew)
	cmd.Flags().BoolVar(&noFocus, "no-focus", false,
		a.text.CLI.Start.FlagNoFocus)
	a.addSpaceFlags(cmd, &space, a.text.CLI.Start.FlagNewSpace)
	return cmd
}

// resolveStartCwd picks the cwd a new session starts in.
//
// An explicit --cwd wins outright, blank or not: a blank one is rejected here rather than treated
// as "not given", since silently falling through to a candidate would start the agent somewhere
// the caller did not ask for. Without --cwd the ranked candidates (frequency, then recency, then
// name — model.RankSessionCwds) decide: none normally means there is nothing to default to and
// --cwd is required, but a task with no SessionRef yet has no ranked candidates even when a
// previous attempt's agent is still alive and recoverable (its pane never reached the link step
// that would have recorded one) — recoverableAgentCwd catches exactly that case, since without it
// the launch that findReusableAgent exists to make retriable would instead stop one step earlier on
// an unrelated "no cwd" error, right when a new user hitting a first-run failure (a trust-folder
// prompt, say) needs the retry to just work. Exactly one candidate is unambiguous and used outright,
// and several are resolved the same way jump resolves an ambiguous session — an explicit flag in
// --json mode, an interactive pick otherwise.
func (a *app) resolveStartCwd(ctx context.Context, f model.File, flag string, changed bool, agentName string) (string, error) {
	if changed {
		cwd := strings.TrimSpace(flag)
		if cwd == "" {
			return "", &UserError{
				Msg:      a.text.CLI.Start.BlankCwd.Msg,
				HintText: a.text.CLI.Start.BlankCwd.Hint,
			}
		}
		return a.paths().Expand(cwd), nil
	}

	candidates := model.RankSessionCwds(f)
	switch len(candidates) {
	case 0:
		if cwd, ok := a.recoverableAgentCwd(ctx, agentName); ok {
			return cwd, nil
		}
		return "", &UserError{
			Msg:      a.text.CLI.Start.NoCandidate.Msg,
			HintText: a.text.CLI.Start.NoCandidate.Hint,
		}
	case 1:
		return candidates[0], nil
	}

	if a.jsonOut {
		return "", &UserError{
			Msg:      a.text.CLI.Start.ManyCandidates.Msg,
			HintText: fmt.Sprintf(a.text.CLI.Start.ManyCandidates.Hint, strings.Join(candidates, ", ")),
		}
	}
	return a.promptStartCwd(candidates)
}

// recoverableAgentCwd looks up this task's own previous-attempt agent (see findReusableAgent) just
// far enough to learn where it is running, for resolveStartCwd's no-candidates fallback. Whether it
// is actually reusable in full (not already linked, not stuck) is left to findReusableAgent's own
// check once startSession runs for real — that check needs the very cwd this one exists to supply,
// so it cannot run here first.
//
// ok is false when there is no such agent, herdr could not be asked, or the agent is not usable yet;
// resolveStartCwd's own "no candidates" error is accurate in all three cases.
func (a *app) recoverableAgentCwd(ctx context.Context, agentName string) (string, bool) {
	snapshot, err := a.herdr().Snapshot(ctx)
	if err != nil {
		return "", false
	}
	agent, ok := snapshot.AgentByName(agentName)
	if !ok || !agentIsUsable(agent) {
		return "", false
	}
	return agent.Cwd, true
}

func (a *app) promptStartCwd(candidates []string) (string, error) {
	fmt.Fprintln(a.env.Out, a.text.CLI.Start.CandidatesHeader)
	for i, cwd := range candidates {
		fmt.Fprintf(a.env.Out, "  %d) %s\n", i+1, cwd)
	}

	choice, err := a.readLine(a.text.CLI.Start.ChoosePrompt)
	if err != nil {
		return "", err
	}
	if index, convErr := strconv.Atoi(choice); convErr == nil {
		if index < 1 || index > len(candidates) {
			return "", &UserError{
				Msg:      fmt.Sprintf(a.text.CLI.Start.BadChoice.Msg, choice),
				HintText: fmt.Sprintf(a.text.CLI.Start.BadChoice.Hint, len(candidates)),
			}
		}
		return candidates[index-1], nil
	}
	cwd := strings.TrimSpace(choice)
	if cwd == "" {
		return "", &UserError{Msg: a.text.CLI.Start.EmptyCwd.Msg, HintText: a.text.CLI.Start.EmptyCwd.Hint}
	}
	return cwd, nil
}

// sessionWaitTimeout is the budget WaitForAgentSession gets: sessionStartWaitTimeout, unless a
// test has overridden it through Env.SessionStartWaitTimeout to drive the wait without actually
// waiting out the real budget.
func (a *app) sessionWaitTimeout() time.Duration {
	if a.env.SessionStartWaitTimeout > 0 {
		return a.env.SessionStartWaitTimeout
	}
	return sessionStartWaitTimeout
}

// findReusableAgent looks for a previous launch's agent under this task's own name, before
// anything is created: a pane left over from an earlier attempt must be recovered rather than
// getting a second one piled on top of it (§4 of the design). herdr only ever assigns this name to
// a pane taskherd itself started, so a match can only be this task's own earlier attempt.
//
// nil, nil means no such agent exists and the caller should start fresh — this is also what a
// snapshot fetch failure falls back to, since CreateTab/StartAgent right after would hit the same
// unreachable herdr anyway and are what actually reports it. A non-nil error means an agent does
// exist but launching now would be wrong (already linked to this task, stuck at a different cwd, or
// not usable yet); it is a plain UserError since nothing has been created up to this point.
//
// cwd is the directory this launch is about to use: a recovered pane at a different cwd is not
// reused (the whole point of retrying with a different cwd would silently be thrown away), and it
// is not started fresh either — that would leave two agents under the same name, which is exactly
// what this check exists to prevent. Only --new is allowed to add a second one.
//
// space is treated the same way for the same reason: recovering a pane in one space when the
// caller asked for another would put the session somewhere it did not ask for and say nothing.
// A space the caller left unspecified matches whatever the recovered pane is in, since that is
// still "wherever herdr had it".
func (a *app) findReusableAgent(ctx context.Context, client *herdrc.Client, task *model.Task, agentName, cwd string, space tui.SpaceChoice) (*herdrc.Agent, error) {
	snapshot, err := client.Snapshot(ctx)
	if err != nil {
		return nil, nil
	}
	agent, ok := snapshot.AgentByName(agentName)
	if !ok {
		return nil, nil
	}

	if !agentIsUsable(agent) {
		return nil, &UserError{
			Msg:      fmt.Sprintf(a.text.CLI.Start.ReusableBusy.Msg, task.ID, agent.PaneID),
			HintText: fmt.Sprintf(a.text.CLI.Start.ReusableBusy.Hint, agent.PaneID),
		}
	}
	sessionID := agent.SessionID()
	if _, linked := task.Session(sessionID); linked {
		return nil, &UserError{
			Msg:      fmt.Sprintf(a.text.CLI.Start.AlreadyLinked.Msg, task.ID),
			HintText: fmt.Sprintf(a.text.CLI.Start.AlreadyLinked.Hint, task.ID),
		}
	}
	if strings.TrimSpace(agent.Cwd) != strings.TrimSpace(cwd) {
		return nil, &UserError{
			Msg: fmt.Sprintf(a.text.CLI.Start.OtherCwd.Msg, task.ID, agent.Cwd, agent.PaneID),
			HintText: fmt.Sprintf(
				a.text.CLI.Start.OtherCwd.Hint, agent.PaneID, task.ID, cwd),
		}
	}
	if space.Create || (space.WorkspaceID != "" && space.WorkspaceID != agent.WorkspaceID) {
		return nil, &UserError{
			Msg: fmt.Sprintf(a.text.CLI.Start.OtherSpace.Msg, task.ID, agent.WorkspaceID, agent.PaneID),
			HintText: fmt.Sprintf(
				a.text.CLI.Start.OtherSpace.Hint, agent.PaneID, task.ID),
		}
	}
	return agent, nil
}

// agentNameFor is the herdr agent name a task's launch runs under, shared by resolveStartCwd's
// recovery lookup and startSession's own so both ask about the same agent.
func agentNameFor(taskID int) string {
	return fmt.Sprintf("taskherd-%d", taskID)
}

// createLaunchPane opens the pane a launch will run in, in whichever space was chosen. Both herdr
// calls answer in the same shape, so the rest of the sequence does not branch on which one ran.
//
// A created space carries the label and the new tab inside it takes herdr's own — `workspace
// create` has no separate tab label, and the space is the thing being named.
func createLaunchPane(ctx context.Context, client *herdrc.Client, space tui.SpaceChoice, cwd, label string, focus bool) (herdrc.Tab, error) {
	if space.Create {
		return client.CreateWorkspace(ctx, herdrc.WorkspaceSpec{Cwd: cwd, Label: space.Label, Focus: focus})
	}
	return client.CreateTab(ctx, herdrc.TabSpec{
		WorkspaceID: space.WorkspaceID,
		Cwd:         cwd,
		Label:       label,
		Focus:       focus,
	})
}

// agentIsUsable reports whether agent is far enough along to recover: it has a session id and is
// not stuck waiting for input. blocked never carries a session id either, but naming it separately
// is what lets a caller's own error say which of the two it actually hit.
func agentIsUsable(agent *herdrc.Agent) bool {
	return agent.SessionID() != "" && agent.AgentStatus != herdrc.StateBlocked
}

// nextAgentName returns the smallest -<n> (n >= 2) suffix of agentName not already held by a live
// agent in snapshot. --new is the only caller: the bare name is reserved for the one launch
// findReusableAgent is willing to recover, so an intentional extra one is always numbered, even
// when the bare name happens to be free (§4.3 of the design) — that keeps "the" launch and "an
// additional" one told apart by name regardless of what state the bare one is currently in.
func nextAgentName(snapshot *herdrc.Snapshot, agentName string) string {
	for n := 2; ; n++ {
		candidate := fmt.Sprintf("%s-%d", agentName, n)
		if _, ok := snapshot.AgentByName(candidate); !ok {
			return candidate
		}
	}
}

// startSession runs the launch sequence: recover a previous attempt's agent when one exists,
// otherwise a fresh tab and agent start, then wait for a session id, save the link, then the
// prompt. Each step's failure is reported with whatever the result holds so far (§6).
//
// forceNew (--new) skips the recovery check entirely and always starts a fresh pane, the one way
// to intentionally run a second session for the same task (§4.3).
//
// focus moves the user to the pane as soon as it exists, which is what makes `g` on the board feel
// like the jump it shares a key with. It happens up front rather than at the end: the launch takes
// around half a minute to reach a linked session with a prompt in it, and pulling focus at the end
// of that would yank the user out of whatever they moved on to. --no-focus is for a launch nobody
// is waiting on, started for a task other than the one at hand.
func (a *app) startSession(ctx context.Context, task *model.Task, cwd, prompt string, space tui.SpaceChoice, forceNew, focus bool) error {
	client := a.herdr()
	result := startResult{TaskID: task.ID}
	agentName := agentNameFor(task.ID)

	var reused *herdrc.Agent
	if forceNew {
		if snapshot, err := client.Snapshot(ctx); err == nil {
			agentName = nextAgentName(snapshot, agentName)
		}
		// A snapshot fetch failure here falls back to the bare name: CreateTab/StartAgent right
		// after hit the same unreachable herdr and are what actually report it, same as
		// findReusableAgent's own fallback.
	} else {
		var err error
		reused, err = a.findReusableAgent(ctx, client, task, agentName, cwd, space)
		if err != nil {
			return err
		}
	}

	var paneID, sessionID, linkCwd string
	if reused != nil {
		paneID, sessionID, linkCwd = reused.PaneID, reused.SessionID(), reused.Cwd
		result.PaneID, result.SessionID, result.Stage, result.Reused = paneID, sessionID, stageWaited, true
		result.WorkspaceID = reused.WorkspaceID
		if focus {
			// Best-effort, like every other focus in this file: a recovered pane that cannot be
			// focused is still a pane the launch is about to link and prompt.
			_ = client.FocusAgent(ctx, paneID)
		}
	} else {
		tab, err := createLaunchPane(ctx, client, space, cwd, task.Title, focus)
		if err != nil {
			// Nothing was created: a plain error, not a partial result.
			return err
		}
		result.PaneID, result.WorkspaceID = tab.PaneID, tab.WorkspaceID

		started, err := client.StartAgent(ctx, herdrc.AgentSpec{
			Name:   agentName,
			Kind:   resumeAgent,
			PaneID: tab.PaneID,
		})
		if err != nil {
			return a.emitStart(result, err, fmt.Sprintf(a.text.CLI.Start.StartFailed, tab.PaneID))
		}
		result.PaneID = started.PaneID
		result.Stage = stageStarted
		if started.NeedsAttention {
			return a.emitStart(result,
				fmt.Errorf(a.text.CLI.Start.WaitingInput, started.Code),
				fmt.Sprintf(a.text.CLI.Start.WaitingInputHint, started.PaneID))
		}

		agent, err := client.WaitForAgentSession(ctx, started.PaneID, a.sessionWaitTimeout())
		if err != nil {
			return a.emitStart(result, err,
				fmt.Sprintf(a.text.CLI.Start.CheckPaneHint, started.PaneID))
		}
		sessionID = agent.SessionID()
		if sessionID == "" {
			// blocked is the state an untrusted cwd's agent settles into (herdr is waiting on a
			// trust-folder prompt or similar), and it never carries a session id. Naming that instead
			// of the generic message matters here: it is the single most common way this wait ends
			// without one.
			if agent.AgentStatus == herdrc.StateBlocked {
				return a.emitStart(result, errors.New(a.text.CLI.Start.TrustPrompt),
					fmt.Sprintf(a.text.CLI.Start.CheckPaneHint, started.PaneID))
			}
			return a.emitStart(result, errors.New(a.text.CLI.Start.NoSessionReported),
				fmt.Sprintf(a.text.CLI.Start.CheckPaneHint, started.PaneID))
		}
		paneID, linkCwd = started.PaneID, cwd
		result.SessionID = sessionID
		result.Stage = stageWaited
	}

	now := a.env.Now()
	err := a.tasks().Update(ctx, func(f *model.File) error {
		t, err := f.Task(task.ID)
		if err != nil {
			return err
		}
		_, err = t.AddSession(model.SessionRef{Agent: resumeAgent, SessionID: sessionID, Cwd: linkCwd}, now)
		return err
	})
	if err != nil {
		return a.emitStart(result, err,
			fmt.Sprintf(a.text.CLI.Start.LinkManuallyHint, paneID, sessionID))
	}
	result.Linked = true
	result.Stage = stageLinked
	a.stampTaskToken(ctx, paneID, task.ID, task.Title)

	if prompt != "" {
		if err := client.SendAgentPrompt(ctx, paneID, prompt); err != nil {
			return a.emitStart(result, err, a.text.CLI.Start.PromptFailedHint)
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
	text := ""
	if err != nil {
		text, _ = i18n.Message(a.text, err)
		result.Error, result.Hint = text, hint
	}

	if a.jsonOut {
		if emitErr := a.emitJSON(result); emitErr != nil {
			return emitErr
		}
		if err != nil {
			return &partialResultError{msg: result.Error, hint: result.Hint, fallback: a.text.CLI.Start.PartialLabel}
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
		fmt.Fprintln(a.env.Out, a.text.CLI.Start.DoneWithPrompt)
	case result.Linked:
		fmt.Fprintln(a.env.Out, a.text.CLI.Start.DoneWithoutPrompt)
	default:
		fmt.Fprintln(a.env.Out)
	}
	if err == nil {
		return nil
	}
	fmt.Fprintf(a.env.Err, a.text.CLI.Root.ErrorPrefix, text)
	if hint != "" {
		fmt.Fprintf(a.env.Err, a.text.CLI.Root.HintPrefix, hint)
	}
	return &partialResultError{msg: text, hint: hint, fallback: a.text.CLI.Start.PartialLabel}
}
