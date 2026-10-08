package postgres

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
)

// Project matching sees source-backed goals even when the project was created
// from a topic rather than a manually filled goal field. Association is by
// stored IDs; semantic matching remains the model's decision.
func projectGoalEvidenceTx(ctx context.Context, tx pgx.Tx, owner memory.ID) (map[string]string, error) {
	rows, err := tx.Query(ctx, `WITH goals AS MATERIALIZED(
 SELECT r.owner_id,r.id,r.version,c.scope,c.value #>> '{}' AS text FROM memory_records r
 JOIN claim_revisions c ON(c.owner_id,c.claim_id,c.version)=(r.owner_id,r.id,r.version)
 JOIN claims cl ON(cl.owner_id,cl.id)=(r.owner_id,r.id)
 WHERE r.owner_id=$1 AND r.state='active' AND cl.retired='' AND c.category='goal' AND claim_source_is_current(r.owner_id,r.id,r.version,now())),
 links AS(SELECT scope->>'project_id' AS project_id,id,text FROM goals WHERE coalesce(scope->>'project_id','')<>''
 UNION SELECT ev.disambiguation->>'work_item_id',g.id,g.text FROM goals g
 JOIN status_current_members m ON(m.owner_id,m.claim_id,m.claim_version)=(g.owner_id,g.id,g.version)
 JOIN memory_records er ON er.owner_id=m.owner_id AND 'entity:'||er.id::text=m.key AND er.state='active'
 JOIN entity_versions ev ON(ev.owner_id,ev.entity_id,ev.version)=(er.owner_id,er.id,er.version)
 WHERE ev.entity_type IN('project','topic') AND coalesce(ev.disambiguation->>'work_item_id','')<>'')
 SELECT project_id,text FROM links ORDER BY project_id,id`, string(owner))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id, text string
		if err = rows.Scan(&id, &text); err != nil {
			return nil, err
		}
		if out[id] != "" {
			out[id] += "\n"
		}
		out[id] += text
	}
	return out, rows.Err()
}
func projectMeaning(goal string, excerpt string, evidence string) string {
	if strings.TrimSpace(goal) != "" {
		return goal
	}
	if strings.TrimSpace(excerpt) != "" {
		return excerpt
	}
	return evidence
}
