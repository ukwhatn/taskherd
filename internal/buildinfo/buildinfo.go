// Package buildinfo reports which build of taskherd is running.
//
// A released binary carries the answer in its linker flags; anything else has to be reconstructed
// from what the toolchain recorded. Both paths end in the same struct so that nothing downstream
// has to know which kind of build it is talking to — except the updater, which refuses to replace
// a build it cannot name a version for.
package buildinfo

import (
	"runtime"
	"runtime/debug"
	"strings"
)

// DevVersion is what an unreleased build reports. The updater treats it as "not a release" and
// declines to replace it: there is no tag to compare against, and overwriting a binary someone
// just built from a working tree would throw away exactly the thing they were testing.
const DevVersion = "dev"

// Stamped by the release build via -ldflags -X. Empty in every other build.
var (
	Version string
	Commit  string
	Date    string
)

// Info is one build, named.
type Info struct {
	// Version is the release tag without its leading v, or DevVersion.
	Version string
	// Commit is the revision the build came from, empty when nothing recorded it.
	Commit string
	// Date is when the revision was committed, in whatever layout the build stamped.
	Date string
	// Go, OS and Arch describe the toolchain and target, which is what a bug report needs.
	Go   string
	OS   string
	Arch string
}

// Released reports whether this build came from a release tag, and can therefore be compared
// against one.
func (i Info) Released() bool { return i.Version != DevVersion && i.Version != "" }

// Get returns the running build.
//
// The linker flags win when they are there. Otherwise the module's own build info is consulted,
// which is what makes `go install github.com/ukwhatn/taskherd/cmd/taskherd@v1.2.3` report v1.2.3
// instead of nothing. A build from a working tree ends up at DevVersion.
func Get() Info {
	info := Info{
		Version: strings.TrimPrefix(Version, "v"),
		Commit:  Commit,
		Date:    Date,
		Go:      runtime.Version(),
		OS:      runtime.GOOS,
		Arch:    runtime.GOARCH,
	}

	build, ok := debug.ReadBuildInfo()
	if !ok {
		if info.Version == "" {
			info.Version = DevVersion
		}
		return info
	}

	dirty := false
	for _, setting := range build.Settings {
		switch setting.Key {
		case "vcs.revision":
			if info.Commit == "" {
				info.Commit = setting.Value
			}
		case "vcs.time":
			if info.Date == "" {
				info.Date = setting.Value
			}
		case "vcs.modified":
			dirty = setting.Value == "true"
		}
	}

	if info.Version == "" {
		info.Version = moduleVersion(build.Main.Version, dirty)
	}
	return info
}

// moduleVersion reads a release out of what the toolchain recorded, or gives up.
//
// Only a real module version means anything here: `go install ...@v1.2.3` records v1.2.3, and that
// is a tag the updater can compare against. Everything else — "(devel)", a pseudo-version the
// toolchain synthesized from the commit, or any build off a modified tree — describes a binary
// with no counterpart on the releases page.
func moduleVersion(main string, dirty bool) string {
	v := strings.TrimPrefix(main, "v")
	if v == "" || v == "(devel)" || dirty || isPseudoVersion(v) {
		return DevVersion
	}
	return v
}

// isPseudoVersion reports whether v is one of the versions the toolchain makes up for a commit
// that no tag points at. They end in a 14-digit UTC timestamp and a 12-character revision, which
// is the part no hand-written tag has.
//
// This matters because a plain `go build` inside a checkout does not record "(devel)" — it records
// a pseudo-version, which would otherwise read as a release and invite the updater to overwrite
// the very build someone is testing.
func isPseudoVersion(v string) bool {
	dash := strings.LastIndex(v, "-")
	if dash < 0 || len(v)-dash-1 != 12 || !isHex(v[dash+1:]) {
		return false
	}
	// The timestamp sits immediately before the revision, introduced by "-" when no tag precedes
	// the commit (0.0.0-<stamp>-<rev>) and by "." when one does (1.2.4-0.<stamp>-<rev>).
	const stampLen = 14
	rest := v[:dash]
	if len(rest) < stampLen+1 {
		return false
	}
	sep := rest[len(rest)-stampLen-1]
	return (sep == '-' || sep == '.') && isDigits(rest[len(rest)-stampLen:])
}

func isHex(s string) bool {
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f':
		default:
			return false
		}
	}
	return true
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// String is the one-line form: "1.2.3 (abc1234, 2026-08-26T00:00:00Z)", dropping whichever of the
// two details the build did not record.
func (i Info) String() string {
	var b strings.Builder
	b.WriteString(i.Version)

	details := make([]string, 0, 2)
	if i.Commit != "" {
		details = append(details, shortCommit(i.Commit))
	}
	if i.Date != "" {
		details = append(details, i.Date)
	}
	if len(details) > 0 {
		b.WriteString(" (")
		b.WriteString(strings.Join(details, ", "))
		b.WriteString(")")
	}
	return b.String()
}

// shortCommit trims a revision to the length a human reads, leaving anything already short alone.
func shortCommit(commit string) string {
	const short = 7
	if len(commit) <= short {
		return commit
	}
	return commit[:short]
}
