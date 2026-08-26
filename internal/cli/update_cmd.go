package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/ukwhatn/taskherd/internal/buildinfo"
	"github.com/ukwhatn/taskherd/internal/config"
	"github.com/ukwhatn/taskherd/internal/update"
)

func (a *app) updateCmd() *cobra.Command {
	var checkOnly bool
	var assumeYes bool

	cmd := &cobra.Command{
		Use:   "update",
		Short: a.text.CLI.Update.Short,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runUpdate(cmd, checkOnly, assumeYes)
		},
	}
	cmd.Flags().BoolVar(&checkOnly, "check", false, a.text.CLI.Update.FlagCheck)
	cmd.Flags().BoolVarP(&assumeYes, "yes", "y", false, a.text.CLI.Update.FlagYes)
	return cmd
}

func (a *app) runUpdate(cmd *cobra.Command, checkOnly, assumeYes bool) error {
	text := a.text.CLI.Update
	info := buildinfo.Get()
	if !info.Released() {
		return &UserError{Msg: text.NotReleased.Msg, HintText: text.NotReleased.Hint}
	}

	// Typing `taskherd update` is asking now, so the day-old record is bypassed entirely.
	checker := a.updateChecker()
	tag, err := checker.FetchLatest(cmd.Context())
	if err != nil {
		return &UserError{
			Msg:      fmt.Sprintf(text.Failed.Msg, err),
			HintText: text.Failed.Hint,
		}
	}
	if tag == "" {
		return &UserError{Msg: text.NoRelease.Msg, HintText: text.NoRelease.Hint}
	}

	// The record is refreshed on the way past, so a check made here also settles what the board
	// would otherwise announce.
	state := checker.Load()
	state.CheckedAt = a.env.Now()
	state.LatestTag = tag
	checker.MarkNoticed(state, tag)

	if !update.Newer(info.Version, tag) {
		if a.jsonOut {
			return a.emitUpdateJSON(info.Version, tag, false, "")
		}
		fmt.Fprintf(a.env.Out, text.UpToDate, info.Version)
		return nil
	}

	if checkOnly {
		if a.jsonOut {
			return a.emitUpdateJSON(info.Version, tag, true, "")
		}
		fmt.Fprintf(a.env.Out, text.Available, tag, info.Version)
		return nil
	}

	if !assumeYes && !a.jsonOut {
		fmt.Fprintf(a.env.Out, text.Available, tag, info.Version)
		ok, err := a.confirm(fmt.Sprintf(text.Confirm, tag))
		if err != nil {
			return err
		}
		if !ok {
			fmt.Fprintln(a.env.Out, a.text.CLI.Root.Cancelled)
			return nil
		}
	}

	if !a.jsonOut {
		fmt.Fprintf(a.env.Out, text.Downloading, tag)
	}
	path, err := (&update.Applier{}).Apply(cmd.Context(), tag)
	if err != nil {
		var notWritable *update.NotWritableError
		if errors.As(err, &notWritable) {
			return &UserError{
				Msg:      fmt.Sprintf(text.NotWritable.Msg, notWritable.Dir),
				HintText: text.NotWritable.Hint,
			}
		}
		return &UserError{
			Msg:      fmt.Sprintf(text.Failed.Msg, err),
			HintText: text.Failed.Hint,
		}
	}

	if a.jsonOut {
		return a.emitUpdateJSON(info.Version, tag, true, path)
	}
	fmt.Fprintf(a.env.Out, text.Done, info.Version, tag, path)
	return nil
}

func (a *app) emitUpdateJSON(current, latest string, available bool, installed string) error {
	return a.emitJSON(struct {
		Current   string `json:"current"`
		Latest    string `json:"latest"`
		Available bool   `json:"update_available"`
		Installed string `json:"installed,omitempty"`
	}{Current: current, Latest: latest, Available: available, Installed: installed})
}

// updateChecker is the record of the releases page for this machine's state directory.
func (a *app) updateChecker() *update.Checker {
	return &update.Checker{Dir: a.env.Paths.StateDir, Now: a.env.Now}
}

// updateCheckerFor returns the checker the board should use, or nil when this installation has
// asked not to be checked. Nil is the whole switch: with no checker the board never reaches the
// network, rather than reaching it and discarding the answer.
func (a *app) updateCheckerFor(cfg *config.Config) *update.Checker {
	if !cfg.Update.Check || a.env.Getenv("TASKHERD_NO_UPDATE_CHECK") != "" {
		return nil
	}
	return a.updateChecker()
}

// updateNotice returns the one line to print when a newer release is already known, and marks it
// as told. It never asks the network: whatever the board last recorded is all a short-lived
// command is willing to know.
func (a *app) updateNotice() string {
	if a.jsonOut || a.env.Getenv("TASKHERD_NO_UPDATE_CHECK") != "" {
		return ""
	}
	info := buildinfo.Get()
	if !info.Released() {
		return ""
	}

	checker := a.updateChecker()
	state := checker.Load()
	tag := checker.Notice(state, info.Version)
	if tag == "" {
		return ""
	}
	checker.MarkNoticed(state, tag)
	return fmt.Sprintf(a.text.CLI.Update.Notice, tag, info.Version)
}
