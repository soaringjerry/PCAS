package postgres

import (
	"context"
	"github.com/soaringjerry/PCAS/internal/memory"
)

// Adjacent context remains bounded, keeps roles, and cannot supply extraction quotes.
func (s *Store) adjacentContext(ctx context.Context, scope memory.Scope, source memory.SourceResult) ([]map[string]any, error) {
	out := []map[string]any{}
	if source.Context == nil || source.Context.Conversation == "" {
		return out, nil
	}
	at := source.Source.RecordedAt
	if source.Source.ExpressedAt != nil {
		at = *source.Source.ExpressedAt
	}
	rows, err := s.pool.Query(ctx, `SELECT v.source_id::text,c.role,c.branch,left(v.body,1200),rv.expressed_at FROM source_contexts c JOIN source_versions v ON(v.owner_id,v.source_id,v.version)=(c.owner_id,c.source_id,c.source_version) JOIN sources src ON(src.owner_id,src.id)=(v.owner_id,v.source_id) JOIN memory_records r ON(r.owner_id,r.id,r.version)=(v.owner_id,v.source_id,v.version) JOIN record_versions rv ON(rv.owner_id,rv.record_id,rv.version)=(v.owner_id,v.source_id,v.version) WHERE c.owner_id=$1 AND c.conversation_key=$2 AND c.source_id!=$3 AND r.state='active' AND rv.state='active' AND ($4 OR EXISTS(SELECT 1 FROM record_grants g WHERE g.owner_id=r.owner_id AND g.record_id=r.id AND g.principal_id=$5)) ORDER BY (src.external_id LIKE '%/'||$6) DESC,abs(extract(epoch from(coalesce(rv.expressed_at,rv.recorded_at)-$7))) LIMIT 6`, string(scope.OwnerID), source.Context.Conversation, string(source.Source.ID), scope.IsOwner, scope.PrincipalID, source.Context.Parent, at)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, role, branch, text string
		var at any
		if err := rows.Scan(&id, &role, &branch, &text, &at); err != nil {
			return out, err
		}
		out = append(out, map[string]any{"id": id, "role": role, "branch": branch, "text": text, "expressed_at": at})
	}
	return out, rows.Err()
}
