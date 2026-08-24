// Package cli defines the taskherd command line interface.
package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/ukwhatn/taskherd/internal/config"
	"github.com/ukwhatn/taskherd/internal/fetch"
	"github.com/ukwhatn/taskherd/internal/herdrc"
	"github.com/ukwhatn/taskherd/internal/model"
	"github.com/ukwhatn/taskherd/internal/store"
)

// Env carries the process boundaries the CLI depends on, so tests can drive it
// without touching real user data, the wall clock, or the terminal.
type Env struct {
	Paths  config.Paths
	Out    io.Writer
	Err    io.Writer
	In     io.Reader
	Now    func() time.Time
	Getenv func(string) string
	// Herdr overrides the herdr client. When nil it is built from Getenv.
	Herdr *herdrc.Client
}

// UserError is an error caused by the invocation itself. Hint tells the user how to fix it.
type UserError struct {
	Msg      string
	HintText string
}

func (e *UserError) Error() string { return e.Msg }

// Hint implements the hinter interface consumed by the error reporter.
func (e *UserError) Hint() string { return e.HintText }

type hinter interface {
	Hint() string
}

type app struct {
	env         Env
	jsonOut     bool
	cfg         *config.Config
	taskStore   *store.Store
	cacheStore  *fetch.Cache
	herdrClient *herdrc.Client
	stdin       *bufio.Reader
}

// Run executes args and returns the process exit code.
func Run(env Env, args []string) int {
	a := &app{env: env}
	root := a.rootCmd()
	root.SetArgs(args)
	root.SetOut(env.Out)
	root.SetErr(env.Err)
	root.SetIn(env.In)

	if err := root.Execute(); err != nil {
		a.report(err)
		return 1
	}
	return 0
}

func (a *app) rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "taskherd",
		Short: "herdr のエージェントセッション・PR・チケットをタスク単位で束ねるタスク管理ツール",
		// Errors and usage are rendered by report() so that --json emits JSON only.
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	root.PersistentFlags().BoolVar(&a.jsonOut, "json", false, "結果を JSON で stdout に出力する（対話は行わない）")

	root.AddCommand(
		a.addCmd(),
		a.listCmd(),
		a.showCmd(),
		a.editCmd(),
		a.moveCmd(),
		a.doneCmd(),
		a.noteCmd(),
		a.linkCmd(),
		a.unlinkCmd(),
		a.sessionCmd(),
		a.jumpCmd(),
		a.rmCmd(),
		a.configCmd(),
		a.refreshCmd(),
		a.boardCmd(),
		a.pluginCmd(),
		a.pickerCmd(),
	)
	return root
}

func (a *app) report(err error) {
	hint := ""
	var h hinter
	if errors.As(err, &h) {
		hint = h.Hint()
	}

	if a.jsonOut {
		payload := struct {
			Error string `json:"error"`
			Hint  string `json:"hint,omitempty"`
		}{Error: err.Error(), Hint: hint}
		data, marshalErr := json.Marshal(payload)
		if marshalErr != nil {
			fmt.Fprintf(a.env.Err, "エラー: %v\n", err)
			return
		}
		fmt.Fprintln(a.env.Err, string(data))
		return
	}

	fmt.Fprintf(a.env.Err, "エラー: %v\n", err)
	if hint != "" {
		fmt.Fprintf(a.env.Err, "ヒント: %s\n", hint)
	}
}

// config loads config.toml on first use. Commands that only report paths never trigger it,
// so a broken config does not block `taskherd config path`.
func (a *app) config() (*config.Config, error) {
	if a.cfg == nil {
		cfg, err := config.Load(a.env.Paths.ConfigPath)
		if err != nil {
			return nil, err
		}
		a.cfg = cfg
	}
	return a.cfg, nil
}

func (a *app) tasks() *store.Store {
	if a.taskStore == nil {
		a.taskStore = store.New(a.env.Paths.StateDir)
	}
	return a.taskStore
}

// cache owns cache.json's path: config.Paths deliberately has no cache field (PR-1),
// so callers that need the path (config path) go through here rather than duplicating it.
func (a *app) cache() *fetch.Cache {
	if a.cacheStore == nil {
		a.cacheStore = fetch.NewCache(a.env.Paths.StateDir)
	}
	return a.cacheStore
}

// fetcher builds a Fetcher wired to cfg. The Jira token is read from the environment
// variable cfg.Jira.TokenEnv names, never from config.toml itself.
func (a *app) fetcher(cfg *config.Config) *fetch.Fetcher {
	return &fetch.Fetcher{
		GitHub:     fetch.NewGitHubFetcher(cfg.GitHub.Accounts),
		Jira:       fetch.NewJiraFetcher(),
		Cache:      a.cache(),
		Classifier: cfg.Classifier(),
		JiraCreds: fetch.JiraCredentials{
			Site:  cfg.Jira.Site,
			Email: cfg.Jira.Email,
			Token: a.env.Getenv(cfg.Jira.TokenEnv),
		},
		Now: a.env.Now,
	}
}

func (a *app) herdr() *herdrc.Client {
	if a.env.Herdr != nil {
		return a.env.Herdr
	}
	if a.herdrClient == nil {
		a.herdrClient = herdrc.New(herdrc.Options{Getenv: a.env.Getenv})
	}
	return a.herdrClient
}

func (a *app) emitJSON(v any) error {
	enc := json.NewEncoder(a.env.Out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("JSON を出力できない: %w", err)
	}
	return nil
}

// emitTask prints the task as JSON, or textLine when --json is not set.
func (a *app) emitTask(task *model.Task, textLine string) error {
	if a.jsonOut {
		return a.emitJSON(struct {
			Task *model.Task `json:"task"`
		}{Task: task})
	}
	fmt.Fprintln(a.env.Out, textLine)
	return nil
}

func parseID(arg string) (int, error) {
	id, err := strconv.Atoi(strings.TrimPrefix(strings.TrimSpace(arg), "#"))
	if err != nil || id < 1 {
		return 0, &UserError{
			Msg:      fmt.Sprintf("タスク id が不正: %q", arg),
			HintText: "id は正の整数で指定する（#12 表記も可）",
		}
	}
	return id, nil
}

func requireColumn(cfg *config.Config, status string) error {
	if _, ok := cfg.Columns.Find(status); !ok {
		return &UserError{
			Msg:      fmt.Sprintf("未定義の列 id: %q", status),
			HintText: "有効な列 id: " + strings.Join(cfg.Columns.IDs(), ", "),
		}
	}
	return nil
}

func parseDueFlag(raw string) (*model.Date, error) {
	if raw == "" {
		return nil, nil
	}
	due, err := model.ParseDate(raw)
	if err != nil {
		return nil, &UserError{Msg: err.Error(), HintText: "例: --due 2026-08-31"}
	}
	return &due, nil
}

func parseLinkURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	u, err := url.Parse(trimmed)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", &UserError{
			Msg:      fmt.Sprintf("URL が不正: %q", raw),
			HintText: "スキームとホストを含む URL を指定する（例: https://github.com/owner/repo/pull/1）",
		}
	}
	return trimmed, nil
}
