package usestore

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/contextbuilder/reader"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/postgres/kernel"
	"github.com/soaringjerry/PCAS/internal/postgres/memoryread"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// ReaderStore gives the memory readers the memories of a group.
func (s *Use) ReaderStore() reader.Store { return readerStore{s} }

type readerStore struct{ store *Use }

func (a readerStore) GroupMembers(ctx context.Context, scope memory.Scope, key string) ([]string, error) {
	ids := []string{}
	err := pgx.BeginTxFunc(ctx, a.store.Pool, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		var e error
		ids, e = kernel.QueryDocuments[string](ctx, tx, "SELECT to_jsonb(claim_id::text) FROM status_current_members WHERE owner_id=$1 AND key=$2 ORDER BY claim_id", string(scope.OwnerID), key)
		return e
	})
	return ids, err
}

func (a readerStore) Memories(ctx context.Context, scope memory.Scope, agent workspace.Agent, thing *string, ids []string) ([]workspace.Memory, error) {
	var ms []workspace.Memory
	err := pgx.BeginTxFunc(ctx, a.store.Pool, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		var e error
		ms, e = a.store.UseMemoriesTx(ctx, tx, scope, agent, thing, ids)
		return e
	})
	return ms, err
}

func (a readerStore) Overflow(ctx context.Context, scope memory.Scope, reason string, count int) {
	a.store.recordUseOverflow(ctx, scope, reason, count)
}

func (s *Use) DeputyUseContext(ctx context.Context, scope memory.Scope, run *workspace.Run) (UseContext, workspace.Agent, error) {
	var u UseContext
	var agent workspace.Agent
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		var err error
		agent, err = kernel.QueryDocument[workspace.Agent](ctx, tx, "SELECT document FROM workspace_agents WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), run.AgentID)
		if err != nil {
			return err
		}
		u, err = s.StartUseContextTx(ctx, tx, scope)
		if err != nil {
			return err
		}
		ms, err := s.ReadMemoriesTx(ctx, tx, memory.Scope{OwnerID: scope.OwnerID, PrincipalID: agent.ID}, true, memoryread.MemoryReadOptions{UseCurrent: true, IDs: run.ContextMemoryIDs})
		if err != nil {
			return err
		}
		settings, err := kernel.QueryDocument[workspace.Settings](ctx, tx, "SELECT settings FROM workspace_owners WHERE owner_id=$1", string(scope.OwnerID))
		if err != nil {
			return err
		}
		u.Location = settings.Location()
		u.Tier = run.MemoryTier
		return s.FinishUseContextTx(ctx, tx, scope, agent, &run.ThingID, run.Prompt, ms, &u)
	})
	return u, agent, err
}

func (s *Use) recordUseOverflow(ctx context.Context, scope memory.Scope, reason string, count int) {
	persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if _, err := s.Pool.Exec(persist, "INSERT INTO background_stage_events(owner_id,stage,outcome,reason,count) VALUES($1,'reader','overflow',$2,$3)", string(scope.OwnerID), reason, count); err != nil {
		slog.WarnContext(persist, "reader overflow accounting failed", "reason", reason, "count", count)
	}
}
