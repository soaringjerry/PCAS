package secretary

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
	"github.com/soaringjerry/PCAS/internal/workspace/effort"
)

// Change is one item that an action changed, with the summary of the change.
type Change struct{ ID, Summary string }

// Workspace is what one action reads and changes. All calls of one action
// share one transaction, which the caller commits or rolls back.
type Workspace interface {
	// Command runs one workspace command. Its history entries wait for Publish.
	Command(ctx context.Context, command workspace.Command) error
	Item(ctx context.Context, id string) (workspace.Item, error)
	// SaveItem saves an item without a history entry.
	SaveItem(ctx context.Context, item workspace.Item) error
	// NoteChange saves an item and notes the change for Publish.
	NoteChange(ctx context.Context, item workspace.Item, summary string) error
	// Recorded reports whether this action already changed the item.
	Recorded(id string) bool
	// Changed returns the items that this action changed, in order.
	Changed() []Change
	// Publish writes the history entry of one changed item.
	Publish(ctx context.Context, item workspace.Item, summary string) error
	// ProjectNamed reports whether a project has this title.
	ProjectNamed(ctx context.Context, title string) (bool, error)
	// CurrentProject returns the project of the item that the owner has open.
	CurrentProject(ctx context.Context, thingID string) (string, error)
	// ResolveProject returns the project that the model named: an alias, no
	// project, or a new project that is made as its own action.
	ResolveProject(ctx context.Context, value string, aliases map[string]workspace.Item) (string, error)
	// CloseDate closes a remembered date. That write belongs to memory.
	CloseDate(ctx context.Context, action Action) (workspace.DeskReceipt, error)
	// Document returns a document that the turn showed, by its alias.
	Document(alias string) (workspace.Doc, bool)
	// DelegateTurns returns the turns that a deputy run gets as its history.
	DelegateTurns() []string
}

// Env is the turn in which an action runs.
type Env struct {
	// Aliases are the items that the turn showed, by their alias. The model
	// never gives a database identifier.
	Aliases  map[string]workspace.Item
	Agent    workspace.Agent
	Location *time.Location
	// CurrentThingID is the item that the owner has open, or empty.
	CurrentThingID string
}

// Skipped is the receipt of an action that was not done, with the reason
// that the owner reads.
func Skipped(op, reason string) workspace.DeskReceipt {
	return workspace.DeskReceipt{Op: op, Text: reason, Status: "skipped", Reason: reason}
}

func stamp() string       { return time.Now().UTC().Format(time.RFC3339Nano) }
func asJSON(v any) []byte { b, _ := json.Marshal(v); return b }
func oneOf(value string, values ...string) bool {
	for _, v := range values {
		if value == v {
			return true
		}
	}
	return false
}
func pointerValue(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

// taskReceipt is how a receipt names a task: its due time, its title, its
// project, its reminder, and its urgency.
func taskReceipt(ctx context.Context, w Workspace, item workspace.Item, loc *time.Location) string {
	parts := []string{}
	if item.Due != "" {
		parts = append(parts, LocalDate(item.Due, loc)+" "+item.Title)
	} else {
		parts = append(parts, item.Title)
	}
	if item.ProjectID != "" {
		if p, err := w.Item(ctx, item.ProjectID); err == nil {
			parts = append(parts, p.Title+" 项目")
		}
	}
	for _, trigger := range item.Triggers {
		if trigger.ID == "due-reminder" && trigger.Active {
			if at, err := time.Parse(time.RFC3339, trigger.NextAt); err == nil {
				parts = append(parts, at.In(loc).Format("15:04")+" 提醒")
			}
		}
	}
	if due, err := time.Parse(time.RFC3339, item.Due); err == nil && !due.After(time.Now()) {
		parts = append(parts, "时间已过，没有设提醒")
	}
	if item.Urgent {
		parts = append(parts, "尽快")
	}
	return "已建：" + strings.Join(parts, " · ")
}

// TaskReceipt is taskReceipt for a caller outside this package.
func TaskReceipt(ctx context.Context, w Workspace, item workspace.Item, loc *time.Location) string {
	return taskReceipt(ctx, w, item, loc)
}

// Execute does one action of the answer and returns its receipt. An action
// that cannot be done returns a skipped receipt with its reason; the caller
// then rolls the action back. An item is named by its alias only.
func Execute(ctx context.Context, w Workspace, a Action, env Env) (workspace.DeskReceipt, error) {
	aliases, agent, loc, currentThingID := env.Aliases, env.Agent, env.Location, env.CurrentThingID
	receipt := workspace.DeskReceipt{Op: a.Op, Status: "done"}
	call := func(c workspace.Command) error { return w.Command(ctx, c) }
	var id string
	note := ""
	if oneOf(a.Op, "update", "add_steps", "delegate") && !(a.Op == "delegate" && a.Ref == "new") {
		item, ok := aliases[a.Ref]
		if !ok {
			return Skipped(a.Op, "找不到要改的那件事"), nil
		}
		id = item.ID
	}
	switch a.Op {
	case "close_date":
		return w.CloseDate(ctx, a)
	case "create_task", "create_idea", "create_project":
		title := strings.TrimSpace(a.Title)
		if a.Op == "create_project" {
			title = strings.TrimSpace(a.Name)
		}
		if !workspace.ValidTitle(title) {
			return Skipped(a.Op, "标题为空或太长"), nil
		}
		if a.Op == "create_project" {
			exists, e := w.ProjectNamed(ctx, title)
			if e != nil {
				return receipt, e
			}
			if exists {
				return Skipped(a.Op, "已有同名项目，沿用已有项目"), nil
			}
		}
		id = string(memory.NewID())
		project := ""
		var err error
		if a.Op != "create_project" && a.Project == nil {
			project, err = w.CurrentProject(ctx, currentThingID)
			if err != nil {
				return receipt, err
			}
		}
		if a.Project != nil {
			project, err = w.ResolveProject(ctx, *a.Project, aliases)
			if err != nil {
				return Skipped(a.Op, "找不到所属项目"), err
			}
		}
		kind := map[string]string{"create_task": "addTask", "create_idea": "addIdea", "create_project": "addProject"}[a.Op]
		if err = call(workspace.Command{Type: kind, ID: id, Title: title, Name: title, ProjectID: project}); err != nil {
			return receipt, err
		}
		if a.Op == "create_task" {
			patch := map[string]any{}
			dateOnly := false
			if a.Due != nil {
				due, date, ok := Due(*a.Due, loc)
				dateOnly = date
				if ok {
					patch["due"] = due
					patch["dueDateOnly"] = date
				} else {
					note = " · 时间没看懂"
				}
			}
			if a.Notes != nil {
				patch["notes"] = *a.Notes
			}
			if a.OwedTo != nil {
				patch["owedTo"] = workspace.Owed{Who: *a.OwedTo, Since: stamp()}
			}
			if a.WaitingFor != nil {
				patch["waitingFor"] = *a.WaitingFor
			}
			if a.Urgent != nil && *a.Urgent {
				patch["urgent"] = true
			}
			if len(patch) > 0 {
				if err = call(workspace.Command{Type: "updateTask", ID: id, Patch: asJSON(patch)}); err != nil {
					return receipt, err
				}
			}
			item, err := w.Item(ctx, id)
			if err != nil {
				return receipt, err
			}
			ApplyDueReminder(&item, ReminderValue(a.Remind, dateOnly), loc)
			if a.Remind != nil {
				item.RemindersOn = *a.Remind != "none"
			}
			if err = w.SaveItem(ctx, item); err != nil {
				return receipt, err
			}
			receipt.Text = taskReceipt(ctx, w, item, loc) + note
		} else if a.Op == "create_idea" {
			receipt.Text = "记成想法：" + title
			if a.Condition != nil && strings.TrimSpace(*a.Condition) != "" {
				due := ""
				if a.ConditionDue != nil {
					var ok bool
					due, _, ok = Due(*a.ConditionDue, loc)
					if !ok {
						note = " · 时间没看懂"
					}
				}
				if err = call(workspace.Command{Type: "addCondition", ID: id, Description: *a.Condition, Due: due}); err != nil {
					return receipt, err
				}
				receipt.Text += " · " + *a.Condition + note
			}
		} else {
			receipt.Text = "建了项目「" + title + "」"
		}
	case "update":
		item, err := w.Item(ctx, id)
		if err != nil {
			return receipt, err
		}
		if title, ok := TextField(a.Set, "title"); ok {
			if !workspace.ValidTitle(title) {
				return Skipped(a.Op, "标题为空或太长"), nil
			}
			if err = call(workspace.Command{Type: "renameThing", ID: id, Title: strings.TrimSpace(title)}); err != nil {
				return receipt, err
			}
		}
		patch := map[string]any{}
		dateOnly := false
		dueChanged := false
		if due, ok := TextField(a.Set, "due"); ok && item.Kind == "task" {
			parsed, date, valid := Due(due, loc)
			dateOnly = date
			if valid {
				patch["due"] = parsed
				patch["dueDateOnly"] = date
				dueChanged = true
			} else {
				note = " · 时间没看懂"
			}
		}
		// null leaves the mark as it is; only an explicit true or false changes it.
		if raw, ok := a.Set["urgent"]; ok && item.Kind == "task" {
			var urgent bool
			if string(raw) != "null" && json.Unmarshal(raw, &urgent) == nil && urgent != item.Urgent {
				patch["urgent"] = urgent
			}
		}
		if raw, ok := a.Set["estimatedHours"]; ok && string(raw) != "null" && item.Kind == "task" {
			var h float64
			if json.Unmarshal(raw, &h) != nil {
				return Skipped(a.Op, "工作量不是有效小时数"), nil
			}
			if _, err = effort.StartDate(item.Due, &h, loc); err != nil {
				return Skipped(a.Op, "工作量须为非负小时数，并能计算有效开工日"), nil
			}
			patch["estimatedHours"] = h
		}
		if len(patch) > 0 {
			if err = call(workspace.Command{Type: "updateTask", ID: id, Patch: asJSON(patch)}); err != nil {
				return receipt, err
			}
		}
		if status, ok := TextField(a.Set, "status"); ok {
			if item.Kind == "task" && oneOf(status, "todo", "doing", "waiting", "done", "cancelled") {
				if err = call(workspace.Command{Type: "setTaskStatus", ID: id, Status: status}); err != nil {
					return receipt, err
				}
			}
			if item.Kind == "project" && oneOf(status, "active", "paused", "done") {
				if err = call(workspace.Command{Type: "updateProject", ID: id, Patch: asJSON(map[string]string{"status": status})}); err != nil {
					return receipt, err
				}
			}
		}
		if project, ok := TextField(a.Set, "project"); ok && item.Kind != "project" {
			pid, err := w.ResolveProject(ctx, project, aliases)
			if err != nil {
				return Skipped(a.Op, "找不到所属项目"), err
			}
			if err = call(workspace.Command{Type: "moveThing", ID: id, ProjectID: pid}); err != nil {
				return receipt, err
			}
		}
		if appendText, ok := TextField(a.Set, "notesAppend"); ok && strings.TrimSpace(appendText) != "" {
			current, err := w.Item(ctx, id)
			if err != nil {
				return receipt, err
			}
			text := current.Notes
			if current.Kind == "idea" {
				text = current.Body
			}
			if current.Kind == "project" {
				text = current.Goal
			}
			if text != "" {
				text += "\n"
			}
			text += appendText
			if err = call(workspace.Command{Type: "setNotes", ID: id, Text: text}); err != nil {
				return receipt, err
			}
		}
		item, err = w.Item(ctx, id)
		if err != nil {
			return receipt, err
		}
		remind, hasRemind := TextField(a.Set, "remind")
		if item.Kind == "task" && (dueChanged || hasRemind) {
			if dueChanged && !hasRemind {
				hasExisting := false
				for _, t := range item.Triggers {
					if t.ID == "due-reminder" {
						hasExisting = true
					}
				}
				if !hasExisting {
					remind = ReminderValue(nil, dateOnly)
				}
			}
			ApplyDueReminder(&item, remind, loc)
			if hasRemind {
				item.RemindersOn = remind != "none"
			}
			if !w.Recorded(id) {
				item.Version++
				item.UpdatedAt = stamp()
			}
			if err = w.NoteChange(ctx, item, "调整提醒"); err != nil {
				return receipt, err
			}
		}
		receipt.Text = "已改：" + item.Title
		if strings.EqualFold(id, currentThingID) {
			receipt.Text = "已改"
		}
		if item.Status == "done" {
			receipt.Text = "已完成：" + item.Title
			if strings.EqualFold(id, currentThingID) {
				receipt.Text = "已完成"
			}
		} else if dueChanged && item.Due != "" {
			if strings.EqualFold(id, currentThingID) {
				receipt.Text += "：→ " + LocalDate(item.Due, loc)
			} else {
				receipt.Text += " → " + LocalDate(item.Due, loc)
			}
		}
		if item.Kind == "task" && (dueChanged || hasRemind) {
			if due, err := time.Parse(time.RFC3339, item.Due); err == nil && !due.After(time.Now()) {
				receipt.Text += " · 时间已过，没有设提醒"
			}
		}
		receipt.Text += note
	case "add_steps":
		item, err := w.Item(ctx, id)
		if err != nil {
			return receipt, err
		}
		if item.Kind != "task" || len(a.Steps) == 0 || len(a.Steps) > 100 {
			return Skipped(a.Op, "这件事不能加这些步骤"), nil
		}
		for _, step := range a.Steps {
			if !workspace.ValidTitle(step) {
				return Skipped(a.Op, "步骤为空或太长"), nil
			}
			if err = call(workspace.Command{Type: "addCheck", ID: id, Text: strings.TrimSpace(step)}); err != nil {
				return receipt, err
			}
		}
		receipt.Text = fmt.Sprintf("给「%s」加了 %d 步", item.Title, len(a.Steps))
		if strings.EqualFold(id, currentThingID) {
			receipt.Text = fmt.Sprintf("加了 %d 步", len(a.Steps))
		}
	case "delegate":
		if !oneOf(a.Kind, "plan", "draft", "breakdown", "summary", "ask", "revise") || strings.TrimSpace(a.Prompt) == "" {
			return Skipped(a.Op, "没说清要交给副手做什么"), nil
		}
		c := workspace.Command{Type: "requestRun", ThingID: id, AgentID: agent.ID, Kind: a.Kind, Prompt: a.Prompt}
		if a.Kind == "revise" {
			doc, ok := w.Document(pointerValue(a.DocumentID))
			if !ok {
				return Skipped(a.Op, "目标文档不明确"), nil
			}
			if a.BaseVersion == nil || *a.BaseVersion < 1 {
				return Skipped(a.Op, "基准版本不明确"), nil
			}
			c.DocumentID = doc.ID
			c.BaseVersion = *a.BaseVersion
			c.ThingID = doc.ThingID
			id = doc.ThingID
			if a.Ref == "new" {
				return Skipped(a.Op, "改写不能新建事项"), nil
			}
		}
		c.DeskTurnIDs = w.DelegateTurns()
		if a.Ref == "new" {
			if !workspace.ValidTitle(a.Title) {
				return Skipped(a.Op, "标题为空或太长"), nil
			}
			id = string(memory.NewID())
			c.Type = "delegateTask"
			c.ID = id
			c.Title = strings.TrimSpace(a.Title)
			var err error
			c.ProjectID, err = w.CurrentProject(ctx, currentThingID)
			if err != nil {
				return receipt, err
			}
		}
		if err := call(c); err != nil {
			return receipt, err
		}
		receipt.Text = "交给 " + agent.Name + "：" + a.Prompt
	default:
		return Skipped(a.Op, "不认识这个动作"), nil
	}
	for _, change := range w.Changed() {
		changedID := change.ID
		item, err := w.Item(ctx, changedID)
		if err != nil {
			return receipt, err
		}
		summary := change.Summary
		if summary == "创建" {
			summary = "新建：" + item.Title
		}
		if changedID == id {
			summary = receipt.Text
			switch a.Op {
			case "create_task":
				summary = "新建：" + strings.TrimPrefix(receipt.Text, "已建：")
			case "update":
				summary = "更新：" + strings.TrimPrefix(taskReceipt(ctx, w, item, loc), "已建：") + note
				if item.Status == "done" {
					summary = "完成：" + item.Title
				}
			}
		}
		if err = w.Publish(ctx, item, summary); err != nil {
			return receipt, err
		}
	}
	receipt.ThingID = &id
	return receipt, nil
}

// closeDateActionTx closes the date a memory carries: met, not wanted any more,
// or turned into a to-do. It marks the memory version the same way the home
// page's circle does, so the date leaves the home page and the list the
// secretary is shown; the memory stays and the action can be undone.
