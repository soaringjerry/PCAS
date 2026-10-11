package secretary

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/modelcall"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func TestParseSecretaryFirstObjectAndQuotedBraces(t *testing.T) {
	for _, text := range []string{
		`前言 {不是 JSON} {"reply":"括号 } { 和引号 \"","actions":[{"op":"create_task","title":"第一个"}]} 后文 {"actions":[{"op":"create_task","title":"不执行第二个"}]}`,
		"说明\n```JSON\n" + `{"reply":"括号 } { 和引号 \"","actions":[{"op":"create_task","title":"第一个"}]}` + "\n```\n后文",
	} {
		out, err := ParseOutput(text)
		if err != nil || out.Reply != "括号 } { 和引号 \"" || len(out.Actions) != 1 || out.Actions[0].Title != "第一个" {
			t.Fatal(err, out)
		}
	}
	// Invalid fields in the first object cannot be replaced by a later object.
	out, err := ParseOutput(`{"reply":42} {"actions":[{"op":"create_task","title":"不能执行"}]}`)
	if err == nil || len(out.Actions) != 0 {
		t.Fatal(err, out)
	}
}

func TestSecretaryCaptureReasonForBudgetInfrastructure(t *testing.T) {
	if CaptureText("budget", context.DeadlineExceeded) != "已记下原话；暂时无法检查额度，稍后会自动整理" {
		t.Fatal("budget infrastructure timeout mislabeled as quota exhaustion")
	}
	if ErrorType("budget", workspace.ErrBudget) != "budget_exceeded" {
		t.Fatal("budget error not categorized")
	}
}

// A date without a time means the end of that day where the user is, also on
// the days the clocks change.
func TestDateOnlyDueStaysOnItsDayAcrossClockChanges(t *testing.T) {
	year := time.Now().Year() + 1
	for _, zone := range []string{"Australia/Melbourne", "Europe/Berlin", "America/New_York", "Asia/Shanghai"} {
		loc, err := time.LoadLocation(zone)
		if err != nil {
			t.Fatal(err)
		}
		for day := time.Date(year, 1, 1, 12, 0, 0, 0, loc); day.Year() == year; day = day.AddDate(0, 0, 1) {
			value := day.Format("2006-01-02")
			out, dateOnly, ok := Due(value, loc)
			at, err := time.Parse(time.RFC3339, out)
			if !ok || !dateOnly || err != nil {
				t.Fatalf("%s %s: %q %v %v %v", zone, value, out, dateOnly, ok, err)
			}
			if local := at.In(loc); local.Format("2006-01-02 15:04") != value+" 23:59" {
				t.Fatalf("%s %s became %s", zone, value, local.Format("2006-01-02 15:04"))
			}
		}
	}
}

// A wrong field type in one action cannot reject the reply or the other actions.
func TestOneInvalidActionKeepsItsOpAndTheOtherActions(t *testing.T) {
	out, err := ParseOutput(`{"reply":"虚构","actions":[{"op":"create_task","title":7,"ref":"T1"},null,{"op":"create_idea","title":"虚构的想法"}]}`)
	if err != nil || out.Reply != "虚构" || len(out.Actions) != 3 {
		t.Fatal(err, out)
	}
	if a := out.Actions[0]; a.ParseErr == nil || a.Op != "create_task" || a.Ref != "" {
		t.Fatal("an invalid action keeps its op only", a)
	}
	if out.Actions[1].ParseErr == nil || out.Actions[2].ParseErr != nil || out.Actions[2].Title != "虚构的想法" {
		t.Fatal(out.Actions)
	}
	if _, err := ParseOutput("虚构的散文，没有对象"); !errors.Is(err, ErrNoObject) {
		t.Fatal(err)
	}
}

// Only fixed classes reach the log.
func TestFailureClasses(t *testing.T) {
	for want, err := range map[string]error{
		"unsupported_capability": ai.ErrUnsupportedCapability,
		"unknown_outcome":        modelcall.ErrOutcomeUnknown,
		"timeout":                context.DeadlineExceeded,
		"canceled":               context.Canceled,
		"unavailable":            memory.ErrUnavailable,
		"conflict":               memory.ErrConflict,
		"no_json_object":         ErrNoObject,
		"invalid_input":          memory.ErrInvalid,
		"model_error":            errors.New("fictitious private text"),
	} {
		if got := ErrorType("model", err); got != want {
			t.Fatal(want, got)
		}
	}
	if ErrorType("budget", memory.ErrUnavailable) != "budget_exceeded" {
		t.Fatal("an unavailable budget is an exceeded budget")
	}
}

func TestReminderFollowsTheDueTimeAndKeepsItsOffset(t *testing.T) {
	loc := time.FixedZone("fictitious", 8*3600)
	now := time.Date(2026, 3, 1, 9, 0, 0, 0, loc)
	task := workspace.Item{Title: "虚构的任务", Due: time.Date(2026, 3, 2, 15, 0, 0, 0, loc).UTC().Format(time.RFC3339)}
	ApplyDueReminderAt(&task, ReminderValue(nil, false), loc, now)
	if len(task.Triggers) != 1 || !task.Triggers[0].Active || task.Triggers[0].Offset != "-30m" || task.Triggers[0].NextAt != time.Date(2026, 3, 2, 14, 30, 0, 0, loc).UTC().Format(time.RFC3339) {
		t.Fatalf("%+v", task.Triggers)
	}
	// A new due time keeps the offset that the task has.
	task.Due = time.Date(2026, 3, 3, 10, 0, 0, 0, loc).UTC().Format(time.RFC3339)
	ApplyDueReminderAt(&task, "", loc, now)
	if task.Triggers[0].NextAt != time.Date(2026, 3, 3, 9, 30, 0, 0, loc).UTC().Format(time.RFC3339) {
		t.Fatalf("%+v", task.Triggers)
	}
	// An elapsed due time has no active reminder, and it keeps the preference.
	task.Due = time.Date(2026, 2, 1, 10, 0, 0, 0, loc).UTC().Format(time.RFC3339)
	ApplyDueReminderAt(&task, "", loc, now)
	if task.Triggers[0].Active || task.Triggers[0].Offset != "-30m" {
		t.Fatalf("%+v", task.Triggers)
	}
	ApplyDueReminderAt(&task, "none", loc, now)
	if len(task.Triggers) != 0 {
		t.Fatal("none removes the reminder")
	}
	if ReminderValue(nil, true) != "09:00" {
		t.Fatal("a date is reminded at 09:00")
	}
	if _, _, ok := Due("2019-01-01", loc); ok {
		t.Fatal("a time more than one year in the past is not valid")
	}
	if out, dateOnly, ok := Due("2099-01-02T15:04", loc); !ok || dateOnly || out != "2099-01-02T07:04:00Z" {
		t.Fatal(out, dateOnly, ok)
	}
}
