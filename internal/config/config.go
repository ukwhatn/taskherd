// Package config loads config.toml and resolves the data and config file paths.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
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
	Editor  string
	Board   Board
	Columns model.Columns
	GitHub  GitHub
	Jira    Jira
}

// Board configures the board TUI and the live fetch cadence.
type Board struct {
	RefreshIntervalMinutes int
	CacheTTLMinutes        int
}

// GitHub configures GitHub and GHES handling.
type GitHub struct {
	GHESHosts []string
}

// Jira configures Jira Cloud. The token is read from the environment variable named by TokenEnv,
// never stored in the file.
type Jira struct {
	Site     string
	Email    string
	TokenEnv string
}

// fileConfig mirrors config.toml. Scalars are pointers so that an explicit 0 is distinguishable
// from an absent key (0 disables background refresh).
type fileConfig struct {
	Editor *string `toml:"editor"`
	Board  struct {
		RefreshIntervalMinutes *int `toml:"refresh_interval_minutes"`
		CacheTTLMinutes        *int `toml:"cache_ttl_minutes"`
	} `toml:"board"`
	Columns []model.Column `toml:"columns"`
	GitHub  struct {
		GHESHosts []string `toml:"ghes_hosts"`
	} `toml:"github"`
	Jira struct {
		Site     *string `toml:"site"`
		Email    *string `toml:"email"`
		TokenEnv *string `toml:"token_env"`
	} `toml:"jira"`
}

// Default returns the settings used when config.toml is absent.
func Default() *Config {
	return &Config{
		Board:   Board{RefreshIntervalMinutes: 10, CacheTTLMinutes: 5},
		Columns: model.DefaultColumns(),
		Jira:    Jira{TokenEnv: "TASKHERD_JIRA_TOKEN"},
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
		return Paths{}, errors.New("HOME が設定されていない。XDG_STATE_HOME と TASKHERD_CONFIG を明示するか HOME を設定する")
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
		return nil, fmt.Errorf("%s を読めない: %w", path, err)
	}

	var raw fileConfig
	if _, err := toml.Decode(string(data), &raw); err != nil {
		return nil, fmt.Errorf("%s を解析できない: %w", path, err)
	}

	cfg := Default()
	if raw.Editor != nil {
		cfg.Editor = *raw.Editor
	}
	if raw.Board.RefreshIntervalMinutes != nil {
		cfg.Board.RefreshIntervalMinutes = *raw.Board.RefreshIntervalMinutes
	}
	if raw.Board.CacheTTLMinutes != nil {
		cfg.Board.CacheTTLMinutes = *raw.Board.CacheTTLMinutes
	}
	if raw.Columns != nil {
		cfg.Columns = raw.Columns
	}
	if raw.GitHub.GHESHosts != nil {
		cfg.GitHub.GHESHosts = raw.GitHub.GHESHosts
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

	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
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
	if c.Board.RefreshIntervalMinutes < 0 {
		violations = append(violations, model.Violation{
			Path:    "board.refresh_interval_minutes",
			Message: fmt.Sprintf("0 以上でなければならない（0 で背景更新を無効化。実際: %d）", c.Board.RefreshIntervalMinutes),
		})
	}
	if c.Board.CacheTTLMinutes < 0 {
		violations = append(violations, model.Violation{
			Path:    "board.cache_ttl_minutes",
			Message: fmt.Sprintf("0 以上でなければならない（実際: %d）", c.Board.CacheTTLMinutes),
		})
	}

	if len(violations) > 0 {
		return &model.ValidationError{Subject: "config.toml", Violations: violations}
	}
	return nil
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
