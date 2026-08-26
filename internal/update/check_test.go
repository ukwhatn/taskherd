package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var baseTime = time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)

// releasesServer answers /releases/latest with tag, counting how often it was asked.
func releasesServer(t *testing.T, tag string, calls *int) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*calls++
		if tag == "" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if got := r.Header.Get("Accept"); got != "application/vnd.github+json" {
			t.Errorf("Accept = %q, want application/vnd.github+json", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name":"` + tag + `","name":"whatever"}`))
	}))
	t.Cleanup(server.Close)
	return server
}

func newChecker(t *testing.T, url string, now time.Time) *Checker {
	t.Helper()
	return &Checker{Dir: t.TempDir(), URL: url, Now: func() time.Time { return now }}
}

func TestRefreshRecordsTheTag(t *testing.T) {
	calls := 0
	server := releasesServer(t, "v1.3.0", &calls)
	c := newChecker(t, server.URL, baseTime)

	state, err := c.Refresh(context.Background())
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if state.LatestTag != "v1.3.0" {
		t.Errorf("LatestTag = %q, want v1.3.0", state.LatestTag)
	}
	if !state.CheckedAt.Equal(baseTime) {
		t.Errorf("CheckedAt = %v, want %v", state.CheckedAt, baseTime)
	}
	if reloaded := c.Load(); reloaded.LatestTag != "v1.3.0" {
		t.Errorf("保存後の Load() = %+v, want v1.3.0 を含む", reloaded)
	}
}

// The whole point of the record is that the endpoint is not asked again within the interval.
func TestDueHonoursTheInterval(t *testing.T) {
	c := newChecker(t, "", baseTime)

	for _, tc := range []struct {
		name  string
		since time.Duration
		want  bool
	}{
		{"直後", 0, false},
		{"23 時間後", 23 * time.Hour, false},
		{"24 時間後", 24 * time.Hour, true},
		{"一度も確認していない", 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := State{}
			if tc.name != "一度も確認していない" {
				state.CheckedAt = baseTime.Add(-tc.since)
			}
			if got := c.Due(state); got != tc.want {
				t.Errorf("Due() = %v, want %v", got, tc.want)
			}
		})
	}
}

// An offline machine must not pay for a timeout on every board start, so a failed look still
// counts as a look.
func TestRefreshAdvancesCheckedAtOnFailure(t *testing.T) {
	calls := 0
	server := releasesServer(t, "", &calls) // answers 404
	c := newChecker(t, server.URL, baseTime)

	state, err := c.Refresh(context.Background())
	if err == nil {
		t.Fatal("Refresh() error = nil, want 失敗")
	}
	if !state.CheckedAt.Equal(baseTime) {
		t.Errorf("CheckedAt = %v, want 失敗しても進む", state.CheckedAt)
	}
	if c.Due(c.Load()) {
		t.Error("失敗後すぐに Due() = true（毎回叩きにいってしまう）")
	}
}

// A failed look must not erase what a successful one found.
func TestRefreshKeepsTheLastKnownTagOnFailure(t *testing.T) {
	calls := 0
	good := releasesServer(t, "v1.3.0", &calls)
	c := newChecker(t, good.URL, baseTime)
	if _, err := c.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}

	bad := releasesServer(t, "", &calls)
	c.URL = bad.URL
	c.Now = func() time.Time { return baseTime.Add(48 * time.Hour) }

	state, _ := c.Refresh(context.Background())
	if state.LatestTag != "v1.3.0" {
		t.Errorf("LatestTag = %q, want 直前に取得した v1.3.0 を保持", state.LatestTag)
	}
}

// The record is a cache: anything unreadable is rebuilt rather than reported.
func TestLoadTreatsAnUnreadableRecordAsUnknown(t *testing.T) {
	c := newChecker(t, "", baseTime)
	if err := os.MkdirAll(c.Dir, 0o700); err != nil {
		t.Fatalf("ディレクトリを作れない: %v", err)
	}
	if err := os.WriteFile(c.Path(), []byte("{not json"), 0o600); err != nil {
		t.Fatalf("壊れた記録を書けない: %v", err)
	}

	if state := c.Load(); state != (State{}) {
		t.Errorf("Load() = %+v, want 空", state)
	}
	if !c.Due(c.Load()) {
		t.Error("壊れた記録の後に Due() = false（再取得できない）")
	}
}

func TestNotice(t *testing.T) {
	for _, tc := range []struct {
		name    string
		state   State
		current string
		want    string
	}{
		{"新しい版がある", State{LatestTag: "v1.3.0"}, "1.2.3", "v1.3.0"},
		{"最新である", State{LatestTag: "v1.2.3"}, "1.2.3", ""},
		{"まだ確認できていない", State{}, "1.2.3", ""},
		{"開発ビルドには黙る", State{LatestTag: "v1.3.0"}, "dev", ""},
		{
			"同じ版は 24 時間以内なら黙る",
			State{LatestTag: "v1.3.0", NoticedTag: "v1.3.0", NoticedAt: baseTime.Add(-23 * time.Hour)},
			"1.2.3", "",
		},
		{
			"24 時間経てばもう一度言う",
			State{LatestTag: "v1.3.0", NoticedTag: "v1.3.0", NoticedAt: baseTime.Add(-25 * time.Hour)},
			"1.2.3", "v1.3.0",
		},
		{
			"別の版なら間隔に関係なく言う",
			State{LatestTag: "v1.4.0", NoticedTag: "v1.3.0", NoticedAt: baseTime},
			"1.2.3", "v1.4.0",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newChecker(t, "", baseTime)
			if got := c.Notice(tc.state, tc.current); got != tc.want {
				t.Errorf("Notice() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestMarkNoticedSilencesTheNextNotice(t *testing.T) {
	c := newChecker(t, "", baseTime)
	state := State{CheckedAt: baseTime, LatestTag: "v1.3.0"}

	c.MarkNoticed(state, "v1.3.0")

	if got := c.Notice(c.Load(), "1.2.3"); got != "" {
		t.Errorf("Notice() = %q, want 空（既に伝えている）", got)
	}
}

// `taskherd update` asks now, on purpose: a day-old answer is not what the person typing it wants.
func TestFetchLatestDoesNotTouchTheRecord(t *testing.T) {
	calls := 0
	server := releasesServer(t, "v2.0.0", &calls)
	c := newChecker(t, server.URL, baseTime)

	tag, err := c.FetchLatest(context.Background())
	if err != nil {
		t.Fatalf("FetchLatest() error = %v", err)
	}
	if tag != "v2.0.0" {
		t.Errorf("FetchLatest() = %q, want v2.0.0", tag)
	}
	if _, err := os.Stat(c.Path()); !os.IsNotExist(err) {
		t.Errorf("%s が作られている（記録は触らないはず）", filepath.Base(c.Path()))
	}
}
