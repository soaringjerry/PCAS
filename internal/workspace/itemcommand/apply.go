// Package itemcommand owns the workspace rules that change one task, idea,
// or project by one command. The rules need no storage: the caller reads the
// item, applies the command, and saves the item with the returned summary.
package itemcommand

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
	"github.com/soaringjerry/PCAS/internal/workspace/secretary"
)

// Effects are the two things outside the item that a command can need.
type Effects interface {
	// Location returns the owner's time zone.
	Location(ctx context.Context) (*time.Location, error)
	// PromoteIdea saves the task that an idea becomes and moves the idea's
	// documents to the task.
	PromoteIdea(ctx context.Context, ideaID string, task workspace.Item) error
}

// Target returns the identity of the item that a command changes.
func Target(c workspace.Command) string {
	id := c.ID
	if c.TaskID != "" {
		id = c.TaskID
	}
	if c.IdeaID != "" {
		id = c.IdeaID
	}
	return id
}

func asJSON(v any) []byte { b, _ := json.Marshal(v); return b }
func oneOf(value string, values ...string) bool {
	for _, v := range values {
		if value == v {
			return true
		}
	}
	return false
}

// RequireText fails on a text that is empty or longer than one megabyte.
func RequireText(v string) error {
	if strings.TrimSpace(v) == "" || len(v) > 1<<20 {
		return memory.ErrInvalid
	}
	return nil
}

// Patch merges the allowed fields of a JSON patch into dst. A null field is
// removed. A field that is not allowed, or a value of the wrong type, makes
// the whole patch invalid and leaves dst as it was.
func Patch(dst any, patch json.RawMessage, keys ...string) error {
	var fields map[string]json.RawMessage
	if len(patch) == 0 || json.Unmarshal(patch, &fields) != nil || len(fields) == 0 {
		return memory.ErrInvalid
	}
	for k := range fields {
		if !oneOf(k, keys...) {
			return memory.ErrInvalid
		}
	}
	var merged map[string]json.RawMessage
	if json.Unmarshal(asJSON(dst), &merged) != nil {
		return memory.ErrInvalid
	}
	for k, v := range fields {
		if string(v) == "null" {
			delete(merged, k)
		} else {
			merged[k] = v
		}
	}
	replacement := reflect.New(reflect.TypeOf(dst).Elem())
	decoder := json.NewDecoder(bytes.NewReader(asJSON(merged)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(replacement.Interface()) != nil {
		return memory.ErrInvalid
	}
	reflect.ValueOf(dst).Elem().Set(replacement.Elem())
	return nil
}

// Apply changes the item by one command and returns the summary for the
// item's history. A command of a task changes a task only, and a command of
// an idea changes an idea only. The item gets its next version.
func Apply(ctx context.Context, item *workspace.Item, c workspace.Command, fx Effects) (string, error) {
	if oneOf(c.Type, "updateTask", "setTaskStatus", "toggleTrigger", "addCheck", "toggleCheck", "removeCheck", "deferTask") && item.Kind != "task" {
		return "", memory.ErrInvalid
	}
	if (strings.HasPrefix(c.Type, "idea") || oneOf(c.Type, "addCondition", "removeCondition")) && item.Kind != "idea" {
		return "", memory.ErrInvalid
	}
	summary := c.Summary
	switch c.Type {
	case "updateTask":
		if err := Patch(item, c.Patch, "title", "notes", "status", "projectId", "due", "dueDateOnly", "scheduled", "waitingFor", "owedTo", "urgent", "dependsOn", "estimatedHours", "remindersOn"); err != nil {
			return "", err
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(c.Patch, &fields) == nil {
			if _, changed := fields["estimatedHours"]; changed {
				item.EffortSource = "user"
				item.EffortReason = "用户设定"
			}
			if _, changed := fields["due"]; changed {
				if _, known := fields["dueDateOnly"]; !known {
					value := false
					item.DueDateOnly = &value
				}
				loc, err := fx.Location(ctx)
				if err != nil {
					return "", err
				}
				secretary.ApplyDueReminder(item, "", loc)
			}
		}
	case "updateProject":
		if item.Kind != "project" {
			return "", memory.ErrInvalid
		}
		if err := Patch(item, c.Patch, "name", "goal", "status"); err != nil {
			return "", err
		}
		item.Title = item.Name
	case "setTaskStatus":
		item.Status = c.Status
		summary = "状态改为 " + c.Status
	case "renameThing":
		item.Title = c.Title
		if item.Kind == "project" {
			item.Name = c.Title
		}
		summary = "更改名称"
	case "setNotes":
		if item.Kind == "task" {
			item.Notes = c.Text
		} else if item.Kind == "idea" {
			item.Body = c.Text
		} else {
			item.Goal = c.Text
		}
		summary = "更新说明"
	case "moveThing":
		item.ProjectID = c.ProjectID
		summary = "移动项目"
	case "deferTask":
		if c.Days < 1 || c.Days > 3650 {
			return "", memory.ErrInvalid
		}
		item.Scheduled = time.Now().AddDate(0, 0, c.Days).UTC().Format(time.RFC3339)
		summary = "推迟处理"
	case "toggleTrigger":
		found := false
		for i := range item.Triggers {
			if item.Triggers[i].ID == c.TriggerID {
				item.Triggers[i].Active = !item.Triggers[i].Active
				found = true
			}
		}
		if !found {
			return "", memory.ErrNotFound
		}
		summary = "调整提醒"
	case "addCheck":
		if err := RequireText(c.Text); err != nil {
			return "", err
		}
		item.Checklist = append(item.Checklist, workspace.Check{ID: string(memory.NewID()), Text: c.Text})
		summary = "添加子步骤"
	case "toggleCheck", "removeCheck":
		found := false
		for i := range item.Checklist {
			if item.Checklist[i].ID == c.ItemID {
				found = true
				if c.Type == "removeCheck" {
					item.Checklist = append(item.Checklist[:i], item.Checklist[i+1:]...)
				} else {
					item.Checklist[i].Done = !item.Checklist[i].Done
				}
				break
			}
		}
		if !found {
			return "", memory.ErrNotFound
		}
		summary = "更新子步骤"
	case "ideaContinue":
		item.Status = "active"
		item.Wake = nil
		summary = "继续考虑"
	case "ideaPromote":
		if item.Status == "promoted" {
			return "", memory.ErrConflict
		}
		task := workspace.NewItem("task", item.Title)
		task.Notes = item.Body
		task.ProjectID = item.ProjectID
		task.IdeaID = item.ID
		task.Sources = item.Sources
		if err := fx.PromoteIdea(ctx, item.ID, task); err != nil {
			return "", err
		}
		item.Status = "promoted"
		item.Wake = nil
		summary = "转为待办"
	case "ideaSnooze":
		if c.Days < 1 || c.Days > 3650 {
			return "", memory.ErrInvalid
		}
		if item.Wake == nil {
			return "", memory.ErrConflict
		}
		item.Wake.SnoozedUntil = time.Now().AddDate(0, 0, c.Days).UTC().Format(time.RFC3339)
		summary = "暂缓提醒"
	case "ideaStopReminders":
		item.RemindersOn = false
		summary = "停止提醒"
	case "ideaShelve":
		item.Status = "shelved"
		item.ShelvedReason = c.Reason
		item.Wake = nil
		if strings.TrimSpace(c.Condition) != "" {
			item.Conditions = append(item.Conditions, workspace.Condition{ID: string(memory.NewID()), Kind: "event", Description: c.Condition})
		}
		summary = "搁置：" + c.Reason
	case "ideaDrop":
		item.Status = "dropped"
		item.RemindersOn = false
		item.Wake = nil
		summary = "放下想法"
	case "ideaNote":
		if err := RequireText(c.Note); err != nil {
			return "", err
		}
		item.Body += "\n" + c.Note
		summary = c.Note
	case "addCondition":
		if err := RequireText(c.Description); err != nil {
			return "", err
		}
		kind := "event"
		if c.Due != "" {
			if _, err := time.Parse(time.RFC3339, c.Due); err != nil {
				return "", memory.ErrInvalid
			}
			kind = "time"
		}
		item.Conditions = append(item.Conditions, workspace.Condition{ID: string(memory.NewID()), Kind: kind, Description: c.Description, DueAt: c.Due})
		summary = "新增唤醒条件"
	case "removeCondition":
		found := false
		for i := range item.Conditions {
			if item.Conditions[i].ID == c.ConditionID {
				item.Conditions = append(item.Conditions[:i], item.Conditions[i+1:]...)
				found = true
				break
			}
		}
		if !found {
			return "", memory.ErrNotFound
		}
		summary = "移除唤醒条件"
	default:
		return "", memory.ErrInvalid
	}
	item.Version++
	item.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if summary == "" {
		summary = "更新"
	}
	return summary, nil
}
