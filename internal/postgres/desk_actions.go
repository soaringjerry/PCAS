package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
	"strings"
	"time"
	"unicode/utf8"
)

// applyDueReminder maintains the one fixed reminder independently of other triggers.
// An empty remind preserves the existing offset; the secretary supplies defaults.
func applyDueReminder(item *workspace.Item, remind string, loc *time.Location) {
	applyDueReminderAt(item, remind, loc, time.Now())
}

func applyDueReminderAt(item *workspace.Item, remind string, loc *time.Location, now time.Time) {
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

// Model actions never accept database identifiers. Refs resolve exclusively
// through the aliases in this turn's server-owned context.
type secretaryAction struct {
	parseErr     error
	Op           string                     `json:"op"`
	Ref          string                     `json:"ref"`
	Title        string                     `json:"title"`
	Name         string                     `json:"name"`
	Due          *string                    `json:"due"`
	Remind       *string                    `json:"remind"`
	Project      *string                    `json:"project"`
	Notes        *string                    `json:"notes"`
	OwedTo       *string                    `json:"owedTo"`
	WaitingFor   *string                    `json:"waitingFor"`
	Condition    *string                    `json:"condition"`
	ConditionDue *string                    `json:"conditionDue"`
	Set          map[string]json.RawMessage `json:"set"`
	Steps        []string                   `json:"steps"`
	Kind         string                     `json:"kind"`
	Prompt       string                     `json:"prompt"`
}
type secretaryOutput struct {
	Reply    string             `json:"reply"`
	Used     []string           `json:"used"`
	Links    []string           `json:"links"`
	Show     []string           `json:"show"`
	Remember bool               `json:"remember"`
	Actions  []secretaryAction  `json:"actions"`
	Ask      *workspace.DeskAsk `json:"ask"`
}

// Internal commands still persist in order for validation and undo collection.
// Publish history and its memory source only after the whole secretary action,
// including reminder changes, has reached its final state.
type secretaryHistoryKey struct{}
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

func textField(fields map[string]json.RawMessage, key string) (string, bool) {
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
func validDeskTitle(title string) bool {
	return strings.TrimSpace(title) != "" && utf8.RuneCountInString(title) <= 200
}
func deskDue(value string, loc *time.Location) (string, bool, bool) {
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
			at = at.Add(23*time.Hour + 59*time.Minute)
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
		if !validDeskTitle(name) {
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
		err = s.commandTx(ctx, tx, scope, workspace.Command{Type: "addProject", ID: id, Name: name})
		return id, err
	}
	return "", memory.ErrInvalid
}
func reminderValue(remind *string, dateOnly bool) string {
	if remind != nil {
		return *remind
	}
	if dateOnly {
		return "09:00"
	}
	return "-30m"
}
func localDeskDate(due string, loc *time.Location) string {
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
func taskReceiptText(ctx context.Context, tx pgx.Tx, scope memory.Scope, item workspace.Item, loc *time.Location) string {
	parts := []string{}
	if item.Due != "" {
		parts = append(parts, localDeskDate(item.Due, loc)+" "+item.Title)
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
	case "create_task", "create_idea", "create_project":
		title := strings.TrimSpace(a.Title)
		if a.Op == "create_project" {
			title = strings.TrimSpace(a.Name)
		}
		if !validDeskTitle(title) {
			return skippedReceipt(a.Op, "标题为空或太长"), nil
		}
		id = string(memory.NewID())
		project := ""
		var err error
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
				due, date, ok := deskDue(*a.Due, loc)
				dateOnly = date
				if ok {
					patch["due"] = due
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
			if len(patch) > 0 {
				if err = call(workspace.Command{Type: "updateTask", ID: id, Patch: asJSON(patch)}); err != nil {
					return receipt, err
				}
			}
			item, err := getItem(ctx, tx, scope, id)
			if err != nil {
				return receipt, err
			}
			applyDueReminder(&item, reminderValue(a.Remind, dateOnly), loc)
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
					due, _, ok = deskDue(*a.ConditionDue, loc)
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
			receipt.Text = "新建项目：" + title
		}
	case "update":
		item, err := getItem(ctx, tx, scope, id)
		if err != nil {
			return receipt, err
		}
		if title, ok := textField(a.Set, "title"); ok {
			if !validDeskTitle(title) {
				return skippedReceipt(a.Op, "标题为空或太长"), nil
			}
			if err = call(workspace.Command{Type: "renameThing", ID: id, Title: strings.TrimSpace(title)}); err != nil {
				return receipt, err
			}
		}
		patch := map[string]string{}
		dateOnly := false
		dueChanged := false
		if due, ok := textField(a.Set, "due"); ok && item.Kind == "task" {
			parsed, date, valid := deskDue(due, loc)
			dateOnly = date
			if valid {
				patch["due"] = parsed
				dueChanged = true
			} else {
				note = " · 时间没看懂"
			}
		}
		if len(patch) > 0 {
			if err = call(workspace.Command{Type: "updateTask", ID: id, Patch: asJSON(patch)}); err != nil {
				return receipt, err
			}
		}
		if status, ok := textField(a.Set, "status"); ok {
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
		if project, ok := textField(a.Set, "project"); ok && item.Kind != "project" {
			pid, err := s.secretaryProjectTx(ctx, tx, scope, project, aliases)
			if err != nil {
				return skippedReceipt(a.Op, "找不到所属项目"), err
			}
			if err = call(workspace.Command{Type: "moveThing", ID: id, ProjectID: pid}); err != nil {
				return receipt, err
			}
		}
		if appendText, ok := textField(a.Set, "notesAppend"); ok && strings.TrimSpace(appendText) != "" {
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
		remind, hasRemind := textField(a.Set, "remind")
		if item.Kind == "task" && (dueChanged || hasRemind) {
			if dueChanged && !hasRemind {
				hasExisting := false
				for _, t := range item.Triggers {
					if t.ID == "due-reminder" {
						hasExisting = true
					}
				}
				if !hasExisting {
					remind = reminderValue(nil, dateOnly)
				}
			}
			applyDueReminder(&item, remind, loc)
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
				receipt.Text += "：→ " + localDeskDate(item.Due, loc)
			} else {
				receipt.Text += " → " + localDeskDate(item.Due, loc)
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
			if !validDeskTitle(step) {
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
		if !oneOf(a.Kind, "plan", "draft", "breakdown", "summary", "ask") || strings.TrimSpace(a.Prompt) == "" {
			return skippedReceipt(a.Op, "没说清要交给副手做什么"), nil
		}
		c := workspace.Command{Type: "requestRun", ThingID: id, AgentID: agent.ID, Kind: a.Kind, Prompt: a.Prompt}
		if a.Ref == "new" {
			if !validDeskTitle(a.Title) {
				return skippedReceipt(a.Op, "标题为空或太长"), nil
			}
			id = string(memory.NewID())
			c.Type = "delegateTask"
			c.ID = id
			c.Title = strings.TrimSpace(a.Title)
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
