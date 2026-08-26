package cli_test

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/ukwhatn/taskherd/internal/config"
	"github.com/ukwhatn/taskherd/internal/i18n"
)

// enCatalog is the English wording the assertions here read from. It is named apart from the ja
// used elsewhere in this package so both can be asserted against in one test.
var enCatalog = i18n.For(i18n.LangEN)

// The errors raised deep in the stack — a sentinel from model, a validation failure from config —
// are the ones a language switch is easiest to miss, because nothing near the command mentions them.
func TestErrorsSpeakTheConfiguredLanguage(t *testing.T) {
	for _, tc := range []struct {
		lang string
		want string
	}{
		{"ja", ja.Err.Task.TaskNotFound},
		{"en", enCatalog.Err.Task.TaskNotFound},
	} {
		t.Run(tc.lang, func(t *testing.T) {
			h := newHarness(t)
			h.writeConfig(t, fmt.Sprintf("language = %q\n", tc.lang))

			res := h.run(t, "show", "99")

			if res.code == 0 {
				t.Fatalf("show 99 が成功している: %s", res.stdout)
			}
			if !strings.Contains(res.stderr, tc.want) {
				t.Errorf("stderr に %q が無い:\n%s", tc.want, res.stderr)
			}
			if !strings.Contains(res.stderr, "#99") {
				t.Errorf("stderr が対象の id に触れていない:\n%s", res.stderr)
			}
		})
	}
}

// TASKHERD_LANG has to reach an error too, not just the help text: it is the switch someone reaches
// for when they need to paste a failure somewhere English-speaking.
func TestErrorsFollowTheEnvironmentOverride(t *testing.T) {
	h := newHarness(t)
	h.writeConfig(t, "language = \"ja\"\n")
	h.env[i18n.Env] = "en"

	res := h.run(t, "show", "99")

	if !strings.Contains(res.stderr, enCatalog.Err.Task.TaskNotFound) {
		t.Errorf("stderr が英語になっていない:\n%s", res.stderr)
	}
}

// --json carries the same text as stderr does, so it has to be localized in the same place.
func TestJSONErrorIsLocalized(t *testing.T) {
	h := newHarness(t)
	h.writeConfig(t, "language = \"en\"\n")

	res := h.run(t, "--json", "show", "99")

	var payload struct {
		Error string `json:"error"`
		Hint  string `json:"hint"`
	}
	if err := json.Unmarshal([]byte(res.stderr), &payload); err != nil {
		t.Fatalf("stderr が JSON ではない (%v):\n%s", err, res.stderr)
	}
	if !strings.Contains(payload.Error, enCatalog.Err.Task.TaskNotFound) {
		t.Errorf("error = %q, want 英語の文言", payload.Error)
	}
}

// A config.toml that fails validation is reported through the language that same file names, which
// is the one case where the language and the thing being rejected come from the same source.
func TestConfigViolationsSpeakTheConfiguredLanguage(t *testing.T) {
	for _, tc := range []struct {
		lang string
		want string
	}{
		{"ja", ja.Err.Data.CacheTTLNegative},
		{"en", enCatalog.Err.Data.CacheTTLNegative},
	} {
		t.Run(tc.lang, func(t *testing.T) {
			h := newHarness(t)
			h.writeConfig(t, fmt.Sprintf("language = %q\n\n[board]\ncache_ttl_minutes = -1\n", tc.lang))

			res := h.run(t, "list")

			want := fmt.Sprintf(tc.want, -1)
			if !strings.Contains(res.stderr, want) {
				t.Errorf("stderr に %q が無い:\n%s", want, res.stderr)
			}
			if !strings.Contains(res.stderr, "board.cache_ttl_minutes") {
				t.Errorf("stderr が違反箇所に触れていない:\n%s", res.stderr)
			}
		})
	}
}

// config init has no config.toml to read the language from, so the environment is the only thing
// that can pick which of the two templates it writes.
func TestConfigInitWritesTheEnvironmentsLanguage(t *testing.T) {
	for _, tc := range []struct {
		env  string
		want string
	}{
		{"", `language = "ja"`},
		{"en", `language = "en"`},
	} {
		t.Run("TASKHERD_LANG="+tc.env, func(t *testing.T) {
			h := newHarness(t)
			if tc.env != "" {
				h.env[i18n.Env] = tc.env
			}

			if res := h.run(t, "config", "init"); res.code != 0 {
				t.Fatalf("config init が失敗した: %s%s", res.stdout, res.stderr)
			}

			data, err := os.ReadFile(h.configPath)
			if err != nil {
				t.Fatalf("生成した config を読めない: %v", err)
			}
			if !strings.Contains(string(data), tc.want) {
				t.Errorf("生成した config に %q が無い:\n%s", tc.want, string(data))
			}
			if _, err := config.Load(h.configPath); err != nil {
				t.Errorf("生成した config が読み込めない: %v", err)
			}
		})
	}
}
