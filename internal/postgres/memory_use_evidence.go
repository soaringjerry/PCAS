package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
)

type useEvidenceBatchKey struct{}
type useEvidenceAnchor struct {
	source              string
	version, start, end int
}

// Preserve the per-claim anchor ordering and visibility conditions; fetch the
// bounded set at once so a full card does not add a database round trip per row.
func useEvidenceAnchorsTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, claims []evidenceContextClaim) (map[memory.Ref]useEvidenceAnchor, error) {
	out := map[memory.Ref]useEvidenceAnchor{}
	refs := []memory.Ref{}
	for _, c := range claims {
		refs = append(refs, c.Ref)
	}
	rows, err := tx.Query(ctx, `SELECT c.id::text,c.version,a.source_id::text,a.source_version,a.start,a.finish
 FROM jsonb_to_recordset($2::jsonb) AS c(id uuid,version integer)
 CROSS JOIN LATERAL(
 SELECT e.source_id,e.source_version,coalesce((e.locator->>'start_rune')::int,0) AS start,coalesce((e.locator->>'end_rune')::int,0) AS finish
 FROM evidence e JOIN memory_records r ON(r.owner_id,r.id,r.version)=(e.owner_id,e.source_id,e.source_version)
 JOIN record_versions rv ON(rv.owner_id,rv.record_id,rv.version)=(r.owner_id,r.id,r.version)
 JOIN source_contexts sc ON(sc.owner_id,sc.source_id,sc.source_version)=(r.owner_id,r.id,r.version)
 WHERE e.owner_id=$1 AND e.target_id=c.id AND e.target_version=c.version AND r.state='active' AND rv.state='active' AND sc.conversation_key<>'' AND sc.branch<>'historical'
 ORDER BY e.id LIMIT 1) a`, string(scope.OwnerID), asJSON(refs))
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id string
		var version int
		var a useEvidenceAnchor
		if err := rows.Scan(&id, &version, &a.source, &a.version, &a.start, &a.end); err != nil {
			rows.Close()
			return nil, err
		}
		out[memory.Ref{ID: memory.ID(id), Version: version, Kind: memory.ClaimKind}] = a
	}
	err = rows.Err()
	rows.Close()
	return out, err
}

func useEvidenceRow(anchors map[memory.Ref]useEvidenceAnchor, ref memory.Ref, source *string, version, start, end *int) error {
	a, ok := anchors[ref]
	if !ok {
		return pgx.ErrNoRows
	}
	*source, *version, *start, *end = a.source, a.version, a.start, a.end
	return nil
}
