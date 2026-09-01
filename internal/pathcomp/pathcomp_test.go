package pathcomp

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeEntry is one name in a fake directory. link marks it as a symlink, which is only a
// directory once Stat has followed it.
type fakeEntry struct {
	name  string
	dir   bool
	link  bool
	toDir bool
}

func (e fakeEntry) Name() string { return e.name }
func (e fakeEntry) IsDir() bool  { return e.dir }
func (e fakeEntry) Type() fs.FileMode {
	if e.link {
		return fs.ModeSymlink
	}
	if e.dir {
		return fs.ModeDir
	}
	return 0
}
func (e fakeEntry) Info() (fs.FileInfo, error) { return fakeInfo{e}, nil }

type fakeInfo struct{ entry fakeEntry }

func (i fakeInfo) Name() string       { return i.entry.name }
func (i fakeInfo) Size() int64        { return 0 }
func (i fakeInfo) Mode() fs.FileMode  { return i.entry.Type() }
func (i fakeInfo) ModTime() time.Time { return time.Time{} }
func (i fakeInfo) IsDir() bool        { return i.entry.dir || i.entry.toDir }
func (i fakeInfo) Sys() any           { return nil }

// fakeFS is a directory tree plus a count of how often each directory was read, so a test can see
// the memo working.
type fakeFS struct {
	dirs  map[string][]fakeEntry
	reads map[string]int
	err   error
}

func newFakeFS(dirs map[string][]fakeEntry) *fakeFS {
	return &fakeFS{dirs: dirs, reads: map[string]int{}}
}

func (f *fakeFS) readDir(dir string) ([]os.DirEntry, error) {
	f.reads[dir]++
	if f.err != nil {
		return nil, f.err
	}
	entries, ok := f.dirs[dir]
	if !ok {
		return nil, fs.ErrNotExist
	}
	out := make([]os.DirEntry, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry)
	}
	return out, nil
}

func (f *fakeFS) stat(path string) (os.FileInfo, error) {
	dir, name := filepath.Dir(path), filepath.Base(path)
	for _, entry := range f.dirs[dir] {
		if entry.name == name {
			return fakeInfo{entry}, nil
		}
	}
	return nil, fs.ErrNotExist
}

const home = "/Users/y"

func testCompleter(fs *fakeFS) *Completer {
	return &Completer{Home: home, ReadDir: fs.readDir, Stat: fs.stat}
}

func devFS() *fakeFS {
	return newFakeFS(map[string][]fakeEntry{
		home: {
			{name: "dev", dir: true},
			{name: "Desktop", dir: true},
			{name: ".claude", dir: true},
			{name: ".zshrc"},
			{name: "current", link: true, toDir: true},
			{name: "broken", link: true},
		},
		home + "/dev": {
			{name: "8_tools", dir: true},
			{name: "8_tools_old", dir: true},
			{name: "0_sol", dir: true},
			{name: "notes.md"},
		},
	})
}

func TestExpand(t *testing.T) {
	tests := []struct {
		name, in, want string
		home           string
	}{
		{name: "チルダ単体", in: "~", want: home, home: home},
		{name: "チルダ + スラッシュ", in: "~/", want: home + "/", home: home},
		{name: "チルダ配下", in: "~/dev/8_tools", want: home + "/dev/8_tools", home: home},
		{name: "末尾スラッシュを保つ", in: "~/dev/", want: home + "/dev/", home: home},
		{name: "他人の home は展開しない", in: "~alice/x", want: "~alice/x", home: home},
		{name: "絶対パスはそのまま", in: "/tmp/work", want: "/tmp/work", home: home},
		{name: "相対パスはそのまま", in: "dev", want: "dev", home: home},
		{name: "空", in: "", want: "", home: home},
		{name: "HOME 不明なら展開しない", in: "~/dev", want: "~/dev", home: ""},
		{name: "HOME の末尾スラッシュを重ねない", in: "~/dev", want: home + "/dev", home: home + "/"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := Completer{Home: tc.home}
			if got := c.Expand(tc.in); got != tc.want {
				t.Errorf("Expand(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestAbbreviate(t *testing.T) {
	c := Completer{Home: home}
	tests := []struct{ in, want string }{
		{home, "~"},
		{home + "/dev", "~/dev"},
		{"/Users/yuki/dev", "/Users/yuki/dev"}, // a longer name that merely starts with home
		{"/tmp/work", "/tmp/work"},
	}
	for _, tc := range tests {
		if got := c.Abbreviate(tc.in); got != tc.want {
			t.Errorf("Abbreviate(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSuggest(t *testing.T) {
	tests := []struct {
		name  string
		in    string
		limit int
		want  []string
		total int
	}{
		{name: "空入力では何も出さない", in: "", want: nil},
		{name: "空白だけでも何も出さない", in: "   ", want: nil},
		{
			name: "末尾スラッシュはその配下を並べる", in: "~/dev/",
			want: []string{"~/dev/0_sol/", "~/dev/8_tools/", "~/dev/8_tools_old/"}, total: 3,
		},
		{
			name: "接頭辞で絞る", in: "~/dev/8",
			want: []string{"~/dev/8_tools/", "~/dev/8_tools_old/"}, total: 2,
		},
		{name: "ファイルは候補にしない", in: "~/dev/n", want: nil},
		{
			name: "大文字小文字を無視して照合する", in: "~/de",
			want: []string{"~/Desktop/", "~/dev/"}, total: 2,
		},
		{
			name: "隠しディレクトリは . から打った時だけ出す", in: "~/.",
			want: []string{"~/.claude/"}, total: 1,
		},
		{name: "隠しディレクトリは普段は出さない", in: "~/c", want: []string{"~/current/"}, total: 1},
		{
			name: "絶対パスで打てば絶対パスで返す", in: "/Users/y/dev/0",
			want: []string{"/Users/y/dev/0_sol/"}, total: 1,
		},
		{
			name: "limit を超えたら総数だけ残す", in: "~/dev/", limit: 2,
			want: []string{"~/dev/0_sol/", "~/dev/8_tools/"}, total: 3,
		},
		{name: "存在しないディレクトリ", in: "~/nope/x", want: nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := testCompleter(devFS())
			got := c.Suggest(tc.in, tc.limit)
			if strings.Join(got.Items, ",") != strings.Join(tc.want, ",") {
				t.Errorf("Suggest(%q, %d).Items = %v, want %v", tc.in, tc.limit, got.Items, tc.want)
			}
			if got.Total != tc.total {
				t.Errorf("Suggest(%q, %d).Total = %d, want %d", tc.in, tc.limit, got.Total, tc.total)
			}
		})
	}
}

// A symlink to a directory is a working cwd, and pointing one at the checkout you are on is a
// common enough habit that leaving them out would look like the completion is broken.
func TestSuggestFollowsSymlinksToDirectories(t *testing.T) {
	c := testCompleter(devFS())

	if got := c.Suggest("~/cur", 0).Items; strings.Join(got, ",") != "~/current/" {
		t.Errorf("Items = %v, want ディレクトリへの symlink を含む", got)
	}
	if got := c.Suggest("~/bro", 0).Items; len(got) != 0 {
		t.Errorf("Items = %v, want ディレクトリでない symlink は除く", got)
	}
}

func TestComplete(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{name: "共通接頭辞まで伸ばす", in: "~/dev/8", want: "~/dev/8_tools"},
		{name: "1 件に確定したらスラッシュを付ける", in: "~/dev/0", want: "~/dev/0_sol/"},
		{name: "伸ばせないなら入力を変えない", in: "~/de", want: "~/de"},
		{name: "候補が無ければ入力を変えない", in: "~/zzz", want: "~/zzz"},
		{name: "空入力は何もしない", in: "", want: ""},
		{name: "既に共通接頭辞ちょうどなら変えない", in: "~/dev/8_tools", want: "~/dev/8_tools"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := testCompleter(devFS())
			got, _ := c.Complete(tc.in, 0)
			if got != tc.want {
				t.Errorf("Complete(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// Completing to the one match and then completing again walks into it rather than standing still.
func TestCompleteWalksIntoTheSingleMatch(t *testing.T) {
	c := testCompleter(devFS())

	first, _ := c.Complete("~/dev/0", 0)
	if first != "~/dev/0_sol/" {
		t.Fatalf("1 回目 = %q", first)
	}
	second, sugg := c.Complete(first, 0)
	if second != first {
		t.Errorf("2 回目 = %q, want 変化なし（配下が空）", second)
	}
	if sugg.Total != 0 {
		t.Errorf("Total = %d, want 0", sugg.Total)
	}
}

func TestReadDirErrorsAreSilent(t *testing.T) {
	fs := devFS()
	fs.err = errors.New("読めない")
	c := testCompleter(fs)

	if got := c.Suggest("~/dev/8", 0); len(got.Items) != 0 || got.Total != 0 {
		t.Errorf("Suggest = %+v, want 空", got)
	}
	if got, _ := c.Complete("~/dev/8", 0); got != "~/dev/8" {
		t.Errorf("Complete = %q, want 入力そのまま", got)
	}
}

// Typing within one directory must not re-read it on every keystroke.
func TestReadDirIsMemoisedPerDirectory(t *testing.T) {
	fs := devFS()
	c := testCompleter(fs)

	for _, in := range []string{"~/dev/8", "~/dev/8_", "~/dev/8_t"} {
		c.Suggest(in, 0)
	}
	if got := fs.reads[home+"/dev"]; got != 1 {
		t.Errorf("~/dev の読み取り = %d 回, want 1", got)
	}

	c.Suggest("~/de", 0)
	c.Suggest("~/dev/8", 0)
	if got := fs.reads[home+"/dev"]; got != 2 {
		t.Errorf("別ディレクトリを挟んだあとの読み取り = %d 回, want 2", got)
	}

	c.Reset()
	c.Suggest("~/dev/8", 0)
	if got := fs.reads[home+"/dev"]; got != 3 {
		t.Errorf("Reset 後の読み取り = %d 回, want 3", got)
	}
}

// 発 and 登 share the first two bytes of their UTF-8 encoding, so a byte-wise common prefix would
// hand the field half a character.
func TestCompleteCutsTheCommonPrefixOnARuneBoundary(t *testing.T) {
	c := testCompleter(newFakeFS(map[string][]fakeEntry{
		home: {
			{name: "開発", dir: true},
			{name: "開登", dir: true},
		},
	}))

	got, sugg := c.Complete("~/開", 0)
	if got != "~/開" {
		t.Errorf("Complete = %q, want ~/開（rune の途中で切らない）", got)
	}
	if sugg.Total != 2 {
		t.Errorf("Total = %d, want 2", sugg.Total)
	}
	for _, r := range got {
		if r == '�' {
			t.Fatalf("不正な UTF-8 が入力欄に返った: %q", got)
		}
	}
}

func TestCompleteExtendsThroughMultibyteNames(t *testing.T) {
	c := testCompleter(newFakeFS(map[string][]fakeEntry{
		home: {
			{name: "開発ノート", dir: true},
			{name: "開発ログ", dir: true},
		},
	}))

	if got, _ := c.Complete("~/開", 0); got != "~/開発" {
		t.Errorf("Complete = %q, want ~/開発", got)
	}
}

func TestCompleterUsesTheRealFilesystemByDefault(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "workspace"), 0o755); err != nil {
		t.Fatalf("一時ディレクトリを作れない: %v", err)
	}
	c := &Completer{Home: root}

	got, _ := c.Complete("~/work", 0)
	if got != "~/workspace/" {
		t.Errorf("Complete = %q, want ~/workspace/", got)
	}
}
