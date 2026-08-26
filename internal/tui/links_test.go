package tui

import (
	"strings"
	"testing"
)

func TestParseLinkURLs(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    []string
		wantErr bool
	}{
		{name: "空", raw: "   "},
		{
			name: "1 件",
			raw:  "https://github.com/o/r/pull/1",
			want: []string{"https://github.com/o/r/pull/1"},
		},
		{
			name: "空白区切り",
			raw:  "https://github.com/o/r/pull/1  https://github.com/o/r/pull/2",
			want: []string{"https://github.com/o/r/pull/1", "https://github.com/o/r/pull/2"},
		},
		{
			name: "改行区切り",
			raw:  "https://a.example/1\nhttps://b.example/2\n",
			want: []string{"https://a.example/1", "https://b.example/2"},
		},
		{
			name: "同一 URL は 1 件に畳む",
			raw:  "https://a.example/1 https://a.example/1",
			want: []string{"https://a.example/1"},
		},
		{
			name:    "スキーム無しは全体を拒否",
			raw:     "https://a.example/1 github.com/o/r",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseLinkURLs(ja, tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("err = nil, want エラー（got = %v）", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if strings.Join(got, " ") != strings.Join(tc.want, " ") {
				t.Errorf("got = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSplitTitleLines(t *testing.T) {
	got := splitTitleLines(" 設計する \n\n実装する\n  \nレビューする\n")
	want := []string{"設計する", "実装する", "レビューする"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("got = %v, want %v", got, want)
	}
	if len(splitTitleLines("  \n \n")) != 0 {
		t.Error("空行だけの入力からタイトルが作られた")
	}
}
