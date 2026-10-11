package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
	"github.com/soaringjerry/PCAS/internal/workspace/effort"
	"github.com/soaringjerry/PCAS/internal/workspace/secretary"
	"strings"
	"time"
)

type (
	secretaryAction = secretary.Action
	secretaryOutput = secretary.Output
)

// The memories this turn's prompt showed, by their M* alias; close_date
// resolves its ref through them and nothing else.
type secretaryMemoriesKey struct{}

// Internal commands still persist in order for validation and undo collection.
// Publish history and its memory source only after the whole secretary action,
// including reminder changes, has reached its final state.
type secretaryHistoryKey struct{}
type secretaryCreationKey struct{}
type secretaryProjectReceiptsKey struct{}
type secretaryHistory struct {
	ids       []string
	summaries map[string]string
}

func (h *secretaryHistory) record(id, summary string) {
	if _, ok := h.summaries[id]; !ok {
		h.ids = append(h.ids, id)
		h.summaries[id] = summary
	}
}

func skippedReceipt(op, reason string) workspace.DeskReceipt {
	return workspace.DeskReceipt{Op: op, Text: reason, Status: "skipped", Reason: reason}
}
func (s *Store) secretaryProjectTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, value string, aliases map[string]workspace.Item) (string, error) {
	if value == "" || value == "none" {
		return "", nil
	}
	if p, ok := aliases[value]; ok && p.Kind == "project" {
		return p.ID, nil
	}
	if strings.HasPrefix(value, "new:") {
		name := strings.TrimSpace(strings.TrimPrefix(value, "new:"))
		if !workspace.ValidTitle(name) {
			return "", memory.ErrInvalid
		}
		var id string
		err := tx.QueryRow(ctx, "SELECT id::text FROM work_items WHERE owner_id=$1 AND kind='project' AND lower(btrim(title))=lower($2) ORDER BY created_at,id LIMIT 1", string(scope.OwnerID), name).Scan(&id)
		if err == nil {
			return id, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return "", err
		}
		id = string(memory.NewID())
		// A companion project is a distinct reversible action. Restore the
		// enclosing task buffer so task undo does not also remove its project.
		var buffer string
		if err = tx.QueryRow(ctx, "SELECT current_setting('pcas.action_changes',true)").Scan(&buffer); err != nil {
			return "", err
		}
		log, _ := ctx.Value(actionLogKey{}).(actionLog)
		actionID := string(memory.NewID())
		projectCtx := withActionLog(ctx, actionID, "desk", log.turnID, "建了项目「"+name+"」")
		projectCtx = context.WithValue(projectCtx, secretaryHistoryKey{}, nil)
		if err = beginActionLogTx(projectCtx, tx); err != nil {
			return "", err
		}
		if err = s.commandTx(projectCtx, tx, scope, workspace.Command{Type: "addProject", ID: id, Name: name}); err != nil {
			return "", err
		}
		if err = flushActionLog(projectCtx, tx, scope); err != nil {
			return "", err
		}
		if _, err = tx.Exec(ctx, "SELECT set_config('pcas.action_changes',$1,true)", buffer); err != nil {
			return "", err
		}
		if receipts, ok := ctx.Value(secretaryProjectReceiptsKey{}).(*[]workspace.DeskReceipt); ok {
			*receipts = append(*receipts, workspace.DeskReceipt{ActionID: &actionID, Op: "create_project", Text: "建了项目「" + name + "」", ThingID: &id, Undoable: true, Status: "done"})
		}
		return id, nil
	}
	return "", memory.ErrInvalid
}
func taskReceiptText(ctx context.Context, tx pgx.Tx, scope memory.Scope, item workspace.Item, loc *time.Location) string {
	parts := []string{}
	if item.Due != "" {
		parts = append(parts, secretary.LocalDate(item.Due, loc)+" "+item.Title)
	} else {
		parts = append(parts, item.Title)
	}
	if item.ProjectID != "" {
		if p, err := getItem(ctx, tx, scope, item.ProjectID); err == nil {
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
func (s *Store) executeSecretaryActionTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, a secretaryAction, aliases map[string]workspace.Item, agent workspace.Agent, loc *time.Location, currentThingID string) (workspace.DeskReceipt, error) {
	historyCtx := ctx
	history := &secretaryHistory{summaries: map[string]string{}}
	ctx = context.WithValue(ctx, secretaryHistoryKey{}, history)
	receipt := workspace.DeskReceipt{Op: a.Op, Status: "done"}
	call := func(c workspace.Command) error { return s.commandTx(ctx, tx, scope, c) }
	var id string
	note := ""
	if oneOf(a.Op, "update", "add_steps", "delegate") && !(a.Op == "delegate" && a.Ref == "new") {
		item, ok := aliases[a.Ref]
		if !ok {
			return skippedReceipt(a.Op, "找不到要改的那件事"), nil
		}
		id = item.ID
	}
	switch a.Op {
	case "close_date":
		return s.closeDateActionTx(ctx, tx, scope, a)
	case "create_task", "create_idea", "create_project":
		title := strings.TrimSpace(a.Title)
		if a.Op == "create_project" {
			title = strings.TrimSpace(a.Name)
		}
		if !workspace.ValidTitle(title) {
			return skippedReceipt(a.Op, "标题为空或太长"), nil
		}
		if a.Op == "create_project" {
			var existing string
			e := tx.QueryRow(ctx, "SELECT id::text FROM work_items WHERE owner_id=$1 AND kind='project' AND lower(btrim(title))=lower($2) ORDER BY id LIMIT 1", string(scope.OwnerID), title).Scan(&existing)
			if e == nil {
				return skippedReceipt(a.Op, "已有同名项目，沿用已有项目"), nil
			}
			if !errors.Is(e, pgx.ErrNoRows) {
				return receipt, e
			}
		}
		id = string(memory.NewID())
		project := ""
		var err error
		if a.Op != "create_project" && a.Project == nil {
			project, err = currentStudioProjectTx(ctx, tx, scope, currentThingID)
			if err != nil {
				return receipt, err
			}
		}
		if a.Project != nil {
			project, err = s.secretaryProjectTx(ctx, tx, scope, *a.Project, aliases)
			if err != nil {
				return skippedReceipt(a.Op, "找不到所属项目"), err
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
				due, date, ok := secretary.Due(*a.Due, loc)
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
			item, err := getItem(ctx, tx, scope, id)
			if err != nil {
				return receipt, err
			}
			secretary.ApplyDueReminder(&item, secretary.ReminderValue(a.Remind, dateOnly), loc)
			if a.Remind != nil {
				item.RemindersOn = *a.Remind != "none"
			}
			if err = saveItem(ctx, tx, scope, item); err != nil {
				return receipt, err
			}
			receipt.Text = taskReceiptText(ctx, tx, scope, item, loc) + note
		} else if a.Op == "create_idea" {
			receipt.Text = "记成想法：" + title
			if a.Condition != nil && strings.TrimSpace(*a.Condition) != "" {
				due := ""
				if a.ConditionDue != nil {
					var ok bool
					due, _, ok = secretary.Due(*a.ConditionDue, loc)
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
		item, err := getItem(ctx, tx, scope, id)
		if err != nil {
			return receipt, err
		}
		if title, ok := secretary.TextField(a.Set, "title"); ok {
			if !workspace.ValidTitle(title) {
				return skippedReceipt(a.Op, "标题为空或太长"), nil
			}
			if err = call(workspace.Command{Type: "renameThing", ID: id, Title: strings.TrimSpace(title)}); err != nil {
				return receipt, err
			}
		}
		patch := map[string]any{}
		dateOnly := false
		dueChanged := false
		if due, ok := secretary.TextField(a.Set, "due"); ok && item.Kind == "task" {
			parsed, date, valid := secretary.Due(due, loc)
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
				return skippedReceipt(a.Op, "工作量不是有效小时数"), nil
			}
			if _, err = effort.StartDate(item.Due, &h, loc); err != nil {
				return skippedReceipt(a.Op, "工作量须为非负小时数，并能计算有效开工日"), nil
			}
			patch["estimatedHours"] = h
		}
		if len(patch) > 0 {
			if err = call(workspace.Command{Type: "updateTask", ID: id, Patch: asJSON(patch)}); err != nil {
				return receipt, err
			}
		}
		if status, ok := secretary.TextField(a.Set, "status"); ok {
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
		if project, ok := secretary.TextField(a.Set, "project"); ok && item.Kind != "project" {
			pid, err := s.secretaryProjectTx(ctx, tx, scope, project, aliases)
			if err != nil {
				return skippedReceipt(a.Op, "找不到所属项目"), err
			}
			if err = call(workspace.Command{Type: "moveThing", ID: id, ProjectID: pid}); err != nil {
				return receipt, err
			}
		}
		if appendText, ok := secretary.TextField(a.Set, "notesAppend"); ok && strings.TrimSpace(appendText) != "" {
			current, err := getItem(ctx, tx, scope, id)
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
		item, err = getItem(ctx, tx, scope, id)
		if err != nil {
			return receipt, err
		}
		remind, hasRemind := secretary.TextField(a.Set, "remind")
		if item.Kind == "task" && (dueChanged || hasRemind) {
			if dueChanged && !hasRemind {
				hasExisting := false
				for _, t := range item.Triggers {
					if t.ID == "due-reminder" {
						hasExisting = true
					}
				}
				if !hasExisting {
					remind = secretary.ReminderValue(nil, dateOnly)
				}
			}
			secretary.ApplyDueReminder(&item, remind, loc)
			if hasRemind {
				item.RemindersOn = remind != "none"
			}
			if _, recorded := history.summaries[id]; !recorded {
				item.Version++
				item.UpdatedAt = stamp()
			}
			if err = s.saveAction(ctx, tx, scope, item, "调整提醒"); err != nil {
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
				receipt.Text += "：→ " + secretary.LocalDate(item.Due, loc)
			} else {
				receipt.Text += " → " + secretary.LocalDate(item.Due, loc)
			}
		}
		if item.Kind == "task" && (dueChanged || hasRemind) {
			if due, err := time.Parse(time.RFC3339, item.Due); err == nil && !due.After(time.Now()) {
				receipt.Text += " · 时间已过，没有设提醒"
			}
		}
		receipt.Text += note
	case "add_steps":
		item, err := getItem(ctx, tx, scope, id)
		if err != nil {
			return receipt, err
		}
		if item.Kind != "task" || len(a.Steps) == 0 || len(a.Steps) > 100 {
			return skippedReceipt(a.Op, "这件事不能加这些步骤"), nil
		}
		for _, step := range a.Steps {
			if !workspace.ValidTitle(step) {
				return skippedReceipt(a.Op, "步骤为空或太长"), nil
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
			return skippedReceipt(a.Op, "没说清要交给副手做什么"), nil
		}
		c := workspace.Command{Type: "requestRun", ThingID: id, AgentID: agent.ID, Kind: a.Kind, Prompt: a.Prompt}
		if a.Kind == "revise" {
			docs, _ := ctx.Value(secretaryDocumentsKey{}).(map[string]workspace.Doc)
			doc, ok := docs[pointerValue(a.DocumentID)]
			if !ok {
				return skippedReceipt(a.Op, "目标文档不明确"), nil
			}
			if a.BaseVersion == nil || *a.BaseVersion < 1 {
				return skippedReceipt(a.Op, "基准版本不明确"), nil
			}
			c.DocumentID = doc.ID
			c.BaseVersion = *a.BaseVersion
			c.ThingID = doc.ThingID
			id = doc.ThingID
			if a.Ref == "new" {
				return skippedReceipt(a.Op, "改写不能新建事项"), nil
			}
		}
		if history, ok := ctx.Value(delegateHistoryKey{}).(delegateHistoryContext); ok {
			c.DeskTurnIDs = append([]string{}, history.IDs...)
		}
		if a.Ref == "new" {
			if !workspace.ValidTitle(a.Title) {
				return skippedReceipt(a.Op, "标题为空或太长"), nil
			}
			id = string(memory.NewID())
			c.Type = "delegateTask"
			c.ID = id
			c.Title = strings.TrimSpace(a.Title)
			var err error
			c.ProjectID, err = currentStudioProjectTx(ctx, tx, scope, currentThingID)
			if err != nil {
				return receipt, err
			}
		}
		if err := call(c); err != nil {
			return receipt, err
		}
		receipt.Text = "交给 " + agent.Name + "：" + a.Prompt
	default:
		return skippedReceipt(a.Op, "不认识这个动作"), nil
	}
	for _, changedID := range history.ids {
		item, err := getItem(ctx, tx, scope, changedID)
		if err != nil {
			return receipt, err
		}
		summary := history.summaries[changedID]
		if summary == "创建" {
			summary = "新建：" + item.Title
		}
		if changedID == id {
			summary = receipt.Text
			switch a.Op {
			case "create_task":
				summary = "新建：" + strings.TrimPrefix(receipt.Text, "已建：")
			case "update":
				summary = "更新：" + strings.TrimPrefix(taskReceiptText(ctx, tx, scope, item, loc), "已建：") + note
				if item.Status == "done" {
					summary = "完成：" + item.Title
				}
			}
		}
		if err = s.saveAction(historyCtx, tx, scope, item, summary); err != nil {
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
func (s *Store) closeDateActionTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, a secretaryAction) (workspace.DeskReceipt, error) {
	receipt := workspace.DeskReceipt{Op: a.Op, Status: "done"}
	if smokeID(ctx) != "" {
		// A check-only turn is cleaned up by restoring item documents; a mark on
		// a memory is outside what that cleanup can put back, so it is not made.
		return skippedReceipt(a.Op, "检查模式下不改记忆上的日期"), nil
	}
	as, ok := validDeadlineClose(a.As)
	if !ok {
		return skippedReceipt(a.Op, "没说清是做完了还是不要了"), nil
	}
	memories, _ := ctx.Value(secretaryMemoriesKey{}).(map[string]workspace.Memory)
	m, ok := memories[a.Ref]
	if !ok {
		return skippedReceipt(a.Op, "找不到说的是哪一条"), nil
	}
	var title string
	var version int
	err := tx.QueryRow(ctx, "SELECT title,claim_version FROM deadlines WHERE owner_id=$1 AND claim_id=$2 ORDER BY id LIMIT 1", string(scope.OwnerID), m.ID).Scan(&title, &version)
	if errors.Is(err, pgx.ErrNoRows) {
		return skippedReceipt(a.Op, "这条记忆上没有期限或安排"), nil
	}
	if err != nil {
		return receipt, err
	}
	if err = activeClaim(ctx, tx, scope, m.ID); err != nil {
		return skippedReceipt(a.Op, "这条记忆已经不在了"), nil
	}
	if err = completeDeadlineTx(ctx, tx, scope, m.ID, version, as); errors.Is(err, memory.ErrConflict) {
		return skippedReceipt(a.Op, "这条记忆刚改过，再说一遍"), nil
	} else if err != nil {
		return receipt, err
	}
	receipt.Text = map[string]string{"done": "记下做完了：", "dropped": "不再提：", "task": "转成了待办，不再单独提："}[as] + title
	return receipt, nil
}
