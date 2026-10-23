package command

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
	"github.com/soaringjerry/PCAS/internal/workspace/itemcommand"
)

// fakeUnit implements the methods that these tests reach. Any other method
// panics through the nil embedded interface.
type fakeUnit struct {
	Unit
	agent      workspace.Agent
	available  bool
	items      map[string]workspace.Item
	saved      []workspace.Item
	runs       []workspace.Command
	docs       map[string]workspace.Doc
	savedDocs  []workspace.Doc
	deleted    []string
	children   bool
	running    bool
	candidates []workspace.Candidate
	changed    bool
	authorized bool
}

func newFake() *fakeUnit {
	return &fakeUnit{agent: workspace.Agent{ID: "a", Enabled: true, Channel: "model"}, available: true, items: map[string]workspace.Item{}, docs: map[string]workspace.Doc{}}
}

func (f *fakeUnit) Agent(context.Context, string) (workspace.Agent, error) { return f.agent, nil }
func (f *fakeUnit) Available(string) bool                                  { return f.available }
func (f *fakeUnit) Origin(context.Context) itemcommand.Origin {
	return itemcommand.Origin{Actor: "owner"}
}
func (f *fakeUnit) Actor(context.Context) string                     { return "owner" }
func (f *fakeUnit) Location(context.Context) (*time.Location, error) { return time.UTC, nil }
func (f *fakeUnit) ItemExists(_ context.Context, id string) (bool, error) {
	_, ok := f.items[id]
	return ok, nil
}
func (f *fakeUnit) Item(_ context.Context, id string) (workspace.Item, error) {
	item, ok := f.items[id]
	if !ok {
		return item, memory.ErrNotFound
	}
	return item, nil
}
func (f *fakeUnit) SaveAction(_ context.Context, item workspace.Item, _ string) error {
	f.items[item.ID] = item
	f.saved = append(f.saved, item)
	return nil
}
func (f *fakeUnit) RunCommand(_ context.Context, c workspace.Command) error {
	f.runs = append(f.runs, c)
	return nil
}
func (f *fakeUnit) Document(_ context.Context, id string) (workspace.Doc, error) {
	doc, ok := f.docs[id]
	if !ok {
		return doc, memory.ErrNotFound
	}
	return doc, nil
}
func (f *fakeUnit) DocumentExists(_ context.Context, id string) (bool, error) {
	_, ok := f.docs[id]
	return ok, nil
}
func (f *fakeUnit) DeleteDocument(_ context.Context, id string) error {
	f.deleted = append(f.deleted, id)
	return nil
}
func (f *fakeUnit) SaveDoc(_ context.Context, doc workspace.Doc) error {
	f.savedDocs = append(f.savedDocs, doc)
	return nil
}
func (f *fakeUnit) ItemUse(context.Context, string) (bool, bool, error) {
	return f.children, f.running, nil
}
func (f *fakeUnit) DeleteItem(_ context.Context, item workspace.Item) error {
	f.deleted = append(f.deleted, item.ID)
	return nil
}
func (f *fakeUnit) Capture(context.Context, memory.IngestRequest) (memory.Ref, error) {
	return memory.Ref{ID: "s", Version: 1}, nil
}
func (f *fakeUnit) SaveCandidate(_ context.Context, c workspace.Candidate) error {
	f.candidates = append(f.candidates, c)
	return nil
}
func (f *fakeUnit) SetSampleState(context.Context, string, string) (bool, error) {
	return f.changed, nil
}
func (f *fakeUnit) RetryJob(context.Context, string) (bool, error) { return f.changed, nil }
func (f *fakeUnit) AuthorizeRecovery(context.Context, string, string) error {
	f.authorized = true
	return nil
}

const (
	taskID = "11111111-1111-4111-8111-111111111111"
	docID  = "22222222-2222-4222-8222-222222222222"
)

func apply(f *fakeUnit, c workspace.Command) error {
	return Apply(context.Background(), f, c, time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC))
}

func TestADelegationMakesATaskAndQueuesADraft(t *testing.T) {
	f := newFake()
	if err := apply(f, workspace.Command{Type: "delegateTask", ID: taskID, AgentID: "a", Prompt: "写邀请函"}); err != nil {
		t.Fatal(err)
	}
	if len(f.saved) != 1 || f.saved[0].ID != taskID || f.saved[0].Kind != "task" {
		t.Fatalf("%+v", f.saved)
	}
	if len(f.runs) != 1 || f.runs[0].Type != "requestRun" || f.runs[0].ThingID != taskID || f.runs[0].ID != "" || f.runs[0].Kind != "draft" {
		t.Fatalf("%+v", f.runs)
	}
}

func TestADelegationToAnAgentThatCannotWorkChangesNothing(t *testing.T) {
	for name, change := range map[string]func(*fakeUnit){
		"disabled":    func(f *fakeUnit) { f.agent.Enabled = false },
		"manual":      func(f *fakeUnit) { f.agent.Channel = "manual" },
		"unavailable": func(f *fakeUnit) { f.available = false },
	} {
		f := newFake()
		change(f)
		if err := apply(f, workspace.Command{Type: "delegateTask", ID: taskID, AgentID: "a", Prompt: "写"}); !errors.Is(err, memory.ErrUnavailable) || len(f.saved)+len(f.runs) != 0 {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

func TestAnItemIsMadeOnceUnderItsIdentity(t *testing.T) {
	f := newFake()
	add := workspace.Command{Type: "addTask", ID: taskID, Title: "订车"}
	if err := apply(f, add); err != nil {
		t.Fatal(err)
	}
	if err := apply(f, add); !errors.Is(err, memory.ErrConflict) || len(f.saved) != 1 {
		t.Fatal(err)
	}
}

func TestTheOwnerMakesADocumentAndAChangeKeepsItsBase(t *testing.T) {
	f := newFake()
	f.items[taskID] = workspace.Item{ID: taskID, Kind: "task"}
	if err := apply(f, workspace.Command{Type: "createDoc", Doc: &workspace.Doc{ID: docID, ThingID: taskID, Title: "提纲", By: "ai"}}); !errors.Is(err, memory.ErrInvalid) {
		t.Fatal("a document of the owner was made in another name", err)
	}
	if err := apply(f, workspace.Command{Type: "createDoc", Doc: &workspace.Doc{ID: docID, ThingID: taskID, Title: "提纲", By: "user", Version: 7}}); err != nil {
		t.Fatal(err)
	}
	if doc := f.savedDocs[0]; doc.Version != 0 || doc.CreatedAt == "" {
		t.Fatalf("%+v", doc)
	}
	f.docs[docID] = workspace.Doc{ID: docID, ThingID: taskID, Title: "提纲", Version: 3, By: "user"}
	if err := apply(f, workspace.Command{Type: "updateDoc", ID: docID, Patch: []byte(`{"body":"新正文"}`)}); err != nil {
		t.Fatal(err)
	}
	if doc := f.savedDocs[1]; *doc.BasedOn != 3 || doc.Body != "新正文" || doc.By != "owner" {
		t.Fatalf("%+v", doc)
	}
	if err := apply(f, workspace.Command{Type: "updateDoc", ID: docID, Patch: []byte(`{"runId":"x"}`)}); !errors.Is(err, memory.ErrInvalid) {
		t.Fatal("a change outside the title and the body was saved", err)
	}
}

func TestAnItemWithItemsUnderItOrARunningRunIsNotDeleted(t *testing.T) {
	for want, change := range map[error]func(*fakeUnit){
		memory.ErrConflict:       func(f *fakeUnit) { f.children = true },
		workspace.ErrWorkStarted: func(f *fakeUnit) { f.running = true },
	} {
		f := newFake()
		f.items[taskID] = workspace.Item{ID: taskID, Kind: "project"}
		change(f)
		if err := apply(f, workspace.Command{Type: "deleteThing", ID: taskID}); !errors.Is(err, want) || len(f.deleted) != 0 {
			t.Fatal(err)
		}
	}
	f := newFake()
	f.items[taskID] = workspace.Item{ID: taskID, Kind: "task"}
	if err := apply(f, workspace.Command{Type: "deleteThing", ID: taskID}); err != nil || len(f.deleted) != 1 {
		t.Fatal(err)
	}
}

func TestAQuickNoteBecomesAPendingCandidateOfItsSource(t *testing.T) {
	f := newFake()
	if err := apply(f, workspace.Command{Type: "capture", RequestID: "r", Text: "周五交房租"}); err != nil {
		t.Fatal(err)
	}
	if c := f.candidates[0]; c.State != "pending" || c.Source.SourceID != "s" || c.Source.Excerpt != "周五交房租" {
		t.Fatalf("%+v", c)
	}
	if err := apply(f, workspace.Command{Type: "capture", Text: " "}); !errors.Is(err, memory.ErrInvalid) {
		t.Fatal(err)
	}
}

func TestASampleAndAJobChangeOnlyFromAStateThatAllowsIt(t *testing.T) {
	f := newFake()
	if err := apply(f, workspace.Command{Type: "setSampleState", ID: taskID, State: "deleted"}); !errors.Is(err, memory.ErrInvalid) {
		t.Fatal(err)
	}
	if err := apply(f, workspace.Command{Type: "setSampleState", ID: taskID, State: "included"}); !errors.Is(err, memory.ErrConflict) {
		t.Fatal(err)
	}
	if err := apply(f, workspace.Command{Type: "retryJob", ID: taskID}); !errors.Is(err, memory.ErrConflict) || f.authorized {
		t.Fatal(err)
	}
	f.changed = true
	if err := apply(f, workspace.Command{Type: "retryJob", ID: taskID, RequestID: "r"}); err != nil || !f.authorized {
		t.Fatal(err)
	}
}
