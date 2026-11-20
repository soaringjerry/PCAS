package deputystore

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/postgres/accessstore"
	"github.com/soaringjerry/PCAS/internal/postgres/intakestore"
	"github.com/soaringjerry/PCAS/internal/postgres/itemstore"
	"github.com/soaringjerry/PCAS/internal/postgres/kernel"
	"github.com/soaringjerry/PCAS/internal/postgres/usestore"
	"github.com/soaringjerry/PCAS/internal/postgres/workspacestore"
	"github.com/soaringjerry/PCAS/internal/postgres/workstore"
	"github.com/soaringjerry/PCAS/internal/workspace"
	"github.com/soaringjerry/PCAS/internal/workspace/deputy"
)

// adoptUnit is the storage of one adoption. deputy.Adopt decides; adoptUnit
// reads and writes.
type adoptUnit struct {
	store *Deputy
	tx    pgx.Tx
	scope memory.Scope
}

var _ deputy.AdoptUnit = (*adoptUnit)(nil)

func (u *adoptUnit) Agent(ctx context.Context, id string) (workspace.Agent, error) {
	return kernel.QueryDocument[workspace.Agent](ctx, u.tx, "SELECT document FROM workspace_agents WHERE owner_id=$1 AND id=$2", string(u.scope.OwnerID), id)
}

func (u *adoptUnit) VerifyAccess(ctx context.Context, run workspace.Run) error {
	return accessstore.VerifyRunAccessTx(ctx, usestore.UseClockTx{Tx: u.tx, At: time.Now()}, u.scope, run)
}

func (u *adoptUnit) Item(ctx context.Context, id string) (workspace.Item, error) {
	return itemstore.GetItem(ctx, u.tx, u.scope, id)
}

func (u *adoptUnit) ClearAdoption(ctx context.Context, run string) error {
	_, err := u.tx.Exec(ctx, "DELETE FROM adopted_artifacts WHERE owner_id=$1 AND run_id=$2", string(u.scope.OwnerID), run)
	return err
}

func (u *adoptUnit) Revise(ctx context.Context, run workspace.Run, text string) error {
	return u.store.adoptReviseDocumentTx(ctx, u.tx, u.scope, run, text)
}

func (u *adoptUnit) SaveDoc(ctx context.Context, doc workspace.Doc) error {
	return workspacestore.SaveDoc(ctx, u.tx, u.scope, doc)
}

func (u *adoptUnit) ProjectProgress(ctx context.Context, run *workspace.Run, project workspace.Item, text, actionID string, auto bool) error {
	return u.store.AdoptProjectMemoryTx(ctx, u.tx, u.scope, run, project, text, actionID, auto)
}

func (u *adoptUnit) Artifact(ctx context.Context, run, item, kind, id, text string) error {
	return workstore.ArtifactTx(ctx, u.tx, u.scope, run, item, kind, id, text)
}

func (u *adoptUnit) SaveItem(ctx context.Context, item workspace.Item) error {
	return itemstore.SaveItem(ctx, u.tx, u.scope, item)
}

func (u *adoptUnit) Actor(ctx context.Context) string { return kernel.ActorFromContext(ctx) }

func (u *adoptUnit) SaveAction(ctx context.Context, item workspace.Item, summary string) error {
	return u.store.SaveAction(ctx, u.tx, u.scope, item, summary)
}

func (u *adoptUnit) Sample(ctx context.Context, sample workspace.Sample) error {
	return intakestore.SampleTx(ctx, u.tx, u.scope, sample)
}

func (u *adoptUnit) RecordUse(ctx context.Context, event memory.UseEvent) error {
	return intakestore.RecordUseTx(ctx, u.tx, u.scope, event)
}
