package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/modelcall"
	"github.com/soaringjerry/PCAS/internal/prompts"
)

type recallCommandOriginKey struct{}
type recallInputRefsKey struct{}

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
func (s *Store) Recall(ctx context.Context, scope memory.Scope, in memory.RecallRequest) (out memory.RecallResult, err error) {
	out = memory.RecallResult{Memories: []memory.Ref{}, Evidence: []memory.Evidence{}, Unresolved: []string{}, Coverage: coverage(), FollowUps: []string{}}
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
	var invocation memory.ID
	var readExecution *modelcall.Request
	var releaseRead func() error
	if query != "" && s.models != nil && s.models.EmbeddingID() != "" {
		provider, _ := s.models.Get(s.models.EmbeddingID())
		var embeddings []memory.Embedding
		var e error
		reserved := float64(len(query)+16) * provider.InputPerMillion / 1e6
		request, hasParent := ctx.Value(interactiveExecutionKey{}).(modelcall.Request)
		if hasParent && request.OwnerID != scope.OwnerID {
			return out, memory.ErrForbidden
		}
		if !hasParent {
			request, releaseRead, e = (interactiveCalls{store: s}).beginRecall(ctx, scope)
		}
		if releaseRead != nil {
			defer func() {
				if releaseErr := releaseRead(); releaseErr != nil {
					err = errors.Join(err, releaseErr, s.recordRecallFailure(ctx, scope.OwnerID, "read_session_release", releaseErr))
				}
			}()
		}
		if e == nil {
			policy, ok := request.Policy.(interactiveCallPolicy)
			if !ok {
				return out, memory.ErrInvalid
			}
			policy.RecallScope, policy.BudgetOwner, policy.InputPolicy, policy.RetryOf = &scope, "", "", ""
			if policy.AgentID == "" {
				policy.AgentID = scope.PrincipalID
			}
			policy.Usage = modelUsage{Purpose: "query_embedding"}
			if policy.Kind == "deputy" {
				policy.Usage.RunID = string(request.ExecutionID)
			}
			if origin, ok := ctx.Value(recallCommandOriginKey{}).(string); ok {
				policy.CommandOrigin = origin
			}
			request.Policy = policy
			request.Function, request.Stage = "query_embedding", fmt.Sprintf("query:%x", sha256.Sum256(asJSON(map[string]any{"query": query, "scope": recallScopeManifest(scope)})))
			request.Operation, request.ProviderID, request.ProviderSnapshot, request.ReservationEstimate = "embedding", provider.ID, &provider, &reserved
			request.Instructions, request.Schema, request.Search = prompts.Definition{}, prompts.Schema{}, false
			request.ContextBuilderVersion = "recall-query-v1"
			request.Prompt = asJSON([]string{provider.EmbeddingQueryPrefix + query})
			inputRefs, _ := ctx.Value(recallInputRefsKey{}).([]memory.Ref)
			request.Refs = uniqueRefs(append([]memory.Ref(nil), inputRefs...))
			readExecution = &request
			type returned struct {
				paid *modelcall.PaidResult
				err  error
			}
			finished := make(chan returned, 1)
			go func() {
				paid, err := s.calls.Call(executionCallContext(ctx, "query_embedding"), request)
				finished <- returned{paid, err}
			}()
			select {
			case <-ctx.Done():
				e = ctx.Err()
			case result := <-finished:
				e = result.err
				var failure *modelcall.Failure
				if errors.As(e, &failure) {
					invocation = failure.InvocationID
				}
				if result.paid != nil {
					invocation = result.paid.InvocationID
				}
				if e == nil {
					e = json.Unmarshal([]byte(result.paid.Output), &embeddings)
				}
			}
			var persistence *modelcall.PersistenceError
			if errors.As(e, &persistence) {
				return out, e
			}
		}
		if e != nil {
			if recordErr := s.recordRecallFailure(ctx, scope.OwnerID, "semantic_fallback", e); recordErr != nil {
				return out, errors.Join(e, recordErr)
			}
		}
		if e == nil && len(embeddings) == 1 {
			vector = asJSON(embeddings[0].Values)
			model = embeddings[0].Model
		} else {
			out.Coverage.Gaps = append(out.Coverage.Gaps, "语义检索暂不可用，已使用原文与属性检索")
		}
	} else {
		out.Coverage.Gaps = append(out.Coverage.Gaps, "未配置语义索引；模糊措辞的覆盖尚不完整")
		reason := "provider_not_configured"
		if query == "" {
			reason = "empty_query"
		}
		if err := s.recordRecallEvent(ctx, scope.OwnerID, "deferred", reason); err != nil {
			return out, err
		}
	}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		return s.recallTx(ctx, tx, scope, in, b, query, fts, vector, model, offset, fingerprint, tokens, &out)
	})
	if readExecution != nil && invocation != "" {
		outcome, reason := "used", "query_vector_used"
		if err != nil {
			outcome, reason = "not_applicable", "retrieval_failed"
		} else if vector == nil {
			outcome, reason = "not_applicable", "semantic_fallback"
		}
		if finishErr := (interactiveCalls{store: s}).finishRecall(ctx, *readExecution, invocation, outcome, reason); finishErr != nil {
			return out, errors.Join(err, finishErr)
		}
	}
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
	structured := []memory.Ref{}
	if scope.Team && in.Team != nil {
		var err error
		structured, out.TimeRelaxed, err = structuredRecallTx(ctx, tx, scope, *in.Team, b.Candidates)
		if err != nil {
			return err
		}
		out.Structured = structured
		if len(structured) > 0 && in.Team.Candidates > b.Candidates {
			b.Candidates = in.Team.Candidates
		}
	}
	structuredIDs := []memory.ID{}
	for _, ref := range structured {
		structuredIDs = append(structuredIDs, ref.ID)
	}
	// Two steps. First every record is matched and scored, which decides the
	// order; then the passage to show is worked out only for the few that are
	// returned. Choosing a passage for every match, and joins the planner ran
	// as a scan per row, made this take a minute or more over tens of
	// thousands of records.
	querySQL := `WITH ranking_clock AS (SELECT coalesce($18::timestamptz,now()) AS at), applicable AS MATERIALIZED (SELECT claim_id,version FROM applicable_claim_versions($1,coalesce($9,now()),coalesce($10,now())) WHERE $6!='history'), linked AS (SELECT m.member_id FROM episode_members m JOIN memory_records e ON(e.owner_id,e.id,e.version)=(m.owner_id,m.episode_id,m.episode_version) JOIN episodes ep ON(ep.owner_id,ep.id,ep.version)=(e.owner_id,e.id,e.version) WHERE m.owner_id=$1 AND e.state='active' AND (m.episode_id=ANY($8::uuid[]) OR ($6='history' AND $4!='' AND position(lower($4) in lower(ep.title))>0)) AND ($2 OR ($17 AND e.kind='source') OR EXISTS(SELECT 1 FROM record_grants g WHERE g.owner_id=e.owner_id AND g.record_id=e.id AND g.principal_id=$3))), hits AS (
 SELECT t.id::text AS id,t.id AS uid,t.version,r.kind,
		 (CASE WHEN $4='' THEN 0 WHEN position(lower($4) in lb.body)>0 THEN 5 ELSE 0 END
		 +CASE WHEN $5='' THEN 0 ELSE coalesce(ts_rank_cd(rs.search_vector,to_tsquery('simple',$5)),0) END
         +(SELECT count(*) FROM unnest(words.patterns) pattern WHERE lb.body LIKE pattern)::float
		 +CASE WHEN $11::text IS NULL THEN 0 ELSE coalesce(greatest((SELECT max(1-(e.embedding <=> $11::vector)) FROM embeddings e WHERE e.owner_id=t.owner_id AND e.record_id=t.id AND e.record_version=t.version AND e.model=$12 AND e.dimensions=$13),(SELECT max(1-(e.embedding <=> $11::vector)) FROM chunks c JOIN embeddings e ON (e.owner_id,e.record_id)=(c.owner_id,c.id) WHERE c.owner_id=t.owner_id AND c.source_id=t.id AND c.source_version=t.version AND e.model=$12 AND e.dimensions=$13)),0) END
		 +CASE WHEN t.id=ANY($8::uuid[]) OR t.id IN (SELECT claim_id FROM claim_revisions WHERE owner_id=$1 AND (subject_id=ANY($8::uuid[]) OR scope->>'project_id'=ANY($8::text[]))) THEN 10 ELSE 0 END
		 +CASE WHEN $6='continue' THEN coalesce(CASE WHEN a.pinned THEN 1 ELSE exp(-0.693147*greatest(0,extract(epoch from(now()-a.last_effective_use_at)))/(a.half_life_seconds*a.stability)) END,0)*0.2 ELSE 0 END) AS score,
         coalesce(src.connector,'') AS connector,coalesce(src.external_id,'') AS external_id,
         rv.expressed_at,rv.recorded_at,t.id=ANY($8::uuid[]) AS explicit
		 FROM memory_text t JOIN memory_records r ON (r.owner_id,r.id)=(t.owner_id,t.id)
		 LEFT JOIN applicable av ON av.claim_id=t.id
		 CROSS JOIN LATERAL (SELECT lower(t.body) AS body OFFSET 0) lb
		 CROSS JOIN (SELECT coalesce((SELECT array_agg('%'||replace(replace(replace(lower(token),'\','\\'),'%','\%'),'_','\_')||'%') FROM unnest($16::text[]) token WHERE length(token)>1),'{}'::text[]) AS patterns) words
		 JOIN record_versions rv ON (rv.owner_id,rv.record_id,rv.version)=(t.owner_id,t.id,t.version)
		 LEFT JOIN record_search rs ON (rs.owner_id,rs.record_id,rs.record_version)=(t.owner_id,t.id,t.version)

		 LEFT JOIN LATERAL (SELECT x.pinned,x.last_effective_use_at,x.half_life_seconds,x.stability FROM activity x WHERE x.owner_id=t.owner_id AND x.record_id=t.id OFFSET 0) a ON true
 LEFT JOIN LATERAL (SELECT x.connector,x.external_id FROM sources x WHERE x.owner_id=t.owner_id AND x.id=t.id OFFSET 0) src ON true
		 WHERE t.owner_id=$1 AND r.state='active' AND rv.state='active' AND ($2 OR ($17 AND r.kind='source') OR EXISTS(SELECT 1 FROM record_grants g WHERE g.owner_id=t.owner_id AND g.record_id=t.id AND g.principal_id=$3))
		 AND (NOT $17 OR r.kind!='source' OR t.version=r.version)
		 AND ($6='history' OR (r.kind='claim' AND t.version=av.version) OR (r.kind!='claim' AND t.version=(SELECT max(v.version) FROM record_versions v WHERE v.owner_id=t.owner_id AND v.record_id=t.id AND v.recorded_at<=coalesce($10,now()))))
		 AND ($9::timestamptz IS NULL OR (rv.valid_from IS NULL OR rv.valid_from<=$9) AND (rv.valid_to IS NULL OR rv.valid_to>$9))
		 AND ($10::timestamptz IS NULL OR rv.recorded_at<=$10)
		 AND ($4='' OR lb.body LIKE ANY(words.patterns) OR position(lower($4) in lb.body)>0 OR ($5!='' AND (coalesce(rs.search_vector,to_tsvector('simple',$7)) @@ to_tsquery('simple',$5)))
		 OR t.id IN (SELECT member_id FROM linked) OR t.id=ANY($8::uuid[]) OR t.id IN (SELECT claim_id FROM claim_revisions WHERE owner_id=$1 AND (subject_id=ANY($8::uuid[]) OR scope->>'project_id'=ANY($8::text[])))
		 OR ($11::text IS NOT NULL AND (EXISTS(SELECT 1 FROM embeddings e WHERE e.owner_id=t.owner_id AND e.record_id=t.id AND e.record_version=t.version AND e.model=$12 AND CASE WHEN e.dimensions=$13 THEN (e.embedding <=> $11::vector)<0.65 ELSE false END) OR EXISTS(SELECT 1 FROM chunks c JOIN embeddings e ON (e.owner_id,e.record_id)=(c.owner_id,c.id) WHERE c.owner_id=t.owner_id AND c.source_id=t.id AND c.source_version=t.version AND e.model=$12 AND CASE WHEN e.dimensions=$13 THEN (e.embedding <=> $11::vector)<0.65 ELSE false END))))
 )`
	detailSQL := ` SELECT p.id,p.version,p.kind,coalesce(hit.body,t.body) AS body,p.score,coalesce(sc.role,'') AS role,coalesce(sc.branch,'') AS branch,coalesce(sc.gaps,'[]') AS gaps,
         coalesce(hit.excerpt,sv.body,'') AS excerpt,coalesce(sv.title,'') AS title,p.connector,p.external_id,p.expressed_at,p.recorded_at,p.explicit,
         coalesce(btrim(sv.body)!='' AND (sv.media_type LIKE 'text/%' OR sv.representation IN ('ocr','transcript','extracted','vision')),false) AS readable,p.total_count
 FROM picked p
 JOIN LATERAL (SELECT m.body FROM memory_text m WHERE m.owner_id=$1 AND m.id=p.uid AND m.version=p.version) t ON true
        LEFT JOIN LATERAL (
          SELECT '[片段 ' || c.ordinal::text || '；字符 ' || c.start_rune::text || '-' || c.end_rune::text || '] ' || c.body AS body,c.body AS excerpt
          FROM chunks c JOIN record_versions cv ON (cv.owner_id,cv.record_id,cv.version)=(c.owner_id,c.id,c.version)
          JOIN memory_records cr ON (cr.owner_id,cr.id)=(c.owner_id,c.id)
          WHERE p.kind='source' AND c.owner_id=$1 AND c.source_id=p.uid AND c.source_version=p.version
            AND cv.state='active' AND cr.state='active'
          ORDER BY
            (CASE WHEN $4!='' AND position(lower($4) in lower(c.body))>0 THEN 5 ELSE 0 END
             +CASE WHEN $5='' THEN 0 ELSE ts_rank_cd(c.search_vector,to_tsquery('simple',$5)) END
             + (SELECT count(*) FROM unnest($16::text[]) token WHERE length(token)>1 AND position(lower(token) in lower(c.body))>0)::float
             +CASE WHEN $11::text IS NULL THEN 0 ELSE coalesce((SELECT max(CASE WHEN e.dimensions=$13 THEN 1-(e.embedding <=> $11::vector) ELSE 0 END) FROM embeddings e WHERE (e.owner_id,e.record_id,e.record_version)=(c.owner_id,c.id,c.version) AND e.model=$12),0) END) DESC,
            c.ordinal
          LIMIT 1
        ) hit ON true
 LEFT JOIN source_versions sv ON (sv.owner_id,sv.source_id,sv.version)=($1,p.uid,p.version)
 LEFT JOIN source_contexts sc ON(sc.owner_id,sc.source_id,sc.source_version)=($1,p.uid,p.version)`
	querySQL = strings.Replace(querySQL, "WHERE t.owner_id=$1 AND r.state=", "WHERE t.owner_id=$1 AND (r.kind<>'claim' OR "+currentMemorySQL("r.owner_id", "r.id")+") AND r.state=", 1)
	// Only explicit effective-use events activate the prior. Legacy activity
	// timestamps populated from expression time carry no retrieval weight.
	activityTerm := `CASE WHEN a.pinned THEN 1 WHEN EXISTS(SELECT 1 FROM use_events ue WHERE ue.owner_id=t.owner_id AND ue.record_id=t.id AND ue.kind IN ('user_mention','confirmation','adoption')) THEN coalesce(exp(-0.693147*greatest(0,extract(epoch from((SELECT at FROM ranking_clock)-a.last_effective_use_at)))/nullif(a.half_life_seconds*a.stability,0)),0) ELSE 0 END`
	if enabled, ok := ctx.Value(activityRankingKey{}).(bool); ok && !enabled {
		activityTerm = "0::float"
	}
	querySQL = strings.Replace(querySQL, `+CASE WHEN $6='continue' THEN coalesce(CASE WHEN a.pinned THEN 1 ELSE exp(-0.693147*greatest(0,extract(epoch from(now()-a.last_effective_use_at)))/(a.half_life_seconds*a.stability)) END,0)*0.2 ELSE 0 END`, "+0.2*("+activityTerm+")", 1)
	if scope.Team && in.Team != nil && in.Team.RankFusion {
		// Keep independently ranked lexical and vector evidence on the scoped row set.
		vectorTerm := `+CASE WHEN $11::text IS NULL THEN 0 ELSE coalesce(greatest((SELECT max(1-(e.embedding <=> $11::vector)) FROM embeddings e WHERE e.owner_id=t.owner_id AND e.record_id=t.id AND e.record_version=t.version AND e.model=$12 AND e.dimensions=$13),(SELECT max(1-(e.embedding <=> $11::vector)) FROM chunks c JOIN embeddings e ON (e.owner_id,e.record_id)=(c.owner_id,c.id) WHERE c.owner_id=t.owner_id AND c.source_id=t.id AND c.source_version=t.version AND e.model=$12 AND e.dimensions=$13)),0) END`
		querySQL = strings.Replace(querySQL, vectorTerm, "", 1)
		objectMatch := `t.id=ANY($8::uuid[]) OR t.id IN (SELECT claim_id FROM claim_revisions WHERE owner_id=$1 AND (subject_id=ANY($8::uuid[]) OR scope->>'project_id'=ANY($8::text[])))`
		querySQL = strings.Replace(querySQL, `+CASE WHEN `+objectMatch+` THEN 10 ELSE 0 END`, "", 1)
		querySQL = strings.Replace(querySQL, `t.id=ANY($8::uuid[]) AS explicit`, `(`+objectMatch+`) AS explicit`, 1)
		querySQL = strings.Replace(querySQL, "+0.2*("+activityTerm+")", "", 1)

		querySQL = strings.Replace(querySQL, ") AS score,", ") AS lexical_score, "+strings.TrimPrefix(vectorTerm, "+")+" AS vector_score, "+activityTerm+" AS activity_score,", 1)
		querySQL = strings.Replace(querySQL, "hits AS (", "scored AS (", 1)
		querySQL += `, hits AS (SELECT *, CASE WHEN lexical_score>0 THEN 1.0/(60+row_number() OVER (ORDER BY lexical_score DESC,uid,version)) ELSE 0 END + CASE WHEN vector_score>0 THEN 1.0/(60+row_number() OVER (ORDER BY vector_score DESC,uid,version)) ELSE 0 END + 0.002*activity_score AS score FROM scored)`
	}
	if len(structured) > 0 {
		querySQL = strings.Replace(querySQL, "AND ($4='' OR lb.body LIKE ANY", "AND (t.id=ANY($19::uuid[]) OR $4='' OR lb.body LIKE ANY", 1)
	}
	// Team source excerpts have their own candidate/token allowance. They must
	// not displace the existing claim and graph budgets. Public recall retains
	// its original ordering and pagination across all record kinds.
	const hitColumns = "id,version,kind,body,score,role,branch,gaps,excerpt,title,connector,external_id,expressed_at,recorded_at,readable,total_count"
	hitOrder := "CASE WHEN $6='history' THEN recorded_at END,explicit DESC,score DESC,id::uuid,version"
	args := []any{string(scope.OwnerID), scope.IsOwner, scope.PrincipalID, query, fts, string(in.Mode), "", in.Context.Objects, in.Context.ValidAt, in.Context.KnownAt, nullString(string(vector)), model, embeddingDimensions(vector), b.Candidates + 1, offset, tokens, scope.Team}
	// A diagnostic clock fixes decay only. Production retains the transaction's
	// now(), and applicable versions, permissions, and usage keep actual time.
	var activityAt *time.Time
	if s.businessClock != nil {
		at := s.businessNow()
		activityAt = &at
	}
	args = append(args, activityAt)
	if len(structured) > 0 {
		args = append(args, structuredIDs)
		hitOrder = "array_position($19::uuid[],id::uuid) ASC NULLS LAST," + hitOrder
	}
	if scope.Team && in.Team != nil && in.Team.Plan.Time != nil {
		slot := len(args) + 1
		args = append(args, in.Team.Plan.Time.From, in.Team.Plan.Time.To)
		at := "coalesce(expressed_at,CASE WHEN connector IN ('desk','capture','telegram','desk-incomplete') THEN recorded_at END)"
		hitOrder = fmt.Sprintf("CASE WHEN kind='source' THEN coalesce(%s >= $%d AND %s < $%d,false) ELSE false END DESC,", at, slot, at, slot+1) + hitOrder
	}
	if scope.Team {
		querySQL += ", ranked AS (SELECT *,row_number() OVER (PARTITION BY kind='source' ORDER BY " + hitOrder + ") AS rank,count(*) OVER(PARTITION BY kind='source') AS total_count FROM hits), picked AS (SELECT * FROM ranked WHERE rank>$15 AND rank<=$15+$14)"
	} else {
		querySQL += ", picked AS (SELECT *,count(*) OVER() AS total_count FROM hits ORDER BY " + hitOrder + " LIMIT $14 OFFSET $15)"
	}
	querySQL += " SELECT " + hitColumns + " FROM (" + detailSQL + ") shown ORDER BY " + hitOrder

	// Compiling this statement costs the database far more than running it.
	if _, err := tx.Exec(ctx, "SET LOCAL jit = off"); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, querySQL, args...)
	if err != nil {
		return err
	}
	var summary strings.Builder
	consumed := 0
	teamSources := []memory.Ref{}
	claimBudgetFull := false
	totalClaims, totalSources := 0, 0
	for rows.Next() {
		var ref memory.Ref
		var text string
		var score float64
		var role, branch string
		var gaps []string
		var excerpt memory.RecallExcerpt
		var total int
		if err := rows.Scan(&ref.ID, &ref.Version, &ref.Kind, &text, &score, &role, &branch, &gaps, &excerpt.Text, &excerpt.Title, &excerpt.Connector, &excerpt.ExternalID, &excerpt.ExpressedAt, &excerpt.RecordedAt, &excerpt.Readable, &total); err != nil {
			rows.Close()
			return err
		}
		if scope.Team && ref.Kind == memory.SourceKind {
			totalSources = total
		} else {
			totalClaims = total
		}
		_ = score
		excerpt.Ref, excerpt.Role = ref, role
		if ref.Kind == memory.SourceKind {
			excerpt.Text = sourceExcerpt(strings.TrimSpace(excerpt.Text), query, tokens, 600)
		}
		if scope.Team && ref.Kind == memory.SourceKind {
			if len(teamSources) < b.Candidates {
				out.Excerpts = append(out.Excerpts, excerpt)
				teamSources = append(teamSources, ref)
			}
			continue
		}
		if claimBudgetFull {
			continue
		}
		if role != "" || branch != "" {
			text = "[原文角色=" + role + "；分支=" + branch + "] " + text
		}
		out.Coverage.Gaps = append(out.Coverage.Gaps, gaps...)
		if consumed >= b.Candidates || summary.Len()+len(text) > b.Tokens*3 && consumed > 0 {
			out.Coverage.Complete = false
			out.Coverage.NextCursor = base64.RawURLEncoding.EncodeToString([]byte(fingerprint + ":" + strconv.Itoa(offset+consumed)))
			if !scope.Team {
				break
			}
			claimBudgetFull = true
			continue
		}
		if len([]rune(text)) > b.Tokens {
			text = matchedExcerpt(text, query, tokens, b.Tokens)
			out.Coverage.Gaps = append(out.Coverage.Gaps, "部分原文超出上下文预算，请展开来源")
		}
		fmt.Fprintf(&summary, "[%s@%d] %s\n", ref.ID, ref.Version, text)
		out.Memories = append(out.Memories, ref)
		if ref.Kind != memory.SourceKind {
			excerpt.Text = text
		}
		out.Excerpts = append(out.Excerpts, excerpt)
		consumed++
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	out.Coverage.Omitted = max(0, totalClaims-offset-consumed)
	out.Coverage.OmittedSources = max(0, totalSources-offset-len(teamSources))
	out.Summary = summary.String()
	relations, related, err := graphTx(ctx, tx, scope, out.Memories, b, in.Mode == memory.History, in.Context)
	if err != nil {
		return err
	}
	for _, relation := range relations {
		ev, err := evidenceTx(ctx, tx, scope, relation.Ref)
		if err != nil {
			return err
		}
		out.Evidence = append(out.Evidence, ev...)
	}
	for _, ref := range related {
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
	out.Memories = append(out.Memories, teamSources...)
	for _, ref := range out.Memories {
		evidence, err := evidenceTx(ctx, tx, scope, ref)
		if err != nil {
			return err
		}
		out.Evidence = append(out.Evidence, evidence...)
	}
	pending, err := tx.Query(ctx, `SELECT DISTINCT j.record_id::text FROM memory_jobs j WHERE j.owner_id=$1 AND j.state!='done' AND ($2 OR ($4 AND EXISTS(SELECT 1 FROM memory_records r WHERE (r.owner_id,r.id)=(j.owner_id,j.record_id) AND r.kind='source')) OR EXISTS(SELECT 1 FROM record_grants g WHERE g.owner_id=j.owner_id AND g.record_id=j.record_id AND g.principal_id=$3)) LIMIT 100`, string(scope.OwnerID), scope.IsOwner, scope.PrincipalID, scope.Team)
	if err != nil {
		return err
	}
	for pending.Next() {
		var id memory.ID
		if err := pending.Scan(&id); err != nil {
			pending.Close()
			return err
		}
		out.Coverage.PendingSources = append(out.Coverage.PendingSources, id)
	}
	err = pending.Err()
	pending.Close()
	return err
}

func embeddingDimensions(vector []byte) int {
	var values []float32
	_ = json.Unmarshal(vector, &values)
	return len(values)
}
func evidenceTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, ref memory.Ref) ([]memory.Evidence, error) {
	result := []memory.Evidence{}
	rows, err := tx.Query(ctx, `SELECT e.id::text,e.source_id::text,e.source_version,e.locator,e.acquisition,e.stance FROM evidence e JOIN memory_records r ON (r.owner_id,r.id)=(e.owner_id,e.source_id)
		WHERE e.owner_id=$1 AND e.target_id=$2 AND e.target_version=$3 AND r.state='active' AND ($4 OR ($6 AND r.kind='source') OR EXISTS(SELECT 1 FROM record_grants g WHERE g.owner_id=e.owner_id AND g.record_id=e.source_id AND g.principal_id=$5)) ORDER BY e.id`, string(scope.OwnerID), string(ref.ID), ref.Version, scope.IsOwner, scope.PrincipalID, scope.Team)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		e := memory.Evidence{Source: memory.Ref{Kind: memory.SourceKind}, Target: ref}
		if err := rows.Scan(&e.ID, &e.Source.ID, &e.Source.Version, &e.Locator, &e.Acquisition, &e.Stance); err != nil {
			return nil, err
		}
		result = append(result, e)
	}
	return result, rows.Err()
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
			if !ref.ID.Valid() || ref.Version < 0 {
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
		out.Relations = relations
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
	start, end := matchedExcerptRange(text, query, tokens, limit)
	return string([]rune(text)[start:end])
}

func matchedExcerptRange(text, query string, tokens []string, limit int) (int, int) {
	runes := []rune(text)
	if len(runes) <= limit {
		return 0, len(runes)
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
	return start, start + limit
}

// Leave room for the truncation mark within the per-excerpt rune budget.
func sourceExcerpt(text, query string, tokens []string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	start, end := sourceExcerptRange(text, query, tokens, limit-2)
	excerpt := string(runes[start:end])
	if start > 0 {
		excerpt = "…" + excerpt
	}
	if end < len(runes) {
		excerpt += "…"
	}
	return excerpt
}

// Score windows anchored before each hit by distinct query words. Merge each word's
// matching window intervals so repeated occurrences only contribute one vote.
func sourceExcerptRange(text, query string, tokens []string, limit int) (int, int) {
	lower := strings.ToLower(text)
	if query != "" && strings.Contains(lower, strings.ToLower(query)) {
		return matchedExcerptRange(text, query, tokens, limit)
	}
	length := len([]rune(text))
	if length <= limit {
		return 0, length
	}
	// Substring searches use bytes; all window bounds use Unicode characters.
	positions := make([]int, len(lower)+1)
	character := 0
	for byteIndex := range lower {
		positions[byteIndex] = character
		character++
	}
	positions[len(lower)] = character
	lastStart := length - limit
	votes := make([]int, lastStart+2)
	candidates := make([]bool, lastStart+1)
	prefix := min(200, limit/4)
	seen := map[string]bool{}
	matches := 0
	for _, token := range tokens {
		token = strings.ToLower(token)
		width := len([]rune(token))
		if width <= 1 || seen[token] {
			continue
		}
		seen[token] = true
		left, right := -1, -1
		for offset := 0; offset < len(lower); {
			found := strings.Index(lower[offset:], token)
			if found < 0 {
				break
			}
			at := offset + found
			offset = at + 1
			matches++
			candidates[min(max(0, positions[at]-prefix), lastStart)] = true
			first := max(0, positions[at]+width-limit)
			last := min(positions[at], lastStart)
			if first > last {
				continue
			}
			if left < 0 {
				left, right = first, last
			} else if first <= right+1 {
				right = max(right, last)
			} else {
				votes[left]++
				votes[right+1]--
				left, right = first, last
			}
		}
		if left >= 0 {
			votes[left]++
			votes[right+1]--
		}
	}
	// Keep the existing amount of preceding context for a single hit.
	if matches == 1 {
		return matchedExcerptRange(text, query, tokens, limit)
	}
	start, best, score := 0, -1, 0
	for candidate := 0; candidate <= lastStart; candidate++ {
		score += votes[candidate]
		if candidates[candidate] && score > best {
			start, best = candidate, score
		}
	}
	return start, start + limit
}

// teamSourceVisibleSQL follows explicit visibility and item exclusions on any
// currently applicable claim evidenced by the source. Its arguments are trusted
// SQL expressions, never request values; category and project filters do not apply.
// Keep the temporal lookup correlated to this source's evidence targets.
// Pulling it into a user-wide join repeated all current claims for each window
// and each source dependency verification. OFFSET 0 preserves the bounded plan.
func teamSourceVisibleSQL(ownerID, sourceID, principalID, thingID string) string {
	return fmt.Sprintf(`NOT EXISTS (
 SELECT 1 FROM evidence source_evidence
 JOIN LATERAL (
 SELECT current_claim.claim_id,current_claim.version
 FROM applicable_claim_versions(%[1]s,now(),now()) current_claim
 WHERE current_claim.claim_id=source_evidence.target_id OFFSET 0
 ) source_claim ON source_claim.version=source_evidence.target_version
 WHERE source_evidence.owner_id=%[1]s AND source_evidence.source_id=%[2]s
 AND (NOT EXISTS (SELECT 1 FROM record_grants source_grant
  WHERE source_grant.owner_id=source_evidence.owner_id AND source_grant.record_id=source_evidence.target_id AND source_grant.principal_id=%[3]s)
 OR EXISTS (SELECT 1 FROM context_exclusions source_exclusion
  WHERE source_exclusion.owner_id=source_evidence.owner_id AND source_exclusion.memory_id=source_evidence.target_id AND source_exclusion.thing_id=%[4]s::uuid))
)`, ownerID, sourceID, principalID, thingID)
}

// Pick one current, readable excerpt per source, preserving retrieval rank.
// Recheck only record metadata here: the matched text is already in Recall.
func teamSourceExcerptsTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, principal string, thingID *string, excerpts []memory.RecallExcerpt, historyRequests []string, maxSegments, maxCharacters int) ([]memory.RecallExcerpt, error) {
	ids := []memory.ID{}
	for _, excerpt := range excerpts {
		if excerpt.Kind == memory.SourceKind {
			ids = append(ids, excerpt.ID)
		}
	}
	selected := []memory.RecallExcerpt{}
	if len(ids) == 0 {
		return selected, nil
	}
	rows, err := tx.Query(ctx, `SELECT r.id::text,r.version FROM memory_records r
 JOIN record_versions v ON (v.owner_id,v.record_id,v.version)=(r.owner_id,r.id,r.version)
 WHERE r.owner_id=$1 AND r.id=ANY($2::uuid[]) AND r.kind='source' AND r.state='active' AND v.state='active'
 AND `+teamSourceVisibleSQL("$1", "r.id", "$3", "$4"), string(scope.OwnerID), ids, principal, thingID)
	if err != nil {
		return nil, err
	}
	current := map[memory.Ref]bool{}
	for rows.Next() {
		ref := memory.Ref{Kind: memory.SourceKind}
		if err := rows.Scan(&ref.ID, &ref.Version); err != nil {
			rows.Close()
			return nil, err
		}
		current[ref] = true
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	history := map[string]bool{}
	for _, requestID := range historyRequests {
		if requestID != "" {
			history[strings.ToLower(requestID)] = true
		}
	}
	seen := map[memory.ID]bool{}
	characters := 0
	omitted := 0
	for _, excerpt := range excerpts {
		if excerpt.Kind != memory.SourceKind || seen[excerpt.ID] || !current[excerpt.Ref] || !excerpt.Readable || strings.TrimSpace(excerpt.Text) == "" || oneOf(excerpt.Connector, "actions", "corrections", "memory-input") {
			continue
		}
		if oneOf(excerpt.Connector, "desk", "capture", "desk-incomplete") && history[strings.ToLower(excerpt.ExternalID)] {
			continue
		}
		if excerpt.Connector == "desk" {
			undone, err := deskTurnFullyUndoneTx(ctx, tx, scope.OwnerID, excerpt.ExternalID)
			if err != nil {
				return nil, err
			}
			if undone {
				continue
			}
		}
		seen[excerpt.ID] = true
		count := len([]rune(excerpt.Text))
		if len(selected) >= maxSegments || characters+count > maxCharacters {
			omitted++
			continue
		}
		selected = append(selected, excerpt)
		characters += count
	}
	if omitted > 0 {
		if err := stageEventTx(ctx, tx, scope.OwnerID, "secretary", "overflow", "source_excerpt_budget", omitted); err != nil {
			return nil, err
		}
	}
	return selected, nil
}

func sourceExcerptTime(excerpt memory.RecallExcerpt) (time.Time, string) {
	if excerpt.ExpressedAt != nil {
		return *excerpt.ExpressedAt, "说于"
	}
	return excerpt.RecordedAt, "记录于"
}

// Enumerate only the alias lengths present for this owner, then probe the
// existing (owner_id, lower(alias)) index with exact substrings. The lateral
// boundary keeps probes parameterized instead of scanning all active entities. We never run
// position(message, alias) for every entity; work is message windows plus one
// owner-scoped pass over alias lengths, independent of memory count.
const teamEntitiesSQL = `WITH lengths AS MATERIALIZED (
 SELECT DISTINCT char_length(alias) AS n FROM aliases
 WHERE owner_id=$1 AND char_length(alias) BETWEEN 2 AND char_length($2)
), names AS MATERIALIZED (
 SELECT DISTINCT lower(substr($2,p,n)) AS name FROM lengths
 CROSS JOIN LATERAL generate_series(1,char_length($2)-n+1) AS p
)
SELECT DISTINCT a.entity_id::text FROM names
JOIN LATERAL (
 SELECT owner_id,entity_id,entity_version FROM aliases
 WHERE owner_id=$1 AND lower(alias)=names.name OFFSET 0
) a ON true
JOIN memory_records r ON (r.owner_id,r.id,r.version)=(a.owner_id,a.entity_id,a.entity_version)
JOIN record_versions v ON (v.owner_id,v.record_id,v.version)=(r.owner_id,r.id,r.version)
WHERE r.kind='entity' AND r.state='active' AND v.state='active'
ORDER BY a.entity_id::text`

// Candidate IDs use mentions, speech-time and event-time indexes. Current
// versions and all prompt visibility restrictions are checked before deciding
// whether the time condition has any hits, including the relaxation decision.
var teamStructuredSQL = `WITH candidates AS (
 SELECT c.claim_id,c.version FROM claim_revisions c WHERE c.owner_id=$1 AND c.subject_id=ANY($2::uuid[])
 UNION SELECT m.claim_id,m.claim_version FROM claim_mentions m WHERE m.owner_id=$1 AND m.entity_id=ANY($2::uuid[])
 UNION SELECT v.record_id,v.version FROM record_versions v WHERE v.owner_id=$1 AND cardinality($2::uuid[])=0 AND v.expressed_at >= $3 AND v.expressed_at < $4
 UNION SELECT c.claim_id,c.version FROM claim_revisions c WHERE c.owner_id=$1 AND cardinality($2::uuid[])=0 AND $5='either' AND c.event_from < $4 AND (c.event_to IS NULL OR (c.event_to > $3 AND c.event_to > c.event_from))
), visible AS MATERIALIZED (
 SELECT c.claim_id,c.version,c.subject_id,c.nature,v.expressed_at,c.event_from,c.event_to
 FROM candidates hit
 JOIN claim_revisions c ON (c.owner_id,c.claim_id,c.version)=($1,hit.claim_id,hit.version)
 JOIN memory_records r ON (r.owner_id,r.id)=(c.owner_id,c.claim_id)
 JOIN record_versions v ON (v.owner_id,v.record_id,v.version)=(c.owner_id,c.claim_id,c.version)
 JOIN applicable_claim_versions($1,now(),now()) current ON (current.claim_id,current.version)=(c.claim_id,c.version)
 JOIN workspace_agents agent ON agent.owner_id=$1 AND agent.id=$6
 WHERE r.state='active' AND v.state='active'
 AND EXISTS(SELECT 1 FROM record_grants g WHERE g.owner_id=$1 AND g.record_id=c.claim_id AND g.principal_id=$6)
 AND agent.document->'memoryKinds' ? c.nature
 AND ` + currentMemorySQL("r.owner_id", "r.id") + `
 AND (coalesce((agent.document->>'includeInferred')::boolean,false) OR ` + humanMemorySQL("c") + `)
 AND NOT EXISTS(SELECT 1 FROM context_exclusions ex WHERE ex.owner_id=$1 AND ex.thing_id=$7::uuid AND ex.memory_id=c.claim_id)
 AND ($8::text IS NULL OR coalesce(c.scope->>'project_id','')='' OR c.scope->>'project_id'=$8)
), timed AS (
 SELECT *,($3::timestamptz IS NULL OR (expressed_at >= $3 AND expressed_at < $4) OR ($5='either' AND event_from < $4 AND (event_to IS NULL OR (event_to > $3 AND event_to > event_from)))) IS TRUE AS fits FROM visible
), chosen AS (
 SELECT *,NOT fits AS relaxed FROM timed
 WHERE fits OR (cardinality($2::uuid[])>0 AND $3::timestamptz IS NOT NULL AND NOT EXISTS(SELECT 1 FROM timed WHERE fits))
)
SELECT claim_id::text,version,relaxed FROM chosen
ORDER BY EXISTS(SELECT 1 FROM entity_versions self JOIN memory_records sr ON (sr.owner_id,sr.id,sr.version)=(self.owner_id,self.entity_id,self.version)
 WHERE self.owner_id=$1 AND self.entity_id=chosen.subject_id AND self.entity_type='self' AND sr.state='active') DESC,
 nature=ANY($9::text[]) DESC,expressed_at DESC NULLS LAST,claim_id,version
LIMIT $10`

func structuredRecallTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, hints memory.TeamRecall, limit int) ([]memory.Ref, bool, error) {
	ids := []memory.ID{}
	entitiesSQL := teamEntitiesSQL
	mergesAvailable, err := entityMergeSchemaTx(ctx, tx)
	if err != nil {
		return nil, false, err
	}
	if mergesAvailable {
		entitiesSQL = strings.Replace(entitiesSQL, "ORDER BY a.entity_id::text", "AND NOT EXISTS(SELECT 1 FROM entity_merges m WHERE m.owner_id=r.owner_id AND m.merged_id=r.id AND m.undone_at IS NULL) ORDER BY a.entity_id::text", 1)
	}
	rows, err := tx.Query(ctx, entitiesSQL, string(scope.OwnerID), hints.Text)
	if err != nil {
		return nil, false, err
	}
	for rows.Next() {
		var id memory.ID
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, false, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, false, err
	}
	refs := []memory.Ref{}
	if len(ids) == 0 && hints.Plan.Time == nil {
		return refs, false, nil
	}
	var from, to *time.Time
	axis := ""
	if hints.Plan.Time != nil {
		from = &hints.Plan.Time.From
		to = &hints.Plan.Time.To
		axis = hints.Plan.Time.Axis
	}
	if hints.Candidates > limit {
		limit = hints.Candidates
	}
	rows, err = tx.Query(ctx, teamStructuredSQL, string(scope.OwnerID), ids, from, to, axis, scope.PrincipalID, hints.ThingID, hints.ProjectID, hints.Plan.Natures, limit)
	if err != nil {
		return nil, false, err
	}
	relaxed := false
	for rows.Next() {
		ref := memory.Ref{Kind: memory.ClaimKind}
		var r bool
		if err = rows.Scan(&ref.ID, &ref.Version, &r); err != nil {
			rows.Close()
			return nil, false, err
		}
		refs = append(refs, ref)
		relaxed = relaxed || r
	}
	err = rows.Err()
	rows.Close()
	return refs, relaxed, err
}

func orderTeamExcerpts(excerpts []memory.RecallExcerpt, plan memory.QueryPlan) {
	if plan.Time == nil {
		return
	}
	inTime := func(e memory.RecallExcerpt) bool {
		at := e.ExpressedAt
		if at == nil && oneOf(e.Connector, "desk", "capture", "telegram", "desk-incomplete") {
			at = &e.RecordedAt
		}
		return at != nil && !at.Before(plan.Time.From) && at.Before(plan.Time.To)
	}
	sort.SliceStable(excerpts, func(i, j int) bool { return inTime(excerpts[i]) && !inTime(excerpts[j]) })
}

type activityRankingKey struct{}

// WithActivityRanking supports fictional on/off evaluations without a UI switch.
func WithActivityRanking(ctx context.Context, enabled bool) context.Context {
	return context.WithValue(ctx, activityRankingKey{}, enabled)
}
