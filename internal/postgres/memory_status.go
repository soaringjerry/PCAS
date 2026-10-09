package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// These expressions are also valid in pre-037 migration fixtures. No database
// functions or changes to the frozen migration are required.
func currentMemorySQL(owner, id string) string {
	return fmt.Sprintf(`EXISTS(SELECT 1 FROM claims status WHERE status.owner_id=%s AND status.id=%s AND coalesce(to_jsonb(status)->>'retired','') NOT IN ('superseded','duplicate'))`, owner, id)
}
func humanMemorySQL(claim string) string {
	return fmt.Sprintf(`(%[1]s.acquisition<>'inferred' AND NOT EXISTS(
 SELECT 1 FROM evidence trust_e JOIN sources trust_s ON (trust_s.owner_id,trust_s.id)=(trust_e.owner_id,trust_e.source_id)
 LEFT JOIN source_contexts trust_context ON (trust_context.owner_id,trust_context.source_id,trust_context.source_version)=(trust_e.owner_id,trust_e.source_id,trust_e.source_version)
 WHERE (trust_e.owner_id,trust_e.target_id,trust_e.target_version)=(%[1]s.owner_id,%[1]s.claim_id,%[1]s.version)
 AND trust_e.stance='supports' AND (trust_context.role IN ('assistant','system','tool') OR trust_s.connector IN ('ai','assistant','system','tool','agent','actions'))))`, claim)
}

type claimStatus struct {
	Retired, By    string
	MergedFrom     int
	AI             bool
	EvidenceGroups int
	KeeperCurrent  bool
}

func claimStatusesTx(ctx context.Context, tx pgx.Tx, owner memory.ID, ids []string) (map[string]claimStatus, error) {
	out := map[string]claimStatus{}
	if len(ids) == 0 {
		return out, nil
	}
	query := `WITH merged_counts AS MATERIALIZED (
 SELECT (to_jsonb(merged)->>'retired_by')::uuid AS kept,count(*) AS n
 FROM claims merged JOIN memory_records mr ON(mr.owner_id,mr.id)=(merged.owner_id,merged.id)
 WHERE merged.owner_id=$1 AND to_jsonb(merged)->>'retired'='duplicate' AND (to_jsonb(merged)->>'retired_by')::uuid=ANY($2::uuid[]) AND mr.state='active'
 GROUP BY (to_jsonb(merged)->>'retired_by')::uuid)
 SELECT cl.id::text,coalesce(to_jsonb(cl)->>'retired',''),coalesce(to_jsonb(cl)->>'retired_by',''),
 coalesce(merged_counts.n,0),
 NOT ` + humanMemorySQL("c") + `,
 (SELECT count(DISTINCT coalesce(nullif(sc.conversation_key,''),t.conversation_id::text,e.source_id::text))
 FROM evidence e LEFT JOIN source_contexts sc ON(sc.owner_id,sc.source_id,sc.source_version)=(e.owner_id,e.source_id,e.source_version)
 JOIN sources src ON(src.owner_id,src.id)=(e.owner_id,e.source_id)
 LEFT JOIN desk_turns t ON t.owner_id=src.owner_id AND t.request_id::text=lower(src.external_id) AND src.connector IN ('desk','capture','desk-incomplete')
 WHERE (e.owner_id,e.target_id,e.target_version)=(cl.owner_id,cl.id,c.version) AND e.stance='supports'),
 EXISTS(SELECT 1 FROM claims keeper JOIN memory_records kr ON(kr.owner_id,kr.id)=(keeper.owner_id,keeper.id)
 WHERE keeper.owner_id=cl.owner_id AND keeper.id=(to_jsonb(cl)->>'retired_by')::uuid AND kr.state='active' AND coalesce(to_jsonb(keeper)->>'retired','')=''
 AND EXISTS(SELECT 1 FROM applicable_claim_versions(cl.owner_id,now(),now()) kv WHERE kv.claim_id=keeper.id AND kv.version=kr.version))
 FROM claims cl JOIN memory_records r ON(r.owner_id,r.id)=(cl.owner_id,cl.id)
 JOIN claim_revisions c ON(c.owner_id,c.claim_id,c.version)=(r.owner_id,r.id,r.version)
 LEFT JOIN merged_counts ON merged_counts.kept=cl.id
 WHERE cl.owner_id=$1 AND cl.id=ANY($2::uuid[])`
	// Use the 037 index on (owner_id,retired,compared) rather than scanning all
	// current claims every time an old answer checks a few dependencies. Older
	// migration fixtures still use the JSON expressions above.
	var supported bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_attribute WHERE attrelid='claims'::regclass AND attname='retired' AND NOT attisdropped)").Scan(&supported); err != nil {
		return nil, err
	}
	if supported {
		query = strings.NewReplacer(
			"coalesce(to_jsonb(cl)->>'retired','')", "cl.retired",
			"coalesce(to_jsonb(cl)->>'retired_by','')", "coalesce(cl.retired_by::text,'')",
			"(to_jsonb(cl)->>'retired_by')::uuid", "cl.retired_by",
			"coalesce(to_jsonb(keeper)->>'retired','')", "keeper.retired",
			"(to_jsonb(merged)->>'retired_by')::uuid", "merged.retired_by",
			"to_jsonb(merged)->>'retired'", "merged.retired",
		).Replace(query)
	}
	rows, err := tx.Query(ctx, query, string(owner), ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var status claimStatus
		if err := rows.Scan(&id, &status.Retired, &status.By, &status.MergedFrom, &status.AI, &status.EvidenceGroups, &status.KeeperCurrent); err != nil {
			return nil, err
		}
		out[id] = status
	}
	return out, rows.Err()
}

func memoryTrust(text, acquisition string, status claimStatus) string {
	if acquisition == "inferred" || status.AI {
		return "inferred"
	}
	if acquisition == "reported" {
		return "reported"
	}
	if qualifiedCapture(text) {
		return "tentative"
	}
	if status.EvidenceGroups >= 2 {
		return "repeated"
	}
	return "stated"
}

func fillMemoryStatusesTx(ctx context.Context, tx pgx.Tx, owner memory.ID, memories []workspace.Memory) error {
	ids := make([]string, len(memories))
	for i, m := range memories {
		ids[i] = m.ID
	}
	statuses, err := claimStatusesTx(ctx, tx, owner, ids)
	if err != nil {
		return err
	}
	for i := range memories {
		m := &memories[i]
		status := statuses[m.ID]
		m.Trust = memoryTrust(m.Text, m.Acquisition, status)
		m.Retired = status.Retired
		m.RetiredBy = status.By
		m.MergedFrom = status.MergedFrom
	}
	return nil
}

type retirementAccessKey struct{}

// Existing answers may retain their original permissions and citations after
// a duplicate merges. Supersession changes validity, not access to old text.
func retirementDependencyAllowed(ctx context.Context, status claimStatus) bool {
	if access, _ := ctx.Value(retirementAccessKey{}).(bool); access {
		return true
	}
	return status.Retired == "" || status.Retired == "duplicate" && status.KeeperCurrent
}

func dependencyMemorySQL(owner, id string) string {
	return fmt.Sprintf(`EXISTS(SELECT 1 FROM claims status WHERE status.owner_id=%[1]s AND status.id=%[2]s
 AND (coalesce(to_jsonb(status)->>'retired','')='' OR to_jsonb(status)->>'retired'='duplicate' AND EXISTS(
 SELECT 1 FROM claims keeper JOIN memory_records kr ON(kr.owner_id,kr.id)=(keeper.owner_id,keeper.id)
 WHERE keeper.owner_id=status.owner_id AND keeper.id=(to_jsonb(status)->>'retired_by')::uuid AND kr.state='active'
 AND coalesce(to_jsonb(keeper)->>'retired','')='' AND EXISTS(SELECT 1 FROM applicable_claim_versions(status.owner_id,now(),now()) kv WHERE kv.claim_id=keeper.id AND kv.version=kr.version))))`, owner, id)
}

func entityMergeSchemaTx(ctx context.Context, tx pgx.Tx) (bool, error) {
	var exists bool
	err := tx.QueryRow(ctx, "SELECT to_regclass('entity_merges') IS NOT NULL").Scan(&exists)
	return exists, err
}
func resolveMergedEntityTx(ctx context.Context, tx pgx.Tx, owner memory.ID, id memory.ID) (memory.ID, error) {
	available, err := entityMergeSchemaTx(ctx, tx)
	if err != nil || !available {
		return id, err
	}
	seen := map[memory.ID]bool{}
	for {
		if seen[id] {
			return "", memory.ErrConflict
		}
		seen[id] = true
		var kept memory.ID
		err := tx.QueryRow(ctx, "SELECT kept_id::text FROM entity_merges WHERE owner_id=$1 AND merged_id=$2 AND undone_at IS NULL", string(owner), string(id)).Scan(&kept)
		if err == pgx.ErrNoRows {
			return id, nil
		}
		if err != nil {
			return "", err
		}
		id = kept
	}
}

func resetComparisonTx(ctx context.Context, tx pgx.Tx, owner memory.ID, id string) error {
	var supported bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_attribute WHERE attrelid='claims'::regclass AND attname='compared' AND NOT attisdropped)").Scan(&supported); err != nil {
		return err
	}
	if !supported {
		return nil
	}
	_, err := tx.Exec(ctx, "UPDATE claims SET compared=0 WHERE owner_id=$1 AND id=$2", string(owner), id)
	return err
}
