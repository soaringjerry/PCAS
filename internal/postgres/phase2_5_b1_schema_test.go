package postgres_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/soaringjerry/PCAS/internal/memory"
)

func TestPhase25B1_Migration035Contract(t *testing.T) {
	f := phase25B1NewFixture(t)
	t.Run("column_shapes", func(t *testing.T) {
		for _, want := range []struct{ table, column, typ, nullable, def string }{
			{"claim_revisions", "category", "text", "NO", "'unknown'::text"},
			{"claim_revisions", "durable", "boolean", "YES", ""},
			{"claims", "organized", "integer", "NO", "0"},
			{"claims", "organize_attempts", "smallint", "NO", "0"},
		} {
			var typ, nullable, def string
			if err := f.db.QueryRow(f.ctx, `SELECT data_type,is_nullable,coalesce(column_default,'') FROM information_schema.columns WHERE table_schema='public' AND table_name=$1 AND column_name=$2`, want.table, want.column).Scan(&typ, &nullable, &def); err != nil {
				t.Fatal(err)
			}
			if typ != want.typ || nullable != want.nullable || def != want.def {
				t.Errorf("%s.%s = %s,%s,%s", want.table, want.column, typ, nullable, def)
			}
		}
	})
	ref := f.claim(t, "虚构测试迁移应当保留便签文字。")
	t.Run("category_values", func(t *testing.T) {
		for _, category := range []string{"identity", "taste", "rule", "goal", "progress", "event", "opinion", "other_person", "unknown"} {
			f.exec(t, `UPDATE claim_revisions SET category=$3 WHERE owner_id=$1 AND claim_id=$2`, f.scope.OwnerID, ref.ID, category)
		}
		_, err := f.db.Exec(f.ctx, `UPDATE claim_revisions SET category='unrecognized' WHERE owner_id=$1 AND claim_id=$2`, f.scope.OwnerID, ref.ID)
		phase25B1WantCheckViolation(t, err)
	})
	t.Run("all_old_and_new_mention_roles", func(t *testing.T) {
		for _, role := range []string{"person", "place", "organization", "thing", "project", "topic", "area"} {
			entity := f.entity(t, role, "虚构"+role)
			f.exec(t, `INSERT INTO claim_mentions(owner_id,claim_id,claim_version,entity_id,role) VALUES($1,$2,$3,$4,$5)`, f.scope.OwnerID, ref.ID, ref.Version, entity, role)
		}
		_, err := f.db.Exec(f.ctx, `UPDATE claim_mentions SET role='unrecognized' WHERE owner_id=$1 AND claim_id=$2`, f.scope.OwnerID, ref.ID)
		phase25B1WantCheckViolation(t, err)
	})
	t.Run("usage_preserves_vision_and_adds_organize", func(t *testing.T) {
		for _, purpose := range []string{"vision", "organize"} {
			f.exec(t, `INSERT INTO model_usage(owner_id,id,purpose,model,input_tokens,output_tokens,cost,memory_refs) VALUES($1,$2,$3,'fictitious-model',10,3,0.001,'[]')`, f.scope.OwnerID, memory.NewID(), purpose)
		}
		_, err := f.db.Exec(f.ctx, `INSERT INTO model_usage(owner_id,id,purpose,model,input_tokens,output_tokens,cost) VALUES($1,$2,'unrecognized','fictitious-model',10,3,0.001)`, f.scope.OwnerID, memory.NewID())
		phase25B1WantCheckViolation(t, err)
	})
	t.Run("required_indexes", func(t *testing.T) {
		for _, want := range []struct{ table, columns string }{{"claims", "(owner_id, organized)"}, {"claim_revisions", "(owner_id, category)"}} {
			rows, err := f.db.Query(f.ctx, `SELECT indexdef FROM pg_indexes WHERE schemaname='public' AND tablename=$1`, want.table)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for rows.Next() {
				var definition string
				if err := rows.Scan(&definition); err != nil {
					rows.Close()
					t.Fatal(err)
				}
				if strings.Contains(definition, want.columns) && !strings.Contains(definition, " WHERE ") {
					found = true
				}
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				t.Fatal(err)
			}
			if !found {
				t.Errorf("missing %s index on %s", want.table, want.columns)
			}
		}
	})
	t.Run("rerun_preserves_existing_data", func(t *testing.T) {
		f.exec(t, `UPDATE claim_revisions SET category='rule',durable=false WHERE owner_id=$1 AND claim_id=$2`, f.scope.OwnerID, ref.ID)
		f.exec(t, `UPDATE claims SET organized=1,organize_attempts=2 WHERE owner_id=$1 AND id=$2`, f.scope.OwnerID, ref.ID)
		if err := f.store.Migrate(f.ctx); err != nil {
			t.Fatal(err)
		}
		var category, text string
		var durable bool
		var organized, attempts, revisions int
		if err := f.db.QueryRow(f.ctx, `SELECT category,durable,value #>> '{}' FROM claim_revisions WHERE owner_id=$1 AND claim_id=$2 AND version=$3`, f.scope.OwnerID, ref.ID, ref.Version).Scan(&category, &durable, &text); err != nil {
			t.Fatal(err)
		}
		if err := f.db.QueryRow(f.ctx, `SELECT organized,organize_attempts FROM claims WHERE owner_id=$1 AND id=$2`, f.scope.OwnerID, ref.ID).Scan(&organized, &attempts); err != nil {
			t.Fatal(err)
		}
		if err := f.db.QueryRow(f.ctx, `SELECT count(*) FROM claim_revisions WHERE owner_id=$1 AND claim_id=$2`, f.scope.OwnerID, ref.ID).Scan(&revisions); err != nil {
			t.Fatal(err)
		}
		if category != "rule" || durable || organized != 1 || attempts != 2 || revisions != 1 || text != "虚构测试迁移应当保留便签文字。" {
			t.Errorf("migration altered fixture: category=%s durable=%v organized=%d attempts=%d revisions=%d text=%s", category, durable, organized, attempts, revisions, text)
		}
	})
}

func phase25B1WantCheckViolation(t *testing.T, err error) {
	t.Helper()
	var databaseErr *pgconn.PgError
	if !errors.As(err, &databaseErr) || databaseErr.Code != "23514" {
		t.Errorf("want CHECK violation, got %v", err)
	}
}
