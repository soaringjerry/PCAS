// Package secretary owns the workspace rules of one secretary turn that need
// no storage: the reading of the answer, the classes of a failure, and the
// due time and reminder of a task.
package secretary

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/modelcall"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// UsePlan is how the answer says it used the memories that it was shown.
type UsePlan struct {
	Depth     string   `json:"depth,omitempty"`
	Mentioned []string `json:"mentioned,omitempty"`
	Adopted   []string `json:"adopted,omitempty"`
	Groups    []string `json:"groups"`
}

// Model actions never accept database identifiers. Refs resolve exclusively
// through server-owned context aliases and earlier committed N creation aliases.
type Action struct {
	DocumentID       *string                    `json:"documentId"`
	BaseVersion      *int                       `json:"baseVersion"`
	SelfcheckDropped bool                       `json:"-"`
	ParseErr         error                      `json:"-"`
	Op               string                     `json:"op"`
	Ref              string                     `json:"ref"`
	Title            string                     `json:"title"`
	Name             string                     `json:"name"`
	Due              *string                    `json:"due"`
	Remind           *string                    `json:"remind"`
	Project          *string                    `json:"project"`
	Notes            *string                    `json:"notes"`
	OwedTo           *string                    `json:"owedTo"`
	WaitingFor       *string                    `json:"waitingFor"`
	Urgent           *bool                      `json:"urgent"`
	Condition        *string                    `json:"condition"`
	ConditionDue     *string                    `json:"conditionDue"`
	Set              map[string]json.RawMessage `json:"set"`
	Steps            []string                   `json:"steps"`
	Kind             string                     `json:"kind"`
	Prompt           string                     `json:"prompt"`
	As               string                     `json:"as"`
}

// Output is the secretary's answer for one turn.
type Output struct {
	MemoryPlan     *UsePlan           `json:"memoryPlan,omitempty"`
	MissingKeyInfo bool               `json:"missingKeyInfo"`
	Reply          string             `json:"reply"`
	Used           []string           `json:"used"`
	Links          []string           `json:"links"`
	Show           []string           `json:"show"`
	Remember       bool               `json:"remember"`
	Actions        []Action           `json:"actions"`
	Ask            *workspace.DeskAsk `json:"ask"`
}

// ErrNoObject means that the answer holds no JSON object.
var ErrNoObject = errors.New("no JSON object")

// Decode the first complete JSON object, allowing surrounding prose and fences.
// The decoder handles nested objects and braces/escapes inside JSON strings.
// Stop at that object even if its fields are invalid: later objects must not
// supply actions that were absent from the first response.
func ParseOutput(text string) (Output, error) {
	for i := 0; i < len(text); i++ {
		if text[i] != '{' {
			continue
		}
		var object json.RawMessage
		if json.NewDecoder(strings.NewReader(text[i:])).Decode(&object) != nil {
			continue
		}
		var out Output
		// Decode each action independently so a wrong field type cannot reject
		// the reply or other actions. Unknown fields are intentionally ignored.
		var raw struct {
			*Output
			Actions []json.RawMessage `json:"actions"`
		}
		raw.Output = &out
		if err := json.Unmarshal(object, &raw); err != nil {
			return Output{}, err
		}
		for _, data := range raw.Actions {
			var action Action
			action.ParseErr = json.Unmarshal(data, &action)
			if strings.TrimSpace(string(data)) == "null" {
				action.ParseErr = memory.ErrInvalid
			}
			if action.ParseErr != nil {
				// Retain only the op for the skipped receipt, never partially
				// decoded executable fields.
				var header struct {
					Op string `json:"op"`
				}
				_ = json.Unmarshal(data, &header)
				action = Action{Op: header.Op, ParseErr: action.ParseErr}
			}
			out.Actions = append(out.Actions, action)
		}
		return out, nil
	}
	return Output{}, ErrNoObject
}

// Only fixed categories reach logs. Error messages can contain private text
// (including JSON field values), provider bodies or database details.
func ErrorType(stage string, err error) string {
	var fieldErr *json.UnmarshalTypeError
	var syntaxErr *json.SyntaxError
	switch {
	case errors.Is(err, ai.ErrUnsupportedCapability):
		return "unsupported_capability"
	case errors.Is(err, modelcall.ErrOutcomeUnknown):
		return "unknown_outcome"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, workspace.ErrBudget), stage == "budget" && errors.Is(err, memory.ErrUnavailable):
		return "budget_exceeded"
	case errors.Is(err, memory.ErrUnavailable):
		return "unavailable"
	case errors.Is(err, memory.ErrConflict):
		return "conflict"
	case errors.Is(err, ErrNoObject):
		return "no_json_object"
	case errors.As(err, &fieldErr):
		return "invalid_field_type"
	case errors.As(err, &syntaxErr):
		return "invalid_json"
	case errors.Is(err, memory.ErrInvalid):
		return "invalid_input"
	default:
		return stage + "_error"
	}
}

// CaptureText is what the owner reads when a turn did not finish. The
// owner's words are kept in each case.
func CaptureText(stage string, err error) string {
	if errors.Is(err, ai.ErrUnsupportedCapability) {
		return "已记下原话；当前模型不支持所需的输出格式或搜索能力"
	}
	if errors.Is(err, modelcall.ErrOutcomeUnknown) {
		return "已记下原话；模型结果尚未确认，本轮操作未完成"
	}
	reason := "模型没有响应"
	switch stage {
	case "context":
		if errors.Is(err, memory.ErrUnavailable) {
			reason = "没有可用的模型"
		} else {
			reason = "暂时无法读取上下文"
		}
	case "budget":
		if ErrorType(stage, err) == "budget_exceeded" {
			reason = "超过今天的额度"
		} else {
			reason = "暂时无法检查额度"
		}
	case "model":
		if errors.Is(err, context.DeadlineExceeded) {
			reason = "模型响应超时"
		}
	case "verify":
		if errors.Is(err, memory.ErrForbidden) {
			return "上下文已变更，请重试"
		}
		return "本轮要改动的事项已变更，请重试"
	}
	return "已记下原话；" + reason + "，稍后会自动整理"
}

// ApplyDueReminder maintains the one fixed reminder independently of other triggers.
// An empty remind preserves the existing offset; the secretary supplies defaults.
func ApplyDueReminder(item *workspace.Item, remind string, loc *time.Location) {
	ApplyDueReminderAt(item, remind, loc, time.Now())
}

// ApplyDueReminderAt is ApplyDueReminder at a given time.
func ApplyDueReminderAt(item *workspace.Item, remind string, loc *time.Location, now time.Time) {
	if loc == nil {
		loc = time.UTC
	}
	existing := -1
	for i, t := range item.Triggers {
		if t.ID == "due-reminder" {
			existing = i
			if remind == "" {
				remind = t.Offset
			}
			break
		}
	}
	remove := func() {
		if existing >= 0 {
			item.Triggers = append(item.Triggers[:existing], item.Triggers[existing+1:]...)
		}
	}
	if item.Due == "" || remind == "none" {
		remove()
		return
	}
	if remind == "" {
		return
	}
	due, err := time.Parse(time.RFC3339, item.Due)
	if err != nil {
		return
	}
	next := due
	switch {
	case remind == "at":
	case strings.HasPrefix(remind, "-"):
		if !strings.HasSuffix(remind, "m") && !strings.HasSuffix(remind, "h") {
			return
		}
		offset, err := time.ParseDuration(remind)
		if err != nil || offset >= 0 {
			return
		}
		next = due.Add(offset)
	default:
		clock, err := time.Parse("15:04", remind)
		if err != nil {
			return
		}
		d := due.In(loc)
		next = time.Date(d.Year(), d.Month(), d.Day(), clock.Hour(), clock.Minute(), 0, 0, loc)
	}
	trigger := workspace.Trigger{ID: "due-reminder", Kind: "time", Description: item.Title, Offset: remind}
	if due.After(now) {
		if !next.After(now) {
			next = due
		}
		trigger.Active = true
		trigger.NextAt = next.UTC().Format(time.RFC3339)
	}
	// An elapsed due has no effective reminder, but retains its preference so
	// a later reschedule can reactivate the same offset. Clearing due removes it.
	if existing < 0 {
		item.Triggers = append(item.Triggers, trigger)
	} else {
		item.Triggers[existing] = trigger
	}
}

// TextField reads one text field of a model object.
func TextField(fields map[string]json.RawMessage, key string) (string, bool) {
	raw, ok := fields[key]
	if !ok || string(raw) == "null" {
		return "", false
	}
	var v string
	if json.Unmarshal(raw, &v) != nil {
		return "", false
	}
	return v, true
}

// Due reads a due time from the model: a date, a local minute, or an
// instant. A date is due at the end of that day where the owner is. A time
// more than one year in the past is not valid.
func Due(value string, loc *time.Location) (string, bool, bool) {
	if value == "" {
		return "", false, true
	}
	var at time.Time
	var err error
	dateOnly := len(value) == 10
	switch {
	case dateOnly:
		at, err = time.ParseInLocation("2006-01-02", value, loc)
		if err == nil {
			// The end of that calendar day where the user is. Adding hours to
			// midnight lands on the wrong day when the clocks change.
			at = time.Date(at.Year(), at.Month(), at.Day(), 23, 59, 0, 0, loc)
		}
	case len(value) == 16:
		at, err = time.ParseInLocation("2006-01-02T15:04", value, loc)
	default:
		at, err = time.Parse(time.RFC3339, value)
	}
	if err != nil || at.Before(time.Now().In(loc).AddDate(-1, 0, 0)) {
		return "", dateOnly, false
	}
	return at.UTC().Format(time.RFC3339), dateOnly, true
}

// ReminderValue is the reminder of a new due time: the model's choice, else
// 09:00 for a date and 30 minutes before for a time.
func ReminderValue(remind *string, dateOnly bool) string {
	if remind != nil {
		return *remind
	}
	if dateOnly {
		return "09:00"
	}
	return "-30m"
}

// LocalDate is how a receipt names a due time to the owner.
func LocalDate(due string, loc *time.Location) string {
	at, err := time.Parse(time.RFC3339, due)
	if err != nil {
		return ""
	}
	at = at.In(loc)
	now := time.Now().In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	day := time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, loc)
	label := fmt.Sprintf("%d 月 %d 日", at.Month(), at.Day())
	monday := today.AddDate(0, 0, -(int(today.Weekday())+6)%7)
	if day.Equal(today) {
		label = "今天"
	} else if day.Equal(today.AddDate(0, 0, 1)) {
		label = "明天"
	} else if !day.Before(monday) && day.Before(monday.AddDate(0, 0, 7)) {
		label = "周" + []string{"日", "一", "二", "三", "四", "五", "六"}[at.Weekday()]
	}
	if at.Hour() != 23 || at.Minute() != 59 {
		label += " " + at.Format("15:04")
	}
	return label
}
