// Package cli defines the taskherd command line interface.
package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/ukwhatn/taskherd/internal/buildinfo"
	"github.com/ukwhatn/taskherd/internal/config"
	"github.com/ukwhatn/taskherd/internal/fetch"
	"github.com/ukwhatn/taskherd/internal/herdrc"
	"github.com/ukwhatn/taskherd/internal/i18n"
	"github.com/ukwhatn/taskherd/internal/model"
	"github.com/ukwhatn/taskherd/internal/pathcomp"
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
	// SessionStartWaitTimeout overrides how long start waits for a session id once the agent is
	// idle (sessionStartWaitTimeout in start_cmd.go). Zero means the production default; the field
	// exists so a test can drive that wait without actually waiting out the real budget, same as Now.
	SessionStartWaitTimeout time.Duration
}

// UserError is an error caused by the invocation itself. Hint tells the user how to fix it.
type UserError struct {
	Msg      string
	HintText string
}

func (e *UserError) Error() string { return e.Msg }

// Hint implements the interface i18n.Message reads advice through.
func (e *UserError) Hint() string { return e.HintText }

type app struct {
	env Env
	// text is the language every command speaks, settled before the command tree is built because
	// cobra evaluates each command's help as it is assembled.
	text    *i18n.Catalog
	jsonOut bool
	// notifyLabel names the operation in a herdr notification raised when the command fails. It is
	// empty for an ordinary invocation, where stderr is being read by whoever typed the command;
	// the board sets it on the launches it detaches, which nobody is watching.
	notifyLabel string
	cfg         *config.Config
	taskStore   *store.Store
	cacheStore  *fetch.Cache
	herdrClient *herdrc.Client
	stdin       *bufio.Reader
}

// Run executes args and returns the process exit code.
func Run(env Env, args []string) int {
	a := &app{env: env, text: i18n.For(resolveLang(env))}
	root := a.rootCmd()
	root.SetArgs(args)
	root.SetOut(env.Out)
	root.SetErr(env.Err)
	root.SetIn(env.In)

	// The notice goes out last whatever happens: an error is what the reader came for, and news
	// about taskherd has no business sitting above it.
	defer a.printUpdateNotice()

	if err := root.Execute(); err != nil {
		// Raised before report() so that both failure shapes — a plain error and a partial result
		// already on stdout — reach the notification through the same path.
		a.notifyError(err)
		// A partialResultError means the command already wrote its (possibly partial) result to
		// stdout: report() would print a second, conflicting thing to stderr on top of it.
		var partial *partialResultError
		if !errors.As(err, &partial) {
			a.report(err)
		}
		return 1
	}
	return 0
}

// printUpdateNotice mentions a newer release on stderr, once a day at most.
//
// stderr rather than stdout: a command's output is something people pipe, and news about taskherd
// is not part of the answer they asked for.
func (a *app) printUpdateNotice() {
	if notice := a.updateNotice(); notice != "" {
		fmt.Fprint(a.env.Err, notice)
	}
}

// resolveLang settles the UI language before anything is rendered.
//
// It reads config.toml itself rather than going through a.config(), because the command tree — help
// text and all — is built before any command has decided it needs the configuration, and a config
// error at this point has no language to be reported in. config.Load reports such errors properly
// when a command actually loads its settings.
func resolveLang(env Env) i18n.Lang {
	return i18n.Resolve(env.Getenv, config.PeekLanguage(env.Paths.ConfigPath))
}

// notifyError announces a failure through herdr when --notify-error named the operation.
//
// This exists for the launches the board detaches: the board quits the moment it hands one off,
// so its status line is gone, and the process that took over writes to a log nobody is watching.
// Without this a launch that stops at a trust-folder prompt looks exactly like one still running.
// Best-effort by design — the same text is already on stderr and in the log.
func (a *app) notifyError(err error) {
	if a.notifyLabel == "" {
		return
	}
	body, hint := i18n.Message(a.text, err)
	if hint != "" {
		body = fmt.Sprintf(a.text.CLI.Root.NotifyBody, body, hint)
	}
	_ = a.herdr().Notify(context.Background(), fmt.Sprintf(a.text.CLI.Root.NotifyTitle, a.notifyLabel), body)
}

func (a *app) rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "taskherd",
		Short: a.text.CLI.Root.Short,
		// --version answers the same thing the version subcommand does, because both spellings
		// are the first thing anyone tries when reporting a problem.
		Version: buildinfo.Get().String(),
		// Errors and usage are rendered by report() so that --json emits JSON only.
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	root.PersistentFlags().BoolVar(&a.jsonOut, "json", false, a.text.CLI.Root.FlagJSON)
	root.PersistentFlags().StringVar(&a.notifyLabel, "notify-error", "", a.text.CLI.Root.FlagNotifyError)
	// Hidden: this is how the board reaches a launch it has already detached from itself, not
	// something to type.
	_ = root.PersistentFlags().MarkHidden("notify-error")

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
		a.startCmd(),
		a.rmCmd(),
		a.configCmd(),
		a.refreshCmd(),
		a.boardCmd(),
		a.pluginCmd(),
		a.pickerCmd(),
		a.versionCmd(),
		a.updateCmd(),
	)
	return root
}

func (a *app) report(err error) {
	text, hint := i18n.Message(a.text, err)

	if a.jsonOut {
		payload := struct {
			Error string `json:"error"`
			Hint  string `json:"hint,omitempty"`
		}{Error: text, Hint: hint}
		data, marshalErr := json.Marshal(payload)
		if marshalErr != nil {
			fmt.Fprintf(a.env.Err, a.text.CLI.Root.ErrorPrefix, text)
			return
		}
		fmt.Fprintln(a.env.Err, string(data))
		return
	}

	fmt.Fprintf(a.env.Err, a.text.CLI.Root.ErrorPrefix, text)
	if hint != "" {
		fmt.Fprintf(a.env.Err, a.text.CLI.Root.HintPrefix, hint)
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

// fetcher builds a Fetcher wired to cfg. The Jira token comes from the environment or from a
// file, never from config.toml itself.
func (a *app) fetcher(cfg *config.Config) *fetch.Fetcher {
	return &fetch.Fetcher{
		GitHub:     fetch.NewGitHubFetcher(cfg.GitHub.Accounts),
		Jira:       fetch.NewJiraFetcher(),
		Cache:      a.cache(),
		Classifier: cfg.Classifier(),
		JiraCreds:  a.jiraCredentials(cfg),
		Now:        a.env.Now,
		Text:       a.text,
	}
}

// jiraCredentials resolves the Jira token: the environment variable named by token_env first, then
// the file named by token_file.
//
// The environment wins so that a one-off `TASKHERD_JIRA_TOKEN=... taskherd refresh` still works,
// and the file is what makes the board work when it is opened as a herdr plugin: that board is
// spawned by the herdr server and inherits its environment, not the shell's.
//
// A file that cannot be read is not an error here. The fetch reports it per link, the same way an
// unreachable GitHub does, so one broken setting does not stop the rest of the board from working.
func (a *app) jiraCredentials(cfg *config.Config) fetch.JiraCredentials {
	creds := fetch.JiraCredentials{Site: cfg.Jira.Site, Email: cfg.Jira.Email}
	if token := strings.TrimSpace(a.env.Getenv(cfg.Jira.TokenEnv)); token != "" {
		creds.Token = token
		return creds
	}
	if cfg.Jira.TokenFile == "" {
		return creds
	}

	data, err := os.ReadFile(a.paths().Expand(cfg.Jira.TokenFile))
	if err != nil {
		// The path is quoted but the file's content never is: this string is written to cache.json
		// and drawn on the board.
		creds.TokenReason = fmt.Sprintf(a.text.CLI.Root.TokenFileUnreadable, cfg.Jira.TokenFile, err)
		return creds
	}
	if creds.Token = strings.TrimSpace(string(data)); creds.Token == "" {
		creds.TokenReason = fmt.Sprintf(a.text.CLI.Root.TokenFileEmpty, cfg.Jira.TokenFile)
	}
	return creds
}

// paths resolves the ~ a hand-written config or a quoted --cwd leaves for the program to expand.
// It reads HOME through the app's own environment rather than os.UserHomeDir so that a test can
// hand it one.
func (a *app) paths() pathcomp.Completer {
	return pathcomp.Completer{Home: a.env.Getenv("HOME")}
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
		return fmt.Errorf("cannot encode the result as JSON: %w", err)
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

func (a *app) parseID(arg string) (int, error) {
	id, err := strconv.Atoi(strings.TrimPrefix(strings.TrimSpace(arg), "#"))
	if err != nil || id < 1 {
		return 0, &UserError{
			Msg:      fmt.Sprintf(a.text.CLI.Root.BadTaskID.Msg, arg),
			HintText: a.text.CLI.Root.BadTaskID.Hint,
		}
	}
	return id, nil
}

func (a *app) requireColumn(cfg *config.Config, status string) error {
	if _, ok := cfg.Columns.Find(status); !ok {
		return &UserError{
			Msg:      fmt.Sprintf(a.text.CLI.Root.UnknownColumn.Msg, status),
			HintText: fmt.Sprintf(a.text.CLI.Root.UnknownColumn.Hint, strings.Join(cfg.Columns.IDs(), ", ")),
		}
	}
	return nil
}

func (a *app) parseDueFlag(raw string) (*model.Date, error) {
	if raw == "" {
		return nil, nil
	}
	due, err := model.ParseDate(raw)
	if err != nil {
		text, _ := i18n.Message(a.text, err)
		return nil, &UserError{Msg: text, HintText: a.text.CLI.Root.BadDueHint}
	}
	return &due, nil
}

func (a *app) parseLinkURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	u, err := url.Parse(trimmed)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", &UserError{
			Msg:      fmt.Sprintf(a.text.CLI.Root.BadURL.Msg, raw),
			HintText: a.text.CLI.Root.BadURL.Hint,
		}
	}
	return trimmed, nil
}
