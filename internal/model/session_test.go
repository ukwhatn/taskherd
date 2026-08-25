package model_test

import (
	"reflect"
	"testing"

	"github.com/ukwhatn/taskherd/internal/model"
)

func fileWithSessions(sessions ...model.SessionRef) model.File {
	f := *model.NewFile()
	f.Tasks = []model.Task{{ID: 1, Title: "a", Status: "todo", Sessions: sessions}}
	return f
}

func TestRankSessionCwdsOrdersByFrequencyThenRecencyThenName(t *testing.T) {
	f := fileWithSessions(
		model.SessionRef{SessionID: "s1", Agent: "claude", Cwd: "/repo/rare", LinkedAt: "2026-08-24T10:00:00+09:00"},
		model.SessionRef{SessionID: "s2", Agent: "claude", Cwd: "/repo/frequent", LinkedAt: "2026-08-20T10:00:00+09:00"},
		model.SessionRef{SessionID: "s3", Agent: "claude", Cwd: "/repo/frequent", LinkedAt: "2026-08-24T09:00:00+09:00"},
	)

	got := model.RankSessionCwds(f)
	want := []string{"/repo/frequent", "/repo/rare"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("RankSessionCwds() = %v, want %v（頻度優先）", got, want)
	}
}

func TestRankSessionCwdsBreaksFrequencyTieByRecency(t *testing.T) {
	f := fileWithSessions(
		model.SessionRef{SessionID: "s1", Agent: "claude", Cwd: "/repo/old", LinkedAt: "2026-08-20T10:00:00+09:00"},
		model.SessionRef{SessionID: "s2", Agent: "claude", Cwd: "/repo/new", LinkedAt: "2026-08-24T10:00:00+09:00"},
	)

	got := model.RankSessionCwds(f)
	want := []string{"/repo/new", "/repo/old"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("RankSessionCwds() = %v, want %v（新しい LinkedAt を優先）", got, want)
	}
}

// LinkedAt has only second resolution, so two candidates tied on both frequency and recency are
// a real case, not a corner one; without a lexical tiebreaker the order would depend on map
// iteration. Inserted in reverse to catch an implementation that happens to preserve insertion
// order instead of actually sorting.
func TestRankSessionCwdsBreaksFullTieByCwdName(t *testing.T) {
	f := fileWithSessions(
		model.SessionRef{SessionID: "s1", Agent: "claude", Cwd: "/repo/zzz", LinkedAt: "2026-08-24T10:00:00+09:00"},
		model.SessionRef{SessionID: "s2", Agent: "claude", Cwd: "/repo/aaa", LinkedAt: "2026-08-24T10:00:00+09:00"},
	)

	got := model.RankSessionCwds(f)
	want := []string{"/repo/aaa", "/repo/zzz"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("RankSessionCwds() = %v, want %v（辞書順）", got, want)
	}
}

func TestRankSessionCwdsExcludesBlankCwd(t *testing.T) {
	f := fileWithSessions(
		model.SessionRef{SessionID: "s1", Agent: "claude", Cwd: "   ", LinkedAt: "2026-08-24T10:00:00+09:00"},
		model.SessionRef{SessionID: "s2", Agent: "claude", Cwd: "/repo/real", LinkedAt: "2026-08-24T10:00:00+09:00"},
	)

	got := model.RankSessionCwds(f)
	want := []string{"/repo/real"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("RankSessionCwds() = %v, want %v（空白だけの cwd を除く）", got, want)
	}
}

func TestRankSessionCwdsEmptyInput(t *testing.T) {
	if got := model.RankSessionCwds(*model.NewFile()); len(got) != 0 {
		t.Errorf("RankSessionCwds(空) = %v, want 空", got)
	}
}

func TestRenderPromptExpandsAllPlaceholders(t *testing.T) {
	task := model.Task{
		ID: 12, Title: "設計する", Status: "working", Note: "note 本文",
		Links: []model.Link{{URL: "https://github.com/o/r/pull/1"}, {URL: "https://example.com/doc"}},
	}

	got := model.RenderPrompt("#{{id}} {{title}} ({{status}})\n{{note}}\n{{links}}", task)
	want := "#12 設計する (working)\nnote 本文\n- https://github.com/o/r/pull/1\n- https://example.com/doc"
	if got != want {
		t.Errorf("RenderPrompt() = %q, want %q", got, want)
	}
}

func TestRenderPromptDropsLinksLineWhenNoLinks(t *testing.T) {
	task := model.Task{ID: 1, Title: "a", Status: "todo"}

	got := model.RenderPrompt("先頭行\n{{links}}\n末尾行", task)
	want := "先頭行\n末尾行"
	if got != want {
		t.Errorf("RenderPrompt() = %q, want %q（リンク無しの行は落ちる）", got, want)
	}
}

// A title containing literal "{{...}}" text must not be expanded a second time by a placeholder
// substituted in after it: this is what single-pass replacement (strings.NewReplacer) buys over
// chaining ReplaceAll calls.
func TestRenderPromptDoesNotDoubleExpandSubstitutedText(t *testing.T) {
	task := model.Task{ID: 1, Title: "{{note}} を直す", Status: "todo", Note: "実際の note"}

	got := model.RenderPrompt("{{title}} / {{note}}", task)
	want := "{{note}} を直す / 実際の note"
	if got != want {
		t.Errorf("RenderPrompt() = %q, want %q（title 由来の {{note}} が再展開されない）", got, want)
	}
}

func TestRenderPromptEmptyTemplate(t *testing.T) {
	if got := model.RenderPrompt("", model.Task{ID: 1, Title: "a"}); got != "" {
		t.Errorf("RenderPrompt(\"\") = %q, want 空", got)
	}
}
