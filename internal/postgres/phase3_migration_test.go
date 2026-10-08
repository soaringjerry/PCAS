package postgres

import (
	"crypto/sha256"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
)

func TestPhase3D1LegacyDocumentMigratesToVersionOne(t *testing.T) {
	phase3Finding(t, "S-P3-002")
	s, ctx := phase26DisposableStore(t)
	// This store was JUST created by the owned disposable-container helper; it
	// cannot be an environment/production DSN. Build its pre-050 schema, then
	// migrate real legacy data through the production migration runner.
	if _, err := s.pool.Exec(ctx, "DROP SCHEMA public CASCADE"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, "CREATE SCHEMA public"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, "CREATE TABLE schema_migrations(name text PRIMARY KEY,checksum text NOT NULL,applied_at timestamptz NOT NULL DEFAULT now())"); err != nil {
		t.Fatal(err)
	}
	files, err := migrations.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if file.Name() >= "050" {
			continue
		}
		body, err := migrations.ReadFile("migrations/" + file.Name())
		if err != nil {
			t.Fatal(err)
		}
		if err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, string(body)); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, "INSERT INTO schema_migrations(name,checksum) VALUES($1,$2)", file.Name(), fmt.Sprintf("%x", sha256.Sum256(body)))
			return err
		}); err != nil {
			t.Fatal(file.Name(), err)
		}
	}
	scope := memory.Scope{OwnerID: memory.NewID(), PrincipalID: "fictitious-phase3-upgrade", IsOwner: true}
	pID, dID := string(memory.NewID()), string(memory.NewID())
	settings := map[string]any{"dailyBudget": 100, "autoAccept": false, "wakeIdeas": false, "followUps": false, "dailyReviewAt": "09:00", "timezone": "UTC"}
	if _, err := s.pool.Exec(ctx, `INSERT INTO workspace_owners(owner_id,settings) VALUES($1,$2)`, scope.OwnerID, asJSON(settings)); err != nil {
		t.Fatal(err)
	}
	item := map[string]any{"id": pID, "itemKind": "project", "recordVersion": 1, "name": "虚构迁移项目", "title": "虚构迁移项目", "status": "active", "createdAt": "2026-09-22T12:00:00Z", "updatedAt": "2026-10-06T12:00:00Z"}
	if _, err := s.pool.Exec(ctx, `INSERT INTO work_items(owner_id,id,kind,title,status,document,version,created_at,updated_at) VALUES($1,$2,'project','虚构迁移项目','active',$3,1,'2026-09-22T12:00:00Z','2026-10-06T12:00:00Z')`, scope.OwnerID, pID, asJSON(item)); err != nil {
		t.Fatal(err)
	}
	doc := map[string]any{"id": dID, "thingId": pID, "title": "虚构遗留方案", "body": "虚构遗留原文，不得在迁移时丢掉。", "by": "user", "createdAt": "2026-09-22T12:00:00Z", "updatedAt": "2026-10-06T12:00:00Z"}
	if _, err := s.pool.Exec(ctx, `INSERT INTO work_documents(owner_id,id,thing_id,document) VALUES($1,$2,$3,$4)`, scope.OwnerID, dID, pID, asJSON(doc)); err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	f := &phase3LoadedFixture{phase26LoadedFixture: &phase26LoadedFixture{Store: s, Context: ctx, Scope: scope}}
	h := phase3NewHTTP(t, f)
	var list phase3VersionList
	h.get(t, ctx, phase3DocumentPath(dID, "versions"), &list)
	if len(list.Items) != 1 || list.Items[0].Version != 1 || list.CurrentVersion != 1 {
		t.Fatal("legacy document not a single v1", list)
	}
	var version phase3Version
	h.get(t, ctx, phase3DocumentPath(dID, "versions/1"), &version)
	if version.Body != doc["body"] || version.Author != "user" || version.WrittenAt != doc["updatedAt"] {
		t.Fatal("migration lost original metadata/body", version)
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	h.get(t, ctx, phase3DocumentPath(dID, "versions"), &list)
	if len(list.Items) != 1 {
		t.Fatal("migration replay duplicated v1")
	}
}
