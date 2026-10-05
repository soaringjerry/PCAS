package postgres_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/soaringjerry/PCAS/internal/httpapi"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/postgres"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// These fixtures use only fictitious data and a container owned by this test.
// Never accept an external DSN: acceptance must not touch a shared database.
type phase25B234Fixture struct {
	ctx       context.Context
	db        *pgxpool.Pool
	store     *postgres.Store
	scope     memory.Scope
	handler   http.Handler
	statusNow time.Time // Test-only clock for the real status queue's due jobs.
}

func (f *phase25B234Fixture) retire(t *testing.T, ref, kept memory.Ref, reason string) {
	t.Helper()
	// Frozen-schema precondition for reads only; never a substitute for running compare.
	f.exec(t, `UPDATE claims SET retired=$3,retired_by=$4,retired_at=now() WHERE owner_id=$1 AND id=$2`, f.scope.OwnerID, ref.ID, reason, kept.ID)
}

func (f *phase25B234Fixture) correct(t *testing.T, ref memory.Ref, text string) memory.Ref {
	t.Helper()
	var subject memory.ID
	if err := f.db.QueryRow(f.ctx, `SELECT subject_id FROM claim_revisions WHERE owner_id=$1 AND claim_id=$2 AND version=$3`, f.scope.OwnerID, ref.ID, ref.Version).Scan(&subject); err != nil {
		t.Fatal(err)
	}
	value, err := json.Marshal(text)
	if err != nil {
		t.Fatal(err)
	}
	out, err := f.store.Correct(f.ctx, f.scope, memory.CorrectRequest{Target: ref, Reason: "虚构验收纠正", Replacement: memory.Claim{
		Revision: memory.Revision{Ref: ref, State: "active"}, SubjectID: subject, Predicate: "acceptance_note", Value: value, Nature: "fact", Acquisition: "direct", Confirmation: "unknown",
	}})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func (f *phase25B234Fixture) assertRevisions(t *testing.T, refs ...memory.Ref) {
	t.Helper()
	for _, ref := range refs {
		var n, version int
		if err := f.db.QueryRow(f.ctx, `SELECT r.version,(SELECT count(*) FROM claim_revisions c WHERE c.owner_id=r.owner_id AND c.claim_id=r.id) FROM memory_records r WHERE r.owner_id=$1 AND r.id=$2`, f.scope.OwnerID, ref.ID).Scan(&version, &n); err != nil {
			t.Fatal(err)
		}
		if version != ref.Version || n != ref.Version {
			t.Errorf("memory %s has version=%d revisions=%d, want %d", ref.ID, version, n, ref.Version)
		}
	}
}

func phase25B234AssertIDs(t *testing.T, memories []workspace.Memory, want ...memory.Ref) {
	t.Helper()
	counts := make(map[string]int)
	for _, m := range memories {
		counts[m.ID]++
		if m.Version < 1 {
			t.Error("memory has no current version")
		}
	}
	if len(memories) != len(want) {
		t.Errorf("memory count=%d, want %d; got=%+v", len(memories), len(want), memories)
	}
	for _, ref := range want {
		if counts[string(ref.ID)] != 1 {
			t.Errorf("memory %s occurs %d times, want once", ref.ID, counts[string(ref.ID)])
		}
	}
}

func phase25B234NewFixture(t *testing.T) *phase25B234Fixture {
	return phase25B234NewFixtureTimeout(t, 90*time.Second)
}

func phase25B234NewFixtureTimeout(t *testing.T, timeout time.Duration) *phase25B234Fixture {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	t.Cleanup(cancel)
	name := "pcas-p25-t234-" + string(memory.NewID())
	run := func(args ...string) string {
		t.Helper()
		out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("docker %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run("run", "--detach", "--rm", "--name", name,
		"--label", "pcas.acceptance=phase2_5-b234-T234",
		"--tmpfs", "/var/lib/postgresql/data:rw,size=512m",
		"--env", "POSTGRES_PASSWORD=fictitious-test-password",
		"--publish", "127.0.0.1::5432", "pgvector/pgvector:0.8.2-pg16")
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		if out, err := exec.CommandContext(cleanupCtx, "docker", "rm", "--force", "--volumes", name).CombinedOutput(); err != nil {
			t.Errorf("remove owned container %s: %v: %s", name, err, out)
		}
	})
	address := run("port", name, "5432/tcp")
	dsn := fmt.Sprintf("postgres://postgres:fictitious-test-password@%s/postgres?sslmode=disable", address)
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	readyCtx, readyCancel := context.WithTimeout(ctx, 20*time.Second)
	defer readyCancel()
	for {
		if err := db.Ping(readyCtx); err == nil {
			break
		}
		select {
		case <-readyCtx.Done():
			t.Fatalf("owned test database readiness: %v", readyCtx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
	store, err := postgres.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	f := &phase25B234Fixture{ctx: ctx, db: db, store: store,
		scope: memory.Scope{OwnerID: memory.NewID(), PrincipalID: "fictitious-owner", IsOwner: true}}
	f.handler = httpapi.New(store, store, httpapi.NewOwnerToken("fictitious-test-token", f.scope.OwnerID),
		store.Ping, slog.New(slog.NewTextHandler(io.Discard, nil)),
		httpapi.Options{Workspace: store, Editor: store, Writer: store})
	return f
}

func (f *phase25B234Fixture) get(t *testing.T, path string, target any) {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, path, nil).WithContext(f.ctx)
	request.Header.Set("Authorization", "Bearer fictitious-test-token")
	response := httptest.NewRecorder()
	f.handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("GET %s: status %d: %s", path, response.Code, response.Body.String())
	}
	if err := json.Unmarshal(response.Body.Bytes(), target); err != nil {
		t.Fatal(err)
	}
}

func (f *phase25B234Fixture) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := f.db.Exec(f.ctx, sql, args...); err != nil {
		t.Fatal(err)
	}
}

func (f *phase25B234Fixture) entity(t *testing.T, typ, name string) memory.ID {
	t.Helper()
	id := memory.NewID()
	_, err := f.store.Commit(f.ctx, f.scope, memory.CommitRequest{
		RequestID: memory.NewID(), Entities: []memory.Entity{{
			Revision: memory.Revision{Ref: memory.Ref{ID: id, Version: 1, Kind: memory.EntityKind}, State: "active"},
			Type:     typ, Name: name, Aliases: []string{name},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func (f *phase25B234Fixture) claim(t *testing.T, text string) memory.Ref {
	t.Helper()
	return f.claimWith(t, text, "direct", "unknown", nil)
}

func (f *phase25B234Fixture) claimWith(t *testing.T, text, acquisition, confirmation string, locator map[string]json.RawMessage) memory.Ref {
	t.Helper()
	source, err := f.store.Ingest(f.ctx, f.scope, memory.IngestRequest{
		Connector: "acceptance", ExternalID: string(memory.NewID()), Title: "虚构验收便签", Text: text,
	})
	if err != nil {
		t.Fatal(err)
	}
	subject := f.entity(t, "person", "虚构人物陆青")
	ref := memory.Ref{ID: memory.NewID(), Version: 1, Kind: memory.ClaimKind}
	evidenceID := memory.NewID()
	value, err := json.Marshal(text)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.store.Commit(f.ctx, f.scope, memory.CommitRequest{
		RequestID: memory.NewID(),
		Claims: []memory.Claim{{
			Revision: memory.Revision{Ref: ref, State: "active"}, SubjectID: subject,
			Predicate: "acceptance_note", Value: value, Nature: "fact", Acquisition: acquisition,
			Confirmation: confirmation, Evidence: []memory.ID{evidenceID},
		}},
		Evidence: []memory.Evidence{{ID: evidenceID, Source: source.Ref, Target: ref,
			Acquisition: acquisition, Stance: "supports", Locator: locator}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return ref
}

// Direct SQL here creates the precondition for read/delete acceptance only.
// Organizing acceptance must call the real background entrypoint instead.
func (f *phase25B234Fixture) labels(t *testing.T, ref memory.Ref, category string, durable bool, organized int, groups ...workspace.MemoryGroup) {
	t.Helper()
	f.exec(t, `UPDATE claim_revisions SET category=$4,durable=$5 WHERE owner_id=$1 AND claim_id=$2 AND version=$3`, f.scope.OwnerID, ref.ID, ref.Version, category, durable)
	f.exec(t, `UPDATE claims SET organized=$3 WHERE owner_id=$1 AND id=$2`, f.scope.OwnerID, ref.ID, organized)
	for _, group := range groups {
		f.exec(t, `INSERT INTO claim_mentions(owner_id,claim_id,claim_version,entity_id,role) VALUES ($1,$2,$3,$4,$5)`,
			f.scope.OwnerID, ref.ID, ref.Version, group.EntityID, group.Type)
	}
}
