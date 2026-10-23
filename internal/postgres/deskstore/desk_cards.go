package deskstore

import (
	"context"
	"time"

	"github.com/soaringjerry/PCAS/internal/postgres/itemstore"
	"github.com/soaringjerry/PCAS/internal/postgres/kernel"
	"github.com/soaringjerry/PCAS/internal/workspace/secretary"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// SecretaryCardsTx builds the owner's cards of a turn. secretary.Cards
// decides; cardUnit reads.
func (s *Desk) SecretaryCardsTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, answer SecretaryOutput, sent map[string]workspace.Memory, sentSources map[string]workspace.DeskSourceItem, aliases map[string]workspace.Item, _ *time.Location, recall bool) ([]workspace.DeskCard, error) {
	return secretary.Cards(ctx, cardUnit{tx: tx, scope: scope}, answer, sent, sentSources, aliases, recall)
}

// cardUnit reads for the cards of one turn.
type cardUnit struct {
	tx    pgx.Tx
	scope memory.Scope
}

func (u cardUnit) Timeline(ctx context.Context, refs []memory.Ref) (map[string]secretary.TimelineMetadata, error) {
	return secretaryTimelineMetadataTx(ctx, u.tx, u.scope, refs)
}

func (u cardUnit) Item(ctx context.Context, id string) (workspace.Item, error) {
	return itemstore.GetItem(ctx, u.tx, u.scope, id)
}

// Resolve metadata and the creating turn's surviving items in one batch. Source
// provenance belongs only to the owner's cards, never to the model's prompt.
func secretaryTimelineMetadataTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, refs []memory.Ref) (map[string]secretary.TimelineMetadata, error) {
	result := map[string]secretary.TimelineMetadata{}
	if len(refs) == 0 {
		return result, nil
	}
	rows, err := kernel.QueryDocuments[secretary.TimelineMetadata](ctx, tx, `WITH cited AS (
 SELECT c.*,rv.expressed_at FROM jsonb_to_recordset($2::jsonb) AS i(id uuid,version integer)
 JOIN claim_revisions c ON c.owner_id=$1 AND c.claim_id=i.id AND c.version=i.version
 JOIN record_versions rv ON (rv.owner_id,rv.record_id,rv.version)=(c.owner_id,c.claim_id,c.version)
), turns AS (
 SELECT DISTINCT c.claim_id,c.version,t.id,t.response FROM cited c
 JOIN evidence e ON (e.owner_id,e.target_id,e.target_version)=(c.owner_id,c.claim_id,c.version)
 JOIN sources s ON (s.owner_id,s.id)=(e.owner_id,e.source_id)
 JOIN desk_turns t ON t.owner_id=s.owner_id AND t.request_id::text=lower(s.external_id)
 WHERE s.connector IN ('desk','capture','desk-incomplete')
), created_items AS (
 SELECT DISTINCT t.claim_id,t.version,w.id,w.status FROM turns t
 CROSS JOIN LATERAL jsonb_array_elements(CASE WHEN jsonb_typeof(t.response#>'{turn,receipts}')='array'
   THEN t.response#>'{turn,receipts}' ELSE '[]'::jsonb END) receipt
 JOIN work_items w ON w.owner_id=$1 AND w.id::text=receipt->>'thingId'
 WHERE receipt->>'op' IN ('create_task','create_idea','create_project') AND receipt->>'status'='done'
)
SELECT jsonb_build_object(
 'memory',jsonb_build_object('id',c.claim_id,'expressedAt',c.expressed_at,
   'eventFrom',c.event_from,'eventTo',c.event_to,
   'eventPrecision',CASE WHEN c.event_from IS NULL AND c.event_to IS NULL THEN '' ELSE c.event_precision END,
   'mentions',coalesce((SELECT jsonb_agg(jsonb_build_object('entityId',m.entity_id,'name',v.name,'role',m.role) ORDER BY m.role,v.name,m.entity_id)
     FROM claim_mentions m JOIN memory_records r ON r.owner_id=m.owner_id AND r.id=m.entity_id AND r.state='active'
     JOIN entity_versions v ON (v.owner_id,v.entity_id,v.version)=(r.owner_id,r.id,r.version)
     WHERE (m.owner_id,m.claim_id,m.claim_version)=(c.owner_id,c.claim_id,c.version)),'[]'::jsonb)),
 'source',coalesce((SELECT jsonb_build_object('sourceId',v.source_id,'version',v.version,'label',v.title,
   'at',rv.recorded_at,'expressedAt',rv.expressed_at,'connector',s.connector)
   FROM evidence e JOIN source_versions v ON (v.owner_id,v.source_id,v.version)=(e.owner_id,e.source_id,e.source_version)
   JOIN sources s ON (s.owner_id,s.id)=(v.owner_id,v.source_id)
   JOIN record_versions rv ON (rv.owner_id,rv.record_id,rv.version)=(v.owner_id,v.source_id,v.version)
   WHERE (e.owner_id,e.target_id,e.target_version)=(c.owner_id,c.claim_id,c.version)
   ORDER BY rv.recorded_at,v.source_id,v.version LIMIT 1),'{}'::jsonb),
 'tasks',coalesce((SELECT jsonb_agg(jsonb_build_object('id',w.id,'status',w.status) ORDER BY w.id)
   FROM created_items w WHERE (w.claim_id,w.version)=(c.claim_id,c.version)),'[]'::jsonb),
 'changed',EXISTS(SELECT 1 FROM claim_revisions v WHERE v.owner_id=c.owner_id AND v.claim_id=c.claim_id
   AND v.version>1 AND v.version<=c.version AND v.change_type IN ('change','correction')))
FROM cited c`, string(scope.OwnerID), kernel.JSON(refs))
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		result[row.Memory.ID] = row
	}
	return result, nil
}
