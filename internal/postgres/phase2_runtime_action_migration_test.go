package postgres

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

const phase2RTActionMigrationName = "025_secretary_action_lineage.sql"

// Frozen DDL from root-approved dd1ffc4, not a checksum recalculated from the
// current product to manufacture an expected value.
const phase2RTActionMigrationChecksum = "ce73cfd82375ee0af81a50736aa07e2f043757d8c78daeb33dfb7650e3e8a1d1"

func phase2RTLegacy024Store(t *testing.T) *Store {
	t.Helper()
	s := phase2RTLegacy022Store(t)
	phase2RTApplyLegacy023(t, s)
	name := "024_context_execution_recovery.sql"
	body, err := migrations.ReadFile("migrations/" + name)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, string(body)); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, "INSERT INTO schema_migrations(name,checksum) VALUES($1,$2)", name, fmt.Sprintf("%x", sha256.Sum256(body)))
		return err
	}); err != nil {
		t.Fatal("actual legacy 024 schema fixture", err)
	}
	var total, originColumns int
	if err := s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM schema_migrations),(SELECT count(*) FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='action_log' AND column_name IN ('context_task','context_stale'))`).Scan(&total, &originColumns); err != nil || total != 24 || originColumns != 0 {
		t.Fatal("old fixture is not real 024 without origin columns", err, total, originColumns)
	}
	return s
}

// SQL fixtures operate on the real 024 document triggers and capture their
// changes/afterHash. They never call today's Execute against an old schema or
// invent a historical recipient. Public Store.Undo is tested after migration.
func phase2RTLegacyAction(t *testing.T, s *Store, scope memory.Scope, item workspace.Item, source string, update bool) string {
	t.Helper()
	ctx := context.Background()
	id := string(memory.NewID())
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT set_config('pcas.action_changes','[]',true)"); err != nil {
			return err
		}
		if update {
			if _, err := tx.Exec(ctx, "UPDATE work_items SET title=$3,version=$4,document=$5,updated_at=$6 WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), item.ID, item.Title, item.Version, asJSON(item), item.UpdatedAt); err != nil {
				return err
			}
		} else {
			if _, err := tx.Exec(ctx, "INSERT INTO work_items(owner_id,id,kind,title,status,version,document,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)", string(scope.OwnerID), item.ID, item.Kind, item.Title, item.Status, item.Version, asJSON(item), item.CreatedAt, item.UpdatedAt); err != nil {
				return err
			}
		}
		_, err := tx.Exec(ctx, "INSERT INTO action_log(owner_id,id,source,summary,changes) VALUES($1,$2,$3,$4,current_setting('pcas.action_changes')::jsonb)", string(scope.OwnerID), id, source, "synthetic legacy "+item.Title)
		return err
	})
	if err != nil {
		t.Fatal("real old document/action trigger fixture", err)
	}
	var changes int
	if err := s.pool.QueryRow(ctx, "SELECT jsonb_array_length(changes) FROM action_log WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), id).Scan(&changes); err != nil || changes != 1 {
		t.Fatal("old trigger did not collect actual work item change", err, changes)
	}
	return id
}

func TestPhase2Runtime025PreservesLegacyActionsAndUndoMeaning(t *testing.T) {
	s := phase2RTLegacy024Store(t)
	ctx, scope := context.Background(), owner()
	if _, err := s.pool.Exec(ctx, "INSERT INTO workspace_owners(owner_id,settings) VALUES($1,'{}')", string(scope.OwnerID)); err != nil {
		t.Fatal(err)
	}
	live := newItem("task", "LEGACY-LIVE-417")
	liveAction := phase2RTLegacyAction(t, s, scope, live, "command", false)
	undone := newItem("task", "LEGACY-UNDONE-418")
	undoneAction := phase2RTLegacyAction(t, s, scope, undone, "desk", false)
	// Explicit old-state SQL restores the old insertion and marks the receipt
	// undone. This is a migration fixture, not a claim of old binary execution.
	if _, err := s.pool.Exec(ctx, "DELETE FROM work_items WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), undone.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, "UPDATE action_log SET undone_at=now() WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), undoneAction); err != nil {
		t.Fatal(err)
	}
	expired := newItem("task", "LEGACY-EXPIRED-419")
	expiredAction := phase2RTLegacyAction(t, s, scope, expired, "worker", false)
	if _, err := s.pool.Exec(ctx, "UPDATE action_log SET created_at=now()-interval '31 days',expired_at=now(),changes='[]' WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), expiredAction); err != nil {
		t.Fatal(err)
	}
	blocked := newItem("task", "LEGACY-BLOCKED-420")
	blockedAction := phase2RTLegacyAction(t, s, scope, blocked, "command", false)
	blocked.Title, blocked.Name, blocked.Version, blocked.UpdatedAt = "LEGACY-LATER-421", "LEGACY-LATER-421", blocked.Version+1, stamp()
	laterAction := phase2RTLegacyAction(t, s, scope, blocked, "command", true)
	actions := func(upgraded bool) string {
		expression := "to_jsonb(a)"
		if upgraded {
			expression += "-'context_task'-'context_stale'"
		}
		var text string
		if err := s.pool.QueryRow(ctx, "SELECT jsonb_agg("+expression+" ORDER BY action_order,id)::text FROM action_log a WHERE owner_id=$1", string(scope.OwnerID)).Scan(&text); err != nil {
			t.Fatal(err)
		}
		return text
	}
	items := func() string {
		var text string
		if err := s.pool.QueryRow(ctx, "SELECT jsonb_agg(to_jsonb(i) ORDER BY id)::text FROM work_items i WHERE owner_id=$1", string(scope.OwnerID)).Scan(&text); err != nil {
			t.Fatal(err)
		}
		return text
	}
	beforeActions, beforeItems := actions(false), items()
	if err := s.Migrate(ctx); err != nil {
		t.Fatal("actual 024->025 upgrade", err)
	}
	if actions(true) != beforeActions || items() != beforeItems {
		t.Fatal("025 changed historical IDs/source/body/hash/time/order or original work")
	}
	var applied int
	var checksum string
	if err := s.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM schema_migrations),(SELECT checksum FROM schema_migrations WHERE name=$1)", phase2RTActionMigrationName).Scan(&applied, &checksum); err != nil || applied != 25 || checksum != phase2RTActionMigrationChecksum {
		t.Fatal("024 upgrade did not apply exactly the frozen025 migration", err, applied, checksum)
	}
	var count, invented int
	if err := s.pool.QueryRow(ctx, "SELECT count(*),count(*) FILTER(WHERE context_task IS NOT NULL OR context_stale) FROM action_log WHERE owner_id=$1", string(scope.OwnerID)).Scan(&count, &invented); err != nil || count != 5 || invented != 0 {
		t.Fatal("migration fabricated historical trusted task or stale state", err, count, invented)
	}
	var ledger string
	if err := s.pool.QueryRow(ctx, "SELECT jsonb_agg(to_jsonb(m) ORDER BY name)::text FROM schema_migrations m").Scan(&ledger); err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatal("repeat upgraded startup", err)
	}
	var afterLedger string
	if err := s.pool.QueryRow(ctx, "SELECT jsonb_agg(to_jsonb(m) ORDER BY name)::text FROM schema_migrations m").Scan(&afterLedger); err != nil || afterLedger != ledger || actions(true) != beforeActions || items() != beforeItems {
		t.Fatal("repeat migrate changed old state or applied ledger", err)
	}
	phase2RTEvidence(t, "025-old-state-upgrade", map[string]any{"old_schema": 24, "old_actions": beforeActions, "old_items": beforeItems, "ledger": ledger, "old_action_count": count, "invented_trusted_tasks": invented, "fixture": "SQL + real 024 document triggers; no old binary API"})
	for _, check := range []struct {
		name, id string
		want     error
	}{
		{"already_undone", undoneAction, workspace.ErrAlreadyUndone},
		{"expired", expiredAction, workspace.ErrExpired},
		{"newer_action", blockedAction, workspace.ErrNewerAction},
	} {
		t.Run(check.name, func(t *testing.T) {
			if _, err := s.Undo(ctx, scope, check.id); !errors.Is(err, check.want) {
				t.Fatalf("old exact undo protection changed: got=%v want=%v", err, check.want)
			}
		})
	}
	t.Run("live_action_still_undoable", func(t *testing.T) {
		state, err := s.Undo(ctx, scope, liveAction)
		if err != nil {
			t.Fatal("old live original action no longer undoable", err)
		}
		for _, item := range state.Tasks {
			if item.ID == live.ID {
				t.Fatal("old creation undo did not remove original item")
			}
		}
		var marked bool
		if err := s.pool.QueryRow(ctx, "SELECT undone_at IS NOT NULL FROM action_log WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), liveAction).Scan(&marked); err != nil || !marked {
			t.Fatal("old successful undo lost original receipt", err)
		}
		phase2RTEvidence(t, "025-real-undo", map[string]any{"live_action": liveAction, "later_action": laterAction, "undone_marked": marked, "state": state})
	})
}

func TestPhase2Runtime025OriginDDLOnlyAcceptsObjectOrSQLNull(t *testing.T) {
	for _, check := range []struct {
		name      string
		value     any
		code      string
		staleNull bool
	}{
		{"sql_null", nil, "", false}, {"object", `{}`, "", false},
		{"array", `[]`, "23514", false}, {"string", `"PRIVATE-DIAGNOSTIC-462"`, "23514", false},
		{"number", `7`, "23514", false}, {"boolean", `true`, "23514", false},
		{"json_null", `null`, "23514", false}, {"stale_not_null", nil, "23502", true},
	} {
		t.Run(check.name, func(t *testing.T) {
			// Each negative SQL statement has its own fresh isolated schema.
			// No failed transaction can mask later real Undo controls.
			s, scope := testStore(t), owner()
			ctx, id := context.Background(), string(memory.NewID())
			if _, err := s.pool.Exec(ctx, "INSERT INTO workspace_owners(owner_id,settings) VALUES($1,'{}')", string(scope.OwnerID)); err != nil {
				t.Fatal(err)
			}
			if _, err := s.pool.Exec(ctx, "INSERT INTO action_log(owner_id,id,source,summary,changes) VALUES($1,$2,'command','synthetic DDL probe','[]')", string(scope.OwnerID), id); err != nil {
				t.Fatal(err)
			}
			var originalNull, defaultStale bool
			if err := s.pool.QueryRow(ctx, "SELECT context_task IS NULL,context_stale FROM action_log WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), id).Scan(&originalNull, &defaultStale); err != nil || !originalNull || defaultStale {
				t.Fatal("new metadata defaults invented recipient/stale", err)
			}
			var err error
			if check.staleNull {
				_, err = s.pool.Exec(ctx, "UPDATE action_log SET context_stale=NULL WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), id)
			} else {
				_, err = s.pool.Exec(ctx, "UPDATE action_log SET context_task=$3::jsonb WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), id, check.value)
			}
			if check.code == "" {
				if err != nil {
					t.Fatal("DDL rejected legal SQL NULL/object shape", err)
				}
			} else {
				var database *pgconn.PgError
				if !errors.As(err, &database) || database.Code != check.code {
					t.Fatalf("wrong SQL constraint result got=%v wantSQLSTATE=%s", err, check.code)
				}
			}
			phase2RTEvidence(t, "025-ddl-shape", map[string]any{"case": check.name, "expected_sqlstate": check.code, "accepted": err == nil, "valid_trusted_task_proven": false})
		})
	}
}

func TestPhase2Runtime025FreshExactly25AndRestartStable(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	var total, origin int
	var checksum, ledger string
	if err := s.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM schema_migrations),(SELECT count(*) FROM schema_migrations WHERE name=$1),(SELECT checksum FROM schema_migrations WHERE name=$1),(SELECT jsonb_agg(to_jsonb(m) ORDER BY name)::text FROM schema_migrations m)", phase2RTActionMigrationName).Scan(&total, &origin, &checksum, &ledger); err != nil || total != 25 || origin != 1 {
		t.Fatal("fresh action-origin migration is not exact25/once", err, total, origin)
	}
	if checksum != phase2RTActionMigrationChecksum {
		t.Fatal("fresh025 bytes differ from approved frozen DDL", checksum)
	}
	if err := s.CheckSchema(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	var after string
	if err := s.pool.QueryRow(ctx, "SELECT jsonb_agg(to_jsonb(m) ORDER BY name)::text FROM schema_migrations m").Scan(&after); err != nil || after != ledger || s.models != nil {
		t.Fatal("fresh repeat startup changed ledger or configured an external model", err)
	}
	phase2RTEvidence(t, "025-fresh-restart", map[string]any{"total": total, "origin_migration_count": origin, "checksum": checksum, "ledger": ledger, "external_provider": "none configured"})
}
