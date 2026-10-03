package postgres

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// Adjacent context remains bounded, keeps roles, and cannot supply extraction quotes.
func (s *Store) adjacentContext(ctx context.Context, scope memory.Scope, source memory.SourceResult) ([]map[string]any, error) {
	if oneOf(source.Source.Connector, "desk", "desk-incomplete") {
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
	return out, err
}

func sourceExpressedAt(source memory.SourceResult) *time.Time {
	if source.Source.ExpressedAt != nil {
		return source.Source.ExpressedAt
	}
	if oneOf(source.Source.Connector, "desk", "capture", "telegram", "desk-incomplete") {
		return &source.Source.RecordedAt
	}
	return nil
}

type extractedWhen struct {
	From      string `json:"from"`
	To        string `json:"to"`
	Precision string `json:"precision"`
	Quote     string `json:"quote"`
}

func extractionEvent(when *extractedWhen, source memory.SourceResult, loc *time.Location) (*time.Time, *time.Time, string) {
	if when == nil || when.Quote == "" || !strings.Contains(source.Source.Text, when.Quote) ||
		!oneOf(when.Precision, "day", "month", "year", "range") {
		return nil, nil, "unknown"
	}
	if sourceExpressedAt(source) == nil && relativeEventQuote(when.Quote) {
		return nil, nil, "unknown"
	}
	from, err := time.ParseInLocation("2006-01-02", when.From, loc)
	if err != nil {
		return nil, nil, "unknown"
	}
	to, err := time.ParseInLocation("2006-01-02", when.To, loc)
	if err != nil || to.Before(from) || to.After(from.AddDate(10, 0, 0)) || when.Precision == "range" && !to.After(from) {
		return nil, nil, "unknown"
	}
	switch when.Precision {
	case "day":
		to = from.AddDate(0, 0, 1)
	case "month":
		from = time.Date(from.Year(), from.Month(), 1, 0, 0, 0, 0, loc)
		to = from.AddDate(0, 1, 0)
	case "year":
		from = time.Date(from.Year(), 1, 1, 0, 0, 0, 0, loc)
		to = from.AddDate(1, 0, 0)
	}
	return &from, &to, when.Precision
}

func relativeEventQuote(quote string) bool {
	for _, word := range []string{"今天", "明天", "昨天", "后天", "前天", "今晚", "明晚", "下周", "上周", "本周", "这周", "下星期", "上星期", "这星期", "本星期", "下个月", "上个月", "这个月", "本月", "去年", "今年", "明年", "前年", "后年", "下半年", "上半年", "这半年", "本季度", "下季度", "上季度", "next", "last", "this", "today", "tomorrow", "yesterday", "ago"} {
		if strings.Contains(strings.ToLower(quote), word) {
			return true
		}
	}
	return relativeEventPattern.MatchString(quote)
}

var relativeEventPattern = regexp.MustCompile(`[0-9一二两三四五六七八九十几]+(个)?[天日周月年](前|后)|[周星期礼拜]+[一二三四五六日天]`)
