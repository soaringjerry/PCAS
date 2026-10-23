package interpstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/soaringjerry/PCAS/internal/interpretation/comparison"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/postgres/kernel"
	"github.com/soaringjerry/PCAS/internal/postgres/sourcestore"
	"github.com/soaringjerry/PCAS/internal/worker"
)

type CompareGroup struct {
	Key  string `json:"key"`
	Kind string `json:"kind"`
	Name string `json:"name"`
}
type CompareMemory struct {
	OrganizeMemory
	Protected bool `json:"protected"`
}

// A group's progress is derived from claims, not a separate cursor. A memory
// can participate through its subject or a mention; self categories are only
// about the owner, not an arbitrary third person's preference.
var compareCurrent = ` FROM claims cl
 JOIN memory_records r ON (r.owner_id,r.id)=(cl.owner_id,cl.id)
 JOIN claim_revisions c ON (c.owner_id,c.claim_id,c.version)=(r.owner_id,r.id,r.version)
 JOIN record_versions rv ON (rv.owner_id,rv.record_id,rv.version)=(r.owner_id,r.id,r.version)
 WHERE r.state='active' AND rv.state='active' AND cl.retired='' AND cl.organized >= ` + kernel.OrganizeRequiredSQL("c.category", "$1") + `
 AND claim_source_is_current(cl.owner_id,cl.id,r.version,now())`

const compareGroupKeys = `SELECT key,kind,name FROM status_current_members
 WHERE (owner_id,claim_id,claim_version)=(c.owner_id,c.claim_id,c.version)`

func nextCompareGroupTx(ctx context.Context, tx pgx.Tx, owner memory.ID, version int) (CompareGroup, error) {
	b, err := nextComparisonBatchTx(ctx, tx, owner, version)
	if err != nil {
		return CompareGroup{}, err
	}
	if b == nil {
		return CompareGroup{}, pgx.ErrNoRows
	}
	return b.Group, nil
}

func enqueueCompareTx(ctx context.Context, tx pgx.Tx, owner memory.ID, now time.Time, version int, entity bool, anchors ...memory.Ref) (bool, error) {
	stage := comparison.Stage
	if entity {
		stage = comparison.EntityStage
		if version == kernel.CompareVersion {
			version = kernel.EntityCompareVersion
		}
	}
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended(current_database()||':'||current_schema()||':queue:'||$1||':'||$2,0))", string(owner), stage); err != nil {
		return false, err
	}
	var pending bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM memory_jobs WHERE owner_id=$1 AND stage LIKE $2 AND state IN ('queued','leased'))", string(owner), stage+":%").Scan(&pending); err != nil || pending {
		return false, err
	}

	if entity {
		var left, right string
		err := tx.QueryRow(ctx, aliasPendingSQL, string(owner), version, []string(nil)).Scan(&left, &right)
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
	}

	var anchor memory.Ref
	if len(anchors) > 0 {
		anchor = anchors[0]
	} else {
		var err error
		anchor, err = SeedOrganizeGroupsTx(ctx, tx, owner)
		if err != nil {
			return false, err
		}
	}
	id := memory.NewID()
	_, err := tx.Exec(ctx, `INSERT INTO memory_jobs(id,owner_id,record_id,record_version,stage,priority,available_at)
 VALUES($1,$2,$3,$4,$5,$6,$7)`, string(id), string(owner), string(anchor.ID), anchor.Version, fmt.Sprintf("%s:%d:%s", stage, version, id), kernel.ComparePriority, now)
	return err == nil, err
}

func (s *Interp) ScheduleCompare(ctx context.Context, now time.Time) (int, error) {
	return s.ScheduleCompareVersion(ctx, now, kernel.CompareVersion)
}
func (s *Interp) ScheduleCompareVersion(ctx context.Context, now time.Time, version int) (int, error) {
	if now.IsZero() || version < 1 {
		return 0, memory.ErrInvalid
	}
	if _, ok := s.TextModel(); !ok {
		return 0, nil
	}
	count := 0
	rows, err := s.Pool.Query(ctx, "SELECT DISTINCT cl.owner_id::text"+compareCurrent, kernel.OrganizeVersion)
	if err != nil {
		return count, err
	}
	owners := []memory.ID{}
	for rows.Next() {
		var owner memory.ID
		if err := rows.Scan(&owner); err != nil {
			rows.Close()
			return count, err
		}
		owners = append(owners, owner)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return count, err
	}
	var failed error
	for _, owner := range owners {
		for _, entity := range []bool{false, true} {
			v := version
			if entity && version == kernel.CompareVersion {
				v = kernel.EntityCompareVersion
			}
			var input *ComparisonBatch
			if entity {
				pair, e := s.nextEntityPair(ctx, owner, v)
				if e != nil {
					failed = e
					continue
				}
				if pair == nil {
					continue
				}
			} else {
				var e error
				input, e = s.NextComparisonBatch(ctx, owner, v)
				if e != nil {
					failed = e
					continue
				}
				if input == nil {
					continue
				}
			}
			e := kernel.BackgroundResultTx(ctx, s.Pool, owner, func(ctx context.Context, tx pgx.Tx) error {
				if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended(current_database()||':'||current_schema()||':queue:'||$1||':'||$2,0))", string(owner), kernel.BaseStage(map[bool]string{false: comparison.Stage, true: comparison.EntityStage}[entity])); err != nil {
					return err
				}
				if input != nil {
					var complete bool
					if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM memory_comparison_batches WHERE owner_id=$1 AND group_key=$2 AND block_a=$3 AND block_b=$4 AND fingerprint=$5 AND rule>=$6 AND completed_at IS NOT NULL)", string(owner), input.Group.Key, input.A, input.B, input.Fingerprint, v).Scan(&complete); err != nil {
						return err
					}
					if complete {
						return nil
					}
				}
				queued, err := enqueueCompareTx(ctx, tx, owner, now, v, entity)
				if queued {
					count++
				}
				return err
			})
			if e != nil {
				stage := comparison.Stage
				if entity {
					stage = comparison.EntityStage
				}
				if !s.ScheduleYielded(ctx, owner, stage, e) {
					failed = e
				}
			}
		}
	}
	err = failed

	return count, err
}

func (s *Interp) RunCompare(ctx context.Context, logger *slog.Logger) {
	timer := time.NewTicker(kernel.CompareInterval)
	defer timer.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		if _, err := s.ScheduleCompare(ctx, time.Now()); err != nil && ctx.Err() == nil {
			s.RecordScheduleFailure(ctx, comparison.Stage, err)
		}
		if _, err := s.ScheduleEntityCandidates(ctx, time.Now()); err != nil && ctx.Err() == nil {
			s.RecordScheduleFailure(ctx, EntityCandidatesStage, err)
		}
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
	}
}

func compareJobVersion(j worker.Job) (int, error) {
	parts := strings.SplitN(j.Stage, ":", 3)
	if len(parts) != 3 {
		return 0, memory.ErrInvalid
	}
	v, err := strconv.Atoi(parts[1])
	if err != nil || v < 1 {
		return 0, memory.ErrInvalid
	}
	return v, nil
}

// CompareStore gives the statement comparison service its reads and writes.
func (s *Interp) CompareStore() comparison.Store { return compareStore{s} }

type compareStore struct{ store *Interp }

func (a compareStore) Reschedule(ctx context.Context) {
	if _, err := a.store.ScheduleCompare(ctx, time.Now()); err != nil {
		a.store.RecordScheduleFailure(ctx, comparison.Stage, err)
	}
}

func (a compareStore) Session(ctx context.Context) (comparison.Session, func(), error) {
	conn, release, err := a.store.ComparisonConnection(ctx)
	if err != nil {
		return nil, nil, err
	}
	return compareSession{store: a.store, conn: conn}, release, nil
}

type compareSession struct {
	store *Interp
	conn  *pgxpool.Conn
}

func (a compareSession) Ready() error {
	unavailable := &worker.JobError{Code: "provider_unavailable", Until: time.Now().Add(kernel.CompareInterval), NoAttempt: true}
	if p, ok := a.store.TextModel(); !ok || !a.store.ModelAvailable(p.ID) {
		return unavailable
	}
	return nil
}

func (a compareSession) NextBatch(ctx context.Context, j worker.Job, version int) (json.RawMessage, []memory.Ref, bool, error) {
	var input *ComparisonBatch
	err := pgx.BeginFunc(ctx, a.conn, func(tx pgx.Tx) error {
		if err := kernel.LockJob(ctx, tx, j); err != nil {
			return err
		}
		var err error
		input, err = nextComparisonBatchTx(ctx, tx, j.OwnerID, version)
		if err != nil {
			return err
		}
		if input == nil {
			return kernel.Acknowledge(ctx, tx, j)
		}
		return a.store.HourlyLimit(ctx, tx, comparison.Stage, "compare_hourly_limit")
	})
	if err != nil || input == nil {
		return nil, nil, false, err
	}
	return kernel.JSON(input), input.Refs, true, nil
}

func (a compareSession) Apply(ctx context.Context, j worker.Job, record json.RawMessage, edges []comparison.Edge, valid bool) error {
	var input ComparisonBatch
	if err := json.Unmarshal(record, &input); err != nil {
		return err
	}
	// A result that was paid before rule versions were saved used rule one.
	if input.Rule == 0 {
		input.Rule = 1
	}
	if len(input.Memories) != len(input.Refs) {
		return memory.ErrInvalid
	}
	for i := range input.Memories {
		input.Memories[i].Ref = input.Refs[i]
	}
	return kernel.BackgroundResultTx(ctx, a.conn, j.OwnerID, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended(current_database()||':'||current_schema()||':queue:'||$1||':'||$2,0))", string(j.OwnerID), comparison.Stage); err != nil {
			return err
		}
		if err := kernel.LockJob(ctx, tx, j); err != nil {
			return err
		}
		if valid {
			if err := a.store.WriteComparisonTx(ctx, tx, j.OwnerID, input.Rule, input.Memories, edges, true); err != nil {
				return err
			}
		}
		if err := writeComparisonReceiptTx(ctx, tx, j.OwnerID, input.Rule, input, valid); err != nil {
			return err
		}
		if err := kernel.DiscardPaidResult(ctx, tx, j); err != nil {
			return err
		}
		return kernel.Acknowledge(ctx, tx, j)
	})
}

func (s *Interp) WriteComparisonTx(ctx context.Context, tx pgx.Tx, owner memory.ID, version int, batch []CompareMemory, edges []CompareEdge, valid bool) error {
	eligible, protected := map[int]bool{}, map[int]bool{}
	inputs := make([]map[string]any, 0, len(batch))
	for _, m := range batch {
		inputs = append(inputs, map[string]any{"id": m.Ref.ID, "version": m.Ref.Version, "n": m.N})
	}
	rows, err := tx.Query(ctx, `SELECT input.n,c.confirmation='confirmed',EXISTS(SELECT 1 FROM record_versions edited WHERE edited.owner_id=r.owner_id AND edited.record_id=r.id AND edited.version>1 AND edited.actor='user'),`+restoredMemorySQL("$3")+
		strings.Replace(compareCurrent, " WHERE r.state", ` JOIN jsonb_to_recordset($4::jsonb) input(id uuid,version integer,n integer) ON r.id=input.id AND r.version=input.version WHERE r.state`, 1)+`
 AND cl.owner_id=$2 ORDER BY r.id FOR UPDATE OF r,cl`, kernel.OrganizeVersion, string(owner), version, kernel.JSON(inputs))
	if err != nil {
		return err
	}
	for rows.Next() {
		var n int
		var confirmed, edited, restored bool
		if err := rows.Scan(&n, &confirmed, &edited, &restored); err != nil {
			rows.Close()
			return err
		}
		eligible[n] = true
		protected[n] = confirmed || edited || restored
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if valid {
		for _, edge := range resolveCompareEdges(edges, eligible, protected) {
			old, new := batch[edge.Old-1], batch[edge.New-1]
			if edge.Kind == "duplicate" {
				if _, err := tx.Exec(ctx, `INSERT INTO evidence(owner_id,id,source_id,source_version,target_id,target_version,locator,acquisition,stance)
 SELECT DISTINCT ON(e.source_id) e.owner_id,gen_random_uuid(),e.source_id,e.source_version,$3,$4,e.locator,e.acquisition,e.stance
 FROM evidence e WHERE e.owner_id=$1 AND e.target_id=$2 AND e.target_version=$5
 AND NOT EXISTS(SELECT 1 FROM evidence present WHERE present.owner_id=e.owner_id AND present.target_id=$3 AND present.target_version=$4 AND present.source_id=e.source_id)
 ORDER BY e.source_id,e.source_version DESC,e.id`, string(owner), string(old.Ref.ID), string(new.Ref.ID), new.Ref.Version, old.Ref.Version); err != nil {
					return err
				}
			}
			// The AFTER claims trigger sees only current membership. Invalidate
			// before retirement too, so an unselected claim's secondary groups
			// are still visible. Use the status layer's existing debounce path.
			if _, err := tx.Exec(ctx, "SELECT status_invalidate($1,$2)", string(owner), string(old.Ref.ID)); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, "UPDATE claims SET retired=$3,retired_by=$4,retired_at=now() WHERE owner_id=$1 AND id=$2", string(owner), string(old.Ref.ID), edge.Kind, string(new.Ref.ID)); err != nil {
				return err
			}
			// Keep older pointers terminal too. Supersession of a keeper also
			// supersedes its earlier duplicates rather than silently preserving
			// their old answers against the changed assertion.
			ancestors, err := kernel.QueryDocuments[string](ctx, tx, `WITH changed AS (
 UPDATE claims SET retired_by=$3,retired=CASE WHEN $4='superseded' THEN 'superseded' ELSE retired END WHERE owner_id=$1 AND retired_by=$2 RETURNING id)
 SELECT to_jsonb(id::text) FROM changed`, string(owner), string(old.Ref.ID), string(new.Ref.ID), edge.Kind)
			if err != nil {
				return err
			}
			if edge.Kind == "superseded" {
				for _, id := range append(ancestors, string(old.Ref.ID)) {
					if err := sourcestore.InvalidateTx(ctx, tx, memory.Scope{OwnerID: owner, IsOwner: true}, id); err != nil {
						return err
					}
				}
			}
		}
	}
	changed := []string{}
	for _, m := range batch {
		if !eligible[m.N] {
			continue
		}
		if !valid {
			continue
		}
		changed = append(changed, string(m.Ref.ID))
	}
	if len(changed) > 0 {
		// Only this bulk metadata update defers per-row invalidation. Other
		// claims writes (including retirement) retain the normal trigger path.
		if _, err := tx.Exec(ctx, "SET LOCAL pcas.defer_compared_invalidation='on'"); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "UPDATE claims SET compared=$3 WHERE owner_id=$1 AND id=ANY($2::uuid[])", string(owner), changed, version); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "SET LOCAL pcas.defer_compared_invalidation='off'"); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `SELECT status_invalidate_keys($1,ARRAY(SELECT DISTINCT key FROM status_current_members WHERE owner_id=$1 AND claim_id=ANY($2::uuid[])))`, string(owner), changed); err != nil {
			return err
		}
		return nil
	}
	return nil
}
