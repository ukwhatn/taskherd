package config

import "github.com/ukwhatn/taskherd/internal/i18n"

// promptTemplate returns the built-in [session_start] prompt_template for lang.
func promptTemplate(lang string) string {
	if parsed, ok := i18n.Parse(lang); ok && parsed == i18n.LangEN {
		return promptTemplateEN
	}
	return promptTemplateJA
}

// DefaultFileContent returns the config.toml that config init writes for lang. A language nothing
// recognises gets the default one, which is what the rest of the program falls back to as well.
func DefaultFileContent(lang string) string {
	if parsed, ok := i18n.Parse(lang); ok && parsed == i18n.LangEN {
		return fileContentEN
	}
	return fileContentJA
}
