package postgres

import (
	"context"
	"time"

	"github.com/soaringjerry/PCAS/internal/interpretation/extraction"
	"github.com/soaringjerry/PCAS/internal/postgres/kernel"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// Adjacent context remains bounded, keeps roles, and cannot supply extraction quotes.
func (s *Store) adjacentContext(ctx context.Context, scope memory.Scope, source memory.SourceResult) ([]map[string]any, error) {
	if kernel.OneOf(source.Source.Connector, "desk", "desk-incomplete") {
		return s.deskExtractionContext(ctx, scope, source.Source.ExternalID)
	}
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

// Only earlier exchanges in this request's conversation can resolve references.
// Failed/canceled requests have no desk_turns row; their admission metadata still
// identifies the conversation, while all context bodies come from desk_turns.
func (s *Store) deskExtractionContext(ctx context.Context, scope memory.Scope, request string) ([]map[string]any, error) {
	out := []map[string]any{}
	if !memory.ID(request).Valid() {
		return out, nil
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `WITH anchor AS (
 SELECT t.conversation_id,t.created_at,t.id,o.admission_order FROM desk_turns t
 LEFT JOIN desk_turn_order o ON (o.owner_id,o.request_id)=(t.owner_id,t.request_id)
 WHERE t.owner_id=$1 AND t.request_id=$2
 UNION ALL
 SELECT conversation_id,accepted_at,'ffffffff-ffff-ffff-ffff-ffffffffffff'::uuid,admission_order FROM desk_turn_order
 WHERE owner_id=$1 AND request_id=$2 AND NOT EXISTS(SELECT 1 FROM desk_turns WHERE owner_id=$1 AND request_id=$2)
), recent AS (
 SELECT t.id,t.question,t.answer,t.dependencies,t.agent_id,t.thing_id,t.created_at,
 coalesce(o.accepted_at,t.created_at) AS ordered_at,o.admission_order
 FROM desk_turns t JOIN anchor a ON t.conversation_id=a.conversation_id
 LEFT JOIN desk_turn_order o ON (o.owner_id,o.request_id)=(t.owner_id,t.request_id)
 WHERE t.owner_id=$1 AND t.question<>'' AND (
  (a.admission_order IS NOT NULL AND o.admission_order IS NOT NULL AND o.admission_order<a.admission_order)
  OR ((a.admission_order IS NULL OR o.admission_order IS NULL) AND (t.created_at,t.id)<(a.created_at,a.id)))
 ORDER BY ordered_at DESC,o.admission_order DESC NULLS LAST,t.id DESC LIMIT 6
)
SELECT id::text,left(question,1200),left(answer,1200),dependencies,agent_id,coalesce(thing_id::text,''),created_at
FROM recent ORDER BY ordered_at,admission_order NULLS FIRST,id`, string(scope.OwnerID), request)
		if err != nil {
			return err
		}
		type exchange struct {
			id, question, answer string
			run                  workspace.Run
			at                   time.Time
		}
		var turns []exchange
		for rows.Next() {
			var turn exchange
			if err := rows.Scan(&turn.id, &turn.question, &turn.answer, &turn.run.ContextVersions, &turn.run.AgentID, &turn.run.ThingID, &turn.at); err != nil {
				rows.Close()
				return err
			}
			turns = append(turns, turn)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, turn := range turns {
			if len(turn.run.ContextVersions) > 0 && verifyRunTx(ctx, tx, scope, turn.run) != nil {
				turn.answer = outdatedDeskAnswer
			}
			for _, message := range []struct{ role, text string }{{"user", turn.question}, {"assistant", turn.answer}} {
				out = append(out, map[string]any{"id": turn.id, "role": message.role, "branch": "current", "text": message.text, "expressed_at": turn.at})
			}
		}
		return nil
	})
	if err == nil {
		var attachmentContext string
		e := s.pool.QueryRow(ctx, "SELECT coalesce(response->>'attachmentContext','') FROM desk_turns WHERE owner_id=$1 AND request_id=$2", string(scope.OwnerID), request).Scan(&attachmentContext)
		if e != nil && e != pgx.ErrNoRows {
			return out, e
		}
		if attachmentContext != "" {
			out = append(out, map[string]any{"role": "assistant", "branch": "current", "text": boundedAttachmentText(attachmentContext, 1200), "kind": "attachment_context"})
		}
	}
	return out, err
}

type extractedWhen = extraction.When
