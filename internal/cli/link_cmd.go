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
		Short: "外部リンクを紐づける（種別は URL から自動判別）",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseID(args[0])
			if err != nil {
				return err
			}
			url, err := parseLinkURL(args[1])
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
			return a.emitTask(updated, fmt.Sprintf("#%d に [%s] %s を紐づけた", updated.ID, kind, url))
		},
	}

	cmd.Flags().StringVar(&note, "note", "", "リンク単位のメモ")
	return cmd
}

func (a *app) unlinkCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unlink <id> <url>",
		Short: "外部リンクの紐づけを外す",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseID(args[0])
			if err != nil {
				return err
			}
			url, err := parseLinkURL(args[1])
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
			return a.emitTask(updated, fmt.Sprintf("#%d から %s の紐づけを外した", updated.ID, url))
		},
	}
}
