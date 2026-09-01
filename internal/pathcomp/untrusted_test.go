package pathcomp

import (
	"strings"
	"testing"
)

// Directory names are the one input here that nobody typed. A name carrying an escape sequence
// would reach the terminal intact through whatever draws the suggestions, so it never becomes one.
func TestNamesWithControlCharactersAreNotOffered(t *testing.T) {
	fs := newFakeFS(map[string][]fakeEntry{
		home + "/dev": {
			{name: "ok", dir: true},
			{name: "osc\x1b]52;c;cGF5bG9hZA==\x07", dir: true},
			{name: "two\nlines", dir: true},
			{name: "bell\a", dir: true},
			{name: "bad\xff\xfeutf8", dir: true},
		},
	})
	c := testCompleter(fs)

	got := c.Suggest("~/dev/", 0)
	if len(got.Items) != 1 || got.Items[0] != "~/dev/ok/" || got.Total != 1 {
		t.Fatalf("Suggest = %+v, want ok/ だけ", got)
	}
	for _, item := range got.Items {
		if strings.ContainsAny(item, "\x1b\n\a") {
			t.Errorf("候補 %q に制御文字が残っている", item)
		}
	}
}

// The rejected names must not drag the good ones down with them by becoming the common prefix that
// completion extends to.
func TestCompleteIgnoresNamesWithControlCharacters(t *testing.T) {
	fs := newFakeFS(map[string][]fakeEntry{
		home + "/dev": {
			{name: "tool\x1b[31m", dir: true},
			{name: "toolbox", dir: true},
		},
	})
	c := testCompleter(fs)

	if got, _ := c.Complete("~/dev/too", 0); got != "~/dev/toolbox/" {
		t.Errorf("Complete = %q, want ~/dev/toolbox/（制御文字を含む方は無視）", got)
	}
}

// A bare ~ names the home directory itself. Taken as a prefix instead it would list the home
// directory's siblings — another account's home among them — and complete to none of them.
func TestBareTildeLooksInsideTheHomeDirectory(t *testing.T) {
	fs := newFakeFS(map[string][]fakeEntry{
		"/Users": {
			{name: "y", dir: true},
			{name: "yamada", dir: true},
		},
		home: {
			{name: "dev", dir: true},
			{name: "Desktop", dir: true},
		},
	})
	c := testCompleter(fs)

	got := c.Suggest("~", 0)
	if len(got.Items) != 2 || got.Items[0] != "~/Desktop/" || got.Items[1] != "~/dev/" {
		t.Errorf("Suggest = %+v, want ホーム配下", got)
	}

	completed, _ := c.Complete("~", 0)
	if completed != "~/" {
		t.Errorf("Complete = %q, want ~/（ホームへ入る）", completed)
	}
}
