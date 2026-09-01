// Package pathcomp expands and completes directory paths for the places taskherd asks for one:
// the launch modal's working-directory field, and the paths a config or a flag can write with a
// leading ~.
//
// Only directories are ever offered. Every path taskherd completes is a working directory, and a
// list that also held files would mostly hold things that cannot be answers.
package pathcomp

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Completer expands and completes paths against one home directory and one filesystem.
//
// The zero value works: it reads the real filesystem and leaves ~ alone. ReadDir and Stat are the
// seam tests substitute; Home is separate from them because it also comes from a different place
// depending on the caller (os.UserHomeDir in the board, the environment in the CLI).
type Completer struct {
	// Home is the directory ~ stands for. Empty leaves ~ untouched.
	Home string
	// ReadDir lists a directory. Nil reads the real filesystem.
	ReadDir func(string) ([]os.DirEntry, error)
	// Stat resolves a symlink far enough to tell whether it points at a directory. Nil stats the
	// real filesystem.
	Stat func(string) (os.FileInfo, error)

	// The last directory read, kept so that typing along inside one directory costs a single
	// listing rather than one per keystroke. One entry is enough: completion walks a path
	// left to right, so the directory only changes when a separator is crossed.
	cached        bool
	cachedDir     string
	cachedEntries []os.DirEntry
	cachedOK      bool
}

// Suggestions are the directories an input matches: the ones worth drawing, and how many there
// were in total so a caller can say how many it is not showing.
type Suggestions struct {
	Items []string
	Total int
}

// Reset drops the cached listing. The board calls it when the launch modal opens, which bounds how
// stale a listing can get to one visit to the modal.
func (c *Completer) Reset() {
	c.cached, c.cachedDir, c.cachedEntries, c.cachedOK = false, "", nil, false
}

// Expand resolves a leading ~ against Home. Anything else — including the ~user form, which needs
// a user database rather than an environment variable — is returned as written.
//
// A trailing separator is preserved rather than cleaned away: it is what tells the completion
// apart from a prefix of a name in the parent.
func (c Completer) Expand(input string) string {
	home := c.home()
	if home == "" || !strings.HasPrefix(input, "~") {
		return input
	}
	if input == "~" {
		return home
	}
	if strings.HasPrefix(input, "~/") {
		return home + input[1:]
	}
	return input
}

// Abbreviate is Expand's inverse, for putting a resolved path back into the notation it was
// written in.
func (c Completer) Abbreviate(path string) string {
	home := c.home()
	if home == "" {
		return path
	}
	if path == home {
		return "~"
	}
	if strings.HasPrefix(path, home+"/") {
		return "~" + path[len(home):]
	}
	return path
}

// Suggest lists the directories input matches, at most limit of them (limit <= 0 means all).
func (c *Completer) Suggest(input string, limit int) Suggestions {
	return suggestionsOf(c.matches(input), limit)
}

// Complete extends input as far as its matches agree, and returns them alongside.
//
// The extension is the common prefix of the matched names, and it is only applied when it is
// actually longer than what was typed. That matters because matching ignores case, so a typed
// "de" can match both "Desktop" and "dev", whose common prefix is shorter than the input — there
// the input stands and the list is the answer instead. A lone match gains a trailing separator, so
// pressing the key again walks into it.
func (c *Completer) Complete(input string, limit int) (string, Suggestions) {
	// A bare ~ is answered with ~/ even when nothing inside it agrees on a prefix, so that pressing
	// the key walks into the home directory rather than doing nothing.
	input = forCompletion(input)
	matches := c.matches(input)
	suggestions := suggestionsOf(matches, limit)
	if len(matches) == 0 {
		return input, suggestions
	}
	if len(matches) == 1 {
		return matches[0], suggestions
	}

	common := strings.TrimSuffix(commonPrefix(matches), "/")
	if len(common) > len(input) && strings.HasPrefix(strings.ToLower(common), strings.ToLower(input)) {
		return common, suggestions
	}
	return input, suggestions
}

// matches lists the directories input names, written the way input was written.
func (c *Completer) matches(input string) []string {
	if strings.TrimSpace(input) == "" {
		return nil
	}
	input = forCompletion(input)
	dir, prefix := splitInput(c.Expand(input))
	entries, ok := c.readDir(dir)
	if !ok {
		return nil
	}

	lowered := strings.ToLower(prefix)
	hidden := strings.HasPrefix(prefix, ".")
	var out []string
	for _, entry := range entries {
		name := entry.Name()
		if !displayable(name) {
			continue
		}
		if !strings.HasPrefix(strings.ToLower(name), lowered) {
			continue
		}
		if !hidden && strings.HasPrefix(name, ".") {
			continue
		}
		full := filepath.Join(dir, name)
		if !c.isDir(full, entry) {
			continue
		}
		out = append(out, c.asWritten(input, full)+"/")
	}
	sort.Strings(out)
	return out
}

// displayable reports whether a name can be drawn as text.
//
// Directory names are the one input here that neither taskherd nor the person typing produced:
// they come off the filesystem as arbitrary bytes, and a name carrying an escape sequence would
// reach the terminal intact through whatever draws the suggestions — a terminal renderer measures
// an escape as zero cells and passes it through rather than removing it, so a length check is no
// defence. Such a name cannot be completed; it can still be typed in full.
func displayable(name string) bool {
	if !utf8.ValidString(name) {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// forCompletion is how an input reads while it is being completed rather than submitted. A bare ~
// names the home directory itself — the ~user form is deliberately not resolved, so ~ is not the
// start of any other name — and completing it means looking inside it. Left alone it would be
// taken as a prefix and list the home directory's siblings instead.
func forCompletion(input string) string {
	if input == "~" {
		return "~/"
	}
	return input
}

// splitInput separates the directory to list from the prefix to filter its names by. A path that
// already ends in a separator names the directory itself, with nothing to filter.
func splitInput(path string) (dir, prefix string) {
	if strings.HasSuffix(path, "/") {
		dir = strings.TrimSuffix(path, "/")
		if dir == "" {
			dir = "/"
		}
		return dir, ""
	}
	return filepath.Dir(path), filepath.Base(path)
}

// asWritten puts a resolved path back into input's own notation, so that completing "~/dev/8"
// answers with "~/..." rather than jumping to an absolute path mid-edit.
func (c Completer) asWritten(input, path string) string {
	if strings.HasPrefix(input, "~") {
		return c.Abbreviate(path)
	}
	return path
}

// isDir reports whether the entry can be a working directory. A symlink is only known to be one
// after it has been followed, which costs a stat — paid only for the symlinks themselves.
func (c *Completer) isDir(path string, entry os.DirEntry) bool {
	if entry.IsDir() {
		return true
	}
	if entry.Type()&os.ModeSymlink == 0 {
		return false
	}
	info, err := c.stat(path)
	return err == nil && info.IsDir()
}

// readDir lists dir, reusing the last listing when it is the same directory. A failure is cached
// as a failure: a path being typed spends most of its keystrokes not existing yet, and re-asking
// the filesystem about the same missing directory on each one buys nothing.
func (c *Completer) readDir(dir string) ([]os.DirEntry, bool) {
	if c.cached && c.cachedDir == dir {
		return c.cachedEntries, c.cachedOK
	}
	read := c.ReadDir
	if read == nil {
		read = os.ReadDir
	}
	entries, err := read(dir)
	c.cached, c.cachedDir, c.cachedEntries, c.cachedOK = true, dir, entries, err == nil
	return entries, err == nil
}

func (c *Completer) stat(path string) (os.FileInfo, error) {
	if c.Stat != nil {
		return c.Stat(path)
	}
	return os.Stat(path)
}

func (c Completer) home() string {
	return strings.TrimSuffix(c.Home, "/")
}

func suggestionsOf(matches []string, limit int) Suggestions {
	if limit > 0 && len(matches) > limit {
		return Suggestions{Items: matches[:limit], Total: len(matches)}
	}
	return Suggestions{Items: matches, Total: len(matches)}
}

func commonPrefix(values []string) string {
	prefix := values[0]
	for _, value := range values[1:] {
		if prefix = sharedPrefix(prefix, value); prefix == "" {
			return ""
		}
	}
	return prefix
}

// sharedPrefix is the longest prefix a and b have in common, cut on a rune boundary.
//
// Bytes are the wrong unit here: two directory names can differ only in the last byte of a
// multi-byte character (発 and 登 share their first two), and a prefix cut between them is not a
// string — writing it back into the field would leave half a character in it.
func sharedPrefix(a, b string) string {
	end := 0
	for i, r := range a {
		next := i + utf8.RuneLen(r)
		if next > len(b) || a[i:next] != b[i:next] {
			break
		}
		end = next
	}
	return a[:end]
}
