package update

import (
	"strconv"
	"strings"
)

// Newer reports whether candidate is a later release than current.
//
// Only the three numbers are compared. A candidate carrying a prerelease suffix is never newer,
// because /releases/latest does not offer prereleases and nothing should be talked into installing
// one; a current version carrying one is treated as its base, so someone running 1.3.0-rc1 is
// offered the finished 1.3.0.
//
// Anything unparsable is not newer. The alternative — guessing — would mean offering to replace a
// working binary on the strength of a string nobody understood.
func Newer(current, candidate string) bool {
	have, ok := parse(current)
	if !ok {
		return false
	}
	want, ok := parse(candidate)
	if !ok || want.prerelease {
		return false
	}
	for i := range want.nums {
		switch {
		case want.nums[i] > have.nums[i]:
			return true
		case want.nums[i] < have.nums[i]:
			return false
		}
	}
	// Equal numbers: the finished release beats the prerelease that led to it.
	return have.prerelease
}

type version struct {
	nums       [3]int
	prerelease bool
}

// parse reads "v1.2.3", "1.2.3" or "1.2.3-rc1". Build metadata after "+" is dropped, as it carries
// no ordering.
func parse(s string) (version, bool) {
	s = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), "v"))
	if s == "" {
		return version{}, false
	}
	if plus := strings.IndexByte(s, '+'); plus >= 0 {
		s = s[:plus]
	}

	var v version
	if dash := strings.IndexByte(s, '-'); dash >= 0 {
		v.prerelease = true
		s = s[:dash]
	}

	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return version{}, false
	}
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return version{}, false
		}
		v.nums[i] = n
	}
	return v, true
}
