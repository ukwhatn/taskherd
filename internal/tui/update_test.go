package tui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/ukwhatn/taskherd/internal/buildinfo"
	"github.com/ukwhatn/taskherd/internal/update"
)

// releasesServer answers the releases endpoint with one tag and counts the asks.
func releasesServer(t *testing.T, tag string, calls *int) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*calls++
		_, _ = w.Write([]byte(`{"tag_name":"` + tag + `"}`))
	}))
	t.Cleanup(server.Close)
	return server
}

func asReleased(t *testing.T, version string) {
	t.Helper()
	buildinfo.Version = version
	t.Cleanup(func() { buildinfo.Version = "" })
}

func TestBoardAnnouncesANewerRelease(t *testing.T) {
	asReleased(t, "v1.0.0")
	calls := 0
	server := releasesServer(t, "v1.3.0", &calls)

	b := &Board{
		ctx:  context.Background(),
		text: ja,
		deps: Deps{UpdateChecker: &update.Checker{Dir: t.TempDir(), URL: server.URL}},
	}

	cmd := b.checkUpdateCmd()
	if cmd == nil {
		t.Fatal("checkUpdateCmd() = nil, want 確認する")
	}
	msg, ok := cmd().(updateFoundMsg)
	if !ok {
		t.Fatalf("cmd() = %T, want updateFoundMsg", cmd())
	}
	if msg.tag != "v1.3.0" {
		t.Errorf("tag = %q, want v1.3.0", msg.tag)
	}

	b.announceUpdate(msg.tag)
	if !strings.Contains(b.status, "v1.3.0") {
		t.Errorf("status = %q, want 新しい版を含む", b.status)
	}
	if b.statusIsError {
		t.Error("statusIsError = true, want false（更新の案内はエラーではない）")
	}
}

// A nil checker is the switch config's [update] check = false turns: the board must not reach the
// network at all, rather than reaching it and discarding the answer.
func TestBoardWithoutACheckerNeverAsks(t *testing.T) {
	asReleased(t, "v1.0.0")

	b := &Board{ctx: context.Background(), text: ja}
	if cmd := b.checkUpdateCmd(); cmd != nil {
		t.Error("checkUpdateCmd() != nil, want 確認しない")
	}
}

func TestBoardDoesNotCheckForADevelopmentBuild(t *testing.T) {
	calls := 0
	server := releasesServer(t, "v1.3.0", &calls)

	b := &Board{
		ctx:  context.Background(),
		text: ja,
		deps: Deps{UpdateChecker: &update.Checker{Dir: t.TempDir(), URL: server.URL}},
	}

	if cmd := b.checkUpdateCmd(); cmd != nil {
		t.Error("開発ビルドで確認しようとしている")
	}
	if calls != 0 {
		t.Errorf("問い合わせ回数 = %d, want 0", calls)
	}
}

// The record is what keeps the board off the network on every start; a fresh one within the
// interval must not produce a second ask.
func TestBoardHonoursTheRecordedInterval(t *testing.T) {
	asReleased(t, "v1.0.0")
	calls := 0
	server := releasesServer(t, "v1.3.0", &calls)

	dir := t.TempDir()
	now := time.Now()
	checker := &update.Checker{Dir: dir, URL: server.URL, Now: func() time.Time { return now }}
	if _, err := checker.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if calls != 1 {
		t.Fatalf("下準備の問い合わせ回数 = %d, want 1", calls)
	}

	b := &Board{ctx: context.Background(), text: ja, deps: Deps{UpdateChecker: checker}}
	if msg := b.checkUpdateCmd()(); msg == nil {
		t.Error("記録済みの新しい版が通知されていない")
	}
	if calls != 1 {
		t.Errorf("問い合わせ回数 = %d, want 1（記録が新しいので聞き直さない）", calls)
	}
}

// A board whose Init includes the check must still start when the network is not there.
func TestBoardStartsWhenTheCheckFails(t *testing.T) {
	asReleased(t, "v1.0.0")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	b := &Board{
		ctx:  context.Background(),
		text: ja,
		deps: Deps{UpdateChecker: &update.Checker{Dir: t.TempDir(), URL: server.URL}},
	}

	var msg tea.Msg = b.checkUpdateCmd()()
	if msg != nil {
		t.Errorf("失敗時に %v を返している, want nil（知らないことは知らせない）", msg)
	}
}
