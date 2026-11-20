package command

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/workspace"
)

// undoStore is an in-memory UndoUnit with one action.
type undoStore struct {
	UndoUnit
	action    Action
	items     map[string]workspace.Item
	children  []workspace.Item
	statuses  []string
	changed   bool
	saved     []string
	removed   []string
	suppress  int
	undone    bool
	forgotten string
}

func (s *undoStore) TopicProject(context.Context, string) (bool, error) { return false, nil }
func (s *undoStore) Comparison(context.Context, string) (bool, error)   { return false, nil }
func (s *undoStore) Action(ctx context.Context, _ string) (context.Context, Action, error) {
	return ctx, s.action, nil
}
func (s *undoStore) CheckSuccessors(context.Context, Action) error         { return nil }
func (s *undoStore) RunStatuses(context.Context, string) ([]string, error) { return s.statuses, nil }
func (s *undoStore) RunStatus(context.Context, string) (string, error)     { return "", nil }
func (s *undoStore) Unchanged(context.Context, Change) (bool, error)       { return !s.changed, nil }
func (s *undoStore) Item(_ context.Context, id string) (workspace.Item, error) {
	return s.items[id], nil
}
func (s *undoStore) Children(context.Context, string) ([]workspace.Item, error) {
	return s.children, nil
}
func (s *undoStore) SaveAction(_ context.Context, item workspace.Item, summary string) error {
	s.saved = append(s.saved, item.ID+" "+summary)
	return nil
}
func (s *undoStore) RemoveItem(_ context.Context, id string) error {
	s.removed = append(s.removed, id)
	return nil
}
func (s *undoStore) Suppress(context.Context, workspace.Item, workspace.Trigger) error {
	s.suppress++
	return nil
}
func (s *undoStore) Undone(context.Context, Action) error { s.undone = true; return nil }
func (s *undoStore) ForgetTurn(_ context.Context, turn string) error {
	s.forgotten = turn
	return nil
}

const actionID = "33333333-3333-4333-8333-333333333333"

func made(id string) Change {
	return Change{Table: "work_items", ID: id, Before: json.RawMessage("null")}
}

func undo(s *undoStore) error {
	return Undo(context.Background(), s, actionID, time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC))
}

func TestAnActionIsUndoneOnceWithinItsWindowAndUnchanged(t *testing.T) {
	for want, change := range map[error]func(*undoStore){
		workspace.ErrAlreadyUndone: func(s *undoStore) { s.action.Undone = true },
		workspace.ErrExpired:       func(s *undoStore) { s.action.Expired = true },
		workspace.ErrChangedSince:  func(s *undoStore) { s.changed = true },
		workspace.ErrWorkStarted:   func(s *undoStore) { s.statuses = []string{"queued", "running"} },
	} {
		s := &undoStore{action: Action{Changes: []Change{made("t")}}}
		change(s)
		if err := undo(s); !errors.Is(err, want) || s.undone || len(s.removed) != 0 {
			t.Fatalf("%v: %v", want, err)
		}
	}
}

func TestUndoingASecretaryProjectReturnsItsItemsToNoProject(t *testing.T) {
	project := workspace.Item{ID: "p", Kind: "project", Creation: &workspace.ItemCreation{By: "secretary"}}
	s := &undoStore{action: Action{Source: "desk", Turn: new(string), Changes: []Change{made("p")}}, items: map[string]workspace.Item{"p": project}, children: []workspace.Item{{ID: "t", ProjectID: "p", Version: 1}}}
	*s.action.Turn = "turn"
	if err := undo(s); err != nil {
		t.Fatal(err)
	}
	if len(s.saved) != 1 || s.saved[0] != "t 撤销项目，事项回到无项目" || s.removed[0] != "p" || !s.undone || s.forgotten != "turn" {
		t.Fatalf("%+v", s)
	}
	project.Creation.By = "user"
	s = &undoStore{action: Action{Changes: []Change{made("p")}}, items: map[string]workspace.Item{"p": project}, children: []workspace.Item{{ID: "t"}}}
	if err := undo(s); !errors.Is(err, workspace.ErrChangedSince) || len(s.removed) != 0 {
		t.Fatal("an owner's project with items was removed by undo", err)
	}
}

func TestARestoredTaskKeepsItsVersionOrderAndDoesNotRemindOfAnOldTime(t *testing.T) {
	before, _ := json.Marshal(workspace.Item{ID: "t", Kind: "task", Status: "todo", Triggers: []workspace.Trigger{{ID: "r", Active: true, NextAt: "2026-03-01T00:00:00Z"}}})
	s := &undoStore{action: Action{Summary: "完成", Changes: []Change{{Table: "work_items", ID: "t", Before: before}}}, items: map[string]workspace.Item{"t": {ID: "t", Kind: "task", Status: "done", Version: 4}}}
	if err := undo(s); err != nil {
		t.Fatal(err)
	}
	if len(s.saved) != 1 || s.saved[0] != "t 撤销：完成" || s.suppress != 1 || s.forgotten != "" {
		t.Fatalf("%+v", s)
	}
}
