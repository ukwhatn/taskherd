package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ukwhatn/taskherd/internal/config"
	"github.com/ukwhatn/taskherd/internal/model"
)

func envFunc(pairs map[string]string) func(string) string {
	return func(key string) string { return pairs[key] }
}

func TestResolvePaths(t *testing.T) {
	tests := []struct {
		name           string
		env            map[string]string
		wantStateDir   string
		wantConfigPath string
		wantErr        bool
	}{
		{
			name:           "既定は HOME 配下",
			env:            map[string]string{"HOME": "/home/u"},
			wantStateDir:   "/home/u/.local/state/taskherd",
			wantConfigPath: "/home/u/.config/taskherd/config.toml",
		},
		{
			name:           "XDG_STATE_HOME を優先する",
			env:            map[string]string{"HOME": "/home/u", "XDG_STATE_HOME": "/var/state"},
			wantStateDir:   "/var/state/taskherd",
			wantConfigPath: "/home/u/.config/taskherd/config.toml",
		},
		{
			name:           "相対パスの XDG_STATE_HOME は無視する",
			env:            map[string]string{"HOME": "/home/u", "XDG_STATE_HOME": "state"},
			wantStateDir:   "/home/u/.local/state/taskherd",
			wantConfigPath: "/home/u/.config/taskherd/config.toml",
		},
		{
			name:           "TASKHERD_CONFIG で config を上書きする",
			env:            map[string]string{"HOME": "/home/u", "TASKHERD_CONFIG": "/etc/taskherd.toml"},
			wantStateDir:   "/home/u/.local/state/taskherd",
			wantConfigPath: "/etc/taskherd.toml",
		},
		{
			name:    "HOME も XDG_STATE_HOME も無ければエラー",
			env:     map[string]string{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			paths, err := config.ResolvePaths(envFunc(tt.env))
			if tt.wantErr {
				if err == nil {
					t.Fatal("ResolvePaths() error = nil, want エラー")
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolvePaths() error = %v", err)
			}
			if paths.StateDir != tt.wantStateDir {
				t.Errorf("StateDir = %q, want %q", paths.StateDir, tt.wantStateDir)
			}
			if paths.ConfigPath != tt.wantConfigPath {
				t.Errorf("ConfigPath = %q, want %q", paths.ConfigPath, tt.wantConfigPath)
			}
		})
	}
}

func TestLoadMissingFileReturnsDefaults(t *testing.T) {
	cfg, err := config.Load(filepath.Join(t.TempDir(), "存在しない.toml"))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !reflect.DeepEqual(cfg, config.Default()) {
		t.Errorf("Load() = %+v, want Default()", cfg)
	}
}

func TestLoadDefaultFileContentMatchesDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(config.DefaultFileContent()), 0o600); err != nil {
		t.Fatalf("config.toml を書けない: %v", err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !reflect.DeepEqual(cfg.Columns, config.Default().Columns) {
		t.Errorf("既定 config ファイルの columns = %+v, want %+v", cfg.Columns, config.Default().Columns)
	}
	if !reflect.DeepEqual(cfg.Board, config.Default().Board) {
		t.Errorf("既定 config ファイルの board = %+v, want %+v", cfg.Board, config.Default().Board)
	}
}

func TestLoadPartialConfigKeepsDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	content := `
[jira]
site = "example.atlassian.net"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("config.toml を書けない: %v", err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !reflect.DeepEqual(cfg.Columns, model.DefaultColumns()) {
		t.Errorf("columns = %+v, want 既定 6 列", cfg.Columns)
	}
	if cfg.Board.RefreshIntervalMinutes != 10 || cfg.Board.CacheTTLMinutes != 5 {
		t.Errorf("board = %+v, want 既定値", cfg.Board)
	}
	if cfg.Jira.Site != "example.atlassian.net" {
		t.Errorf("jira.site = %q", cfg.Jira.Site)
	}
}

func TestLoadZeroRefreshIntervalIsRespected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	content := `
[board]
refresh_interval_minutes = 0
cache_ttl_minutes = 0
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("config.toml を書けない: %v", err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Board.RefreshIntervalMinutes != 0 {
		t.Errorf("refresh_interval_minutes = %d, want 0（明示的な 0 を既定値で上書きしてはならない）", cfg.Board.RefreshIntervalMinutes)
	}
	if cfg.Board.CacheTTLMinutes != 0 {
		t.Errorf("cache_ttl_minutes = %d, want 0", cfg.Board.CacheTTLMinutes)
	}
}

func TestLoadCustomColumnsReplaceDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	content := `
[[columns]]
id = "backlog"
label = "Backlog"
kind = "open"
color = "gray"

[[columns]]
id = "shipped"
label = "Shipped"
kind = "terminal"
color = "purple"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("config.toml を書けない: %v", err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := cfg.Columns.IDs(); !reflect.DeepEqual(got, []string{"backlog", "shipped"}) {
		t.Errorf("columns = %v, want [backlog shipped]（既定列への追加ではなく置換）", got)
	}
}

func TestLoadRejectsInvalidConfig(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		wantPath string
	}{
		{
			name: "列 id の重複",
			content: `
[[columns]]
id = "todo"
label = "ToDo"
kind = "open"

[[columns]]
id = "todo"
label = "やること"
kind = "open"
`,
			wantPath: "columns[1].id",
		},
		{
			name: "列 kind が未知",
			content: `
[[columns]]
id = "todo"
label = "ToDo"
kind = "opened"
`,
			wantPath: "columns[0].kind",
		},
		{
			name: "refresh_interval_minutes が負",
			content: `
[board]
refresh_interval_minutes = -1
`,
			wantPath: "board.refresh_interval_minutes",
		},
		{
			name: "cache_ttl_minutes が負",
			content: `
[board]
cache_ttl_minutes = -5
`,
			wantPath: "board.cache_ttl_minutes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			if err := os.WriteFile(path, []byte(tt.content), 0o600); err != nil {
				t.Fatalf("config.toml を書けない: %v", err)
			}

			_, err := config.Load(path)

			var invalid *model.ValidationError
			if !errors.As(err, &invalid) {
				t.Fatalf("Load() error = %v, want *ValidationError", err)
			}
			found := false
			for _, v := range invalid.Violations {
				if v.Path == tt.wantPath {
					found = true
				}
			}
			if !found {
				t.Errorf("違反 = %v, want %q を含む", invalid.Violations, tt.wantPath)
			}
		})
	}
}

func TestLoadRejectsBrokenTOML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[board\n"), 0o600); err != nil {
		t.Fatalf("config.toml を書けない: %v", err)
	}

	if _, err := config.Load(path); err == nil {
		t.Fatal("Load() error = nil, want TOML パースエラー")
	}
}

func TestClassifierUsesConfiguredHosts(t *testing.T) {
	cfg := config.Default()
	cfg.GitHub.GHESHosts = []string{"github.example.com"}
	cfg.Jira.Site = "example.atlassian.net"

	classifier := cfg.Classifier()

	if got := classifier.Classify("https://github.example.com/o/r/pull/1"); got != model.LinkKindGitHubPR {
		t.Errorf("GHES PR = %q, want github_pr", got)
	}
	if got := classifier.Classify("https://example.atlassian.net/browse/ABC-1"); got != model.LinkKindJira {
		t.Errorf("Jira = %q, want jira", got)
	}
}

func TestLoadReadsEditor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	content := `
editor = "nano"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("config.toml を書けない: %v", err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Editor != "nano" {
		t.Errorf("editor = %q, want nano", cfg.Editor)
	}
}

// The generated config leaves editor commented out: an uncommented value would take precedence
// over the environment, which is not what a user who never set the key asked for.
func TestDefaultFileContentLeavesEditorUnset(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(config.DefaultFileContent()), 0o600); err != nil {
		t.Fatalf("config.toml を書けない: %v", err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Editor != "" {
		t.Errorf("editor = %q, want 空", cfg.Editor)
	}
	if got := cfg.ResolveEditor(envFunc(map[string]string{"EDITOR": "vim"})); got != "vim" {
		t.Errorf("ResolveEditor = %q, want vim（既定 config が環境を上書きしない）", got)
	}
}

func TestResolveEditorOrder(t *testing.T) {
	tests := []struct {
		name   string
		editor string
		env    map[string]string
		want   string
	}{
		{"config が最優先", "nano", map[string]string{"VISUAL": "code -w", "EDITOR": "vim"}, "nano"},
		{"config が無ければ VISUAL", "", map[string]string{"VISUAL": "code -w", "EDITOR": "vim"}, "code -w"},
		{"VISUAL も無ければ EDITOR", "", map[string]string{"EDITOR": "vim"}, "vim"},
		{"空白だけの指定は未設定として扱う", "  ", map[string]string{"EDITOR": "vim"}, "vim"},
		{"どこにも無ければ空", "", map[string]string{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Default()
			cfg.Editor = tt.editor
			if got := cfg.ResolveEditor(envFunc(tt.env)); got != tt.want {
				t.Errorf("ResolveEditor() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLoadReadsIconsAndHyperlinks(t *testing.T) {
	tests := []struct {
		name           string
		content        string
		wantIcons      string
		wantHyperlinks bool
	}{
		{name: "既定", content: "", wantIcons: "nerd", wantHyperlinks: true},
		{name: "ascii へ切替", content: "[board]\nicons = \"ascii\"\n", wantIcons: "ascii", wantHyperlinks: true},
		{name: "ハイパーリンク無効", content: "[board]\nhyperlinks = false\n", wantIcons: "nerd", wantHyperlinks: false},
		{name: "両方指定", content: "[board]\nicons = \"none\"\nhyperlinks = false\n", wantIcons: "none", wantHyperlinks: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			if err := os.WriteFile(path, []byte(tc.content), 0o600); err != nil {
				t.Fatalf("config.toml を書けない: %v", err)
			}
			cfg, err := config.Load(path)
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if cfg.Board.Icons != tc.wantIcons {
				t.Errorf("Icons = %q, want %q", cfg.Board.Icons, tc.wantIcons)
			}
			if cfg.Board.Hyperlinks != tc.wantHyperlinks {
				t.Errorf("Hyperlinks = %v, want %v", cfg.Board.Hyperlinks, tc.wantHyperlinks)
			}
		})
	}
}

func TestLoadRejectsUnknownIconMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[board]\nicons = \"emoji\"\n"), 0o600); err != nil {
		t.Fatalf("config.toml を書けない: %v", err)
	}

	_, err := config.Load(path)
	var invalid *model.ValidationError
	if !errors.As(err, &invalid) {
		t.Fatalf("Load() error = %v, want ValidationError", err)
	}
	if invalid.Violations[0].Path != "board.icons" {
		t.Errorf("Path = %q, want board.icons", invalid.Violations[0].Path)
	}
}

func TestLoadReadsGitHubAccounts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	content := `
[github]
ghes_hosts = ["github.example.com"]

[github.accounts]
"github.com" = "ukwhatn"
"github.example.com" = "work-account"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("config.toml を書けない: %v", err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	want := map[string]string{"github.com": "ukwhatn", "github.example.com": "work-account"}
	if !reflect.DeepEqual(cfg.GitHub.Accounts, want) {
		t.Errorf("Accounts = %+v, want %+v", cfg.GitHub.Accounts, want)
	}
	if len(cfg.GitHub.GHESHosts) != 1 {
		t.Errorf("ghes_hosts = %+v, want 1 件（accounts と共存する）", cfg.GitHub.GHESHosts)
	}
}

func TestLoadRejectsEmptyGitHubAccount(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[github.accounts]\n\"github.com\" = \"\"\n"), 0o600); err != nil {
		t.Fatalf("config.toml を書けない: %v", err)
	}

	_, err := config.Load(path)
	var invalid *model.ValidationError
	if !errors.As(err, &invalid) {
		t.Fatalf("Load() error = %v, want ValidationError", err)
	}
	if invalid.Violations[0].Path != "github.accounts" {
		t.Errorf("Path = %q, want github.accounts", invalid.Violations[0].Path)
	}
}

// The generated config must not carry an account: which account to use is per machine, and a
// value copied out of a template would silently override gh's own resolution.
func TestDefaultFileContentLeavesGitHubAccountsUnset(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(config.DefaultFileContent()), 0o600); err != nil {
		t.Fatalf("config.toml を書けない: %v", err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.GitHub.Accounts) != 0 {
		t.Errorf("Accounts = %+v, want 空", cfg.GitHub.Accounts)
	}
	if !strings.Contains(config.DefaultFileContent(), "[github.accounts]") {
		t.Error("テンプレートに github.accounts の説明が無い")
	}
}
