package postgres

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/worker"
)

const maxAttempts = 5

func (s *Store) Claim(ctx context.Context, lease time.Duration) (*worker.Job, error) {
	if lease <= 0 {
		return nil, memory.ErrInvalid
	}
	if err := s.expireExhaustedJobs(ctx); err != nil {
		return nil, err
	}
	var err error
	var job worker.Job
	var id, ownerID, recordID, token, kind string
	// History preparation and conversation indexing must yield to fresh input
	// too. Resolve archive membership at claim time so already queued jobs and
	// follow-up stages receive the same priority without replaying an import.
	// Which job is next is decided among the first few hundred in queue order,
	// which the index hands over at once; looking at every waiting job took
	// seconds per claim with a large import queued. Only when all of those are
	// held back (a paused or stored-only import) is the whole queue examined.
	leaseToken := string(memory.NewID())
	for _, query := range []string{claimWindowed, claimWhole} {
		err = s.pool.QueryRow(ctx, query, lease.Seconds(), leaseToken, maxAttempts).Scan(&id, &ownerID, &recordID, &job.Record.Version, &job.Stage, &job.Attempts, &token, &kind)
		if !errors.Is(err, pgx.ErrNoRows) {
			break
		}
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	job.ID, job.OwnerID, job.Record.ID, job.LeaseToken = memory.ID(id), memory.ID(ownerID), memory.ID(recordID), memory.ID(token)
	job.Record.Kind = memory.Kind(kind)
	return &job, nil
}

func (s *Store) Block(ctx context.Context, job worker.Job, code string) error {
	return s.finishAttempt(ctx, job, "blocked", code, 0)
}

func (s *Store) Retry(ctx context.Context, job worker.Job, code string) error {
	state := "queued"
	if job.Attempts >= maxAttempts {
		state = "failed"
	}
	return s.finishAttempt(ctx, job, state, code, retryDelay(job.Attempts))
}

func (s *Store) finishAttempt(ctx context.Context, job worker.Job, state, code string, delay time.Duration) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if strings.HasPrefix(job.Stage, "source.extract") {
			if err := extractionOwnerLock(ctx, tx, job.OwnerID); err != nil {
				return err
			}
		}
		if err := lockJob(ctx, tx, job); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE memory_jobs SET state=$3,error_code=$4,available_at=now()+$5*interval '1 second',lease_until=NULL,lease_token=NULL,updated_at=now()
            WHERE id=$1 AND lease_token=$2`, string(job.ID), string(job.LeaseToken), state, code, delay.Seconds())
		if err != nil {
			return err
		}
		if (state == "blocked" || state == "failed") && strings.HasPrefix(job.Stage, "source.extract") {
			return extractionStateTx(ctx, tx, job, "failed")
		}
		return nil
	})
}

func retryDelay(attempt int) time.Duration {
	delays := [...]time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour}
	return delays[max(0, min(attempt-1, len(delays)-1))]
}

func (s *Store) Defer(ctx context.Context, job worker.Job, code string, until time.Time, noAttempt bool) error {
	if until.IsZero() {
		return memory.ErrInvalid
	}
	decrement := 0
	if noAttempt {
		decrement = 1
	}
	tag, err := s.pool.Exec(ctx, `UPDATE memory_jobs SET state='queued',error_code=$3,available_at=$4,attempts=greatest(0,attempts-$5),
        lease_until=NULL,lease_token=NULL,updated_at=now() WHERE id=$1 AND lease_token=$2 AND state='leased' AND lease_until>clock_timestamp()`,
		string(job.ID), string(job.LeaseToken), code, until, decrement)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return worker.ErrLeaseLost
	}
	return nil
}

func (s *Store) expireExhaustedJobs(ctx context.Context) error {
	rows, err := s.pool.Query(ctx, `SELECT id::text,owner_id::text,record_id::text,record_version,stage,lease_token::text FROM memory_jobs
        WHERE state='leased' AND lease_until<now() AND attempts >= $1`, maxAttempts)
	if err != nil {
		return err
	}
	var jobs []worker.Job
	for rows.Next() {
		var j worker.Job
		if err := rows.Scan(&j.ID, &j.OwnerID, &j.Record.ID, &j.Record.Version, &j.Stage, &j.LeaseToken); err != nil {
			rows.Close()
			return err
		}
		jobs = append(jobs, j)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, j := range jobs {
		if err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
			if strings.HasPrefix(j.Stage, "source.extract") {
				if err := extractionOwnerLock(ctx, tx, j.OwnerID); err != nil {
					return err
				}
			}
			tag, err := tx.Exec(ctx, `UPDATE memory_jobs SET state='failed',lease_until=NULL,lease_token=NULL,error_code='attempts_exhausted',updated_at=now()
                WHERE id=$1 AND lease_token=$2 AND state='leased' AND lease_until<now() AND attempts >= $3`, string(j.ID), string(j.LeaseToken), maxAttempts)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 1 && strings.HasPrefix(j.Stage, "source.extract") {
				return extractionStateTx(ctx, tx, j, "failed")
			}
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ProcessChunks(ctx context.Context, job worker.Job) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var valid bool
		err := tx.QueryRow(ctx, `SELECT true FROM memory_jobs WHERE id=$1 AND owner_id=$2 AND record_id=$3 AND record_version=$4
			AND lease_token=$5 AND state='leased' AND stage='source.chunk' AND lease_until>clock_timestamp() FOR UPDATE`,
			string(job.ID), string(job.OwnerID), string(job.Record.ID), job.Record.Version, string(job.LeaseToken)).Scan(&valid)
		if errors.Is(err, pgx.ErrNoRows) {
			return worker.ErrLeaseLost
		}
		if err != nil {
			return err
		}
		var body string
		err = tx.QueryRow(ctx, `SELECT v.body FROM source_versions v
			JOIN memory_records r ON (r.owner_id,r.id)=(v.owner_id,v.source_id)
			JOIN record_versions rv ON (rv.owner_id,rv.record_id,rv.version)=(v.owner_id,v.source_id,v.version)
			WHERE v.owner_id=$1 AND v.source_id=$2 AND v.version=$3 AND r.state='active' AND rv.state='active' FOR SHARE OF r,rv`,
			string(job.OwnerID), string(job.Record.ID), job.Record.Version).Scan(&body)
		if err != nil {
			return err
		}
		for _, chunk := range memory.SplitText(body, 1200, 160) {
			var exists bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM chunks WHERE owner_id=$1 AND source_id=$2 AND source_version=$3 AND ordinal=$4)`, string(job.OwnerID), string(job.Record.ID), job.Record.Version, chunk.Ordinal).Scan(&exists); err != nil {
				return err
			}
			if exists {
				continue
			}
			id := string(memory.NewID())
			if _, err := tx.Exec(ctx, "INSERT INTO memory_records(owner_id,id,kind,version) VALUES($1,$2,'chunk',1)", string(job.OwnerID), id); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, "INSERT INTO record_versions(owner_id,record_id,version) VALUES($1,$2,1)", string(job.OwnerID), id); err != nil {
				return err
			}
			// search_text remains empty until a tokenizer is configured. Do not
			// misrepresent an unsegmented Chinese string as a working FTS index.
			if _, err := tx.Exec(ctx, `INSERT INTO chunks(owner_id,id,version,source_id,source_version,ordinal,start_rune,end_rune,body)
				VALUES($1,$2,1,$3,$4,$5,$6,$7,$8)`, string(job.OwnerID), id, string(job.Record.ID), job.Record.Version, chunk.Ordinal, chunk.StartRune, chunk.EndRune, chunk.Text); err != nil {
				return err
			}
		}
		for _, stage := range []string{"source.extract", "source.embed", "source.tokenize"} {
			if err := enqueue(ctx, tx, job.OwnerID, job.Record.ID, job.Record.Version, stage); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE memory_jobs SET priority=10 WHERE owner_id=$1 AND record_id=$2 AND record_version=$3 AND stage='source.extract'
            AND EXISTS(SELECT 1 FROM archive_entries WHERE owner_id=$1 AND source_id=$2 AND source_version=$3)`, string(job.OwnerID), string(job.Record.ID), job.Record.Version); err != nil {
			return err
		}

		tag, err := tx.Exec(ctx, `UPDATE memory_jobs SET state='done',lease_until=NULL,lease_token=NULL,error_code='',updated_at=now()
			WHERE id=$1 AND lease_token=$2 AND lease_until>clock_timestamp()`, string(job.ID), string(job.LeaseToken))
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return worker.ErrLeaseLost
		}
		return nil
	})
}

const claimWhole = `WITH candidate AS (
		SELECT j.id,r.kind,
          CASE WHEN EXISTS(SELECT 1 FROM import_batches b WHERE b.owner_id=j.owner_id AND b.archive_id=j.record_id)
            OR EXISTS(SELECT 1 FROM archive_entries ae WHERE ae.owner_id=j.owner_id AND ae.source_id=j.record_id AND ae.source_version=j.record_version)
            OR EXISTS(SELECT 1 FROM episode_members em JOIN archive_entries ae
              ON (ae.owner_id,ae.source_id,ae.source_version)=(em.owner_id,em.member_id,em.member_version)
              WHERE em.owner_id=j.owner_id AND em.episode_id=j.record_id AND em.episode_version=j.record_version)
          THEN greatest(j.priority,10) ELSE j.priority END AS dispatch_priority
        FROM memory_jobs j JOIN memory_records r ON (r.owner_id,r.id)=(j.owner_id,j.record_id)
		LEFT JOIN source_contexts own ON(own.owner_id,own.source_id,own.source_version)=(j.owner_id,j.record_id,j.record_version)
		WHERE ((j.state='queued' AND j.available_at<=now()) OR (j.state='leased' AND j.lease_until<now() AND j.attempts<$3))
        AND NOT EXISTS (SELECT 1 FROM archive_entries ae JOIN import_batches ib ON (ib.owner_id,ib.archive_id)=(ae.owner_id,ae.archive_id)
            WHERE ae.owner_id=j.owner_id AND ae.source_id=j.record_id
              AND (ib.state='paused' OR (ib.hold_organizing AND (j.stage='source.extract' OR j.stage LIKE 'source.extract:%'))))
		-- A conversation must be fully stored before being organized. Pause/hold
		-- applies to every message in it, including a segment anchored in another batch.
		AND (j.stage NOT LIKE 'source.extract%' OR coalesce(own.conversation_key,'')='' OR NOT EXISTS (
		 SELECT 1 FROM source_contexts sibling
		 JOIN archive_entries ae ON(ae.owner_id,ae.source_id,ae.source_version)=(sibling.owner_id,sibling.source_id,sibling.source_version)
		 JOIN import_batches ib ON(ib.owner_id,ib.archive_id)=(ae.owner_id,ae.archive_id)
		 WHERE sibling.owner_id=own.owner_id AND sibling.conversation_key=own.conversation_key
		 AND (ib.stored<>ib.total OR ib.state='paused' OR ib.hold_organizing)))
		-- Per-message extraction jobs coalesce into a single sequential family.
		-- Completed/failed segment identities deduplicate retries in enqueue;
		-- a failed old manifest must not prevent processing a new source version.
		AND (j.stage NOT LIKE 'source.extract%' OR j.stage LIKE 'source.extract:conversation:%' OR coalesce(own.conversation_key,'')='' OR NOT EXISTS (
		 SELECT 1 FROM source_contexts sibling
		 JOIN memory_jobs family ON(family.owner_id,family.record_id,family.record_version)=(sibling.owner_id,sibling.source_id,sibling.source_version)
		 JOIN memory_records live ON(live.owner_id,live.id,live.version)=(family.owner_id,family.record_id,family.record_version) AND live.state='active'
		 WHERE sibling.owner_id=own.owner_id AND sibling.conversation_key=own.conversation_key
		 AND family.stage LIKE 'source.extract:conversation:%' AND family.state IN('queued','leased')))
		ORDER BY dispatch_priority,j.available_at,j.created_at,j.id FOR UPDATE OF j SKIP LOCKED LIMIT 1
	) UPDATE memory_jobs j SET priority=c.dispatch_priority,state='leased',attempts=j.attempts+1,lease_until=now()+$1*interval '1 second',lease_token=$2,updated_at=now()
	FROM candidate c WHERE j.id=c.id RETURNING j.id::text,j.owner_id::text,j.record_id::text,j.record_version,j.stage,j.attempts,j.lease_token::text,c.kind`

const claimWindowed = `WITH ready AS MATERIALIZED (
		-- The head of the queue is taken per kind of work. Per-message organizing
		-- jobs of an imported conversation wait for that conversation's own job
		-- and can fill the front of the queue by the thousand; taking the first
		-- few of each kind keeps something claimable in view.
		(SELECT id FROM memory_jobs WHERE state='queued' AND available_at<=now() AND stage NOT LIKE 'source.extract%' ORDER BY priority,available_at,created_at,id LIMIT 300)
		UNION ALL
		(SELECT id FROM memory_jobs WHERE state='queued' AND available_at<=now() AND stage LIKE 'source.extract:conversation:%' ORDER BY priority,available_at,created_at,id LIMIT 100)
		UNION ALL
		(SELECT id FROM memory_jobs WHERE state='queued' AND available_at<=now() AND stage='source.extract' ORDER BY priority,available_at,created_at,id LIMIT 100)
		UNION ALL
		(SELECT id FROM memory_jobs WHERE state='leased' AND lease_until<now() AND attempts<$3 ORDER BY lease_until LIMIT 100)
	), candidate AS (
		SELECT j.id,r.kind,
          CASE WHEN EXISTS(SELECT 1 FROM import_batches b WHERE b.owner_id=j.owner_id AND b.archive_id=j.record_id)
            OR EXISTS(SELECT 1 FROM archive_entries ae WHERE ae.owner_id=j.owner_id AND ae.source_id=j.record_id AND ae.source_version=j.record_version)
            OR EXISTS(SELECT 1 FROM episode_members em JOIN archive_entries ae
              ON (ae.owner_id,ae.source_id,ae.source_version)=(em.owner_id,em.member_id,em.member_version)
              WHERE em.owner_id=j.owner_id AND em.episode_id=j.record_id AND em.episode_version=j.record_version)
          THEN greatest(j.priority,10) ELSE j.priority END AS dispatch_priority
        FROM memory_jobs j JOIN memory_records r ON (r.owner_id,r.id)=(j.owner_id,j.record_id)
		LEFT JOIN source_contexts own ON(own.owner_id,own.source_id,own.source_version)=(j.owner_id,j.record_id,j.record_version)
		WHERE j.id IN (SELECT id FROM ready) AND ((j.state='queued' AND j.available_at<=now()) OR (j.state='leased' AND j.lease_until<now() AND j.attempts<$3))
        AND NOT EXISTS (SELECT 1 FROM archive_entries ae JOIN import_batches ib ON (ib.owner_id,ib.archive_id)=(ae.owner_id,ae.archive_id)
            WHERE ae.owner_id=j.owner_id AND ae.source_id=j.record_id
              AND (ib.state='paused' OR (ib.hold_organizing AND (j.stage='source.extract' OR j.stage LIKE 'source.extract:%'))))
		-- A conversation must be fully stored before being organized. Pause/hold
		-- applies to every message in it, including a segment anchored in another batch.
		AND (j.stage NOT LIKE 'source.extract%' OR coalesce(own.conversation_key,'')='' OR NOT EXISTS (
		 SELECT 1 FROM source_contexts sibling
		 JOIN archive_entries ae ON(ae.owner_id,ae.source_id,ae.source_version)=(sibling.owner_id,sibling.source_id,sibling.source_version)
		 JOIN import_batches ib ON(ib.owner_id,ib.archive_id)=(ae.owner_id,ae.archive_id)
		 WHERE sibling.owner_id=own.owner_id AND sibling.conversation_key=own.conversation_key
		 AND (ib.stored<>ib.total OR ib.state='paused' OR ib.hold_organizing)))
		-- Per-message extraction jobs coalesce into a single sequential family.
		-- Completed/failed segment identities deduplicate retries in enqueue;
		-- a failed old manifest must not prevent processing a new source version.
		AND (j.stage NOT LIKE 'source.extract%' OR j.stage LIKE 'source.extract:conversation:%' OR coalesce(own.conversation_key,'')='' OR NOT EXISTS (
		 SELECT 1 FROM source_contexts sibling
		 JOIN memory_jobs family ON(family.owner_id,family.record_id,family.record_version)=(sibling.owner_id,sibling.source_id,sibling.source_version)
		 JOIN memory_records live ON(live.owner_id,live.id,live.version)=(family.owner_id,family.record_id,family.record_version) AND live.state='active'
		 WHERE sibling.owner_id=own.owner_id AND sibling.conversation_key=own.conversation_key
		 AND family.stage LIKE 'source.extract:conversation:%' AND family.state IN('queued','leased')))
		ORDER BY dispatch_priority,j.available_at,j.created_at,j.id FOR UPDATE OF j SKIP LOCKED LIMIT 1
	) UPDATE memory_jobs j SET priority=c.dispatch_priority,state='leased',attempts=j.attempts+1,lease_until=now()+$1*interval '1 second',lease_token=$2,updated_at=now()
	FROM candidate c WHERE j.id=c.id RETURNING j.id::text,j.owner_id::text,j.record_id::text,j.record_version,j.stage,j.attempts,j.lease_token::text,c.kind`
