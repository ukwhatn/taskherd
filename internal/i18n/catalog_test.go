package i18n

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// TestCatalogsComplete is what makes a translation safe to add: every language has to fill in every
// field, and fill it in with the same arguments. Both failures are invisible at runtime — a missing
// entry renders as a blank line, and a mismatched one as %!s(MISSING) buried in a status message —
// so they are caught here instead.
func TestCatalogsComplete(t *testing.T) {
	for _, lang := range Names() {
		catalog := For(Lang(lang))
		walkStrings(t, reflect.ValueOf(*catalog), "", func(path, value string) {
			if strings.TrimSpace(value) == "" {
				t.Errorf("%s: %s が空", lang, path)
			}
		})
	}
}

func TestCatalogsAgreeOnFormatArguments(t *testing.T) {
	ja := map[string]string{}
	walkStrings(t, reflect.ValueOf(jaCatalog), "", func(path, value string) { ja[path] = value })

	walkStrings(t, reflect.ValueOf(enCatalog), "", func(path, value string) {
		want := formatArgs(ja[path])
		got := formatArgs(value)
		if !reflect.DeepEqual(want, got) {
			t.Errorf("%s: 書式引数が一致しない\n  ja: %v (%q)\n  en: %v (%q)", path, want, ja[path], got, value)
		}
	})
}

func TestParse(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want Lang
		ok   bool
	}{
		{"ja", LangJA, true},
		{"EN", LangEN, true},
		{"  ja  ", LangJA, true},
		{"ja_JP.UTF-8", "", false},
		{"", "", false},
	} {
		got, ok := Parse(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("Parse(%q) = %q, %v; want %q, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestResolveOrder(t *testing.T) {
	env := func(vars map[string]string) func(string) string {
		return func(key string) string { return vars[key] }
	}

	for _, tc := range []struct {
		name       string
		vars       map[string]string
		configured string
		want       Lang
	}{
		{"環境変数が config より優先される", map[string]string{Env: "en"}, "ja", LangEN},
		{"環境変数が無ければ config", nil, "en", LangEN},
		{"どちらも無ければ既定", nil, "", Default},
		{"環境変数が不正なら config へ落ちる", map[string]string{Env: "fr"}, "en", LangEN},
		{"config も不正なら既定へ落ちる", map[string]string{Env: "fr"}, "fr", Default},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Resolve(env(tc.vars), tc.configured); got != tc.want {
				t.Errorf("Resolve = %q, want %q", got, tc.want)
			}
		})
	}

	if got := Resolve(nil, "en"); got != LangEN {
		t.Errorf("Resolve(nil, en) = %q, want en", got)
	}
}

func TestForFallsBackToDefault(t *testing.T) {
	if For("fr") != For(Default) {
		t.Error("未知の言語は既定のカタログを返すべき")
	}
}

// walkStrings visits every string field in a catalog, naming it by its path so a failure says
// which entry is wrong rather than only that one is.
func walkStrings(t *testing.T, v reflect.Value, prefix string, fn func(path, value string)) {
	t.Helper()
	typ := v.Type()
	for i := 0; i < v.NumField(); i++ {
		path := typ.Field(i).Name
		if prefix != "" {
			path = prefix + "." + path
		}
		field := v.Field(i)
		switch field.Kind() {
		case reflect.String:
			fn(path, field.String())
		case reflect.Struct:
			walkStrings(t, field, path, fn)
		default:
			t.Fatalf("%s: カタログは string と struct しか持てない（実際: %s）", path, field.Kind())
		}
	}
}

// verbClass groups the verbs that consume the same kind of Go value, so that a translation may
// quote naturally (%q where the other language wrote %s) without that reading as a mismatch. What
// the comparison is actually for is an argument that went missing, was added, or changed type.
func verbClass(verb byte) string {
	switch verb {
	case 's', 'q', 'v', 'w':
		return "value"
	case 'd', 'b', 'o', 'x', 'X', 'c', 'U':
		return "integer"
	case 'f', 'F', 'e', 'E', 'g', 'G':
		return "float"
	default:
		return string(verb)
	}
}

// formatArgs is the argument list a format string implies: position -> what it consumes. Explicit
// indexes (%[2]d) are followed, which is how a translation can reorder arguments without the two
// languages looking different here.
func formatArgs(format string) map[int]string {
	args := map[int]string{}
	next := 1
	for i := 0; i < len(format); i++ {
		if format[i] != '%' || i+1 >= len(format) {
			continue
		}
		i++
		if format[i] == '%' {
			continue
		}
		if format[i] == '[' {
			end := strings.IndexByte(format[i:], ']')
			if end < 0 {
				continue
			}
			var idx int
			if _, err := fmt.Sscanf(format[i+1:i+end], "%d", &idx); err == nil && idx > 0 {
				next = idx
			}
			i += end + 1
		}
		// Skip the flags, width and precision between the verb and what came before it.
		for i < len(format) && strings.IndexByte("+-# 0123456789.", format[i]) >= 0 {
			i++
		}
		if i >= len(format) {
			break
		}
		args[next] = verbClass(format[i])
		next++
	}
	return args
}
