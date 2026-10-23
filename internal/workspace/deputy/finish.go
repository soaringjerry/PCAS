package deputy

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/ai/siwc"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// What the owner reads of a run that did not finish. Interrupted is also
// how storage finds a run whose process ended without its result.
const (
	Interrupted     = "进程中断，结果和用量未确认；未自动重试"
	ContextChanged  = "记忆或授权已变化，请重新生成"
	TargetChanged   = "目标文档已改变或不存在，请重新发起修改"
	WriteIncomplete = "处理未完成；已保存的模型结果将复用，不会自动重复生成"
	// RevisionWriteRetry is a paid revision whose document is not written yet.
	RevisionWriteRetry = "模型结果已保存，文档写入失败，正在重试写入"
	callIncomplete     = "模型调用未完成，结果和用量可能未确认；请检查登录、额度与服务配置"
	changedBefore      = "生成前记忆或授权已变化，请重新生成"
	revoked            = "生成期间授权已撤回或资料已删除，请重新生成"
)

// Tier is the memory tier of a leased run: an unknown tier reads heavy.
func Tier(tier string) string {
	if tier != "light" && tier != "medium" && tier != "heavy" {
		return "heavy"
	}
	return tier
}

// Application is what became of a paid call of a run.
type Application struct {
	Outcome, Reason string
}

// FinishUnit checks a run at its end.
type FinishUnit interface {
	// PromptCurrent is false when what the brief holds changed.
	PromptCurrent(ctx context.Context, run workspace.Run) bool
	// Permitted is false when the agent may no longer use what the run used.
	Permitted(ctx context.Context, run workspace.Run) bool
}

// Ending is the end of a run as it is written: the run, its notice, what
// became of its answer and its self-check, and the failure of the
// self-check that is counted.
type Ending struct {
	Run          workspace.Run
	Notice       string
	Answer       Application
	Review       Application
	RecordReview bool
	// SelfcheckFailure is the reason of a self-check that was not taken, or empty.
	SelfcheckFailure string
}

// Finish ends a leased run. current is the run as stored; run is the run as
// the attempt built it. A run whose prompt changed is stale; a run whose
// access was revoked fails and loses its output. Groups of memory that the
// run had no time to read are named at the end of its output.
func Finish(ctx context.Context, u FinishUnit, current, run workspace.Run, end End, now time.Time) Ending {
	for _, ref := range run.ContextVersions {
		if !contains(run.ContextMemoryIDs, string(ref.ID)) {
			run.ContextMemoryIDs = append(run.ContextMemoryIDs, string(ref.ID))
		}
	}
	current.ContextMemoryIDs = run.ContextMemoryIDs
	current.ContextVersions = run.ContextVersions
	current.ProjectHandoverWrittenAt = run.ProjectHandoverWrittenAt
	current.MemoryGroups = run.MemoryGroups
	current.MemoryTier = run.MemoryTier
	current.MemoryContextRange = run.MemoryContextRange
	current.Brief = run.Brief
	current.FinishedAt = now.UTC().Format(time.RFC3339Nano)
	current.Status = "done"
	current.Output = end.Text
	current.Searches = end.Searches
	if end.GenerationErr != nil {
		current.Status = "failed"
		current.Error = callIncomplete
		if end.VerifyErr != nil {
			current.Error = changedBefore
		}
		var capability *ai.CapabilityError
		if errors.As(end.GenerationErr, &capability) {
			current.Error = "模型通道不支持必需能力：" + capability.Capability
		}
		var provider *siwc.ProviderError
		if errors.As(end.GenerationErr, &provider) {
			current.Error = provider.Message()
			current.ProviderError = asJSON(provider)
		}
	}
	current.Cost = end.Cost
	if !u.PromptCurrent(ctx, current) {
		current.StaleContext = true
	}
	if !u.Permitted(ctx, current) {
		current.StaleContext = true
		current.Status = "failed"
		current.Output = ""
		current.Error = revoked
	}
	coverage := end.Use.Coverage
	if current.Output != "" && coverage != nil && len(coverage.Skipped) > 0 {
		current.Output += "\n这几组没来得及看：" + strings.Join(coverage.Skipped, "、")
	}
	out := Ending{Run: current, Notice: NoticeReason(current), Answer: Application{Outcome: "applied"}, SelfcheckFailure: end.SelfcheckFallback}
	// Work handed off is reported back where reminders go, so the person who
	// asked from their phone hears how it ended without opening the page.
	if current.Status == "done" && coverage != nil && len(coverage.Notices) > 0 {
		out.Notice += "\n" + strings.Join(coverage.Notices, "\n")
	}
	if current.Status != "done" || current.StaleContext {
		out.Answer = Application{Outcome: "not_applicable", Reason: "run_failed_or_stale"}
	}
	if end.GenerationErr != nil && end.Saved != nil && end.Saved.ErrorCode != "" {
		out.Answer.Reason = end.Saved.ErrorCode
	}
	if end.GenerationErr == nil && run.MemoryTier != "light" {
		out.RecordReview = true
		out.Review = Application{Outcome: "applied", Reason: end.SelfcheckFallback}
		if end.SelfcheckFallback != "" {
			out.Review.Outcome = "skipped"
		}
		if current.Status != "done" {
			out.Review.Outcome = "not_applicable"
		}
	}
	if end.SelfcheckFallback != "" && current.Status == "done" {
		if end.SelfcheckFallback == "selfcheck_output_rejected" {
			out.Notice += "\n自查结果未采用，保留了原稿。"
		} else {
			out.Notice += "\n自查未完成，保留了原稿。"
		}
	}
	return out
}

func contains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}

// RevisionOutput reads the answer of a revision: its body, or why it is not
// a revision. A revision must say that its body is complete.
func RevisionOutput(output string) (body, rejection string) {
	var result struct {
		Body     string `json:"body"`
		Complete *bool  `json:"complete"`
	}
	switch {
	case json.Unmarshal([]byte(output), &result) != nil:
		return "", "改写输出格式不合格"
	case strings.TrimSpace(result.Body) == "":
		return result.Body, "改写正文为空"
	case result.Complete == nil || !*result.Complete:
		return result.Body, "副手未输出完整正文（截断或未完成）"
	}
	return result.Body, ""
}

// RevisionUnit checks a paid revision at its end.
type RevisionUnit interface {
	FinishUnit
	// Base is the body of the version that the revision started from; ok is
	// false when it is gone.
	Base(ctx context.Context, run workspace.Run) (body string, ok bool)
	// TargetCurrent is false when the document changed while the run worked.
	TargetCurrent(ctx context.Context, run workspace.Run) bool
}

// FinishRevision ends a revision whose answer was paid. A body that is not a
// revision, or that repeats its base, fails the run and is counted. A
// revision of a document that changed, or of an item that changed, keeps its
// result without adopting it. A revision whose access was revoked fails and
// loses its output.
func FinishRevision(ctx context.Context, u RevisionUnit, run workspace.Run, output string, cost float64, now time.Time) (ending Ending, invalid bool) {
	body, rejection := RevisionOutput(output)
	if base, ok := u.Base(ctx, run); !ok {
		rejection = "基准文档已删除或版本不存在"
	} else if strings.TrimSpace(body) == strings.TrimSpace(base) {
		rejection = "改写正文与基准完全相同"
	}
	run.Status = "done"
	run.Output = body
	run.FinishedAt = now.UTC().Format(time.RFC3339Nano)
	run.Cost = cost
	if rejection != "" {
		run.Status = "failed"
		run.Error = rejection
	} else if !u.TargetCurrent(ctx, run) {
		run.StaleContext = true
		run.Error = "目标文档在工作期间已改变；结果保留，未覆盖当前文档"
	} else if !u.PromptCurrent(ctx, run) {
		run.StaleContext = true
		run.Error = "事项在工作期间已改变；结果保留，未采纳"
	}
	if !u.Permitted(ctx, run) {
		run.StaleContext = true
		run.Status = "failed"
		run.Output = ""
		run.Error = "工作资料已删除或访问状态已改变"
	}
	ending = Ending{Run: run, Notice: NoticeReason(run), Answer: Application{Outcome: "applied"}}
	if run.Status != "done" || run.StaleContext {
		ending.Answer = Application{Outcome: "not_applicable", Reason: "revision_not_adopted"}
	}
	return ending, rejection != ""
}

// The words of generated text whose inputs were removed.
const (
	// RemovedText replaces generated text whose inputs were deleted.
	RemovedText = "已删除的生成内容"
	// MixedWriting is why an older text that mixes the owner's writing with
	// generated text was set aside.
	MixedWriting = "旧版文字混有生成内容，已隔离；请检查并单独保存自己的文字。"
)
