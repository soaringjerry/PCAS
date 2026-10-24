package intakestore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/interpretation/claim"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/postgres/kernel"
	"github.com/soaringjerry/PCAS/internal/postgres/memoryread"
	"github.com/soaringjerry/PCAS/internal/postgres/sourcestore"
	"github.com/soaringjerry/PCAS/internal/workspace"
	"github.com/soaringjerry/PCAS/internal/workspace/erasure"
)

// MemoryCommandTx applies a memory command of the owner. claim.Edit
// decides; editUnit reads and writes.
func (s *Intake) MemoryCommandTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, c workspace.Command) error {
	return claim.Edit(ctx, editUnit{store: s, tx: tx, scope: scope}, c, time.Now())
}

// editUnit is the storage of one memory command.
type editUnit struct {
	store *Intake
	tx    pgx.Tx
	scope memory.Scope
}

func (u editUnit) Active(ctx context.Context, id string) error {
	return ActiveClaim(ctx, u.tx, u.scope, id)
}

func (u editUnit) Delete(ctx context.Context, id string, includeSources bool) error {
	return u.store.DeleteTx(ctx, u.tx, u.scope, memory.DeleteRequest{Targets: []memory.Ref{{ID: memory.ID(id), Kind: memory.ClaimKind}}, BlockReimport: true, IncludeSources: includeSources})
}

func (u editUnit) TogglePin(ctx context.Context, id string) error {
	_, err := u.tx.Exec(ctx, "UPDATE activity SET pinned=NOT pinned WHERE owner_id=$1 AND record_id=$2", string(u.scope.OwnerID), id)
	return err
}

func (u editUnit) AgentExists(ctx context.Context, id string) (bool, error) {
	var exists bool
	err := u.tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM workspace_agents WHERE owner_id=$1 AND id=$2)", string(u.scope.OwnerID), id).Scan(&exists)
	return exists, err
}

func (u editUnit) SetVisibility(ctx context.Context, id string, agents []string) error {
	if _, err := u.tx.Exec(ctx, "DELETE FROM record_grants WHERE owner_id=$1 AND record_id=$2", string(u.scope.OwnerID), id); err != nil {
		return err
	}
	for _, agent := range agents {
		if _, err := u.tx.Exec(ctx, "INSERT INTO record_grants(owner_id,record_id,principal_id) VALUES($1,$2,$3) ON CONFLICT DO NOTHING", string(u.scope.OwnerID), id, agent); err != nil {
			return err
		}
	}
	return sourcestore.InvalidateTx(ctx, u.tx, u.scope, id)
}

func (u editUnit) Read(ctx context.Context, id string) (memory.Claim, error) {
	return readClaim(ctx, u.tx, u.scope, memory.ID(id), 0)
}

func (u editUnit) Correct(ctx context.Context, request memory.CorrectRequest) (memory.Ref, error) {
	return u.store.correctTx(ctx, u.tx, u.scope, request)
}

func (u editUnit) Sample(ctx context.Context, sample workspace.Sample) error {
	return SampleTx(ctx, u.tx, u.scope, sample)
}

func (u editUnit) RecordUse(ctx context.Context, event memory.UseEvent) error {
	return RecordUseTx(ctx, u.tx, u.scope, event)
}

func readClaim(ctx context.Context, tx pgx.Tx, scope memory.Scope, id memory.ID, version int) (memory.Claim, error) {
	var c memory.Claim
	var subject string
	err := tx.QueryRow(ctx, `SELECT r.id::text,c.version,c.subject_id::text,c.predicate,c.value,c.scope,c.nature,c.acquisition,c.confirmation,
		rv.valid_from,rv.valid_to,rv.time_precision,rv.expressed_at,rv.recorded_at,rv.state
		FROM memory_records r JOIN claim_revisions c ON c.owner_id=r.owner_id AND c.claim_id=r.id AND c.version=CASE WHEN $3=0 THEN r.version ELSE $3 END
		JOIN record_versions rv ON (rv.owner_id,rv.record_id,rv.version)=(c.owner_id,c.claim_id,c.version)
		WHERE r.owner_id=$1 AND r.id=$2 AND r.state='active' AND ($4 OR EXISTS(SELECT 1 FROM record_grants g WHERE g.owner_id=r.owner_id AND g.record_id=r.id AND g.principal_id=$5))`, string(scope.OwnerID), string(id), version, scope.IsOwner, scope.PrincipalID).Scan(&c.ID, &c.Version, &subject, &c.Predicate, &c.Value, &c.Scope, &c.Nature, &c.Acquisition, &c.Confirmation, &c.ValidTime.From, &c.ValidTime.To, &c.ValidTime.Precision, &c.ExpressedAt, &c.RecordedAt, &c.State)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, memory.ErrNotFound
	}
	c.SubjectID = memory.ID(subject)
	c.Kind = memory.ClaimKind
	c.Evidence = []memory.ID{}
	return c, err
}
func (s *Intake) Correct(ctx context.Context, scope memory.Scope, in memory.CorrectRequest) (memory.Ref, error) {
	var out memory.Ref
	if err := kernel.RequireOwner(scope); err != nil {
		return out, err
	}
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		if err := s.EnsureOwner(ctx, tx, scope); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "SELECT 1 FROM workspace_owners WHERE owner_id=$1 FOR UPDATE", string(scope.OwnerID)); err != nil {
			return err
		}
		var err error
		out, err = s.correctTx(ctx, tx, scope, in)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, "UPDATE workspace_owners SET revision=revision+1 WHERE owner_id=$1", string(scope.OwnerID))
		return err
	})
	return out, err
}
func (s *Intake) correctTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, in memory.CorrectRequest) (memory.Ref, error) {
	var out memory.Ref
	if !in.Target.ID.Valid() || in.Target.Version < 1 {
		return out, memory.ErrInvalid
	}
	var version int
	if err := tx.QueryRow(ctx, "SELECT version FROM memory_records WHERE owner_id=$1 AND id=$2 AND state='active' FOR UPDATE", string(scope.OwnerID), string(in.Target.ID)).Scan(&version); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return out, memory.ErrNotFound
		}
		return out, err
	}
	if version != in.Target.Version {
		return out, memory.ErrConflict
	}
	prior, err := readClaim(ctx, tx, scope, in.Target.ID, version)
	if err != nil {
		return out, err
	}
	c, changeType, err := claim.CheckCorrection(in)
	if err != nil {
		return out, err
	}
	var entity bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM entities WHERE owner_id=$1 AND id=$2)", string(scope.OwnerID), string(c.SubjectID)).Scan(&entity); err != nil {
		return out, err
	}
	if !entity {
		return out, memory.ErrInvalid
	}
	version++
	out = memory.Ref{ID: in.Target.ID, Version: version, Kind: memory.ClaimKind}
	if _, err := tx.Exec(ctx, `INSERT INTO record_versions(owner_id,record_id,version,valid_from,valid_to,time_precision,expressed_at,actor) VALUES($1,$2,$3,$4,$5,$6,$7,'user')`, string(scope.OwnerID), string(out.ID), version, c.ValidTime.From, c.ValidTime.To, c.ValidTime.Precision, c.ExpressedAt); err != nil {
		return out, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO claim_revisions(owner_id,claim_id,version,subject_id,predicate,value,scope,nature,acquisition,confirmation,change_type,reason) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, string(scope.OwnerID), string(out.ID), version, string(c.SubjectID), c.Predicate, c.Value, kernel.JSON(c.Scope), c.Nature, c.Acquisition, c.Confirmation, changeType, in.Reason); err != nil {
		return out, err
	}

	if _, err := tx.Exec(ctx, `UPDATE claim_revisions newer SET event_from=old.event_from,event_to=old.event_to,event_precision=old.event_precision,
        category=old.category,durable=old.durable
        FROM claim_revisions old WHERE (newer.owner_id,newer.claim_id,newer.version)=($1,$2,$3)
        AND (old.owner_id,old.claim_id,old.version)=($1,$2,$4)`, string(scope.OwnerID), string(out.ID), version, prior.Version); err != nil {
		return out, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO claim_mentions(owner_id,claim_id,claim_version,entity_id,role)
        SELECT owner_id,claim_id,$3,entity_id,role FROM claim_mentions WHERE owner_id=$1 AND claim_id=$2 AND claim_version=$4`, string(scope.OwnerID), string(out.ID), version, prior.Version); err != nil {
		return out, err
	}
	if err := memoryread.ResetComparisonTx(ctx, tx, scope.OwnerID, string(out.ID)); err != nil {
		return out, err
	}
	if _, err := tx.Exec(ctx, "UPDATE claims SET organized=0,organize_attempts=0 WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), string(out.ID)); err != nil {
		return out, err
	}

	if _, err := tx.Exec(ctx, "UPDATE memory_records SET version=$3,updated_at=now() WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), string(out.ID), version); err != nil {
		return out, err
	}
	// Retain original evidence and append the user's correction as a new source.
	if _, err := tx.Exec(ctx, `INSERT INTO evidence(owner_id,id,source_id,source_version,target_id,target_version,locator,acquisition,stance)
		SELECT owner_id,gen_random_uuid(),source_id,source_version,target_id,$3,locator,acquisition,stance FROM evidence WHERE owner_id=$1 AND target_id=$2 AND target_version=$4`, string(scope.OwnerID), string(out.ID), version, prior.Version); err != nil {
		return out, err
	}
	source, err := s.IngestTx(ctx, tx, scope, memory.IngestRequest{Connector: "corrections", ExternalID: string(out.ID), ExternalVersion: fmt.Sprint(version), Title: claim.CorrectionTitle, Text: string(kernel.JSON(map[string]any{"before": prior.Value, "after": c.Value, "reason": in.Reason})), MediaType: "text/plain"})
	if err != nil {
		return out, err
	}
	if _, err := tx.Exec(ctx, "INSERT INTO evidence(owner_id,id,source_id,source_version,target_id,target_version,locator,acquisition,stance) VALUES($1,$2,$3,$4,$5,$6,$7,'direct','supports')", string(scope.OwnerID), string(memory.NewID()), string(source.ID), source.Version, string(out.ID), out.Version, kernel.JSON(map[string]string{"field": "after"})); err != nil {
		return out, err
	}
	for _, e := range in.Evidence {
		if !e.Valid() {
			return out, memory.ErrInvalid
		}
		tag, err := tx.Exec(ctx, `INSERT INTO evidence(owner_id,id,source_id,source_version,target_id,target_version,locator,acquisition,stance) SELECT owner_id,gen_random_uuid(),source_id,source_version,$3,$4,locator,acquisition,stance FROM evidence WHERE owner_id=$1 AND id=$2`, string(scope.OwnerID), string(e), string(out.ID), out.Version)
		if err != nil {
			return out, err
		}
		if tag.RowsAffected() == 0 {
			return out, memory.ErrNotFound
		}
	}
	if _, err := tx.Exec(ctx, "UPDATE claim_source_keys SET corrected=true WHERE owner_id=$1 AND claim_id=$2", string(scope.OwnerID), string(out.ID)); err != nil {
		return out, err
	}
	if _, err := tx.Exec(ctx, "INSERT INTO claim_key_redirects(owner_id,key_hash,claim_id) SELECT owner_id,decode(claim_key,'hex'),claim_id FROM claim_source_keys WHERE owner_id=$1 AND claim_id=$2 ON CONFLICT(owner_id,key_hash) DO NOTHING", string(scope.OwnerID), string(out.ID)); err != nil {
		return out, err
	}
	if err := sourcestore.InvalidateTx(ctx, tx, scope, string(out.ID)); err != nil {
		return out, err
	}
	return out, kernel.Enqueue(ctx, tx, scope.OwnerID, out.ID, out.Version, "memory.index")
}
func (s *Intake) RecordUse(ctx context.Context, scope memory.Scope, in memory.UseEvent) error {
	if err := kernel.RequireOwner(scope); err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error { return RecordUseTx(ctx, tx, scope, in) })
}
func RecordUseTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, in memory.UseEvent) error {
	if !in.Ref.ID.Valid() || in.Ref.Version < 1 || in.EventID == "" || len(in.EventID) > 200 || !kernel.OneOf(in.Kind, "user_mention", "confirmation", "adoption") || in.At.IsZero() || in.At.After(time.Now().Add(time.Minute)) {
		return memory.ErrInvalid
	}
	var version int
	err := tx.QueryRow(ctx, "SELECT version FROM memory_records WHERE owner_id=$1 AND id=$2 AND state='active'", string(scope.OwnerID), string(in.Ref.ID)).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		return memory.ErrNotFound
	}
	if err != nil {
		return err
	}
	if version != in.Ref.Version {
		return memory.ErrConflict
	}
	tag, err := tx.Exec(ctx, "INSERT INTO use_events(owner_id,event_key,record_id,record_version,kind,occurred_at) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING", string(scope.OwnerID), in.EventID, string(in.Ref.ID), in.Ref.Version, in.Kind, in.At)
	if err != nil || tag.RowsAffected() == 0 {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO activity(owner_id,record_id,last_effective_use_at) VALUES($1,$2,$3) ON CONFLICT(owner_id,record_id) DO UPDATE SET last_effective_use_at=greatest(activity.last_effective_use_at,excluded.last_effective_use_at),stability=CASE WHEN activity.last_effective_use_at<excluded.last_effective_use_at-interval '1 day' THEN least(activity.reinforcement_limit,activity.stability*1.1) ELSE activity.stability END`, string(scope.OwnerID), string(in.Ref.ID), in.At)
	return err
}

func (s *Intake) Delete(ctx context.Context, scope memory.Scope, in memory.DeleteRequest) error {
	for _, ref := range in.Targets {
		if ref.Version < 1 {
			return memory.ErrInvalid
		}
	}
	if err := kernel.RequireOwner(scope); err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		if err := s.EnsureOwner(ctx, tx, scope); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "SELECT 1 FROM workspace_owners WHERE owner_id=$1 FOR UPDATE", string(scope.OwnerID)); err != nil {
			return err
		}
		if err := s.DeleteTx(ctx, tx, scope, in); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, "UPDATE workspace_owners SET revision=revision+1 WHERE owner_id=$1", string(scope.OwnerID))
		return err
	})
}

// Mark this deletion's work-item/document writes for snapshot expiration.
// The trigger observes shared purge helpers and cascades as well as local saves.
func (s *Intake) DeleteTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, in memory.DeleteRequest) (err error) {
	var previous string
	if err = tx.QueryRow(ctx, "SELECT coalesce(current_setting('pcas.expire_actions',true),'')").Scan(&previous); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "SELECT set_config('pcas.expire_actions',$1,true)", string(scope.OwnerID)); err != nil {
		return err
	}
	defer func() {
		_, restoreErr := tx.Exec(ctx, "SELECT set_config('pcas.expire_actions',$1,true)", previous)
		if err == nil {
			err = restoreErr
		}
	}()
	return s.DeleteRecordsTx(ctx, tx, scope, in)
}

type RetainUndoneAnswersKey struct{}

// DeleteRecordsTx erases memory records. erasure.Erase decides; eraseUnit
// reads and writes.
func (s *Intake) DeleteRecordsTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, in memory.DeleteRequest) error {
	keep, _ := ctx.Value(RetainUndoneAnswersKey{}).(bool)
	return erasure.Erase(ctx, eraseUnit{store: s, tx: tx, scope: scope}, erasure.Request{Targets: in.Targets, IncludeSources: in.IncludeSources, BlockReimport: in.BlockReimport, KeepAnswers: keep}, time.Now())
}

// Keep origin references on retained answers. A later explicit deletion of the
// original must still erase its copies even though undo removed the memory.
func retainDeletedMemoryOriginsTx(ctx context.Context, tx pgx.Tx, owner memory.ID, ids []string) error {
	for _, stmt := range []string{
		`UPDATE desk_turns t SET dependencies=(SELECT jsonb_agg(ref) FROM(
            SELECT value AS ref FROM jsonb_array_elements(coalesce(t.dependencies,'[]'::jsonb))
            UNION SELECT jsonb_build_object('id',e.source_id,'version',e.source_version,'kind','source')
            FROM evidence e JOIN jsonb_array_elements(coalesce(t.dependencies,'[]'::jsonb)) d ON d->>'id'=e.target_id::text
            WHERE e.owner_id=t.owner_id AND e.target_id=ANY($2::uuid[]) AND e.target_version=(d->>'version')::integer
            AND NOT(e.source_id=ANY($2::uuid[]))) refs)
            WHERE t.owner_id=$1 AND EXISTS(SELECT 1 FROM jsonb_array_elements(coalesce(t.dependencies,'[]'::jsonb)) d WHERE d->>'id'=ANY($2::text[]))`,
		`UPDATE agent_runs r SET document=jsonb_set(r.document,'{contextVersions}',(SELECT jsonb_agg(ref) FROM(
            SELECT value AS ref FROM jsonb_array_elements(coalesce(r.document->'contextVersions','[]'::jsonb))
            UNION SELECT jsonb_build_object('id',e.source_id,'version',e.source_version,'kind','source')
            FROM run_dependencies d JOIN evidence e ON(e.owner_id,e.target_id,e.target_version)=(d.owner_id,d.memory_id,d.memory_version)
            WHERE d.owner_id=r.owner_id AND d.run_id=r.id AND d.memory_id=ANY($2::uuid[]) AND NOT(e.source_id=ANY($2::uuid[]))) refs))
            WHERE r.owner_id=$1 AND EXISTS(SELECT 1 FROM run_dependencies d WHERE d.owner_id=r.owner_id AND d.run_id=r.id AND d.memory_id=ANY($2::uuid[]))`,
		`INSERT INTO run_dependencies(owner_id,run_id,memory_id,memory_version)
            SELECT DISTINCT ON(d.owner_id,d.run_id,e.source_id) d.owner_id,d.run_id,e.source_id,e.source_version
            FROM run_dependencies d JOIN evidence e ON(e.owner_id,e.target_id,e.target_version)=(d.owner_id,d.memory_id,d.memory_version)
            WHERE d.owner_id=$1 AND d.memory_id=ANY($2::uuid[]) AND NOT(e.source_id=ANY($2::uuid[]))
            ORDER BY d.owner_id,d.run_id,e.source_id,e.source_version DESC ON CONFLICT DO NOTHING`,
	} {
		if _, err := tx.Exec(ctx, stmt, string(owner), ids); err != nil {
			return err
		}
	}
	return nil
}
