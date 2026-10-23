package deputy

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/contextbuilder"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// checks is an in-memory RevisionUnit.
type checks struct {
	prompt, permitted, target, baseFound bool
	base                                 string
}

func current() *checks {
	return &checks{prompt: true, permitted: true, target: true, baseFound: true, base: "旧稿"}
}

func (c *checks) PromptCurrent(context.Context, workspace.Run) bool { return c.prompt }
func (c *checks) Permitted(context.Context, workspace.Run) bool     { return c.permitted }
func (c *checks) TargetCurrent(context.Context, workspace.Run) bool { return c.target }
func (c *checks) Base(context.Context, workspace.Run) (string, bool) {
	return c.base, c.baseFound
}

var at = time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

func TestAFinishedRunKeepsItsOutputAndSaysWhatItDidNotRead(t *testing.T) {
	run := workspace.Run{MemoryTier: "heavy", ContextVersions: []memory.Ref{{ID: "m", Version: 1}}}
	end := End{Text: "草稿", Use: contextbuilder.UseContext{Coverage: &contextbuilder.Coverage{Skipped: []string{"旅行"}, Notices: []string{"有一组没读完"}}}}
	e := Finish(context.Background(), current(), workspace.Run{Status: "running"}, run, end, at)
	if e.Run.Status != "done" || e.Run.Output != "草稿\n这几组没来得及看：旅行" || e.Run.ContextMemoryIDs[0] != "m" {
		t.Fatalf("%+v", e.Run)
	}
	if !strings.HasSuffix(e.Notice, "\n有一组没读完") || e.Answer.Outcome != "applied" || !e.RecordReview || e.Review.Outcome != "applied" {
		t.Fatalf("%+v", e)
	}
}

func TestARunWhoseAccessWasRevokedFailsWithoutItsOutput(t *testing.T) {
	c := current()
	c.permitted = false
	e := Finish(context.Background(), c, workspace.Run{}, workspace.Run{MemoryTier: "light"}, End{Text: "草稿"}, at)
	if e.Run.Status != "failed" || e.Run.Output != "" || !e.Run.StaleContext || e.Answer.Outcome != "not_applicable" || e.RecordReview {
		t.Fatalf("%+v", e)
	}
}

func TestAFailedCallSaysWhyAndKeepsTheJournalsReason(t *testing.T) {
	end := End{GenerationErr: errors.New("x"), VerifyErr: errors.New("y"), Saved: &CallRecord{ErrorCode: "provider_timeout"}}
	e := Finish(context.Background(), current(), workspace.Run{}, workspace.Run{MemoryTier: "heavy"}, end, at)
	if e.Run.Error != changedBefore || e.Answer.Reason != "provider_timeout" || e.RecordReview {
		t.Fatalf("%+v", e)
	}
}

func TestAnUntakenSelfcheckIsSaidAndCounted(t *testing.T) {
	e := Finish(context.Background(), current(), workspace.Run{}, workspace.Run{MemoryTier: "medium"}, End{Text: "稿", SelfcheckFallback: "selfcheck_output_rejected"}, at)
	if e.SelfcheckFailure != "selfcheck_output_rejected" || e.Review.Outcome != "skipped" || !strings.HasSuffix(e.Notice, "自查结果未采用，保留了原稿。") {
		t.Fatalf("%+v", e)
	}
}

func TestARevisionMustBeCompleteNewAndStillApply(t *testing.T) {
	for output, want := range map[string]string{
		"不是 JSON":                       "改写输出格式不合格",
		`{"body":" ","complete":true}`:  "改写正文为空",
		`{"body":"新稿"}`:                 "副手未输出完整正文（截断或未完成）",
		`{"body":"旧稿","complete":true}`: "改写正文与基准完全相同",
	} {
		e, invalid := FinishRevision(context.Background(), current(), workspace.Run{}, output, 0.1, at)
		if !invalid || e.Run.Status != "failed" || e.Run.Error != want {
			t.Errorf("%s: %+v", output, e.Run)
		}
	}
	c := current()
	c.target = false
	e, invalid := FinishRevision(context.Background(), c, workspace.Run{}, `{"body":"新稿","complete":true}`, 0.1, at)
	if invalid || e.Run.Status != "done" || !e.Run.StaleContext || e.Run.Output != "新稿" || e.Answer.Reason != "revision_not_adopted" {
		t.Fatalf("%+v", e)
	}
	e, _ = FinishRevision(context.Background(), current(), workspace.Run{}, `{"body":"新稿","complete":true}`, 0.1, at)
	if e.Run.Status != "done" || e.Run.StaleContext || e.Answer.Outcome != "applied" || e.Run.Cost != 0.1 {
		t.Fatalf("%+v", e)
	}
}
