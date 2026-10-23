package deputy

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// adoptStore is an in-memory AdoptUnit.
type adoptStore struct {
	agent    workspace.Agent
	item     workspace.Item
	cleared  bool
	revised  string
	docs     []workspace.Doc
	progress string
	parts    []string
	items    []workspace.Item
	saved    *workspace.Item
	samples  int
	uses     []memory.UseEvent
	useErr   error
}

func (s *adoptStore) Agent(context.Context, string) (workspace.Agent, error) { return s.agent, nil }
func (s *adoptStore) VerifyAccess(context.Context, workspace.Run) error      { return nil }
func (s *adoptStore) Item(context.Context, string) (workspace.Item, error)   { return s.item, nil }
func (s *adoptStore) ClearAdoption(context.Context, string) error            { s.cleared = true; return nil }
func (s *adoptStore) Revise(_ context.Context, _ workspace.Run, text string) error {
	s.revised = text
	return nil
}
func (s *adoptStore) SaveDoc(_ context.Context, doc workspace.Doc) error {
	s.docs = append(s.docs, doc)
	return nil
}
func (s *adoptStore) ProjectProgress(_ context.Context, _ *workspace.Run, _ workspace.Item, text, _ string, _ bool) error {
	s.progress = text
	return nil
}
func (s *adoptStore) Artifact(_ context.Context, _, _, kind, _, _ string) error {
	s.parts = append(s.parts, kind)
	return nil
}
func (s *adoptStore) SaveItem(_ context.Context, item workspace.Item) error {
	s.items = append(s.items, item)
	return nil
}
func (s *adoptStore) Actor(context.Context) string { return "owner" }
func (s *adoptStore) SaveAction(_ context.Context, item workspace.Item, _ string) error {
	s.saved = &item
	return nil
}
func (s *adoptStore) Sample(context.Context, workspace.Sample) error { s.samples++; return nil }
func (s *adoptStore) RecordUse(_ context.Context, event memory.UseEvent) error {
	s.uses = append(s.uses, event)
	return s.useErr
}

func finishedRun(kind string) *workspace.Run {
	return &workspace.Run{ID: "r", Kind: kind, Status: "done", Output: "初稿", AgentID: "a", ThingID: "t", ContextVersions: []memory.Ref{{ID: "m", Version: 1}}}
}

func adopt(s *adoptStore, run *workspace.Run, as, text string, auto bool) error {
	return Adopt(context.Background(), s, run, as, text, "act", auto, time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC))
}

func TestTheOwnersAdoptionRecordsTheUseOfTheRunsMemories(t *testing.T) {
	s := &adoptStore{agent: workspace.Agent{Enabled: true}, item: workspace.Item{ID: "t", Kind: "task", Version: 2}, useErr: memory.ErrConflict}
	run := finishedRun("draft")
	if err := adopt(s, run, "doc", "改过的初稿", false); err != nil {
		t.Fatal(err)
	}
	if !s.cleared || len(s.docs) != 1 || s.docs[0].Body != "改过的初稿" || s.saved.Version != 3 || s.samples != 1 || len(s.uses) != 1 {
		t.Fatalf("%+v", s)
	}
	if !run.Adopted.Edited || run.Adopted.Auto || run.Adopted.ActionID != "act" {
		t.Fatalf("%+v", *run.Adopted)
	}
}

func TestAnAutomaticAdoptionIsUneditedAndRecordsNoUse(t *testing.T) {
	s := &adoptStore{agent: workspace.Agent{Enabled: true}, item: workspace.Item{ID: "t", Kind: "task"}}
	run := finishedRun("summary")
	if err := adopt(s, run, "progress", run.Output, true); err != nil {
		t.Fatal(err)
	}
	if run.Adopted.Edited || !run.Adopted.Auto || len(s.uses) != 0 || s.saved.Notes != "\n初稿" || s.parts[0] != "notes" {
		t.Fatalf("%+v %+v", *run.Adopted, s)
	}
}

func TestSubtasksAreChecksOfATaskAndTasksOfAProject(t *testing.T) {
	task := &adoptStore{agent: workspace.Agent{Enabled: true}, item: workspace.Item{ID: "t", Kind: "task"}}
	if err := adopt(task, finishedRun("breakdown"), "subtasks", "- [ ] 一\n- [ ] 二", false); err != nil {
		t.Fatal(err)
	}
	if len(task.saved.Checklist) != 2 || len(task.items) != 0 {
		t.Fatalf("%+v", task.saved)
	}
	project := &adoptStore{agent: workspace.Agent{Enabled: true}, item: workspace.Item{ID: "p", Kind: "project"}}
	if err := adopt(project, finishedRun("breakdown"), "subtasks", "- [ ] 一\n- [ ] 二", false); err != nil {
		t.Fatal(err)
	}
	if len(project.items) != 2 || project.items[0].ProjectID != "p" || project.items[0].History[0].By != "owner" {
		t.Fatalf("%+v", project.items)
	}
}

func TestProgressOfAProjectIsRememberedAndARevisionIsAVersion(t *testing.T) {
	project := &adoptStore{agent: workspace.Agent{Enabled: true}, item: workspace.Item{ID: "p", Kind: "project"}}
	if err := adopt(project, finishedRun("summary"), "progress", "进展", false); err != nil || project.progress != "进展" {
		t.Fatal(err, project.progress)
	}
	revise := &adoptStore{agent: workspace.Agent{Enabled: true}, item: workspace.Item{ID: "t", Kind: "task"}}
	if err := adopt(revise, finishedRun("revise"), "doc", "新稿", false); err != nil || revise.revised != "新稿" || len(revise.docs) != 0 {
		t.Fatal(err, revise.revised, revise.docs)
	}
}

func TestADisabledAgentsResultIsNotAdopted(t *testing.T) {
	s := &adoptStore{item: workspace.Item{ID: "t", Kind: "task"}}
	run := finishedRun("draft")
	if err := adopt(s, run, "doc", "初稿", false); !errors.Is(err, memory.ErrForbidden) || s.cleared || run.Adopted != nil {
		t.Fatal(err)
	}
}

func TestAPastedRevisionIsRejectedWhenItSaysNothingNew(t *testing.T) {
	base := "这是一段足够长的基准正文，用来检查改写后的文字是否被截断，长度超过四十个字符，再多写几个字。"
	for text, want := range map[string]string{
		" ":        "改写正文为空",
		base:       "改写正文与基准完全相同",
		"当前稿":      "改写正文与当前版本完全相同",
		"短":        "改写正文明显短于基准，疑似截断；未存为新版本",
		base + "补": "",
	} {
		if got := RevisionRejection(text, &base, "当前稿"); got != want {
			t.Errorf("%q: %q", text, got)
		}
	}
	if RevisionRejection("新稿", nil, "") != "基准文档已删除或版本不存在" {
		t.Fatal("a revision without its base is saved")
	}
}

func TestTheLogSaysWhereAnAutomaticResultWent(t *testing.T) {
	run := workspace.Run{Output: "- [ ] 一\n- [ ] 二"}
	for as, want := range map[string]string{"subtasks": "副手结果：加了 2 个子任务", "doc": "副手结果：存成文档", "progress": "副手结果：写进进度"} {
		if got := AdoptionSummary(run, workspace.Item{Kind: "task"}, as); got != want {
			t.Errorf("%s: %s", as, got)
		}
	}
	if AdoptionSummary(run, workspace.Item{Kind: "project"}, "progress") != "副手结果：记住项目进展" {
		t.Fatal("project progress")
	}
}
