package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
)

// This reserve accounts for future bounded state/timestamp fields without
// making revocation compete for diagnostic quota. metadata_bytes itself is
// always the server-measured logical row and dependency JSON byte count.
const contextControlReserve = 512

func lockContextDiagnosticsTx(ctx context.Context, tx pgx.Tx, owner memory.ID) error {
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", "context-diagnostics:"+string(owner))
	return err
}

func recountContextMetadataTx(ctx context.Context, tx pgx.Tx, owner memory.ID) error {
	// Imported/older diagnostics may not have reserved room. Dropping only an
	// oversized diagnostic skeleton must never stop a source revoke/delete;
	// durable artifact dependencies have no FK to these rows.
	if _, err := tx.Exec(ctx, `DELETE FROM context_attempts a WHERE owner_id=$1 AND
 octet_length((to_jsonb(a)-ARRAY['snapshot','metadata_bytes'])::text)
 + coalesce((SELECT sum(octet_length(to_jsonb(d)::text)) FROM attempt_typed_dependencies d WHERE (d.owner_id,d.attempt_id)=(a.owner_id,a.id)),0)>$2`, string(owner), memory.DefaultContextAttemptMetadataBytes); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE context_attempts a SET metadata_bytes=
 octet_length((to_jsonb(a)-ARRAY['snapshot','metadata_bytes'])::text)
 + coalesce((SELECT sum(octet_length(to_jsonb(d)::text)) FROM attempt_typed_dependencies d WHERE (d.owner_id,d.attempt_id)=(a.owner_id,a.id)),0)
 WHERE a.owner_id=$1`, string(owner))
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `WITH ranked AS (SELECT id,sum(metadata_bytes) OVER(ORDER BY created_at DESC,id DESC) AS retained FROM context_attempts WHERE owner_id=$1)
 DELETE FROM context_attempts WHERE owner_id=$1 AND id IN(SELECT id FROM ranked WHERE retained>$2)`, string(owner), memory.DefaultContextOwnerMetadataBytes)
	return err
}

func cleanupContextOwnerTx(ctx context.Context, tx pgx.Tx, owner memory.ID, now time.Time) error {
	if _, err := tx.Exec(ctx, "DELETE FROM context_attempts WHERE owner_id=$1 AND metadata_expires_at<=$2", string(owner), now); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE context_attempts SET snapshot=NULL,snapshot_bytes=0,snapshot_state='expired' WHERE owner_id=$1 AND snapshot IS NOT NULL AND body_expires_at<=$2`, string(owner), now); err != nil {
		return err
	}
	return recountContextMetadataTx(ctx, tx, owner)
}

func (s *Store) CleanupContextAttempts(ctx context.Context, now time.Time) error {
	if now.IsZero() {
		return memory.ErrInvalid
	}
	rows, err := s.pool.Query(ctx, "SELECT DISTINCT owner_id::text FROM context_attempts ORDER BY owner_id::text")
	if err != nil {
		return err
	}
	owners := []memory.ID{}
	for rows.Next() {
		var owner memory.ID
		if err = rows.Scan(&owner); err != nil {
			rows.Close()
			return err
		}
		owners = append(owners, owner)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, owner := range owners {
		if err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
			if err := lockContextDiagnosticsTx(ctx, tx, owner); err != nil {
				return err
			}
			return cleanupContextOwnerTx(ctx, tx, owner, now)
		}); err != nil {
			return err
		}
	}
	return nil
}

// The fixed process-start cutoff excludes new executions. A live automatic
// call has the same five-minute upper bound as the existing worker lease;
// recovery cannot touch it until that bound expires. Manual delivery has a
// separate state machine and is deliberately excluded. Nothing is resent.
func (s *Store) RecoverContextAttempts(ctx context.Context, startupCutoff, now time.Time) error {
	if startupCutoff.IsZero() || now.IsZero() {
		return memory.ErrInvalid
	}
	rows, err := s.pool.Query(ctx, `SELECT DISTINCT owner_id::text FROM context_attempts WHERE observation_layer<>'manual_package' AND state IN('prepared','dispatched') AND created_at<=$1 AND execution_expires_at<=$2`, startupCutoff, now)
	if err != nil {
		return err
	}
	owners := []memory.ID{}
	for rows.Next() {
		var owner memory.ID
		if err = rows.Scan(&owner); err != nil {
			rows.Close()
			return err
		}
		owners = append(owners, owner)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, owner := range owners {
		if err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
			if err := lockContextDiagnosticsTx(ctx, tx, owner); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE context_attempts SET state='outcome_unknown',error_code='execution_recovered_unknown',completed_at=$3 WHERE owner_id=$1 AND observation_layer<>'manual_package' AND state IN('prepared','dispatched') AND created_at<=$2 AND execution_expires_at<=$3`, string(owner), startupCutoff, now); err != nil {
				return err
			}
			return recountContextMetadataTx(ctx, tx, owner)
		}); err != nil {
			return err
		}
	}
	return nil
}

func loadAttemptDependenciesTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, id memory.ID) ([]memory.TypedDependency, error) {
	rows, err := tx.Query(ctx, "SELECT dependency_id::text,dependency_version,dependency_kind,purpose,hard_scope,policy_id::text,policy_revision,scope_revision FROM attempt_typed_dependencies WHERE owner_id=$1 AND attempt_id=$2", string(scope.OwnerID), string(id))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []memory.TypedDependency{}
	for rows.Next() {
		var d memory.TypedDependency
		var hard []byte
		var policy *string
		var revision *int
		if err := rows.Scan(&d.Ref.ID, &d.Ref.Version, &d.Ref.Kind, &d.Purpose, &hard, &policy, &revision, &d.ScopeRevision); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(hard, &d.Scope); err != nil {
			return nil, err
		}
		if policy != nil && revision != nil {
			d.Authorization = &memory.AuthorizationStamp{PolicyID: memory.ID(*policy), Revision: *revision}
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func controlledContextManifest(task memory.TrustedTaskContext, id memory.ID, payload []byte, layer string, entries []memory.EvidenceEntry, candidates []memory.CandidateRecord, indirect []memory.TypedDependency) (memory.ContextManifest, error) {
	estimatedTokens := (len(payload) + 2) / 3
	m := memory.ContextManifest{Version: 1, AttemptID: id, Recipient: task.Recipient, Purpose: task.Purpose, Scope: task.Scope, View: task.View, Candidates: []memory.CandidateRecord{}, Input: []memory.InputRecord{}, Used: []memory.UsedRecord{}, IndirectDependencies: indirect, DeskActions: append([]memory.ID{}, task.DeskActions...), Coverage: coverage(), InputBytes: len(payload), InputTokens: memory.TokenCount{Value: &estimatedTokens, Method: "estimated", Model: task.Recipient.Model}, ObservationLayer: layer}
	for _, candidate := range candidates {
		if !candidate.Ref.ID.Valid() || candidate.Ref.Version < 1 {
			continue
		}
		candidate.Stage = "retrieved"
		if !oneOf(candidate.Disposition, "selected", "rejected", "candidate") {
			candidate.Disposition = "candidate"
		}
		if candidate.Reason != "" {
			candidate.Reason = "context_filtered"
		}
		m.Candidates = append(m.Candidates, candidate)
	}
	for _, entry := range entries {
		if entry.Text == "" {
			continue
		}
		marker, err := hex.DecodeString(entry.AssemblyMarker)
		if err != nil || len(marker) != 16 {
			return m, memory.ErrConflict
		}
		begin, end := contextEvidenceDelimiters(entry.AssemblyMarker)
		encode := func(text string) []byte {
			if layer == "manual_package" {
				return []byte(text)
			}
			raw, _ := json.Marshal(text)
			return raw[1 : len(raw)-1]
		}
		block := encode(begin + entry.Text + end)
		literal := encode(entry.Text)
		prefix := encode(begin)
		input := memory.InputRecord{Ref: entry.Ref, SourceSpan: entry.SourceSpan, PayloadSpans: []memory.PayloadSpan{}}
		offset := bytes.Index(payload, block)
		if offset < 0 || bytes.Index(payload[offset+len(block):], block) >= 0 {
			return m, memory.ErrConflict
		}
		offset += len(prefix)
		input.PayloadSpans = append(input.PayloadSpans, memory.PayloadSpan{StartByte: offset, EndByte: offset + len(literal)})
		for _, gap := range entry.Gaps {
			if gap == "input_truncated" {
				input.Truncated = true
				m.Truncated = true
			}
			m.Coverage.Complete = false
			m.Coverage.Gaps = append(m.Coverage.Gaps, "input_coverage_gap")
		}
		m.Input = append(m.Input, input)
	}
	return m, nil
}

func (s *Store) prepareContextAttempt(ctx context.Context, scope memory.Scope, operationID string, task memory.TrustedTaskContext, deps []memory.TypedDependency, entries []memory.EvidenceEntry, candidates []memory.CandidateRecord, indirect []memory.TypedDependency, payload []byte, layer string) (memory.ContextAttempt, error) {
	var out memory.ContextAttempt
	if !scope.Valid() || task.OwnerID != scope.OwnerID || !task.Recipient.Valid() || !task.Scope.Valid() || task.Purpose != memory.KnowledgePurpose || task.Now.IsZero() || !oneOf(string(task.View.Mode), "continue", "remember", "history") {
		return out, memory.ErrForbidden
	}
	if operationID == "" || len(operationID) > 128 || len(payload) > memory.DefaultContextAttemptBodyBytes || !oneOf(layer, "serialized_request", "adapter_arguments", "manual_package") || len(deps) > 256 {
		return out, memory.ErrRecordCapacity
	}
	// A shared engineering estimate over the final observable UTF-8 payload.
	// This is not a tokenizer count or a claim about provider hidden context.
	if task.TotalInputTokens <= 0 || (len(payload)+2)/3 > task.TotalInputTokens {
		return out, memory.ErrRecordCapacity
	}
	for _, value := range []string{task.Recipient.PrincipalID, task.Recipient.Role, task.Recipient.Model, task.Recipient.Provider, task.Recipient.Protocol, task.Recipient.Channel} {
		if len(value) > 256 {
			return out, memory.ErrRecordCapacity
		}
	}
	out.ID = memory.NewID()
	out.OperationID = operationID
	out.State = memory.AttemptPrepared
	out.SnapshotState = memory.SnapshotRetained
	out.ExternalReceipt = "unknown"
	var err error
	out.Manifest, err = controlledContextManifest(task, out.ID, payload, layer, entries, candidates, indirect)
	if err != nil {
		return out, err
	}
	out.CreatedAt = time.Now().UTC()
	out.BodyExpiresAt = out.CreatedAt.Add(memory.DefaultContextBodyRetention)
	out.MetadataExpiresAt = out.CreatedAt.Add(memory.DefaultContextMetadataRetention)
	if layer != "manual_package" {
		deadline := out.CreatedAt.Add(5 * time.Minute)
		out.ExecutionExpiresAt = &deadline
	}
	if err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.ensureOwner(ctx, tx, scope); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "SELECT 1 FROM workspace_owners WHERE owner_id=$1 FOR UPDATE", string(scope.OwnerID)); err != nil {
			return err
		}
		if err := s.verifyTaskDeskActionsTx(ctx, tx, scope, task); err != nil {
			return err
		}
		if err := verifyTypedContextTx(ctx, tx, scope, task, deps); err != nil {
			return err
		}
		for _, entry := range entries {
			if entry.Text == "" {
				continue
			}
			live, err := hydrateTypedOneTx(ctx, tx, scope, task, entry.Ref, map[memory.Ref]bool{}, 0)
			if err != nil {
				return err
			}
			if entry.SourceSpan != nil {
				span := entry.SourceSpan
				runes := []rune(live.Text)
				if !span.Valid() || span.Source != entry.Ref || span.EndRune > len(runes) || string(runes[span.StartRune:span.EndRune]) != entry.Text {
					return memory.ErrConflict
				}
			} else if !strings.Contains(live.Text, entry.Text) {
				return memory.ErrConflict
			}
			for _, required := range live.Dependencies {
				found := false
				for _, d := range deps {
					if d.Ref == required.Ref && d.Scope == required.Scope && d.Purpose == required.Purpose && d.ScopeRevision == required.ScopeRevision && ((d.Authorization == nil && required.Authorization == nil) || (d.Authorization != nil && required.Authorization != nil && *d.Authorization == *required.Authorization)) {
						found = true
						break
					}
				}
				if !found {
					return memory.ErrConflict
				}
			}
		}
		if err := lockContextDiagnosticsTx(ctx, tx, scope.OwnerID); err != nil {
			return err
		}
		if err := cleanupContextOwnerTx(ctx, tx, scope.OwnerID, out.CreatedAt); err != nil {
			return err
		}
		var count, total int64
		if err := tx.QueryRow(ctx, "SELECT count(*),coalesce(sum(metadata_bytes+$2),0) FROM context_attempts WHERE owner_id=$1", string(scope.OwnerID), contextControlReserve).Scan(&count, &total); err != nil {
			return err
		}
		if count >= memory.DefaultContextOwnerAttempts {
			return memory.ErrRecordCapacity
		}
		if err := tx.QueryRow(ctx, "SELECT coalesce(max(ordinal),0)+1 FROM context_attempts WHERE owner_id=$1 AND operation_id=$2", string(scope.OwnerID), operationID).Scan(&out.Ordinal); err != nil {
			return err
		}
		hash := sha256.Sum256(payload)
		// First try the full diagnostic candidates, then only mandatory mappings.
		manifest := asJSON(out.Manifest)
		var initialBytes int
		if err := tx.QueryRow(ctx, "SELECT octet_length($1::jsonb::text)+octet_length($2::jsonb::text)", manifest, asJSON(task.Recipient)).Scan(&initialBytes); err != nil {
			return err
		}
		if initialBytes > memory.DefaultContextAttemptMetadataBytes-contextControlReserve {
			out.Manifest.Candidates = []memory.CandidateRecord{}
			out.Manifest.Coverage.Complete = false
			out.Manifest.Coverage.Gaps = []string{"candidate_metadata_omitted"}
			manifest = asJSON(out.Manifest)
			if err := tx.QueryRow(ctx, "SELECT octet_length($1::jsonb::text)+octet_length($2::jsonb::text)", manifest, asJSON(task.Recipient)).Scan(&initialBytes); err != nil {
				return err
			}
		}
		if initialBytes > memory.DefaultContextAttemptMetadataBytes-contextControlReserve {
			return memory.ErrRecordCapacity
		}
		if _, err := tx.Exec(ctx, `INSERT INTO context_attempts(owner_id,id,operation_id,ordinal,state,recipient,purpose,manifest,metadata_bytes,snapshot,snapshot_state,snapshot_bytes,input_bytes,payload_hash,observation_layer,created_at,body_expires_at,metadata_expires_at,execution_expires_at) VALUES($1,$2,$3,$4,'prepared',$5,$6,$7,$8,$9,'retained',$10,$10,$11,$12,$13,$14,$15,$16)`, string(scope.OwnerID), string(out.ID), operationID, out.Ordinal, asJSON(task.Recipient), string(task.Purpose), manifest, initialBytes, payload, len(payload), hash[:], layer, out.CreatedAt, out.BodyExpiresAt, out.MetadataExpiresAt, out.ExecutionExpiresAt); err != nil {
			return err
		}
		for _, dep := range deps {
			if !memory.ContextKindSupported(dep.Ref.Kind) || !dep.Ref.ID.Valid() || dep.Ref.Version < 1 || dep.Purpose != task.Purpose || dep.Scope != task.Scope {
				return memory.ErrInvalid
			}
			var policy any
			var revision any
			if dep.Authorization != nil {
				policy = string(dep.Authorization.PolicyID)
				revision = dep.Authorization.Revision
			}
			if _, err := tx.Exec(ctx, `INSERT INTO attempt_typed_dependencies(owner_id,attempt_id,dependency_id,dependency_version,dependency_kind,purpose,hard_scope,policy_id,policy_revision,scope_revision) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT DO NOTHING`, string(scope.OwnerID), string(out.ID), string(dep.Ref.ID), dep.Ref.Version, string(dep.Ref.Kind), string(dep.Purpose), asJSON(dep.Scope), policy, revision, dep.ScopeRevision); err != nil {
				return err
			}
		}
		var measured int64
		measure := func() error {
			return tx.QueryRow(ctx, `SELECT octet_length((to_jsonb(a)-ARRAY['snapshot','metadata_bytes'])::text)+coalesce((SELECT sum(octet_length(to_jsonb(d)::text)) FROM attempt_typed_dependencies d WHERE (d.owner_id,d.attempt_id)=(a.owner_id,a.id)),0) FROM context_attempts a WHERE owner_id=$1 AND id=$2`, string(scope.OwnerID), string(out.ID)).Scan(&measured)
		}
		if err := measure(); err != nil {
			return err
		}
		if (measured > memory.DefaultContextAttemptMetadataBytes-contextControlReserve || total+measured+contextControlReserve > memory.DefaultContextOwnerMetadataBytes) && len(out.Manifest.Candidates) > 0 {
			out.Manifest.Candidates = []memory.CandidateRecord{}
			out.Manifest.Coverage.Complete = false
			out.Manifest.Coverage.Gaps = append(out.Manifest.Coverage.Gaps, "candidate_metadata_omitted")
			if _, err := tx.Exec(ctx, "UPDATE context_attempts SET manifest=$3 WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), string(out.ID), asJSON(out.Manifest)); err != nil {
				return err
			}
			if err := measure(); err != nil {
				return err
			}
		}
		if measured > memory.DefaultContextAttemptMetadataBytes-contextControlReserve {
			return memory.ErrRecordCapacity
		}
		if total+measured+contextControlReserve > memory.DefaultContextOwnerMetadataBytes {
			return memory.ErrRecordCapacity
		}
		if _, err := tx.Exec(ctx, "UPDATE context_attempts SET metadata_bytes=$3 WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), string(out.ID), measured); err != nil {
			return err
		}
		// Reclaim only the oldest diagnostic bodies. Dependencies and artifacts
		// remain intact; the current final payload is either retained in full or
		// the whole external operation is refused.
		rows, err := tx.Query(ctx, "SELECT id::text,snapshot_bytes FROM context_attempts WHERE owner_id=$1 AND snapshot IS NOT NULL ORDER BY created_at,id", string(scope.OwnerID))
		if err != nil {
			return err
		}
		type body struct {
			id string
			n  int64
		}
		bodies := []body{}
		var bodyTotal int64
		for rows.Next() {
			var b body
			if err = rows.Scan(&b.id, &b.n); err != nil {
				rows.Close()
				return err
			}
			bodies = append(bodies, b)
			bodyTotal += b.n
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, body := range bodies {
			if bodyTotal <= memory.DefaultContextOwnerBodyBytes {
				break
			}
			if body.id == string(out.ID) {
				return memory.ErrRecordCapacity
			}
			if _, err = tx.Exec(ctx, "UPDATE context_attempts SET snapshot=NULL,snapshot_bytes=0,snapshot_state='capacity_omitted' WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), body.id); err != nil {
				return err
			}
			bodyTotal -= body.n
		}
		return recountContextMetadataTx(ctx, tx, scope.OwnerID)
	}); err != nil {
		return memory.ContextAttempt{}, err
	}
	return out, nil
}

func verifyContextAttemptTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, id memory.ID, task memory.TrustedTaskContext, deps []memory.TypedDependency) error {
	if len(task.DeskActions) > 0 {
		if verified, _ := ctx.Value(verifiedDeskOriginsKey{}).(bool); !verified {
			return memory.ErrForbidden
		}
	}
	if !id.Valid() {
		return memory.ErrInvalid
	}
	if _, err := tx.Exec(ctx, "SELECT 1 FROM workspace_owners WHERE owner_id=$1 FOR UPDATE", string(scope.OwnerID)); err != nil {
		return err
	}
	if err := verifyTypedContextTx(ctx, tx, scope, task, deps); err != nil {
		return err
	}
	if err := lockContextDiagnosticsTx(ctx, tx, scope.OwnerID); err != nil {
		return err
	}
	var state string
	var recipient, manifest []byte
	var expired bool
	err := tx.QueryRow(ctx, `SELECT state,recipient,manifest,metadata_expires_at<=now() OR coalesce(execution_expires_at<=clock_timestamp(),false) FROM context_attempts WHERE owner_id=$1 AND id=$2 FOR UPDATE`, string(scope.OwnerID), string(id)).Scan(&state, &recipient, &manifest, &expired)
	if errors.Is(err, pgx.ErrNoRows) {
		return memory.ErrConflict
	}
	if err != nil {
		return err
	}
	var bound memory.Recipient
	if err = json.Unmarshal(recipient, &bound); err != nil {
		return err
	}
	if expired || !oneOf(state, "prepared", "dispatched", "completed") || bound != task.Recipient {
		return memory.ErrConflict
	}
	var m memory.ContextManifest
	if err = json.Unmarshal(manifest, &m); err != nil {
		return err
	}
	if m.Scope != task.Scope || m.Purpose != task.Purpose || !sameDeskActions(m.DeskActions, task.DeskActions) {
		return memory.ErrConflict
	}
	stored, err := loadAttemptDependenciesTx(ctx, tx, scope, id)
	if err != nil {
		return err
	}
	if len(stored) != len(deps) {
		return memory.ErrConflict
	}
	for _, d := range stored {
		matched := false
		for _, want := range deps {
			if d.Ref == want.Ref && d.Scope == want.Scope && d.Purpose == want.Purpose && d.ScopeRevision == want.ScopeRevision && ((d.Authorization == nil && want.Authorization == nil) || (d.Authorization != nil && want.Authorization != nil && *d.Authorization == *want.Authorization)) {
				matched = true
				break
			}
		}
		if !matched {
			return memory.ErrConflict
		}
	}
	return nil
}

func persistContextArtifactDependenciesTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, parentKind, parentID string, parentVersion int, task memory.TrustedTaskContext, deps []memory.TypedDependency) error {
	if !oneOf(parentKind, "run", "desk_turn", "artifact", "summary", "manual_package", "training_sample") || parentID == "" || len(parentID) > 128 || parentVersion < 1 {
		return memory.ErrInvalid
	}
	if err := verifyTypedContextTx(ctx, tx, scope, task, deps); err != nil {
		return err
	}
	for _, d := range deps {
		var policy, revision any
		if d.Authorization != nil {
			policy = string(d.Authorization.PolicyID)
			revision = d.Authorization.Revision
		}
		if _, err := tx.Exec(ctx, `INSERT INTO context_artifact_dependencies(owner_id,parent_kind,parent_id,parent_version,dependency_id,dependency_version,dependency_kind,purpose,hard_scope,recipient,policy_id,policy_revision,scope_revision) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) ON CONFLICT DO NOTHING`, string(scope.OwnerID), parentKind, parentID, parentVersion, string(d.Ref.ID), d.Ref.Version, string(d.Ref.Kind), string(d.Purpose), asJSON(d.Scope), asJSON(task.Recipient), policy, revision, d.ScopeRevision); err != nil {
			return err
		}
	}
	return nil
}

func scanContextAttempt(row pgx.Row) (memory.ContextAttempt, error) {
	var out memory.ContextAttempt
	var manifest []byte
	err := row.Scan(&out.ID, &out.OperationID, &out.Ordinal, &out.State, &out.SnapshotState, &manifest, &out.CreatedAt, &out.DispatchReservedAt, &out.DispatchedAt, &out.DeliveredAt, &out.CompletedAt, &out.ExternalReceipt, &out.InvalidationReason, &out.BodyExpiresAt, &out.MetadataExpiresAt, &out.ExecutionExpiresAt)
	if err == nil {
		err = json.Unmarshal(manifest, &out.Manifest)
	}
	return out, err
}

const contextAttemptColumns = `id::text,operation_id,ordinal,state,snapshot_state,manifest,created_at,dispatch_reserved_at,dispatched_at,delivered_at,completed_at,external_receipt,invalidation_reason,body_expires_at,metadata_expires_at,execution_expires_at`

func (s *Store) ContextAttempts(ctx context.Context, scope memory.Scope, operationID string) ([]memory.ContextAttempt, error) {
	out := []memory.ContextAttempt{}
	if err := requireOwner(scope); err != nil {
		return out, err
	}
	if len(operationID) > 128 {
		return out, memory.ErrInvalid
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := lockContextDiagnosticsTx(ctx, tx, scope.OwnerID); err != nil {
			return err
		}
		if err := cleanupContextOwnerTx(ctx, tx, scope.OwnerID, time.Now()); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, "SELECT "+contextAttemptColumns+" FROM context_attempts WHERE owner_id=$1 AND operation_id=$2 ORDER BY ordinal", string(scope.OwnerID), operationID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			a, err := scanContextAttempt(rows)
			if err != nil {
				return err
			}
			out = append(out, a)
		}
		return rows.Err()
	})
	return out, err
}

func (s *Store) ContextAttemptSnapshot(ctx context.Context, scope memory.Scope, id memory.ID) ([]byte, error) {
	if err := requireOwner(scope); err != nil {
		return nil, err
	}
	if !id.Valid() {
		return nil, memory.ErrInvalid
	}
	var payload []byte
	var readErr error
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := lockContextDiagnosticsTx(ctx, tx, scope.OwnerID); err != nil {
			return err
		}
		if err := cleanupContextOwnerTx(ctx, tx, scope.OwnerID, time.Now()); err != nil {
			return err
		}
		err := tx.QueryRow(ctx, "SELECT snapshot FROM context_attempts WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), string(id)).Scan(&payload)
		if errors.Is(err, pgx.ErrNoRows) {
			readErr = memory.ErrNotFound
			return nil
		}
		if err == nil && payload == nil {
			readErr = memory.ErrUnavailable
		}
		return err
	})
	if err == nil {
		err = readErr
	}
	return payload, err
}

func (s *Store) markContextAttemptDelivered(ctx context.Context, scope memory.Scope, id memory.ID) error {
	if err := requireOwner(scope); err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT 1 FROM workspace_owners WHERE owner_id=$1 FOR UPDATE", string(scope.OwnerID)); err != nil {
			return err
		}
		var raw []byte
		var created time.Time
		var layer string
		if err := tx.QueryRow(ctx, "SELECT manifest,created_at,observation_layer FROM context_attempts WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), string(id)).Scan(&raw, &created, &layer); err != nil {
			return err
		}
		var m memory.ContextManifest
		if err := json.Unmarshal(raw, &m); err != nil {
			return err
		}
		if layer != "manual_package" || m.Recipient.Role != "manual" {
			return memory.ErrInvalid
		}
		live, err := s.contextRecipientTx(ctx, tx, scope, m.Recipient.PrincipalID, "manual", &m.Recipient)
		if err != nil {
			return err
		}
		if live != m.Recipient {
			return memory.ErrConflict
		}
		deps, err := loadAttemptDependenciesTx(ctx, tx, scope, id)
		if err != nil {
			return err
		}
		task := memory.TrustedTaskContext{OwnerID: scope.OwnerID, Recipient: live, Purpose: m.Purpose, Scope: m.Scope, View: m.View, Now: created, DeskActions: append([]memory.ID{}, m.DeskActions...)}
		if err := s.verifyContextAttemptTx(ctx, tx, scope, id, task, deps); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, "UPDATE context_attempts SET delivered_at=now() WHERE owner_id=$1 AND id=$2 AND state='prepared' AND snapshot IS NOT NULL AND body_expires_at>now()", string(scope.OwnerID), string(id))
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return memory.ErrConflict
		}
		return recountContextMetadataTx(ctx, tx, scope.OwnerID)
	})
}
