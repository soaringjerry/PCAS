package interpstore

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/soaringjerry/PCAS/internal/interpretation/organize"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/postgres/budgetstore"
	"github.com/soaringjerry/PCAS/internal/postgres/claimstore"
	"github.com/soaringjerry/PCAS/internal/postgres/kernel"
	"github.com/soaringjerry/PCAS/internal/postgres/memoryread"
	"github.com/soaringjerry/PCAS/internal/postgres/statestore"
	"github.com/soaringjerry/PCAS/internal/worker"
)

type (
	OrganizeMemory = organize.Memory
	OrganizeGroup  = organize.Group
)

// An area is a stable queue anchor; deleting/correcting any claim in a model
// batch cannot cascade-delete the job and prevent the other claims from writing.
func SeedOrganizeGroupsTx(ctx context.Context, tx pgx.Tx, owner memory.ID) (memory.Ref, error) {
	var anchor memory.Ref
	// Status and comparison schedules seed the same anchors. Fence user writes
	// first, then serialize only this owner's anchor initialization.
	if _, err := tx.Exec(ctx, "SELECT 1 FROM workspace_owners WHERE owner_id=$1 FOR KEY SHARE", string(owner)); err != nil {
		return anchor, err
	}
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended(current_database() || ':' || current_schema() || ':background-anchor:' || $1::text,0))", string(owner)); err != nil {
		return anchor, err
	}
	for _, name := range organize.Areas {
		id, err := claimstore.EntityTx(ctx, tx, owner, "area", name)
		if err != nil {
			return anchor, err
		}
		if anchor.ID == "" {
			anchor.ID, anchor.Kind = id, memory.EntityKind
			if err := tx.QueryRow(ctx, "SELECT version FROM memory_records WHERE owner_id=$1 AND id=$2", string(owner), string(id)).Scan(&anchor.Version); err != nil {
				return anchor, err
			}
		}
	}
	rows, err := tx.Query(ctx, "SELECT id::text,title FROM work_items WHERE owner_id=$1 AND kind='project' AND status<>'done' ORDER BY created_at,id", string(owner))
	if err != nil {
		return anchor, err
	}
	type project struct{ id, name string }
	projects := []project{}
	for rows.Next() {
		var p project
		if err := rows.Scan(&p.id, &p.name); err != nil {
			rows.Close()
			return anchor, err
		}
		projects = append(projects, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return anchor, err
	}
	for _, p := range projects {
		id, err := claimstore.EntityTx(ctx, tx, owner, "project", p.name)
		if err != nil {
			return anchor, err
		}
		if _, err := tx.Exec(ctx, `UPDATE entity_versions ev SET disambiguation=ev.disambiguation || jsonb_build_object('work_item_id',$3::text)
 FROM memory_records r WHERE (ev.owner_id,ev.entity_id,ev.version)=(r.owner_id,r.id,r.version)
 AND ev.owner_id=$1 AND ev.entity_id=$2 AND NOT(ev.disambiguation ? 'work_item_id')`, string(owner), string(id), p.id); err != nil {
			return anchor, err
		}
	}
	return anchor, nil
}

var OrganizeEligible = ` FROM claims cl JOIN memory_records r ON(r.owner_id,r.id)=(cl.owner_id,cl.id)
 JOIN claim_revisions c ON(c.owner_id,c.claim_id,c.version)=(r.owner_id,r.id,r.version)
 JOIN record_versions rv ON(rv.owner_id,rv.record_id,rv.version)=(r.owner_id,r.id,r.version)
 WHERE r.state='active' AND rv.state='active' AND cl.organized<` + kernel.OrganizeRequiredSQL("c.category", "$1") + ` AND cl.organize_after<=now()
 AND (coalesce(to_jsonb(cl)->>'retired','')='' OR ($1::int=3 AND c.category='rule' AND cl.organized>0))
 AND claim_source_is_current(r.owner_id,c.claim_id,c.version,now())`

func EnqueueOrganizeTx(ctx context.Context, tx pgx.Tx, owner memory.ID, now time.Time, version int) (bool, error) {
	var eligible, pending bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1"+OrganizeEligible+" AND cl.owner_id=$2)", version, string(owner)).Scan(&eligible); err != nil {
		return false, err
	}
	if !eligible {
		return false, nil
	}
	if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM memory_jobs WHERE owner_id=$1 AND stage LIKE 'memory.organize:%' AND state IN('queued','leased'))", string(owner)).Scan(&pending); err != nil {
		return false, err
	}
	if pending {
		return false, nil
	}
	anchor, err := SeedOrganizeGroupsTx(ctx, tx, owner)
	if err != nil {
		return false, err
	}
	id := memory.NewID()
	_, err = tx.Exec(ctx, `INSERT INTO memory_jobs(id,owner_id,record_id,record_version,stage,priority,available_at)
 VALUES($1,$2,$3,$4,$5,$6,$7)`, string(id), string(owner), string(anchor.ID), anchor.Version,
		fmt.Sprintf("%s:%d:%s", kernel.OrganizeStage, version, id), kernel.OrganizePriority, now)
	return err == nil, err
}

// No progress table: pending work is always derived from active claim versions.
func (s *Interp) ScheduleOrganize(ctx context.Context, now time.Time) (int, error) {
	if now.IsZero() {
		return 0, memory.ErrInvalid
	}
	p, ok := s.Models.Get(s.Models.ExtractionID())
	if !ok || p.Embedding || p.Transcription {
		return 0, nil
	}
	count := 0
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended(current_database() || ':' || current_schema() || ':organize-schedule',0))"); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, "SELECT DISTINCT cl.owner_id::text"+OrganizeEligible, kernel.OrganizeVersion)
		if err != nil {
			return err
		}
		owners := []memory.ID{}
		for rows.Next() {
			var owner memory.ID
			if err := rows.Scan(&owner); err != nil {
				rows.Close()
				return err
			}
			owners = append(owners, owner)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, owner := range owners {
			scope := memory.Scope{OwnerID: owner, PrincipalID: "worker", IsOwner: true}
			if err := s.EnsureOwner(ctx, tx, scope); err != nil {
				return err
			}
			if err := kernel.OwnerLock(ctx, tx, owner); err != nil {
				return err
			}
			queued, err := EnqueueOrganizeTx(ctx, tx, owner, now, kernel.OrganizeVersion)
			if err != nil {
				return err
			}
			if queued {
				count++
			}
		}
		return nil
	})
	return count, err
}

func (s *Interp) RunOrganize(ctx context.Context, logger *slog.Logger) {
	timer := time.NewTicker(kernel.OrganizeInterval)
	defer timer.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		if _, err := s.ScheduleOrganize(ctx, time.Now()); err != nil && ctx.Err() == nil {
			s.RecordScheduleFailure(ctx, kernel.OrganizeStage, err)
		}
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
	}
}

func organizeVocabularyTx(ctx context.Context, tx pgx.Tx, owner memory.ID) ([]OrganizeGroup, error) {
	rows, err := tx.Query(ctx, `SELECT ev.entity_id::text,ev.entity_type,ev.name,coalesce(ev.disambiguation->>'description','')
 FROM entity_versions ev JOIN memory_records r ON(r.owner_id,r.id,r.version)=(ev.owner_id,ev.entity_id,ev.version)
 WHERE ev.owner_id=$1 AND r.state='active' AND ev.entity_type IN('project','topic','area') ORDER BY ev.entity_type,ev.name,ev.entity_id`, string(owner))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	groups := []OrganizeGroup{}
	for rows.Next() {
		var g OrganizeGroup
		if err := rows.Scan(&g.ID, &g.Type, &g.Name, &g.Description); err != nil {
			return nil, err
		}
		groups = append(groups, g)
	}
	return groups, rows.Err()
}

// Organizing, memory comparisons and same-person checks share one rolling-hour
// allowance. All three hold the organize-call session lock through reservation
// and generation, so checking this shared ledger cannot race another call.
// Count invocation reservations, including zero-cost and failed calls; other
// background stages and foreground calls have their own limits.
func organizeCompareHourlyTx(ctx context.Context, tx pgx.Tx, code string) error {
	stage := kernel.OrganizeStage
	if strings.HasPrefix(code, "compare") {
		stage = kernel.CompareStage
	}
	return kernel.HourlyLimit(ctx, tx, stage, code)
}

// OrganizeStore gives the organize service its reads and writes.
func (s *Interp) OrganizeStore() organize.Store { return organizeStore{s} }

type organizeStore struct{ store *Interp }

// Hold a dedicated session lock over generation, not the user's row lock:
// corrections/deletions can proceed, but a recovered lease cannot race a call.
func (a organizeStore) Session(ctx context.Context) (organize.Session, func(), error) {
	conn, release, err := a.store.OrganizeCallConnection(ctx, "organize_busy", kernel.OrganizeInterval)
	if err != nil {
		return nil, nil, err
	}
	return organizeSession{store: a.store, conn: conn}, release, nil
}

type organizeSession struct {
	store *Interp
	conn  *pgxpool.Conn
}

func (a organizeSession) Ready() error {
	p, ok := a.store.Models.Get(a.store.Models.ExtractionID())
	if !ok || p.Embedding || p.Transcription || !a.store.Models.Available(p.ID) {
		return &worker.JobError{Code: "provider_unavailable", Until: time.Now().Add(kernel.OrganizeInterval), NoAttempt: true}
	}
	return nil
}

func (a organizeSession) NextBatch(ctx context.Context, j worker.Job, version int) (organize.Batch, bool, error) {
	batch := organize.Batch{Memories: []OrganizeMemory{}}
	found := false
	err := pgx.BeginFunc(ctx, a.conn, func(tx pgx.Tx) error {
		if err := kernel.OwnerLock(ctx, tx, j.OwnerID); err != nil {
			return err
		}
		if err := kernel.LockJob(ctx, tx, j); err != nil {
			return err
		}
		if _, err := SeedOrganizeGroupsTx(ctx, tx, j.OwnerID); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT r.id::text,r.version,c.value #>> '{}',rv.expressed_at`+OrganizeEligible+`
 AND cl.owner_id=$2 ORDER BY EXISTS(SELECT 1 FROM evidence e JOIN archive_entries ae ON(ae.owner_id,ae.source_id,ae.source_version)=(e.owner_id,e.source_id,e.source_version)
 WHERE (e.owner_id,e.target_id,e.target_version)=(c.owner_id,c.claim_id,c.version)),r.created_at DESC,r.id LIMIT $3`, version, string(j.OwnerID), kernel.OrganizeBatchLimit)
		if err != nil {
			return err
		}
		for rows.Next() {
			m := OrganizeMemory{N: len(batch.Memories) + 1, Ref: memory.Ref{Kind: memory.ClaimKind}}
			if err := rows.Scan(&m.Ref.ID, &m.Ref.Version, &m.Text, &m.ExpressedAt); err != nil {
				rows.Close()
				return err
			}
			batch.Memories = append(batch.Memories, m)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if len(batch.Memories) == 0 {
			return kernel.Acknowledge(ctx, tx, j)
		}
		if err := organizeCompareHourlyTx(ctx, tx, "organize_hourly_limit"); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, "SELECT settings->>'timezone' FROM workspace_owners WHERE owner_id=$1", string(j.OwnerID)).Scan(&batch.Timezone); err != nil {
			return err
		}
		batch.Groups, err = organizeVocabularyTx(ctx, tx, j.OwnerID)
		found = err == nil
		return err
	})
	return batch, found, err
}

func (a organizeSession) Apply(ctx context.Context, j worker.Job, result organize.Result) (organize.Applied, error) {
	version, batch, items, timezone := result.Version, result.Memories, result.Items, result.Timezone
	exhausted := []memory.ID{}
	err := kernel.BackgroundResultTx(ctx, a.conn, j.OwnerID, func(ctx context.Context, tx pgx.Tx) error {
		if err := kernel.OwnerLock(ctx, tx, j.OwnerID); err != nil {
			return err
		}
		if err := kernel.LockJob(ctx, tx, j); err != nil {
			return err
		}
		// Lock every still-current row before creating groups or updating labels.
		eligible := map[int]bool{}
		for _, m := range batch {
			var id memory.ID
			err := tx.QueryRow(ctx, `SELECT r.id::text`+OrganizeEligible+` AND cl.owner_id=$2 AND cl.id=$3 AND r.version=$4 FOR UPDATE OF r,cl`, version, string(j.OwnerID), string(m.Ref.ID), m.Ref.Version).Scan(&id)
			if errors.Is(err, pgx.ErrNoRows) {
				continue
			}
			if err != nil {
				return err
			}
			eligible[m.N] = true
		}
		groups, err := resolveOrganizeGroupsTx(ctx, tx, j.OwnerID, items, organize.UsedDeclarations(result.Declared, items, eligible), eligible)
		if err != nil {
			return err
		}
		for _, m := range batch {
			if !eligible[m.N] {
				continue
			}
			item, valid := items[m.N]
			if !valid {
				var attempts int
				if err := tx.QueryRow(ctx, "UPDATE claims SET organize_attempts=organize_attempts+1 WHERE owner_id=$1 AND id=$2 RETURNING organize_attempts", string(j.OwnerID), string(m.Ref.ID)).Scan(&attempts); err != nil {
					return err
				}
				if _, err := tx.Exec(ctx, "UPDATE claims SET organize_after=clock_timestamp()+$3*interval '1 second' WHERE owner_id=$1 AND id=$2", string(j.OwnerID), string(m.Ref.ID), kernel.RetryDelay(attempts).Seconds()); err != nil {
					return err
				}
				exhausted = append(exhausted, m.Ref.ID)
				if err := kernel.StageEvent(ctx, tx, j.OwnerID, kernel.OrganizeStage, "failure", "organize_invalid_output", 1); err != nil {
					return err
				}
				continue
			}
			if _, err := tx.Exec(ctx, "UPDATE claim_revisions SET category=$4,durable=$5 WHERE owner_id=$1 AND claim_id=$2 AND version=$3", string(j.OwnerID), string(m.Ref.ID), m.Ref.Version, item.Category, item.Durable); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, "DELETE FROM claim_mentions WHERE owner_id=$1 AND claim_id=$2 AND claim_version=$3 AND role IN('project','topic','area')", string(j.OwnerID), string(m.Ref.ID), m.Ref.Version); err != nil {
				return err
			}
			for _, g := range groups[m.N] {
				if _, err := tx.Exec(ctx, "INSERT INTO claim_mentions(owner_id,claim_id,claim_version,entity_id,role) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING", string(j.OwnerID), string(m.Ref.ID), m.Ref.Version, string(g.ID), g.Type); err != nil {
					return err
				}
			}
			cm := kernel.CardMemory{N: 1, Text: m.Text, Ref: m.Ref}
			if m.ExpressedAt != nil {
				cm.ExpressedAt = m.ExpressedAt.Format(time.RFC3339Nano)
			}
			deadlines := append([]kernel.CardDeadline{}, item.Deadlines...)
			for i := range deadlines {
				deadlines[i].N = 1
			}
			var previousDates []string
			if err := tx.QueryRow(ctx, "SELECT coalesce(array_agg(id::text),'{}') FROM deadlines WHERE owner_id=$1 AND claim_id=$2", string(j.OwnerID), string(m.Ref.ID)).Scan(&previousDates); err != nil {
				return err
			}
			if err := statestore.WriteOrganizedDeadlinesTx(ctx, tx, j.OwnerID, cm, deadlines, timezone); err != nil {
				return err
			}
			var withdrawnDates int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM unnest($2::uuid[]) old(id) WHERE NOT EXISTS(SELECT 1 FROM deadlines d WHERE d.owner_id=$1 AND d.id=old.id)`, string(j.OwnerID), previousDates).Scan(&withdrawnDates); err != nil {
				return err
			}
			if withdrawnDates > 0 {
				if err := kernel.StageEvent(ctx, tx, j.OwnerID, kernel.OrganizeStage, "success", "reorganize_deadlines_withdrawn", withdrawnDates); err != nil {
					return err
				}
			}
			removedRequirements, err := tx.Exec(ctx, "DELETE FROM assistant_requirements WHERE owner_id=$1 AND claim_id=$2", string(j.OwnerID), string(m.Ref.ID))
			if err != nil {
				return err
			}
			if item.Category != "rule" && removedRequirements.RowsAffected() > 0 {
				if err := kernel.StageEvent(ctx, tx, j.OwnerID, kernel.OrganizeStage, "success", "reorganize_requirements_withdrawn", int(removedRequirements.RowsAffected())); err != nil {
					return err
				}
			}
			if item.Category == "rule" {
				if _, err := tx.Exec(ctx, "INSERT INTO assistant_requirements(owner_id,claim_id,claim_version,unrestricted,scope) VALUES($1,$2,$3,$4,$5)", string(j.OwnerID), string(m.Ref.ID), m.Ref.Version, item.Unrestricted, item.Scope); err != nil {
					return err
				}
			}
			if _, err := tx.Exec(ctx, "UPDATE claims SET organized=$3,organize_attempts=0,organize_after='-infinity' WHERE owner_id=$1 AND id=$2", string(j.OwnerID), string(m.Ref.ID), version); err != nil {
				return err
			}
		}
		if err := kernel.Acknowledge(ctx, tx, j); err != nil {
			return err
		}
		if err := kernel.DiscardPaidResult(ctx, tx, j); err != nil {
			return err
		}
		_, err = EnqueueOrganizeTx(ctx, tx, j.OwnerID, time.Now(), kernel.OrganizeVersion)
		return err
	})
	if err != nil {
		return organize.Applied{}, err
	}
	return organize.Applied{Deferred: exhausted}, nil
}

// Organizing retries are automatic, including transaction recovery. Reserve a
// fresh invocation through the shared budget policy, then bind it to the fenced
// job. Each call's reservation ID also deduplicates its usage log.
func (s *Interp) reserveOrganizeCost(ctx context.Context, j worker.Job, cost float64) (string, error) {
	reserveCtx, cancel := context.WithTimeout(ctx, kernel.BackgroundWriteTimeout)
	id, err := s.ReserveModelCostID(reserveCtx, j.OwnerID, cost, &j)
	cancel()
	if errors.Is(err, memory.ErrUnavailable) {
		var timezone string
		if err := s.Pool.QueryRow(ctx, "SELECT settings->>'timezone' FROM workspace_owners WHERE owner_id=$1", string(j.OwnerID)).Scan(&timezone); err != nil {
			return "", err
		}
		loc, err := time.LoadLocation(timezone)
		if err != nil {
			return "", err
		}
		jitter, err := rand.Int(rand.Reader, big.NewInt(int64(budgetstore.BudgetJitter)+1))
		if err != nil {
			return "", err
		}
		return "", &worker.JobError{Code: "budget_deferred", Until: budgetstore.NextBudgetDay(time.Now(), loc).Add(time.Duration(jitter.Int64())), NoAttempt: true}
	}
	if err != nil {
		return "", kernel.BackgroundWriteError(ctx, err)
	}
	err = pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		if err := kernel.LockJob(ctx, tx, j); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, "UPDATE background_usage SET job_id=$3 WHERE owner_id=$1 AND id=$2", string(j.OwnerID), id, string(j.ID))
		return err
	})
	if err != nil {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_, cleanupErr := s.Pool.Exec(cleanupCtx, "DELETE FROM background_usage WHERE owner_id=$1 AND id=$2", string(j.OwnerID), id)
		if cleanupErr != nil {
			return "", cleanupErr
		}
		return "", err
	}
	return id, nil
}

func FindOrganizeGroupTx(ctx context.Context, tx pgx.Tx, owner memory.ID, kind, name string) (memory.ID, error) {
	var id memory.ID
	err := tx.QueryRow(ctx, `SELECT ev.entity_id::text FROM entity_versions ev JOIN memory_records r ON(r.owner_id,r.id,r.version)=(ev.owner_id,ev.entity_id,ev.version)
 WHERE ev.owner_id=$1 AND ev.entity_type=$2 AND r.state='active' AND EXISTS(SELECT 1 FROM aliases a WHERE a.owner_id=ev.owner_id AND a.entity_id=ev.entity_id AND lower(btrim(a.alias,$4))=lower($3))
 ORDER BY r.created_at,r.id LIMIT 1`, string(owner), kind, strings.TrimSpace(name), claimstore.EntityWhitespace).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return memoryread.ResolveMergedEntityTx(ctx, tx, owner, id)
}

// declarations are the declared groups that a current statement uses.
func resolveOrganizeGroupsTx(ctx context.Context, tx pgx.Tx, owner memory.ID, items map[int]organize.Item, declarations []OrganizeGroup, eligible map[int]bool) (map[int][]OrganizeGroup, error) {
	out := map[int][]OrganizeGroup{}
	// Take the same alias-creation lock as entityTx before resolving any names.
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", string(owner)+":entities"); err != nil {
		return nil, err
	}
	for _, g := range declarations {
		id, err := FindOrganizeGroupTx(ctx, tx, owner, g.Type, g.Name)
		if err != nil {
			return nil, err
		}
		if id != "" {
			continue
		}
		id, err = claimstore.EntityTx(ctx, tx, owner, g.Type, g.Name)
		if err != nil {
			return nil, err
		}
		if g.Description != "" {
			if _, err := tx.Exec(ctx, "UPDATE entity_versions SET disambiguation=disambiguation || jsonb_build_object('description',$3::text) WHERE owner_id=$1 AND entity_id=$2 AND version=1", string(owner), string(id), g.Description); err != nil {
				return nil, err
			}
		}
	}
	for n, item := range items {
		if !eligible[n] {
			continue
		}
		for _, g := range item.Groups {
			id, err := FindOrganizeGroupTx(ctx, tx, owner, g.Type, g.Name)
			if err != nil {
				return nil, err
			}
			if id != "" {
				g.ID = id
				out[n] = append(out[n], g)
			}
		}
	}
	return out, nil
}
