package postgres

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/soaringjerry/PCAS/internal/memory"
)

func TestConversationSegmentBoundaries(t *testing.T) {
	makeSource := func(role, text string) memory.SourceResult {
		return memory.SourceResult{Source: memory.Source{Text: text}, Context: &memory.SourceContext{Role: role, Branch: "current"}}
	}
	sources := []memory.SourceResult{
		makeSource("user", strings.Repeat("甲", 6000)),
		makeSource("assistant", strings.Repeat("乙", 5000)),
		makeSource("user", strings.Repeat("丙", 4800)),
		makeSource("user", strings.Repeat("丁", 15000)),
		makeSource("user", "最后一句"),
	}
	segments := splitConversation(sources)
	if len(segments) != 5 || len(segments[0].Messages) != 2 {
		t.Fatalf("message boundaries: %+v", segments)
	}
	if utf8.RuneCountInString(segments[0].Messages[1].Text) != 5000 || segments[2].Messages[0].Text+segments[3].Messages[0].Text != sources[3].Source.Text {
		t.Fatal("full text coverage or original changed")
	}
	for i, size := range []int{12000, 3000} {
		fragment := segments[i+2].Messages[0]
		if fragment.Index != 4 || fragment.Part != i+1 || fragment.Parts != 2 || utf8.RuneCountInString(fragment.Text) != size {
			t.Fatal("fragment numbering or budget", fragment)
		}
	}
	context := segments[2].Context
	if len(context) != 2 || context[0].Index != 2 || context[1].Index != 3 || context[0].Text != sources[1].Source.Text || context[1].Text != sources[2].Source.Text || segments[4].Messages[0].Index != 5 {
		t.Fatal("global numbering or complete overlap")
	}
}

func TestConversationOverlapDoesNotConsumeBodyBudget(t *testing.T) {
	sources := []memory.SourceResult{}
	for _, count := range []int{7000, 5000, 6000, 6000} {
		sources = append(sources, memory.SourceResult{Source: memory.Source{Text: strings.Repeat("字", count)}, Context: &memory.SourceContext{Role: "user"}})
	}
	segments := splitConversation(sources)
	if len(segments) != 2 || len(segments[1].Messages) != 2 || len(segments[1].Context) != 2 || segments[1].Messages[0].Index != 3 {
		t.Fatal("overlap consumed the 12000-character body budget")
	}
}

func TestConversationManifestFencesInputChanges(t *testing.T) {
	source := memory.SourceResult{Source: memory.Source{Text: "去成都"}, Context: &memory.SourceContext{Role: "user", Branch: "current"}}
	source.Source.ID, source.Source.Version = memory.NewID(), 1
	run := conversationRun([]memory.SourceResult{source})
	for _, mutate := range []func(*memory.SourceResult){
		func(s *memory.SourceResult) { s.Source.Version++ },
		func(s *memory.SourceResult) { s.Source.Text = "去大理" },
		func(s *memory.SourceResult) { s.Context = &memory.SourceContext{Role: "user", Branch: "historical"} },
	} {
		changed := source
		mutate(&changed)
		if run == conversationRun([]memory.SourceResult{changed}) {
			t.Fatal("changed input retained the same manifest")
		}
	}
}
