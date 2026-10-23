package interpstore

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/soaringjerry/PCAS/internal/interpretation/aliasscan"
	"github.com/soaringjerry/PCAS/internal/interpretation/comparison"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/postgres/kernel"
	"github.com/soaringjerry/PCAS/internal/worker"
)

type comparisonEntity struct {
	N           int              `json:"n"`
	Type        string           `json:"type"`
	Name        string           `json:"name"`
	MemoryCount int              `json:"memoryCount"`
	Memories    []OrganizeMemory `json:"memories"`
	Ref         memory.Ref       `json:"-"`
}
type ComparisonEntityPair struct {
	Entities      [2]comparisonEntity `json:"entities"`
	Marker        string              `json:"-"`
	SharedContext bool                `json:"-"`
}

// The ten most recent current memories of every entity, read in one pass:
// asking per entity made finding the next pair slower with every pair judged.
func entityComparisonSamplesTx(ctx context.Context, tx pgx.Tx, owner memory.ID, only ...[]string) (map[memory.ID][]OrganizeMemory, error) {
	var ids []string
	if len(only) > 0 {
		ids = only[0]
	}
	rows, err := tx.Query(ctx, `SELECT entity_id,id,version,value,expressed_at FROM (
 SELECT x.entity_id::text,r.id::text,r.version,c.value #>> '{}' AS value,rv.expressed_at,
 row_number() OVER(PARTITION BY x.entity_id ORDER BY rv.expressed_at DESC NULLS LAST,r.created_at DESC,r.id) AS n
 FROM (SELECT owner_id,claim_id,version,subject_id AS entity_id FROM claim_revisions WHERE owner_id=$1 AND subject_id IS NOT NULL
 UNION SELECT owner_id,claim_id,claim_version,entity_id FROM claim_mentions WHERE owner_id=$1) x
 JOIN claims cl ON(cl.owner_id,cl.id)=(x.owner_id,x.claim_id)
 JOIN memory_records r ON(r.owner_id,r.id,r.version)=(x.owner_id,x.claim_id,x.version)
 JOIN claim_revisions c ON(c.owner_id,c.claim_id,c.version)=(r.owner_id,r.id,r.version)
 JOIN record_versions rv ON(rv.owner_id,rv.record_id,rv.version)=(r.owner_id,r.id,r.version)
 WHERE cl.retired='' AND r.state='active' AND rv.state='active' AND ($2::uuid[] IS NULL OR x.entity_id=ANY($2::uuid[]))
 AND claim_source_is_current(cl.owner_id,cl.id,r.version,now())) ranked WHERE n<=10 ORDER BY entity_id,n`, string(owner), ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[memory.ID][]OrganizeMemory{}
	for rows.Next() {
		var entity memory.ID
		m := OrganizeMemory{Ref: memory.Ref{Kind: memory.ClaimKind}}
		if err := rows.Scan(&entity, &m.Ref.ID, &m.Ref.Version, &m.Text, &m.ExpressedAt); err != nil {
			return nil, err
		}
		m.N = len(out[entity]) + 1
		out[entity] = append(out[entity], m)
	}
	return out, rows.Err()
}

// Finding the next pair only reads, and can take longer than a background
// write may hold the owner, so it runs outside that window.
func (s *Interp) nextEntityPair(ctx context.Context, owner memory.ID, version int) (*ComparisonEntityPair, error) {
	var pair *ComparisonEntityPair
	err := pgx.BeginTxFunc(ctx, s.Pool, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		var err error
		pair, err = nextEntityPairTx(ctx, tx, owner, version)
		return err
	})
	return pair, err
}

func nextEntityPairTx(ctx context.Context, tx pgx.Tx, owner memory.ID, version int) (*ComparisonEntityPair, error) {
	names, err := EntityCandidateNamesTx(ctx, tx, owner, version)
	if err != nil {
		return nil, err
	}
	// Preserve the established strongest-representative-first merge order.
	// Eligibility comes exclusively from model-created candidates, not names.
	ranked := append([]aliasscan.Name(nil), names...)
	sort.SliceStable(ranked, func(i, j int) bool {
		a, b := ranked[i], ranked[j]
		if a.Type != b.Type {
			return a.Type < b.Type
		}
		if a.MemoryCount != b.MemoryCount {
			return a.MemoryCount > b.MemoryCount
		}
		if chineseEntityName(a.Name) != chineseEntityName(b.Name) {
			return chineseEntityName(a.Name)
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.Ref.ID < b.Ref.ID
	})
	order := make([]string, len(ranked))
	for i, e := range ranked {
		order[i] = string(e.Ref.ID)
	}
	var left, right string
	err = tx.QueryRow(ctx, aliasPendingSQL, string(owner), version, order).Scan(&left, &right)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	pair := &ComparisonEntityPair{}
	for _, e := range names {
		for i, id := range []string{left, right} {
			if string(e.Ref.ID) == id {
				pair.Entities[i] = comparisonEntity{N: i + 1, Type: e.Type, Name: e.Name, Ref: e.Ref, MemoryCount: e.MemoryCount}
			}
		}
	}
	if pair.Entities[0].Ref.ID == "" || pair.Entities[1].Ref.ID == "" || !EntityPairTypesAllowed(pair.Entities[0].Type, pair.Entities[1].Type) {
		return nil, nil
	}
	samples, err := entityComparisonSamplesTx(ctx, tx, owner, []string{left, right})
	if err != nil {
		return nil, err
	}
	for i := range pair.Entities {
		pair.Entities[i].Memories = samples[pair.Entities[i].Ref.ID]
	}
	// Stable presentation keeps fixture/model numbering independent of UUIDs.
	sort.Slice(pair.Entities[:], func(i, j int) bool {
		a, b := pair.Entities[i], pair.Entities[j]
		if a.Type != b.Type {
			return a.Type < b.Type
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.Ref.ID < b.Ref.ID
	})
	for i := range pair.Entities {
		pair.Entities[i].N = i + 1
	}
	pair.Marker = fmt.Sprintf("memory.entity_result:%d:%s:%s:%s", version, left, right, pairNameHash(pair))
	return pair, nil
}
func pairNameHash(pair *ComparisonEntityPair) string {
	a, b := pair.Entities[0], pair.Entities[1]
	if a.Ref.ID > b.Ref.ID {
		a, b = b, a
	}
	return fmt.Sprintf("%x", md5.Sum([]byte(a.Name+"\n"+b.Name)))
}

// EntityCompareStore gives the entity comparison service its reads and writes.
func (s *Interp) EntityCompareStore() comparison.EntityStore { return entityCompareStore{s} }

type entityCompareStore struct{ store *Interp }

func (a entityCompareStore) RetryLater(j worker.Job, code string) error {
	return &worker.JobError{Code: code, Until: time.Now().Add(kernel.RetryDelay(j.Attempts))}
}

func (a entityCompareStore) Session(ctx context.Context) (comparison.EntitySession, func(), error) {
	conn, release, err := a.store.ComparisonConnection(ctx)
	if err != nil {
		return nil, nil, err
	}
	return entityCompareSession{store: a.store, conn: conn}, release, nil
}

type entityCompareSession struct {
	store *Interp
	conn  *pgxpool.Conn
}

func (a entityCompareSession) Ready() error {
	return compareSession{store: a.store, conn: a.conn}.Ready()
}

func (a entityCompareSession) NextPair(ctx context.Context, j worker.Job, version int) (json.RawMessage, []memory.Ref, bool, error) {
	pair, err := a.store.nextEntityPair(ctx, j.OwnerID, version)
	if err != nil || pair == nil {
		return nil, nil, false, err
	}
	refs := []memory.Ref{}
	omitted := 0
	for _, e := range pair.Entities {
		refs = append(refs, e.Ref)
		for _, m := range e.Memories {
			refs = append(refs, m.Ref)
		}
		omitted += max(0, e.MemoryCount-len(e.Memories))
	}
	if omitted > 0 {
		if _, err := a.store.Pool.Exec(ctx, "INSERT INTO background_stage_events(owner_id,stage,outcome,reason,count) VALUES($1,$2,'overflow','identity_sample_memory_limit',$3)", string(j.OwnerID), comparison.EntityStage, omitted); err != nil {
			return nil, nil, false, err
		}
	}
	return kernel.JSON(pair), refs, true, nil
}

func (a entityCompareSession) Finish(ctx context.Context, j worker.Job) error {
	return kernel.BackgroundWriteTx(ctx, a.conn, j.OwnerID, func(ctx context.Context, tx pgx.Tx) error {
		if err := kernel.LockJob(ctx, tx, j); err != nil {
			return err
		}
		return kernel.Acknowledge(ctx, tx, j)
	})
}

// The saved record has no record identities. They come from the saved
// result's references, in the order of the entities and their memories.
func entityPairFromRecord(record json.RawMessage, refs []memory.Ref) (*ComparisonEntityPair, error) {
	var pair *ComparisonEntityPair
	if err := json.Unmarshal(record, &pair); err != nil || pair == nil {
		return nil, memory.ErrInvalid
	}
	cursor := 0
	for i := range pair.Entities {
		if cursor >= len(refs) {
			return nil, memory.ErrInvalid
		}
		pair.Entities[i].Ref = refs[cursor]
		cursor++
		for k := range pair.Entities[i].Memories {
			if cursor >= len(refs) {
				return nil, memory.ErrInvalid
			}
			pair.Entities[i].Memories[k].Ref = refs[cursor]
			cursor++
		}
	}
	return pair, nil
}

func (a entityCompareSession) RecordInvalid(ctx context.Context, j worker.Job, version int, record json.RawMessage, refs []memory.Ref, output string) error {
	pair, err := entityPairFromRecord(record, refs)
	if err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, a.conn, func(tx pgx.Tx) error {
		if err := kernel.DiscardPaidResult(ctx, tx, j); err != nil {
			return err
		}
		return writeAliasReceiptTx(ctx, tx, j.OwnerID, version, pair, nil, output)
	})
}

func (a entityCompareSession) Apply(ctx context.Context, j worker.Job, version int, record json.RawMessage, refs []memory.Ref, merge bool, fn func(comparison.EntityUnit) error) error {
	pair, err := entityPairFromRecord(record, refs)
	if err != nil {
		return err
	}
	write := func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended(current_database()||':'||current_schema()||':queue:'||$1||':'||$2,0))", string(j.OwnerID), comparison.EntityStage); err != nil {
			return err
		}
		if err := kernel.LockJob(ctx, tx, j); err != nil {
			return err
		}
		return fn(entityCompareUnit{ctx: ctx, tx: tx, job: j, version: version, pair: pair, refs: refs})
	}
	if merge {
		return kernel.BackgroundMergeResultTx(ctx, a.conn, j.OwnerID, write)
	}
	return kernel.BackgroundResultTx(ctx, a.conn, j.OwnerID, write)
}

type entityCompareUnit struct {
	ctx     context.Context
	tx      pgx.Tx
	job     worker.Job
	version int
	pair    *ComparisonEntityPair
	refs    []memory.Ref
}

func (u entityCompareUnit) owner() string { return string(u.job.OwnerID) }

func (u entityCompareUnit) NamesUnchanged() (bool, error) {
	var hash *string
	err := u.tx.QueryRow(u.ctx, "SELECT entity_name_hash($1,$2,$3)", u.owner(), string(u.pair.Entities[0].Ref.ID), string(u.pair.Entities[1].Ref.ID)).Scan(&hash)
	return err == nil && hash != nil && *hash == pairNameHash(u.pair), err
}

// Positive identity decisions require the sampled memories to remain current.
// Negative decisions persist by name only and ignore memory-count changes.
func (u entityCompareUnit) EvidenceCurrent() (bool, error) {
	current := true
	for _, ref := range u.refs {
		if ref.Kind != memory.ClaimKind {
			continue
		}
		var live bool
		if err := u.tx.QueryRow(u.ctx, "SELECT EXISTS(SELECT 1 FROM memory_records r JOIN claims c ON(c.owner_id,c.id)=(r.owner_id,r.id) WHERE r.owner_id=$1 AND r.id=$2 AND r.version=$3 AND r.state='active' AND c.retired='' AND claim_source_is_current(r.owner_id,r.id,r.version,now()))", u.owner(), string(ref.ID), ref.Version).Scan(&live); err != nil {
			return false, err
		}
		if !live {
			current = false
		}
	}
	return current, nil
}

func (u entityCompareUnit) LockEntities() (bool, error) {
	for i := range u.pair.Entities {
		if err := u.tx.QueryRow(u.ctx, "SELECT version FROM memory_records WHERE owner_id=$1 AND id=$2 AND state='active' FOR UPDATE", u.owner(), string(u.pair.Entities[i].Ref.ID)).Scan(&u.pair.Entities[i].Ref.Version); err != nil {
			return false, err
		}
	}
	var currentHash string
	if err := u.tx.QueryRow(u.ctx, "SELECT entity_name_hash($1,$2,$3)", u.owner(), string(u.pair.Entities[0].Ref.ID), string(u.pair.Entities[1].Ref.ID)).Scan(&currentHash); err != nil {
		return false, err
	}
	return currentHash == pairNameHash(u.pair), nil
}

func (u entityCompareUnit) Merge(keep int) error {
	return mergeEntityTx(u.ctx, u.tx, u.job.OwnerID, u.pair.Entities[1-keep].Ref, u.pair.Entities[keep].Ref, u.version)
}

func (u entityCompareUnit) Event(outcome, reason string, count int) error {
	return kernel.StageEvent(u.ctx, u.tx, u.job.OwnerID, comparison.EntityStage, outcome, reason, count)
}

func (u entityCompareUnit) Receipt(same *bool, output string) error {
	return writeAliasReceiptTx(u.ctx, u.tx, u.job.OwnerID, u.version, u.pair, same, output)
}

func (u entityCompareUnit) DiscardResult() error { return kernel.DiscardPaidResult(u.ctx, u.tx, u.job) }
func (u entityCompareUnit) Acknowledge() error   { return kernel.Acknowledge(u.ctx, u.tx, u.job) }

func writeAliasReceiptTx(ctx context.Context, tx pgx.Tx, owner memory.ID, rule int, pair *ComparisonEntityPair, same *bool, output string) error {
	a, b := pair.Entities[0].Ref.ID, pair.Entities[1].Ref.ID
	if a > b {
		a, b = b, a
	}
	var completed *time.Time
	if same != nil {
		v := time.Now()
		completed = &v
	}
	_, err := tx.Exec(ctx, `INSERT INTO entity_alias_receipts(owner_id,left_id,right_id,name_hash,rule,same,output,attempts,retry_after,completed_at)
 VALUES($1,$2,$3,$4,$5,$6,$7,CASE WHEN $6::boolean IS NULL THEN 1 ELSE 0 END,CASE WHEN $6::boolean IS NULL THEN now()+interval '1 minute' ELSE '-infinity' END,$8)
 ON CONFLICT(owner_id,left_id,right_id,name_hash,rule) DO UPDATE SET same=excluded.same,output=excluded.output,completed_at=excluded.completed_at,
 attempts=CASE WHEN excluded.same IS NULL THEN entity_alias_receipts.attempts+1 ELSE 0 END,
 retry_after=CASE WHEN excluded.same IS NULL THEN now()+least(3600,power(3,entity_alias_receipts.attempts)*60)*interval '1 second' ELSE '-infinity' END`, string(owner), string(a), string(b), pairNameHash(pair), rule, same, output, completed)
	return err
}

func entityPairKeepIndex(pair *ComparisonEntityPair, fallback int) int {
	a, b := pair.Entities[0], pair.Entities[1]
	return comparison.KeepIndex(comparison.NamedEntity{Type: a.Type, Name: a.Name, MemoryCount: a.MemoryCount}, comparison.NamedEntity{Type: b.Type, Name: b.Name, MemoryCount: b.MemoryCount}, fallback)
}

func chineseEntityName(s string) bool { return comparison.HasChinese(s) }

type entitySubjectChange struct {
	ID      string `json:"id"`
	Version int    `json:"version"`
}
type entityMentionChange struct {
	entitySubjectChange
	Role       string `json:"role"`
	KeptBefore bool   `json:"keptBefore"`
}
type EntityMergeSnapshot struct {
	Merged       memory.Ref            `json:"merged"`
	Kept         memory.Ref            `json:"kept"`
	Subjects     []entitySubjectChange `json:"subjects"`
	Mentions     []entityMentionChange `json:"mentions"`
	AddedAliases []string              `json:"addedAliases"`
}

func EntityMergeFingerprintTx(ctx context.Context, tx pgx.Tx, owner memory.ID, snapshot EntityMergeSnapshot) (string, error) {
	ids := []string{}
	for _, s := range snapshot.Subjects {
		ids = append(ids, s.ID)
	}
	for _, m := range snapshot.Mentions {
		ids = append(ids, m.ID)
	}
	var data []byte
	err := tx.QueryRow(ctx, `SELECT jsonb_build_object(
 'subjects',coalesce((SELECT jsonb_agg(jsonb_build_array(c.claim_id,c.version,c.subject_id) ORDER BY c.claim_id,c.version) FROM claim_revisions c WHERE c.owner_id=$1 AND c.claim_id=ANY($2::uuid[])),'[]'::jsonb),
 'mentions',coalesce((SELECT jsonb_agg(jsonb_build_array(m.claim_id,m.claim_version,m.entity_id,m.role) ORDER BY m.claim_id,m.claim_version,m.entity_id,m.role) FROM claim_mentions m WHERE m.owner_id=$1 AND m.claim_id=ANY($2::uuid[])),'[]'::jsonb),
 'aliases',coalesce((SELECT jsonb_agg(jsonb_build_array(a.entity_id,a.entity_version,a.alias) ORDER BY a.entity_id,a.entity_version,a.alias) FROM aliases a WHERE a.owner_id=$1 AND a.entity_id=ANY($3::uuid[])),'[]'::jsonb))`, string(owner), ids, []string{string(snapshot.Merged.ID), string(snapshot.Kept.ID)}).Scan(&data)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(data)), nil
}

func mergeEntityTx(ctx context.Context, tx pgx.Tx, owner memory.ID, merged, kept memory.Ref, version int) error {
	if merged.ID == kept.ID {
		return memory.ErrInvalid
	}
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", string(owner)+":entities"); err != nil {
		return err
	}
	var mergedType, keptType string
	if err := tx.QueryRow(ctx, `SELECT entity_type FROM entity_versions WHERE owner_id=$1 AND entity_id=$2 AND version=$3`, string(owner), string(merged.ID), merged.Version).Scan(&mergedType); err != nil {
		return err
	}
	if err := tx.QueryRow(ctx, `SELECT entity_type FROM entity_versions WHERE owner_id=$1 AND entity_id=$2 AND version=$3`, string(owner), string(kept.ID), kept.Version).Scan(&keptType); err != nil {
		return err
	}
	if !EntityPairTypesAllowed(mergedType, keptType) || mergedType != keptType && mergedType != "topic" {
		return memory.ErrInvalid
	}
	var available bool
	if err := tx.QueryRow(ctx, `SELECT count(*)=2 FROM entity_versions ev
 JOIN memory_records r ON(r.owner_id,r.id,r.version)=(ev.owner_id,ev.entity_id,ev.version)
 WHERE ev.owner_id=$1 AND r.state='active' AND ((r.id=$2 AND r.version=$3) OR (r.id=$4 AND r.version=$5))
 AND NOT EXISTS(SELECT 1 FROM entity_merges m WHERE m.owner_id=r.owner_id AND m.merged_id=r.id AND (m.undone_at IS NULL OR m.rule=$6))`, string(owner), string(merged.ID), merged.Version, string(kept.ID), kept.Version, version).Scan(&available); err != nil {
		return err
	}
	if !available {
		return nil
	}
	snapshot := EntityMergeSnapshot{Merged: merged, Kept: kept, Subjects: []entitySubjectChange{}, Mentions: []entityMentionChange{}, AddedAliases: []string{}}
	rows, err := tx.Query(ctx, "SELECT claim_id::text,version FROM claim_revisions WHERE owner_id=$1 AND subject_id=$2 ORDER BY claim_id,version", string(owner), string(merged.ID))
	if err != nil {
		return err
	}
	for rows.Next() {
		var c entitySubjectChange
		if err := rows.Scan(&c.ID, &c.Version); err != nil {
			rows.Close()
			return err
		}
		snapshot.Subjects = append(snapshot.Subjects, c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	rows, err = tx.Query(ctx, `SELECT m.claim_id::text,m.claim_version,m.role,EXISTS(SELECT 1 FROM claim_mentions k WHERE (k.owner_id,k.claim_id,k.claim_version,k.role)=(m.owner_id,m.claim_id,m.claim_version,m.role) AND k.entity_id=$3)
 FROM claim_mentions m WHERE m.owner_id=$1 AND m.entity_id=$2 ORDER BY m.claim_id,m.claim_version,m.role`, string(owner), string(merged.ID), string(kept.ID))
	if err != nil {
		return err
	}
	for rows.Next() {
		var c entityMentionChange
		if err := rows.Scan(&c.ID, &c.Version, &c.Role, &c.KeptBefore); err != nil {
			rows.Close()
			return err
		}
		snapshot.Mentions = append(snapshot.Mentions, c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	aliases, err := kernel.QueryDocuments[string](ctx, tx, `SELECT to_jsonb(alias) FROM (SELECT alias FROM aliases WHERE owner_id=$1 AND entity_id=$2 AND entity_version=$3 UNION SELECT name FROM entity_versions WHERE owner_id=$1 AND entity_id=$2 AND version=$3) names ORDER BY alias`, string(owner), string(merged.ID), merged.Version)
	if err != nil {
		return err
	}
	for _, alias := range aliases {
		var inserted string
		err := tx.QueryRow(ctx, `INSERT INTO aliases(owner_id,entity_id,entity_version,alias) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING RETURNING alias`, string(owner), string(kept.ID), kept.Version, alias).Scan(&inserted)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		snapshot.AddedAliases = append(snapshot.AddedAliases, inserted)
	}
	ids := []string{}
	seen := map[string]bool{}
	for _, c := range snapshot.Subjects {
		if !seen[c.ID] {
			ids = append(ids, c.ID)
			seen[c.ID] = true
		}
	}
	for start := 0; start < len(ids); start += 100 {
		if _, err := tx.Exec(ctx, "UPDATE claim_revisions SET subject_id=$3 WHERE owner_id=$1 AND subject_id=$2 AND claim_id=ANY($4::uuid[])", string(owner), string(merged.ID), string(kept.ID), ids[start:min(start+100, len(ids))]); err != nil {
			return err
		}
	}
	ids = []string{}
	seen = map[string]bool{}
	for _, m := range snapshot.Mentions {
		if !seen[m.ID] {
			ids = append(ids, m.ID)
			seen[m.ID] = true
		}
	}
	for start := 0; start < len(ids); start += 100 {
		batch := ids[start:min(start+100, len(ids))]
		if _, err := tx.Exec(ctx, `INSERT INTO claim_mentions(owner_id,claim_id,claim_version,entity_id,role) SELECT owner_id,claim_id,claim_version,$3,role FROM claim_mentions WHERE owner_id=$1 AND entity_id=$2 AND claim_id=ANY($4::uuid[]) ON CONFLICT DO NOTHING`, string(owner), string(merged.ID), string(kept.ID), batch); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "DELETE FROM claim_mentions WHERE owner_id=$1 AND entity_id=$2 AND claim_id=ANY($3::uuid[])", string(owner), string(merged.ID), batch); err != nil {
			return err
		}
	}

	if _, err := tx.Exec(ctx, `INSERT INTO entity_merges(owner_id,merged_id,kept_id,rule) VALUES($1,$2,$3,$4)
 ON CONFLICT(owner_id,merged_id) DO UPDATE SET kept_id=excluded.kept_id,merged_at=now(),rule=excluded.rule,undone_at=NULL`, string(owner), string(merged.ID), string(kept.ID), version); err != nil {
		return err
	}
	fingerprint, err := EntityMergeFingerprintTx(ctx, tx, owner, snapshot)
	if err != nil {
		return err
	}
	changes := kernel.JSON([]map[string]any{{"table": "entity_merge", "id": string(merged.ID), "before": snapshot, "afterHash": fingerprint, "rule": version}})
	if _, err := tx.Exec(ctx, `INSERT INTO action_log(owner_id,id,source,summary,changes) VALUES($1,$2,'worker',$4,$3)`, string(owner), string(memory.NewID()), changes, comparison.MergeSummary); err != nil {
		return err
	}
	return nil
}

const aliasPendingSQL = `SELECT d.left_id::text,d.right_id::text FROM entity_alias_candidates d
 JOIN memory_records a ON(a.owner_id,a.id)=(d.owner_id,d.left_id) JOIN memory_records b ON(b.owner_id,b.id)=(d.owner_id,d.right_id)
 JOIN entity_versions av ON(av.owner_id,av.entity_id,av.version)=(a.owner_id,a.id,a.version)
 JOIN entity_versions bv ON(bv.owner_id,bv.entity_id,bv.version)=(b.owner_id,b.id,b.version)
 LEFT JOIN entity_alias_receipts done ON(done.owner_id,done.left_id,done.right_id,done.name_hash,done.rule)=(d.owner_id,d.left_id,d.right_id,d.name_hash,d.rule)
 WHERE d.owner_id=$1 AND d.rule=$2 AND a.state='active' AND b.state='active'
 AND ((av.entity_type=bv.entity_type AND av.entity_type IN('person','place','organization','topic','project')) OR (av.entity_type='topic' AND bv.entity_type IN('place','organization')) OR (bv.entity_type='topic' AND av.entity_type IN('place','organization')))
 AND d.name_hash=md5(av.name||E'\n'||bv.name)
 AND done.completed_at IS NULL AND (done.retry_after IS NULL OR done.retry_after<=now())
 AND NOT EXISTS(SELECT 1 FROM entity_merges m WHERE m.owner_id=d.owner_id AND m.merged_id IN(d.left_id,d.right_id) AND(m.undone_at IS NULL OR m.rule=$2))
 ORDER BY least(array_position($3::uuid[],d.left_id),array_position($3::uuid[],d.right_id)),
 greatest(array_position($3::uuid[],d.left_id),array_position($3::uuid[],d.right_id)),d.created_at,d.left_id,d.right_id LIMIT 1`
