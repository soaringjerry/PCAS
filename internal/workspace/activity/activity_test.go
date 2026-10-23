package activity

import (
	"testing"
	"time"
)

var at = time.Date(2026, 3, 1, 8, 0, 0, 0, time.UTC)

func TestFailuresAreOneLineForEachReason(t *testing.T) {
	lines := Problems([]Failure{{Code: "provider_not_configured", Count: 2, At: at}, {Code: "handler_not_configured", Count: 1, At: at.Add(time.Hour)}, {Code: "model_call_failed", Count: 1, At: at}})
	if len(lines) != 2 {
		t.Fatalf("%+v", lines)
	}
	for _, line := range lines {
		if line.ID == "problem:还没配置处理它的模型" && (line.Text != "有 3 项整理没做完：还没配置处理它的模型" || line.At != at.Add(time.Hour).Format(time.RFC3339Nano)) {
			t.Fatalf("%+v", line)
		}
	}
}

func TestAJobSaysWhatItDoesAndHowABlockedOneGoesOn(t *testing.T) {
	j := Job("j", "source.extract:12", "blocked", "model_output_invalid", "周报", at, at)
	if j.Title != "从资料里读出要点" || j.Status != "failed" || j.Detail != "「周报」 · 模型的输出没看懂" || j.Recovery != "可以直接重试" || j.NextRunAt != "" {
		t.Fatalf("%+v", j)
	}
	if q := Job("q", "memory.summary", "queued", "", "", at, at.Add(time.Hour)); q.Status != "queued" || q.NextRunAt == "" || q.Detail != "" {
		t.Fatalf("%+v", q)
	}
}

func TestTheLinesOfAwayTimeNameWhatHappened(t *testing.T) {
	if a := SourcesFinished("2026-03-01", at, 3, "合同"); a.Text != "整理好了「合同」等 3 份资料" || a.ID != "sources:2026-03-01" {
		t.Fatalf("%+v", a)
	}
	if a := SourcesFinished("2026-03-01", at, 1, "合同"); a.Text != "整理好了「合同」" {
		t.Fatalf("%+v", a)
	}
	if a := Reminded("t", "due", at, "交房租", "到期了", at); a.To != "/t/t" || a.Text != "提醒了你「交房租」：到期了" {
		t.Fatalf("%+v", a)
	}
	if AgentNote("codex", "gpt") != "ChatGPT 订阅 · gpt" || AgentNote("siwc", "x") != "" || AgentNote("openai", "m") != "通用 API · m" {
		t.Fatal("agent notes")
	}
}
