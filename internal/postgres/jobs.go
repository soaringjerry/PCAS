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

// These stages keep paid snapshots and retry until success or changed inputs.
func persistentBackgroundStage(stage string) bool {
	_, ok := backgroundHourlyBudgets[backgroundStage(stage)]
	return ok
}

const persistentJobStageSQL = "j.stage ~ '^memory\\.(organize|compare|entity_compare|entity_candidates|handover|project_handover|effort|topic_project):'"
const persistentStageSQL = "stage ~ '^memory\\.(organize|compare|entity_compare|entity_candidates|handover|project_handover|effort|topic_project):'"

const maxAttempts = 5

func (s *Store) Claim(ctx context.Context, lease time.Duration) (*worker.Job, error) {
	return s.claim(ctx, lease, false)
}

// ClaimIndex reserves indexing capacity independently of long model calls.
// Both lanes use the same lease fencing, pause and fresh-input rules.
func (s *Store) ClaimIndex(ctx context.Context, lease time.Duration) (*worker.Job, error) {
	return s.claim(ctx, lease, true)
}

func (s *Store) claim(ctx context.Context, lease time.Duration, indexOnly bool) (*worker.Job, error) {
	if err := s.warnLongDeferrals(ctx); err != nil {
		return nil, err
	}
	if lease <= 0 {
		return nil, memory.ErrInvalid
	}
	if err := s.expireExhaustedJobs(ctx); err != nil {
		return nil, err
	}
	var err error
	var job worker.Job
	var id, ownerID, recordID, token, kind string
	// Priorities are settled at enqueue. If a page is entirely paused or
	// coalesced, advance by its ordering key rather than sorting the whole queue.
	leaseToken := string(memory.NewID())
	query, cursorQuery := claimWindowed, claimFirstCursor
	nextQuery, nextCursorQuery := claimNextWindow, claimNextCursor
	if indexOnly {
		query, cursorQuery = claimIndexWindowed, claimIndexFirstCursor
		nextQuery, nextCursorQuery = claimIndexNextWindow, claimIndexNextCursor
	}
	args := []any{lease.Seconds(), leaseToken, maxAttempts}
	for {
		err = s.pool.QueryRow(ctx, query, args...).Scan(&id, &ownerID, &recordID, &job.Record.Version, &job.Stage, &job.Attempts, &token, &kind)
		if !errors.Is(err, pgx.ErrNoRows) {
			break
		}
		var priority int
		var available, created time.Time
		var lastID string
		cursorArgs := []any{maxAttempts}
		if len(args) > 3 {
			cursorArgs = append(cursorArgs, args[3:]...)
		}
		err = s.pool.QueryRow(ctx, cursorQuery, cursorArgs...).Scan(&priority, &available, &created, &lastID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		query, cursorQuery = nextQuery, nextCursorQuery
		args = []any{lease.Seconds(), leaseToken, maxAttempts, priority, available, created, lastID}
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
	if job.Attempts >= maxAttempts && !persistentBackgroundStage(job.Stage) {
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
        WHERE state='leased' AND lease_until<now() AND attempts >= $1 AND NOT (`+persistentStageSQL+`)`, maxAttempts)
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

// Keep the whole-queue form as a test oracle for dispatch order. Production
// only uses indexed pages and never invokes this unbounded eligibility query.
const claimWhole = `WITH ready AS (SELECT id FROM memory_jobs), candidate AS (` + claimCandidate + claimFinish

const claimCandidate = `SELECT j.id,r.kind,j.priority AS dispatch_priority
        FROM memory_jobs j JOIN memory_records r ON (r.owner_id,r.id)=(j.owner_id,j.record_id)
		LEFT JOIN source_contexts own ON(own.owner_id,own.source_id,own.source_version)=(j.owner_id,j.record_id,j.record_version)
		WHERE j.id IN (SELECT id FROM ready) AND ((j.state='queued' AND j.available_at<=now()) OR (j.state='leased' AND j.lease_until<now() AND (j.attempts<$3 OR ` + persistentJobStageSQL + `)))
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
		 AND family.stage LIKE 'source.extract:conversation:%' AND family.state IN('queued','leased')))`
const claimFinish = `
		ORDER BY dispatch_priority,
 CASE WHEN j.priority=8 AND ((j.stage LIKE 'memory.card:%' AND (SELECT prefer_cards FROM background_dispatch))
 OR ((j.stage LIKE 'memory.compare:%' OR j.stage LIKE 'memory.entity_compare:%') AND NOT (SELECT prefer_cards FROM background_dispatch))) THEN 0 ELSE 1 END,
 j.available_at,j.created_at,j.id FOR UPDATE OF j SKIP LOCKED LIMIT 1
	), claimed AS (UPDATE memory_jobs j SET priority=c.dispatch_priority,state='leased',attempts=j.attempts+1,lease_until=now()+$1*interval '1 second',lease_token=$2,updated_at=now()
	FROM candidate c WHERE j.id=c.id RETURNING j.id,j.owner_id,j.record_id,j.record_version,j.stage,j.attempts,j.lease_token,c.kind,j.priority
 ), rotated AS (UPDATE background_dispatch SET prefer_cards=claimed.stage NOT LIKE 'memory.card:%'
 FROM claimed WHERE claimed.priority=8 AND (claimed.stage LIKE 'memory.card:%' OR claimed.stage LIKE 'memory.compare:%' OR claimed.stage LIKE 'memory.entity_compare:%') RETURNING background_dispatch.prefer_cards)
 SELECT id::text,owner_id::text,record_id::text,record_version,stage,attempts,lease_token::text,kind FROM claimed`

const claimFairReady = ` UNION (SELECT id FROM memory_jobs WHERE priority=8 AND stage LIKE 'memory.card:%' AND (SELECT prefer_cards FROM background_dispatch)
 AND state IN('queued','leased') AND ((state='queued' AND available_at<=now()) OR (state='leased' AND lease_until<now() AND (attempts<$3 OR ` + persistentStageSQL + `))) ORDER BY priority,available_at,created_at,id LIMIT 500)
 UNION (SELECT id FROM memory_jobs WHERE priority=8 AND (stage LIKE 'memory.compare:%' OR stage LIKE 'memory.entity_compare:%') AND NOT (SELECT prefer_cards FROM background_dispatch)
 AND state IN('queued','leased') AND ((state='queued' AND available_at<=now()) OR (state='leased' AND lease_until<now() AND (attempts<$3 OR ` + persistentStageSQL + `))) ORDER BY priority,available_at,created_at,id LIMIT 500)`

const claimWindowed = `WITH ready AS MATERIALIZED ((SELECT id FROM memory_jobs WHERE state IN ('queued','leased') AND ((state='queued' AND available_at<=now()) OR (state='leased' AND lease_until<now() AND (attempts<$3 OR ` + persistentStageSQL + `))) ORDER BY priority,available_at,created_at,id LIMIT 500)` + claimFairReady + `), candidate AS (` + claimCandidate + claimFinish
const claimNextWindow = `WITH ready AS MATERIALIZED ((SELECT id FROM memory_jobs WHERE state IN ('queued','leased') AND ((state='queued' AND available_at<=now()) OR (state='leased' AND lease_until<now() AND (attempts<$3 OR ` + persistentStageSQL + `))) AND (priority,available_at,created_at,id)>($4,$5,$6,$7::uuid) ORDER BY priority,available_at,created_at,id LIMIT 500)` + claimFairReady + `), candidate AS (` + claimCandidate + claimFinish

const claimCursorBase = `SELECT priority,available_at,created_at,id::text FROM memory_jobs WHERE state IN ('queued','leased') AND ((state='queued' AND available_at<=now()) OR (state='leased' AND lease_until<now() AND (attempts<$1 OR ` + persistentStageSQL + `)))`
const claimFirstCursor = claimCursorBase + ` ORDER BY priority,available_at,created_at,id OFFSET 499 LIMIT 1`
const claimNextCursor = claimCursorBase + ` AND (priority,available_at,created_at,id)>($2,$3,$4,$5::uuid) ORDER BY priority,available_at,created_at,id OFFSET 499 LIMIT 1`

// This predicate matches jobs_index_dispatch_ready_idx; the index lane must
// not walk all waiting extraction entrances before finding an index job.
const claimIndexStages = `(stage IN ('source.embed','source.tokenize','memory.embed','memory.index') OR stage LIKE 'memory.embed:%')`

var claimIndexWindowed = strings.Replace(strings.Replace(claimWindowed, claimFairReady, "", 1), "WHERE state IN", "WHERE "+claimIndexStages+" AND state IN", 1)
var claimIndexNextWindow = strings.Replace(strings.Replace(claimNextWindow, claimFairReady, "", 1), "WHERE state IN", "WHERE "+claimIndexStages+" AND state IN", 1)

const claimIndexCursorBase = claimCursorBase + ` AND ` + claimIndexStages
const claimIndexFirstCursor = claimIndexCursorBase + ` ORDER BY priority,available_at,created_at,id OFFSET 499 LIMIT 1`
const claimIndexNextCursor = claimIndexCursorBase + ` AND (priority,available_at,created_at,id)>($2,$3,$4,$5::uuid) ORDER BY priority,available_at,created_at,id OFFSET 499 LIMIT 1`
