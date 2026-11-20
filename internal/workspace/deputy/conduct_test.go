package deputy

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/contextbuilder"
	"github.com/soaringjerry/PCAS/internal/contextbuilder/reader"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/modelcall"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// fakeRun is the storage and the model of one run.
type fakeRun struct {
	run         workspace.Run
	saved       *CallRecord
	inputSaved  bool
	ready       bool
	stale       error
	answer      string
	answerErr   error
	checked     string
	checkErr    error
	selection   *Selection
	attempted   bool
	log         []string
	end         *End
	savedChoice *Selection
	settled     *float64
}

func (f *fakeRun) Owner() memory.ID    { return "owner" }
func (f *fakeRun) Token() string       { return "token" }
func (f *fakeRun) Run() *workspace.Run { return &f.run }
func (f *fakeRun) Started() time.Time  { return time.Now() }
func (f *fakeRun) Saved() *CallRecord  { return f.saved }
func (f *fakeRun) InputSaved() bool    { return f.inputSaved }
func (f *fakeRun) SavedContext() (bool, *contextbuilder.Coverage) {
	return true, &contextbuilder.Coverage{}
}
func (f *fakeRun) Prepare(context.Context) (contextbuilder.UseContext, workspace.Agent, error) {
	f.log = append(f.log, "prepare")
	return contextbuilder.UseContext{Ready: f.ready, Coverage: &contextbuilder.Coverage{}}, workspace.Agent{ID: "a"}, nil
}
func (f *fakeRun) VerifyPrompt(context.Context) error { return f.stale }
func (f *fakeRun) Preparing()                         {}
func (f *fakeRun) Prepared()                          {}
func (f *fakeRun) Call(_ context.Context, request modelcall.Request) (*CallRecord, error) {
	return &CallRecord{ID: memory.ID("call-" + request.Stage), Reservation: "r"}, nil
}
func (f *fakeRun) FinishRevision(context.Context, *modelcall.PaidResult) error {
	f.log = append(f.log, "revision written")
	return nil
}
func (f *fakeRun) SettleCost(_ context.Context, cost float64) error {
	f.settled = &cost
	return nil
}
func (f *fakeRun) Selection(context.Context, memory.ID) (*Selection, string, error) {
	if f.selection == nil {
		return nil, "", nil
	}
	return f.selection, "虚构的已选文本", nil
}
func (f *fakeRun) ReviewAttempted(context.Context) (memory.ID, bool, error) {
	return "earlier-review", f.attempted, nil
}
func (f *fakeRun) VerifySelfcheck(context.Context) error { return nil }
func (f *fakeRun) SaveSelection(_ context.Context, _ modelcall.Request, selection Selection) error {
	f.savedChoice = &selection
	return nil
}
func (f *fakeRun) Finish(_ context.Context, end End) error {
	f.end = &end
	f.log = append(f.log, "finished")
	return nil
}

// answerTime is how long the fake answer takes. The self-check gets the
// answer's time as its budget, so an instant answer would leave it none.
const answerTime = 50 * time.Millisecond

func (f *fakeRun) Answer(context.Context, modelcall.Request) (*modelcall.PaidResult, error) {
	time.Sleep(answerTime)
	f.log = append(f.log, "answer")
	if f.answerErr != nil {
		return nil, f.answerErr
	}
	return &modelcall.PaidResult{Output: f.answer, InvocationID: "paid-answer", Cost: 0.25}, nil
}
func (f *fakeRun) Selfcheck(context.Context, modelcall.Request) (*modelcall.PaidResult, error) {
	f.log = append(f.log, "selfcheck")
	if f.checkErr != nil {
		return nil, f.checkErr
	}
	return &modelcall.PaidResult{Output: f.checked}, nil
}
func (f *fakeRun) Read(context.Context, context.Context, reader.Task, contextbuilder.UseContext) ([]workspace.Memory, []memory.Ref, []string) {
	f.log = append(f.log, "read")
	return nil, []memory.Ref{{ID: "m1", Version: 1}}, []string{"self:goal"}
}

func conduct(f *fakeRun) error {
	return Conduct(context.Background(), context.Background(), f, Steps{Reader: f, Calls: f}, func(stage string, _ error) string { return stage + "_error" })
}

func TestLightRunIsAnsweredOnceWithoutASelfcheck(t *testing.T) {
	f := &fakeRun{run: workspace.Run{ID: "r1", MemoryTier: "light", Brief: "虚构的任务"}, answer: "虚构的回答"}
	if err := conduct(f); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.log, []string{"prepare", "answer", "finished"}) || f.end.Text != "虚构的回答" || f.end.GenerationErr != nil || f.end.Answer != "call-answer" || f.end.Review != "" {
		t.Fatal(f.log, f.end)
	}
	// The gateway linked a reservation to the call, so the run's admission is not settled again.
	if f.settled != nil || f.savedChoice == nil || f.savedChoice.Output != "call-answer" {
		t.Fatal(f.settled, f.savedChoice)
	}
}

func TestHeavyRunReadsMemoryFirstAndTakesTheCheckedText(t *testing.T) {
	draft := "1. 虚构步骤甲\n2. 虚构步骤乙"
	f := &fakeRun{run: workspace.Run{ID: "r1", MemoryTier: "heavy", Brief: "虚构的任务"}, ready: true, answer: draft, checked: "1. 虚构步骤甲（已查）\n2. 虚构步骤乙"}
	if err := conduct(f); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.log, []string{"prepare", "read", "answer", "selfcheck", "finished"}) || !reflect.DeepEqual(f.run.MemoryGroups, []string{"self:goal"}) || len(f.run.ContextVersions) != 1 {
		t.Fatal(f.log, f.run.MemoryGroups, f.run.ContextVersions)
	}
	if f.end.SelfcheckFallback != "" || f.end.Review != "call-selfcheck" || f.savedChoice.Output != "call-selfcheck" || f.end.Text == draft {
		t.Fatal(f.end, f.savedChoice)
	}
}

func TestHeavyRunWithoutBuiltMemoryReadsAtTheMediumTier(t *testing.T) {
	f := &fakeRun{run: workspace.Run{ID: "r1", MemoryTier: "heavy"}, answer: "虚构的回答", checked: "虚构的回答"}
	if err := conduct(f); err != nil || f.run.MemoryTier != "medium" {
		t.Fatal(err, f.run.MemoryTier)
	}
	for _, step := range f.log {
		if step == "read" {
			t.Fatal("memory that is not built was read by readers", f.log)
		}
	}
}

func TestSelfcheckThatFailsKeepsTheDraftAndSaysWhy(t *testing.T) {
	f := &fakeRun{run: workspace.Run{ID: "r1", MemoryTier: "medium"}, answer: "虚构的草稿", checkErr: errors.New("fictitious failure")}
	if err := conduct(f); err != nil {
		t.Fatal(err)
	}
	if f.end.Text != "虚构的草稿" || f.end.SelfcheckFallback == "" || f.savedChoice.Output != "call-answer" || f.savedChoice.Fallback != f.end.SelfcheckFallback {
		t.Fatal(f.end, f.savedChoice)
	}
}

func TestRunWhoseContextChangedMakesNoCall(t *testing.T) {
	f := &fakeRun{run: workspace.Run{ID: "r1", MemoryTier: "medium"}, stale: memory.ErrConflict}
	if err := conduct(f); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.log, []string{"prepare", "finished"}) || !errors.Is(f.end.VerifyErr, memory.ErrConflict) || !errors.Is(f.end.GenerationErr, memory.ErrConflict) || f.settled == nil || *f.settled != 0 {
		t.Fatal(f.log, f.end, f.settled)
	}
}

func TestResumedRunNeverRepeatsItsPaidCalls(t *testing.T) {
	// An earlier attempt failed before it saved its input: the run ends as failed without a call.
	f := &fakeRun{run: workspace.Run{ID: "r1", MemoryTier: "medium"}, saved: &CallRecord{ID: "earlier", ErrorCode: "provider_unavailable", Reservation: "r"}}
	if err := conduct(f); err != nil {
		t.Fatal(err)
	}
	var failure *modelcall.Failure
	if !reflect.DeepEqual(f.log, []string{"finished"}) || !errors.As(f.end.GenerationErr, &failure) || failure.Code != "provider_unavailable" || f.end.Answer != "earlier" {
		t.Fatal(f.log, f.end)
	}
	// An earlier attempt saved its input: the brief is not built again. Its self-check was started, so it is not made again.
	f = &fakeRun{run: workspace.Run{ID: "r1", MemoryTier: "medium", Brief: "虚构的已存任务"}, saved: &CallRecord{ID: "earlier", Reservation: "r"}, inputSaved: true, answer: "虚构的回答", attempted: true}
	if err := conduct(f); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.log, []string{"answer", "finished"}) || f.end.SelfcheckFallback != "selfcheck_interrupted" || f.end.Review != "earlier-review" || f.end.Text != "虚构的回答" {
		t.Fatal(f.log, f.end)
	}
	// A saved selection is taken as it is.
	f = &fakeRun{run: workspace.Run{ID: "r1", MemoryTier: "medium"}, answer: "虚构的回答", selection: &Selection{Main: "call-answer", Output: "review-1", Review: "review-1"}}
	if err := conduct(f); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.log, []string{"prepare", "answer", "finished"}) || f.end.Text != "虚构的已选文本" || f.end.Review != "review-1" {
		t.Fatal(f.log, f.end)
	}
}

func TestRevisionIsWrittenFromItsPaidAnswerAndAnAccountingFailureEndsTheRun(t *testing.T) {
	f := &fakeRun{run: workspace.Run{ID: "r1", Kind: "revise", MemoryTier: "light"}, answer: `{"body":"虚构的新稿"}`}
	if err := conduct(f); err != nil || !reflect.DeepEqual(f.log, []string{"prepare", "answer", "revision written"}) {
		t.Fatal(err, f.log)
	}
	failure := &modelcall.PersistenceError{Err: errors.New("fictitious write failure")}
	f = &fakeRun{run: workspace.Run{ID: "r1", MemoryTier: "light"}, answerErr: failure}
	if err := conduct(f); !errors.Is(err, failure) || f.end != nil {
		t.Fatal(err, f.log)
	}
}
