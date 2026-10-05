package postgres

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

var errSecretaryBusy = errors.New("secretary turn busy")

type secretaryTicket struct {
	conversation string
	creator      string
	own          bool
	legacy       bool
}

func secretaryConversationKey(owner, conversation string) string {
	return strings.ToLower("secretary-conversation:" + owner + ":" + conversation)
}

func secretaryTryLock(ctx context.Context, tx pgx.Tx, key string) error {
	var locked bool
	if err := tx.QueryRow(ctx, "SELECT pg_try_advisory_xact_lock(hashtextextended($1,0))", strings.ToLower(key)).Scan(&locked); err != nil {
		return err
	}
	if !locked {
		return errSecretaryBusy
	}
	return nil
}

func secretaryPause(ctx, callerCtx context.Context) error {
	timer := time.NewTimer(25 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-callerCtx.Done():
		return callerCtx.Err()
	case <-ctx.Done():
		return ctx.Err()
	}
}

// A successful commit is the common acceptance point. The separate, short
// admission lock serializes allocation AND commit, without waiting for models.
// Sequence allocation alone would not establish commit order across instances.
func (s *Store) admitSecretaryTurn(ctx, callerCtx context.Context, owner, request, conversation string, hash []byte) (secretaryTicket, error) {
	ticket := secretaryTicket{conversation: strings.ToLower(conversation), creator: string(memory.NewID())}
	deadline, _ := ctx.Deadline()
	for {
		if err := callerCtx.Err(); err != nil {
			return ticket, err
		}
		err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
			if err := lockSmokeTurnTx(ctx, tx, owner); err != nil {
				return err
			}
			if err := secretaryTryLock(ctx, tx, "secretary-admission-request:"+owner+":"+request); err != nil {
				return err
			}
			var smokeRequest bool
			if smokeID(ctx) == "" {
				if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM desk_smoke_requests WHERE owner_id=$1 AND request_id=$2)", owner, request).Scan(&smokeRequest); err != nil {
					return err
				}
				if smokeRequest {
					return memory.ErrConflict
				}
			}
			// Existing completed exchanges, including scrubbed history, take priority.
			// Never recreate their source or invent a migration-era admission order.
			var priorHash []byte
			err := tx.QueryRow(ctx, "SELECT request_hash,conversation_id::text FROM desk_turns WHERE owner_id=$1 AND request_id=$2", owner, request).Scan(&priorHash, &ticket.conversation)
			if err == nil {
				if !bytes.Equal(hash, priorHash) {
					return memory.ErrConflict
				}
				ticket.legacy = true
				return nil
			}
			if !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
			var creator string
			err = tx.QueryRow(ctx, "SELECT request_hash,conversation_id::text,creator_id::text FROM desk_turn_order WHERE owner_id=$1 AND request_id=$2", owner, request).Scan(&priorHash, &ticket.conversation, &creator)
			if err == nil {
				if !bytes.Equal(hash, priorHash) {
					return memory.ErrConflict
				}
				ticket.own = creator == ticket.creator
				return nil
			}
			if !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
			if err := secretaryTryLock(ctx, tx, "secretary-admission-conversation:"+owner+":"+ticket.conversation); err != nil {
				return err
			}
			if smokeID(ctx) == "" {
				if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM desk_smoke_groups WHERE owner_id=$1 AND id=$2)", owner, ticket.conversation).Scan(&smokeRequest); err != nil {
					return err
				}
				if smokeRequest {
					return memory.ErrConflict
				}
			}
			if err := callerCtx.Err(); err != nil {
				return err
			}
			_, err = tx.Exec(ctx, "INSERT INTO desk_turn_order(owner_id,request_id,conversation_id,request_hash,creator_id,expires_at) VALUES($1,$2,$3,$4,$5,$6)", owner, request, ticket.conversation, hash, ticket.creator, deadline)
			ticket.own = err == nil
			return err
		})
		if !errors.Is(err, errSecretaryBusy) {
			return ticket, err
		}
		if err := secretaryPause(ctx, callerCtx); err != nil {
			return ticket, err
		}
	}
}

// A dead creator has no stored request body to resume. Its fixed persistence
// deadline expires the ticket. Never expire a head while its transaction still
// owns the conversation lock: it may yet commit a real result.
func (s *Store) expireSecretaryTickets(ctx context.Context, owner, conversation string) error {
	var expired bool
	if err := s.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM desk_turn_order WHERE owner_id=$1 AND conversation_id=$2 AND status='pending' AND expires_at<=clock_timestamp())", owner, conversation).Scan(&expired); err != nil {
		return err
	}
	if !expired {
		return nil
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := secretaryTryLock(ctx, tx, secretaryConversationKey(owner, conversation)); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, "UPDATE desk_turn_order SET status='expired' WHERE owner_id=$1 AND conversation_id=$2 AND status='pending' AND expires_at<=clock_timestamp()", owner, conversation)
		return err
	})
	if errors.Is(err, errSecretaryBusy) {
		return nil
	}
	return err
}

func (s *Store) secretaryTicketReady(ctx context.Context, owner, request string, ticket secretaryTicket) (bool, error) {
	if ticket.legacy {
		return true, nil
	}
	if err := s.expireSecretaryTickets(ctx, owner, ticket.conversation); err != nil {
		return false, err
	}
	var ready bool
	err := s.pool.QueryRow(ctx, `SELECT status<>'pending' OR ($3 AND NOT EXISTS(
 SELECT 1 FROM desk_turn_order earlier WHERE earlier.owner_id=t.owner_id AND earlier.conversation_id=t.conversation_id AND earlier.status='pending' AND earlier.admission_order<t.admission_order))
 FROM desk_turn_order t WHERE owner_id=$1 AND request_id=$2`, owner, request, ticket.own).Scan(&ready)
	return ready, err
}

// Only the head competes for a generation slot. Other tickets release their
// short-query connections before waiting; neither slots nor owner locks are
// held by the queue. The transaction rechecks eligibility after both locks.
func (s *Store) withOrderedSecretaryTurn(ctx, callerCtx context.Context, owner, request string, ticket secretaryTicket, work func(pgx.Tx, bool) error) (err error) {
	defer func() {
		if err == nil || !ticket.own || ticket.legacy {
			return
		}
		status := "failed"
		if callerCtx.Err() != nil {
			status = "canceled"
		}
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		// Failure to clean up (e.g. database outage) is bounded by expires_at.
		_, _ = s.pool.Exec(cleanupCtx, "UPDATE desk_turn_order SET status=$3 WHERE owner_id=$1 AND request_id=$2 AND creator_id=$4 AND status='pending'", owner, request, status, ticket.creator)
	}()
	for {
		if err := callerCtx.Err(); err != nil {
			return err
		}
		ready, err := s.secretaryTicketReady(ctx, owner, request, ticket)
		if err != nil {
			return err
		}
		if !ready {
			if err := secretaryPause(ctx, callerCtx); err != nil {
				return err
			}
			continue
		}
		select {
		case s.secretarySlots <- struct{}{}:
		case <-callerCtx.Done():
			return callerCtx.Err()
		case <-ctx.Done():
			return ctx.Err()
		}
		err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
			if err := lockSmokeTurnTx(ctx, tx, owner); err != nil {
				return err
			}
			for _, key := range []string{owner + ":" + request, secretaryConversationKey(owner, ticket.conversation)} {
				if err := secretaryTryLock(ctx, tx, key); err != nil {
					return err
				}
			}
			captureOnly := false
			if !ticket.legacy {
				var status string
				var head, expired bool
				if err := tx.QueryRow(ctx, `SELECT status,expires_at<=clock_timestamp(),NOT EXISTS(
 SELECT 1 FROM desk_turn_order earlier WHERE earlier.owner_id=t.owner_id AND earlier.conversation_id=t.conversation_id AND earlier.status='pending' AND earlier.admission_order<t.admission_order)
 FROM desk_turn_order t WHERE owner_id=$1 AND request_id=$2 FOR UPDATE`, owner, request).Scan(&status, &expired, &head); err != nil {
					return err
				}
				if status == "pending" {
					if !ticket.own || !head {
						return errSecretaryBusy
					}
					if expired {
						if _, err := tx.Exec(ctx, "UPDATE desk_turn_order SET status='expired' WHERE owner_id=$1 AND request_id=$2", owner, request); err != nil {
							return err
						}
						captureOnly = true
					}
				} else {
					captureOnly = status != "done"
				}
			}
			if err := callerCtx.Err(); err != nil {
				return err
			}
			if err := work(tx, captureOnly); err != nil {
				return err
			}
			if ticket.legacy {
				return nil
			}
			_, err := tx.Exec(ctx, "UPDATE desk_turn_order SET status='done' WHERE owner_id=$1 AND request_id=$2", owner, request)
			return err
		})
		<-s.secretarySlots
		if !errors.Is(err, errSecretaryBusy) {
			return err
		}
		if err := secretaryPause(ctx, callerCtx); err != nil {
			return err
		}
	}
}

// Retired input must remain available without later extraction reviving old
// actions. Remove only this transaction's new, unpublished chunk job. Existing
// sources/jobs must not be silently changed, even on an identity collision.
func (s *Store) captureIncompleteSecretaryTurn(ctx context.Context, tx pgx.Tx, scope memory.Scope, request, text string) error {
	in, err := s.ingestTx(ctx, tx, scope, memory.IngestRequest{Connector: "desk-incomplete", ExternalID: request, ExternalVersion: "1", Title: "未完成的秘书原话", Text: text, MediaType: "text/plain"})
	if err != nil {
		return err
	}
	if in.Duplicate {
		return memory.ErrConflict
	}
	tag, err := tx.Exec(ctx, `DELETE FROM memory_jobs WHERE owner_id=$1 AND record_id=$2 AND record_version=$3 AND stage='source.chunk' AND state='queued' AND attempts=0`, string(scope.OwnerID), string(in.ID), in.Version)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return memory.ErrConflict
	}
	return saveCandidate(ctx, tx, scope, workspace.Candidate{ID: string(memory.NewID()), Kind: "unknown", Text: text, Confidence: 0, Source: workspace.SourceRef{SourceID: string(in.ID), Version: in.Version, Label: "未完成的秘书原话", Excerpt: text, At: stamp()}, State: "pending", CreatedAt: stamp()})
}
