package postgres

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/httpapi"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func TestWorkspaceDeliveryVersionsAndConditionalRead(t *testing.T) {
	s, scope, ctx := testStore(t), owner(), context.Background()
	b1Model(t, s, `{"items":[{"n":1,"category":"taste","durable":true,"deadlines":[]}]}`)
	taskState := workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "虚构便签整理", Text: "虚构便签整理"})
	taskState = workspaceCommand(t, s, scope, workspace.Command{Type: "addCheck", ID: taskState.Tasks[0].ID, Text: "虚构打勾步骤"})
	ref := organizeTestMemory(t, s, scope, "虚构人物偏爱蓝色便签")
	job := organizeTestJob(t, s, scope)
	before, err := s.Snapshot(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	api := httpapi.New(s, s, b1Auth{scope}, func(context.Context) error { return nil }, slog.New(slog.NewTextHandler(io.Discard, nil)), httpapi.Options{Workspace: s})
	read := func(tag string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, "/v1/workspace", nil)
		r.Header.Set("Authorization", "Bearer synthetic-b1-auth")
		if tag != "" {
			r.Header.Set("If-None-Match", tag)
		}
		w := httptest.NewRecorder()
		api.ServeHTTP(w, r)
		return w
	}
	first := read("")
	if first.Code != 200 || first.Header().Get("ETag") == "" {
		t.Fatal(first.Code, first.Body.String())
	}
	cached := read(first.Header().Get("ETag"))
	if cached.Code != 304 || cached.Body.Len() != 0 {
		t.Fatal("unchanged snapshot must have no body", cached.Code, cached.Body.String())
	}
	if err := s.ProcessOrganize(ctx, job); err != nil {
		t.Fatal(err)
	}
	after, err := s.Snapshot(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	if after.Revision != before.Revision || after.MemoryRevision <= before.MemoryRevision {
		t.Fatal("classification must update only library version", before.Revision, after.Revision, before.MemoryRevision, after.MemoryRevision)
	}
	changed := read(first.Header().Get("ETag"))
	if changed.Code != 200 {
		t.Fatal("changed content needs body", changed.Code)
	}
	// Scheduler/progress bookkeeping affects delivery, never command or memory version.
	if _, err := s.pool.Exec(ctx, "UPDATE claims SET organize_attempts=organize_attempts+1 WHERE owner_id=$1 AND id=$2", scope.OwnerID, ref.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, "UPDATE memory_jobs SET updated_at=clock_timestamp() WHERE id=$1", job.ID); err != nil {
		t.Fatal(err)
	}
	progress, err := s.Snapshot(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	if progress.MemoryRevision != after.MemoryRevision || progress.Revision != before.Revision {
		t.Fatal("bookkeeping changed operation/library version")
	}
	// A command submitted with the pre-classification version still succeeds.
	checked, err := s.Execute(ctx, scope, workspace.Command{Type: "toggleCheck", ID: taskState.Tasks[0].ID, ItemID: taskState.Tasks[0].Checklist[0].ID, RequestID: string(memory.NewID()), ExpectedRevision: before.Revision})
	if err != nil || !checked.Tasks[0].Checklist[0].Done {
		t.Fatal("background classification rejected user command", checked, err)
	}
	if response := read("W/" + changed.Header().Get("ETag")); response.Code != 200 || !strings.Contains(response.Body.String(), `"done":true`) {
		t.Fatal(response.Code, response.Body.String())
	}
}

func TestWorkspaceDeliveryJournalDoesNotBlockBehindCommandOwner(t *testing.T) {
	s, scope, ctx := testStore(t), owner(), context.Background()
	b1Model(t, s, `{"items":[]}`)
	workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "虚构队列锁验证"})
	organizeTestMemory(t, s, scope, "虚构队列锁验证记忆")
	job := organizeTestJob(t, s, scope)
	before, err := s.SnapshotETag(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	foreground, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer foreground.Rollback(ctx)
	if _, err := foreground.Exec(ctx, "SELECT 1 FROM workspace_owners WHERE owner_id=$1 FOR UPDATE", scope.OwnerID); err != nil {
		t.Fatal(err)
	}
	// Queue updates cannot take the owner lock after the job lock: a command
	// already holding the owner must remain free to cancel or inspect that job.
	bounded, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if _, err := s.pool.Exec(bounded, "UPDATE memory_jobs SET updated_at=clock_timestamp() WHERE id=$1", job.ID); err != nil {
		t.Fatal("background delivery waits on foreground owner", err)
	}
	if _, err := foreground.Exec(bounded, "SELECT 1 FROM memory_jobs WHERE id=$1 FOR UPDATE", job.ID); err != nil {
		t.Fatal(err)
	}
	if err := foreground.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	after, err := s.SnapshotETag(ctx, scope)
	if err != nil || after == before {
		t.Fatal("committed queue update must change delivery", before, after, err)
	}
	state, err := s.Snapshot(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	commandVersion, libraryVersion := state.Revision, state.MemoryRevision
	// A rolled-back event is invisible. A lower-ID event committed after a
	// higher-ID event must still invalidate delivery (MAX(id) cannot do that).
	early, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer early.Rollback(ctx)
	if _, err := early.Exec(ctx, "INSERT INTO workspace_snapshot_events(owner_id) VALUES($1)", scope.OwnerID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, "INSERT INTO workspace_snapshot_events(owner_id) VALUES($1)", scope.OwnerID); err != nil {
		t.Fatal(err)
	}
	higher, err := s.SnapshotETag(ctx, scope)
	if err != nil || higher == after {
		t.Fatal(higher, err)
	}
	if err := early.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	later, err := s.SnapshotETag(ctx, scope)
	if err != nil || later == higher {
		t.Fatal("late commit was hidden", later, err)
	}
	rolledBack, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rolledBack.Rollback(ctx)
	if _, err := rolledBack.Exec(ctx, "INSERT INTO workspace_snapshot_events(owner_id) VALUES($1)", scope.OwnerID); err != nil {
		t.Fatal(err)
	}
	if err := rolledBack.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	final, err := s.SnapshotETag(ctx, scope)
	if err != nil || final != later {
		t.Fatal("rollback changed delivery", final, later, err)
	}
	state, err = s.Snapshot(ctx, scope)
	if err != nil || state.Revision != commandVersion || state.MemoryRevision != libraryVersion {
		t.Fatal("delivery changed command or library version", state.Revision, state.MemoryRevision, err)
	}
}
