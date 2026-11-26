package workspacestore

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/postgres/kernel"
	"github.com/soaringjerry/PCAS/internal/workspace/effort"
)

// EnqueueEffortTx queues effort estimation jobs for projects with unestimated
// due tasks; effort.Due decides which projects are queued.
func EnqueueEffortTx(ctx context.Context, tx pgx.Tx, owner memory.ID, anchor memory.Ref, now time.Time) (int, error) {
	installed, err := StudioInstalledTx(ctx, tx)
	if err != nil || !installed {
		return 0, err
	}
	rows, err := tx.Query(ctx, `SELECT p.id::text,
 EXISTS(SELECT 1 FROM work_items w WHERE w.owner_id=p.owner_id AND w.project_id=p.id AND w.kind='task' AND w.due_at IS NOT NULL AND (w.document->>'estimatedHours') IS NULL AND coalesce(w.document->>'effortSource','')<>'user'),
 EXISTS(SELECT 1 FROM memory_jobs j WHERE j.owner_id=p.owner_id AND j.stage LIKE $2||':'||p.id::text||':%' AND j.state IN('queued','leased'))
 FROM work_items p WHERE p.owner_id=$1 AND p.kind='project' ORDER BY p.created_at,p.id`, string(owner), effort.Stage)
	if err != nil {
		return 0, err
	}
	projects := []effort.Project{}
	for rows.Next() {
		var p effort.Project
		if err := rows.Scan(&p.ID, &p.NeedsEstimate, &p.Pending); err != nil {
			rows.Close()
			return 0, err
		}
		projects = append(projects, p)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()
	due := effort.Due(projects)
	for _, id := range due {
		if _, err = tx.Exec(ctx, `INSERT INTO memory_jobs(id,owner_id,record_id,record_version,stage,priority,available_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, string(memory.NewID()), string(owner), string(anchor.ID), anchor.Version, effort.Stage+":"+id+":"+string(memory.NewID()), kernel.HandoverPriority, now); err != nil {
			return 0, err
		}
	}
	return len(due), nil
}
