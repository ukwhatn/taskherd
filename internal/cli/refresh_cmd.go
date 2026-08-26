package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/ukwhatn/taskherd/internal/fetch"
	"github.com/ukwhatn/taskherd/internal/i18n"
	"github.com/ukwhatn/taskherd/internal/model"
)

func (a *app) refreshCmd() *cobra.Command {
	var all bool

	cmd := &cobra.Command{
		Use:   "refresh [<id>]",
		Short: a.text.CLI.Refresh.Short,
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch {
			case len(args) == 0 && !all:
				return &UserError{
					Msg:      a.text.CLI.Refresh.NoTarget.Msg,
					HintText: a.text.CLI.Refresh.NoTarget.Hint,
				}
			case len(args) > 0 && all:
				return &UserError{
					Msg:      a.text.CLI.Refresh.BothIDAndAll.Msg,
					HintText: a.text.CLI.Refresh.BothIDAndAll.Hint,
				}
			}

			var id int
			if len(args) > 0 {
				var err error
				id, err = a.parseID(args[0])
				if err != nil {
					return err
				}
			}

			cfg, err := a.config()
			if err != nil {
				return err
			}
			f, err := a.tasks().Load()
			if err != nil {
				return err
			}

			var urls []string
			if all {
				urls = collectLinkURLs(f.Tasks)
			} else {
				task, err := f.Task(id)
				if err != nil {
					return err
				}
				urls = linkURLs(task.Links)
			}

			result, err := a.fetcher(cfg).RefreshLinks(cmd.Context(), urls)
			if err != nil {
				return err
			}
			return a.emitRefreshResult(result)
		},
	}

	cmd.Flags().BoolVar(&all, "all", false, a.text.CLI.Refresh.FlagAll)
	return cmd
}

// collectLinkURLs gathers every link URL across tasks, deduplicated: the same URL linked
// from two tasks shares one cache entry and only needs fetching once per cycle.
func collectLinkURLs(tasks []model.Task) []string {
	seen := make(map[string]bool)
	var urls []string
	for _, task := range tasks {
		for _, link := range task.Links {
			if seen[link.URL] {
				continue
			}
			seen[link.URL] = true
			urls = append(urls, link.URL)
		}
	}
	return urls
}

func linkURLs(links []model.Link) []string {
	urls := make([]string, len(links))
	for i, link := range links {
		urls[i] = link.URL
	}
	return urls
}

type refreshFailure struct {
	URL   string `json:"url"`
	Error string `json:"error"`
}

func (a *app) emitRefreshResult(result *fetch.RefreshResult) error {
	updated := []string{}
	failed := []refreshFailure{}
	for _, outcome := range result.Outcomes {
		if outcome.Err == nil {
			updated = append(updated, outcome.URL)
			continue
		}
		text, _ := i18n.Message(a.text, outcome.Err)
		failed = append(failed, refreshFailure{URL: outcome.URL, Error: text})
	}

	if a.jsonOut {
		return a.emitJSON(struct {
			Updated           []string         `json:"updated"`
			Failed            []refreshFailure `json:"failed"`
			GitHubInterrupted bool             `json:"github_interrupted"`
			JiraInterrupted   bool             `json:"jira_interrupted"`
		}{
			Updated:           updated,
			Failed:            failed,
			GitHubInterrupted: result.GitHubInterrupted,
			JiraInterrupted:   result.JiraInterrupted,
		})
	}

	fmt.Fprintf(a.env.Out, a.text.CLI.Refresh.Refreshed, len(updated))
	if len(failed) > 0 {
		fmt.Fprintf(a.env.Out, a.text.CLI.Refresh.FailedSuffix, len(failed))
	}
	fmt.Fprintln(a.env.Out)
	for _, fail := range failed {
		fmt.Fprintf(a.env.Out, "  - %s: %s\n", fail.URL, fail.Error)
	}
	if result.GitHubInterrupted {
		fmt.Fprintln(a.env.Out, a.text.CLI.Refresh.GitHubLimited)
	}
	if result.JiraInterrupted {
		fmt.Fprintln(a.env.Out, a.text.CLI.Refresh.JiraLimited)
	}
	return nil
}
