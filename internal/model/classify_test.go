package model_test

import (
	"testing"

	"github.com/ukwhatn/taskherd/internal/model"
)

func TestURLClassifierClassify(t *testing.T) {
	const jiraSite = "dena.atlassian.net"

	tests := []struct {
		name      string
		url       string
		ghesHosts []string
		jiraSite  string
		want      model.LinkKind
	}{
		{name: "github.com の pull は github_pr", url: "https://github.com/owner/repo/pull/123", want: model.LinkKindGitHubPR},
		{name: "github.com の issues は github_issue", url: "https://github.com/owner/repo/issues/45", want: model.LinkKindGitHubIssue},
		{name: "ホスト名は大文字小文字を区別しない", url: "https://GitHub.COM/owner/repo/pull/1", want: model.LinkKindGitHubPR},
		{name: "PR URL の後続セグメントは無視する", url: "https://github.com/owner/repo/pull/123/files", want: model.LinkKindGitHubPR},
		{name: "fragment 付きの issue URL", url: "https://github.com/owner/repo/issues/45#issuecomment-1", want: model.LinkKindGitHubIssue},
		{name: "番号が非数値なら other", url: "https://github.com/owner/repo/pull/abc", want: model.LinkKindOther},
		{name: "番号が全角数字なら other", url: "https://github.com/owner/repo/pull/１２３", want: model.LinkKindOther},
		{name: "番号が 0 なら other", url: "https://github.com/owner/repo/pull/0", want: model.LinkKindOther},
		{name: "番号が欠けていれば other", url: "https://github.com/owner/repo/pull/", want: model.LinkKindOther},
		{name: "リポジトリルートは other", url: "https://github.com/owner/repo", want: model.LinkKindOther},
		{name: "owner/repo が欠けた pull は other", url: "https://github.com/pull/123", want: model.LinkKindOther},
		{name: "http スキームも判別する", url: "http://github.com/owner/repo/pull/9", want: model.LinkKindGitHubPR},
		{name: "http/https 以外のスキームは other", url: "ssh://github.com/owner/repo/pull/1", want: model.LinkKindOther},

		{name: "config の ghes_hosts に一致すれば github_pr", url: "https://github.dena.jp/owner/repo/pull/7", ghesHosts: []string{"github.dena.jp"}, want: model.LinkKindGitHubPR},
		{name: "ghes_hosts の指定は大文字小文字・空白を無視する", url: "https://github.dena.jp/owner/repo/issues/7", ghesHosts: []string{" GitHub.DENA.jp "}, want: model.LinkKindGitHubIssue},
		{name: "ghes_hosts に無い GitHub 風ホストは other", url: "https://github.example.com/owner/repo/pull/7", ghesHosts: []string{"github.dena.jp"}, want: model.LinkKindOther},
		{name: "ポート付きホストも ghes_hosts と一致する", url: "https://github.dena.jp:8443/owner/repo/pull/1", ghesHosts: []string{"github.dena.jp"}, want: model.LinkKindGitHubPR},

		{name: "config の jira.site の browse は jira", url: "https://dena.atlassian.net/browse/ABC-123", jiraSite: jiraSite, want: model.LinkKindJira},
		{name: "jira.site にスキーム・末尾スラッシュが付いていても一致する", url: "https://dena.atlassian.net/browse/ABC-123", jiraSite: "https://dena.atlassian.net/", want: model.LinkKindJira},
		{name: "小文字のプロジェクトキーも受理する", url: "https://dena.atlassian.net/browse/abc-123", jiraSite: jiraSite, want: model.LinkKindJira},
		{name: "クエリ付きの browse URL", url: "https://dena.atlassian.net/browse/ABC-123?focusedCommentId=1", jiraSite: jiraSite, want: model.LinkKindJira},
		{name: "config と異なる atlassian テナントは other", url: "https://other.atlassian.net/browse/ABC-123", jiraSite: jiraSite, want: model.LinkKindOther},
		{name: "jira.site 未設定なら atlassian.net でも other", url: "https://dena.atlassian.net/browse/ABC-123", want: model.LinkKindOther},
		{name: "課題番号のないキーは other", url: "https://dena.atlassian.net/browse/ABC", jiraSite: jiraSite, want: model.LinkKindOther},
		{name: "browse 以外のパスは other", url: "https://dena.atlassian.net/projects/ABC", jiraSite: jiraSite, want: model.LinkKindOther},
		{name: "browse の後続セグメントは other", url: "https://dena.atlassian.net/browse/ABC-1/comments", jiraSite: jiraSite, want: model.LinkKindOther},

		{name: "URL でない文字列は other", url: "これは URL ではない", want: model.LinkKindOther},
		{name: "空文字は other", url: "", want: model.LinkKindOther},
		{name: "ホストのない URL は other", url: "/owner/repo/pull/1", want: model.LinkKindOther},
		{name: "無関係な https URL は other", url: "https://example.com/owner/repo/pull/1", want: model.LinkKindOther},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := model.URLClassifier{GHESHosts: tt.ghesHosts, JiraSite: tt.jiraSite}
			if got := c.Classify(tt.url); got != tt.want {
				t.Errorf("Classify(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

func TestURLClassifierJiraKey(t *testing.T) {
	const jiraSite = "dena.atlassian.net"
	c := model.URLClassifier{JiraSite: jiraSite}

	tests := []struct {
		name   string
		url    string
		want   string
		wantOK bool
	}{
		{name: "browse URL からキーを取り出す", url: "https://dena.atlassian.net/browse/ABC-123", want: "ABC-123", wantOK: true},
		{name: "小文字キーはそのまま返す（呼び出し側が正規化を決める）", url: "https://dena.atlassian.net/browse/abc-123", want: "abc-123", wantOK: true},
		{name: "クエリ・フラグメント付きでもキーだけ返す", url: "https://dena.atlassian.net/browse/ABC-123?focusedCommentId=1", want: "ABC-123", wantOK: true},
		{name: "github の URL は対象外", url: "https://github.com/owner/repo/pull/1", want: "", wantOK: false},
		{name: "config と異なるテナントは対象外", url: "https://other.atlassian.net/browse/ABC-123", want: "", wantOK: false},
		{name: "browse 以外のパスは対象外", url: "https://dena.atlassian.net/projects/ABC", want: "", wantOK: false},
		{name: "不正な URL は対象外", url: "これは URL ではない", want: "", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := c.JiraKey(tt.url)
			if ok != tt.wantOK || got != tt.want {
				t.Errorf("JiraKey(%q) = (%q, %v), want (%q, %v)", tt.url, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestURLClassifierGitHubRef(t *testing.T) {
	c := model.URLClassifier{GHESHosts: []string{"github.dena.jp"}}

	tests := []struct {
		name   string
		url    string
		want   model.GitHubRef
		wantOK bool
	}{
		{name: "PR URL", url: "https://github.com/owner/repo/pull/123", want: model.GitHubRef{Owner: "owner", Repo: "repo", Number: 123}, wantOK: true},
		{name: "issue URL", url: "https://github.com/owner/repo/issues/45", want: model.GitHubRef{Owner: "owner", Repo: "repo", Number: 45}, wantOK: true},
		{name: "後続セグメントは無視する", url: "https://github.com/owner/repo/pull/123/files", want: model.GitHubRef{Owner: "owner", Repo: "repo", Number: 123}, wantOK: true},
		{name: "GHES ホスト", url: "https://github.dena.jp/team/svc/pull/7", want: model.GitHubRef{Owner: "team", Repo: "svc", Number: 7}, wantOK: true},
		{name: "Jira URL は対象外", url: "https://x.atlassian.net/browse/ABC-1", wantOK: false},
		{name: "リポジトリルートは対象外", url: "https://github.com/owner/repo", wantOK: false},
		{name: "URL でない文字列は対象外", url: "これは URL ではない", wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := c.GitHubRef(tt.url)
			if ok != tt.wantOK || got != tt.want {
				t.Errorf("GitHubRef(%q) = (%+v, %v), want (%+v, %v)", tt.url, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestLinkHost(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"https://example.com/some/page", "example.com"},
		{"https://EXAMPLE.com:8443/x", "EXAMPLE.com"},
		{"https://github.com/o/r/pull/1", "github.com"},
		{"/relative/path", ""},
		{"", ""},
	}
	for _, tt := range tests {
		if got := model.LinkHost(tt.url); got != tt.want {
			t.Errorf("LinkHost(%q) = %q, want %q", tt.url, got, tt.want)
		}
	}
}

func TestLinkOwner(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"PR", "https://github.com/some-org/server/pull/12", "some-org"},
		{"issue", "https://github.com/me/tool/issues/3", "me"},
		{"GHES（ghes_hosts 未登録でも取れる）", "https://github.example.com/team/repo/pull/1", "team"},
		{"ホストのみ", "https://github.com", ""},
		{"ホスト + スラッシュ", "https://github.com/", ""},
		{"ホストのない文字列", "not a url", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := model.LinkOwner(tc.in); got != tc.want {
				t.Errorf("LinkOwner(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
