package itemcommand

import (
	"context"
	"testing"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func create(c workspace.Command, origin Origin) (workspace.Item, error) {
	return New(context.Background(), c, origin, &effects{})
}

func TestANewItemNamesWhoMadeIt(t *testing.T) {
	task, err := create(workspace.Command{Type: "addTask", Title: "虚构的任务", Text: "虚构的说明", ProjectID: "p1", Due: "2099-03-02T07:00:00Z"}, Origin{Actor: "user"})
	if err != nil || task.Kind != "task" || task.Notes != "虚构的说明" || task.ProjectID != "p1" || task.Creation != nil || len(task.History) != 1 || task.History[0].By != "user" {
		t.Fatalf("%+v %v", task, err)
	}
	// A new task with a due time gets no reminder until a reminder is chosen.
	if task.Due != "2099-03-02T07:00:00Z" || len(task.Triggers) != 0 {
		t.Fatalf("%+v", task.Triggers)
	}
	project, _ := create(workspace.Command{Type: "addProject", Name: "虚构的项目", Title: "不用"}, Origin{Actor: "user"})
	if project.Kind != "project" || project.Title != "虚构的项目" {
		t.Fatalf("%+v", project)
	}
	if _, err := create(workspace.Command{Type: "addTask", Title: "虚构", Due: "明天"}, Origin{}); err != memory.ErrInvalid {
		t.Fatal(err)
	}
}

func TestTheSecretarysItemKeepsTheOwnersSentenceAndOneCreationEntry(t *testing.T) {
	source := workspace.SourceRef{SourceID: "s1", Excerpt: "虚构的原话"}
	// Inside an action, the secretary publishes the history at the end.
	collected, _ := create(workspace.Command{Type: "addTask", Title: "虚构的任务"}, Origin{Actor: "secretary", SecretarySource: &source, ActionID: "a1", Collected: true})
	if collected.Creation == nil || collected.Creation.By != "secretary" || collected.Creation.ActionID != "a1" || collected.Creation.Source.Excerpt != "虚构的原话" || len(collected.Sources) != 1 || len(collected.History) != 0 || len(collected.Evolution) != 0 {
		t.Fatalf("%+v", collected)
	}
	// A companion project is its own action. The save writes its one entry.
	companion, _ := create(workspace.Command{Type: "addProject", Name: "虚构的项目"}, Origin{Actor: "secretary", SecretarySource: &source})
	if len(companion.History) != 0 || len(companion.Evolution) != 1 {
		t.Fatalf("%+v %+v", companion.History, companion.Evolution)
	}
	// A sentence without a saved source is not listed as a source.
	unsaved, _ := create(workspace.Command{Type: "addIdea", Title: "虚构的想法"}, Origin{SecretarySource: &workspace.SourceRef{}})
	if len(unsaved.Sources) != 0 || unsaved.Creation == nil {
		t.Fatalf("%+v", unsaved)
	}
}
