package retrieval

import (
	"strings"
	"testing"

	"github.com/soaringjerry/PCAS/internal/memory"
)

func TestASummaryKeepsItsBudgetAndSaysWhatItLeftOut(t *testing.T) {
	entries := []SummaryEntry{
		{Ref: memory.Ref{ID: "s", Version: 1, Kind: memory.SourceKind}, Text: "原话", Role: "user"},
		{Ref: memory.Ref{ID: "c", Version: 2, Kind: memory.ClaimKind}, Text: strings.Repeat("长", 50), Nature: "fact", Confirmation: "candidate"},
		{Ref: memory.Ref{ID: "d", Version: 1, Kind: memory.ClaimKind}, Text: "短", Nature: "plan", Confirmation: "confirmed", Gaps: []string{"缺上下文"}},
	}
	text, deps, coverage := Summary(entries, false, true, "s", 30)
	if text != "[s v1] 原文摘录，保留当时表达 / user：原话\n[d v1] plan / confirmed：短\n" || len(deps) != 3 {
		t.Fatalf("%q", text)
	}
	if coverage.Complete || len(coverage.PendingSources) != 1 || strings.Join(coverage.Gaps, "|") != "来源仍有待处理或失败步骤；摘要只使用目前可读内容|摘要受上下文预算限制，展开来源可查看全文|缺上下文" {
		t.Fatalf("%+v", coverage)
	}
	if _, _, c := Summary(entries[:1], false, false, "s", 2000); !c.Complete {
		t.Fatal("a summary with everything is incomplete")
	}
}
