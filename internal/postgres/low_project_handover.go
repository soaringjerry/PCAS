package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func studioInstalledTx(ctx context.Context, tx pgx.Tx) (bool, error) {
	var ok bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema() AND c.relname='project_handovers')`).Scan(&ok)
	return ok, err
}

func projectHandoverTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, id string) (workspace.ProjectHandover, error) {
	out := emptyProjectHandover(id)
	var raw json.RawMessage
	var written *time.Time
	err := tx.QueryRow(ctx, `SELECT h.body,h.written_at,h.input_events<>(SELECT count(*) FROM project_input_events e WHERE e.owner_id=h.owner_id AND e.project_id=h.project_id)
 FROM project_handovers h WHERE h.owner_id=$1 AND h.project_id=$2`, string(scope.OwnerID), id).Scan(&raw, &written, &out.Stale)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM project_input_events WHERE owner_id=$1 AND project_id=$2)`, string(scope.OwnerID), id).Scan(&out.Stale)
		return out, err
	}
	if err != nil {
		return out, err
	}
	stale := out.Stale
	if err = json.Unmarshal(raw, &out); err != nil {
		return out, err
	}
	out.ProjectID = id
	out.Stale = stale
	if written != nil {
		at := written.UTC().Format(time.RFC3339Nano)
		out.WrittenAt = &at
	}
	return out, nil
}
