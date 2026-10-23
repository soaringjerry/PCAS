// Package activity puts background work into the owner's words: the jobs
// list of the library, the home page's "while you were away" lines, and
// the notes of the configured agents. The raw stage names and error codes
// stay in the database and the logs. Storage reads the rows.
package activity

import (
	"fmt"
	"strings"
	"time"

	"github.com/soaringjerry/PCAS/internal/workspace"
)

// JobTitle is what a processing stage does, in the owner's words.
func JobTitle(stage string) string {
	switch strings.SplitN(stage, ":", 2)[0] {
	case "source.parse":
		return "读取附件"
	case "source.chunk":
		return "切分原文"
	case "source.extract":
		return "从资料里读出要点"
	case "source.embed", "memory.embed":
		return "建立语义索引"
	case "source.tokenize", "memory.index":
		return "建立检索索引"
	case "memory.summary":
		return "更新摘要"
	}
	return "后台整理"
}

// JobProblem is why a job did not finish, in the owner's words.
func JobProblem(code string) string {
	switch code {
	case "provider_not_configured", "handler_not_configured":
		return "还没配置处理它的模型"
	case "provider_unavailable":
		return "模型通道暂时不可用；稍后会自动再试，若已停住请检查连接后点重试"
	case "budget_deferred":
		return "今天的预算已用完，明天会继续；需要现在处理可提高每日预算"
	case "archive_redacted", "source_unavailable":
		return "原文已经删除"
	case "attempts_exhausted", "processing_failed":
		return "试了几次没成功"
	case "model_call_failed":
		return "模型调用没成功"
	case "model_output_invalid":
		return "模型的输出没看懂"
	}
	return "没做完"
}

// Job is one processing job as the library lists it. source is the title of
// its source, or empty. A blocked job says how it can go on.
func Job(id, stage, state, code, source string, created, next time.Time) workspace.Job {
	j := workspace.Job{ID: id, Title: JobTitle(stage), Trigger: "资料处理", CreatedAt: created.Format(time.RFC3339Nano)}
	j.Status = map[string]string{"done": "done", "queued": "queued", "leased": "running", "blocked": "failed", "failed": "failed"}[state]
	var detail []string
	if source != "" {
		detail = append(detail, "「"+source+"」")
	}
	if code != "" {
		detail = append(detail, JobProblem(code))
	}
	j.Detail = strings.Join(detail, " · ")
	if state == "queued" {
		j.NextRunAt = next.Format(time.RFC3339Nano)
	}
	if state == "blocked" {
		j.Recovery = "配置对应模型或处理器后重试"
		if code == "model_call_failed" || code == "model_output_invalid" {
			j.Recovery = "可以直接重试"
		}
	}
	return j
}

// NoticeJob is a reminder that was made, as the library lists it.
func NoticeJob(id, reason string, created time.Time) workspace.Job {
	return workspace.Job{ID: id, Title: "事项提醒", Trigger: "条件与时间", Status: "done", Detail: reason, CreatedAt: created.Format(time.RFC3339Nano)}
}

// ReviewJob is a daily review, as the library lists it.
func ReviewJob(id string, pending int, created time.Time) workspace.Job {
	return workspace.Job{ID: id, Title: "每日整理", Trigger: "定时检查", Status: "done", Detail: fmt.Sprintf("有 %d 条拿不准的等你确认", pending), CreatedAt: created.Format(time.RFC3339Nano)}
}

// AgentNote is the line under a configured agent: its channel and model.
func AgentNote(protocol, model string) string {
	switch protocol {
	case "codex":
		return "ChatGPT 订阅 · " + model
	case "siwc":
		return ""
	}
	return "通用 API · " + model
}

// SourcesFinished is the line of the sources finished on one day.
func SourcesFinished(day string, at time.Time, count int, title string) workspace.Activity {
	text := "整理好了「" + title + "」"
	if count > 1 {
		text = fmt.Sprintf("整理好了「%s」等 %d 份资料", title, count)
	}
	return workspace.Activity{ID: "sources:" + day, At: at.UTC().Format(time.RFC3339Nano), Text: text, To: "/library?tab=sources"}
}

// Failure is the count of jobs that stopped with one code.
type Failure struct {
	Code  string
	Count int
	At    time.Time
}

// Problems are the lines of the jobs that stopped, one for each reason.
// Several codes share a reason.
func Problems(failures []Failure) []workspace.Activity {
	latest := map[string]time.Time{}
	counts := map[string]int{}
	for _, f := range failures {
		reason := JobProblem(f.Code)
		counts[reason] += f.Count
		if f.At.After(latest[reason]) {
			latest[reason] = f.At
		}
	}
	out := []workspace.Activity{}
	for reason, at := range latest {
		out = append(out, workspace.Activity{ID: "problem:" + reason, At: at.UTC().Format(time.RFC3339Nano), Text: fmt.Sprintf("有 %d 项整理没做完：%s", counts[reason], reason), To: "/library?tab=sources", Failed: true})
	}
	return out
}

// Reminded is the line of a reminder of a task.
func Reminded(thing, trigger string, due time.Time, title, reason string, at time.Time) workspace.Activity {
	return workspace.Activity{ID: "notice:" + thing + ":" + trigger + ":" + due.UTC().Format(time.RFC3339), At: at.UTC().Format(time.RFC3339Nano), Text: "提醒了你「" + title + "」：" + reason, To: "/t/" + thing}
}

// Reviewed is the line of a daily review that left memories to confirm.
func Reviewed(due time.Time, pending int, at time.Time) workspace.Activity {
	return workspace.Activity{ID: "review:" + due.UTC().Format(time.RFC3339), At: at.UTC().Format(time.RFC3339Nano), Text: fmt.Sprintf("每日整理：有 %d 条拿不准的等你确认", pending)}
}
