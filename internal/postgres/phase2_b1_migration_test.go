package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func b1EmptyStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("PCAS_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set PCAS_TEST_DATABASE_URL to run PostgreSQL integration tests")
	}
	admin, err := Open(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	if _, err := admin.pool.Exec(context.Background(), "CREATE EXTENSION IF NOT EXISTS vector WITH SCHEMA public"); err != nil {
		t.Fatal(err)
	}
	schema := "pcas_b1_" + strings.ReplaceAll(string(memory.NewID()), "-", "")
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.pool.Exec(context.Background(), "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.pool.Exec(context.Background(), "DROP SCHEMA "+quoted+" CASCADE"); err != nil {
			t.Error(err)
		}
	})
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema+",public")
	u.RawQuery = q.Encode()
	s, err := Open(context.Background(), u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}
func b1Leftovers(t *testing.T) (tables, rows []string, columns map[string][]string) {
	t.Helper()
	var shape struct {
		Tables  []string            `json:"tables"`
		Columns map[string][]string `json:"columns"`
		Rows    []string            `json:"migration_rows"`
	}
	if err := json.Unmarshal(b1Gold(t)["leftovers"], &shape); err != nil {
		t.Fatal(err)
	}
	return shape.Tables, shape.Rows, shape.Columns
}
func b1AssertClean(t *testing.T, s *Store) {
	t.Helper()
	tables, rows, columns := b1Leftovers(t)
	ctx := context.Background()
	for _, name := range tables {
		var count int
		if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM information_schema.tables WHERE table_schema=current_schema() AND table_name=$1", name).Scan(&count); err != nil || count != 0 {
			t.Errorf("legacy table %s remains count=%d err=%v", name, count, err)
		}
	}
	for table, cols := range columns {
		for _, col := range cols {
			var count int
			if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM information_schema.columns WHERE table_schema=current_schema() AND table_name=$1 AND column_name=$2", table, col).Scan(&count); err != nil || count != 0 {
				t.Errorf("legacy column %s.%s remains count=%d err=%v", table, col, count, err)
			}
		}
	}
	var n int
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM schema_migrations WHERE name=ANY($1::text[])", rows).Scan(&n); err != nil || n != 0 {
		t.Error("legacy migration rows remain", n, err)
	}
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM schema_migrations WHERE name='026_drop_phase2_0_leftovers.sql'").Scan(&n); err != nil || n != 1 {
		t.Error("cleanup migration not recorded", n, err)
	}
}

// Snapshot every non-legacy table, every row and every retained column. This
// includes jobs, claims, grants, receipts, actions and source versions, rather
// than comparing a single sentinel count and missing destructive side effects.
func b1DatabaseRows(t *testing.T, s *Store, ignoreLegacy bool) map[string]string {
	t.Helper()
	tables, _, columns := b1Leftovers(t)
	ctx := context.Background()
	rows, err := s.pool.Query(ctx, "SELECT table_name FROM information_schema.tables WHERE table_schema=current_schema() AND table_type='BASE TABLE' ORDER BY table_name")
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	out := map[string]string{}
	for _, name := range names {
		if name == "schema_migrations" || ignoreLegacy && oneOf(name, tables...) {
			continue
		}
		expr := "to_jsonb(t)"
		if ignoreLegacy {
			for _, col := range columns[name] {
				expr += "-'" + col + "'"
			}
		}
		sql := "SELECT coalesce(jsonb_agg(v ORDER BY v::text),'[]')::text FROM (SELECT " + expr + " AS v FROM " + pgx.Identifier{name}.Sanitize() + " t) data"
		var data string
		if err := s.pool.QueryRow(ctx, sql).Scan(&data); err != nil {
			t.Fatal(name, err)
		}
		out[name] = data
	}
	return out
}
func b1LegacyDatabase(t *testing.T) (*Store, map[string]string) {
	t.Helper()
	s := b1EmptyStore(t)
	ctx := context.Background()
	if _, err := s.pool.Exec(ctx, "CREATE TABLE schema_migrations(name text PRIMARY KEY,checksum text NOT NULL,applied_at timestamptz NOT NULL DEFAULT now())"); err != nil {
		t.Fatal(err)
	}
	files, err := migrations.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if file.Name() > "021_desk_turn_order.sql" {
			continue
		}
		body, err := migrations.ReadFile("migrations/" + file.Name())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.pool.Exec(ctx, string(body)); err != nil {
			t.Fatal(file.Name(), err)
		}
		if _, err := s.pool.Exec(ctx, "INSERT INTO schema_migrations(name,checksum) VALUES($1,$2)", file.Name(), fmt.Sprintf("%x", sha256.Sum256(body))); err != nil {
			t.Fatal(err)
		}
	}
	_, names, _ := b1Leftovers(t)
	for _, name := range names {
		body, err := os.ReadFile("../../testdata/phase2/leftover/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.pool.Exec(ctx, string(body)); err != nil {
			t.Fatal(name, err)
		}
		if _, err := s.pool.Exec(ctx, "INSERT INTO schema_migrations(name,checksum) VALUES($1,$2)", name, fmt.Sprintf("%x", sha256.Sum256(body))); err != nil {
			t.Fatal(err)
		}
	}
	scope := owner()
	f := b1Model(t, s, `{"reply":"迁移前秘书回执","actions":[{"op":"create_task","title":"迁移前事项"}]}`)
	b1Trip(t, s, scope)
	out := mustTurn(t, s, scope, turnRequest("迁移前安排"))
	src := b1Source(t, s, scope, "快速记录", "迁移前成都证据", "manual")
	b1Claim(t, s, scope, "迁移前成都陈述", "fact", "adopted", src)
	workspaceCommand(t, s, scope, workspace.Command{Type: "addProject", Name: "迁移前项目"})
	f.set("迁移前副手文档")
	run := b1Run(t, s, scope, "model", "迁移前成都陈述")
	if run.Adopted == nil {
		t.Fatal("deputy fixture not automatically adopted")
	}
	if len(out.State.Tasks) == 0 {
		t.Fatal("no sentinel business data")
	}
	// Populate a retired object as well as live business tables.
	if _, err := s.pool.Exec(ctx, "INSERT INTO source_scope_revisions(owner_id,source_id,revision) VALUES($1,$2,1)", string(scope.OwnerID), string(src.ID)); err != nil {
		t.Fatal(err)
	}
	return s, b1DatabaseRows(t, s, true)
}
func TestPhase2B1_G1_FreshDatabaseMigratesWithoutLegacyObjects(t *testing.T) {
	s := b1EmptyStore(t)
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	b1AssertClean(t, s)
	for _, name := range []string{"sources", "claims", "desk_turns", "action_log", "agent_runs", "work_items"} {
		var table *string
		if err := s.pool.QueryRow(context.Background(), "SELECT to_regclass($1)::text", name).Scan(&table); err != nil || table == nil {
			t.Error("business table absent", name, err)
		}
	}
}
func TestPhase2B1_G2_LegacyCleanupPreservesEveryBusinessRow(t *testing.T) {
	s, before := b1LegacyDatabase(t)
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	b1AssertClean(t, s)
	after := b1DatabaseRows(t, s, true)
	if !reflect.DeepEqual(before, after) {
		for table, data := range before {
			if after[table] != data {
				t.Errorf("cleanup changed business rows in %s", table)
			}
		}
		if len(before) != len(after) {
			t.Error("cleanup changed non-legacy table set")
		}
	}
}
func TestPhase2B1_G3_SecondStartupMakesNoFurtherChanges(t *testing.T) {
	s, _ := b1LegacyDatabase(t)
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	b1AssertClean(t, s)
	before := b1DatabaseRows(t, s, false)
	var migrationBefore, migrationAfter string
	sql := "SELECT jsonb_agg(to_jsonb(m) ORDER BY name)::text FROM schema_migrations m"
	if err := s.pool.QueryRow(context.Background(), sql).Scan(&migrationBefore); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(context.Background(), s.pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reopened.Close)
	if err := reopened.Migrate(context.Background()); err != nil {
		t.Fatal("second startup", err)
	}
	b1AssertClean(t, s)
	if !reflect.DeepEqual(before, b1DatabaseRows(t, s, false)) {
		t.Error("second startup changed business rows or tables")
	}
	if err := s.pool.QueryRow(context.Background(), sql).Scan(&migrationAfter); err != nil {
		t.Fatal(err)
	}
	if migrationAfter != migrationBefore {
		t.Error("second startup changed migration metadata")
	}
}
