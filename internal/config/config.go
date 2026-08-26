// Package config loads config.toml and resolves the data and config file paths.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/ukwhatn/taskherd/internal/i18n"
	"github.com/ukwhatn/taskherd/internal/model"
)

const appName = "taskherd"

// Paths locates the directories taskherd uses; individual file names belong to their own packages.
type Paths struct {
	StateDir   string
	ConfigPath string
}

// Config is the content of config.toml.
type Config struct {
	// Editor is the command note editing opens, taking precedence over the environment.
	Editor string
	// Language is the UI language, "ja" or "en". TASKHERD_LANG overrides it for one invocation.
	//
	// It lives in config.toml rather than only in the environment because a board opened as a herdr
	// plugin is spawned by the long-running herdr server and inherits that server's environment, so
	// a variable exported in a shell never reaches it — the same reason jira.token_file exists.
	Language     string
	Board        Board
	Columns      model.Columns
	GitHub       GitHub
	Jira         Jira
	SessionStart SessionStart
}

// Board configures the board TUI and the live fetch cadence.
type Board struct {
	RefreshIntervalMinutes int
	CacheTTLMinutes        int
	// Icons is the glyph vocabulary: "nerd", "ascii" or "none".
	Icons string
	// Hyperlinks wraps link rows in OSC 8 so a terminal that understands it opens them on a click.
	Hyperlinks bool
}

// GitHub configures GitHub and GHES handling.
type GitHub struct {
	GHESHosts []string
	// Accounts names the gh account to fetch with, so fetching does not depend on which account gh
	// happens to have active. A key is either a host ("github.com") or a host and an owner
	// ("github.com/some-org"); the owner form is what makes a host holding both personal and
	// organization repositories resolvable, since no one account can read both. The value is an
	// account name, never a token.
	Accounts map[string]string
}

// Jira configures Jira Cloud. The token itself is never stored in config.toml: it is read from
// the environment variable named by TokenEnv, or from the file named by TokenFile.
//
// TokenFile exists because the environment is not a path that reaches every way the board starts.
// A board opened as a herdr plugin is spawned by the long-running herdr server, and inherits that
// server's environment rather than the shell's, so a variable exported in a shell never arrives.
// A file is read the same way whichever process starts the board.
type Jira struct {
	Site     string
	Email    string
	TokenEnv string
	// TokenFile is a path to a file holding the token and nothing else. A leading ~/ is expanded
	// against HOME. Surrounding whitespace and the trailing newline are stripped when read.
	TokenFile string
}

// SessionStart configures the prompt a session started from a task opens with.
type SessionStart struct {
	// PromptTemplate is used when a task's status has no entry in Templates.
	PromptTemplate string
	// Templates overrides PromptTemplate per column id, for columns whose work starts differently.
	Templates map[string]string
}

// TemplateFor resolves the template for a column: the column's own override when Templates names
// it, falling back to PromptTemplate otherwise.
//
// Neither side falls back again on an empty string. prompt_template = "" and a per-column entry
// set to "" both mean "launch without sending a prompt" (a session-start config decision, not a
// board one), and treating an explicit empty as "unset" here would resurrect the built-in default
// a config author asked to suppress. The built-in default itself is injected by Default() and
// Load(), never here: TemplateFor only resolves between what the config actually holds.
func (s SessionStart) TemplateFor(status string) string {
	if tmpl, ok := s.Templates[status]; ok {
		return tmpl
	}
	return s.PromptTemplate
}

// fileConfig mirrors config.toml. Scalars are pointers so that an explicit 0 is distinguishable
// from an absent key (0 disables background refresh).
type fileConfig struct {
	Editor   *string `toml:"editor"`
	Language *string `toml:"language"`
	Board    struct {
		RefreshIntervalMinutes *int    `toml:"refresh_interval_minutes"`
		CacheTTLMinutes        *int    `toml:"cache_ttl_minutes"`
		Icons                  *string `toml:"icons"`
		Hyperlinks             *bool   `toml:"hyperlinks"`
	} `toml:"board"`
	Columns []model.Column `toml:"columns"`
	GitHub  struct {
		GHESHosts []string          `toml:"ghes_hosts"`
		Accounts  map[string]string `toml:"accounts"`
	} `toml:"github"`
	Jira struct {
		Site      *string `toml:"site"`
		Email     *string `toml:"email"`
		TokenEnv  *string `toml:"token_env"`
		TokenFile *string `toml:"token_file"`
	} `toml:"jira"`
	SessionStart struct {
		// PromptTemplate is a pointer so that an explicit "" (send no prompt) is distinguishable
		// from the key being absent (use the built-in default).
		PromptTemplate *string           `toml:"prompt_template"`
		Templates      map[string]string `toml:"templates"`
	} `toml:"session_start"`
}

// validIconModes are the glyph vocabularies board.icons may name. The list is repeated here rather
// than imported from the TUI so that config stays below the UI in the dependency order; tui.Icons
// is what turns the value into an actual icon set.
var validIconModes = map[string]bool{"nerd": true, "ascii": true, "none": true}

// Default returns the settings used when config.toml is absent.
func Default() *Config {
	return &Config{
		Language:     string(i18n.Default),
		Board:        Board{RefreshIntervalMinutes: 10, CacheTTLMinutes: 5, Icons: "nerd", Hyperlinks: true},
		Columns:      model.DefaultColumns(),
		Jira:         Jira{TokenEnv: "TASKHERD_JIRA_TOKEN"},
		SessionStart: SessionStart{PromptTemplate: promptTemplate(string(i18n.Default))},
	}
}

// ResolvePaths derives the paths from the environment: XDG_STATE_HOME for data, TASKHERD_CONFIG for the config file.
func ResolvePaths(getenv func(string) string) (Paths, error) {
	var paths Paths

	if stateHome := getenv("XDG_STATE_HOME"); filepath.IsAbs(stateHome) {
		paths.StateDir = filepath.Join(stateHome, appName)
	}
	if configPath := getenv("TASKHERD_CONFIG"); configPath != "" {
		paths.ConfigPath = configPath
	}
	if paths.StateDir != "" && paths.ConfigPath != "" {
		return paths, nil
	}

	home := getenv("HOME")
	if home == "" {
		return Paths{}, i18n.Problemf(func(t *i18n.Catalog) i18n.Problem { return t.Err.Data.NoHome })
	}
	if paths.StateDir == "" {
		paths.StateDir = filepath.Join(home, ".local", "state", appName)
	}
	if paths.ConfigPath == "" {
		paths.ConfigPath = filepath.Join(home, ".config", appName, "config.toml")
	}
	return paths, nil
}

// Load reads config.toml, falling back to defaults when it does not exist so config init stays optional.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Default(), nil
	}
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", path, err)
	}

	var raw fileConfig
	if _, err := toml.Decode(string(data), &raw); err != nil {
		return nil, fmt.Errorf("cannot parse %s: %w", path, err)
	}

	cfg := Default()
	if raw.Editor != nil {
		cfg.Editor = *raw.Editor
	}
	if raw.Language != nil {
		cfg.Language = *raw.Language
	}
	if raw.Board.RefreshIntervalMinutes != nil {
		cfg.Board.RefreshIntervalMinutes = *raw.Board.RefreshIntervalMinutes
	}
	if raw.Board.CacheTTLMinutes != nil {
		cfg.Board.CacheTTLMinutes = *raw.Board.CacheTTLMinutes
	}
	if raw.Board.Icons != nil {
		cfg.Board.Icons = *raw.Board.Icons
	}
	if raw.Board.Hyperlinks != nil {
		cfg.Board.Hyperlinks = *raw.Board.Hyperlinks
	}
	if raw.Columns != nil {
		cfg.Columns = raw.Columns
	}
	if raw.GitHub.GHESHosts != nil {
		cfg.GitHub.GHESHosts = raw.GitHub.GHESHosts
	}
	if raw.GitHub.Accounts != nil {
		cfg.GitHub.Accounts = raw.GitHub.Accounts
	}
	if raw.Jira.Site != nil {
		cfg.Jira.Site = *raw.Jira.Site
	}
	if raw.Jira.Email != nil {
		cfg.Jira.Email = *raw.Jira.Email
	}
	if raw.Jira.TokenEnv != nil {
		cfg.Jira.TokenEnv = *raw.Jira.TokenEnv
	}
	if raw.Jira.TokenFile != nil {
		cfg.Jira.TokenFile = *raw.Jira.TokenFile
	}
	if raw.SessionStart.PromptTemplate != nil {
		cfg.SessionStart.PromptTemplate = *raw.SessionStart.PromptTemplate
	} else {
		// The built-in prompt is the one thing in Default() that the file's own language changes,
		// so it is settled here rather than there: a config that only says language = "en" should
		// not start sessions with a Japanese prompt.
		cfg.SessionStart.PromptTemplate = promptTemplate(cfg.Language)
	}
	cfg.SessionStart.Templates = raw.SessionStart.Templates

	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// PeekLanguage reads the language setting out of config.toml and nothing else.
//
// This exists because the language has to be settled before anything can be rendered — cobra
// builds every command's help text as the command tree is assembled, at the very top of a run —
// while the real Load happens later, once a command knows it needs the rest of the settings.
// Every failure here is swallowed on purpose: a missing file, a syntax error and an unknown key
// all mean "no language was named", and reporting them would need the very language this is
// resolving. Load reports them properly when the command actually reads its config.
func PeekLanguage(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var raw struct {
		Language string `toml:"language"`
	}
	if _, err := toml.Decode(string(data), &raw); err != nil {
		return ""
	}
	return raw.Language
}

// Validate checks the column definitions and the interval settings.
func (c *Config) Validate() error {
	var violations []model.Violation

	if err := c.Columns.Validate(); err != nil {
		var invalid *model.ValidationError
		if !errors.As(err, &invalid) {
			return err
		}
		violations = append(violations, invalid.Violations...)
	}
	add := func(path string, code i18n.ViolationCode, args ...any) {
		violations = append(violations, model.Violation{Path: path, Code: code, Args: args})
	}

	if _, ok := i18n.Parse(c.Language); !ok {
		add("language", i18n.ViolationLanguageInvalid, strings.Join(i18n.Names(), " / "), c.Language)
	}
	if c.Board.RefreshIntervalMinutes < 0 {
		add("board.refresh_interval_minutes", i18n.ViolationIntervalNegative, c.Board.RefreshIntervalMinutes)
	}
	if c.Board.CacheTTLMinutes < 0 {
		add("board.cache_ttl_minutes", i18n.ViolationCacheTTLNegative, c.Board.CacheTTLMinutes)
	}
	if !validIconModes[c.Board.Icons] {
		add("board.icons", i18n.ViolationIconModeInvalid, c.Board.Icons)
	}
	for key, account := range c.GitHub.Accounts {
		if strings.TrimSpace(key) == "" || strings.TrimSpace(account) == "" {
			add("github.accounts", i18n.ViolationAccountIncomplete, key, account)
			continue
		}
		if !validAccountKey(key) {
			add("github.accounts", i18n.ViolationAccountKeyFormat, key)
		}
	}

	if len(violations) > 0 {
		return &model.ValidationError{Subject: "config.toml", Violations: violations}
	}
	return nil
}

// validAccountKey reports whether a [github.accounts] key names a host, or a host and one owner.
//
// A deeper path is rejected rather than ignored: a key written as "<host>/<owner>/<repo>" would
// silently match nothing, and the whole point of the setting is that a link is fetched with the
// account the user named for it.
func validAccountKey(key string) bool {
	parts := strings.Split(strings.TrimSuffix(strings.TrimSpace(key), "/"), "/")
	if len(parts) > 2 {
		return false
	}
	for _, part := range parts {
		if strings.TrimSpace(part) == "" {
			return false
		}
	}
	return true
}

// ResolveEditor is the command note editing opens, in the order the answer is looked for: the
// config's own setting, then VISUAL, then EDITOR.
//
// Config comes first because a pane the herdr plugin starts does not go through a login shell, so
// VISUAL and EDITOR may never reach it; naming the editor in config is what makes note editing
// work there at all. An empty answer means no editor is configured anywhere.
func (c *Config) ResolveEditor(getenv func(string) string) string {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	for _, candidate := range []string{c.Editor, getenv("VISUAL"), getenv("EDITOR")} {
		if editor := strings.TrimSpace(candidate); editor != "" {
			return editor
		}
	}
	return ""
}

// Classifier returns the link classifier built from the configured hosts.
func (c *Config) Classifier() model.URLClassifier {
	return model.URLClassifier{GHESHosts: c.GitHub.GHESHosts, JiraSite: c.Jira.Site}
}

// DefaultStatus returns the column id used when none is given: the first column.
func (c *Config) DefaultStatus() string {
	if len(c.Columns) == 0 {
		return ""
	}
	return c.Columns[0].ID
}
