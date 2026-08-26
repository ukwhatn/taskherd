package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// jiraHarness is a board-less setup with one Jira link, so that `refresh` reports how the token
// was resolved without any network being involved: an unresolved token never reaches an HTTP call.
func jiraHarness(t *testing.T, jiraSection string) *harness {
	t.Helper()
	h := newHarness(t)
	h.writeConfig(t, `
[jira]
site = "your-tenant.atlassian.net"
email = "you@example.com"
`+jiraSection)
	h.mustRun(t, "add", "Jira 付きのタスク", "--link", "https://your-tenant.atlassian.net/browse/ABC-1")
	return h
}

func TestRefreshReadsJiraTokenFromFile(t *testing.T) {
	tokenPath := filepath.Join(t.TempDir(), "jira_token")
	// The trailing newline is what an editor leaves behind, and must not become part of the token.
	if err := os.WriteFile(tokenPath, []byte("secret-token\n"), 0o600); err != nil {
		t.Fatalf("トークンファイルを書けない: %v", err)
	}

	h := jiraHarness(t, "token_file = "+quote(tokenPath))
	res := h.run(t, "refresh", "--all")

	// The token resolved, so the fetch was attempted and failed on the network rather than on the
	// configuration. Either way nothing may echo the token itself.
	if strings.Contains(res.stdout+res.stderr, "Jira の設定がない") {
		t.Errorf("token_file が読まれていない:\n%s%s", res.stdout, res.stderr)
	}
	if strings.Contains(res.stdout+res.stderr, "secret-token") {
		t.Errorf("トークンが出力に漏れている:\n%s%s", res.stdout, res.stderr)
	}
}

func TestRefreshPrefersTheEnvironmentOverTheFile(t *testing.T) {
	tokenPath := filepath.Join(t.TempDir(), "jira_token")
	if err := os.WriteFile(tokenPath, []byte(""), 0o600); err != nil {
		t.Fatalf("トークンファイルを書けない: %v", err)
	}

	h := jiraHarness(t, "token_file = "+quote(tokenPath))
	h.env["TASKHERD_JIRA_TOKEN"] = "from-env"
	res := h.run(t, "refresh", "--all")

	// The file is empty; had it been consulted the run would have said so.
	if strings.Contains(res.stdout+res.stderr, "が空") {
		t.Errorf("環境変数があるのにファイルを見ている:\n%s%s", res.stdout, res.stderr)
	}
}

func TestRefreshSaysWhyTheTokenFileFailed(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "not-there")
	h := jiraHarness(t, "token_file = "+quote(missing))
	res := h.run(t, "refresh", "--all")

	// "Jira の設定がない" on its own is the dead end this reason exists to remove.
	out := res.stdout + res.stderr
	if !strings.Contains(out, "token_file") || !strings.Contains(out, "読めません") {
		t.Errorf("読めない理由が出ていない:\n%s", out)
	}
}

func TestRefreshSaysWhenTheTokenFileIsEmpty(t *testing.T) {
	tokenPath := filepath.Join(t.TempDir(), "jira_token")
	if err := os.WriteFile(tokenPath, []byte("   \n"), 0o600); err != nil {
		t.Fatalf("トークンファイルを書けない: %v", err)
	}

	h := jiraHarness(t, "token_file = "+quote(tokenPath))
	res := h.run(t, "refresh", "--all")

	if !strings.Contains(res.stdout+res.stderr, "が空") {
		t.Errorf("空である旨が出ていない:\n%s%s", res.stdout, res.stderr)
	}
}

func TestRefreshWithoutAnyTokenSourceStillSaysNotConfigured(t *testing.T) {
	h := jiraHarness(t, "")
	res := h.run(t, "refresh", "--all")

	if !strings.Contains(res.stdout+res.stderr, ja.Err.Live.JiraNotConfigured.Msg) {
		t.Errorf("未設定である旨が出ていない:\n%s%s", res.stdout, res.stderr)
	}
}

// A path in the home directory is what a config actually holds, and ~ is how it gets written.
func TestRefreshExpandsTildeInTheTokenFilePath(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".config", "taskherd"), 0o700); err != nil {
		t.Fatalf("HOME 配下を作れない: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".config", "taskherd", "jira_token"), []byte("secret-token\n"), 0o600); err != nil {
		t.Fatalf("トークンファイルを書けない: %v", err)
	}

	h := jiraHarness(t, `token_file = "~/.config/taskherd/jira_token"`)
	h.env["HOME"] = home
	res := h.run(t, "refresh", "--all")

	if strings.Contains(res.stdout+res.stderr, "読めない") {
		t.Errorf("~ が展開されていない:\n%s%s", res.stdout, res.stderr)
	}
	if strings.Contains(res.stdout+res.stderr, "secret-token") {
		t.Errorf("トークンが出力に漏れている:\n%s%s", res.stdout, res.stderr)
	}
}

func quote(s string) string { return `"` + s + `"` }
