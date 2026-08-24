package fetch

import (
	"encoding/json"
	"time"

	"github.com/ukwhatn/taskherd/internal/model"
)

// LinkState is the display-ready reading of one cache entry.
//
// It exists so that callers rendering a link (the board's badges, `show`'s detail output) do
// not each have to know how cache.json distinguishes "never fetched" from "fetched but now
// failing", nor which Data shape belongs to which link kind.
type LinkState struct {
	URL  string
	Kind model.LinkKind

	// Cached is false when the link has no cache entry at all: nothing has tried to fetch it yet.
	Cached bool
	// Fetched is true once a fetch has succeeded. Only then do GitHub/Jira and Age carry meaning:
	// a failure keeps the last successful value, and a link that never succeeded has none.
	Fetched bool
	// Stale reports that the last success is older than the TTL. A link that never succeeded is
	// not stale, it is unfetched, which is a different thing to show.
	Stale bool
	// Age is how long ago the last success was.
	Age time.Duration
	// Err is the last failure, empty when the last attempt succeeded.
	Err string

	GitHub *GitHubData
	Jira   *JiraData
}

// Fetchable reports whether this kind of link has live state at all. An "other" link is only
// a URL and a note.
func (s LinkState) Fetchable() bool {
	switch s.Kind {
	case model.LinkKindGitHubPR, model.LinkKindGitHubIssue, model.LinkKindJira:
		return true
	default:
		return false
	}
}

// LinkState reads the cached state of one link.
func (f *CacheFile) LinkState(link model.Link, now time.Time, ttl time.Duration) LinkState {
	state := LinkState{URL: link.URL, Kind: link.Kind}
	if !state.Fetchable() {
		return state
	}

	entry, ok := f.Get(link.URL)
	if !ok {
		return state
	}
	state.Cached = true
	if !entry.OK {
		state.Err = entry.Error
	}
	if entry.FetchedAt == nil {
		return state
	}
	fetchedAt, err := time.Parse(time.RFC3339, *entry.FetchedAt)
	if err != nil {
		return state
	}

	state.Fetched = true
	state.Age = now.Sub(fetchedAt)
	state.Stale = entry.IsStale(now, ttl)
	state.decode(entry.Data)
	return state
}

// LinkStates reads the cached state of every given link, keyed by URL.
func (f *CacheFile) LinkStates(links []model.Link, now time.Time, ttl time.Duration) map[string]LinkState {
	states := make(map[string]LinkState, len(links))
	for _, link := range links {
		states[link.URL] = f.LinkState(link, now, ttl)
	}
	return states
}

// decode fills in the payload for this link's kind. A payload that no longer parses is treated
// as absent rather than fatal: the cache is rebuildable, and one bad entry must not blank the board.
func (s *LinkState) decode(data json.RawMessage) {
	if len(data) == 0 {
		s.Fetched = false
		return
	}
	switch s.Kind {
	case model.LinkKindGitHubPR, model.LinkKindGitHubIssue:
		var payload GitHubData
		if err := json.Unmarshal(data, &payload); err != nil {
			s.Fetched = false
			return
		}
		s.GitHub = &payload
	case model.LinkKindJira:
		var payload JiraData
		if err := json.Unmarshal(data, &payload); err != nil {
			s.Fetched = false
			return
		}
		s.Jira = &payload
	}
}
