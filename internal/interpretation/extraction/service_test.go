package extraction

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/prompts"
	"github.com/soaringjerry/PCAS/internal/worker"
)

// fake is the storage and the model of one extraction job.
type fake struct {
	source                  memory.SourceResult
	imported, stale, unpaid bool
	notConfigured, notReady bool
	answer                  string
	saved                   json.RawMessage
	modelInput              string
	result                  *Result
	starts                  []int
	log                     []string
}

func (f *fake) Source(context.Context, worker.Job) (memory.SourceResult, bool, error) {
	return f.source, f.imported, nil
}
func (f *fake) Current(context.Context, worker.Job) (bool, error) { return !f.stale, nil }
func (f *fake) FinishEmpty(context.Context, worker.Job) error {
	f.log = append(f.log, "empty")
	return nil
}
func (f *fake) Conversation(_ context.Context, _ worker.Job, conversation string) error {
	f.log = append(f.log, "conversation "+conversation)
	return nil
}
func (f *fake) SplitWindows(_ context.Context, _ worker.Job, starts []int, _ bool) error {
	f.starts = starts
	return nil
}
func (f *fake) Configured() error {
	if f.notConfigured {
		return &worker.JobError{Code: "provider_not_configured"}
	}
	return nil
}
func (f *fake) Ready() error {
	if f.notReady {
		return &worker.JobError{Code: "provider_unavailable", Retry: true}
	}
	return nil
}
func (f *fake) Input(context.Context, worker.Job, memory.SourceResult) (Input, error) {
	return Input{Project: "p1", Timezone: "Asia/Singapore", Adjacent: []map[string]any{{"role": "assistant", "text": "虚构的松木公司怎么样？"}}, Conditions: []map[string]string{}}, nil
}
func (f *fake) Unpaid(string) bool { return f.unpaid }
func (f *fake) DiscardResult(context.Context, worker.Job) error {
	f.log = append(f.log, "discarded")
	return nil
}
func (f *fake) Apply(_ context.Context, _ worker.Job, result Result) error {
	f.result = &result
	f.log = append(f.log, "applied")
	return nil
}
func (f *fake) Failed(context.Context, worker.Job) error {
	f.log = append(f.log, "failed")
	return nil
}
func (f *fake) GenerateForJob(_ context.Context, _ worker.Job, purpose string, _ prompts.Definition, modelInput string, record json.RawMessage, _ []memory.Ref) (json.RawMessage, string, error) {
	f.modelInput = modelInput
	f.log = append(f.log, "call "+purpose)
	if f.saved != nil {
		return f.saved, f.answer, nil
	}
	return record, f.answer, nil
}

func source(connector, text string, context *memory.SourceContext) memory.SourceResult {
	s := memory.SourceResult{Context: context}
	s.Source.Connector, s.Source.Text = connector, text
	return s
}

func run(f *fake, job worker.Job) error {
	return Service{Store: f, Generator: f, MaxAttempts: 5}.Process(context.Background(), job)
}

var job = worker.Job{Stage: Stage}

func TestExtractionSendsTheSourceAndAppliesTheAnswer(t *testing.T) {
	f := &fake{source: source("capture", "我在虚构的松木公司工作。", nil), answer: `{"signals":[],"items":[{"kind":"memory","text":"在松木公司工作","nature":"fact","subject":"我","predicate":"工作","quote":"松木公司工作","confidence":0.9,"explicit":true,"acquisition":"direct"}]}`}
	if err := run(f, job); err != nil {
		t.Fatal(err)
	}
	want := `{"adjacent_messages":[{"role":"assistant","text":"虚构的松木公司怎么样？"}],"expressed_at":"0001-01-01T00:00:00Z","pending_conditions":[],"source":"我在虚构的松木公司工作。","source_context":null,"source_project_id":"p1","timezone":"Asia/Singapore"}`
	if f.modelInput != want {
		t.Fatal(f.modelInput)
	}
	r := f.result
	if r == nil || r.Project != "p1" || len(r.Output.Items) != 1 || r.Location.String() != "Asia/Singapore" || r.MentionText != "我在虚构的松木公司工作。\n虚构的松木公司怎么样？" || !reflect.DeepEqual(f.log, []string{"call extraction", "applied"}) {
		t.Fatalf("%+v %v", r, f.log)
	}
}

func TestSourcesWithNothingToExtractMakeNoCall(t *testing.T) {
	for name, f := range map[string]*fake{
		"what the system wrote":        {source: source("actions", "虚构的记录", nil)},
		"an imported answer of the AI": {source: source("archive", "虚构的回答", &memory.SourceContext{Role: "assistant"}), imported: true},
	} {
		if err := run(f, job); err != nil || !reflect.DeepEqual(f.log, []string{"empty"}) {
			t.Fatal(name, err, f.log)
		}
	}
	f := &fake{source: source("capture", "虚构", nil), stale: true}
	if err := run(f, job); err != nil || len(f.log) != 0 {
		t.Fatal("a replaced source version is not read", err, f.log)
	}
	f = &fake{source: source("archive", "虚构", &memory.SourceContext{Role: "user", Conversation: "c1"}), imported: true}
	if err := run(f, job); err != nil || !reflect.DeepEqual(f.log, []string{"conversation c1"}) {
		t.Fatal(err, f.log)
	}
}

func TestLongSourceIsSplitAndEachWindowReadsItsOwnText(t *testing.T) {
	long := strings.Repeat("云", Window+10)
	f := &fake{source: source("capture", long, nil)}
	if err := run(f, job); err != nil || !reflect.DeepEqual(f.starts, []int{0, Step}) || len(f.log) != 0 {
		t.Fatal(err, f.starts, f.log)
	}
	f = &fake{source: source("capture", long, nil), answer: `{"signals":[],"items":[]}`}
	if err := run(f, worker.Job{Stage: Stage + ":11000"}); err != nil || len([]rune(f.result.Source.Source.Text)) != Window+10-Step || f.result.MentionText[:len(long)] != long {
		t.Fatal(err)
	}
	if err := run(&fake{source: source("capture", long, nil)}, worker.Job{Stage: Stage + ":999999"}); err != memory.ErrInvalid {
		t.Fatal(err)
	}
}

func TestAnInvalidAnswerIsDiscardedAndOnlyAnUnpaidCallIsRepeated(t *testing.T) {
	for unpaid, wantLog := range map[bool][]string{false: {"call extraction", "discarded", "failed"}, true: {"call extraction", "discarded"}} {
		f := &fake{source: source("capture", "虚构", nil), answer: "虚构的散文", unpaid: unpaid}
		err := run(f, job)
		var failure *worker.JobError
		if !errors.As(err, &failure) || failure.Code != "model_output_invalid" || failure.Retry != unpaid || !reflect.DeepEqual(f.log, wantLog) {
			t.Fatal(unpaid, err, f.log)
		}
	}
}

func TestAStoppedJobMarksTheSourceAndADeferralDoesNot(t *testing.T) {
	f := &fake{source: source("capture", "虚构", nil), notConfigured: true}
	if err := run(f, job); err == nil || !reflect.DeepEqual(f.log, []string{"failed"}) {
		t.Fatal(err, f.log)
	}
	f = &fake{source: source("capture", "虚构", nil), notReady: true}
	if err := run(f, job); err == nil || len(f.log) != 0 {
		t.Fatal("a job that the queue tries again is not marked", f.log)
	}
	if err := run(f, worker.Job{Stage: Stage, Attempts: 5}); err == nil || !reflect.DeepEqual(f.log, []string{"failed"}) {
		t.Fatal("the last attempt marks the source", f.log)
	}
}

func TestARecoveredResultKeepsTheProjectOfItsCall(t *testing.T) {
	f := &fake{source: source("capture", "虚构", nil), answer: `{"signals":[],"items":[]}`, saved: json.RawMessage(`{"rawPrompt":"{\"source_project_id\":\"old\"}"}`)}
	if err := run(f, job); err != nil || f.result.Project != "old" {
		t.Fatal(err, f.result)
	}
}

func TestDecisionRules(t *testing.T) {
	text := "我明天要交报告。我喜欢茶。虚构的陈松在松木公司。"
	desk := Facts{Source: source("desk", text, nil), MentionText: text}
	memoryItem := Item{Kind: "memory", Nature: "preference", Text: "喜欢茶", Subject: "我", Quote: "我喜欢茶", Confidence: 0.9, Acquisition: "direct"}
	if d := Decide(memoryItem, desk); d.Action != Remember || d.Confirmation != "adopted" || d.SubjectType != "self" {
		t.Fatalf("%+v", d)
	}
	// The secretary owns the tasks of its own turn.
	task := Item{Kind: "task", Text: "交报告", Quote: "明天要交报告", Confidence: 0.9}
	if d := Decide(task, desk); d.Action != Skip {
		t.Fatal("desk task", d.Action)
	}
	capture := Facts{Source: source("capture", text, nil), MentionText: text}
	if d := Decide(task, capture); d.Action != Capture {
		t.Fatal("capture task", d.Action)
	}
	// An undone turn leaves a statement only when the turn asked to remember, and never a plan.
	undone := desk
	undone.Undone = true
	if d := Decide(memoryItem, undone); d.Action != Skip {
		t.Fatal("undone", d.Action)
	}
	undone.Remembered = true
	plan := memoryItem
	plan.Nature = "plan"
	if Decide(memoryItem, undone).Action != Remember || Decide(plan, undone).Action != Skip {
		t.Fatal("undone and remembered")
	}
	// An archive is history: its statements wait for review.
	imported := capture
	imported.Imported = true
	if d := Decide(memoryItem, imported); d.Action != Remember || d.Confirmation != "candidate" {
		t.Fatalf("%+v", d)
	}
	other := Item{Kind: "memory", Nature: "fact", Text: "陈松在松木公司", Subject: "陈松", Quote: "陈松在松木公司", Confidence: 0.9, Acquisition: "direct", People: []string{"陈松", "不存在的人"}, Organizations: []string{"松木公司"}}
	if d := Decide(other, capture); d.SubjectType != "person" || d.Subject != "陈松" || !reflect.DeepEqual(d.Mentions, []Mention{{"陈松", "person"}, {"松木公司", "organization"}}) {
		t.Fatalf("%+v", d)
	}
	if d := Decide(Item{Kind: "unknown", Text: "虚构", Quote: "我喜欢茶", Confidence: 0.2}, capture); d.Action != Candidate {
		t.Fatal(d.Action)
	}
	if d := Decide(Item{Kind: "memory", Nature: "fact", Text: "虚构", Quote: "源文中没有", Confidence: 0.9}, capture); d.Action != Skip {
		t.Fatal(d.Action)
	}
	answer := Output{Signals: []Signal{{IdeaID: "i"}}}
	if len(UsableSignals(answer, capture.Source, false)) != 1 || UsableSignals(answer, capture.Source, true) != nil || UsableSignals(answer, source("capture", text, &memory.SourceContext{Role: "user", Branch: "historical"}), false) != nil {
		t.Fatal("only the owner's current words wake an idea")
	}
}
