package itemcommand

import (
	"context"
	"testing"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// review records the writes of a candidate review.
type review struct {
	remembered []string
	items      map[string]workspace.Item
}

func (r *review) Remember(_ context.Context, text, nature, _ string, _ workspace.SourceRef) (string, error) {
	r.remembered = append(r.remembered, nature+":"+text)
	return "m1", nil
}
func (r *review) Item(_ context.Context, id string) (workspace.Item, error) {
	item, ok := r.items[id]
	if !ok {
		return item, memory.ErrNotFound
	}
	return item, nil
}
func (r *review) SaveItem(_ context.Context, item workspace.Item) error {
	r.items[item.ID] = item
	return nil
}

func pending() workspace.Candidate {
	return workspace.Candidate{ID: "c1", State: "pending", Text: "虚构的记录", Source: workspace.SourceRef{SourceID: "s1", Version: 1}}
}

func TestAnAcceptedCandidateBecomesAMemoryOrAnItemOnce(t *testing.T) {
	r := &review{items: map[string]workspace.Item{}}
	v := pending()
	if err := ReviewCandidate(context.Background(), &v, workspace.Command{Type: "acceptCandidate", Kind: "memory", Text: "喜欢茶"}, r); err != nil || v.State != "accepted" || v.ResolvedInto != "m1" || r.remembered[0] != "fact:喜欢茶" {
		t.Fatal(err, v, r.remembered)
	}
	if err := ReviewCandidate(context.Background(), &v, workspace.Command{Type: "acceptCandidate", Kind: "memory", Text: "喜欢茶"}, r); err != memory.ErrConflict || len(r.remembered) != 1 {
		t.Fatal("a candidate is accepted once", err)
	}
	v = pending()
	if err := ReviewCandidate(context.Background(), &v, workspace.Command{Type: "acceptCandidate", Kind: "task", Text: "交报告", ProjectID: "p1", Due: "2099-03-02T07:00:00Z"}, r); err != nil || len(r.items) != 1 {
		t.Fatal(err)
	}
	if task := r.items[v.ResolvedInto]; task.Kind != "task" || task.Title != "交报告" || task.ProjectID != "p1" || len(task.Sources) != 1 {
		t.Fatalf("%+v", task)
	}
	v = pending()
	for _, c := range []workspace.Command{{Type: "acceptCandidate", Kind: "note", Text: "虚构"}, {Type: "acceptCandidate", Kind: "task", Text: " "}, {Type: "eraseCandidate"}} {
		if err := ReviewCandidate(context.Background(), &v, c, r); err != memory.ErrInvalid || v.State != "pending" {
			t.Fatal(c.Type, c.Kind, err)
		}
	}
}

func TestOnlyAnIgnoredCandidateIsRestoredAndAMergeAddsToItsItem(t *testing.T) {
	r := &review{items: map[string]workspace.Item{"t1": {ID: "t1", Kind: "task", Notes: "已有", Version: 1}}}
	v := pending()
	if err := ReviewCandidate(context.Background(), &v, workspace.Command{Type: "restoreCandidate"}, r); err != memory.ErrConflict {
		t.Fatal(err)
	}
	if err := ReviewCandidate(context.Background(), &v, workspace.Command{Type: "ignoreCandidate"}, r); err != nil || v.State != "ignored" {
		t.Fatal(err)
	}
	if err := ReviewCandidate(context.Background(), &v, workspace.Command{Type: "mergeCandidate", TargetID: "t1"}, r); err != memory.ErrConflict {
		t.Fatal("an ignored candidate is not merged", err)
	}
	if err := ReviewCandidate(context.Background(), &v, workspace.Command{Type: "restoreCandidate"}, r); err != nil || v.State != "pending" {
		t.Fatal(err)
	}
	if err := ReviewCandidate(context.Background(), &v, workspace.Command{Type: "mergeCandidate", TargetID: "t1"}, r); err != nil || v.State != "merged" || v.ResolvedInto != "t1" {
		t.Fatal(err)
	}
	if task := r.items["t1"]; task.Notes != "已有\n虚构的记录" || task.Version != 2 || len(task.Sources) != 1 {
		t.Fatalf("%+v", task)
	}
}

func TestABulkCommandChangesEachItemOnceAndRefusesTooMany(t *testing.T) {
	lookups := 0
	candidate := func(id string) (workspace.Candidate, error) {
		lookups++
		return workspace.Candidate{Kind: "task", Text: "来自" + id, ProjectID: "p1"}, nil
	}
	commands, err := Expand(workspace.Command{Type: "bulkAccept", IDs: []string{"a", "b", "a"}}, candidate)
	if err != nil || len(commands) != 2 || lookups != 2 || commands[1].Type != "acceptCandidate" || commands[1].ID != "b" || commands[1].Text != "来自b" || commands[1].IDs != nil {
		t.Fatalf("%+v %v", commands, err)
	}
	status, _ := Expand(workspace.Command{Type: "bulkStatus", Status: "done", IDs: []string{"t1"}}, candidate)
	if status[0].Type != "setTaskStatus" || status[0].Status != "done" || lookups != 2 {
		t.Fatalf("%+v", status)
	}
	if _, err := Expand(workspace.Command{Type: "bulkIgnore"}, candidate); err != memory.ErrInvalid {
		t.Fatal(err)
	}
	if _, err := Expand(workspace.Command{Type: "bulkIgnore", IDs: make([]string, 101)}, candidate); err != memory.ErrInvalid {
		t.Fatal(err)
	}
}
