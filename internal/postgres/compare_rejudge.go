package postgres

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/interpretation/comparison"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/postgres/kernel"
	"github.com/soaringjerry/PCAS/internal/worker"
)

// Explicit coordinator operation. One existing retirement pair per invocation;
// no group cursor is reset and no duplicate retirement is selected.
func (s *Store) QueueSupersededRejudge(ctx context.Context, scope memory.Scope) (int, error) {
	if !scope.IsOwner {
		return 0, memory.ErrForbidden
	}
	count := 0
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		if err := kernel.OwnerLock(ctx, tx, scope.OwnerID); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `INSERT INTO memory_jobs(id,owner_id,record_id,record_version,stage,priority)
 SELECT gen_random_uuid(),r.owner_id,r.id,r.version,
 'memory.compare:'||$2::int::text||':rejudge:'||r.id::text||':'||r.version::text||':'||n.id::text||':'||n.version::text,$3
 FROM claims cl JOIN memory_records r ON(r.owner_id,r.id)=(cl.owner_id,cl.id)
 JOIN memory_records n ON(n.owner_id,n.id)=(cl.owner_id,cl.retired_by)
 WHERE cl.owner_id=$1 AND cl.retired='superseded' AND r.state='active' AND n.state='active'
 AND NOT EXISTS(SELECT 1 FROM background_markers m WHERE (m.owner_id,m.record_id,m.record_version)=(r.owner_id,r.id,r.version)
 AND m.stage='memory.compare:'||$2::int::text||':rejudge:'||r.id::text||':'||r.version::text||':'||n.id::text||':'||n.version::text)
 ON CONFLICT(owner_id,record_id,record_version,stage) DO NOTHING`, string(scope.OwnerID), CompareVersion, ComparePriority)
		count = int(tag.RowsAffected())
		return err
	})
	return count, err
}

type rejudgeInput struct {
	Memories []compareMemory `json:"memories"`
	Refs     []memory.Ref    `json:"refs"`
	Rule     int             `json:"rule"`
}

func readRejudgePair(ctx context.Context, tx pgx.Tx, j worker.Job, refs []memory.Ref) (*rejudgeInput, error) {
	var current bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM claims cl JOIN memory_records r ON(r.owner_id,r.id)=(cl.owner_id,cl.id)
 JOIN memory_records n ON(n.owner_id,n.id)=(cl.owner_id,cl.retired_by)
 WHERE cl.owner_id=$1 AND cl.id=$2 AND cl.retired='superseded' AND cl.retired_by=$3
 AND r.version=$4 AND n.version=$5 AND r.state='active' AND n.state='active'
 AND claim_source_is_current($1,$2,$4,now()) AND claim_source_is_current($1,$3,$5,now()))`, string(j.OwnerID), string(refs[0].ID), string(refs[1].ID), refs[0].Version, refs[1].Version).Scan(&current)
	if err != nil || !current {
		return nil, err
	}
	input := &rejudgeInput{Refs: refs, Rule: CompareVersion}
	for i, ref := range refs {
		m := compareMemory{organizeMemory: organizeMemory{N: i + 1, Ref: ref}}
		err = tx.QueryRow(ctx, `SELECT c.value #>> '{}',rv.expressed_at,
 (c.confirmation='confirmed' OR EXISTS(SELECT 1 FROM record_versions edited WHERE edited.owner_id=r.owner_id AND edited.record_id=r.id AND edited.version>1 AND edited.actor='user') OR `+restoredMemorySQL("1")+`)
 FROM claims cl JOIN memory_records r ON(r.owner_id,r.id)=(cl.owner_id,cl.id)
 JOIN claim_revisions c ON(c.owner_id,c.claim_id,c.version)=(r.owner_id,r.id,r.version)
 JOIN record_versions rv ON(rv.owner_id,rv.record_id,rv.version)=(r.owner_id,r.id,r.version)
 WHERE cl.owner_id=$1 AND cl.id=$2`, string(j.OwnerID), string(ref.ID)).Scan(&m.Text, &m.ExpressedAt, &m.Protected)
		if err != nil {
			return nil, err
		}
		input.Memories = append(input.Memories, m)
	}
	return input, nil
}

func finishRejudgeTx(ctx context.Context, tx pgx.Tx, j worker.Job, reason string) error {
	if _, err := tx.Exec(ctx, `INSERT INTO background_markers(owner_id,record_id,record_version,stage,data)
 VALUES($1,$2,$3,$4,jsonb_build_object('outcome',$5::text)) ON CONFLICT DO NOTHING`, string(j.OwnerID), string(j.Record.ID), j.Record.Version, j.Stage, reason); err != nil {
		return err
	}
	if err := kernel.DiscardPaidResult(ctx, tx, j); err != nil {
		return err
	}
	return kernel.Acknowledge(ctx, tx, j)
}

func (a compareSession) RejudgePair(ctx context.Context, j worker.Job, refs []memory.Ref) (json.RawMessage, bool, error) {
	var input *rejudgeInput
	err := pgx.BeginFunc(ctx, a.conn, func(tx pgx.Tx) error {
		if err := kernel.LockJob(ctx, tx, j); err != nil {
			return err
		}
		var e error
		input, e = readRejudgePair(ctx, tx, j, refs)
		if e != nil {
			return e
		}
		if input == nil {
			return finishRejudgeTx(ctx, tx, j, "inputs_changed")
		}
		return backgroundHourlyTx(ctx, tx, CompareStage, "compare_hourly_limit")
	})
	if err != nil || input == nil {
		return nil, false, err
	}
	return kernel.JSON(input), true, nil
}

func (a compareSession) ApplyRejudge(ctx context.Context, j worker.Job, refs []memory.Ref, edges []comparison.Edge, valid bool) error {
	conn := a.conn
	if !valid {
		err := kernel.BackgroundResultTx(ctx, conn, j.OwnerID, func(ctx context.Context, tx pgx.Tx) error {
			if err := kernel.LockJob(ctx, tx, j); err != nil {
				return err
			}
			if err := kernel.StageEvent(ctx, tx, j.OwnerID, CompareStage, "failure", "rejudge_invalid_output", 1); err != nil {
				return err
			}
			return kernel.DiscardPaidResult(ctx, tx, j)
		})
		if err != nil {
			return err
		}
		return &worker.JobError{Code: "rejudge_invalid_output", Until: time.Now().Add(kernel.RetryDelay(j.Attempts))}
	}
	return kernel.BackgroundResultTx(ctx, conn, j.OwnerID, func(ctx context.Context, tx pgx.Tx) error {
		if err := kernel.LockJob(ctx, tx, j); err != nil {
			return err
		}
		// Fence the original retirement and both record versions against user edits.
		rows, err := tx.Query(ctx, `SELECT r.id FROM memory_records r JOIN claims cl ON(cl.owner_id,cl.id)=(r.owner_id,r.id)
 WHERE r.owner_id=$1 AND r.id=ANY($2::uuid[]) ORDER BY r.id FOR UPDATE OF r,cl`, string(j.OwnerID), []string{string(refs[0].ID), string(refs[1].ID)})
		if err != nil {
			return err
		}
		for rows.Next() {
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		current, err := readRejudgePair(ctx, tx, j, refs)
		if err != nil {
			return err
		}
		if current == nil {
			return finishRejudgeTx(ctx, tx, j, "inputs_changed")
		}
		keep := !current.Memories[0].Protected && len(edges) == 1 && edges[0].Kind == "superseded"
		outcome := "kept_superseded"
		if !keep {
			if _, err := tx.Exec(ctx, `UPDATE claims SET retired='',retired_by=NULL,retired_at=NULL,compared=$3 WHERE owner_id=$1 AND id=$2`, string(j.OwnerID), string(refs[0].ID), CompareVersion); err != nil {
				return err
			}
			outcome = "restored"
		} else {
			if _, err := tx.Exec(ctx, "UPDATE claims SET compared=$3 WHERE owner_id=$1 AND id=$2", string(j.OwnerID), string(refs[0].ID), CompareVersion); err != nil {
				return err
			}
		}
		if err := kernel.StageEvent(ctx, tx, j.OwnerID, CompareStage, "success", "rejudge_"+outcome, 1); err != nil {
			return err
		}
		return finishRejudgeTx(ctx, tx, j, outcome)
	})
}
