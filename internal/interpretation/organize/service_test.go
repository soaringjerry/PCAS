package organize

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/prompts"
	"github.com/soaringjerry/PCAS/internal/worker"
)

// fake is the storage and the model of one organize job.
type fake struct {
	notReady, noBatch bool
	saved             json.RawMessage
	output            string
	calls             int
	record            json.RawMessage
	refs              []memory.Ref
	result            *Result
	log               []string
}

func (f *fake) Session(context.Context) (Session, func(), error) {
	return f, func() { f.log = append(f.log, "released") }, nil
}
func (f *fake) Ready() error {
	if f.notReady {
		return &worker.JobError{Code: "provider_unavailable", NoAttempt: true}
	}
	return nil
}
func (f *fake) NextBatch(context.Context, worker.Job, int) (Batch, bool, error) {
	if f.noBatch {
		return Batch{}, false, nil
	}
	at := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	return Batch{Timezone: "Asia/Singapore", Groups: []Group{{Type: "area", Name: "健康", ID: "g"}}, Memories: []Memory{
		{N: 1, Text: "虚构的云杉每周二游泳。", ExpressedAt: &at, Ref: memory.Ref{ID: "a", Version: 1, Kind: memory.ClaimKind}},
		{N: 2, Text: "虚构的云杉想学陶艺。", Ref: memory.Ref{ID: "b", Version: 3, Kind: memory.ClaimKind}},
	}}, true, nil
}
func (f *fake) Apply(_ context.Context, _ worker.Job, result Result) (Applied, error) {
	f.result = &result
	f.log = append(f.log, "applied")
	return Applied{}, nil
}
func (f *fake) HasSavedResult(context.Context, worker.Job) (bool, error) { return f.saved != nil, nil }
func (f *fake) GenerateForJob(_ context.Context, _ worker.Job, purpose string, _ prompts.Definition, modelInput string, record json.RawMessage, refs []memory.Ref) (json.RawMessage, string, error) {
	if purpose != "organize" || modelInput != "" {
		panic("the organize call sends its record as the model input")
	}
	if f.saved != nil {
		return f.saved, f.output, nil
	}
	f.calls++
	f.record, f.refs = record, refs
	return record, f.output, nil
}

var organizeJob = worker.Job{Stage: Stage + ":3:nonce"}

func service(f *fake) Service { return Service{Store: f, Generator: f, Version: 3} }

func TestOrganizeSendsTheBatchAndAppliesTheValidItems(t *testing.T) {
	f := &fake{output: `{"items":[{"n":1,"category":"event","durable":true,"area":"健康","topics":["游泳"],"deadlines":[]},{"n":2,"category":"nonsense","durable":true,"deadlines":[]}],"new":[{"type":"topic","name":"游泳","desc":"虚构"}]}`}
	if err := service(f).Process(context.Background(), organizeJob); err != nil {
		t.Fatal(err)
	}
	want := `{"groups":[{"type":"area","name":"健康","desc":""}],"memories":[{"n":1,"text":"虚构的云杉每周二游泳。","expressedAt":"2026-03-01T00:00:00Z"},{"n":2,"text":"虚构的云杉想学陶艺。","expressedAt":null}],"refs":[{"id":"a","version":1,"kind":"claim"},{"id":"b","version":3,"kind":"claim"}],"rule":3,"timezone":"Asia/Singapore"}`
	if f.calls != 1 || string(f.record) != want || len(f.refs) != 2 {
		t.Fatalf("calls %d, record %s", f.calls, f.record)
	}
	r := f.result
	if r == nil || r.Version != 3 || r.Timezone != "Asia/Singapore" || len(r.Memories) != 2 || r.Memories[1].Ref.ID != "b" || r.Memories[1].Ref.Version != 3 {
		t.Fatalf("%+v", r)
	}
	if _, invalid := r.Items[2]; invalid || r.Items[1].Category != "event" || len(r.Items[1].Groups) != 2 || len(r.Declared) != 1 {
		t.Fatalf("%+v", r)
	}
	if !reflect.DeepEqual(f.log, []string{"applied", "released"}) {
		t.Fatal(f.log)
	}
}

func TestOrganizeWithoutAProviderOrABatchMakesNoCall(t *testing.T) {
	f := &fake{notReady: true}
	if err := service(f).Process(context.Background(), organizeJob); err == nil || f.calls != 0 || f.result != nil {
		t.Fatal(err, f.calls)
	}
	f = &fake{noBatch: true}
	if err := service(f).Process(context.Background(), organizeJob); err != nil || f.calls != 0 || f.result != nil || !reflect.DeepEqual(f.log, []string{"released"}) {
		t.Fatal(err, f.calls, f.log)
	}
}

func TestSavedOrganizeResultIsAppliedWithItsOwnBatchAndRule(t *testing.T) {
	f := &fake{notReady: true, noBatch: true, output: `{"items":[{"n":1,"category":"goal","durable":true,"deadlines":[]}]}`,
		saved: json.RawMessage(`{"memories":[{"n":1,"text":"虚构的旧批次。","expressedAt":null}],"refs":[{"id":"old","version":2,"kind":"claim"}],"timezone":"UTC"}`)}
	if err := service(f).Process(context.Background(), organizeJob); err != nil {
		t.Fatal(err)
	}
	// A result that was saved without a rule version used rule two.
	if f.calls != 0 || f.result == nil || f.result.Version != 2 || f.result.Memories[0].Ref.ID != "old" || f.result.Items[1].Category != "goal" {
		t.Fatalf("%d %+v", f.calls, f.result)
	}
	f.saved = json.RawMessage(`{"memories":[{"n":1}],"refs":[],"rule":3}`)
	if err := service(f).Process(context.Background(), organizeJob); err != memory.ErrInvalid {
		t.Fatal(err)
	}
}

func TestOnlyADeclaredGroupThatACurrentStatementUsesIsCreated(t *testing.T) {
	items := map[int]Item{
		1: {Groups: []Group{{Type: "topic", Name: "游泳"}}},
		2: {Groups: []Group{{Type: "topic", Name: "陶艺"}}},
	}
	declared := []Group{{Type: "topic", Name: " 游泳 ", Description: "虚构"}, {Type: "topic", Name: "陶艺"}, {Type: "topic", Name: "无人使用"}, {Type: "person", Name: "游泳"}, {Type: "topic", Name: "  "}}
	got := UsedDeclarations(declared, items, map[int]bool{1: true})
	if !reflect.DeepEqual(got, []Group{{Type: "topic", Name: "游泳", Description: "虚构"}}) {
		t.Fatalf("%+v", got)
	}
}

func TestScheduleRuleOccursOnItsDaysOnly(t *testing.T) {
	day := func(s string) time.Time { d, _ := time.Parse("2006-01-02", s); return d }
	weekly := ScheduleRule{Frequency: "weekly", Interval: 2, Anchor: "2026-03-03", Weekdays: []int{2}}
	if !weekly.Valid() || !weekly.Occurs(day("2026-03-03")) || weekly.Occurs(day("2026-03-10")) || !weekly.Occurs(day("2026-03-17")) || weekly.Occurs(day("2026-02-17")) {
		t.Fatal("every second Tuesday from the anchor")
	}
	last := ScheduleRule{Frequency: "monthly", Interval: 1, Anchor: "2026-01-01", MonthDays: []int{-1}, Until: "2026-03-15"}
	if !last.Valid() || !last.Occurs(day("2026-02-28")) || last.Occurs(day("2026-02-27")) || last.Occurs(day("2026-03-31")) {
		t.Fatal("the last day of each month until the end date")
	}
	for _, invalid := range []ScheduleRule{{Frequency: "hourly", Interval: 1, Anchor: "2026-01-01"}, {Frequency: "daily", Interval: 0, Anchor: "2026-01-01"}, {Frequency: "daily", Interval: 2, Anchor: "0001-01-01"}, {Frequency: "daily", Interval: 1, Anchor: "2026-01-01", Clock: "25:00"}, {Frequency: "weekly", Interval: 1, Anchor: "2026-01-01", Weekdays: []int{8}}} {
		if invalid.Valid() {
			t.Fatalf("%+v", invalid)
		}
	}
}

func TestAModelDateThatCannotBeTrustedStaysUnclearWithItsReason(t *testing.T) {
	early, late := "2026-02-01", "2026-03-10"
	out, err := NormalizeDeadlines([]Deadline{
		{Kind: "deadline", Title: "交表", At: &early},
		{Kind: "deadline", Title: "体检", At: &late, TimeNote: "上午"},
		{Kind: "recurring", Title: "健身"},
		{Kind: "recurring", Title: "例会", Recurrence: "每周一 09:30"},
	}, "2026-03-01T00:00:00Z", time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if out[0].Kind != "unclear" || out[0].TimeNote != "日期无法校验；" || out[1].At == nil || !*out[1].DateOnly {
		t.Fatalf("%+v %+v", out[0], out[1])
	}
	if out[2].Kind != "unclear" || out[2].TimeNote != "未说明周期；" || out[3].ScheduleRule == nil || out[3].ScheduleRule.Clock != "09:30" {
		t.Fatalf("%+v %+v", out[2], out[3])
	}
	if _, err := NormalizeDeadlines([]Deadline{{Kind: "birthday", Title: "x"}}, "", time.UTC); err == nil {
		t.Fatal("an unknown kind was accepted")
	}
}
