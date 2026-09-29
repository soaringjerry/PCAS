package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func (s *Store) memoryCommandTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, c workspace.Command) error {
	if !memory.ID(c.ID).Valid() {
		return memory.ErrInvalid
	}
	if err := activeClaim(ctx, tx, scope, c.ID); err != nil {
		return err
	}
	if c.Type == "deleteMemory" {
		return s.deleteTx(ctx, tx, scope, memory.DeleteRequest{Targets: []memory.Ref{{ID: memory.ID(c.ID), Kind: memory.ClaimKind}}, BlockReimport: true, IncludeSources: c.IncludeSources})
	}
	if c.Type == "pinMemory" {
		_, err := tx.Exec(ctx, "UPDATE activity SET pinned=NOT pinned WHERE owner_id=$1 AND record_id=$2", string(scope.OwnerID), c.ID)
		return err
	}
	if c.Type == "setMemoryVisibility" {
		for _, id := range c.AgentIDs {
			var exists bool
			if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM workspace_agents WHERE owner_id=$1 AND id=$2)", string(scope.OwnerID), id).Scan(&exists); err != nil {
				return err
			}
			if !exists {
				return memory.ErrInvalid
			}
		}
		if _, err := tx.Exec(ctx, "DELETE FROM record_grants WHERE owner_id=$1 AND record_id=$2", string(scope.OwnerID), c.ID); err != nil {
			return err
		}
		for _, id := range c.AgentIDs {
			if _, err := tx.Exec(ctx, "INSERT INTO record_grants(owner_id,record_id,principal_id) VALUES($1,$2,$3) ON CONFLICT DO NOTHING", string(scope.OwnerID), c.ID, id); err != nil {
				return err
			}
		}
		return invalidateTx(ctx, tx, scope, c.ID)
	}
	claim, err := readClaim(ctx, tx, scope, memory.ID(c.ID), 0)
	if err != nil {
		return err
	}
	prior := claim
	if c.Type == "editMemory" {
		if err := requireText(c.Text); err != nil {
			return err
		}
		claim.Value = asJSON(c.Text)
	}
	claim.Confirmation = "confirmed"
	claim.Acquisition = "direct"
	ref, err := s.correctTx(ctx, tx, scope, memory.CorrectRequest{Target: claim.Ref, Replacement: claim, Reason: c.Reason})
	if err != nil {
		return err
	}
	var before, after string
	_ = json.Unmarshal(prior.Value, &before)
	_ = json.Unmarshal(claim.Value, &after)
	if c.Type == "editMemory" {
		return sampleTx(ctx, tx, scope, workspace.Sample{ID: string(memory.NewID()), Kind: "correction", Prompt: before, Response: after, Origin: workspace.Origin{Label: "记忆纠正", MemoryID: c.ID}, Version: ref.Version, State: "candidate", Epistemic: "confirmed", CreatedAt: stamp()})
	}
	return recordUseTx(ctx, tx, scope, memory.UseEvent{Ref: ref, EventID: c.RequestID, Kind: "confirmation", At: time.Now()})
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
func (s *Store) Correct(ctx context.Context, scope memory.Scope, in memory.CorrectRequest) (memory.Ref, error) {
	var out memory.Ref
	if err := requireOwner(scope); err != nil {
		return out, err
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.ensureOwner(ctx, tx, scope); err != nil {
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
func (s *Store) correctTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, in memory.CorrectRequest) (memory.Ref, error) {
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
	c := in.Replacement
	changeType := in.ChangeType
	if changeType == "" {
		changeType = "correction"
	}
	if !oneOf(changeType, "correction", "change", "supplement", "evidence") {
		return out, memory.ErrInvalid
	}
	if !c.SubjectID.Valid() || requireText(c.Predicate) != nil || !json.Valid(c.Value) || !oneOf(c.Nature, "fact", "preference", "intention", "plan", "decision") || !oneOf(c.Acquisition, "direct", "reported", "inferred", "execution") || !oneOf(c.Confirmation, "unknown", "candidate", "adopted", "confirmed", "disputed") {
		return out, memory.ErrInvalid
	}
	if c.Scope == nil {
		c.Scope = map[string]json.RawMessage{}
	}
	if c.ValidTime.Precision == "" {
		c.ValidTime.Precision = "unknown"
	}
	if !oneOf(c.ValidTime.Precision, "unknown", "instant", "day", "month", "year", "range") || c.ValidTime.From != nil && c.ValidTime.To != nil && c.ValidTime.From.After(*c.ValidTime.To) {
		return out, memory.ErrInvalid
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
	if _, err := tx.Exec(ctx, `INSERT INTO claim_revisions(owner_id,claim_id,version,subject_id,predicate,value,scope,nature,acquisition,confirmation,change_type,reason) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, string(scope.OwnerID), string(out.ID), version, string(c.SubjectID), c.Predicate, c.Value, asJSON(c.Scope), c.Nature, c.Acquisition, c.Confirmation, changeType, in.Reason); err != nil {
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
	source, err := s.ingestTx(ctx, tx, scope, memory.IngestRequest{Connector: "corrections", ExternalID: string(out.ID), ExternalVersion: fmt.Sprint(version), Title: "用户纠正", Text: string(asJSON(map[string]any{"before": prior.Value, "after": c.Value, "reason": in.Reason})), MediaType: "text/plain"})
	if err != nil {
		return out, err
	}
	if _, err := tx.Exec(ctx, "INSERT INTO evidence(owner_id,id,source_id,source_version,target_id,target_version,locator,acquisition,stance) VALUES($1,$2,$3,$4,$5,$6,$7,'direct','supports')", string(scope.OwnerID), string(memory.NewID()), string(source.ID), source.Version, string(out.ID), out.Version, asJSON(map[string]string{"field": "after"})); err != nil {
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
	if err := invalidateTx(ctx, tx, scope, string(out.ID)); err != nil {
		return out, err
	}
	return out, enqueue(ctx, tx, scope.OwnerID, out.ID, out.Version, "memory.index")
}
func invalidateTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, id string) error {
	for _, sql := range []string{
		`WITH RECURSIVE affected(id) AS (SELECT $2::uuid UNION SELECT d.view_id FROM derived_dependencies d JOIN affected a ON a.id=d.dependency_id WHERE d.owner_id=$1) UPDATE derived_views SET stale=true WHERE owner_id=$1 AND id IN (SELECT id FROM affected)`,
		`UPDATE agent_runs SET document=jsonb_set(document,'{staleContext}','true') WHERE owner_id=$1 AND id IN (SELECT run_id FROM run_dependencies WHERE owner_id=$1 AND memory_id=$2)`,
		`UPDATE training_samples SET stale=true WHERE owner_id=$1 AND (memory_id=$2 OR run_id IN (SELECT run_id FROM run_dependencies WHERE owner_id=$1 AND memory_id=$2))`,
		`DELETE FROM embeddings WHERE owner_id=$1 AND record_id=$2`,
	} {
		if _, err := tx.Exec(ctx, sql, string(scope.OwnerID), id); err != nil {
			return err
		}
	}
	return refreshSummaryJobsTx(ctx, tx, scope.OwnerID, memory.ID(id))
}
func (s *Store) RecordUse(ctx context.Context, scope memory.Scope, in memory.UseEvent) error {
	if err := requireOwner(scope); err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error { return recordUseTx(ctx, tx, scope, in) })
}
func recordUseTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, in memory.UseEvent) error {
	if !in.Ref.ID.Valid() || in.Ref.Version < 1 || in.EventID == "" || len(in.EventID) > 200 || !oneOf(in.Kind, "user_mention", "confirmation", "adoption") || in.At.IsZero() || in.At.After(time.Now().Add(time.Minute)) {
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

func (s *Store) Delete(ctx context.Context, scope memory.Scope, in memory.DeleteRequest) error {
	for _, ref := range in.Targets {
		if ref.Version < 1 {
			return memory.ErrInvalid
		}
	}
	if err := requireOwner(scope); err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.ensureOwner(ctx, tx, scope); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "SELECT 1 FROM workspace_owners WHERE owner_id=$1 FOR UPDATE", string(scope.OwnerID)); err != nil {
			return err
		}
		if err := s.deleteTx(ctx, tx, scope, in); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, "UPDATE workspace_owners SET revision=revision+1 WHERE owner_id=$1", string(scope.OwnerID))
		return err
	})
}
func (s *Store) deleteTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, in memory.DeleteRequest) error {
	if len(in.Targets) == 0 || len(in.Targets) > 100 {
		return memory.ErrInvalid
	}
	ids := []string{}
	for _, ref := range in.Targets {
		if !ref.ID.Valid() {
			return memory.ErrInvalid
		}
		var version int
		err := tx.QueryRow(ctx, "SELECT version FROM memory_records WHERE owner_id=$1 AND id=$2 FOR UPDATE", string(scope.OwnerID), string(ref.ID)).Scan(&version)
		if errors.Is(err, pgx.ErrNoRows) {
			return memory.ErrNotFound
		}
		if err != nil {
			return err
		}
		if ref.Version > 0 && ref.Version != version {
			return memory.ErrConflict
		}
		ids = append(ids, string(ref.ID))
	}
	if in.IncludeSources {
		rows, err := tx.Query(ctx, "SELECT DISTINCT source_id::text FROM evidence WHERE owner_id=$1 AND target_id=ANY($2::uuid[])", string(scope.OwnerID), ids)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
	}
	// Deleting a source also deletes its extracted records, chunks and derived views.
	const deletionClosure = `WITH RECURSIVE affected(id) AS (
		SELECT unnest($2::uuid[]) UNION SELECT links.child FROM affected a JOIN (
		SELECT source_id AS parent,id AS child FROM chunks WHERE owner_id=$1 UNION SELECT source_id,target_id FROM evidence WHERE owner_id=$1
		UNION SELECT dependency_id,view_id FROM derived_dependencies WHERE owner_id=$1 UNION SELECT from_id,id FROM relations WHERE owner_id=$1 UNION SELECT to_id,id FROM relations WHERE owner_id=$1
		UNION SELECT subject_id,claim_id FROM claim_revisions WHERE owner_id=$1
 UNION SELECT c.id,s.id FROM claims c JOIN sources s ON s.owner_id=c.owner_id AND s.connector IN ('corrections','memory-input') AND s.external_id=c.id::text WHERE c.owner_id=$1
 UNION SELECT d.memory_id,s.id FROM run_dependencies d JOIN adopted_artifacts a ON (a.owner_id,a.run_id)=(d.owner_id,d.run_id) JOIN sources s ON s.owner_id=a.owner_id AND s.connector='actions' AND s.external_id=a.thing_id::text WHERE d.owner_id=$1
 UNION SELECT (disambiguation->>'source_id')::uuid,entity_id FROM entity_versions WHERE owner_id=$1 AND disambiguation ? 'source_id'
 UNION SELECT archive_id,source_id FROM archive_entries WHERE owner_id=$1
 UNION SELECT derived_from_id,source_id FROM source_versions WHERE owner_id=$1 AND derived_from_id IS NOT NULL
		) links ON links.parent=a.id) SELECT id::text FROM affected`
	rows, err := tx.Query(ctx, deletionClosure, string(scope.OwnerID), ids)
	if err != nil {
		return err
	}
	ids = []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	// Automatically organized episodes have no independent factual authority.
	// Remove those whose complete supporting membership is being erased.
	orphanRows, err := tx.Query(ctx, `SELECT DISTINCT e.id::text FROM episodes e WHERE e.owner_id=$1 AND EXISTS(SELECT 1 FROM episode_members m WHERE m.owner_id=e.owner_id AND m.episode_id=e.id AND m.member_id=ANY($2::uuid[])) AND NOT EXISTS(SELECT 1 FROM episode_members m WHERE m.owner_id=e.owner_id AND m.episode_id=e.id AND NOT(m.member_id=ANY($2::uuid[])))`, string(scope.OwnerID), ids)
	if err != nil {
		return err
	}
	orphans := []string{}
	for orphanRows.Next() {
		var id string
		if err := orphanRows.Scan(&id); err != nil {
			orphanRows.Close()
			return err
		}
		orphans = append(orphans, id)
	}
	err = orphanRows.Err()
	orphanRows.Close()
	if err != nil {
		return err
	}
	if len(orphans) > 0 {
		rows, err := tx.Query(ctx, deletionClosure, string(scope.OwnerID), append(ids, orphans...))
		if err != nil {
			return err
		}
		ids = nil
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
	}
	// An archive may contain several records. Erase its original binary when a
	// contained source is erased, while keeping unrelated normalized sources.
	archiveRows, err := tx.Query(ctx, "SELECT DISTINCT archive_id::text FROM archive_entries WHERE owner_id=$1 AND source_id=ANY($2::uuid[])", string(scope.OwnerID), ids)
	if err != nil {
		return err
	}
	archiveIDs := []string{}
	for archiveRows.Next() {
		var id string
		if err := archiveRows.Scan(&id); err != nil {
			archiveRows.Close()
			return err
		}
		archiveIDs = append(archiveIDs, id)
	}
	err = archiveRows.Err()
	archiveRows.Close()
	if err != nil {
		return err
	}
	if err := redactArchivesTx(ctx, tx, scope, archiveIDs); err != nil {
		return err
	}
	// Remove orphan subjects whose only supporting claims are being deleted.
	subjects, err := tx.Query(ctx, `SELECT DISTINCT subject_id::text FROM claim_revisions c WHERE owner_id=$1 AND claim_id=ANY($2::uuid[]) AND NOT EXISTS(SELECT 1 FROM claim_revisions kept WHERE kept.owner_id=c.owner_id AND kept.subject_id=c.subject_id AND NOT(kept.claim_id=ANY($2::uuid[])))`, string(scope.OwnerID), ids)
	if err != nil {
		return err
	}
	for subjects.Next() {
		var id string
		if err := subjects.Scan(&id); err != nil {
			subjects.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = subjects.Err()
	subjects.Close()
	if err != nil {
		return err
	}
	ids, err = purgeArtifactsTx(ctx, tx, scope, ids)
	if err != nil {
		return err
	}
	if in.BlockReimport {
		rows, err := tx.Query(ctx, "SELECT connector,external_id FROM sources WHERE owner_id=$1 AND id=ANY($2::uuid[])", string(scope.OwnerID), ids)
		if err != nil {
			return err
		}
		keys := [][]byte{}
		for rows.Next() {
			var connector, external string
			if err := rows.Scan(&connector, &external); err != nil {
				rows.Close()
				return err
			}
			keys = append(keys, sourceKey(connector, external))
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, key := range keys {
			if _, err := tx.Exec(ctx, "INSERT INTO reimport_blocks(owner_id,source_key_hash) VALUES($1,$2) ON CONFLICT DO NOTHING", string(scope.OwnerID), key); err != nil {
				return err
			}
		}
		rows, err = tx.Query(ctx, "SELECT DISTINCT claim_key FROM claim_source_keys WHERE owner_id=$1 AND claim_id=ANY($2::uuid[])", string(scope.OwnerID), ids)
		if err != nil {
			return err
		}
		keys = nil
		for rows.Next() {
			var key string
			if err := rows.Scan(&key); err != nil {
				rows.Close()
				return err
			}
			value, err := hex.DecodeString(key)
			if err != nil {
				rows.Close()
				return err
			}
			keys = append(keys, value)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, key := range keys {
			if _, err := tx.Exec(ctx, "INSERT INTO claim_reimport_blocks(owner_id,key_hash) VALUES($1,$2) ON CONFLICT DO NOTHING", string(scope.OwnerID), key); err != nil {
				return err
			}
		}
	}
	for _, sql := range []string{
		`INSERT INTO blob_cleanup_jobs(owner_id,blob_key) SELECT owner_id,blob_key FROM source_versions WHERE owner_id=$1 AND source_id=ANY($2::uuid[]) AND blob_key IS NOT NULL ON CONFLICT DO NOTHING`,
		`DELETE FROM capture_candidates WHERE owner_id=$1 AND document->>'resolvedInto'=ANY($2::text[])`,
		`DELETE FROM work_documents WHERE owner_id=$1 AND document->>'runId' IN (SELECT run_id::text FROM run_dependencies WHERE owner_id=$1 AND memory_id=ANY($2::uuid[]))`,
		`DELETE FROM training_samples WHERE owner_id=$1 AND (memory_id=ANY($2::uuid[]) OR run_id IN (SELECT run_id FROM run_dependencies WHERE owner_id=$1 AND memory_id=ANY($2::uuid[])))`,
		`INSERT INTO background_usage(owner_id,job_id,reserved_cost,created_at) SELECT owner_id,NULL,reserved_cost,created_at FROM agent_runs WHERE owner_id=$1 AND id IN (SELECT run_id FROM run_dependencies WHERE owner_id=$1 AND memory_id=ANY($2::uuid[]))`,
		`DELETE FROM agent_runs WHERE owner_id=$1 AND id IN (SELECT run_id FROM run_dependencies WHERE owner_id=$1 AND memory_id=ANY($2::uuid[]))`,
		`DELETE FROM evidence WHERE owner_id=$1 AND (source_id=ANY($2::uuid[]) OR target_id=ANY($2::uuid[]))`,
		`DELETE FROM episode_members WHERE owner_id=$1 AND member_id=ANY($2::uuid[])`,
		`DELETE FROM derived_dependencies WHERE owner_id=$1 AND dependency_id=ANY($2::uuid[])`,
		`DELETE FROM relations WHERE owner_id=$1 AND (from_id=ANY($2::uuid[]) OR to_id=ANY($2::uuid[]))`,
		`DELETE FROM claim_revisions WHERE owner_id=$1 AND claim_id=ANY($2::uuid[])`,
		`DELETE FROM source_versions WHERE owner_id=$1 AND source_id=ANY($2::uuid[])`,
	} {
		if _, err := tx.Exec(ctx, sql, string(scope.OwnerID), ids); err != nil {
			return err
		}
	}
	// Source previews are derived copies, so purge them from action records too.
	items, err := queryDocuments[workspace.Item](ctx, tx, "SELECT document FROM work_items WHERE owner_id=$1", string(scope.OwnerID))
	if err != nil {
		return err
	}
	deleted := map[string]bool{}
	for _, id := range ids {
		deleted[id] = true
	}
	for _, item := range items {
		kept := []workspace.SourceRef{}
		changed := false
		for _, source := range item.Sources {
			if deleted[source.SourceID] {
				changed = true
			} else {
				kept = append(kept, source)
			}
		}
		for i := range item.Conditions {
			if item.Conditions[i].MetBy != nil && deleted[item.Conditions[i].MetBy.SourceID] {
				item.Conditions[i].MetBy = nil
				item.Conditions[i].Met = false
				item.Conditions[i].MetAt = ""
				if item.Wake != nil && item.Wake.ConditionID == item.Conditions[i].ID {
					item.Wake = nil
					if item.Status == "awakened" {
						item.Status = "shelved"
					}
				}
				changed = true
			}
		}
		evolution := []workspace.Revision{}
		for _, rev := range item.Evolution {
			if rev.SourceID != "" && deleted[rev.SourceID] {
				changed = true
			} else {
				evolution = append(evolution, rev)
			}
		}
		item.Evolution = evolution
		if changed {
			item.Version++
			item.UpdatedAt = stamp()
			item.Sources = kept
			if err := saveItem(ctx, tx, scope, item); err != nil {
				return err
			}
		}
	}
	if in.BlockReimport {
		for _, id := range ids {
			hash := sha256.Sum256([]byte(id))
			if _, err := tx.Exec(ctx, "INSERT INTO record_reimport_blocks(owner_id,id_hash) VALUES($1,$2) ON CONFLICT DO NOTHING", string(scope.OwnerID), hash[:]); err != nil {
				return err
			}
		}
	}
	_, err = tx.Exec(ctx, "DELETE FROM memory_records WHERE owner_id=$1 AND id=ANY($2::uuid[])", string(scope.OwnerID), ids)
	return err
}
