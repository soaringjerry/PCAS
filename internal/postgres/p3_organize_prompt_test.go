package postgres

import (
	"context"
	"encoding/json"
	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
	"os"
	"testing"
	"time"
)

var p3RuleCases = []struct{ Text, Category string }{
	{"以后所有发出去的文案，都先给我看，确认后再发。", "rule"},
	{"回答请一直简洁，别啰嗦。", "rule"},
	{"长期帮我安排时间时，都用北京时间。", "rule"},
	{"今后缺少信息时先问我，不要自行猜测。", "rule"},
	{"以后写正式邮件都用礼貌克制的语气。", "rule"},
	{"明天下午三点提醒我交虚构住处的电费。", "event"},
	{"这一次请在10月10日把虚构青羽方案发给林澄。", "event"},
	{"今天晚上八点提醒我带虚构松泉活动的门票。", "event"},
	{"请这次帮我整理虚构白沙会议的一份纪要，做完即可。", "event"},
	{"周五上午十点提醒我提交虚构月桥报告。", "event"},
}

func p3OrganizePrompt() string {
	at := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	memories := []organizeMemory{}
	for i, c := range p3RuleCases {
		memories = append(memories, organizeMemory{N: i + 1, Text: c.Text, ExpressedAt: &at})
	}
	return string(asJSON(map[string]any{"memories": memories, "groups": []organizeGroup{}, "timezone": "Asia/Shanghai", "refs": []memory.Ref{}, "rule": OrganizeVersion}))
}
func TestP3OrganizePromptLive(t *testing.T) {
	home, path := os.Getenv("PCAS_P3_CODEX_HOME"), os.Getenv("PCAS_P3_RAW_REPORT")
	if home == "" || path == "" {
		t.Skip("requires own Codex home and raw report path")
	}
	c, e := ai.NewCodex("", home)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	r := &ai.Registry{Codex: c, Config: ai.Configuration{Providers: []ai.Provider{{ID: "live", Protocol: "codex", Model: "gpt-6.1-sol", CostMode: "free", MaxOutput: 4096}}}}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	input := p3OrganizePrompt()
	start := time.Now()
	result, e := r.Generate(ctx, "live", organizeInstructions, input)
	raw, _ := json.MarshalIndent(map[string]any{"instructions": organizeInstructions, "input": json.RawMessage(input), "output": result.Text, "elapsed_ms": time.Since(start).Milliseconds()}, "", "  ")
	if e2 := os.WriteFile(path, raw, 0600); e2 != nil {
		t.Fatal(e2)
	}
	if e != nil {
		t.Fatal(e)
	}
	t.Logf("raw output: %s", result.Text)
	if os.Getenv("PCAS_P3_PROMPT_PHASE") == "before" {
		return
	}
	items, _ := parseOrganizeOutput(result.Text, len(p3RuleCases))
	for i, c := range p3RuleCases {
		if items[i+1].Category != c.Category {
			t.Errorf("case %d got=%s want=%s", i+1, items[i+1].Category, c.Category)
		}
	}
}
