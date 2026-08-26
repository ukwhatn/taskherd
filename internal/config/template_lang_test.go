package config_test

import (
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/ukwhatn/taskherd/internal/config"
)

// settingLine matches the key of an active (uncommented) setting, and the table headers around it.
var settingLine = regexp.MustCompile(`(?m)^(\[\[?[\w.]+\]\]?|[\w_]+)\s*(=|$)`)

// activeSettings lists the keys a template actually sets, in the order it sets them. Commented-out
// examples are left out: they are prose, and prose is allowed to differ.
func activeSettings(content string) []string {
	var keys []string
	for _, m := range settingLine.FindAllStringSubmatch(content, -1) {
		keys = append(keys, m[1])
	}
	return keys
}

// The two templates are one file in two languages. A setting present in only one of them is
// invisible to half the users, and a default that differs makes config init a lottery.
func TestTemplatesDefineTheSameSettings(t *testing.T) {
	ja := activeSettings(config.DefaultFileContent("ja"))
	en := activeSettings(config.DefaultFileContent("en"))
	if !reflect.DeepEqual(ja, en) {
		t.Errorf("設定キーが一致しない\n  ja: %v\n  en: %v", ja, en)
	}
}

// The commented-out examples are prose, but the keys they show still have to match: one template
// documenting a setting the other hides is the same gap, one level down.
func TestTemplatesShowTheSameExamples(t *testing.T) {
	examples := func(content string) []string {
		var keys []string
		for _, line := range strings.Split(content, "\n") {
			line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "#"))
			for _, m := range settingLine.FindAllStringSubmatch(line, -1) {
				keys = append(keys, m[1])
			}
		}
		sort.Strings(keys)
		return keys
	}
	ja, en := examples(config.DefaultFileContent("ja")), examples(config.DefaultFileContent("en"))
	if !reflect.DeepEqual(ja, en) {
		t.Errorf("コメント例のキーが一致しない\n  ja: %v\n  en: %v", ja, en)
	}
}

// Both templates have to survive the validation the program runs on every load, and to come back
// as the settings they say they are.
func TestBothTemplatesLoadAndValidate(t *testing.T) {
	for _, lang := range []string{"ja", "en"} {
		t.Run(lang, func(t *testing.T) {
			cfg, err := config.Load(writeConfig(t, config.DefaultFileContent(lang)))
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if cfg.Language != lang {
				t.Errorf("Language = %q, want %q", cfg.Language, lang)
			}
			if cfg.SessionStart.PromptTemplate == "" {
				t.Error("prompt_template が空")
			}
		})
	}
}

// A language the config names but the templates have no file for still has to produce one, and the
// default is the only answer that leaves config init working.
func TestUnknownLanguageFallsBackToTheDefaultTemplate(t *testing.T) {
	if got := config.DefaultFileContent("fr"); got != config.DefaultFileContent("ja") {
		t.Error("未知の言語が既定のテンプレートを返していない")
	}
}

// The built-in prompt follows the file's language even when the file does not set one, because a
// config that only says language = "en" should not start sessions in Japanese.
func TestPromptTemplateFollowsTheConfiguredLanguage(t *testing.T) {
	cfg, err := config.Load(writeConfig(t, "language = \"en\"\n"))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !strings.Contains(cfg.SessionStart.PromptTemplate, "Current status:") {
		t.Errorf("prompt_template = %q, want 英語の既定テンプレート", cfg.SessionStart.PromptTemplate)
	}
}
