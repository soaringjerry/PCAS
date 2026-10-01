package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
)

// A missing or forbidden dependency at a fence means the previously assembled
// context changed. Provider/network errors are never passed through this gate.
func contextFenceError(err error) error {
	if errors.Is(err, memory.ErrNotFound) || errors.Is(err, memory.ErrForbidden) {
		return memory.ErrConflict
	}
	return err
}

// generateContext runs outside all business transactions. Adapter observation
// independently commits the exact final request before a provider barrier.
func (s *Store) generateContext(ctx context.Context, scope memory.Scope, operationID string, task memory.TrustedTaskContext, deps []memory.TypedDependency, entries []memory.EvidenceEntry, candidates []memory.CandidateRecord, indirect []memory.TypedDependency, system, prompt string, schema json.RawMessage) (ai.Result, memory.ContextAttempt, error) {
	// Freeze the assembled origin IDs before adapters/observers can yield.
	if err := appendTaskDeskActions(&task); err != nil {
		return ai.Result{}, memory.ContextAttempt{}, err
	}
	ctx, generationCancel := context.WithTimeout(ctx, 5*time.Minute)
	defer generationCancel()
	var attempt memory.ContextAttempt
	var payloadHash [32]byte
	var observerErr error
	bind := func(ctx context.Context, event memory.ContextRequestEvent) error {
		if event.ProviderID != task.Recipient.Provider || event.Protocol != task.Recipient.Protocol {
			return memory.ErrConflict
		}
		var live memory.Recipient
		err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
			var err error
			live, err = s.contextRecipientModelTx(ctx, tx, scope, task.Recipient.PrincipalID, task.Recipient.Role, nil, event.Model)
			return err
		})
		if err != nil {
			return err
		}
		if event.Model != live.Model {
			return memory.ErrConflict
		}
		p, ok := s.models.Get(live.Provider)
		if !ok {
			return memory.ErrUnavailable
		}
		if p.BaseURL != "" && event.Endpoint != "" && strings.TrimRight(event.Endpoint, "/") != strings.TrimRight(p.BaseURL, "/") {
			return memory.ErrConflict
		}
		if task.Recipient.Model == "unknown" && attempt.ID == "" {
			task.Recipient = live
		} else if live != task.Recipient {
			return memory.ErrConflict
		}
		if event.Stage == "prepared" {
			if attempt.ID != "" {
				return memory.ErrConflict
			}
			attempt, err = s.prepareContextAttempt(ctx, scope, operationID, task, deps, entries, candidates, indirect, event.Payload, event.ObservationLayer)
			if err != nil {
				return err
			}
			payloadHash = sha256.Sum256(event.Payload)
			return nil
		}
		if attempt.ID == "" || sha256.Sum256(event.Payload) != payloadHash {
			return memory.ErrConflict
		}
		if !oneOf(event.Stage, "before_dispatch", "dispatched") {
			return memory.ErrInvalid
		}
		err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
			if err := s.verifyContextAttemptTx(ctx, tx, scope, attempt.ID, task, deps); err != nil {
				return err
			}
			var tagErr error
			if event.Stage == "before_dispatch" {
				_, tagErr = tx.Exec(ctx, "UPDATE context_attempts SET dispatch_reserved_at=now() WHERE owner_id=$1 AND id=$2 AND state='prepared'", string(scope.OwnerID), string(attempt.ID))
			} else {
				tag, e := tx.Exec(ctx, "UPDATE context_attempts SET state='dispatched',dispatched_at=now() WHERE owner_id=$1 AND id=$2 AND state='prepared' AND dispatch_reserved_at IS NOT NULL", string(scope.OwnerID), string(attempt.ID))
				if e != nil {
					return e
				}
				if tag.RowsAffected() != 1 {
					return memory.ErrConflict
				}
			}
			if tagErr != nil {
				return tagErr
			}
			return recountContextMetadataTx(ctx, tx, scope.OwnerID)
		})
		if err == nil {
			now := time.Now().UTC()
			if event.Stage == "before_dispatch" {
				attempt.DispatchReservedAt = &now
			} else {
				attempt.State = memory.AttemptDispatched
				attempt.DispatchedAt = &now
			}
		}
		return err
	}
	workCtx := memory.WithContextRequestObserver(ctx, memory.ContextRequestObserverFunc(func(ctx context.Context, event memory.ContextRequestEvent) error {
		err := contextFenceError(bind(ctx, event))
		if err != nil {
			observerErr = err
		}
		return err
	}))
	var result ai.Result
	var generationErr error
	if len(schema) > 0 {
		if task.Recipient.Role == "secretary" {
			result, generationErr = s.models.GenerateWithSearchSchema(workCtx, task.Recipient.Provider, system, prompt, schema)
		} else {
			result, generationErr = s.models.GenerateSchema(workCtx, task.Recipient.Provider, system, prompt, schema)
		}
	} else {
		result, generationErr = s.models.Generate(workCtx, task.Recipient.Provider, system, prompt)
	}
	if observerErr != nil {
		generationErr = observerErr
	}
	if attempt.ID == "" {
		if generationErr == nil {
			generationErr = memory.ErrUnavailable
		}
		return ai.Result{}, attempt, generationErr
	}
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	state := memory.AttemptCompleted
	code := ""
	if generationErr != nil {
		state = memory.AttemptFailed
		code = "generation_failed"
		if attempt.DispatchedAt != nil {
			state = memory.AttemptOutcomeUnknown
			code = "external_outcome_unknown"
		}
	}
	finishErr := pgx.BeginFunc(finishCtx, s.pool, func(tx pgx.Tx) error {
		if generationErr == nil {
			live, e := s.contextRecipientModelTx(finishCtx, tx, scope, task.Recipient.PrincipalID, task.Recipient.Role, nil, task.Recipient.Model)
			if e != nil {
				return e
			}
			if live != task.Recipient {
				return memory.ErrConflict
			}
			if e = s.verifyContextAttemptTx(finishCtx, tx, scope, attempt.ID, task, deps); e != nil {
				return e
			}
		} else if e := lockContextDiagnosticsTx(finishCtx, tx, scope.OwnerID); e != nil {
			return e
		}
		tag, e := tx.Exec(finishCtx, "UPDATE context_attempts SET state=$3,error_code=$4,completed_at=now() WHERE owner_id=$1 AND id=$2 AND state<>'invalidated'", string(scope.OwnerID), string(attempt.ID), string(state), code)
		if e != nil {
			return e
		}
		if tag.RowsAffected() != 1 {
			return memory.ErrConflict
		}
		return recountContextMetadataTx(finishCtx, tx, scope.OwnerID)
	})
	if finishErr != nil {
		// Route/dependency changes can happen after a request was sent. Preserve
		// the independent trace, erase its body and reject the result.
		_ = pgx.BeginFunc(finishCtx, s.pool, func(tx pgx.Tx) error {
			if err := lockContextDiagnosticsTx(finishCtx, tx, scope.OwnerID); err != nil {
				return err
			}
			if _, err := tx.Exec(finishCtx, "UPDATE context_attempts SET state='invalidated',snapshot=NULL,snapshot_bytes=0,snapshot_state='revoked',error_code='context_changed',completed_at=now() WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), string(attempt.ID)); err != nil {
				return err
			}
			return recountContextMetadataTx(finishCtx, tx, scope.OwnerID)
		})
		attempt.State = memory.AttemptInvalidated
		attempt.SnapshotState = memory.SnapshotRevoked
		return ai.Result{}, attempt, contextFenceError(finishErr)
	}
	attempt.State = state
	now := time.Now().UTC()
	attempt.CompletedAt = &now
	if generationErr != nil {
		return ai.Result{}, attempt, generationErr
	}
	return result, attempt, nil
}

// Used is a model claim, independent of exact Input. No text is retained here.
func (s *Store) recordContextUsed(ctx context.Context, scope memory.Scope, id memory.ID, refs []memory.Ref) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := lockContextDiagnosticsTx(ctx, tx, scope.OwnerID); err != nil {
			return err
		}
		var raw []byte
		if err := tx.QueryRow(ctx, "SELECT manifest FROM context_attempts WHERE owner_id=$1 AND id=$2 AND metadata_expires_at>now() FOR UPDATE", string(scope.OwnerID), string(id)).Scan(&raw); err != nil {
			return err
		}
		var m memory.ContextManifest
		if err := json.Unmarshal(raw, &m); err != nil {
			return err
		}
		m.Used = []memory.UsedRecord{}
		for _, ref := range refs {
			if !ref.ID.Valid() || ref.Version < 1 || !memory.ContextKindSupported(ref.Kind) {
				m.Used = append(m.Used, memory.UsedRecord{Validation: "unknown"})
				continue
			}
			validation := "absent_from_input"
			for _, input := range m.Input {
				if input.Ref == ref {
					validation = "supported"
					break
				}
			}
			m.Used = append(m.Used, memory.UsedRecord{Ref: ref, Validation: validation})
		}
		if len(refs) > 256 {
			return memory.ErrRecordCapacity
		}
		data := asJSON(m)
		var size, total int64
		measure := func() error {
			return tx.QueryRow(ctx, `SELECT metadata_bytes-octet_length(manifest::text)+octet_length($3::jsonb::text),(SELECT coalesce(sum(metadata_bytes+$4),0) FROM context_attempts WHERE owner_id=$1)-octet_length(manifest::text)+octet_length($3::jsonb::text) FROM context_attempts WHERE owner_id=$1 AND id=$2`, string(scope.OwnerID), string(id), data, contextControlReserve).Scan(&size, &total)
		}
		if err := measure(); err != nil {
			return err
		}
		if size > memory.DefaultContextAttemptMetadataBytes-contextControlReserve || total > memory.DefaultContextOwnerMetadataBytes {
			m.Candidates = []memory.CandidateRecord{}
			m.Coverage.Complete = false
			m.Coverage.Gaps = append(m.Coverage.Gaps, "candidate_metadata_omitted")
			data = asJSON(m)
			if err := measure(); err != nil {
				return err
			}
		}
		if size > memory.DefaultContextAttemptMetadataBytes-contextControlReserve || total > memory.DefaultContextOwnerMetadataBytes {
			return memory.ErrRecordCapacity
		}
		if _, err := tx.Exec(ctx, "UPDATE context_attempts SET manifest=$3,metadata_bytes=greatest(metadata_bytes,octet_length($3::jsonb::text)+octet_length(recipient::text)) WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), string(id), data); err != nil {
			return err
		}
		return recountContextMetadataTx(ctx, tx, scope.OwnerID)
	})
}
