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
	var projects bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM work_items WHERE owner_id=$1 AND kind='project')", string(owner)).Scan(&projects); err != nil {
		return nil, err
	}
	if !projects {
		return map[string]string{}, nil
	}
	installed, err := studioInstalledTx(ctx, tx)
	if err != nil {
		return nil, err
	}
	if !installed { // Legacy migration fixtures have no canonical goal labels.
		return map[string]string{}, nil
	}
	rows, err := tx.Query(ctx, `WITH goals AS MATERIALIZED(
 SELECT r.owner_id,r.id,r.version,c.scope,c.subject_id,c.value #>> '{}' AS text FROM memory_records r
 JOIN claim_revisions c ON(c.owner_id,c.claim_id,c.version)=(r.owner_id,r.id,r.version)
 JOIN claims cl ON(cl.owner_id,cl.id)=(r.owner_id,r.id)
 WHERE r.owner_id=$1 AND r.state='active' AND cl.retired='' AND c.category='goal' AND claim_source_is_current(r.owner_id,r.id,r.version,now())),
 entities AS(SELECT owner_id,id,version,subject_id AS entity_id FROM goals WHERE subject_id IS NOT NULL
 UNION SELECT g.owner_id,g.id,g.version,m.entity_id FROM goals g JOIN claim_mentions m
 ON(m.owner_id,m.claim_id,m.claim_version)=(g.owner_id,g.id,g.version)),
 links AS(SELECT w.id::text AS project_id,g.id,g.text FROM goals g JOIN work_items w
 ON w.owner_id=g.owner_id AND w.kind='project' AND w.id::text=g.scope->>'project_id'
 UNION SELECT w.id::text,g.id,g.text FROM goals g
 JOIN entities m ON(m.owner_id,m.id,m.version)=(g.owner_id,g.id,g.version)
 JOIN memory_records er ON er.owner_id=m.owner_id AND er.id=m.entity_id AND er.state='active'
 JOIN entity_versions ev ON(ev.owner_id,ev.entity_id,ev.version)=(er.owner_id,er.id,er.version)
 JOIN work_items w ON w.owner_id=g.owner_id AND w.kind='project' AND w.id::text=ev.disambiguation->>'work_item_id'
 WHERE ev.entity_type IN('project','topic'))
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
