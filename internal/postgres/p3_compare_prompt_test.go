package postgres

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/ai"
)

type supersessionCase struct {
	Old, New   string
	Superseded bool
}

var p3SupersessionCases = []supersessionCase{
	{"虚构青羽项目的负责人是林澄。", "虚构青羽项目的预算是五万元。", false},
	{"虚构白沙项目周二做内部检查。", "虚构白沙项目周五做客户演示。", false},
	{"虚构晨湖项目的客户是纸鸢公司。", "虚构晨湖项目的供应商是山谷公司。", false},
	{"虚构红桥项目要求使用蓝色封面。", "虚构红桥项目要求正文使用黑色字体。", false},
	{"虚构灯塔方案由苏遥负责，我也叫它小灯。", "我把虚构灯塔方案改叫航标，仍由苏遥负责。", false},
	{"虚构新芽计划的工作是整理纸质档案。", "虚构新芽计划现在称为春芽计划，仍然整理纸质档案。", false},
	{"虚构白鹭报告要用简洁的文字。", "虚构白鹭报告要写得精炼，也要附上两张图片。", false},
	{"虚构松泉活动在阅览室举办。", "虚构松泉活动在阅览室举办，详细地点是阅览室二楼。", false},
	{"虚构月桥报告的提交期限是本周三。", "虚构月桥报告的提交期限改为本周五，原周三期限取消。", true},
	{"虚构星帆项目已决定采用 A 方案。", "虚构星帆项目现在决定改用 B 方案，放弃 A 方案。", true},
	{"虚构秋灯预约安排在10月12日15点。", "虚构秋灯预约改到10月13日16点，取消原来的预约时间。", true},
	{"虚构石溪项目的预算上限是三万元。", "虚构石溪项目的预算上限调整为五万元，三万元的上限作废。", true},
}

func p3ComparePrompt() (string, []compareEdge) {
	memories := []compareMemory{}
	expected := []compareEdge{}
	for i, c := range p3SupersessionCases {
		for j, text := range []string{c.Old, c.New} {
			at := time.Date(2026, 10, 7, 9+j, 0, i, 0, time.UTC)
			memories = append(memories, compareMemory{organizeMemory: organizeMemory{N: len(memories) + 1, Text: text, ExpressedAt: &at}})
		}
		if c.Superseded {
			expected = append(expected, compareEdge{Old: 2*i + 1, New: 2*i + 2, Kind: "superseded"})
		}
	}
	return string(asJSON(comparisonBatch{Rule: CompareVersion, Group: compareGroup{Key: "topic:fictional", Kind: "topic", Name: "虚构项目记录"}, Memories: memories})), expected
}

// Opt-in only; uses the executor's login and invented memories. Before changing
// instructions retain the exact instructions, input and raw model reply.
func TestP3ComparePromptLive(t *testing.T) {
	home := os.Getenv("PCAS_P3_CODEX_HOME")
	path := os.Getenv("PCAS_P3_RAW_REPORT")
	if home == "" || path == "" {
		t.Skip("requires own Codex home and raw report path")
	}
	c, err := ai.NewCodex("", home)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	r := &ai.Registry{Codex: c, Config: ai.Configuration{Providers: []ai.Provider{{ID: "live", Protocol: "codex", Model: "gpt-6.1-sol", CostMode: "free", MaxOutput: 4096}}}}
	input, expected := p3ComparePrompt()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	at := time.Now()
	result, err := r.Generate(ctx, "live", compareInstructions, input)
	raw, _ := json.MarshalIndent(map[string]any{"instructions": compareInstructions, "input": json.RawMessage(input), "output": result.Text, "elapsed_ms": time.Since(at).Milliseconds()}, "", "  ")
	if e := os.WriteFile(path, raw, 0600); e != nil {
		t.Fatal(e)
	}
	if err != nil {
		t.Fatal(err)
	}
	edges, valid := parseCompareOutput(result.Text, 24)
	t.Logf("raw output: %s", result.Text)
	if os.Getenv("PCAS_P3_PROMPT_PHASE") != "before" && (!valid || !reflect.DeepEqual(edges, expected)) {
		t.Fatalf("12-pair regression: got=%+v want=%+v valid=%v", edges, expected, valid)
	}
}
