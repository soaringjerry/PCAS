package postgres

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/soaringjerry/PCAS/internal/memory"
)

var _ memory.SourceAuthorizer = (*Store)(nil)
var _ memory.SourceScopeEditor = (*Store)(nil)

func validSourcePurpose(p memory.ContextPurpose) bool {
	return p == memory.KnowledgePurpose || p == memory.ExecutionEvidencePurpose || p == memory.RawAuditPurpose
}

func validSourceTask(t memory.TrustedTaskContext) bool {
	return t.OwnerID.Valid() && t.Recipient.Valid() && validSourcePurpose(t.Purpose) && t.Scope.Valid() && !t.Now.IsZero() && oneOf(string(t.View.Mode), "continue", "remember", "history")
}

func validSourceRef(ref memory.Ref) bool {
	return ref.Kind == memory.SourceKind && ref.ID.Valid() && ref.Version > 0
}

// The policy match is deliberately exact. A task's semantic relevance hints
// cannot change its recipient, purpose, or hard scope.
const sourcePolicyColumns = `id::text,source_id::text,principal_id,role,model,provider,protocol,channel,route_fingerprint,purpose,scope_kind,coalesce(studio_id::text,''),include_global_constraints,revision,revoked,explicit_deny,created_at,updated_at`
const sourcePolicyTuple = `source_id=$2 AND principal_id=$3 AND role=$4 AND model=$5 AND provider=$6 AND protocol=$7 AND channel=$8 AND route_fingerprint=$9 AND purpose=$10 AND scope_kind=$11 AND studio_id IS NOT DISTINCT FROM $12::uuid AND include_global_constraints=$13`

func sourcePolicyArgs(owner memory.ID, source memory.ID, recipient memory.Recipient, purpose memory.ContextPurpose, scope memory.HardScope) []any {
	return []any{string(owner), string(source), recipient.PrincipalID, recipient.Role, recipient.Model, recipient.Provider, recipient.Protocol, recipient.Channel, recipient.RouteFingerprint, string(purpose), string(scope.Kind), nullString(string(scope.StudioID)), scope.IncludeGlobalConstraints}
}

func scanSourcePolicy(row pgx.Row) (memory.SourceAuthorization, error) {
	var out memory.SourceAuthorization
	err := row.Scan(&out.ID, &out.SourceID, &out.Recipient.PrincipalID, &out.Recipient.Role, &out.Recipient.Model, &out.Recipient.Provider, &out.Recipient.Protocol, &out.Recipient.Channel, &out.Recipient.RouteFingerprint, &out.Purpose, &out.Scope.Kind, &out.Scope.StudioID, &out.Scope.IncludeGlobalConstraints, &out.Revision, &out.Revoked, &out.ExplicitDeny, &out.CreatedAt, &out.UpdatedAt)
	return out, err
}

func sourcePolicyTupleTx(ctx context.Context, tx pgx.Tx, owner memory.ID, source memory.ID, recipient memory.Recipient, purpose memory.ContextPurpose, scope memory.HardScope) (memory.SourceAuthorization, error) {
	return scanSourcePolicy(tx.QueryRow(ctx, "SELECT "+sourcePolicyColumns+" FROM source_authorizations WHERE owner_id=$1 AND "+sourcePolicyTuple, sourcePolicyArgs(owner, source, recipient, purpose, scope)...))
}

// Deny is distinct from absence: an independently granted claim needs no raw
// source allow, but the owner's explicit refusal also blocks derived evidence.
func sourcePolicyDeniedTx(ctx context.Context, tx pgx.Tx, task memory.TrustedTaskContext, source memory.Ref) (bool, error) {
	if !validSourceTask(task) || !validSourceRef(source) {
		return false, memory.ErrInvalid
	}
	args := append(sourcePolicyArgs(task.OwnerID, source.ID, task.Recipient, task.Purpose, task.Scope), source.Version)
	var denied bool
	err := tx.QueryRow(ctx, `WITH RECURSIVE ancestors(id,version) AS (
	 SELECT $2::uuid,$14::integer UNION
	 SELECT l.parent_id,l.parent_version FROM ancestors a JOIN (
	  SELECT source_id child_id,version child_version,derived_from_id parent_id,derived_from_version parent_version FROM source_versions WHERE owner_id=$1 AND derived_from_id IS NOT NULL AND derived_from_version IS NOT NULL
	  UNION SELECT source_id,source_version,archive_id,archive_version FROM archive_entries WHERE owner_id=$1
	 ) l ON (l.child_id,l.child_version)=(a.id,a.version)
	) SELECT EXISTS(SELECT 1 FROM source_authorizations WHERE owner_id=$1 AND source_id IN(SELECT id FROM ancestors) AND principal_id=$3 AND role=$4 AND model=$5 AND provider=$6 AND protocol=$7 AND channel=$8 AND route_fingerprint=$9 AND purpose=$10 AND scope_kind=$11 AND studio_id IS NOT DISTINCT FROM $12::uuid AND include_global_constraints=$13 AND revoked AND explicit_deny)`, args...).Scan(&denied)
	return denied, err
}

func sourceScopeAllowsTx(ctx context.Context, tx pgx.Tx, owner memory.ID, source memory.ID, scope memory.HardScope) (bool, error) {
	if !scope.Valid() {
		return false, memory.ErrInvalid
	}
	var allowed bool
	switch scope.Kind {
	case memory.OwnerGlobalContextScope:
		// The caller must have bound an explicit owner-global task. This only
		// passes the scope check; it never grants access to any source.
		return true, nil
	case memory.UnscopedContextScope:
		err := tx.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM source_scope_assignments WHERE owner_id=$1 AND source_id=$2 AND scope_kind<>'unscoped')`, string(owner), string(source)).Scan(&allowed)
		return allowed, err
	case memory.StudioContextScope:
		err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM source_scope_assignments WHERE owner_id=$1 AND source_id=$2 AND ((scope_kind='studio' AND studio_id=$3) OR ($4 AND scope_kind='global_constraint')))`, string(owner), string(source), string(scope.StudioID), scope.IncludeGlobalConstraints).Scan(&allowed)
		return allowed, err
	default:
		return false, memory.ErrInvalid
	}
}

// All raw source consumers need both the coarse record grant and this exact
// external destination policy. Owner audit expansion uses a separate path.
func sourcePolicyAllowsTx(ctx context.Context, tx pgx.Tx, task memory.TrustedTaskContext, source memory.Ref) error {
	if !validSourceTask(task) || !validSourceRef(source) {
		return memory.ErrInvalid
	}
	if task.Purpose == memory.RawAuditPurpose {
		return memory.ErrForbidden // raw audit is an owner operation, not a model purpose
	}
	var connector, media, representation string
	var current int
	var granted bool
	err := tx.QueryRow(ctx, `SELECT r.version,s.connector,v.media_type,v.representation,EXISTS(SELECT 1 FROM record_grants g WHERE g.owner_id=r.owner_id AND g.record_id=r.id AND g.principal_id=$4)
	 FROM sources s JOIN memory_records r ON (r.owner_id,r.id)=(s.owner_id,s.id)
	 JOIN source_versions v ON (v.owner_id,v.source_id)=(s.owner_id,s.id) AND v.version=$3
	 JOIN record_versions rv ON (rv.owner_id,rv.record_id,rv.version)=(v.owner_id,v.source_id,v.version)
	 WHERE s.owner_id=$1 AND s.id=$2 AND r.kind='source' AND r.state='active' AND rv.state='active' FOR SHARE OF r,rv`, string(task.OwnerID), string(source.ID), source.Version, task.Recipient.PrincipalID).Scan(&current, &connector, &media, &representation, &granted)
	if errors.Is(err, pgx.ErrNoRows) {
		return memory.ErrNotFound
	}
	if err != nil {
		return err
	}
	if task.View.Mode != memory.History && current != source.Version {
		return memory.ErrConflict
	}
	if !granted || oneOf(connector, "actions", "corrections", "memory-input") {
		return memory.ErrForbidden
	}
	denied, err := sourcePolicyDeniedTx(ctx, tx, task, source)
	if err != nil {
		return err
	}
	if denied {
		return memory.ErrForbidden
	}
	if representation == "original" && !strings.HasPrefix(media, "text/") {
		return memory.ErrUnavailable // an unconverted attachment is not readable text
	}
	allowed, err := sourceScopeAllowsTx(ctx, tx, task.OwnerID, source.ID, task.Scope)
	if err != nil {
		return err
	}
	if !allowed {
		return memory.ErrForbidden
	}
	policy, err := sourcePolicyTupleTx(ctx, tx, task.OwnerID, source.ID, task.Recipient, task.Purpose, task.Scope)
	if errors.Is(err, pgx.ErrNoRows) {
		return memory.ErrForbidden
	}
	if err != nil {
		return err
	}
	if policy.Revoked {
		return memory.ErrForbidden
	}
	return nil
}

func lockSourceMutationTx(ctx context.Context, tx pgx.Tx, owner memory.ID, ref memory.Ref) (string, error) {
	if !validSourceRef(ref) {
		return "", memory.ErrInvalid
	}
	var version int
	var connector string
	err := tx.QueryRow(ctx, `SELECT r.version,s.connector FROM memory_records r JOIN sources s ON (s.owner_id,s.id)=(r.owner_id,r.id) WHERE r.owner_id=$1 AND r.id=$2 AND r.kind='source' AND r.state='active' FOR UPDATE OF r`, string(owner), string(ref.ID)).Scan(&version, &connector)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", memory.ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if version != ref.Version {
		return "", memory.ErrConflict
	}
	return connector, nil
}

func recipientSelectionMatches(selection, actual memory.Recipient) bool {
	return (selection.PrincipalID == "" || selection.PrincipalID == actual.PrincipalID) &&
		(selection.Role == "" || selection.Role == actual.Role) &&
		(selection.Model == "" || selection.Model == actual.Model) &&
		(selection.Provider == "" || selection.Provider == actual.Provider) &&
		(selection.Protocol == "" || selection.Protocol == actual.Protocol) &&
		(selection.Channel == "" || selection.Channel == actual.Channel) &&
		(selection.RouteFingerprint == "" || selection.RouteFingerprint == actual.RouteFingerprint)
}

// An owner may select a registered principal and role without calculating a
// route fingerprint. Nonempty assertions must all match the one server route.
func (s *Store) canonicalSourceRecipientTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, recipient memory.Recipient) (memory.Recipient, error) {
	if recipient.PrincipalID == "" || recipient.Role == "" {
		return memory.Recipient{}, memory.ErrInvalid
	}
	var manual *memory.Recipient
	if recipient.PrincipalID == "manual" || recipient.Channel == "manual" {
		manual = &recipient
	}
	actual, err := s.contextRecipientTx(ctx, tx, scope, recipient.PrincipalID, recipient.Role, manual)
	if err != nil {
		return memory.Recipient{}, err
	}
	if !actual.Valid() || !recipientSelectionMatches(recipient, actual) {
		return memory.Recipient{}, memory.ErrForbidden
	}
	return actual, nil
}

func syncSourceGrantTx(ctx context.Context, tx pgx.Tx, owner memory.ID, source memory.ID, principal string) error {
	_, err := tx.Exec(ctx, `INSERT INTO record_grants(owner_id,record_id,principal_id) SELECT $1,$2,$3 WHERE EXISTS(SELECT 1 FROM source_authorizations WHERE owner_id=$1 AND source_id=$2 AND principal_id=$3 AND NOT revoked) ON CONFLICT DO NOTHING`, string(owner), string(source), principal)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `DELETE FROM record_grants WHERE owner_id=$1 AND record_id=$2 AND principal_id=$3 AND NOT EXISTS(SELECT 1 FROM source_authorizations WHERE owner_id=$1 AND source_id=$2 AND principal_id=$3 AND NOT revoked)`, string(owner), string(source), principal)
	return err
}

func sourceMutationError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return memory.ErrConflict
	}
	return err
}

// The caller owns the short owner gate, command receipt, and action buffer.
// Replacing by PolicyID updates one row atomically, including a narrower scope.
func (s *Store) mutateSourceAuthorizationTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, in memory.SourceAuthorizationRequest) (memory.SourceAuthorizationResult, error) {
	var out memory.SourceAuthorizationResult
	if err := requireOwner(scope); err != nil {
		return out, err
	}
	if !validSourceRef(in.Source) || in.ExpectedPolicyRevision < 0 || in.PolicyID != "" && !in.PolicyID.Valid() {
		return out, memory.ErrInvalid
	}
	connector, err := lockSourceMutationTx(ctx, tx, scope.OwnerID, in.Source)
	if err != nil {
		return out, err
	}
	if in.Revoke && in.PolicyID != "" {
		stored, err := scanSourcePolicy(tx.QueryRow(ctx, "SELECT "+sourcePolicyColumns+" FROM source_authorizations WHERE owner_id=$1 AND id=$2 FOR UPDATE", string(scope.OwnerID), string(in.PolicyID)))
		if errors.Is(err, pgx.ErrNoRows) {
			return out, memory.ErrNotFound
		}
		if err != nil {
			return out, err
		}
		if stored.SourceID != in.Source.ID || stored.Revision != in.ExpectedPolicyRevision || !recipientSelectionMatches(in.Recipient, stored.Recipient) || in.Purpose != "" && stored.Purpose != in.Purpose || in.Scope != (memory.HardScope{}) && stored.Scope != in.Scope {
			return out, memory.ErrConflict
		}
		in.Recipient, in.Purpose, in.Scope = stored.Recipient, stored.Purpose, stored.Scope
	}
	if !validSourcePurpose(in.Purpose) || !in.Scope.Valid() {
		return out, memory.ErrInvalid
	}
	if !in.Revoke {
		if in.Purpose == memory.RawAuditPurpose || oneOf(connector, "actions", "corrections", "memory-input") {
			return out, memory.ErrForbidden
		}
		in.Recipient, err = s.canonicalSourceRecipientTx(ctx, tx, scope, in.Recipient)
		if err != nil {
			return out, err
		}
	} else if !in.Recipient.Valid() {
		// First explicit refusal may select today's recipient before any
		// policy exists. Existing ID-based revocations were resolved above;
		// a complete old tuple never needs the current route to remain enabled.
		in.Recipient, err = s.canonicalSourceRecipientTx(ctx, tx, scope, in.Recipient)
		if err != nil {
			return out, err
		}
	}
	if !in.Revoke && in.Scope.Kind == memory.StudioContextScope {
		var project bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM work_items WHERE owner_id=$1 AND id=$2 AND kind='project')", string(scope.OwnerID), string(in.Scope.StudioID)).Scan(&project); err != nil {
			return out, err
		}
		if !project {
			return out, memory.ErrInvalid
		}
	}
	var prior memory.SourceAuthorization
	if in.PolicyID != "" {
		prior, err = scanSourcePolicy(tx.QueryRow(ctx, "SELECT "+sourcePolicyColumns+" FROM source_authorizations WHERE owner_id=$1 AND id=$2 FOR UPDATE", string(scope.OwnerID), string(in.PolicyID)))
		if errors.Is(err, pgx.ErrNoRows) {
			return out, memory.ErrNotFound
		}
		if err != nil {
			return out, err
		}
		if prior.SourceID != in.Source.ID || prior.Revision != in.ExpectedPolicyRevision {
			return out, memory.ErrConflict
		}
		// ID-based DELETE must name the original tuple. It must not silently
		// convert an old permission into a denial for a different destination.
		if in.Revoke && (prior.Recipient != in.Recipient || prior.Purpose != in.Purpose || prior.Scope != in.Scope) {
			return out, memory.ErrConflict
		}
	} else {
		prior, err = sourcePolicyTupleTx(ctx, tx, scope.OwnerID, in.Source.ID, in.Recipient, in.Purpose, in.Scope)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return out, err
		}
		if err == nil && prior.Revision != in.ExpectedPolicyRevision || errors.Is(err, pgx.ErrNoRows) && in.ExpectedPolicyRevision != 0 {
			return out, memory.ErrConflict
		}
	}
	var before *memory.SourceAuthorization
	if prior.ID == "" {
		out.Authorization.ID = memory.NewID()
		out.Authorization.Revision = 1
	} else {
		before = &prior
		out.Authorization.ID = prior.ID
		out.Authorization.Revision = prior.Revision + 1
	}
	args := sourcePolicyArgs(scope.OwnerID, in.Source.ID, in.Recipient, in.Purpose, in.Scope)
	args = append(args, string(out.Authorization.ID), out.Authorization.Revision, in.Revoke, in.Revoke && sourcePolicyRevokeIsExplicit(ctx))
	if before == nil {
		out.Authorization, err = scanSourcePolicy(tx.QueryRow(ctx, `INSERT INTO source_authorizations(owner_id,source_id,principal_id,role,model,provider,protocol,channel,route_fingerprint,purpose,scope_kind,studio_id,include_global_constraints,id,revision,revoked,explicit_deny) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17) RETURNING `+sourcePolicyColumns, args...))
	} else {
		out.Authorization, err = scanSourcePolicy(tx.QueryRow(ctx, `UPDATE source_authorizations SET source_id=$2,principal_id=$3,role=$4,model=$5,provider=$6,protocol=$7,channel=$8,route_fingerprint=$9,purpose=$10,scope_kind=$11,studio_id=$12,include_global_constraints=$13,revision=$15,revoked=$16,explicit_deny=$17,updated_at=now() WHERE owner_id=$1 AND id=$14 RETURNING `+sourcePolicyColumns, args...))
	}
	if err != nil {
		return out, sourceMutationError(err)
	}
	if before != nil {
		if err := syncSourceGrantTx(ctx, tx, scope.OwnerID, in.Source.ID, prior.Recipient.PrincipalID); err != nil {
			return out, err
		}
	}
	if err := syncSourceGrantTx(ctx, tx, scope.OwnerID, in.Source.ID, in.Recipient.PrincipalID); err != nil {
		return out, err
	}
	if before != nil || in.Revoke {
		// Even regrant invalidates old attempts. A policy revision is a fence,
		// not a request to resurrect previously invalidated derived material.
		invalidation := memory.ContextInvalidation{RecordIDs: []memory.ID{in.Source.ID}, Reason: memory.ContextRevoked, Recipient: &in.Recipient}
		if before != nil && prior.Recipient != in.Recipient {
			invalidation.Recipient = &prior.Recipient
		}
		if err := invalidateTypedContextTx(ctx, tx, scope, invalidation); err != nil {
			return out, err
		}
	}
	if err := recordSourcePolicyActionTx(ctx, tx, scope, before, out.Authorization); err != nil {
		return out, err
	}
	return out, nil
}

func readSourceScopeTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, id memory.ID) (memory.SourceScopeResult, error) {
	out := memory.SourceScopeResult{Assignments: []memory.SourceScopeAssignment{}}
	if err := requireOwner(scope); err != nil {
		return out, err
	}
	if !id.Valid() {
		return out, memory.ErrInvalid
	}
	err := tx.QueryRow(ctx, `SELECT r.id::text,r.version,coalesce(sr.revision,0) FROM memory_records r JOIN sources s ON (s.owner_id,s.id)=(r.owner_id,r.id) LEFT JOIN source_scope_revisions sr ON (sr.owner_id,sr.source_id)=(r.owner_id,r.id) WHERE r.owner_id=$1 AND r.id=$2 AND r.kind='source' AND r.state='active'`, string(scope.OwnerID), string(id)).Scan(&out.Source.ID, &out.Source.Version, &out.Revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, memory.ErrNotFound
	}
	if err != nil {
		return out, err
	}
	out.Source.Kind = memory.SourceKind
	rows, err := tx.Query(ctx, `SELECT scope_kind,coalesce(studio_id::text,'') FROM source_scope_assignments WHERE owner_id=$1 AND source_id=$2 ORDER BY scope_kind,studio_id`, string(scope.OwnerID), string(id))
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var assignment memory.SourceScopeAssignment
		if err := rows.Scan(&assignment.Kind, &assignment.StudioID); err != nil {
			return out, err
		}
		out.Assignments = append(out.Assignments, assignment)
	}
	return out, rows.Err()
}

func (s *Store) mutateSourceScopeTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, in memory.SourceScopeRequest) (memory.SourceScopeResult, error) {
	var out memory.SourceScopeResult
	if err := requireOwner(scope); err != nil {
		return out, err
	}
	if !validSourceRef(in.Source) || in.ExpectedRevision < 0 || len(in.Assignments) > 100 {
		return out, memory.ErrInvalid
	}
	if _, err := lockSourceMutationTx(ctx, tx, scope.OwnerID, in.Source); err != nil {
		return out, err
	}
	seen := map[memory.SourceScopeAssignment]bool{}
	for _, assignment := range in.Assignments {
		if seen[assignment] || assignment.Kind != memory.StudioSourceScope && assignment.Kind != memory.GlobalConstraintSourceScope && assignment.Kind != memory.UnscopedSourceScope {
			return out, memory.ErrInvalid
		}
		seen[assignment] = true
		if assignment.Kind == memory.StudioSourceScope {
			if !assignment.StudioID.Valid() {
				return out, memory.ErrInvalid
			}
			var exists bool
			if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM work_items WHERE owner_id=$1 AND id=$2 AND kind='project')", string(scope.OwnerID), string(assignment.StudioID)).Scan(&exists); err != nil {
				return out, err
			}
			if !exists {
				return out, memory.ErrInvalid
			}
		} else if assignment.StudioID != "" || assignment.Kind == memory.UnscopedSourceScope && len(in.Assignments) != 1 {
			return out, memory.ErrInvalid
		}
	}
	before, err := readSourceScopeTx(ctx, tx, scope, in.Source.ID)
	if err != nil {
		return out, err
	}
	if before.Revision != in.ExpectedRevision {
		return out, memory.ErrConflict
	}
	if _, err := tx.Exec(ctx, `INSERT INTO source_scope_revisions(owner_id,source_id,revision) VALUES($1,$2,$3) ON CONFLICT(owner_id,source_id) DO UPDATE SET revision=excluded.revision,updated_at=now()`, string(scope.OwnerID), string(in.Source.ID), before.Revision+1); err != nil {
		return out, err
	}
	if _, err := tx.Exec(ctx, "DELETE FROM source_scope_assignments WHERE owner_id=$1 AND source_id=$2", string(scope.OwnerID), string(in.Source.ID)); err != nil {
		return out, err
	}
	for _, assignment := range in.Assignments {
		if _, err := tx.Exec(ctx, `INSERT INTO source_scope_assignments(owner_id,source_id,scope_kind,studio_id) VALUES($1,$2,$3,$4)`, string(scope.OwnerID), string(in.Source.ID), string(assignment.Kind), nullString(string(assignment.StudioID))); err != nil {
			return out, err
		}
	}
	out, err = readSourceScopeTx(ctx, tx, scope, in.Source.ID)
	if err != nil {
		return out, err
	}
	if err := invalidateTypedContextTx(ctx, tx, scope, memory.ContextInvalidation{RecordIDs: []memory.ID{in.Source.ID}, Reason: memory.ContextScopeChanged}); err != nil {
		return out, err
	}
	if err := recordSourceScopeActionTx(ctx, tx, scope, before, out); err != nil {
		return out, err
	}
	return out, nil
}

// Whole current owner utterances only. Source text and model output never enter
// this parser; anchored finite grammar rejects questions, quotes and hypotheticals.
var sourceAuthorizationIntent = regexp.MustCompile(`^(让秘书能用|允许这个副手读取|不再让秘书读取|别再用)《([^《》\r\n]+)》[。！!]?\s*$`)

func (s *Store) resolveSourceAuthorizationIntentTx(ctx context.Context, tx pgx.Tx, ownerScope memory.Scope, currentUserText string, serverTarget memory.TrustedTaskContext) (memory.SourceAuthorizationRequest, bool, error) {
	var out memory.SourceAuthorizationRequest
	if err := requireOwner(ownerScope); err != nil {
		return out, false, err
	}
	match := sourceAuthorizationIntent.FindStringSubmatch(strings.TrimSpace(currentUserText))
	if match == nil {
		return out, false, nil
	}
	if !validSourceTask(serverTarget) || serverTarget.OwnerID != ownerScope.OwnerID {
		return out, false, memory.ErrForbidden
	}
	role := serverTarget.Recipient.Role
	if match[1] == "让秘书能用" || match[1] == "不再让秘书读取" {
		role = "secretary"
	} else if match[1] == "允许这个副手读取" {
		role = "deputy"
	}
	if role != serverTarget.Recipient.Role {
		var manual *memory.Recipient
		if serverTarget.Recipient.Channel == "manual" {
			manual = &serverTarget.Recipient
		}
		actual, err := s.contextRecipientTx(ctx, tx, ownerScope, serverTarget.Recipient.PrincipalID, role, manual)
		if err != nil {
			return out, false, err
		}
		serverTarget.Recipient = actual
	}
	rows, err := tx.Query(ctx, `SELECT r.id::text,r.version FROM sources s JOIN memory_records r ON (r.owner_id,r.id)=(s.owner_id,s.id) JOIN source_versions v ON (v.owner_id,v.source_id,v.version)=(r.owner_id,r.id,r.version) WHERE s.owner_id=$1 AND r.state='active' AND r.kind='source' AND v.title=$2 ORDER BY r.id LIMIT 2`, string(ownerScope.OwnerID), match[2])
	if err != nil {
		return out, false, err
	}
	refs := []memory.Ref{}
	for rows.Next() {
		ref := memory.Ref{Kind: memory.SourceKind}
		if err := rows.Scan(&ref.ID, &ref.Version); err != nil {
			rows.Close()
			return out, false, err
		}
		refs = append(refs, ref)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, false, err
	}
	if len(refs) != 1 {
		return out, false, nil // one necessary selection; never guess a recent source
	}
	out = memory.SourceAuthorizationRequest{Source: refs[0], Recipient: serverTarget.Recipient, Purpose: serverTarget.Purpose, Scope: serverTarget.Scope, Revoke: match[1] == "不再让秘书读取" || match[1] == "别再用"}
	policy, err := sourcePolicyTupleTx(ctx, tx, ownerScope.OwnerID, out.Source.ID, out.Recipient, out.Purpose, out.Scope)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return out, false, err
	}
	if err == nil {
		out.PolicyID, out.ExpectedPolicyRevision = policy.ID, policy.Revision
	}
	return out, true, nil
}
