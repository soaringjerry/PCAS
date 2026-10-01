package postgres

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/httpapi"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func approvedUndoCommand(t *testing.T, s *Store, scope memory.Scope, c workspace.Command) (workspace.State, string) {
	t.Helper()
	c.RequestID = string(memory.NewID())
	st, err := s.Snapshot(context.Background(), scope)
	if err != nil {
		t.Fatal(err)
	}
	c.ExpectedRevision = st.Revision
	st, err = s.Execute(context.Background(), scope, c)
	if err != nil {
		t.Fatal(err)
	}
	return st, c.RequestID
}

func approvedUndoHTTP(t *testing.T, s *Store, scope memory.Scope, action, code string) {
	t.Helper()
	ctx := context.Background()
	st, err := s.Snapshot(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	var wasUndone bool
	if err := s.pool.QueryRow(ctx, "SELECT undone_at IS NOT NULL FROM action_log WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), action).Scan(&wasUndone); err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("a", 64)
	api := httpapi.New(s, s, httpapi.NewOwnerToken(token, scope.OwnerID), func(context.Context) error { return nil }, slog.New(slog.NewTextHandler(io.Discard, nil)), httpapi.Options{Workspace: s})
	body, _ := json.Marshal(workspace.Command{Type: "undoAction", ID: action, RequestID: string(memory.NewID()), ExpectedRevision: st.Revision})
	req := httptest.NewRequest("POST", "/v1/workspace/commands", strings.NewReader(string(body)))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	api.ServeHTTP(w, req)
	if w.Code != 409 || !strings.Contains(w.Body.String(), `"error":"`+code+`"`) {
		t.Fatalf("want 409 %s; got %d %s", code, w.Code, w.Body.String())
	}
	after, err := s.Snapshot(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	if string(asJSON(st)) != string(asJSON(after)) {
		t.Fatal("refused undo changed state")
	}
	var undone bool
	if err = s.pool.QueryRow(ctx, "SELECT undone_at IS NOT NULL FROM action_log WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), action).Scan(&undone); err != nil || undone != wasUndone {
		t.Fatal("refused undo changed audit state", err)
	}
}

func TestApprovedUndoRecordedSources(t *testing.T) {
	for _, source := range []string{"command", "desk", "worker"} {
		t.Run(source, func(t *testing.T) {
			s, scope := testStore(t), owner()
			st, created := approvedUndoCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "original"})
			id := st.Tasks[0].ID
			later := string(memory.NewID())
			ctx := context.Background()
			err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
				ctx := withActionLog(ctx, later, source, "", "recorded update")
				if err := beginActionLogTx(ctx, tx); err != nil {
					return err
				}
				if err := s.commandTx(ctx, tx, scope, workspace.Command{Type: "renameThing", ID: id, Title: "later"}); err != nil {
					return err
				}
				return flushActionLog(ctx, tx, scope)
			})
			if err != nil {
				t.Fatal(err)
			}
			approvedUndoHTTP(t, s, scope, created, "newer_action")
			st, err = s.Undo(ctx, scope, later)
			if err != nil || st.Tasks[0].Title != "original" {
				t.Fatal(err, st.Tasks)
			}
			st, err = s.Undo(ctx, scope, created)
			if err != nil || len(st.Tasks) != 0 {
				t.Fatal(err, st.Tasks)
			}
		})
	}
}

func TestApprovedUndoUnloggedContentAndIndependentRows(t *testing.T) {
	s, scope := testStore(t), owner()
	st, first := approvedUndoCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "first"})
	id := st.Tasks[0].ID
	_, other := approvedUndoCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "unrelated"})
	if _, err := s.Undo(context.Background(), scope, first); err != nil {
		t.Fatal("unrelated later action blocked undo", err)
	}
	st, err := s.Undo(context.Background(), scope, other)
	if err != nil || len(st.Tasks) != 0 {
		t.Fatal(err, st.Tasks)
	}
	st, first = approvedUndoCommand(t, s, scope, workspace.Command{Type: "addTask", ID: string(memory.NewID()), Title: "first"})
	id = st.Tasks[0].ID
	if _, err := s.pool.Exec(context.Background(), `UPDATE work_items SET title='external',document=jsonb_set(document,'{title}','"external"') WHERE owner_id=$1 AND id=$2`, string(scope.OwnerID), id); err != nil {
		t.Fatal(err)
	}
	approvedUndoHTTP(t, s, scope, first, "changed_since")
}

func TestApprovedUndoSameTransactionOrder(t *testing.T) {
	s, scope := testStore(t), owner()
	st, _ := approvedUndoCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "base"})
	id := st.Tasks[0].ID
	actions := []string{string(memory.NewID()), string(memory.NewID()), string(memory.NewID())}
	ctx := context.Background()
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		for i, title := range []string{"first", "second", "first"} {
			logged := withActionLog(ctx, actions[i], "desk", "", "same turn")
			if err := beginActionLogTx(logged, tx); err != nil {
				return err
			}
			if err := s.commandTx(logged, tx, scope, workspace.Command{Type: "renameThing", ID: id, Title: title}); err != nil {
				return err
			}
			if err := flushActionLog(logged, tx, scope); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var timestamps int
	if err := s.pool.QueryRow(ctx, "SELECT count(DISTINCT created_at) FROM action_log WHERE owner_id=$1 AND id=ANY($2::uuid[])", string(scope.OwnerID), actions).Scan(&timestamps); err != nil {
		t.Fatal(err)
	}
	if timestamps != 1 {
		t.Fatal("fixture must exercise tied transaction timestamps", timestamps)
	}
	// The last action restores the first action's business value. A fingerprint
	// match alone must still refuse skipping either recorded successor.
	approvedUndoHTTP(t, s, scope, actions[0], "newer_action")
	approvedUndoHTTP(t, s, scope, actions[1], "newer_action")
	for i := len(actions) - 1; i >= 0; i-- {
		if _, err := s.Undo(ctx, scope, actions[i]); err != nil {
			t.Fatalf("reverse action %d: %v", i, err)
		}
	}
	st, err = s.Snapshot(ctx, scope)
	if err != nil || st.Tasks[0].Title != "base" {
		t.Fatal(err, st.Tasks)
	}
	approvedUndoHTTP(t, s, scope, actions[0], "already_undone")
}

func TestApprovedUndoHistoricalOrder(t *testing.T) {
	for _, mode := range []string{"timestamps", "receipts", "missing-receipt", "duplicate-receipt", "unknown-tie"} {
		t.Run(mode, func(t *testing.T) {
			s, scope := testStore(t), owner()
			ctx := context.Background()
			st, created := approvedUndoCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "original"})
			_, later := approvedUndoCommand(t, s, scope, workspace.Command{Type: "renameThing", ID: st.Tasks[0].ID, Title: "later"})
			if _, err := s.pool.Exec(ctx, "UPDATE action_log SET action_order=NULL WHERE owner_id=$1", string(scope.OwnerID)); err != nil {
				t.Fatal(err)
			}
			if mode != "timestamps" {
				if _, err := s.pool.Exec(ctx, "UPDATE action_log SET created_at=now() WHERE owner_id=$1", string(scope.OwnerID)); err != nil {
					t.Fatal(err)
				}
			}
			if strings.Contains(mode, "receipt") {
				turnID := string(memory.NewID())
				receipts := []map[string]string{{"actionId": created}, {"actionId": later}}
				if mode == "missing-receipt" {
					receipts = receipts[1:]
				}
				if mode == "duplicate-receipt" {
					receipts = append(receipts, map[string]string{"actionId": later})
				}
				response := asJSON(map[string]any{"turn": map[string]any{"receipts": receipts}})
				if _, err := s.pool.Exec(ctx, "INSERT INTO desk_turns(owner_id,id,agent_id,question,answer,response) VALUES($1,$2,'fixture','fixture','fixture',$3)", string(scope.OwnerID), turnID, response); err != nil {
					t.Fatal(err)
				}
				if _, err := s.pool.Exec(ctx, "UPDATE action_log SET source='desk',turn_id=$2 WHERE owner_id=$1", string(scope.OwnerID), turnID); err != nil {
					t.Fatal(err)
				}
			}
			known := mode == "timestamps" || mode == "receipts"
			if known {
				approvedUndoHTTP(t, s, scope, created, "newer_action")
				if _, err := s.Undo(ctx, scope, later); err != nil {
					t.Fatal(err)
				}
				if _, err := s.Undo(ctx, scope, created); err != nil {
					t.Fatal(err)
				}
			} else {
				approvedUndoHTTP(t, s, scope, created, "changed_since")
				approvedUndoHTTP(t, s, scope, later, "changed_since")
				// A provable new successor wins over an ambiguous historical tie.
				_, newer := approvedUndoCommand(t, s, scope, workspace.Command{Type: "setNotes", ID: st.Tasks[0].ID, Text: "new logged mutation"})
				approvedUndoHTTP(t, s, scope, created, "newer_action")
				if _, err := s.Undo(ctx, scope, newer); err != nil {
					t.Fatal(err)
				}
				approvedUndoHTTP(t, s, scope, later, "changed_since")
			}
		})
	}
}

func TestApprovedUndoMigrationPreservesHistoricalAudit(t *testing.T) {
	s, scope := testStore(t), owner()
	ctx := context.Background()
	st, action := approvedUndoCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "historical"})
	var before string
	if err := s.pool.QueryRow(ctx, "SELECT (to_jsonb(a)-'action_order')::text FROM action_log a WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), action).Scan(&before); err != nil {
		t.Fatal(err)
	}
	// Reconstruct the 019 table, retaining real collected snapshots and audit data.
	if _, err := s.pool.Exec(ctx, "ALTER TABLE action_log DROP COLUMN action_order CASCADE"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, "DELETE FROM schema_migrations WHERE name='020_action_order.sql'"); err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatal("migration not repeatable", err)
	}
	var after string
	var historicalOrder *int64
	if err := s.pool.QueryRow(ctx, "SELECT (to_jsonb(a)-'action_order')::text,action_order FROM action_log a WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), action).Scan(&after, &historicalOrder); err != nil {
		t.Fatal(err)
	}
	if after != before || historicalOrder != nil {
		t.Fatal("migration altered history or invented its order")
	}
	_, later := approvedUndoCommand(t, s, scope, workspace.Command{Type: "renameThing", ID: st.Tasks[0].ID, Title: "new"})
	var newOrder *int64
	if err := s.pool.QueryRow(ctx, "SELECT action_order FROM action_log WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), later).Scan(&newOrder); err != nil || newOrder == nil {
		t.Fatal("new order absent", err)
	}
	approvedUndoHTTP(t, s, scope, action, "newer_action")
	if _, err := s.Undo(ctx, scope, later); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Undo(ctx, scope, action); err != nil {
		t.Fatal(err)
	}
}

func TestApprovedUndoProtectionPriority(t *testing.T) {
	s, scope := testStore(t), owner()
	ctx := context.Background()
	s.SetModels(&ai.Registry{Config: ai.Configuration{Providers: []ai.Provider{{ID: "model", Name: "local fixture", Protocol: "openai", BaseURL: "http://127.0.0.1:18152", Model: "fake", CostMode: "free"}}}})
	if _, err := s.Snapshot(ctx, scope); err != nil {
		t.Fatal(err)
	}
	action, id := string(memory.NewID()), string(memory.NewID())
	if _, err := s.Execute(ctx, scope, workspace.Command{Type: "delegateTask", ID: id, Title: "original", Prompt: "fixture", AgentID: "model", RequestID: action}); err != nil {
		t.Fatal(err)
	}
	_, later := approvedUndoCommand(t, s, scope, workspace.Command{Type: "renameThing", ID: id, Title: "later"})
	if _, err := s.pool.Exec(ctx, "UPDATE agent_runs SET status='running' WHERE owner_id=$1 AND thing_id=$2", string(scope.OwnerID), id); err != nil {
		t.Fatal(err)
	}
	approvedUndoHTTP(t, s, scope, action, "newer_action")
	if _, err := s.Undo(ctx, scope, later); err != nil {
		t.Fatal(err)
	}
	approvedUndoHTTP(t, s, scope, action, "work_started")
	if _, err := s.pool.Exec(ctx, "UPDATE action_log SET expired_at=now(),changes='[]' WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), action); err != nil {
		t.Fatal(err)
	}
	approvedUndoHTTP(t, s, scope, action, "expired")
	if _, err := s.pool.Exec(ctx, "UPDATE action_log SET undone_at=now() WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), action); err != nil {
		t.Fatal(err)
	}
	approvedUndoHTTP(t, s, scope, action, "already_undone")
}
