// Package config は config.toml の読み込みと、データ・設定ファイルのパス解決を担う。
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"github.com/ukwhatn/taskherd/internal/model"
)

const appName = "taskherd"

// Paths は taskherd が使うファイルの置き場所。個別ファイル名は各パッケージが持つ。
type Paths struct {
	StateDir   string
	ConfigPath string
}

// Config は config.toml の内容。
type Config struct {
	Board   Board
	Columns model.Columns
	GitHub  GitHub
	Jira    Jira
}

// Board は board（TUI）とライブ取得の挙動。
type Board struct {
	RefreshIntervalMinutes int
	CacheTTLMinutes        int
}

// GitHub は GitHub / GHES の設定。
type GitHub struct {
	GHESHosts []string
}

// Jira は Jira Cloud の設定。トークンは token_env で指定した環境変数から読む（平文保存はしない）。
type Jira struct {
	Site     string
	Email    string
	TokenEnv string
}

// fileConfig は config.toml の生の形。未指定と 0 を区別するためスカラーはポインタで受ける。
type fileConfig struct {
	Board struct {
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

// Default は config.toml が無いときに使う既定値。
func Default() *Config {
	return &Config{
		Board:   Board{RefreshIntervalMinutes: 10, CacheTTLMinutes: 5},
		Columns: model.DefaultColumns(),
		Jira:    Jira{TokenEnv: "TASKHERD_JIRA_TOKEN"},
	}
}

// ResolvePaths は環境変数からパスを決める。データは XDG_STATE_HOME、config は TASKHERD_CONFIG で上書きできる。
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

// Load は config.toml を読む。ファイルが無ければ既定値を返す（config init を必須にしない）。
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

// Validate は列定義と間隔設定を検証する。
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

// Classifier は URL 種別判別器を返す。
func (c *Config) Classifier() model.URLClassifier {
	return model.URLClassifier{GHESHosts: c.GitHub.GHESHosts, JiraSite: c.Jira.Site}
}

// DefaultStatus は add / board で既定の列 id（先頭列）を返す。
func (c *Config) DefaultStatus() string {
	if len(c.Columns) == 0 {
		return ""
	}
	return c.Columns[0].ID
}
