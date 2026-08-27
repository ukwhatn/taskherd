// Package update tells taskherd when a newer release exists, and replaces the running binary with
// it on request.
//
// The check is deliberately quiet. It runs at most once a day, from the board — the one process
// that lives long enough to absorb a network round trip — and every other command only reads what
// that check left behind. Nothing here ever blocks a command, and every failure is a non-event:
// not knowing whether an update exists is the normal state of an offline machine.
package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/ukwhatn/taskherd/internal/atomicfile"
)

const (
	// Repo is where releases come from. It is a constant rather than a setting: a binary that can
	// be pointed at another origin is a binary that can be talked into installing anything.
	Repo = "ukwhatn/taskherd"

	// checkInterval is how long a look at the releases page stays good for. A day is short enough
	// that an update is noticed within a working week and long enough that the unauthenticated
	// 60-per-hour budget is never a consideration.
	checkInterval = 24 * time.Hour

	// noticeInterval is how long the same version stays quiet after it has been mentioned once.
	noticeInterval = 24 * time.Hour

	// requestTimeout bounds the whole round trip. Nothing waits on this, but a hung connection
	// should not keep a goroutine alive for the length of a board session.
	requestTimeout = 3 * time.Second

	recordName = "update.json"
	filePerm   = 0o600
)

// APIURL is the endpoint the check reads. /releases/latest skips drafts and prereleases, which is
// exactly the set worth offering.
var APIURL = "https://api.github.com/repos/" + Repo + "/releases/latest"

// State is what one machine remembers about the releases page.
//
// It is a cache, not data: any of it can be thrown away and rebuilt by asking again, which is what
// happens whenever the file cannot be read.
type State struct {
	// CheckedAt is when the endpoint was last asked, successfully or not. A failed ask still
	// counts, so an offline machine tries daily instead of on every board start.
	CheckedAt time.Time `json:"checked_at"`
	// LatestTag is the newest release tag seen, as written on the release ("v1.2.3").
	LatestTag string `json:"latest_tag,omitempty"`
	// NoticedTag and NoticedAt record which version was last mentioned to the user and when, so
	// the same news is not repeated on every command.
	NoticedTag string    `json:"noticed_tag,omitempty"`
	NoticedAt  time.Time `json:"noticed_at,omitempty"`
}

// Checker reads and refreshes one machine's record of the releases page.
type Checker struct {
	// Dir is the state directory the record lives in.
	Dir string
	// Client is the HTTP client used to ask. Nil means a client bounded by requestTimeout.
	Client *http.Client
	// Now is the clock, so tests can move it.
	Now func() time.Time
	// URL overrides APIURL, for tests.
	URL string
	// Token supplies the credential to read the releases API with, and "" asks unauthenticated.
	// Nil is the same as returning "". Unauthenticated reads are capped per source address, which
	// a shared outbound address exhausts long before one machine's own daily check would.
	Token func(context.Context) string
}

// Path is where the record is kept.
func (c *Checker) Path() string { return filepath.Join(c.Dir, recordName) }

// Load returns what was remembered. A missing, unreadable or unparsable record reads as "nothing
// is known yet", which is also what an empty State means, so no error is worth returning: the
// caller's only recourse would be to check again, and it is about to anyway.
func (c *Checker) Load() State {
	data, err := os.ReadFile(c.Path())
	if err != nil {
		return State{}
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return State{}
	}
	return state
}

// Due reports whether the record is old enough to be worth refreshing.
func (c *Checker) Due(state State) bool {
	return c.now().Sub(state.CheckedAt) >= checkInterval
}

// Refresh asks the releases page and records the answer.
//
// CheckedAt advances whether or not the ask succeeded: a machine with no network would otherwise
// pay for a timeout on every board start. The returned State is what was saved, so a caller that
// already has it does not have to read the file back.
func (c *Checker) Refresh(ctx context.Context) (State, error) {
	state := c.Load()
	state.CheckedAt = c.now()

	tag, err := c.fetchLatestTag(ctx)
	if err == nil {
		state.LatestTag = tag
	}
	// The save happens either way, and its own failure does not change what was learned.
	if saveErr := c.save(state); saveErr != nil && err == nil {
		err = saveErr
	}
	return state, err
}

// FetchLatest asks the releases page without touching the record. `taskherd update` uses this: the
// person typing it is asking now, and a day-old answer is not what they asked for.
func (c *Checker) FetchLatest(ctx context.Context) (string, error) {
	return c.fetchLatestTag(ctx)
}

// Notice returns the tag worth telling the user about, or "" when there is nothing to say.
//
// It is "" when no newer release is known, and also when the same tag was already mentioned within
// the last day — an update notice that reappears on every command stops being read.
func (c *Checker) Notice(state State, current string) string {
	if state.LatestTag == "" || !Newer(current, state.LatestTag) {
		return ""
	}
	if state.NoticedTag == state.LatestTag && c.now().Sub(state.NoticedAt) < noticeInterval {
		return ""
	}
	return state.LatestTag
}

// MarkNoticed records that tag has been mentioned. A failure to save is dropped: the cost is one
// repeated notice, and there is nothing useful to say about it at the point it happens.
func (c *Checker) MarkNoticed(state State, tag string) {
	state.NoticedTag = tag
	state.NoticedAt = c.now()
	_ = c.save(state)
}

func (c *Checker) save(state State) error {
	if err := os.MkdirAll(c.Dir, 0o700); err != nil {
		return fmt.Errorf("cannot create %s: %w", c.Dir, err)
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("cannot build %s: %w", recordName, err)
	}
	return atomicfile.Write(c.Path(), append(data, '\n'), filePerm)
}

func (c *Checker) fetchLatestTag(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url(), nil)
	if err != nil {
		return "", fmt.Errorf("cannot build the request to the releases API: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if c.Token != nil {
		if token := c.Token(ctx); token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	}

	resp, err := c.client().Do(req)
	if err != nil {
		return "", fmt.Errorf("cannot reach the releases API: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		// A repository with no published release answers 404, which is not a malfunction — it is
		// the answer "there is nothing to update to".
		if resp.StatusCode == http.StatusNotFound {
			return "", errNoRelease
		}
		return "", fmt.Errorf("the releases API returned %d", resp.StatusCode)
	}

	// The response carries far more than the tag; capping the read keeps a surprising body from
	// becoming a memory problem.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("cannot read the releases API response: %w", err)
	}
	var payload struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", fmt.Errorf("cannot parse the releases API response: %w", err)
	}
	if payload.TagName == "" {
		return "", errNoRelease
	}
	return payload.TagName, nil
}

// errNoRelease means the repository has published nothing yet.
var errNoRelease = errors.New("no published release")

func (c *Checker) client() *http.Client {
	if c.Client != nil {
		return c.Client
	}
	return &http.Client{Timeout: requestTimeout}
}

func (c *Checker) url() string {
	if c.URL != "" {
		return c.URL
	}
	return APIURL
}

func (c *Checker) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}
