package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/worker"
	"strings"
)

func (s *Store) Summarize(ctx context.Context, scope memory.Scope, in memory.SummaryRequest) (memory.SummaryResult, error) {
	out := memory.SummaryResult{}
	if !scope.Valid() {
		return out, memory.ErrForbidden
	}
	if !in.ID.Valid() || in.Version < 0 || in.Tokens < 0 || in.Tokens > 16000 {
		return out, memory.ErrInvalid
	}
	if in.Tokens == 0 {
		in.Tokens = 2000
	}
	err := pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{IsoLevel: pgx.ReadCommitted}, func(tx pgx.Tx) error { var err error; out, err = s.summarizeTx(ctx, tx, scope, in); return err })
	return out, err
}
func (s *Store) summarizeTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, in memory.SummaryRequest) (memory.SummaryResult, error) {
	out := memory.SummaryResult{Coverage: coverage(), Dependencies: []memory.Ref{}}
	out.Root.ID = in.ID
	err := tx.QueryRow(ctx, `SELECT version,kind FROM memory_records r WHERE owner_id=$1 AND id=$2 AND state='active' AND kind IN ('source','entity','episode','claim') AND ($3 OR EXISTS(SELECT 1 FROM record_grants g WHERE g.owner_id=r.owner_id AND g.record_id=r.id AND g.principal_id=$4)) FOR SHARE`, string(scope.OwnerID), string(in.ID), scope.IsOwner, scope.PrincipalID).Scan(&out.Root.Version, &out.Root.Kind)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, memory.ErrNotFound
	}
	if err != nil {
		return out, err
	}
	if in.Version != 0 && in.Version != out.Root.Version {
		return out, memory.ErrConflict
	}
	principal := scope.PrincipalID
	if scope.IsOwner {
		principal = "owner"
	}
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", fmt.Sprintf("summary:%s:%s:%s:%d", scope.OwnerID, in.ID, principal, in.Tokens)); err != nil {
		return out, err
	}
	rows, err := tx.Query(ctx, `WITH roots AS (SELECT $2::uuid AS id UNION SELECT member_id FROM episode_members WHERE owner_id=$1 AND episode_id=$2 AND episode_version=$3), ids AS (
 SELECT id FROM roots
 UNION SELECT e.target_id FROM evidence e WHERE e.owner_id=$1 AND e.source_id IN (SELECT id FROM roots)
 UNION SELECT claim_id FROM claim_revisions WHERE owner_id=$1 AND subject_id=$2
 UNION SELECT source_id FROM source_versions WHERE owner_id=$1 AND derived_from_id=$2 AND derived_from_version=$3
 UNION SELECT source_id FROM archive_entries WHERE owner_id=$1 AND archive_id=$2 AND archive_version=$3
 ) SELECT r.id::text,t.version,r.kind,left(t.body,1200),coalesce(c.nature,''),coalesce(c.confirmation,''),coalesce(sc.role,''),coalesce(sc.branch,''),coalesce(sc.gaps,'[]'),rv.valid_from,rv.valid_to
 FROM ids JOIN memory_records r ON r.id=ids.id AND r.owner_id=$1 JOIN memory_text t ON t.owner_id=r.owner_id AND t.id=r.id
 JOIN record_versions rv ON (rv.owner_id,rv.record_id,rv.version)=(t.owner_id,t.id,t.version)
 LEFT JOIN claim_revisions c ON (c.owner_id,c.claim_id,c.version)=(t.owner_id,t.id,t.version)
 LEFT JOIN source_contexts sc ON (sc.owner_id,sc.source_id,sc.source_version)=(t.owner_id,t.id,t.version)
 WHERE r.state='active' AND rv.state='active' AND (r.kind='claim' AND t.version=(SELECT a.version FROM applicable_claim_versions($1,now(),now()) a WHERE a.claim_id=r.id) OR r.kind!='claim' AND t.version=r.version)
 AND ($4 OR EXISTS(SELECT 1 FROM record_grants g WHERE g.owner_id=r.owner_id AND g.record_id=r.id AND g.principal_id=$5))
 ORDER BY r.id=$2 DESC,r.kind='claim' DESC,rv.expressed_at NULLS LAST,r.id LIMIT 501`, string(scope.OwnerID), string(in.ID), out.Root.Version, scope.IsOwner, scope.PrincipalID)
	if err != nil {
		return out, err
	}
	type excerpt struct {
		Ref                                      memory.Ref
		Text, Nature, Confirmation, Role, Branch string
		Gaps                                     []string
		From, To                                 any
	}
	entries := []excerpt{}
	for rows.Next() {
		var e excerpt
		if err := rows.Scan(&e.Ref.ID, &e.Ref.Version, &e.Ref.Kind, &e.Text, &e.Nature, &e.Confirmation, &e.Role, &e.Branch, &e.Gaps, &e.From, &e.To); err != nil {
			rows.Close()
			return out, err
		}
		entries = append(entries, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	if len(entries) > 500 {
		entries = entries[:500]
		out.Coverage.Gaps = append(out.Coverage.Gaps, "相关材料超过摘要覆盖范围，请展开经历或完整历史继续查询")
	}
	var pending int
	err = tx.QueryRow(ctx, `SELECT count(*) FROM memory_jobs WHERE owner_id=$1 AND record_id=$2 AND record_version=$3 AND state!='done' AND stage!='memory.summary'`, string(scope.OwnerID), string(in.ID), out.Root.Version).Scan(&pending)
	if err != nil {
		return out, err
	}
	if pending > 0 {
		out.Coverage.Gaps = append(out.Coverage.Gaps, "来源仍有待处理或失败步骤；摘要只使用目前可读内容")
		out.Coverage.PendingSources = append(out.Coverage.PendingSources, in.ID)
	}
	var text strings.Builder
	remaining := in.Tokens * 2
	for _, e := range entries {
		out.Dependencies = append(out.Dependencies, e.Ref)
		out.Coverage.Gaps = append(out.Coverage.Gaps, e.Gaps...)
		label := string(e.Ref.Kind)
		if e.Ref.Kind == memory.SourceKind {
			label = "原文摘录，保留当时表达"
		}
		if e.Nature != "" {
			label = e.Nature + " / " + e.Confirmation
		}
		if e.Role != "" {
			label += " / " + e.Role
		}
		if e.Branch != "" {
			label += " / " + e.Branch
		}
		line := fmt.Sprintf("[%s v%d] %s：%s\n", e.Ref.ID, e.Ref.Version, label, e.Text)
		runes := []rune(line)
		if len(runes) > remaining {
			out.Coverage.Gaps = append(out.Coverage.Gaps, "摘要受上下文预算限制，展开来源可查看全文")
			continue
		}
		text.WriteString(line)
		remaining -= len(runes)
	}
	out.Text = text.String()
	out.Coverage.Complete = len(out.Coverage.Gaps) == 0
	hash := sha256.Sum256(asJSON(struct {
		Entries  []excerpt
		Coverage memory.Coverage
	}{entries, out.Coverage}))
	var previous []byte
	var stale bool
	var summaryID string
	var version int
	err = tx.QueryRow(ctx, `SELECT k.summary_id::text,r.version,k.membership_hash,d.stale FROM summary_keys k JOIN memory_records r ON (r.owner_id,r.id)=(k.owner_id,k.summary_id) JOIN derived_views d ON (d.owner_id,d.id,d.version)=(r.owner_id,r.id,r.version) WHERE k.owner_id=$1 AND k.root_id=$2 AND k.root_version=$3 AND k.principal_id=$4 AND k.budget=$5`, string(scope.OwnerID), string(in.ID), out.Root.Version, principal, in.Tokens).Scan(&summaryID, &version, &previous, &stale)
	if err == nil && !stale && bytes.Equal(previous, hash[:]) {
		out.Ref = memory.Ref{ID: memory.ID(summaryID), Version: version, Kind: "summary"}
		out.Cached = true
		return out, nil
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return out, err
	}
	if summaryID == "" {
		summaryID = string(memory.NewID())
		if _, err = tx.Exec(ctx, "INSERT INTO memory_records(owner_id,id,kind,version) VALUES($1,$2,'summary',1)", string(scope.OwnerID), summaryID); err != nil {
			return out, err
		}
	}
	version++
	out.Ref = memory.Ref{ID: memory.ID(summaryID), Version: version, Kind: "summary"}
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{"INSERT INTO record_versions(owner_id,record_id,version) VALUES($1,$2,$3)", []any{string(scope.OwnerID), summaryID, version}},
		{"INSERT INTO derived_views(owner_id,id,version,purpose,body) VALUES($1,$2,$3,'summary',$4)", []any{string(scope.OwnerID), summaryID, version, out.Text}},
		{"UPDATE memory_records SET version=$3,updated_at=now() WHERE owner_id=$1 AND id=$2", []any{string(scope.OwnerID), summaryID, version}},
		{`INSERT INTO summary_keys(owner_id,root_id,root_version,principal_id,budget,summary_id,membership_hash,coverage) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(owner_id,root_id,root_version,principal_id,budget) DO UPDATE SET membership_hash=excluded.membership_hash,coverage=excluded.coverage`, []any{string(scope.OwnerID), string(in.ID), out.Root.Version, principal, in.Tokens, summaryID, hash[:], asJSON(out.Coverage)}},
	} {
		if _, err = tx.Exec(ctx, q.sql, q.args...); err != nil {
			return out, err
		}
	}
	deps := append([]memory.Ref{out.Root}, out.Dependencies...)
	for _, d := range deps {
		if _, err = tx.Exec(ctx, `INSERT INTO derived_dependencies(owner_id,view_id,view_version,dependency_id,dependency_version) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, string(scope.OwnerID), summaryID, version, string(d.ID), d.Version); err != nil {
			return out, err
		}
	}
	return out, nil
}
func (s *Store) ProcessSummary(ctx context.Context, j worker.Job) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := lockJob(ctx, tx, j); err != nil {
			return err
		}
		_, err := s.summarizeTx(ctx, tx, memory.Scope{OwnerID: j.OwnerID, PrincipalID: "worker", IsOwner: true}, memory.SummaryRequest{ID: j.Record.ID, Tokens: 2000})
		if err != nil && !errors.Is(err, memory.ErrNotFound) {
			return err
		}
		return acknowledge(ctx, tx, j)
	})
}
func (s *Store) ConfigureActivity(ctx context.Context, scope memory.Scope, in memory.ActivitySettings) error {
	if err := requireOwner(scope); err != nil {
		return err
	}
	if !in.Ref.ID.Valid() || in.Ref.Version < 1 || in.HalfLifeDays < 1 || in.HalfLifeDays > 36500 || in.ReinforcementLimit < 1 || in.ReinforcementLimit > 100 {
		return memory.ErrInvalid
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var version int
		if err := tx.QueryRow(ctx, "SELECT version FROM memory_records WHERE owner_id=$1 AND id=$2 AND state='active' FOR UPDATE", string(scope.OwnerID), string(in.Ref.ID)).Scan(&version); err != nil {
			return memory.ErrNotFound
		}
		if version != in.Ref.Version {
			return memory.ErrConflict
		}
		_, err := tx.Exec(ctx, `INSERT INTO activity(owner_id,record_id,last_effective_use_at,half_life_seconds,reinforcement_limit,pinned) VALUES($1,$2,now(),$3,$4,$5) ON CONFLICT(owner_id,record_id) DO UPDATE SET half_life_seconds=excluded.half_life_seconds,reinforcement_limit=excluded.reinforcement_limit,pinned=excluded.pinned,stability=least(activity.stability,excluded.reinforcement_limit)`, string(scope.OwnerID), string(in.Ref.ID), in.HalfLifeDays*86400, in.ReinforcementLimit, in.Pinned)
		return err
	})
}

func refreshSummaryJobsTx(ctx context.Context, tx pgx.Tx, owner, id memory.ID) error {
	_, err := tx.Exec(ctx, `INSERT INTO memory_jobs(id,owner_id,record_id,record_version,stage)
 SELECT gen_random_uuid(),r.owner_id,r.id,r.version,'memory.summary' FROM memory_records r WHERE r.owner_id=$1 AND r.state='active' AND r.kind IN ('source','claim','entity','episode') AND (r.id=$2 OR r.id IN (SELECT k.root_id FROM summary_keys k JOIN derived_dependencies d ON(d.owner_id,d.view_id)=(k.owner_id,k.summary_id) WHERE k.owner_id=$1 AND d.dependency_id=$2))
 ON CONFLICT(owner_id,record_id,record_version,stage) DO UPDATE SET state='queued',attempts=0,available_at=now(),error_code='',lease_token=NULL,lease_until=NULL WHERE memory_jobs.state!='leased'`, string(owner), string(id))
	return err
}
