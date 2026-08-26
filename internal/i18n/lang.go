// Package i18n holds the user-facing text of taskherd in every language it speaks.
//
// Text lives here rather than at the point of use so that a translation is a data change in one
// place, and so that a missing or mis-formatted entry is a test failure rather than a blank line on
// screen (see catalog_test.go). Diagnostics that only a maintainer reads — a failed rename, an
// unparseable JSON body — deliberately stay where they are raised, in English: they are searched
// for verbatim, and a translated one cannot be.
package i18n

import "strings"

// Lang is a language taskherd speaks. The values are what config.toml and TASKHERD_LANG accept.
type Lang string

const (
	LangJA Lang = "ja"
	LangEN Lang = "en"
)

// Default is the language used when nothing names one.
//
// Not derived from LANG: on a developer's machine that variable says whatever the shell was set up
// with years ago (en_US on a machine whose owner reads Japanese is entirely ordinary), so guessing
// from it would silently flip a board that nobody asked to change.
const Default = LangJA

// Env is the environment variable that overrides the configured language for one invocation.
const Env = "TASKHERD_LANG"

// Parse reads a language name, accepting any capitalisation and surrounding space.
func Parse(s string) (Lang, bool) {
	switch Lang(strings.ToLower(strings.TrimSpace(s))) {
	case LangJA:
		return LangJA, true
	case LangEN:
		return LangEN, true
	default:
		return "", false
	}
}

// Names are the accepted language names, for error messages that have to list them.
func Names() []string { return []string{string(LangJA), string(LangEN)} }

// Resolve picks the language: TASKHERD_LANG first, then what config.toml asked for, then the
// default. An unparseable value at either step falls through to the next rather than failing —
// the language is needed to render the complaint, so there is nowhere to report it from yet.
// config.Validate is what rejects a bad config.toml value, in the language resolved here.
func Resolve(getenv func(string) string, configured string) Lang {
	if getenv != nil {
		if lang, ok := Parse(getenv(Env)); ok {
			return lang
		}
	}
	if lang, ok := Parse(configured); ok {
		return lang
	}
	return Default
}

// For returns the catalog of a language. An unknown language gets the default catalog, so a caller
// that skipped Resolve still renders something.
func For(lang Lang) *Catalog {
	switch lang {
	case LangEN:
		return &enCatalog
	case LangJA:
		return &jaCatalog
	default:
		return For(Default)
	}
}
