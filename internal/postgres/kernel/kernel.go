// Package kernel holds what every storage package of the PostgreSQL adapter
// shares: the encoding of a document, the checks of a request, the reads of
// document rows, and the lease of a queue job. It holds no business rule and
// no table of one domain.
package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/worker"
)

// JSON encodes a value that is known to encode. An error gives nil.
func JSON(v any) []byte { b, _ := json.Marshal(v); return b }

// RequireOwner returns memory.ErrForbidden when the scope is not the owner's.
func RequireOwner(scope memory.Scope) error {
	if !scope.Valid() || !scope.IsOwner {
		return memory.ErrForbidden
	}
	return nil
}

// OneOf reports whether value is one of values.
func OneOf(value string, values ...string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}

// RequireText returns memory.ErrInvalid for an empty text and for a text
// above one mebibyte.
func RequireText(v string) error {
	if strings.TrimSpace(v) == "" || len(v) > 1<<20 {
		return memory.ErrInvalid
	}
	return nil
}

// NullString gives SQL NULL for an empty string.
func NullString(v string) any {
	if v == "" {
		return nil
	}
	return v
}

// Stamp is the current time in UTC as a document stores it.
func Stamp() string { return time.Now().UTC().Format(time.RFC3339Nano) }

// QueryDocument reads one JSON document. No row gives memory.ErrNotFound.
func QueryDocument[T any](ctx context.Context, tx pgx.Tx, sql string, args ...any) (T, error) {
	var result T
	var data []byte
	err := tx.QueryRow(ctx, sql, args...).Scan(&data)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, memory.ErrNotFound
	}
	if err != nil {
		return result, err
	}
	err = json.Unmarshal(data, &result)
	return result, err
}

// QueryDocuments reads the JSON documents of all rows.
func QueryDocuments[T any](ctx context.Context, tx pgx.Tx, sql string, args ...any) ([]T, error) {
	out := []T{}
	rows, err := tx.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var data []byte
		var v T
		if err = rows.Scan(&data); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(data, &v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// UniqueRefs keeps the first occurrence of each exact reference. Dependency
// lists are sets: history and recall can name the same memory many times.
func UniqueRefs(refs []memory.Ref) []memory.Ref {
	seen := make(map[memory.Ref]bool, len(refs))
	out := make([]memory.Ref, 0, len(refs))
	for _, ref := range refs {
		if !seen[ref] {
			seen[ref] = true
			out = append(out, ref)
		}
	}
	return out
}

// LockJob locks the job's row in the transaction and checks its lease. A
// lost lease gives worker.ErrLeaseLost.
func LockJob(ctx context.Context, tx pgx.Tx, j worker.Job) error {
	var valid bool
	// The same statement names this job as the cause of the work that the
	// transaction queues. The queue records the root and the depth from it.
	err := tx.QueryRow(ctx, `SELECT set_config('pcas.cause_job',id::text,true) IS NOT NULL FROM memory_jobs WHERE id=$1 AND owner_id=$2 AND record_id=$3 AND record_version=$4 AND lease_token=$5 AND state='leased' AND lease_until>clock_timestamp() FOR UPDATE`, string(j.ID), string(j.OwnerID), string(j.Record.ID), j.Record.Version, string(j.LeaseToken)).Scan(&valid)
	if errors.Is(err, pgx.ErrNoRows) {
		return worker.ErrLeaseLost
	}
	return err
}

// Acknowledge finishes a leased job. A lost lease gives worker.ErrLeaseLost.
func Acknowledge(ctx context.Context, tx pgx.Tx, j worker.Job) error {
	tag, err := tx.Exec(ctx, "UPDATE memory_jobs SET state='done',lease_until=NULL,lease_token=NULL,error_code='',updated_at=now() WHERE id=$1 AND lease_token=$2 AND state='leased' AND lease_until>clock_timestamp()", string(j.ID), string(j.LeaseToken))
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return worker.ErrLeaseLost
	}
	return nil
}

// Enqueue queues one stage for a record version. A queued stage is not
// queued again.
func Enqueue(ctx context.Context, tx pgx.Tx, ownerID, id memory.ID, version int, stage string) error {
	_, err := tx.Exec(ctx, `INSERT INTO memory_jobs(id,owner_id,record_id,record_version,stage) VALUES($1,$2,$3,$4,$5)
		ON CONFLICT(owner_id,record_id,record_version,stage) DO NOTHING`, string(memory.NewID()), string(ownerID), string(id), version, stage)
	return err
}

// RetryDelay is the wait before the next attempt of a job: one minute, five
// minutes, fifteen minutes, then one hour.
func RetryDelay(attempt int) time.Duration {
	delays := [...]time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour}
	return delays[max(0, min(attempt-1, len(delays)-1))]
}

// OwnerLock locks the owner's workspace row until the transaction ends.
func OwnerLock(ctx context.Context, tx pgx.Tx, owner memory.ID) error {
	_, err := tx.Exec(ctx, "SELECT 1 FROM workspace_owners WHERE owner_id=$1 FOR UPDATE", string(owner))
	return err
}
