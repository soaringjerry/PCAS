package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
)

// Graph traversal requires explicit grants on the relationship and both ends.
// Similarity scores never become persistent semantic relationships.
func graphTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, seeds []memory.Ref, b memory.Budget, history bool) ([]memory.Relation, []memory.Ref, error) {
	relations := []memory.Relation{}
	added := []memory.Ref{}
	frontier := []string{}
	seen := map[memory.ID]bool{}
	edges := map[memory.ID]bool{}
	for _, ref := range seeds {
		frontier = append(frontier, string(ref.ID))
		seen[ref.ID] = true
	}
	for hop := 0; hop < b.Hops && len(frontier) > 0 && len(relations) < b.Edges; hop++ {
		rows, err := tx.Query(ctx, `SELECT l.id::text,l.version,l.from_id::text,l.from_version,a.kind,l.to_id::text,l.to_version,z.kind,l.relation_type,rv.recorded_at
		FROM relations l JOIN memory_records r ON (r.owner_id,r.id,r.version)=(l.owner_id,l.id,l.version)
		JOIN record_versions rv ON (rv.owner_id,rv.record_id,rv.version)=(l.owner_id,l.id,l.version)
		JOIN memory_records a ON (a.owner_id,a.id)=(l.owner_id,l.from_id) JOIN memory_records z ON (z.owner_id,z.id)=(l.owner_id,l.to_id)
		WHERE l.owner_id=$1 AND r.state='active' AND a.state='active' AND z.state='active'
		AND ($5 OR a.version=l.from_version AND z.version=l.to_version)
		AND (l.from_id=ANY($2::uuid[]) OR l.to_id=ANY($2::uuid[]))
		AND ($3 OR (SELECT count(*) FROM record_grants g WHERE g.owner_id=l.owner_id AND g.principal_id=$4 AND g.record_id=ANY(ARRAY[l.id,l.from_id,l.to_id]))=(SELECT count(DISTINCT id) FROM unnest(ARRAY[l.id,l.from_id,l.to_id]) AS id))
		ORDER BY l.id LIMIT $6`, string(scope.OwnerID), frontier, scope.IsOwner, scope.PrincipalID, history, b.Edges-len(relations))
		if err != nil {
			return nil, nil, err
		}
		frontier = nil
		for rows.Next() {
			r := memory.Relation{Evidence: []memory.ID{}}
			r.Kind = memory.RelationKind
			r.State = "active"
			if err := rows.Scan(&r.ID, &r.Version, &r.From.ID, &r.From.Version, &r.From.Kind, &r.To.ID, &r.To.Version, &r.To.Kind, &r.Type, &r.RecordedAt); err != nil {
				rows.Close()
				return nil, nil, err
			}
			if edges[r.ID] {
				continue
			}
			edges[r.ID] = true
			relations = append(relations, r)
			for _, ref := range []memory.Ref{r.From, r.To} {
				if !seen[ref.ID] {
					seen[ref.ID] = true
					added = append(added, ref)
					frontier = append(frontier, string(ref.ID))
				}
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, nil, err
		}
	}
	return relations, added, nil
}
