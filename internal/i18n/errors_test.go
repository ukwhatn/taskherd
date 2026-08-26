package i18n

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// violationCodes is every code the checks can raise. A code missing from this list is not caught
// by TestCatalogsComplete: an entry is a field, but a code is only ever a switch arm.
var violationCodes = []ViolationCode{
	ViolationColumnsEmpty,
	ViolationColumnIDEmpty,
	ViolationColumnIDDuplicate,
	ViolationColumnLabelEmpty,
	ViolationColumnKindInvalid,
	ViolationNextIDTooSmall,
	ViolationTaskIDNotPositive,
	ViolationTaskIDDuplicate,
	ViolationTaskDueFormat,
	ViolationTimestampFormat,
	ViolationIntervalNegative,
	ViolationCacheTTLNegative,
	ViolationIconModeInvalid,
	ViolationLanguageInvalid,
	ViolationAccountIncomplete,
	ViolationAccountKeyFormat,
}

// A code with no arm falls through to printing itself, which reads as a message and is not one.
func TestEveryViolationCodeResolves(t *testing.T) {
	for _, lang := range Names() {
		catalog := For(Lang(lang))
		for _, code := range violationCodes {
			got := catalog.ViolationText(code)
			if got == string(code) {
				t.Errorf("%s: %s に対応する文言が無い", lang, code)
			}
			if strings.TrimSpace(got) == "" {
				t.Errorf("%s: %s の文言が空", lang, code)
			}
		}
	}
}

func TestViolationTextNamesAnUnknownCode(t *testing.T) {
	if got := For(LangJA).ViolationText("not.a.code"); got != "not.a.code" {
		t.Errorf("ViolationText(未知のコード) = %q, want コードそのもの", got)
	}
}

// localizable is the shape every taskherd error type answers Message with.
type localizable struct{ hint string }

func (e *localizable) Error() string { return "raw" }

func (e *localizable) Localize(*Catalog) (string, string) { return "localized", e.hint }

type hinted struct{}

func (e *hinted) Error() string { return "raw" }

func (e *hinted) Hint() string { return "advice" }

func TestMessagePrefersLocalize(t *testing.T) {
	for _, tc := range []struct {
		name     string
		err      error
		wantText string
		wantHint string
	}{
		{"nil はどちらも空", nil, "", ""},
		{"Localizer が最優先", &localizable{hint: "advice"}, "localized", "advice"},
		{"包まれていても届く", fmt.Errorf("wrapped: %w", &localizable{}), "localized", ""},
		{"Hint だけの型は Error() と併せて返る", &hinted{}, "raw", "advice"},
		{"何も実装しない型は Error() のまま", errors.New("plain"), "plain", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			text, hint := Message(For(LangJA), tc.err)
			if text != tc.wantText || hint != tc.wantHint {
				t.Errorf("Message() = (%q, %q), want (%q, %q)", text, hint, tc.wantText, tc.wantHint)
			}
		})
	}
}

func TestMessageFallsBackToTheDefaultCatalog(t *testing.T) {
	err := Errorf(func(t *Catalog) string { return t.Err.Task.TaskNotFound })
	text, _ := Message(nil, err)
	if text != For(Default).Err.Task.TaskNotFound {
		t.Errorf("Message(nil, ...) = %q, want 既定言語の文言", text)
	}
}

// An error raised in one language and read in another has to come out in the reading one; that is
// the whole point of deferring the lookup to display time.
func TestDeferredErrorsFollowTheReader(t *testing.T) {
	err := Errorf(func(t *Catalog) string { return t.Err.Task.BadDate }, "2026/08/26")

	ja, _ := Message(For(LangJA), err)
	en, _ := Message(For(LangEN), err)
	if ja == en {
		t.Errorf("ja と en が同じ (%q)", ja)
	}
	for _, text := range []string{ja, en, err.Error()} {
		if !strings.Contains(text, `"2026/08/26"`) {
			t.Errorf("%q に値が入っていない", text)
		}
	}
}

func TestProblemfCarriesItsHint(t *testing.T) {
	err := Problemf(func(t *Catalog) Problem { return t.Err.Data.NoHome })

	text, hint := Message(For(LangEN), err)
	if text != For(LangEN).Err.Data.NoHome.Msg {
		t.Errorf("text = %q, want %q", text, For(LangEN).Err.Data.NoHome.Msg)
	}
	if hint != For(LangEN).Err.Data.NoHome.Hint {
		t.Errorf("hint = %q, want %q", hint, For(LangEN).Err.Data.NoHome.Hint)
	}
}
