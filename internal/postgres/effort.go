package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/postgres/itemstore"
	"github.com/soaringjerry/PCAS/internal/postgres/kernel"
	"github.com/soaringjerry/PCAS/internal/postgres/workspacestore"
	"github.com/soaringjerry/PCAS/internal/worker"
	"github.com/soaringjerry/PCAS/internal/workspace"
	"github.com/soaringjerry/PCAS/internal/workspace/effort"
)

func enqueueEffortTx(ctx context.Context, tx pgx.Tx, owner memory.ID, anchor memory.Ref, now time.Time) (int, error) {
	installed, err := workspacestore.StudioInstalledTx(ctx, tx)
	if err != nil || !installed {
		return 0, err
	}
	ids, err := kernel.QueryDocuments[string](ctx, tx, `SELECT to_jsonb(p.id::text) FROM work_items p WHERE p.owner_id=$1 AND p.kind='project' AND EXISTS(SELECT 1 FROM work_items w WHERE w.owner_id=p.owner_id AND w.project_id=p.id AND w.kind='task' AND w.due_at IS NOT NULL AND (w.document->>'estimatedHours') IS NULL AND coalesce(w.document->>'effortSource','')<>'user') AND NOT EXISTS(SELECT 1 FROM memory_jobs j WHERE j.owner_id=p.owner_id AND j.stage LIKE $2||':'||p.id::text||':%' AND j.state IN('queued','leased')) ORDER BY p.created_at,p.id`, string(owner), workspacestore.EffortStage)
	if err != nil {
		return 0, err
	}
	for _, id := range ids {
		if _, err = tx.Exec(ctx, `INSERT INTO memory_jobs(id,owner_id,record_id,record_version,stage,priority,available_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, string(memory.NewID()), string(owner), string(anchor.ID), anchor.Version, workspacestore.EffortStage+":"+id+":"+string(memory.NewID()), kernel.HandoverPriority, now); err != nil {
			return 0, err
		}
	}
	return len(ids), nil
}

// EffortStore gives the effort service its workspace reads and writes.
func (s *Store) EffortStore() effort.Store { return effortStore{s} }

type effortStore struct{ store *Store }

func (a effortStore) Load(ctx context.Context, j worker.Job, project string, wantTasks bool) (item workspace.Item, tasks []workspace.Item, done bool, err error) {
	scope := memory.Scope{OwnerID: j.OwnerID, PrincipalID: "worker", IsOwner: true}
	err = pgx.BeginTxFunc(ctx, a.store.Pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead}, func(tx pgx.Tx) error {
		if err := kernel.LockJob(ctx, tx, j); err != nil {
			return err
		}
		var err error
		item, err = itemstore.GetItem(ctx, tx, scope, project)
		if errors.Is(err, memory.ErrNotFound) {
			done = true
			return kernel.Acknowledge(ctx, tx, j)
		}
		if err != nil || !wantTasks {
			return err
		}
		tasks, err = kernel.QueryDocuments[workspace.Item](ctx, tx, `SELECT document FROM work_items WHERE owner_id=$1 AND project_id=$2 AND kind='task' AND due_at IS NOT NULL AND document->>'estimatedHours' IS NULL AND coalesce(document->>'effortSource','')<>'user' ORDER BY due_at,id`, string(scope.OwnerID), project)
		if err != nil {
			return err
		}
		if len(tasks) == 0 {
			done = true
			return kernel.Acknowledge(ctx, tx, j)
		}
		return nil
	})
	return item, tasks, done, err
}

func (a effortStore) Apply(ctx context.Context, j worker.Job, fn func(effort.Unit) error) error {
	return kernel.BackgroundResultTx(ctx, a.store.Pool, j.OwnerID, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT 1 FROM workspace_owners WHERE owner_id=$1 FOR UPDATE", string(j.OwnerID)); err != nil {
			return err
		}
		if err := kernel.LockJob(ctx, tx, j); err != nil {
			return err
		}
		return fn(effortUnit{ctx: ctx, tx: tx, job: j, scope: memory.Scope{OwnerID: j.OwnerID, PrincipalID: "worker", IsOwner: true}})
	})
}

type effortUnit struct {
	ctx   context.Context
	tx    pgx.Tx
	job   worker.Job
	scope memory.Scope
}

func (u effortUnit) Location() (*time.Location, error) {
	settings, err := kernel.QueryDocument[workspace.Settings](u.ctx, u.tx, "SELECT settings FROM workspace_owners WHERE owner_id=$1", string(u.scope.OwnerID))
	return settings.Location(), err
}

func (u effortUnit) Task(id string) (workspace.Item, bool, error) {
	item, err := itemstore.GetItem(u.ctx, u.tx, u.scope, id)
	if errors.Is(err, memory.ErrNotFound) {
		return item, false, nil
	}
	return item, err == nil, err
}

func (u effortUnit) SaveTask(item workspace.Item) error {
	return itemstore.SaveItem(u.ctx, u.tx, u.scope, item)
}

func (u effortUnit) Event(outcome, reason string, count int) error {
	return kernel.StageEvent(u.ctx, u.tx, u.job.OwnerID, workspacestore.EffortStage, outcome, reason, count)
}

func (u effortUnit) DiscardResult() error { return kernel.DiscardPaidResult(u.ctx, u.tx, u.job) }

func (u effortUnit) Requeue(code string) error {
	_, err := u.tx.Exec(u.ctx, `UPDATE memory_jobs SET state='queued',error_code=$4,available_at=clock_timestamp()+$3*interval '1 second',lease_token=NULL,lease_until=NULL WHERE id=$1 AND lease_token=$2`, string(u.job.ID), string(u.job.LeaseToken), kernel.RetryDelay(u.job.Attempts).Seconds(), code)
	return err
}

func (u effortUnit) Acknowledge() error { return kernel.Acknowledge(u.ctx, u.tx, u.job) }
