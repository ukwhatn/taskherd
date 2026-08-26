package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ukwhatn/taskherd/internal/config"
	"github.com/ukwhatn/taskherd/internal/i18n"
	"github.com/ukwhatn/taskherd/internal/model"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("config.toml を書けない: %v", err)
	}
	return path
}

func TestDefaultLanguageIsJapanese(t *testing.T) {
	if got := config.Default().Language; got != string(i18n.LangJA) {
		t.Errorf("language = %q, want ja", got)
	}
}

func TestLoadReadsLanguage(t *testing.T) {
	cfg, err := config.Load(writeConfig(t, "language = \"en\"\n"))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Language != "en" {
		t.Errorf("language = %q, want en", cfg.Language)
	}
}

func TestLoadRejectsUnknownLanguage(t *testing.T) {
	_, err := config.Load(writeConfig(t, "language = \"fr\"\n"))
	if err == nil {
		t.Fatal("err = nil, want 検証エラー")
	}
	var invalid *model.ValidationError
	if !errors.As(err, &invalid) {
		t.Fatalf("err = %T, want *model.ValidationError", err)
	}
	if !strings.Contains(err.Error(), "language") {
		t.Errorf("err = %v, want language を指す", err)
	}
}

// PeekLanguage runs before anything can be rendered, so it has to answer even for a config the real
// Load would reject — and answer "nothing named" rather than fail.
func TestPeekLanguage(t *testing.T) {
	for _, tc := range []struct {
		name    string
		content string
		want    string
	}{
		{"設定済み", "language = \"en\"\n", "en"},
		{"未設定", "editor = \"nano\"\n", ""},
		{"他のキーが壊れていても読む", "language = \"en\"\nboard = 1\n", "en"},
		{"構文エラーは空", "language = \n", ""},
		{"検証を通らない値もそのまま返す", "language = \"fr\"\n", "fr"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := config.PeekLanguage(writeConfig(t, tc.content)); got != tc.want {
				t.Errorf("PeekLanguage = %q, want %q", got, tc.want)
			}
		})
	}

	if got := config.PeekLanguage(filepath.Join(t.TempDir(), "absent.toml")); got != "" {
		t.Errorf("存在しないファイルの PeekLanguage = %q, want 空", got)
	}
}

// The generated config must survive its own Load, language included.
func TestDefaultFileContentLoadsWithADefinedLanguage(t *testing.T) {
	cfg, err := config.Load(writeConfig(t, config.DefaultFileContent("ja")))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if _, ok := i18n.Parse(cfg.Language); !ok {
		t.Errorf("language = %q, want 既知の言語", cfg.Language)
	}
}
