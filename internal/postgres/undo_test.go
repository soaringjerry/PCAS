package postgres

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestUndoCommands(t *testing.T) {
	s := testStore(t)
	scope := owner()
	ctx := context.Background()
	id := string(memory.NewID())
	request := string(memory.NewID())
	initial, err := s.Snapshot(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	st, err := s.Execute(ctx, scope, workspace.Command{Type: "addTask", ID: id, Title: "回邮件", RequestID: request, ExpectedRevision: initial.Revision})
	if err != nil {
		t.Fatal(err)
	}
	doneRequest := string(memory.NewID())
	st, err = s.Execute(ctx, scope, workspace.Command{Type: "setTaskStatus", ID: id, Status: "done", RequestID: doneRequest, ExpectedRevision: st.Revision})
	if err != nil {
		t.Fatal(err)
	}
	beforeVersion := st.Tasks[0].Version
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "undoAction", ID: doneRequest})
	if st.Tasks[0].Status != "todo" || st.Tasks[0].Version <= beforeVersion {
		t.Fatal("undo rolled back version or missed status", st.Tasks)
	}
	if _, err = s.Undo(ctx, scope, doneRequest); !errors.Is(err, workspace.ErrAlreadyUndone) {
		t.Fatal(err)
	}
	if st, err = s.Undo(ctx, scope, request); err != nil || len(st.Tasks) != 0 {
		t.Fatal("undo creation after undoing status", err, st.Tasks)
	}
	newRequest := string(memory.NewID())
	st, err = s.Execute(ctx, scope, workspace.Command{Type: "addIdea", Title: "工具", RequestID: newRequest, ExpectedRevision: st.Revision})
	if err != nil {
		t.Fatal(err)
	}
	st, err = s.Undo(ctx, scope, newRequest)
	if err != nil || len(st.Ideas) != 0 {
		t.Fatal(err, st.Ideas)
	}
	if _, err = s.Undo(ctx, owner(), doneRequest); !errors.Is(err, memory.ErrNotFound) {
		t.Fatal("cross-owner undo", err)
	}
}
func TestUndoDocumentsAndFirstBefore(t *testing.T) {
	s := testStore(t)
	scope := owner()
	ctx := context.Background()
	st := workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "文档"})
	thing := st.Tasks[0].ID
	docID := string(memory.NewID())
	workspaceCommand(t, s, scope, workspace.Command{Type: "createDoc", Doc: &workspace.Doc{ID: docID, ThingID: thing, Title: "旧", Body: "原文", By: "user"}})
	request := string(memory.NewID())
	st, _ = s.Snapshot(ctx, scope)
	st, err := s.Execute(ctx, scope, workspace.Command{Type: "deleteDoc", ID: docID, RequestID: request, ExpectedRevision: st.Revision})
	if err != nil || len(st.Docs) != 0 {
		t.Fatal(err)
	}
	st, err = s.Undo(ctx, scope, request)
	if err != nil || len(st.Docs) != 1 || st.Docs[0].Body != "原文" {
		t.Fatal(err, st.Docs)
	}
	action := string(memory.NewID())
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		logCtx := withActionLog(ctx, action, "desk", "", "两次更新")
		if err := beginActionLogTx(logCtx, tx); err != nil {
			return err
		}
		if err := s.commandTx(logCtx, tx, scope, workspace.Command{Type: "renameThing", ID: thing, Title: "第一次"}); err != nil {
			return err
		}
		if err := s.commandTx(logCtx, tx, scope, workspace.Command{Type: "renameThing", ID: thing, Title: "第二次"}); err != nil {
			return err
		}
		return flushActionLog(logCtx, tx, scope)
	})
	if err != nil {
		t.Fatal(err)
	}
	st, err = s.Undo(ctx, scope, action)
	if err != nil || st.Tasks[0].Title != "文档" {
		t.Fatal(err, st.Tasks)
	}
}
func TestUndoNewProjectDoesNotDetachLaterWork(t *testing.T) {
	s := testStore(t)
	scope := owner()
	ctx := context.Background()
	st, _ := s.Snapshot(ctx, scope)
	request := string(memory.NewID())
	st, err := s.Execute(ctx, scope, workspace.Command{Type: "addProject", Name: "项目", RequestID: request, ExpectedRevision: st.Revision})
	if err != nil {
		t.Fatal(err)
	}
	workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "后来归入", ProjectID: st.Projects[0].ID})
	if _, err = s.Undo(ctx, scope, request); !errors.Is(err, workspace.ErrChangedSince) {
		t.Fatal(err)
	}
}

func TestUndoQueuedDelegationAndStartedWork(t *testing.T) {
	s := testStore(t)
	scope := owner()
	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("undo must not call model") }))
	defer server.Close()
	s.SetModels(&ai.Registry{HTTP: server.Client(), Config: ai.Configuration{Providers: []ai.Provider{{ID: "model", Name: "副手", Protocol: "openai", BaseURL: server.URL, Model: "test", MaxOutput: 100, CostMode: "free"}}}})
	if _, err := s.Snapshot(ctx, scope); err != nil {
		t.Fatal(err)
	}
	for _, started := range []bool{false, true} {
		id := string(memory.NewID())
		action := string(memory.NewID())
		err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
			logged := withActionLog(ctx, action, "desk", "", "写方案")
			if err := beginActionLogTx(logged, tx); err != nil {
				return err
			}
			if err := s.commandTx(logged, tx, scope, workspace.Command{Type: "delegateTask", ID: id, Title: "写方案", Prompt: "写方案", AgentID: "model"}); err != nil {
				return err
			}
			return flushActionLog(logged, tx, scope)
		})
		if err != nil {
			t.Fatal(err)
		}
		if started {
			if _, err = s.pool.Exec(ctx, "UPDATE agent_runs SET status='running' WHERE owner_id=$1 AND thing_id=$2", string(scope.OwnerID), id); err != nil {
				t.Fatal(err)
			}
		}
		st, err := s.Undo(ctx, scope, action)
		if started {
			if !errors.Is(err, workspace.ErrWorkStarted) {
				t.Fatal(err)
			}
			continue
		}
		if err != nil || len(st.Tasks) != 0 || len(st.Runs) != 0 || st.BudgetUsage != 0 {
			t.Fatal(err, st.Tasks, st.Runs, st.BudgetUsage)
		}
		var orphan int
		if err = s.pool.QueryRow(ctx, "SELECT count(*) FROM run_dependencies WHERE owner_id=$1", string(scope.OwnerID)).Scan(&orphan); err != nil || orphan != 0 {
			t.Fatal(err, orphan)
		}
	}
}

func TestDueReminderTracksCommandDue(t *testing.T) {
	s := testStore(t)
	scope := owner()
	ctx := context.Background()
	st := workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "回邮件"})
	id := st.Tasks[0].ID
	loc, _ := time.LoadLocation("Asia/Shanghai")
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		item, err := getItem(ctx, tx, scope, id)
		if err != nil {
			return err
		}
		item.Due = "2026-10-02T07:00:00Z"
		applyDueReminder(&item, "-30m", loc)
		return saveItem(ctx, tx, scope, item)
	})
	if err != nil {
		t.Fatal(err)
	}
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "updateTask", ID: id, Patch: asJSON(map[string]string{"due": "2026-10-05T02:00:00Z"})})
	if len(st.Tasks[0].Triggers) != 1 || st.Tasks[0].Triggers[0].NextAt != "2026-10-05T01:30:00Z" {
		t.Fatal(st.Tasks[0].Triggers)
	}
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "updateTask", ID: id, Patch: asJSON(map[string]string{"due": ""})})
	if len(st.Tasks[0].Triggers) != 0 {
		t.Fatal(st.Tasks[0].Triggers)
	}
}
func TestDueReminderOffsets(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Shanghai")
	for _, tc := range []struct{ offset, next string }{{"-2h", "2026-10-02T05:00:00Z"}, {"at", "2026-10-02T07:00:00Z"}, {"09:00", "2026-10-02T01:00:00Z"}} {
		item := workspace.Item{Title: "邮件", Due: "2026-10-02T07:00:00Z", Triggers: []workspace.Trigger{{ID: "other"}}}
		applyDueReminder(&item, tc.offset, loc)
		if len(item.Triggers) != 2 || item.Triggers[1].NextAt != tc.next || item.Triggers[1].Offset != tc.offset {
			t.Fatal(item.Triggers)
		}
		applyDueReminder(&item, "", loc)
		if item.Triggers[1].Offset != tc.offset {
			t.Fatal("lost offset")
		}
		applyDueReminder(&item, "none", loc)
		if len(item.Triggers) != 1 || item.Triggers[0].ID != "other" {
			t.Fatal(item.Triggers)
		}
	}
}

func TestDeletionExpiresItemAndDocumentSnapshots(t *testing.T) {
	s := testStore(t)
	phase2ManualDestination(t, s)
	scope := owner()
	ctx := context.Background()
	st := workspaceCommand(t, s, scope, workspace.Command{Type: "capture", Text: "要删除的私密资料"})
	candidate := st.Candidates[0]
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "acceptCandidate", ID: candidate.ID, Kind: "memory", MemoryKind: "fact", Text: "要删除的私密资料"})
	mem := st.Memories[0]
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "普通工作"})
	itemID := st.Tasks[0].ID
	// Retain a source preview on the item, as an accepted task normally does.
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		item, err := getItem(ctx, tx, scope, itemID)
		if err != nil {
			return err
		}
		item.Sources = append(item.Sources, candidate.Source)
		return saveItem(ctx, tx, scope, item)
	})
	if err != nil {
		t.Fatal(err)
	}
	st, _ = s.Snapshot(ctx, scope)
	itemAction := string(memory.NewID())
	st, err = s.Execute(ctx, scope, workspace.Command{Type: "setNotes", ID: itemID, Text: "自己写的说明", RequestID: itemAction, ExpectedRevision: st.Revision})
	if err != nil {
		t.Fatal(err)
	}
	// A run-backed document is purged when its input memory is erased. Creating
	// this fixture directly avoids invoking model generation or adoption samples.
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "requestRun", ThingID: itemID, AgentID: "manual", ManualRecipient: &memory.Recipient{Provider: phase2ManualProvider}, Kind: "draft", Prompt: "整理要删除的私密资料"})
	run := st.Runs[0]
	phase2RTGetPackage(t, s, scope, run)
	if !hasArtifactDependency(run.ContextVersions, mem.ID) {
		t.Fatal("run fixture has no dependency", run)
	}
	docID, docAction := string(memory.NewID()), string(memory.NewID())
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		logged := withActionLog(ctx, docAction, "worker", "", "文档审计摘要")
		if err := beginActionLogTx(logged, tx); err != nil {
			return err
		}
		if err := saveDoc(logged, tx, scope, workspace.Doc{ID: docID, ThingID: itemID, RunID: run.ID, Title: "生成的文档", Body: "要删除的私密资料", By: "ai", CreatedAt: stamp(), UpdatedAt: stamp()}); err != nil {
			return err
		}
		return flushActionLog(logged, tx, scope)
	})
	if err != nil {
		t.Fatal(err)
	}
	st, _ = s.Snapshot(ctx, scope)
	docEdit := string(memory.NewID())
	if _, err = s.Execute(ctx, scope, workspace.Command{Type: "updateDoc", ID: docID, Patch: asJSON(map[string]string{"title": "文档新标题"}), RequestID: docEdit, ExpectedRevision: st.Revision}); err != nil {
		t.Fatal(err)
	}
	untouched := string(memory.NewID())
	st, _ = s.Snapshot(ctx, scope)
	if _, err = s.Execute(ctx, scope, workspace.Command{Type: "addTask", Title: "不相关的工作", RequestID: untouched, ExpectedRevision: st.Revision}); err != nil {
		t.Fatal(err)
	}
	workspaceCommand(t, s, scope, workspace.Command{Type: "deleteMemory", ID: mem.ID, IncludeSources: true})
	for _, id := range []string{itemAction, docAction, docEdit} {
		var changes, summary string
		var expired bool
		if err = s.pool.QueryRow(ctx, "SELECT changes::text,summary,expired_at IS NOT NULL FROM action_log WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), id).Scan(&changes, &summary, &expired); err != nil || changes != "[]" || !expired || summary == "" {
			t.Fatal("snapshot was retained", err, id, changes, summary, expired)
		}
		if _, err = s.Undo(ctx, scope, id); !errors.Is(err, workspace.ErrExpired) {
			t.Fatal("expired action was undoable", id, err)
		}
	}
	st, err = s.Snapshot(ctx, scope)
	if err != nil || len(st.Docs) != 0 || len(st.Memories) != 0 {
		t.Fatal(err, st.Docs, st.Memories)
	}
	for _, item := range st.Tasks {
		if item.ID == itemID {
			for _, source := range item.Sources {
				if source.SourceID == candidate.Source.SourceID {
					t.Fatal("source preview was retained", item)
				}
			}
		}
	}
	var expired bool
	if err = s.pool.QueryRow(ctx, "SELECT expired_at IS NOT NULL FROM action_log WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), untouched).Scan(&expired); err != nil || expired {
		t.Fatal("expired an unrelated action", err)
	}
	if _, err = s.Undo(ctx, scope, untouched); err != nil {
		t.Fatal("unrelated action cannot be undone", err)
	}
}

func TestActionSnapshotRetentionKeepsAuditMetadata(t *testing.T) {
	s := testStore(t)
	scope := owner()
	other := owner()
	ctx := context.Background()
	st, _ := s.Snapshot(ctx, scope)
	created := string(memory.NewID())
	itemID := string(memory.NewID())
	st, err := s.Execute(ctx, scope, workspace.Command{Type: "addTask", ID: itemID, Title: "旧名称", RequestID: created, ExpectedRevision: st.Revision})
	if err != nil {
		t.Fatal(err)
	}
	edited := string(memory.NewID())
	st, err = s.Execute(ctx, scope, workspace.Command{Type: "renameThing", ID: itemID, Title: "新名称", RequestID: edited, ExpectedRevision: st.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Undo(ctx, scope, edited); err != nil {
		t.Fatal(err)
	}
	if _, err = s.pool.Exec(ctx, "UPDATE action_log SET created_at=now()-interval '31 days' WHERE owner_id=$1 AND id=ANY($2::uuid[])", string(scope.OwnerID), []string{created, edited}); err != nil {
		t.Fatal(err)
	}
	audit := map[string]string{}
	for _, id := range []string{created, edited} {
		var metadata string
		if err = s.pool.QueryRow(ctx, "SELECT (to_jsonb(l)-'changes'-'expired_at')::text FROM action_log l WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), id).Scan(&metadata); err != nil {
			t.Fatal(err)
		}
		audit[id] = metadata
	}
	otherState, _ := s.Snapshot(ctx, other)
	otherID := string(memory.NewID())
	if _, err = s.Execute(ctx, other, workspace.Command{Type: "addTask", Title: "别人的旧动作", RequestID: otherID, ExpectedRevision: otherState.Revision}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.pool.Exec(ctx, "UPDATE action_log SET created_at=now()-interval '31 days' WHERE owner_id=$1 AND id=$2", string(other.OwnerID), otherID); err != nil {
		t.Fatal(err)
	}
	recent := string(memory.NewID())
	st, _ = s.Snapshot(ctx, scope)
	if _, err = s.Execute(ctx, scope, workspace.Command{Type: "addIdea", Title: "新的动作", RequestID: recent, ExpectedRevision: st.Revision}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{created, edited} {
		var metadata, changes string
		var expired bool
		if err = s.pool.QueryRow(ctx, "SELECT (to_jsonb(l)-'changes'-'expired_at')::text,changes::text,expired_at IS NOT NULL FROM action_log l WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), id).Scan(&metadata, &changes, &expired); err != nil || metadata != audit[id] || changes != "[]" || !expired {
			t.Fatal("lost audit metadata or kept snapshot", id, err, metadata, changes, expired)
		}
		want := workspace.ErrExpired
		if id == edited {
			want = workspace.ErrAlreadyUndone
		}
		if _, err = s.Undo(ctx, scope, id); !errors.Is(err, want) {
			t.Fatal(err)
		}
	}
	var undone bool
	if err = s.pool.QueryRow(ctx, "SELECT undone_at IS NOT NULL FROM action_log WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), edited).Scan(&undone); err != nil || !undone {
		t.Fatal("lost undone_at", err)
	}
	for _, entry := range []struct {
		scope memory.Scope
		id    string
	}{{scope, recent}, {other, otherID}} {
		var count int
		var expired bool
		if err = s.pool.QueryRow(ctx, "SELECT jsonb_array_length(changes),expired_at IS NOT NULL FROM action_log WHERE owner_id=$1 AND id=$2", string(entry.scope.OwnerID), entry.id).Scan(&count, &expired); err != nil || count == 0 || expired {
			t.Fatal("expired recent/other-owner action", err, entry.id)
		}
	}
	// A no-op collector flush still enforces the window without creating a log.
	if _, err = s.pool.Exec(ctx, "UPDATE action_log SET created_at=now()-interval '31 days' WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), recent); err != nil {
		t.Fatal(err)
	}
	noOp := string(memory.NewID())
	if err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		logged := withActionLog(ctx, noOp, "desk", "", "无变更")
		if err := beginActionLogTx(logged, tx); err != nil {
			return err
		}
		return flushActionLog(logged, tx, scope)
	}); err != nil {
		t.Fatal(err)
	}
	var cleaned bool
	if err = s.pool.QueryRow(ctx, "SELECT changes='[]'::jsonb AND expired_at IS NOT NULL FROM action_log WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), recent).Scan(&cleaned); err != nil || !cleaned {
		t.Fatal(err, cleaned)
	}
	var logged bool
	if err = s.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM action_log WHERE owner_id=$1 AND id=$2)", string(scope.OwnerID), noOp).Scan(&logged); err != nil || logged {
		t.Fatal("no-op flush created an action", err)
	}
}
