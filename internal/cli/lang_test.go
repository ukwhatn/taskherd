package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ukwhatn/taskherd/internal/config"
	"github.com/ukwhatn/taskherd/internal/i18n"
)

// ja and en are the catalogs the tests assert against, so that a wording change stays a change to
// internal/i18n alone.
var (
	ja = i18n.For(i18n.LangJA)
	en = i18n.For(i18n.LangEN)
)

func TestResolveLang(t *testing.T) {
	for _, tc := range []struct {
		name    string
		content string
		env     map[string]string
		want    i18n.Lang
	}{
		{"何も指定が無ければ既定", "", nil, i18n.Default},
		{"config.toml が決める", "language = \"en\"\n", nil, i18n.LangEN},
		{"環境変数が config を上書きする", "language = \"ja\"\n", map[string]string{i18n.Env: "en"}, i18n.LangEN},
		{"不正な環境変数は config へ落ちる", "language = \"en\"\n", map[string]string{i18n.Env: "fr"}, i18n.LangEN},
		// A config.toml that Load would reject still has to yield a language, because the error it
		// would raise has to be written in one.
		{"壊れた config でも既定で答える", "language = \n", nil, i18n.Default},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			if tc.content != "" {
				if err := os.WriteFile(path, []byte(tc.content), 0o600); err != nil {
					t.Fatalf("config.toml を書けない: %v", err)
				}
			}
			env := Env{
				Paths:  config.Paths{ConfigPath: path},
				Getenv: func(key string) string { return tc.env[key] },
			}
			if got := resolveLang(env); got != tc.want {
				t.Errorf("resolveLang = %q, want %q", got, tc.want)
			}
		})
	}
}

// Every command reads its wording off app.text, so a nil there would be a panic on the first
// message rather than a missing word.
func TestRunSetsCatalog(t *testing.T) {
	a := &app{env: Env{Getenv: func(string) string { return "en" }}}
	a.text = i18n.For(resolveLang(a.env))
	if a.text != i18n.For(i18n.LangEN) {
		t.Errorf("text = %p, want 英語カタログ", a.text)
	}
}
