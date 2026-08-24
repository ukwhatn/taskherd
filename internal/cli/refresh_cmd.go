package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/ukwhatn/taskherd/internal/fetch"
	"github.com/ukwhatn/taskherd/internal/model"
)

func (a *app) refreshCmd() *cobra.Command {
	var all bool

	cmd := &cobra.Command{
		Use:   "refresh [<id>]",
		Short: "リンクのライブ取得を即時実行しキャッシュを更新する",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch {
			case len(args) == 0 && !all:
				return &UserError{
					Msg:      "取得対象が指定されていない",
					HintText: "id を指定するか --all を付ける",
				}
			case len(args) > 0 && all:
				return &UserError{
					Msg:      "id と --all は同時に指定できない",
					HintText: "対象を絞るなら id のみ、全体を更新するなら --all のみを指定する",
				}
			}

			var id int
			if len(args) > 0 {
				var err error
				id, err = parseID(args[0])
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

	cmd.Flags().BoolVar(&all, "all", false, "全タスクのリンクを取得する")
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
		failed = append(failed, refreshFailure{URL: outcome.URL, Error: outcome.Err.Error()})
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

	fmt.Fprintf(a.env.Out, "%d 件取得した", len(updated))
	if len(failed) > 0 {
		fmt.Fprintf(a.env.Out, "（%d 件失敗）", len(failed))
	}
	fmt.Fprintln(a.env.Out)
	for _, fail := range failed {
		fmt.Fprintf(a.env.Out, "  - %s: %s\n", fail.URL, fail.Error)
	}
	if result.GitHubInterrupted {
		fmt.Fprintln(a.env.Out, "GitHub のレート制限のため残りの取得を中断した")
	}
	if result.JiraInterrupted {
		fmt.Fprintln(a.env.Out, "Jira のレート制限のため残りの取得を中断した")
	}
	return nil
}
