package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
)

const (
	ExtractionBackfillInterval    = 10 * time.Minute
	extractionBackfillQueueLimit  = 20
	extractionBackfillHourlyLimit = 30
)

// BackfillExtractions serializes schedulers in PostgreSQL. Only root jobs carry
// the enqueue timestamp: an archive import or a long-source window consumes no
// additional backfill slot.
func (s *Store) BackfillExtractions(ctx context.Context, now time.Time) (int, error) {
	if now.IsZero() {
		return 0, memory.ErrInvalid
	}
	count := 0
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(734826192)"); err != nil {
			return err
		}
		var pending, recent int
		if err := tx.QueryRow(ctx, `SELECT
   count(*) FILTER (WHERE j.state IN ('queued','leased') OR EXISTS(
    SELECT 1 FROM memory_jobs child WHERE (child.owner_id,child.record_id,child.record_version)=(j.owner_id,j.record_id,j.record_version)
      AND child.stage LIKE 'source.extract:%' AND child.state IN ('queued','leased'))),
   count(*) FILTER (WHERE j.backfill_queued_at>$1 AND j.backfill_queued_at<=$2)
   FROM memory_jobs j WHERE j.stage='source.extract' AND j.backfill_queued_at IS NOT NULL`, now.Add(-time.Hour), now).Scan(&pending, &recent); err != nil {
			return err
		}
		capacity := min(extractionBackfillQueueLimit-pending, extractionBackfillHourlyLimit-recent)
		if capacity <= 0 {
			return nil
		}
		rows, err := tx.Query(ctx, `SELECT r.owner_id::text,r.id::text,r.version FROM memory_records r
   JOIN sources src ON (src.owner_id,src.id)=(r.owner_id,r.id)
   JOIN source_versions v ON (v.owner_id,v.source_id,v.version)=(r.owner_id,r.id,r.version)
   JOIN record_versions rv ON (rv.owner_id,rv.record_id,rv.version)=(r.owner_id,r.id,r.version)
   WHERE r.state='active' AND rv.state='active' AND length(btrim(v.body))>0
    AND src.connector NOT IN ('actions','corrections','memory-input')
    AND NOT EXISTS(SELECT 1 FROM source_extractions se WHERE (se.owner_id,se.source_id,se.source_version)=(r.owner_id,r.id,r.version) AND se.extractor>=2)
    AND NOT EXISTS(SELECT 1 FROM memory_jobs j WHERE (j.owner_id,j.record_id,j.record_version)=(r.owner_id,r.id,r.version)
      AND (j.stage IN ('source.parse','source.chunk','source.extract') OR j.stage LIKE 'source.extract:%') AND j.state IN ('queued','leased'))
   AND NOT EXISTS(SELECT 1 FROM memory_jobs j WHERE (j.owner_id,j.record_id,j.record_version)=(r.owner_id,r.id,r.version) AND j.stage='source.extract' AND j.backfill_queued_at IS NOT NULL)
   ORDER BY rv.recorded_at DESC,r.id LIMIT $1`, capacity)
		if err != nil {
			return err
		}
		type source struct {
			owner, id string
			version   int
		}
		var sources []source
		for rows.Next() {
			var item source
			if err := rows.Scan(&item.owner, &item.id, &item.version); err != nil {
				rows.Close()
				return err
			}
			sources = append(sources, item)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, item := range sources {
			// An old extractor's completed window jobs must be rerun with its root.
			// Clear them only after the fenced root insert/reset succeeded.
			tag, err := tx.Exec(ctx, `INSERT INTO memory_jobs(id,owner_id,record_id,record_version,stage,priority,backfill_queued_at,available_at)
    VALUES($1,$2,$3,$4,'source.extract',10,$5,$5)
    ON CONFLICT(owner_id,record_id,record_version,stage) DO UPDATE SET state='queued',attempts=0,error_code='',priority=10,
      backfill_queued_at=excluded.backfill_queued_at,available_at=excluded.available_at,lease_token=NULL,lease_until=NULL,updated_at=now()
    WHERE memory_jobs.state IN ('done','blocked','failed') AND memory_jobs.backfill_queued_at IS NULL`, string(memory.NewID()), item.owner, item.id, item.version, now)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 0 {
				continue
			}
			if _, err := tx.Exec(ctx, `DELETE FROM memory_jobs WHERE owner_id=$1 AND record_id=$2 AND record_version=$3
     AND stage LIKE 'source.extract:%' AND state NOT IN ('queued','leased')`, item.owner, item.id, item.version); err != nil {
				return err
			}
			count++
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}
