package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/ukwhatn/taskherd/internal/model"
)

func (a *app) linkCmd() *cobra.Command {
	var note string

	cmd := &cobra.Command{
		Use:   "link <id> <url>",
		Short: a.text.CLI.Link.LinkShort,
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := a.parseID(args[0])
			if err != nil {
				return err
			}
			url, err := a.parseLinkURL(args[1])
			if err != nil {
				return err
			}
			cfg, err := a.config()
			if err != nil {
				return err
			}

			kind := cfg.Classifier().Classify(url)
			now := a.env.Now()
			var updated *model.Task
			err = a.tasks().Update(cmd.Context(), func(f *model.File) error {
				task, err := f.Task(id)
				if err != nil {
					return err
				}
				if _, err := task.AddLink(url, kind, note, now); err != nil {
					return err
				}
				updated = task
				return nil
			})
			if err != nil {
				return err
			}
			return a.emitTask(updated, fmt.Sprintf(a.text.CLI.Link.Linked, updated.ID, kind, url))
		},
	}

	cmd.Flags().StringVar(&note, "note", "", a.text.CLI.Link.FlagNote)
	return cmd
}

func (a *app) unlinkCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unlink <id> <url>",
		Short: a.text.CLI.Link.UnlinkShort,
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := a.parseID(args[0])
			if err != nil {
				return err
			}
			url, err := a.parseLinkURL(args[1])
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
				if _, err := task.RemoveLink(url, now); err != nil {
					return err
				}
				updated = task
				return nil
			})
			if err != nil {
				return err
			}
			return a.emitTask(updated, fmt.Sprintf(a.text.CLI.Link.Unlinked, updated.ID, url))
		},
	}
}
