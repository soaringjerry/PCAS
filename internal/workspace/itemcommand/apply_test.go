package itemcommand

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// effects records the task that an idea becomes.
type effects struct{ promoted []workspace.Item }

func (e *effects) Location(context.Context) (*time.Location, error) {
	return time.FixedZone("fictitious", 8*3600), nil
}
func (e *effects) PromoteIdea(_ context.Context, _ string, task workspace.Item) error {
	e.promoted = append(e.promoted, task)
	return nil
}

func apply(item *workspace.Item, c workspace.Command) (string, error) {
	return Apply(context.Background(), item, c, &effects{})
}

func TestACommandChangesTheItemAndNamesTheChange(t *testing.T) {
	task := workspace.NewItem("task", "虚构的任务")
	summary, err := apply(&task, workspace.Command{Type: "setTaskStatus", Status: "doing"})
	if err != nil || task.Status != "doing" || summary != "状态改为 doing" || task.Version != 2 {
		t.Fatal(summary, err, task.Version)
	}
	if _, err := apply(&task, workspace.Command{Type: "addCheck", Text: "虚构的一步"}); err != nil || len(task.Checklist) != 1 {
		t.Fatal(err)
	}
	step := task.Checklist[0].ID
	if _, err := apply(&task, workspace.Command{Type: "toggleCheck", ItemID: step}); err != nil || !task.Checklist[0].Done {
		t.Fatal(err)
	}
	if _, err := apply(&task, workspace.Command{Type: "removeCheck", ItemID: "missing"}); err != memory.ErrNotFound {
		t.Fatal(err)
	}
	project := workspace.NewItem("project", "虚构的项目")
	if summary, err := apply(&project, workspace.Command{Type: "renameThing", Title: "新名字"}); err != nil || project.Name != "新名字" || summary != "更改名称" {
		t.Fatal(summary, err)
	}
	if summary, _ := apply(&project, workspace.Command{Type: "setNotes", Text: "虚构的目标"}); project.Goal != "虚构的目标" || summary != "更新说明" {
		t.Fatal(summary)
	}
}

func TestACommandOfOneKindDoesNotChangeAnotherKind(t *testing.T) {
	idea := workspace.NewItem("idea", "虚构的想法")
	task := workspace.NewItem("task", "虚构的任务")
	for name, tc := range map[string]struct {
		item    workspace.Item
		command workspace.Command
	}{
		"a task status on an idea":    {idea, workspace.Command{Type: "setTaskStatus", Status: "done"}},
		"a step on an idea":           {idea, workspace.Command{Type: "addCheck", Text: "虚构"}},
		"an idea command on a task":   {task, workspace.Command{Type: "ideaDrop"}},
		"a wake condition on a task":  {task, workspace.Command{Type: "addCondition", Description: "虚构"}},
		"a project patch on a task":   {task, workspace.Command{Type: "updateProject", Patch: json.RawMessage(`{"goal":"虚构"}`)}},
		"an unknown command":          {task, workspace.Command{Type: "eraseEverything"}},
		"a deferral of too many days": {task, workspace.Command{Type: "deferTask", Days: 4000}},
		"a field that is not allowed": {task, workspace.Command{Type: "updateTask", Patch: json.RawMessage(`{"history":[]}`)}},
		"a value of the wrong type":   {task, workspace.Command{Type: "updateTask", Patch: json.RawMessage(`{"urgent":"yes"}`)}},
	} {
		item := tc.item
		if _, err := apply(&item, tc.command); err != memory.ErrInvalid || item.Version != 1 || item.Status != tc.item.Status {
			t.Fatal(name, err, item.Version)
		}
	}
}

func TestANewDueTimeOfATaskIsATimeAndKeepsItsReminder(t *testing.T) {
	task := workspace.NewItem("task", "虚构的任务")
	dateOnly := true
	task.DueDateOnly = &dateOnly
	task.Triggers = []workspace.Trigger{{ID: "due-reminder", Kind: "time", Offset: "-30m"}}
	if _, err := apply(&task, workspace.Command{Type: "updateTask", Patch: json.RawMessage(`{"due":"2099-03-02T07:00:00Z","estimatedHours":3}`)}); err != nil {
		t.Fatal(err)
	}
	// A due time without the date mark is a time. Hours that the owner sets are the owner's.
	if task.DueDateOnly == nil || *task.DueDateOnly || task.EffortSource != "user" || len(task.Triggers) != 1 || !task.Triggers[0].Active || task.Triggers[0].NextAt != "2099-03-02T06:30:00Z" {
		t.Fatalf("%+v %+v", task.DueDateOnly, task.Triggers)
	}
	// A null field is removed.
	if _, err := apply(&task, workspace.Command{Type: "updateTask", Patch: json.RawMessage(`{"estimatedHours":null}`)}); err != nil || task.EstimatedHours != nil {
		t.Fatal(err, task.EstimatedHours)
	}
}

func TestAnIdeaBecomesATaskOnceAndAShelvedIdeaKeepsItsCondition(t *testing.T) {
	idea := workspace.NewItem("idea", "虚构的想法")
	idea.Body, idea.ProjectID = "虚构的说明", "p1"
	fx := &effects{}
	summary, err := Apply(context.Background(), &idea, workspace.Command{Type: "ideaPromote"}, fx)
	if err != nil || summary != "转为待办" || idea.Status != "promoted" || len(fx.promoted) != 1 {
		t.Fatal(summary, err)
	}
	if task := fx.promoted[0]; task.Kind != "task" || task.Title != "虚构的想法" || task.Notes != "虚构的说明" || task.ProjectID != "p1" || task.IdeaID != idea.ID {
		t.Fatalf("%+v", task)
	}
	if _, err := Apply(context.Background(), &idea, workspace.Command{Type: "ideaPromote"}, fx); err != memory.ErrConflict || len(fx.promoted) != 1 {
		t.Fatal("an idea becomes a task once", err)
	}
	shelved := workspace.NewItem("idea", "另一个想法")
	if summary, err := apply(&shelved, workspace.Command{Type: "ideaShelve", Reason: "时机未到", Condition: "虚构的条件"}); err != nil || summary != "搁置：时机未到" || shelved.Status != "shelved" || len(shelved.Conditions) != 1 || shelved.Conditions[0].Kind != "event" {
		t.Fatal(summary, err)
	}
	if _, err := apply(&shelved, workspace.Command{Type: "ideaSnooze", Days: 3}); err != memory.ErrConflict {
		t.Fatal("an idea that is not awake cannot be snoozed", err)
	}
	if Target(workspace.Command{ID: "a", TaskID: "t"}) != "t" || Target(workspace.Command{ID: "a", IdeaID: "i"}) != "i" || Target(workspace.Command{ID: "a"}) != "a" {
		t.Fatal("target")
	}
}
