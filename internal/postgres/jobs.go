package postgres

import (
	"context"
	"errors"
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
	// Exhausted jobs also terminate if all previous attempts crashed.
	_, err := s.pool.Exec(ctx, `UPDATE memory_jobs SET state='failed',lease_until=NULL,lease_token=NULL,error_code='attempts_exhausted',updated_at=now()
		WHERE state='leased' AND lease_until < now() AND attempts >= $1`, maxAttempts)
	if err != nil {
		return nil, err
	}
	var job worker.Job
	var id, ownerID, recordID, token, kind string
	err = s.pool.QueryRow(ctx, `WITH candidate AS (
		SELECT j.id,r.kind FROM memory_jobs j JOIN memory_records r ON (r.owner_id,r.id)=(j.owner_id,j.record_id)
		WHERE (j.state='queued' AND j.available_at<=now()) OR (j.state='leased' AND j.lease_until<now() AND j.attempts<$3)
		ORDER BY j.available_at,j.created_at FOR UPDATE OF j SKIP LOCKED LIMIT 1
	) UPDATE memory_jobs j SET state='leased',attempts=j.attempts+1,lease_until=now()+$1*interval '1 second',lease_token=$2,updated_at=now()
	FROM candidate c WHERE j.id=c.id RETURNING j.id::text,j.owner_id::text,j.record_id::text,j.record_version,j.stage,j.attempts,j.lease_token::text,c.kind`,
		lease.Seconds(), string(memory.NewID()), maxAttempts).Scan(&id, &ownerID, &recordID, &job.Record.Version, &job.Stage, &job.Attempts, &token, &kind)
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
	return s.finishAttempt(ctx, job, state, code, time.Duration(1<<min(job.Attempts, 6))*time.Second)
}

func (s *Store) finishAttempt(ctx context.Context, job worker.Job, state, code string, delay time.Duration) error {
	tag, err := s.pool.Exec(ctx, `UPDATE memory_jobs SET state=$3,error_code=$4,available_at=now()+$5*interval '1 second',lease_until=NULL,lease_token=NULL,updated_at=now()
		WHERE id=$1 AND lease_token=$2 AND state='leased' AND lease_until>clock_timestamp()`, string(job.ID), string(job.LeaseToken), state, code, delay.Seconds())
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return worker.ErrLeaseLost
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
