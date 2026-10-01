package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func verifyTypedContextTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, task memory.TrustedTaskContext, deps []memory.TypedDependency) error {
	if task.OwnerID != scope.OwnerID || !task.Recipient.Valid() || !task.Scope.Valid() || task.Purpose != memory.KnowledgePurpose || task.Now.IsZero() {
		return memory.ErrForbidden
	}
	for _, dep := range deps {
		if dep.Purpose != task.Purpose || dep.Scope != task.Scope {
			return memory.ErrForbidden
		}
		entry, err := hydrateTypedOneTx(ctx, tx, scope, task, dep.Ref, map[memory.Ref]bool{}, 0)
		if err != nil {
			return err
		}
		if dep.Ref.Kind == memory.SourceKind {
			live := entry.Dependencies[0]
			if dep.Authorization == nil || live.Authorization == nil || *dep.Authorization != *live.Authorization || dep.ScopeRevision != live.ScopeRevision {
				return memory.ErrConflict
			}
		}
	}
	return nil
}

func hydrateTypedContextTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, task memory.TrustedTaskContext, refs []memory.Ref) ([]memory.EvidenceEntry, memory.Coverage, error) {
	out := []memory.EvidenceEntry{}
	cov := coverage()
	if task.OwnerID != scope.OwnerID || !task.Recipient.Valid() || !task.Scope.Valid() || task.Purpose != memory.KnowledgePurpose || task.Now.IsZero() {
		return out, cov, memory.ErrForbidden
	}
	if len(refs) > 256 {
		return out, cov, memory.ErrRecordCapacity
	}
	for _, ref := range refs {
		entry, err := hydrateTypedOneTx(ctx, tx, scope, task, ref, map[memory.Ref]bool{}, 0)
		if errors.Is(err, memory.ErrForbidden) || errors.Is(err, memory.ErrConflict) || errors.Is(err, memory.ErrNotFound) || errors.Is(err, memory.ErrInvalid) {
			cov.Complete = false
			cov.Gaps = append(cov.Gaps, "unavailable_evidence")
			continue
		}
		if err != nil {
			return out, cov, err
		}
		out = append(out, entry)
	}
	return out, cov, nil
}

func hydrateTypedOneTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, task memory.TrustedTaskContext, ref memory.Ref, visiting map[memory.Ref]bool, depth int) (memory.EvidenceEntry, error) {
	out := memory.EvidenceEntry{Ref: ref, Dependencies: []memory.TypedDependency{}, Gaps: []string{}}
	if !ref.ID.Valid() || ref.Version <= 0 || depth > 16 || visiting[ref] {
		return out, memory.ErrInvalid
	}
	visiting[ref] = true
	defer delete(visiting, ref)
	if ref.Kind == memory.ChunkKind {
		var source memory.Ref
		var start, end int
		var body string
		source.Kind = memory.SourceKind
		err := tx.QueryRow(ctx, "SELECT source_id::text,source_version,start_rune,end_rune,body FROM chunks WHERE owner_id=$1 AND id=$2 AND version=$3", string(scope.OwnerID), string(ref.ID), ref.Version).Scan(&source.ID, &source.Version, &start, &end, &body)
		if err != nil {
			return out, typedReadError(err)
		}
		entry, err := hydrateTypedOneTx(ctx, tx, scope, task, source, visiting, depth+1)
		if err != nil {
			return out, err
		}
		runes := []rune(entry.Text)
		if start < 0 || end <= start || end > len(runes) || string(runes[start:end]) != body {
			return out, memory.ErrConflict
		}
		entry.Text = body
		entry.SourceSpan = &memory.SourceSpan{Source: source, StartRune: start, EndRune: end}
		return entry, nil
	}
	if !memory.ContextKindSupported(ref.Kind) {
		return out, memory.ErrInvalid
	}
	var kind memory.Kind
	var current int
	var state string
	var expressed *string
	// Row protection is limited to this short hydration/fence transaction.
	err := tx.QueryRow(ctx, `SELECT r.kind,r.version,r.state FROM memory_records r JOIN record_versions v ON(v.owner_id,v.record_id)=(r.owner_id,r.id) WHERE r.owner_id=$1 AND r.id=$2 AND v.version=$3 AND v.state='active' AND v.recorded_at<=coalesce($4,now()) FOR SHARE OF r`, string(scope.OwnerID), string(ref.ID), ref.Version, task.View.KnownAt).Scan(&kind, &current, &state)
	if err != nil {
		return out, typedReadError(err)
	}
	if state != "active" || kind != ref.Kind {
		return out, memory.ErrConflict
	}
	out.Historical = task.View.Mode == memory.History
	out.Changed = current != ref.Version
	if !out.Historical && ref.Kind != memory.ClaimKind && current != ref.Version {
		return out, memory.ErrConflict
	}
	dep := memory.TypedDependency{Ref: ref, Purpose: task.Purpose, Scope: task.Scope}
	switch ref.Kind {
	case memory.SourceKind:
		if err := sourcePolicyAllowsTx(ctx, tx, task, ref); err != nil {
			return out, err
		}
		var connector, media string
		err = tx.QueryRow(ctx, `SELECT v.body,s.connector,v.media_type,rv.expressed_at,coalesce(sc.role,'') FROM source_versions v JOIN sources s ON(s.owner_id,s.id)=(v.owner_id,v.source_id) JOIN record_versions rv ON(rv.owner_id,rv.record_id,rv.version)=(v.owner_id,v.source_id,v.version) LEFT JOIN source_contexts sc ON(sc.owner_id,sc.source_id,sc.source_version)=(v.owner_id,v.source_id,v.version) WHERE v.owner_id=$1 AND v.source_id=$2 AND v.version=$3`, string(scope.OwnerID), string(ref.ID), ref.Version).Scan(&out.Text, &connector, &media, &out.ExpressedAt, &out.Role)
		if err != nil {
			return out, typedReadError(err)
		}
		if oneOf(connector, "actions", "corrections", "memory-input") || out.Text == "" || !strings.HasPrefix(media, "text/") && !strings.Contains(media, "json") {
			return out, memory.ErrForbidden
		}
		stamp := memory.AuthorizationStamp{}
		err = tx.QueryRow(ctx, `SELECT id::text,revision FROM source_authorizations WHERE owner_id=$1 AND source_id=$2 AND principal_id=$3 AND role=$4 AND model=$5 AND provider=$6 AND protocol=$7 AND channel=$8 AND route_fingerprint=$9 AND purpose=$10 AND scope_kind=$11 AND studio_id IS NOT DISTINCT FROM $12::uuid AND include_global_constraints=$13 AND NOT revoked`, string(scope.OwnerID), string(ref.ID), task.Recipient.PrincipalID, task.Recipient.Role, task.Recipient.Model, task.Recipient.Provider, task.Recipient.Protocol, task.Recipient.Channel, task.Recipient.RouteFingerprint, string(task.Purpose), string(task.Scope.Kind), nullString(string(task.Scope.StudioID)), task.Scope.IncludeGlobalConstraints).Scan(&stamp.PolicyID, &stamp.Revision)
		if err != nil {
			return out, typedReadError(err)
		}
		dep.Authorization = &stamp
		if err = tx.QueryRow(ctx, "SELECT coalesce((SELECT revision FROM source_scope_revisions WHERE owner_id=$1 AND source_id=$2),0)", string(scope.OwnerID), string(ref.ID)).Scan(&dep.ScopeRevision); err != nil {
			return out, err
		}
		out.SourceSpan = &memory.SourceSpan{Source: ref, StartRune: 0, EndRune: utf8.RuneCountInString(out.Text)}
	case memory.ClaimKind:
		principal := memory.Scope{OwnerID: scope.OwnerID, PrincipalID: task.Recipient.PrincipalID, Task: &task}
		claim, err := readClaim(ctx, tx, principal, ref.ID, ref.Version)
		if err != nil {
			return out, err
		}
		if !out.Historical {
			var applicable int
			valid := task.Now
			if task.View.ValidAt != nil {
				valid = *task.View.ValidAt
			}
			known := task.Now
			if task.View.KnownAt != nil {
				known = *task.View.KnownAt
			}
			if err := tx.QueryRow(ctx, "SELECT version FROM applicable_claim_versions($1,$2,$3) WHERE claim_id=$4", string(scope.OwnerID), valid, known, string(ref.ID)).Scan(&applicable); err != nil || applicable != ref.Version {
				return out, memory.ErrConflict
			}
		}
		agent, err := queryDocument[workspace.Agent](ctx, tx, "SELECT document FROM workspace_agents WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), task.Recipient.PrincipalID)
		if err != nil {
			return out, err
		}
		if !agent.Enabled || !oneOf(claim.Nature, agent.MemoryKinds...) || !agent.IncludeInferred && claim.Confirmation != "confirmed" && !(claim.Confirmation == "adopted" && claim.Acquisition == "direct") {
			return out, memory.ErrForbidden
		}
		var project string
		if raw, ok := claim.Scope["project_id"]; ok && json.Unmarshal(raw, &project) != nil {
			return out, memory.ErrInvalid
		}
		if task.Scope.Kind == memory.StudioContextScope && project != string(task.Scope.StudioID) && !(project == "" && task.Scope.IncludeGlobalConstraints && oneOf(claim.Nature, "preference", "decision")) {
			return out, memory.ErrForbidden
		}
		if task.Scope.Kind == memory.UnscopedContextScope && project != "" {
			return out, memory.ErrForbidden
		}
		rows, err := tx.Query(ctx, "SELECT source_id::text,source_version FROM evidence WHERE owner_id=$1 AND target_id=$2 AND target_version=$3", string(scope.OwnerID), string(ref.ID), ref.Version)
		if err != nil {
			return out, err
		}
		sources := []memory.Ref{}
		for rows.Next() {
			r := memory.Ref{Kind: memory.SourceKind}
			if err := rows.Scan(&r.ID, &r.Version); err != nil {
				rows.Close()
				return out, err
			}
			sources = append(sources, r)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return out, err
		}
		for _, r := range sources {
			denied, err := sourcePolicyDeniedTx(ctx, tx, task, r)
			if err != nil {
				return out, err
			}
			if denied {
				return out, memory.ErrForbidden
			}
		}
		if json.Unmarshal(claim.Value, &out.Text) != nil {
			out.Text = string(claim.Value)
		}
		out.ExpressedAt = claim.ExpressedAt
	case memory.SummaryKind:
		var allowed bool
		err = tx.QueryRow(ctx, `SELECT v.body,NOT v.stale AND EXISTS(SELECT 1 FROM record_grants g WHERE g.owner_id=v.owner_id AND g.record_id=v.id AND g.principal_id=$4) FROM derived_views v WHERE v.owner_id=$1 AND v.id=$2 AND v.version=$3`, string(scope.OwnerID), string(ref.ID), ref.Version, task.Recipient.PrincipalID).Scan(&out.Text, &allowed)
		if err != nil {
			return out, typedReadError(err)
		}
		if !allowed {
			return out, memory.ErrForbidden
		}
		rows, err := tx.Query(ctx, "SELECT dependency_id::text,dependency_version,dependency_kind FROM derived_dependencies WHERE owner_id=$1 AND view_id=$2 AND view_version=$3", string(scope.OwnerID), string(ref.ID), ref.Version)
		if err != nil {
			return out, err
		}
		children := []memory.Ref{}
		for rows.Next() {
			var child memory.Ref
			var kind *string
			if err := rows.Scan(&child.ID, &child.Version, &kind); err != nil {
				rows.Close()
				return out, err
			}
			if kind != nil {
				child.Kind = memory.Kind(*kind)
			}
			children = append(children, child)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return out, err
		}
		if len(children) == 0 || len(children) > 256 {
			return out, memory.ErrInvalid
		}
		for _, child := range children {
			entry, err := hydrateTypedOneTx(ctx, tx, scope, task, child, visiting, depth+1)
			if err != nil {
				return out, err
			}
			out.Dependencies = append(out.Dependencies, entry.Dependencies...)
		}
	}
	_ = expressed
	out.Dependencies = append([]memory.TypedDependency{dep}, out.Dependencies...)
	return out, nil
}

func typedReadError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return memory.ErrNotFound
	}
	return err
}

func dependenciesForEntries(entries []memory.EvidenceEntry) []memory.TypedDependency {
	out := []memory.TypedDependency{}
	seen := map[memory.Ref]bool{}
	for _, entry := range entries {
		for _, dep := range entry.Dependencies {
			if !seen[dep.Ref] {
				seen[dep.Ref] = true
				out = append(out, dep)
			}
		}
	}
	return out
}

func refsForDependencies(deps []memory.TypedDependency) []memory.Ref {
	out := []memory.Ref{}
	for _, d := range deps {
		out = append(out, d.Ref)
	}
	return out
}

// Select the actual matching window rather than always taking a source prefix.
// Chunk windows are already normalized above and preserve their original span.
func selectContextExcerpt(entry memory.EvidenceEntry, query string, maxRunes int) memory.EvidenceEntry {
	if maxRunes <= 0 {
		return entry
	}
	if utf8.RuneCountInString(entry.Text) <= maxRunes {
		return entry
	}
	text := matchedExcerpt(entry.Text, query, memory.SearchTokens(query), maxRunes)
	// matchedExcerpt has display ellipses; locate its literal source window.
	text = strings.TrimPrefix(strings.TrimSuffix(text, "…"), "…")
	start := strings.Index(entry.Text, text)
	if start < 0 {
		entry.Gaps = append(entry.Gaps, "excerpt_unavailable")
		entry.Text = ""
		return entry
	}
	if entry.SourceSpan != nil {
		span := *entry.SourceSpan
		span.StartRune += utf8.RuneCountInString(entry.Text[:start])
		span.EndRune = span.StartRune + utf8.RuneCountInString(text)
		entry.SourceSpan = &span
	}
	entry.Text = text
	entry.Gaps = append(entry.Gaps, "input_truncated")
	return entry
}

func contextRefKey(ref memory.Ref) string {
	return fmt.Sprintf("%s:%s:%d", ref.Kind, ref.ID, ref.Version)
}
