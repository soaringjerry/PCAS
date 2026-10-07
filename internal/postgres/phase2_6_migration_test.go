package postgres

import (
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
)

func TestPhase26A4Migrates234FictitiousMarkersLosslessly(t *testing.T) {
	f := phase26LoadFixture(t)
	conn, err := f.Store.pool.Acquire(f.Context)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	schema := "phase2_6_legacy_" + strings.ReplaceAll(string(memory.NewID()), "-", "")
	q := pgx.Identifier{schema}.Sanitize()
	if _, err := conn.Exec(f.Context, "CREATE SCHEMA "+q); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(f.Context, "SET search_path TO "+q+",public"); err != nil {
		t.Fatal(err)
	}
	defer conn.Exec(f.Context, "SET search_path TO public")
	files, err := migrations.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if file.Name() >= "044" {
			break
		}
		body, err := migrations.ReadFile("migrations/" + file.Name())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Exec(f.Context, string(body)); err != nil {
			t.Fatalf("legacy schema migration %s: %v", file.Name(), err)
		}
	}
	err = pgx.BeginFunc(f.Context, conn, func(tx pgx.Tx) error {
		for _, table := range []string{"workspace_owners", "memory_records", "record_versions", "entities", "entity_versions"} {
			filter := "owner_id=$1"
			if table == "memory_records" {
				filter += " AND kind='entity'"
			}
			if table == "record_versions" {
				filter += " AND record_id IN(SELECT id FROM public.memory_records WHERE owner_id=$1 AND kind='entity')"
			}
			if _, err := tx.Exec(f.Context, fmt.Sprintf("INSERT INTO %s SELECT * FROM public.%s WHERE %s", table, table, filter), f.Scope.OwnerID); err != nil {
				return err
			}
		}
		for i := 0; i < 234; i++ {
			stage := fmt.Sprintf("memory.entity_candidates:1:seen:fictitious-%03d", i)
			if i < 117 {
				stage = fmt.Sprintf("memory.entity_result:1:%s:%s:fictitious-%03d", f.Entities[0], f.Entities[i+1], i)
			}
			if _, err := tx.Exec(f.Context, `INSERT INTO memory_jobs(id,owner_id,record_id,record_version,stage,state,attempts,error_code) VALUES($1,$2,$3,1,$4,'done',$5,'fictitious_preserved')`, memory.NewID(), f.Scope.OwnerID, f.Entities[0], stage, i%4); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var before string
	if err := conn.QueryRow(f.Context, `SELECT md5(string_agg(to_jsonb(j)::text,'|' ORDER BY stage)) FROM memory_jobs j WHERE owner_id=$1`, f.Scope.OwnerID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	body, err := migrations.ReadFile("migrations/044_background_receipts.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(f.Context, string(body)); err != nil {
		t.Fatal(err)
	}
	var count, remaining, receipts int
	var after string
	if err := conn.QueryRow(f.Context, `SELECT count(*),md5(string_agg(data::text,'|' ORDER BY stage)) FROM background_markers WHERE owner_id=$1`, f.Scope.OwnerID).Scan(&count, &after); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow(f.Context, `SELECT count(*) FROM memory_jobs WHERE owner_id=$1`, f.Scope.OwnerID).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow(f.Context, `SELECT count(*) FROM entity_alias_receipts WHERE owner_id=$1 AND same=false AND completed_at IS NOT NULL`, f.Scope.OwnerID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if count != 234 || remaining != 0 || receipts != 117 || after != before {
		t.Fatalf("lossless marker migration count=%d remaining=%d negatives=%d before=%s after=%s", count, remaining, receipts, before, after)
	}
	// Queue cleanup must neither delete negative receipts nor the lossless originals.
	if _, err := conn.Exec(f.Context, `DELETE FROM memory_jobs WHERE state='done'`); err != nil {
		t.Fatal(err)
	}
	var retained int
	if err := conn.QueryRow(f.Context, `SELECT count(*) FROM background_markers`).Scan(&retained); err != nil || retained != 234 {
		t.Fatalf("queue cleanup lost markers: %d %v", retained, err)
	}
	t.Log("234 exact synthetic original queue rows retained; 117 negative decisions adopted; done queue safely empty")
}
