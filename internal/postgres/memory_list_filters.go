package postgres

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"strings"
	"unicode"
)

// Match FieldsFunc in qualifiedCapture exactly, independent of PostgreSQL's
// locale-specific [:alnum:] and its Unicode version. Build once, not per row.
var captureWordClass = func() string {
	var b strings.Builder
	appendRange := func(lo, hi, stride rune) {
		if stride == 1 {
			b.WriteRune(lo)
			if hi != lo {
				b.WriteRune('-')
				b.WriteRune(hi)
			}
			return
		}
		for r := lo; r <= hi; r += stride {
			b.WriteRune(r)
		}
	}
	for _, table := range []*unicode.RangeTable{unicode.L, unicode.N} {
		for _, r := range table.R16 {
			appendRange(rune(r.Lo), rune(r.Hi), rune(r.Stride))
		}
		for _, r := range table.R32 {
			appendRange(rune(r.Lo), rune(r.Hi), rune(r.Stride))
		}
	}
	return b.String()
}()

func sqlTextLiteral(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

func qualifiedCaptureSQL(text string) string {
	clauses := make([]string, 0, len(qualifiedCaptureMarkers)+1)
	for _, marker := range qualifiedCaptureMarkers {
		clauses = append(clauses, "strpos("+text+","+sqlTextLiteral(marker)+")>0")
	}
	// C orders Unicode range endpoints by code point; lower() uses the normal
	// database collation, then the same letter/number tokenizer as the Go reader.
	words := "(' '||regexp_replace(lower(" + text + ") COLLATE \"C\"," + sqlTextLiteral("[^"+captureWordClass+"]+") + ",' ','g')||' ')"
	// Cheap ASCII boundaries are a superset of the Go Unicode boundaries.
	// Most memories contain no candidate English marker; avoid expensive Unicode
	// normalization for them without introducing a false negative.
	candidates := make([]string, len(qualifiedCaptureWords))
	for i, word := range qualifiedCaptureWords {
		candidates[i] = strings.ReplaceAll(word, " ", "[^a-z0-9]+")
	}
	fast := sqlTextLiteral("(^|[^a-z0-9])(" + strings.Join(candidates, "|") + ")([^a-z0-9]|$)")
	precise := words + " ~ " + sqlTextLiteral(" ("+strings.Join(qualifiedCaptureWords, "|")+") ")
	clauses = append(clauses, "CASE WHEN lower("+text+") COLLATE \"C\" ~ "+fast+" THEN "+precise+" ELSE false END")
	return "(" + strings.Join(clauses, " OR ") + ")"
}

// Aggregate evidence once for the SQL-scoped candidates rather than doing two
// correlated evidence lookups for each memory before COUNT and LIMIT.
func trustedMemoryIDsTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, opts memoryReadOptions) (int, []string, error) {
	trust := opts.query.Trust
	base := opts
	base.query.Trust = ""
	base.before = nil
	where, args := memoryWhere(scope, false, base)
	predicate := memoryTrustFilterSQL("c", "p", trust)
	args = append(args, opts.limit)
	limitSlot := len(args)
	pageWhere := ""
	if opts.before != nil {
		args = append(args, opts.before.At, opts.before.ID)
		pageWhere = fmt.Sprintf(" WHERE (updated_at<$%d OR (updated_at=$%d AND claim_id>$%d::uuid))", len(args)-1, len(args)-1, len(args))
	}
	query := `WITH candidates AS MATERIALIZED (
 SELECT c.owner_id,c.claim_id,c.version,c.value,c.acquisition,r.updated_at` + memoryJoins + where + `),
 proofs AS MATERIALIZED (
 SELECT e.target_id,e.target_version,
 bool_or(sc.role IN ('assistant','system','tool') OR src.connector IN ('ai','assistant','system','tool','agent','actions')) AS ai,
 count(DISTINCT coalesce(nullif(sc.conversation_key,''),t.conversation_id::text,e.source_id::text)) AS groups
 FROM evidence e
 JOIN sources src ON(src.owner_id,src.id)=(e.owner_id,e.source_id)
 LEFT JOIN source_contexts sc ON(sc.owner_id,sc.source_id,sc.source_version)=(e.owner_id,e.source_id,e.source_version)
 LEFT JOIN desk_turns t ON t.owner_id=src.owner_id AND t.request_id::text=lower(src.external_id) AND src.connector IN ('desk','capture','desk-incomplete')
 WHERE e.owner_id=$1 AND e.stance='supports' AND e.target_id IN (SELECT claim_id FROM candidates) GROUP BY e.target_id,e.target_version),
 filtered AS MATERIALIZED (
 SELECT c.claim_id,c.updated_at FROM candidates c LEFT JOIN proofs p ON(p.target_id,p.target_version)=(c.claim_id,c.version)
 WHERE ` + predicate + `)
 SELECT (SELECT count(*) FROM filtered),coalesce((SELECT array_agg(claim_id::text ORDER BY updated_at DESC,claim_id) FROM (
 SELECT claim_id,updated_at FROM filtered` + pageWhere + fmt.Sprintf(" ORDER BY updated_at DESC,claim_id LIMIT $%d", limitSlot) + `) page),'{}'::text[])`
	var total int
	var ids []string
	err := tx.QueryRow(ctx, query, args...).Scan(&total, &ids)
	return total, ids, err
}

// Same precedence as memoryTrust/claimStatusesTx, with already aggregated proofs.
func memoryTrustFilterSQL(claim, proof, trust string) string {
	human := "(" + claim + ".acquisition<>'inferred' AND NOT coalesce(" + proof + ".ai,false))"
	if trust == "inferred" {
		return "NOT " + human
	}
	if trust == "reported" {
		return human + " AND " + claim + ".acquisition='reported'"
	}
	base := human + " AND " + claim + ".acquisition<>'reported'"
	// Isolate the computed boolean: the planner otherwise combines many
	// substring estimates into "almost every text is qualified", and chooses
	// a quadratic nested loop for NOT qualified even when all texts are direct.
	qualified := "(SELECT " + qualifiedCaptureSQL(claim+".value #>> '{}'") + " OFFSET 0)"
	groups := "coalesce(" + proof + ".groups,0)"
	switch trust {
	case "tentative":
		return base + " AND " + qualified
	case "stated":
		return base + " AND NOT " + qualified + " AND " + groups + "<2"
	case "repeated":
		return base + " AND NOT " + qualified + " AND " + groups + ">=2"
	default:
		return "false"
	}
}
