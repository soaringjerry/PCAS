package retrieval

import (
	"strings"
	"testing"

	"github.com/soaringjerry/PCAS/internal/memory"
)

func TestABudgetGetsItsDefaultsAndHasLimits(t *testing.T) {
	got, err := NormalizeBudget(memory.Budget{})
	if err != nil || got.Candidates != 30 || got.Edges != 30 || got.Tokens != 4000 || got.Hops != 1 {
		t.Fatalf("%+v %v", got, err)
	}
	if got, _ := NormalizeBudget(memory.Budget{Candidates: 100, Tokens: 10000}); got.Candidates != 100 || got.Tokens != 10000 {
		t.Fatalf("%+v", got)
	}
	for _, b := range []memory.Budget{{Candidates: 1001}, {Tokens: 32001}, {Hops: 4}, {Edges: -1}} {
		if _, err := NormalizeBudget(b); err != memory.ErrInvalid {
			t.Fatalf("%+v %v", b, err)
		}
	}
	if c := NewCoverage(); !c.Complete || c.Gaps == nil || c.PendingSources == nil {
		t.Fatalf("%+v", c)
	}
}

func TestAnExcerptHoldsThePartOfTheSourceThatAnswers(t *testing.T) {
	text := strings.Repeat("无关的开头。", 200) + "虚构的云杉喜欢乌龙茶。" + strings.Repeat("无关的结尾。", 200)
	got := SourceExcerpt(text, "喜欢乌龙茶", nil, 120)
	if !strings.Contains(got, "喜欢乌龙茶") || !strings.HasPrefix(got, "…") || !strings.HasSuffix(got, "…") || len([]rune(got)) != 120 {
		t.Fatalf("%d %q", len([]rune(got)), got)
	}
	// Without the whole query in the text, its words choose the window.
	byWords := SourceExcerpt(text, "乌龙茶 在哪里", []string{"乌龙茶", "哪里"}, 120)
	if !strings.Contains(byWords, "乌龙茶") {
		t.Fatalf("%q", byWords)
	}
	if short := SourceExcerpt("短文本", "别的", nil, 120); short != "短文本" {
		t.Fatal(short)
	}
	start, end := MatchedExcerptRange(text, "没有这句话", nil, 100)
	if start != 0 || end != 100 {
		t.Fatal(start, end)
	}
	if got := MatchedExcerpt(text, "喜欢乌龙茶", nil, 60); !strings.Contains(got, "喜欢乌龙茶") || len([]rune(got)) != 60 {
		t.Fatalf("%q", got)
	}
}
