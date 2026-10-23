package secretary

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// desk is a workspace in memory. It applies the few commands that the tests
// need and records each command.
type desk struct {
	items    map[string]workspace.Item
	commands []workspace.Command
	changed  []Change
	docs     map[string]workspace.Doc
	projects map[string]bool
	// The remembered dates that the turn showed.
	checkOnly, changedDate bool
	shown                  map[string]workspace.Memory
	dates                  map[string]string
	closed                 []string
}

func newDesk(items ...workspace.Item) *desk {
	d := &desk{items: map[string]workspace.Item{}, docs: map[string]workspace.Doc{}, projects: map[string]bool{}}
	for _, item := range items {
		d.items[item.ID] = item
	}
	return d
}

func (d *desk) note(id, summary string) {
	if !d.Recorded(id) {
		d.changed = append(d.changed, Change{id, summary})
	}
}
func (d *desk) Command(_ context.Context, c workspace.Command) error {
	d.commands = append(d.commands, c)
	item := d.items[c.ID]
	switch c.Type {
	case "addTask", "addIdea", "addProject":
		item = workspace.Item{ID: c.ID, Title: c.Title, ProjectID: c.ProjectID, Kind: map[string]string{"addTask": "task", "addIdea": "idea", "addProject": "project"}[c.Type]}
		d.note(c.ID, "创建")
	case "updateTask":
		var patch struct {
			Due    *string `json:"due"`
			Urgent *bool   `json:"urgent"`
		}
		if err := json.Unmarshal(c.Patch, &patch); err != nil {
			return err
		}
		if patch.Due != nil {
			item.Due = *patch.Due
		}
		if patch.Urgent != nil {
			item.Urgent = *patch.Urgent
		}
		d.note(c.ID, "更新")
	case "setTaskStatus":
		item.Status = c.Status
		d.note(c.ID, "更新")
	case "renameThing":
		item.Title = c.Title
		d.note(c.ID, "更新")
	default:
		d.note(c.ID, "更新")
	}
	d.items[c.ID] = item
	return nil
}
func (d *desk) Item(_ context.Context, id string) (workspace.Item, error) {
	item, ok := d.items[id]
	if !ok {
		return item, memory.ErrNotFound
	}
	return item, nil
}
func (d *desk) SaveItem(_ context.Context, item workspace.Item) error {
	d.items[item.ID] = item
	return nil
}
func (d *desk) NoteChange(_ context.Context, item workspace.Item, summary string) error {
	d.items[item.ID] = item
	d.note(item.ID, summary)
	return nil
}
func (d *desk) Recorded(id string) bool {
	for _, c := range d.changed {
		if c.ID == id {
			return true
		}
	}
	return false
}
func (d *desk) Changed() []Change { return d.changed }
func (d *desk) Publish(_ context.Context, item workspace.Item, summary string) error {
	item.History = append(item.History, workspace.Revision{Summary: summary})
	d.items[item.ID] = item
	return nil
}
func (d *desk) ProjectNamed(_ context.Context, title string) (bool, error) {
	return d.projects[title], nil
}
func (d *desk) CurrentProject(context.Context, string) (string, error) { return "", nil }
func (d *desk) ResolveProject(_ context.Context, value string, aliases map[string]workspace.Item) (string, error) {
	if p, ok := aliases[value]; ok && p.Kind == "project" {
		return p.ID, nil
	}
	return "", memory.ErrInvalid
}
func (d *desk) CheckOnly(context.Context) bool { return d.checkOnly }
func (d *desk) Shown(_ context.Context, alias string) (workspace.Memory, bool) {
	m, ok := d.shown[alias]
	return m, ok
}
func (d *desk) RememberedDate(_ context.Context, id string) (string, int, bool, error) {
	title, ok := d.dates[id]
	return title, 3, ok, nil
}
func (d *desk) MemoryActive(context.Context, string) error { return nil }
func (d *desk) CloseRememberedDate(_ context.Context, id string, version int, as string) error {
	if d.changedDate {
		return memory.ErrConflict
	}
	d.closed = append(d.closed, id+" "+as)
	return nil
}
func (d *desk) Document(alias string) (workspace.Doc, bool) { doc, ok := d.docs[alias]; return doc, ok }
func (d *desk) DelegateTurns() []string                     { return nil }

func text(v string) *string { return &v }

var zone = time.FixedZone("fictitious", 8*3600)

func execute(d *desk, a Action, aliases map[string]workspace.Item) workspace.DeskReceipt {
	receipt, err := Execute(context.Background(), d, a, Env{Aliases: aliases, Agent: workspace.Agent{ID: "a1", Name: "虚构副手"}, Location: zone})
	if err != nil {
		panic(err)
	}
	return receipt
}

func commandTypes(d *desk) string {
	types := []string{}
	for _, c := range d.commands {
		types = append(types, c.Type)
	}
	return strings.Join(types, ",")
}

func TestCreateTaskSetsItsDueTimeItsReminderAndItsHistory(t *testing.T) {
	d := newDesk()
	receipt := execute(d, Action{Op: "create_task", Title: " 交虚构的报告 ", Due: text("2099-03-02")}, nil)
	if receipt.Status != "done" || receipt.ThingID == nil || commandTypes(d) != "addTask,updateTask" || !strings.HasPrefix(receipt.Text, "已建：") || !strings.Contains(receipt.Text, "交虚构的报告") || !strings.Contains(receipt.Text, "09:00 提醒") {
		t.Fatalf("%+v %s", receipt, commandTypes(d))
	}
	task := d.items[*receipt.ThingID]
	// A date is due at the end of that day where the owner is, and it is reminded at 09:00.
	if task.Due != "2099-03-02T15:59:00Z" || len(task.Triggers) != 1 || task.Triggers[0].Offset != "09:00" {
		t.Fatalf("%+v", task)
	}
	if len(task.History) != 1 || !strings.HasPrefix(task.History[0].Summary, "新建：") {
		t.Fatalf("%+v", task.History)
	}
	// A due time that cannot be read does not stop the task.
	d = newDesk()
	receipt = execute(d, Action{Op: "create_task", Title: "虚构的事", Due: text("下周某天")}, nil)
	if receipt.Status != "done" || !strings.HasSuffix(receipt.Text, " · 时间没看懂") || d.items[*receipt.ThingID].Due != "" {
		t.Fatalf("%+v", receipt)
	}
}

func TestAnActionThatCannotBeDoneIsSkippedWithItsReason(t *testing.T) {
	task := workspace.Item{ID: "t1", Kind: "task", Title: "虚构的任务"}
	idea := workspace.Item{ID: "i1", Kind: "idea", Title: "虚构的想法"}
	aliases := map[string]workspace.Item{"T1": task, "I1": idea}
	duplicate := newDesk()
	duplicate.projects["虚构的项目"] = true
	for name, tc := range map[string]struct {
		desk   *desk
		action Action
		reason string
	}{
		"an alias that the turn did not show": {newDesk(task), Action{Op: "update", Ref: "t1"}, "找不到要改的那件事"},
		"an empty title":                      {newDesk(), Action{Op: "create_task", Title: "  "}, "标题为空或太长"},
		"a project that exists":               {duplicate, Action{Op: "create_project", Name: "虚构的项目"}, "已有同名项目，沿用已有项目"},
		"steps for an idea":                   {newDesk(task, idea), Action{Op: "add_steps", Ref: "I1", Steps: []string{"虚构的一步"}}, "这件事不能加这些步骤"},
		"a delegation without a prompt":       {newDesk(task), Action{Op: "delegate", Ref: "T1", Kind: "plan"}, "没说清要交给副手做什么"},
		"a revision without its document":     {newDesk(task), Action{Op: "delegate", Ref: "T1", Kind: "revise", Prompt: "虚构"}, "目标文档不明确"},
		"an unknown action":                   {newDesk(), Action{Op: "erase_everything"}, "不认识这个动作"},
	} {
		receipt := execute(tc.desk, tc.action, aliases)
		if receipt.Status != "skipped" || receipt.Reason != tc.reason || len(tc.desk.commands) != 0 {
			t.Fatalf("%s: %+v %s", name, receipt, commandTypes(tc.desk))
		}
	}
}

func TestUpdateNamesWhatChangedForTheOwner(t *testing.T) {
	task := workspace.Item{ID: "t1", Kind: "task", Title: "虚构的任务"}
	aliases := map[string]workspace.Item{"T1": task}
	d := newDesk(task)
	receipt := execute(d, Action{Op: "update", Ref: "T1", Set: map[string]json.RawMessage{"status": json.RawMessage(`"done"`)}}, aliases)
	if receipt.Text != "已完成：虚构的任务" || commandTypes(d) != "setTaskStatus" || d.items["t1"].History[0].Summary != "完成：虚构的任务" {
		t.Fatalf("%+v %+v", receipt, d.items["t1"].History)
	}
	d = newDesk(task)
	receipt = execute(d, Action{Op: "update", Ref: "T1", Set: map[string]json.RawMessage{"due": json.RawMessage(`"2099-03-02T15:00"`)}}, aliases)
	// A new due time on a task without a reminder gets the default reminder.
	if !strings.HasPrefix(receipt.Text, "已改：虚构的任务 → ") || d.items["t1"].Triggers[0].Offset != "-30m" {
		t.Fatalf("%+v %+v", receipt, d.items["t1"].Triggers)
	}
	// A status of another kind of item is not applied.
	d = newDesk(task)
	receipt = execute(d, Action{Op: "update", Ref: "T1", Set: map[string]json.RawMessage{"status": json.RawMessage(`"paused"`)}}, aliases)
	if len(d.commands) != 0 || receipt.Status != "done" {
		t.Fatalf("%+v %s", receipt, commandTypes(d))
	}
	// A delegation gives the run to the agent of the turn.
	d = newDesk(task)
	receipt = execute(d, Action{Op: "delegate", Ref: "T1", Kind: "plan", Prompt: "虚构的计划"}, aliases)
	if receipt.Text != "交给 虚构副手：虚构的计划" || d.commands[0].Type != "requestRun" || d.commands[0].ThingID != "t1" || d.commands[0].AgentID != "a1" {
		t.Fatalf("%+v %+v", receipt, d.commands)
	}
}

func TestARememberedDateIsClosedOnlyWhenItIsClearWhichAndHow(t *testing.T) {
	d := newDesk()
	d.shown = map[string]workspace.Memory{"M1": {ID: "m1"}, "M2": {ID: "m2"}}
	d.dates = map[string]string{"m1": "交房租"}
	env := Env{Aliases: map[string]workspace.Item{}, Location: time.UTC}
	for want, a := range map[string]Action{
		"不再提：交房租":      {Op: "close_date", Ref: "M1", As: "dropped"},
		"没说清是做完了还是不要了": {Op: "close_date", Ref: "M1", As: "maybe"},
		"找不到说的是哪一条":    {Op: "close_date", Ref: "M9"},
		"这条记忆上没有期限或安排": {Op: "close_date", Ref: "M2"},
	} {
		receipt, err := Execute(context.Background(), d, a, env)
		if err != nil || !strings.Contains(receipt.Text+receipt.Reason, want) {
			t.Errorf("%s: %+v %v", want, receipt, err)
		}
	}
	if len(d.closed) != 1 || d.closed[0] != "m1 dropped" {
		t.Fatalf("%v", d.closed)
	}
	d.checkOnly = true
	if receipt, _ := Execute(context.Background(), d, Action{Op: "close_date", Ref: "M1"}, env); receipt.Status == "done" || len(d.closed) != 1 {
		t.Fatal("a check-only turn closed a remembered date")
	}
}
