package evidencestore

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/soaringjerry/PCAS/internal/contextbuilder"
	"github.com/soaringjerry/PCAS/internal/postgres/accessstore"
	"github.com/soaringjerry/PCAS/internal/postgres/kernel"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
)

type conversationPoint struct {
	memory.ConversationMessage
	title, conversation, branch, external string
	at                                    time.Time
	permitted                             bool
	currentVersion                        int
}

func (s *Evidence) SourceConversation(ctx context.Context, scope memory.Scope, in memory.ConversationRequest) (memory.ConversationResult, error) {
	out := memory.ConversationResult{Messages: []memory.ConversationMessage{}, Gaps: []string{}}
	if err := kernel.RequireOwner(scope); err != nil {
		return out, err
	}
	if !in.ID.Valid() || in.Version < 0 || in.Limit < 0 || in.Limit > 20 || in.Before != "" && !in.Before.Valid() || in.After != "" && !in.After.Valid() || in.Before != "" && in.After != "" {
		return out, memory.ErrInvalid
	}
	err := pgx.BeginTxFunc(ctx, s.Pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		var err error
		out, err = conversationWindowTx(ctx, tx, scope, in, "", nil)
		return err
	})
	return out, err
}

// Reads only the cited conversation using its existing owner/conversation index.
// The model path gates every window on source visibility, including exclusions.
func conversationWindowTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, in memory.ConversationRequest, principal string, thingID *string) (memory.ConversationResult, error) {
	out := memory.ConversationResult{Messages: []memory.ConversationMessage{}, Gaps: []string{}}
	if _, err := tx.Exec(ctx, "SET LOCAL jit = off"); err != nil {
		return out, err
	}
	parentVisible := `(coalesce(c.role,'')<>'assistant' OR coalesce(c.parent_key,'')='' OR EXISTS(
  SELECT 1 FROM source_contexts parent JOIN sources parent_s ON(parent_s.owner_id,parent_s.id)=(parent.owner_id,parent.source_id)
  JOIN memory_records parent_r ON(parent_r.owner_id,parent_r.id,parent_r.version)=(parent.owner_id,parent.source_id,parent.source_version)
  JOIN record_versions parent_v ON(parent_v.owner_id,parent_v.record_id,parent_v.version)=(parent_r.owner_id,parent_r.id,parent_r.version)
  WHERE parent.owner_id=$1 AND parent.conversation_key=c.conversation_key AND parent.branch=c.branch
  AND right(parent_s.external_id,length(c.parent_key)+1)='/'||c.parent_key AND parent_r.state='active' AND parent_v.state='active'
  AND ` + accessstore.TeamSourceVisibleSQL("$1", "parent_r.id", "$4", "$5") + `))`
	permitted := "($4::text='' OR ((" + accessstore.TeamSourceVisibleSQL("$1", "r.id", "$4", "$5") + ") AND " + parentVisible + "))"
	columns := `r.id::text,rv.version,v.body,coalesce(c.role,''),rv.expressed_at,rv.recorded_at,v.title,coalesce(c.conversation_key,''),coalesce(c.branch,''),s.external_id,` + permitted + `,r.version`
	from := ` FROM memory_records r JOIN sources s ON(s.owner_id,s.id)=(r.owner_id,r.id)
 JOIN source_versions v ON(v.owner_id,v.source_id)=(r.owner_id,r.id)
 JOIN record_versions rv ON(rv.owner_id,rv.record_id,rv.version)=(v.owner_id,v.source_id,v.version)
 LEFT JOIN source_contexts c ON(c.owner_id,c.source_id,c.source_version)=(v.owner_id,v.source_id,v.version)
 WHERE r.owner_id=$1 AND r.kind='source' AND r.state='active' AND rv.state='active'`
	scan := func(row pgx.Row) (conversationPoint, error) {
		var p conversationPoint
		p.Kind = memory.SourceKind
		err := row.Scan(&p.ID, &p.Version, &p.Text, &p.Role, &p.ExpressedAt, &p.RecordedAt, &p.title, &p.conversation, &p.branch, &p.external, &p.permitted, &p.currentVersion)
		p.at = p.RecordedAt
		if p.ExpressedAt != nil {
			p.at = *p.ExpressedAt
		}
		return p, err
	}
	anchor, err := scan(tx.QueryRow(ctx, `SELECT `+columns+from+` AND r.id=$2 AND v.version=CASE WHEN $3=0 THEN r.version ELSE $3 END`, string(scope.OwnerID), string(in.ID), in.Version, principal, thingID))
	if errors.Is(err, pgx.ErrNoRows) {
		return out, memory.ErrNotFound
	}
	if err != nil {
		return out, err
	}
	if !anchor.permitted {
		return out, memory.ErrForbidden
	}
	anchor.Anchor = true
	out.Anchor = anchor.Ref
	out.Title = anchor.title
	finish := func() (memory.ConversationResult, error) {
		if principal != "" {
			ids := make([]string, 0, len(out.Messages))
			for _, message := range out.Messages {
				ids = append(ids, string(message.ID))
			}
			rows, err := tx.Query(ctx, `SELECT DISTINCT parent_r.id::text,parent_r.version
 FROM source_contexts child JOIN source_contexts parent
 ON parent.owner_id=child.owner_id AND parent.conversation_key=child.conversation_key AND parent.branch=child.branch
 JOIN sources parent_s ON(parent_s.owner_id,parent_s.id)=(parent.owner_id,parent.source_id)
 JOIN memory_records parent_r ON(parent_r.owner_id,parent_r.id,parent_r.version)=(parent.owner_id,parent.source_id,parent.source_version)
 WHERE child.owner_id=$1 AND child.source_id=ANY($2::uuid[]) AND child.role='assistant' AND child.parent_key<>''
 AND right(parent_s.external_id,length(child.parent_key)+1)='/'||child.parent_key
 AND parent_r.state='active' ORDER BY parent_r.id::text,parent_r.version`, string(scope.OwnerID), ids)
			if err != nil {
				return out, err
			}
			defer rows.Close()
			for rows.Next() {
				ref := memory.Ref{Kind: memory.SourceKind}
				if err := rows.Scan(&ref.ID, &ref.Version); err != nil {
					return out, err
				}
				out.ProofRefs = append(out.ProofRefs, ref)
			}
			if err := rows.Err(); err != nil {
				return out, err
			}
		}
		return out, nil
	}
	if anchor.Version != anchor.currentVersion {
		out.Gaps = append(out.Gaps, contextbuilder.WindowEarlierVersion)
	}
	if anchor.conversation == "" || anchor.branch == "historical" {
		out.Messages = append(out.Messages, anchor.ConversationMessage)
		if anchor.conversation == "" {
			out.Gaps = append(out.Gaps, contextbuilder.WindowNoConversation)
		} else {
			out.Gaps = append(out.Gaps, contextbuilder.WindowHistorical)
		}
		return finish()
	}
	pivot := anchor
	cursor := string(in.Before)
	if cursor == "" {
		cursor = string(in.After)
	}
	if cursor != "" {
		pivot, err = scan(tx.QueryRow(ctx, `SELECT `+columns+from+` AND r.id=$2 AND v.version=r.version AND c.conversation_key=$3 AND c.branch=$6`, string(scope.OwnerID), cursor, anchor.conversation, principal, thingID, anchor.branch))
		if errors.Is(err, pgx.ErrNoRows) {
			return out, memory.ErrInvalid
		}
		if err != nil {
			return out, err
		}
		if !pivot.permitted {
			return out, memory.ErrForbidden
		}
	}
	limit := in.Limit
	if limit == 0 {
		limit = 7
	}
	before, after := limit/2, limit-1-limit/2
	if in.Before != "" {
		before, after = limit, 0
	}
	if in.After != "" {
		before, after = 0, limit
	}
	read := func(count int, earlier bool) ([]conversationPoint, bool, error) {
		result := []conversationPoint{}
		if count == 0 {
			return result, false, nil
		}
		op, order := ">", "ASC"
		if earlier {
			op, order = "<", "DESC"
		}
		sql := `SELECT ` + columns + from + ` AND v.version=r.version AND c.conversation_key=$2 AND c.branch=$3
   AND (coalesce(rv.expressed_at,rv.recorded_at),s.external_id,r.id) ` + op + ` ($6::timestamptz,$7::text,$8::uuid)
   ORDER BY coalesce(rv.expressed_at,rv.recorded_at) ` + order + `,s.external_id ` + order + `,r.id ` + order + ` LIMIT $9`
		rows, err := tx.Query(ctx, sql, string(scope.OwnerID), anchor.conversation, anchor.branch, principal, thingID, pivot.at, pivot.external, string(pivot.ID), count+1)
		if err != nil {
			return nil, false, err
		}
		defer rows.Close()
		for rows.Next() {
			p, err := scan(rows)
			if err != nil {
				return nil, false, err
			}
			result = append(result, p)
		}
		if err := rows.Err(); err != nil {
			return nil, false, err
		}
		more := len(result) > count
		if more {
			result = result[:count]
		}
		if earlier {
			slices.Reverse(result)
		}
		return result, more, nil
	}
	previous, hasBefore, err := read(before, true)
	if err != nil {
		return out, err
	}
	next, hasAfter, err := read(after, false)
	if err != nil {
		return out, err
	}
	points := append(previous, next...)
	if principal != "" {
		for _, p := range points {
			if !p.permitted {
				out.Messages = []memory.ConversationMessage{anchor.ConversationMessage}
				out.Gaps = append(out.Gaps, contextbuilder.WindowPartial)
				return finish()
			}
		}
	}
	for _, p := range previous {
		out.Messages = append(out.Messages, p.ConversationMessage)
	}
	if in.Before == "" && in.After == "" {
		out.Messages = append(out.Messages, anchor.ConversationMessage)
	}
	for _, p := range next {
		out.Messages = append(out.Messages, p.ConversationMessage)
	}
	if hasBefore && len(out.Messages) > 0 {
		out.Before = out.Messages[0].ID
	}
	if hasAfter && len(out.Messages) > 0 {
		out.After = out.Messages[len(out.Messages)-1].ID
	}
	return finish()
}

type (
	EvidenceContextClaim = contextbuilder.Claim
	EvidenceContextGroup = contextbuilder.EvidenceGroup
)

// EvidenceContextsTx gives the source conversations of the claims that the
// principal may read. contextbuilder.Evidence decides what fits.
func EvidenceContextsTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, principal string, thingID *string, claims []EvidenceContextClaim) ([]EvidenceContextGroup, []string, error) {
	src := evidenceSource{tx: tx, scope: scope, principal: principal, thing: thingID}
	if enabled, _ := ctx.Value(UseEvidenceBatchKey{}).(bool); enabled {
		anchors, err := useEvidenceAnchorsTx(ctx, tx, scope, claims)
		if err != nil {
			return nil, nil, err
		}
		src.anchors = anchors
	}
	return contextbuilder.Evidence(ctx, src, claims)
}

// evidenceSource reads anchors and windows in the transaction of a turn or a
// run. anchors holds the anchors read at once for a memory use, or nil.
type evidenceSource struct {
	tx        pgx.Tx
	scope     memory.Scope
	principal string
	thing     *string
	anchors   map[memory.Ref]useEvidenceAnchor
}

func (e evidenceSource) Anchor(ctx context.Context, ref memory.Ref) (contextbuilder.EvidenceAnchor, bool, error) {
	var a useEvidenceAnchor
	if e.anchors != nil {
		var ok bool
		a, ok = e.anchors[ref]
		if !ok {
			return contextbuilder.EvidenceAnchor{}, false, nil
		}
	} else {
		err := e.tx.QueryRow(ctx, `SELECT e.source_id::text,e.source_version,coalesce((e.locator->>'start_rune')::int,0),coalesce((e.locator->>'end_rune')::int,0)
   FROM evidence e JOIN memory_records r ON(r.owner_id,r.id,r.version)=(e.owner_id,e.source_id,e.source_version)
   JOIN record_versions rv ON(rv.owner_id,rv.record_id,rv.version)=(r.owner_id,r.id,r.version)
   JOIN source_contexts c ON(c.owner_id,c.source_id,c.source_version)=(r.owner_id,r.id,r.version)
   WHERE e.owner_id=$1 AND e.target_id=$2 AND e.target_version=$3 AND r.state='active' AND rv.state='active' AND c.conversation_key<>'' AND c.branch<>'historical'
   ORDER BY e.id LIMIT 1`, string(e.scope.OwnerID), string(ref.ID), ref.Version).Scan(&a.source, &a.version, &a.start, &a.end)
		if errors.Is(err, pgx.ErrNoRows) {
			return contextbuilder.EvidenceAnchor{}, false, nil
		}
		if err != nil {
			return contextbuilder.EvidenceAnchor{}, false, err
		}
	}
	return contextbuilder.EvidenceAnchor{Source: memory.Ref{ID: memory.ID(a.source), Version: a.version, Kind: memory.SourceKind}, Start: a.start, End: a.end}, true, nil
}

func (e evidenceSource) Window(ctx context.Context, source memory.Ref) (memory.ConversationResult, error) {
	return conversationWindowTx(ctx, e.tx, e.scope, memory.ConversationRequest{ID: source.ID, Version: source.Version}, e.principal, e.thing)
}
