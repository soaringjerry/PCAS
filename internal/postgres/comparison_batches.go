package postgres

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
)

const comparisonBlockSize = compareLimit / 2

type comparisonBatch struct {
	Rule        int             `json:"rule"`
	Group       compareGroup    `json:"group"`
	A           int64           `json:"blockA"`
	B           int64           `json:"blockB"`
	Fingerprint string          `json:"fingerprint"`
	Memories    []compareMemory `json:"memories"`
	Refs        []memory.Ref    `json:"refs"`
}

func syncComparisonMembersTx(ctx context.Context, tx pgx.Tx, owner memory.ID) error {
	// Serialize allocation with scheduling. Existing positions are never rewritten.
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended(current_database()||':'||current_schema()||':comparison-members:'||$1,0))", string(owner)); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO memory_comparison_members(owner_id,group_key,claim_id,position)
 SELECT m.owner_id,m.key,m.claim_id,
 coalesce((SELECT max(position)+1 FROM memory_comparison_members p WHERE p.owner_id=m.owner_id AND p.group_key=m.key),0)
 + row_number() OVER(PARTITION BY m.key ORDER BY rv.expressed_at NULLS FIRST,m.claim_id)-1
 FROM status_current_members m JOIN claims cl ON(cl.owner_id,cl.id)=(m.owner_id,m.claim_id)
 JOIN record_versions rv ON(rv.owner_id,rv.record_id,rv.version)=(m.owner_id,m.claim_id,m.claim_version)
 WHERE m.owner_id=$1 AND cl.organized>=`+organizeRequiredSQL("(SELECT ov.category FROM claim_revisions ov WHERE (ov.owner_id,ov.claim_id,ov.version)=(m.owner_id,m.claim_id,m.claim_version))", "$2")+` AND NOT EXISTS(
 SELECT 1 FROM memory_comparison_members p WHERE (p.owner_id,p.group_key,p.claim_id)=(m.owner_id,m.key,m.claim_id))
 ON CONFLICT(owner_id,group_key,claim_id) DO NOTHING`, string(owner), OrganizeVersion)
	return err
}

func comparisonPlanTx(ctx context.Context, tx pgx.Tx, owner memory.ID, version int) ([]comparisonBatch, error) {
	rows, err := tx.Query(ctx, `SELECT m.key,m.kind,m.name,p.position/$3,r.id::text,r.version,c.value #>> '{}',rv.expressed_at,
 (c.confirmation='confirmed' OR EXISTS(SELECT 1 FROM record_versions edited WHERE edited.owner_id=r.owner_id AND edited.record_id=r.id AND edited.version>1 AND edited.actor='user') OR `+restoredMemorySQL("$4")+`)
 FROM status_current_members m JOIN memory_comparison_members p ON(p.owner_id,p.group_key,p.claim_id)=(m.owner_id,m.key,m.claim_id)
 JOIN memory_records r ON(r.owner_id,r.id)=(m.owner_id,m.claim_id)
 JOIN claims cl ON(cl.owner_id,cl.id)=(r.owner_id,r.id)
 JOIN claim_revisions c ON(c.owner_id,c.claim_id,c.version)=(m.owner_id,m.claim_id,m.claim_version)
 JOIN record_versions rv ON(rv.owner_id,rv.record_id,rv.version)=(m.owner_id,m.claim_id,m.claim_version)
 WHERE m.owner_id=$1 AND cl.organized>=`+organizeRequiredSQL("(SELECT ov.category FROM claim_revisions ov WHERE (ov.owner_id,ov.claim_id,ov.version)=(m.owner_id,m.claim_id,m.claim_version))", "$2")+`
 ORDER BY CASE WHEN m.key='self:rule' THEN 0 WHEN m.kind='self' THEN 1 ELSE 2 END,m.key,p.position`, string(owner), OrganizeVersion, comparisonBlockSize, version)
	if err != nil {
		return nil, err
	}
	groups := []compareGroup{}
	blocks := map[string]map[int64][]compareMemory{}
	blockIDs := map[string][]int64{}
	for rows.Next() {
		var g compareGroup
		var block int64
		m := compareMemory{organizeMemory: organizeMemory{Ref: memory.Ref{Kind: memory.ClaimKind}}}
		if err := rows.Scan(&g.Key, &g.Kind, &g.Name, &block, &m.Ref.ID, &m.Ref.Version, &m.Text, &m.ExpressedAt, &m.Protected); err != nil {
			rows.Close()
			return nil, err
		}
		if blocks[g.Key] == nil {
			groups = append(groups, g)
			blocks[g.Key] = map[int64][]compareMemory{}
		}
		if blocks[g.Key][block] == nil {
			blockIDs[g.Key] = append(blockIDs[g.Key], block)
		}
		blocks[g.Key][block] = append(blocks[g.Key][block], m)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	out := []comparisonBatch{}
	for _, g := range groups {
		list := blockIDs[g.Key]
		for i, a := range list {
			for _, b := range list[i:] {
				batch := comparisonBatch{Rule: version, Group: g, A: a, B: b, Memories: append([]compareMemory{}, blocks[g.Key][a]...)}
				if a != b {
					batch.Memories = append(batch.Memories, blocks[g.Key][b]...)
				}
				if len(batch.Memories) < 2 {
					continue
				}
				sort.SliceStable(batch.Memories, func(i, j int) bool {
					a, b := batch.Memories[i], batch.Memories[j]
					if a.ExpressedAt == nil {
						return b.ExpressedAt != nil
					}
					if b.ExpressedAt == nil {
						return false
					}
					if !a.ExpressedAt.Equal(*b.ExpressedAt) {
						return a.ExpressedAt.Before(*b.ExpressedAt)
					}
					return a.Ref.ID < b.Ref.ID
				})
				if len(batch.Memories) < 2 {
					continue
				}
				for i := range batch.Memories {
					batch.Memories[i].N = i + 1
					batch.Refs = append(batch.Refs, batch.Memories[i].Ref)
				}
				hash := sha256.Sum256(asJSON([]any{batch.Refs, batch.Memories}))
				batch.Fingerprint = fmt.Sprintf("%x", hash)
				out = append(out, batch)
			}
		}
	}
	return out, nil
}

func nextComparisonBatchTx(ctx context.Context, tx pgx.Tx, owner memory.ID, version int) (*comparisonBatch, error) {
	if err := syncComparisonMembersTx(ctx, tx, owner); err != nil {
		return nil, err
	}
	plan, err := comparisonPlanTx(ctx, tx, owner, version)
	if err != nil {
		return nil, err
	}
	type receipt struct {
		Fingerprint string
		Rule        int
		After       *time.Time
		Completed   *time.Time
	}
	receipts := map[string]receipt{}
	rows, err := tx.Query(ctx, "SELECT group_key,block_a,block_b,fingerprint,rule,nullif(retry_after,'-infinity'),completed_at FROM memory_comparison_batches WHERE owner_id=$1", string(owner))
	if err != nil {
		return nil, err
	}
	key := func(g string, a, b int64) string { return fmt.Sprintf("%s/%d/%d", g, a, b) }
	for rows.Next() {
		var g string
		var a, b int64
		var r receipt
		if err := rows.Scan(&g, &a, &b, &r.Fingerprint, &r.Rule, &r.After, &r.Completed); err != nil {
			rows.Close()
			return nil, err
		}
		receipts[key(g, a, b)] = r
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	// Completion is scoped to a memory and group. A row completes only when
	// every current batch containing it has a matching successful receipt.
	pending := map[string]bool{}
	for _, b := range plan {
		r := receipts[key(b.Group.Key, b.A, b.B)]
		if !comparisonRuleAccepted(r.Rule, version) || r.Fingerprint != b.Fingerprint || r.Completed == nil {
			for _, m := range b.Memories {
				pending[b.Group.Key+"/"+string(m.Ref.ID)] = true
			}
		}
	}
	rows, err = tx.Query(ctx, `SELECT m.key,m.claim_id::text,m.claim_version FROM status_current_members m
 JOIN claims cl ON(cl.owner_id,cl.id)=(m.owner_id,m.claim_id) WHERE m.owner_id=$1 AND cl.organized>=`+organizeRequiredSQL("(SELECT ov.category FROM claim_revisions ov WHERE (ov.owner_id,ov.claim_id,ov.version)=(m.owner_id,m.claim_id,m.claim_version))", "$2")+``, string(owner), OrganizeVersion)
	if err != nil {
		return nil, err
	}
	groups, ids, versions, complete := []string{}, []string{}, []int{}, []bool{}
	for rows.Next() {
		var group, id string
		var v int
		if err := rows.Scan(&group, &id, &v); err != nil {
			rows.Close()
			return nil, err
		}
		groups = append(groups, group)
		ids = append(ids, id)
		versions = append(versions, v)
		complete = append(complete, !pending[group+"/"+id])
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO memory_group_progress(owner_id,group_key,claim_id,claim_version,rule,compared_at,completed)
 SELECT $1,x.g,x.id,x.v,$6,clock_timestamp(),x.done FROM unnest($2::text[],$3::uuid[],$4::int[],$5::boolean[]) x(g,id,v,done)
 ON CONFLICT(owner_id,group_key,claim_id) DO UPDATE SET claim_version=excluded.claim_version,rule=excluded.rule,completed=excluded.completed,compared_at=excluded.compared_at
 WHERE (memory_group_progress.claim_version,memory_group_progress.rule,memory_group_progress.completed) IS DISTINCT FROM (excluded.claim_version,excluded.rule,excluded.completed)`, string(owner), groups, ids, versions, complete, version); err != nil {
		return nil, err
	}
	for _, b := range plan {
		r := receipts[key(b.Group.Key, b.A, b.B)]
		if comparisonRuleAccepted(r.Rule, version) && r.Fingerprint == b.Fingerprint && (r.Completed != nil || r.After != nil && r.After.After(time.Now())) {
			continue
		}
		return &b, nil
	}
	return nil, nil
}

func writeComparisonReceiptTx(ctx context.Context, tx pgx.Tx, owner memory.ID, version int, b comparisonBatch, valid bool) error {
	if !valid {
		if err := stageEventTx(ctx, tx, owner, CompareStage, "failure", "compare_invalid_output", 1); err != nil {
			return err
		}
		var attempts int
		if err := tx.QueryRow(ctx, `INSERT INTO memory_comparison_batches(owner_id,group_key,block_a,block_b,fingerprint,rule,attempts,retry_after)
 VALUES($1,$2,$3,$4,$5,$6,1,now()+interval '1 minute')
 ON CONFLICT(owner_id,group_key,block_a,block_b) DO UPDATE SET fingerprint=excluded.fingerprint,rule=excluded.rule,attempts=memory_comparison_batches.attempts+1,completed_at=NULL RETURNING attempts`, string(owner), b.Group.Key, b.A, b.B, b.Fingerprint, version).Scan(&attempts); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, "UPDATE memory_comparison_batches SET retry_after=clock_timestamp()+$5*interval '1 second' WHERE owner_id=$1 AND group_key=$2 AND block_a=$3 AND block_b=$4", string(owner), b.Group.Key, b.A, b.B, retryDelay(attempts).Seconds())
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO memory_comparison_batches(owner_id,group_key,block_a,block_b,fingerprint,rule,completed_at)
 VALUES($1,$2,$3,$4,$5,$6,clock_timestamp()) ON CONFLICT(owner_id,group_key,block_a,block_b)
 DO UPDATE SET fingerprint=excluded.fingerprint,rule=excluded.rule,completed_at=excluded.completed_at,attempts=0,retry_after='-infinity'`, string(owner), b.Group.Key, b.A, b.B, b.Fingerprint, version); err != nil {
		return err
	}
	// Per-memory/per-group receipts record participation, never replace the batch
	// coverage ledger that must finish every cross-block comparison.
	_, err := tx.Exec(ctx, `INSERT INTO memory_group_progress(owner_id,group_key,claim_id,claim_version,rule,compared_at)
 SELECT $1,$2,input.id,input.version,$3,clock_timestamp() FROM jsonb_to_recordset($4::jsonb) input(id uuid,version integer)
 JOIN claims cl ON(cl.owner_id,cl.id)=($1::uuid,input.id)
 ON CONFLICT(owner_id,group_key,claim_id) DO UPDATE SET claim_version=excluded.claim_version,rule=excluded.rule,compared_at=excluded.compared_at,completed=false
 WHERE (memory_group_progress.claim_version,memory_group_progress.rule) IS DISTINCT FROM (excluded.claim_version,excluded.rule)`, string(owner), b.Group.Key, version, asJSON(b.Refs))
	return err
}

func (s *Store) nextComparisonBatch(ctx context.Context, owner memory.ID, version int) (*comparisonBatch, error) {
	var batch *comparisonBatch
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var e error
		batch, e = nextComparisonBatchTx(ctx, tx, owner, version)
		return e
	})
	return batch, err
}

// Version 2 changes future judgments and explicitly selected superseded pairs.
// Successful, unchanged version-1 batches retain their original receipt;
// upgrading the prompt must not schedule a library-wide comparison.
func comparisonRuleAccepted(rule, current int) bool {
	return rule >= current || rule == 1 && current == 2
}
