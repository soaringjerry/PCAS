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
	if len(segments) != 3 || len(segments[0].Messages) != 3 || len(segments[1].Messages) != 1 || len(segments[2].Messages) != 1 {
		t.Fatalf("message boundaries: %+v", segments)
	}
	if utf8.RuneCountInString(segments[0].Messages[1].Text) != 1200 || utf8.RuneCountInString(segments[1].Messages[0].Text) != 12000 || utf8.RuneCountInString(sources[3].Source.Text) != 15000 {
		t.Fatal("visible limits or original changed")
	}
	context := segments[1].Context
	if len(context) != 2 || context[0].Index != 2 || context[1].Index != 3 || utf8.RuneCountInString(context[1].Text) != 1200 || segments[1].Messages[0].Index != 4 || segments[2].Messages[0].Index != 5 {
		t.Fatal("global numbering or bounded overlap")
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
