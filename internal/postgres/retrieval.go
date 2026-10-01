package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
)

func normalizedBudget(b memory.Budget) (memory.Budget, error) {
	if b.Candidates < 0 || b.Candidates > 1000 || b.Edges < 0 || b.Edges > 1000 || b.Tokens < 0 || b.Tokens > 32000 || b.Hops < 0 || b.Hops > 3 {
		return b, memory.ErrInvalid
	}
	if b.Candidates == 0 {
		b.Candidates = 30
	}
	if b.Edges == 0 {
		b.Edges = 30
	}
	if b.Tokens == 0 {
		b.Tokens = 4000
	}
	if b.Hops == 0 {
		b.Hops = 1
	}
	return b, nil
}
func coverage() memory.Coverage {
	return memory.Coverage{Complete: true, Gaps: []string{}, PendingSources: []memory.ID{}}
}
func (s *Store) Recall(ctx context.Context, scope memory.Scope, in memory.RecallRequest) (memory.RecallResult, error) {
	out := memory.RecallResult{Memories: []memory.Ref{}, Evidence: []memory.Evidence{}, Unresolved: []string{}, Coverage: coverage(), FollowUps: []string{}}
	if !scope.Valid() {
		return out, memory.ErrForbidden
	}
	if len(in.Query) > 4000 || !oneOf(string(in.Mode), "continue", "remember", "history") {
		return out, memory.ErrInvalid
	}
	b, err := normalizedBudget(in.Budget)
	if err != nil {
		return out, err
	}
	for _, id := range in.Context.Objects {
		if !id.Valid() {
			return out, memory.ErrInvalid
		}
	}
	key := in
	key.Cursor = ""
	fingerprint := fmt.Sprintf("%x", sha256.Sum256(asJSON(key)))
	offset := 0
	if in.Cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(in.Cursor)
		if err != nil {
			return out, memory.ErrInvalid
		}
		parts := strings.Split(string(raw), ":")
		if len(parts) != 2 || parts[0] != fingerprint {
			return out, memory.ErrInvalid
		}
		offset, err = strconv.Atoi(parts[1])
		if err != nil || offset < 0 || offset > 1000000 {
			return out, memory.ErrInvalid
		}
	}
	query := strings.TrimSpace(in.Query)
	if query == "" {
		query = strings.TrimSpace(in.Context.Text)
	}
	tokens := memory.SearchTokens(query)
	if len(tokens) > 120 {
		tokens = tokens[:120]
	}
	terms := []string{}
	for _, token := range tokens {
		if len([]rune(token)) > 1 || len(tokens) == 1 {
			terms = append(terms, "'"+strings.ReplaceAll(token, "'", "''")+"'")
		}
	}
	fts := strings.Join(terms, " | ")
	// Query-time embedding is optional. Missing semantic coverage is explicit.
	var vector []byte
	var model string
	if query != "" && s.models != nil && s.models.EmbeddingID() != "" {
		provider, _ := s.models.Get(s.models.EmbeddingID())
		var embeddings []memory.Embedding
		var e error
		if !s.models.Available(provider.ID) {
			e = memory.ErrUnavailable
		} else {
			e = s.reserveModelCost(ctx, scope.OwnerID, float64(len(query)+16)*provider.InputPerMillion/1e6, nil)
		}
		if e == nil {
			embeddings, e = s.models.EmbedProvider(ctx, provider, []string{provider.EmbeddingQueryPrefix + query})
		}
		if e == nil && len(embeddings) == 1 {
			vector = asJSON(embeddings[0].Values)
			model = embeddings[0].Model
		} else {
			out.Coverage.Gaps = append(out.Coverage.Gaps, "语义检索暂不可用，已使用原文与属性检索")
		}
	} else {
		out.Coverage.Gaps = append(out.Coverage.Gaps, "未配置语义索引；模糊措辞的覆盖尚不完整")
	}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		return s.recallTx(ctx, tx, scope, in, b, query, fts, vector, model, offset, fingerprint, tokens, &out)
	})
	if len(out.Coverage.PendingSources) > 0 {
		out.Coverage.Gaps = append(out.Coverage.Gaps, "部分资料尚未完成索引或抽取；可通过原文继续追溯")
	}
	if len(out.Coverage.Gaps) > 0 {
		out.Coverage.Complete = false
	}
	if len(out.Memories) == 0 {
		out.FollowUps = append(out.FollowUps, "减少日期或属性限制，使用原文关键词再查")
	}
	return out, err
}

// recallTx shares the scoped, ranked retrieval path with queued runs. Callers
// already holding the owner transaction must not open another transaction or
// reserve model cost; they use lexical/graph retrieval when no vector is supplied.
func (s *Store) recallTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, in memory.RecallRequest, b memory.Budget, query, fts string, vector []byte, model string, offset int, fingerprint string, tokens []string, out *memory.RecallResult) error {
	rows, err := tx.Query(ctx, `WITH linked AS (SELECT m.member_id FROM episode_members m JOIN memory_records e ON(e.owner_id,e.id,e.version)=(m.owner_id,m.episode_id,m.episode_version) JOIN episodes ep ON(ep.owner_id,ep.id,ep.version)=(e.owner_id,e.id,e.version) WHERE m.owner_id=$1 AND e.state='active' AND (m.episode_id=ANY($8::uuid[]) OR ($6='history' AND $4!='' AND position(lower($4) in lower(ep.title))>0)) AND ($2 OR EXISTS(SELECT 1 FROM record_grants g WHERE g.owner_id=e.owner_id AND g.record_id=e.id AND g.principal_id=$3)))
 SELECT t.id::text,t.version,r.kind,coalesce(hit.body,t.body),
		 (CASE WHEN $4='' THEN 0 WHEN position(lower($4) in lower(t.body))>0 THEN 5 ELSE 0 END
		 +CASE WHEN $5='' THEN 0 ELSE coalesce(ts_rank_cd(rs.search_vector,to_tsquery('simple',$5)),0) END
         +(SELECT count(*) FROM unnest($16::text[]) token WHERE length(token)>1 AND position(lower(token) in lower(t.body))>0)::float
		 +CASE WHEN $11::text IS NULL THEN 0 ELSE coalesce((SELECT max(1-(e.embedding <=> $11::vector)) FROM embeddings e WHERE e.owner_id=t.owner_id AND ((e.record_id,e.record_version)=(t.id,t.version) OR e.record_id IN (SELECT id FROM chunks WHERE owner_id=t.owner_id AND source_id=t.id AND source_version=t.version)) AND e.model=$12 AND e.dimensions=$13),0) END
		 +CASE WHEN t.id=ANY($8::uuid[]) OR t.id IN (SELECT claim_id FROM claim_revisions WHERE owner_id=$1 AND (subject_id=ANY($8::uuid[]) OR scope->>'project_id'=ANY($8::text[]))) THEN 10 ELSE 0 END
		 +CASE WHEN $6='continue' THEN coalesce(CASE WHEN a.pinned THEN 1 ELSE exp(-0.693147*greatest(0,extract(epoch from(now()-a.last_effective_use_at)))/(a.half_life_seconds*a.stability)) END,0)*0.2 ELSE 0 END) AS score,coalesce(sc.role,''),coalesce(sc.branch,''),coalesce(sc.gaps,'[]'),hit.start_rune,hit.end_rune
		 FROM memory_text t JOIN memory_records r ON (r.owner_id,r.id)=(t.owner_id,t.id)
		 JOIN record_versions rv ON (rv.owner_id,rv.record_id,rv.version)=(t.owner_id,t.id,t.version)
		 LEFT JOIN record_search rs ON (rs.owner_id,rs.record_id,rs.record_version)=(t.owner_id,t.id,t.version)

        LEFT JOIN LATERAL (
          SELECT '[片段 ' || c.ordinal::text || '；字符 ' || c.start_rune::text || '-' || c.end_rune::text || '] ' || c.body AS body,c.start_rune,c.end_rune
          FROM chunks c JOIN record_versions cv ON (cv.owner_id,cv.record_id,cv.version)=(c.owner_id,c.id,c.version)
          JOIN memory_records cr ON (cr.owner_id,cr.id)=(c.owner_id,c.id)
          WHERE r.kind='source' AND c.owner_id=t.owner_id AND c.source_id=t.id AND c.source_version=t.version
            AND cv.state='active' AND cr.state='active'
            AND (($4!='' AND position(lower($4) in lower(c.body))>0)
              OR EXISTS(SELECT 1 FROM unnest($16::text[]) token WHERE length(token)>1 AND position(lower(token) in lower(c.body))>0)
              OR ($5!='' AND c.search_vector @@ to_tsquery('simple',$5))
              OR ($11::text IS NOT NULL AND EXISTS(SELECT 1 FROM embeddings e WHERE (e.owner_id,e.record_id,e.record_version)=(c.owner_id,c.id,c.version) AND e.model=$12 AND CASE WHEN e.dimensions=$13 THEN (e.embedding <=> $11::vector)<0.65 ELSE false END)))
          ORDER BY
            (CASE WHEN $4!='' AND position(lower($4) in lower(c.body))>0 THEN 5 ELSE 0 END
             +CASE WHEN $5='' THEN 0 ELSE ts_rank_cd(c.search_vector,to_tsquery('simple',$5)) END
             + (SELECT count(*) FROM unnest($16::text[]) token WHERE length(token)>1 AND position(lower(token) in lower(c.body))>0)::float
             +CASE WHEN $11::text IS NULL THEN 0 ELSE coalesce((SELECT max(CASE WHEN e.dimensions=$13 THEN 1-(e.embedding <=> $11::vector) ELSE 0 END) FROM embeddings e WHERE (e.owner_id,e.record_id,e.record_version)=(c.owner_id,c.id,c.version) AND e.model=$12),0) END) DESC,
            c.ordinal
          LIMIT 1
        ) hit ON true
		 LEFT JOIN activity a ON (a.owner_id,a.record_id)=(t.owner_id,t.id)
 LEFT JOIN source_contexts sc ON(sc.owner_id,sc.source_id,sc.source_version)=(t.owner_id,t.id,t.version)
		 WHERE t.owner_id=$1 AND r.state='active' AND rv.state='active' AND ($2 OR EXISTS(SELECT 1 FROM record_grants g WHERE g.owner_id=t.owner_id AND g.record_id=t.id AND g.principal_id=$3))
		 AND ($6='history' OR (r.kind='claim' AND t.version=(SELECT a.version FROM applicable_claim_versions($1,coalesce($9,now()),coalesce($10,now())) a WHERE a.claim_id=t.id)) OR (r.kind!='claim' AND t.version=(SELECT max(v.version) FROM record_versions v WHERE v.owner_id=t.owner_id AND v.record_id=t.id AND v.recorded_at<=coalesce($10,now()))))
		 AND ($9::timestamptz IS NULL OR (rv.valid_from IS NULL OR rv.valid_from<=$9) AND (rv.valid_to IS NULL OR rv.valid_to>$9))
		 AND ($10::timestamptz IS NULL OR rv.recorded_at<=$10)
		 AND ($4='' OR EXISTS(SELECT 1 FROM unnest($16::text[]) token WHERE length(token)>1 AND position(lower(token) in lower(t.body))>0) OR position(lower($4) in lower(t.body))>0 OR ($5!='' AND (coalesce(rs.search_vector,to_tsvector('simple',$7)) @@ to_tsquery('simple',$5)))
		 OR t.id IN (SELECT member_id FROM linked) OR t.id=ANY($8::uuid[]) OR t.id IN (SELECT claim_id FROM claim_revisions WHERE owner_id=$1 AND (subject_id=ANY($8::uuid[]) OR scope->>'project_id'=ANY($8::text[])))
		 OR ($11::text IS NOT NULL AND EXISTS(SELECT 1 FROM embeddings e WHERE e.owner_id=t.owner_id AND ((e.record_id,e.record_version)=(t.id,t.version) OR e.record_id IN (SELECT id FROM chunks WHERE owner_id=t.owner_id AND source_id=t.id AND source_version=t.version)) AND e.model=$12 AND CASE WHEN e.dimensions=$13 THEN (e.embedding <=> $11::vector)<0.65 ELSE false END)))
		 ORDER BY CASE WHEN $6='history' THEN rv.recorded_at END,t.id=ANY($8::uuid[]) DESC,score DESC,t.id,t.version
		 LIMIT $14 OFFSET $15`, string(scope.OwnerID), scope.IsOwner, scope.PrincipalID, query, fts, string(in.Mode), "", in.Context.Objects, in.Context.ValidAt, in.Context.KnownAt, nullString(string(vector)), model, embeddingDimensions(vector), b.Candidates+1, offset, tokens)
	if err != nil {
		return err
	}
	type candidate struct {
		ref          memory.Ref
		text         string
		role, branch string
		gaps         []string
		start, end   *int
	}
	candidates := []candidate{}
	for rows.Next() {
		var c candidate
		var score float64
		if err := rows.Scan(&c.ref.ID, &c.ref.Version, &c.ref.Kind, &c.text, &score, &c.role, &c.branch, &c.gaps, &c.start, &c.end); err != nil {
			rows.Close()
			return err
		}
		candidates = append(candidates, c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	var summary strings.Builder
	consumed := 0
	for _, c := range candidates {
		ref, text, role, branch, gaps := c.ref, c.text, c.role, c.branch, c.gaps
		allowed, err := contextReadAllowsTx(ctx, tx, scope, ref)
		if err != nil {
			return err
		}
		if !allowed {
			out.Coverage.Complete = false
			out.Coverage.Gaps = append(out.Coverage.Gaps, "unavailable_evidence")
			continue
		}

		if role != "" || branch != "" {
			text = "[原文角色=" + role + "；分支=" + branch + "] " + text
		}
		out.Coverage.Gaps = append(out.Coverage.Gaps, gaps...)
		if consumed >= b.Candidates || summary.Len()+len(text) > b.Tokens*3 && consumed > 0 {
			out.Coverage.Complete = false
			out.Coverage.NextCursor = base64.RawURLEncoding.EncodeToString([]byte(fingerprint + ":" + strconv.Itoa(offset+consumed)))
			break
		}
		if len([]rune(text)) > b.Tokens {
			text = matchedExcerpt(text, query, tokens, b.Tokens)
			out.Coverage.Gaps = append(out.Coverage.Gaps, "部分原文超出上下文预算，请展开来源")
		}
		fmt.Fprintf(&summary, "[%s@%d] %s\n", ref.ID, ref.Version, text)
		out.Memories = append(out.Memories, ref)
		if ref.Kind == memory.SourceKind && c.start != nil && c.end != nil {
			out.SourceSpans = append(out.SourceSpans, memory.SourceSpan{Source: ref, StartRune: *c.start, EndRune: *c.end})
		}
		consumed++
	}
	out.Summary = summary.String()
	relations, related, err := graphTx(ctx, tx, scope, out.Memories, b, in.Mode == memory.History, in.Context)
	if err != nil {
		return err
	}
	for _, relation := range relations {
		fromOK, e := contextReadAllowsTx(ctx, tx, scope, relation.From)
		if e != nil {
			return e
		}
		toOK, e := contextReadAllowsTx(ctx, tx, scope, relation.To)
		if e != nil {
			return e
		}
		if !fromOK || !toOK {
			continue
		}
		ev, err := evidenceTx(ctx, tx, scope, relation.Ref)
		if err != nil {
			return err
		}
		out.Evidence = append(out.Evidence, ev...)
	}
	for _, ref := range related {
		allowed, e := contextReadAllowsTx(ctx, tx, scope, ref)
		if e != nil {
			return e
		}
		if !allowed {
			continue
		}
		if len(out.Memories) >= b.Candidates {
			out.Coverage.Gaps = append(out.Coverage.Gaps, "关系展开达到候选预算，可继续展开相关对象")
			break
		}
		var body string
		err := tx.QueryRow(ctx, "SELECT body FROM memory_text WHERE owner_id=$1 AND id=$2 AND version=$3", string(scope.OwnerID), string(ref.ID), ref.Version).Scan(&body)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		if len(out.Summary)+len(body) > b.Tokens*3 {
			break
		}
		out.Memories = append(out.Memories, ref)
		out.Summary += fmt.Sprintf("[%s@%d] %s\n", ref.ID, ref.Version, body)
	}
	for _, ref := range out.Memories {
		evidence, err := evidenceTx(ctx, tx, scope, ref)
		if err != nil {
			return err
		}
		out.Evidence = append(out.Evidence, evidence...)
	}
	pending, err := tx.Query(ctx, `SELECT DISTINCT j.record_id::text FROM memory_jobs j WHERE j.owner_id=$1 AND j.state!='done' AND ($2 OR EXISTS(SELECT 1 FROM record_grants g WHERE g.owner_id=j.owner_id AND g.record_id=j.record_id AND g.principal_id=$3)) LIMIT 100`, string(scope.OwnerID), scope.IsOwner, scope.PrincipalID)
	if err != nil {
		return err
	}
	pendingIDs := []memory.ID{}
	for pending.Next() {
		var id memory.ID
		if err := pending.Scan(&id); err != nil {
			pending.Close()
			return err
		}
		pendingIDs = append(pendingIDs, id)
	}
	err = pending.Err()
	pending.Close()
	if err != nil {
		return err
	}
	for _, id := range pendingIDs {
		var version int
		if err = tx.QueryRow(ctx, "SELECT version FROM memory_records WHERE owner_id=$1 AND id=$2 AND state='active'", string(scope.OwnerID), string(id)).Scan(&version); errors.Is(err, pgx.ErrNoRows) {
			continue
		} else if err != nil {
			return err
		}
		allowed, e := contextEvidenceAllowsTx(ctx, tx, scope, memory.Ref{ID: id, Kind: memory.SourceKind, Version: version})
		if e != nil {
			return e
		}
		if allowed {
			out.Coverage.PendingSources = append(out.Coverage.PendingSources, id)
		}
	}
	return nil
}

func embeddingDimensions(vector []byte) int {
	var values []float32
	_ = json.Unmarshal(vector, &values)
	return len(values)
}
func evidenceTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, ref memory.Ref) ([]memory.Evidence, error) {
	result := []memory.Evidence{}
	rows, err := tx.Query(ctx, `SELECT e.id::text,e.source_id::text,e.source_version,e.locator,e.acquisition,e.stance FROM evidence e JOIN memory_records r ON (r.owner_id,r.id)=(e.owner_id,e.source_id)
		WHERE e.owner_id=$1 AND e.target_id=$2 AND e.target_version=$3 AND r.state='active' AND ($4 OR EXISTS(SELECT 1 FROM record_grants g WHERE g.owner_id=e.owner_id AND g.record_id=e.source_id AND g.principal_id=$5)) ORDER BY e.id`, string(scope.OwnerID), string(ref.ID), ref.Version, scope.IsOwner, scope.PrincipalID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		e := memory.Evidence{Source: memory.Ref{Kind: memory.SourceKind}, Target: ref}
		if err := rows.Scan(&e.ID, &e.Source.ID, &e.Source.Version, &e.Locator, &e.Acquisition, &e.Stance); err != nil {
			rows.Close()
			return nil, err
		}
		result = append(result, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	filtered := []memory.Evidence{}
	for _, e := range result {
		allowed, err := contextEvidenceAllowsTx(ctx, tx, scope, e.Source)
		if err != nil {
			return nil, err
		}
		if allowed {
			filtered = append(filtered, e)
		}
	}
	return filtered, nil
}
func (s *Store) Expand(ctx context.Context, scope memory.Scope, in memory.ExpandRequest) (memory.ExpandResult, error) {
	out := memory.ExpandResult{Entities: []memory.Entity{}, Episodes: []memory.Episode{}, Sources: []memory.Source{}, Claims: []memory.Claim{}, Relations: []memory.Relation{}, Evidence: []memory.Evidence{}, Coverage: coverage()}
	if !scope.Valid() {
		return out, memory.ErrForbidden
	}
	b, err := normalizedBudget(in.Budget)
	if err != nil || len(in.Refs) == 0 || len(in.Refs) > 100 {
		return out, memory.ErrInvalid
	}
	// Expansion pages count actual versions, rather than silently truncating history.
	offset := 0
	if in.Cursor != "" {
		offset, err = strconv.Atoi(in.Cursor)
		if err != nil || offset < 0 {
			return out, memory.ErrInvalid
		}
	}
	returnOutErr := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		refs := []memory.Ref{}
		for _, ref := range in.Refs {
			if !ref.ID.Valid() || ref.Version < 0 || ref.Kind == "" {
				return memory.ErrInvalid
			}
			rows, err := tx.Query(ctx, `SELECT r.kind,rv.version FROM memory_records r JOIN record_versions rv ON (rv.owner_id,rv.record_id)=(r.owner_id,r.id) WHERE r.owner_id=$1 AND r.id=$2 AND r.state='active' AND rv.state='active' AND ($3 OR EXISTS(SELECT 1 FROM record_grants g WHERE g.owner_id=r.owner_id AND g.record_id=r.id AND g.principal_id=$4)) AND ($5 OR rv.version=CASE WHEN $6=0 THEN r.version ELSE $6 END) ORDER BY rv.version`, string(scope.OwnerID), string(ref.ID), scope.IsOwner, scope.PrincipalID, in.History, ref.Version)
			if err != nil {
				return err
			}
			for rows.Next() {
				var r memory.Ref
				r.ID = ref.ID
				if err := rows.Scan(&r.Kind, &r.Version); err != nil {
					rows.Close()
					return err
				}
				if ref.Kind != r.Kind {
					rows.Close()
					return memory.ErrInvalid
				}
				refs = append(refs, r)
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return err
			}
		}
		if len(refs) == 0 {
			return memory.ErrNotFound
		}
		if offset > len(refs) {
			return memory.ErrInvalid
		}
		end := min(len(refs), offset+b.Candidates)
		if end < len(refs) {
			out.Coverage.Complete = false
			out.Coverage.NextCursor = strconv.Itoa(end)
		}
		for _, ref := range refs[offset:end] {
			allowed, err := contextReadAllowsTx(ctx, tx, scope, ref)
			if err != nil {
				return err
			}
			if !allowed {
				out.Coverage.Complete = false
				out.Coverage.Gaps = append(out.Coverage.Gaps, "unavailable_evidence")
				continue
			}
			switch ref.Kind {
			case memory.ClaimKind:
				c, err := readClaim(ctx, tx, scope, ref.ID, ref.Version)
				if err != nil {
					return err
				}
				out.Claims = append(out.Claims, c)
			case memory.EntityKind:
				e, err := readEntity(ctx, tx, scope, ref)
				if err != nil {
					return err
				}
				out.Entities = append(out.Entities, e)
			case memory.EpisodeKind:
				e, err := readEpisode(ctx, tx, scope, ref)
				if err != nil {
					return err
				}
				out.Episodes = append(out.Episodes, e)
			case memory.SourceKind:
				var source memory.Source
				source.Ref = ref
				err := tx.QueryRow(ctx, `SELECT s.connector,s.external_id,v.external_version,v.title,v.body,v.media_type,v.representation,v.blob_key IS NOT NULL,rv.valid_from,rv.valid_to,rv.time_precision,rv.expressed_at,rv.recorded_at,rv.state FROM sources s JOIN source_versions v ON (v.owner_id,v.source_id)=(s.owner_id,s.id) JOIN record_versions rv ON (rv.owner_id,rv.record_id,rv.version)=(v.owner_id,v.source_id,v.version) WHERE s.owner_id=$1 AND s.id=$2 AND v.version=$3`, string(scope.OwnerID), string(ref.ID), ref.Version).Scan(&source.Connector, &source.ExternalID, &source.ExternalVersion, &source.Title, &source.Text, &source.MediaType, &source.Representation, &source.HasAttachment, &source.ValidTime.From, &source.ValidTime.To, &source.ValidTime.Precision, &source.ExpressedAt, &source.RecordedAt, &source.State)
				if err != nil {
					return err
				}
				out.Sources = append(out.Sources, source)
			default:
				out.Coverage.Gaps = append(out.Coverage.Gaps, "此对象请通过关联的经历或原文查看")
			}
			if in.Evidence {
				ev, err := evidenceTx(ctx, tx, scope, ref)
				if err != nil {
					return err
				}
				out.Evidence = append(out.Evidence, ev...)
			}
		}
		relations, _, err := graphTx(ctx, tx, scope, refs[offset:end], b, in.History, memory.WorkingContext{})
		if err != nil {
			return err
		}
		for _, r := range relations {
			a, e := contextReadAllowsTx(ctx, tx, scope, r.From)
			if e != nil {
				return e
			}
			z, e := contextReadAllowsTx(ctx, tx, scope, r.To)
			if e != nil {
				return e
			}
			if a && z {
				out.Relations = append(out.Relations, r)
			}
		}
		if len(out.Coverage.Gaps) > 0 {
			out.Coverage.Complete = false
		}
		return nil
	})
	if errors.Is(returnOutErr, pgx.ErrNoRows) {
		returnOutErr = memory.ErrNotFound
	}
	return out, returnOutErr
}

// matchedExcerpt keeps the answer-bearing span even while chunking is pending.
func matchedExcerpt(text, query string, tokens []string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	lower := strings.ToLower(text)
	at := -1
	if query != "" {
		at = strings.Index(lower, strings.ToLower(query))
	}
	if at < 0 {
		for _, token := range tokens {
			if len([]rune(token)) > 1 {
				if i := strings.Index(lower, strings.ToLower(token)); i >= 0 {
					at = i
					break
				}
			}
		}
	}
	start := 0
	if at >= 0 {
		start = len([]rune(lower[:at])) - min(200, limit/4)
		if start < 0 {
			start = 0
		}
	}
	if start+limit > len(runes) {
		start = len(runes) - limit
	}
	return string(runes[start : start+limit])
}
