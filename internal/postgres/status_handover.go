package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/soaringjerry/PCAS/internal/interpretation/extraction"
	"github.com/soaringjerry/PCAS/internal/postgres/kernel"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/currentstate/handover"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/worker"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

var handoverTitles = handover.Titles

// The input assembly below is the context builder of the global handover service.
type handoverSection = handover.Section
type handoverOutput = handover.Output

type handoverDependency struct {
	Key     string `json:"key"`
	BuiltAt string `json:"builtAt"`
}
type handoverCardField struct {
	Field string `json:"field"`
	Items []int  `json:"items"`
}
type handoverCard struct {
	Key    string              `json:"key"`
	Kind   string              `json:"kind"`
	Name   string              `json:"name"`
	Fields []handoverCardField `json:"fields"`
}
type handoverInput struct {
	Cards        []handoverCard                   `json:"cards"`
	Memories     []cardMemory                     `json:"memories"`
	Deadlines    []workspace.LibraryDeadline      `json:"deadlines"`
	Requirements []workspace.AssistantRequirement `json:"requirements"`
	InputHash    string                           `json:"inputHash"`
}

func enqueueStatusHandoversTx(ctx context.Context, tx pgx.Tx, anchors map[memory.ID]memory.Ref, now time.Time) (int, error) {
	count := 0
	for owner, anchor := range anchors {
		var pending bool
		var hash string
		var old *string
		var built *time.Time
		if err := tx.QueryRow(ctx, `SELECT library_handover_hash($1),
 (SELECT input_hash FROM handovers WHERE owner_id=$1 AND rule>=$2),
 (SELECT built_at FROM handovers WHERE owner_id=$1),
 EXISTS(SELECT 1 FROM memory_jobs WHERE owner_id=$1 AND stage LIKE 'memory.handover:%' AND state IN('queued','leased'))`, string(owner), HandoverVersion).Scan(&hash, &old, &built, &pending); err != nil {
			return count, err
		}
		if pending || old != nil && *old == hash {
			continue
		}
		if old == nil {
			var exists bool
			if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM library_handover_members($1))", string(owner)).Scan(&exists); err != nil {
				return count, err
			}
			if !exists {
				continue
			}
		}
		due := now
		if built != nil {
			due = maxTime(now, built.Add(time.Hour))
		}
		id := memory.NewID()
		if _, err := tx.Exec(ctx, `INSERT INTO memory_jobs(id,owner_id,record_id,record_version,stage,priority,available_at)
 VALUES($1,$2,$3,$4,$5,$6,$7)`, string(id), string(owner), string(anchor.ID), anchor.Version, fmt.Sprintf("%s:%d:%s", HandoverStage, HandoverVersion, id), HandoverPriority, due); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

// Use evidence roles as well as acquisition: imported AI assertions must never
// be allowed into a handover while batch 2's trust reader is being integrated.
func statusTrustTx(ctx context.Context, tx pgx.Tx, owner memory.ID, m workspace.Memory) (string, error) {
	var inferred bool
	var distinct int
	var acquisition string
	err := tx.QueryRow(ctx, `SELECT c.acquisition IN('inferred') OR EXISTS(
 SELECT 1 FROM evidence e JOIN sources src ON(src.owner_id,src.id)=(e.owner_id,e.source_id)
 LEFT JOIN source_contexts sc ON(sc.owner_id,sc.source_id,sc.source_version)=(e.owner_id,e.source_id,e.source_version)
 WHERE e.owner_id=c.owner_id AND e.target_id=c.claim_id AND e.target_version=c.version AND e.stance='supports'
 AND (sc.role IN('assistant','system','tool') OR src.connector IN('ai','assistant','system','tool','agent','actions'))),
 (SELECT count(DISTINCT coalesce(nullif(sc.conversation_key,''),t.conversation_id::text,e.source_id::text))
 FROM evidence e LEFT JOIN source_contexts sc ON(sc.owner_id,sc.source_id,sc.source_version)=(e.owner_id,e.source_id,e.source_version)
 JOIN sources src ON(src.owner_id,src.id)=(e.owner_id,e.source_id)
 LEFT JOIN desk_turns t ON t.owner_id=src.owner_id AND t.request_id::text=lower(src.external_id) AND src.connector IN('desk','capture','desk-incomplete')
 WHERE e.owner_id=c.owner_id AND e.target_id=c.claim_id AND e.target_version=c.version AND e.stance='supports'),c.acquisition
 FROM claim_revisions c JOIN record_versions rv ON(rv.owner_id,rv.record_id,rv.version)=(c.owner_id,c.claim_id,c.version)
 WHERE c.owner_id=$1 AND c.claim_id=$2 AND c.version=$3`, string(owner), m.ID, m.Version).Scan(&inferred, &distinct, &acquisition)
	if err != nil {
		return "", err
	}
	if inferred {
		return "inferred", nil
	}
	if acquisition == "reported" {
		return "reported", nil
	}
	if extraction.QualifiedCapture(m.Text) {
		return "tentative", nil
	}
	if distinct >= 2 {
		return "repeated", nil
	}
	return "stated", nil
}

func (s *Store) handoverInputTx(ctx context.Context, tx pgx.Tx, scope memory.Scope) (handoverInput, []handoverDependency, []memory.Ref, error) {
	out := handoverInput{Cards: []handoverCard{}, Memories: []cardMemory{}, Deadlines: []workspace.LibraryDeadline{}, Requirements: []workspace.AssistantRequirement{}}
	refs := []memory.Ref{}
	if err := tx.QueryRow(ctx, "SELECT library_handover_hash($1)", string(scope.OwnerID)).Scan(&out.InputHash); err != nil {
		return out, nil, refs, err
	}
	ids := []string{}
	rows, err := tx.Query(ctx, "SELECT claim_id::text FROM library_handover_members($1) ORDER BY claim_id", string(scope.OwnerID))
	if err != nil {
		return out, nil, refs, err
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return out, nil, refs, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, nil, refs, err
	}
	memories, err := s.readMemoriesTx(ctx, tx, scope, false, memoryReadOptions{ids: ids})
	if err != nil {
		return out, nil, refs, err
	}
	for _, m := range memories {
		trust, err := statusTrustTx(ctx, tx, scope.OwnerID, m)
		if err != nil {
			return out, nil, refs, err
		}
		if trust == "inferred" {
			continue
		}
		ref := memory.Ref{ID: memory.ID(m.ID), Version: m.Version, Kind: memory.ClaimKind}
		refs = append(refs, ref)
		out.Memories = append(out.Memories, cardMemory{N: len(out.Memories) + 1, Text: m.Text, Category: m.Category, Durable: m.Durable, Trust: trust, ExpressedAt: m.ExpressedAt, Ref: ref})
	}
	deadlines, err := s.libraryDeadlinesTx(ctx, tx, scope, workspace.DeadlineQuery{})
	if err != nil {
		return out, nil, refs, err
	}
	out.Deadlines = deadlines.Items
	requirements, err := s.assistantRequirementsTx(ctx, tx, scope)
	if err != nil {
		return out, nil, refs, err
	}
	out.Requirements = requirements.Items
	return out, []handoverDependency{}, refs, nil
}

func parseHandover(text string, memories []cardMemory) (string, bool) {
	trusts := make([]string, len(memories))
	for i, m := range memories {
		trusts[i] = m.Trust
	}
	return handover.Parse(text, trusts)
}
func emptyHandover() string { return handover.Empty() }

// HandoverStore gives the global handover service its reads and writes.
func (s *Store) HandoverStore() handover.Store { return handoverStore{s} }

type handoverStore struct{ store *Store }

func (a handoverStore) Exclusive(ctx context.Context) (func(), error) {
	return statusCallLock(ctx, a.store)
}

func (a handoverStore) Read(ctx context.Context, j worker.Job, fn func(handover.Reader) error) error {
	return pgx.BeginTxFunc(ctx, a.store.Pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead}, func(tx pgx.Tx) error {
		if err := kernel.LockJob(ctx, tx, j); err != nil {
			return err
		}
		return fn(handoverUnit{ctx: ctx, tx: tx, job: j, store: a.store})
	})
}

func (a handoverStore) DiscardResult(ctx context.Context, j worker.Job) error {
	return pgx.BeginFunc(ctx, a.store.Pool, func(tx pgx.Tx) error { return discardPaidResultTx(ctx, tx, j) })
}

func (a handoverStore) RetryLater(j worker.Job, code string) error {
	return &worker.JobError{Code: code, Until: time.Now().Add(kernel.RetryDelay(j.Attempts)), NoAttempt: false}
}

func (a handoverStore) Apply(ctx context.Context, j worker.Job, fn func(handover.Unit) error) error {
	return backgroundResultTx(ctx, a.store.Pool, j.OwnerID, func(ctx context.Context, tx pgx.Tx) error {
		if err := kernel.LockJob(ctx, tx, j); err != nil {
			return err
		}
		return fn(handoverUnit{ctx: ctx, tx: tx, job: j, store: a.store})
	})
}

type handoverUnit struct {
	ctx   context.Context
	tx    pgx.Tx
	job   worker.Job
	store *Store
}

func (u handoverUnit) WrittenAt() (*time.Time, error) {
	var built *time.Time
	err := u.tx.QueryRow(u.ctx, "SELECT (SELECT built_at FROM handovers WHERE owner_id=$1)", string(u.job.OwnerID)).Scan(&built)
	return built, err
}

func (u handoverUnit) Input() (json.RawMessage, []memory.Ref, error) {
	input, _, refs, err := u.store.handoverInputTx(u.ctx, u.tx, memory.Scope{OwnerID: u.job.OwnerID, PrincipalID: "worker", IsOwner: true})
	if err != nil {
		return nil, nil, err
	}
	return kernel.JSON(map[string]any{"input": input, "refs": refs, "memories": input.Memories, "requirements": input.Requirements, "deadlines": input.Deadlines}), refs, nil
}

// If the input changed during generation, the valid text is kept and marked
// stale, so that the next hourly update can catch up without discarding it.
func (u handoverUnit) SaveHandover(body, inputHash string) error {
	_, err := u.tx.Exec(u.ctx, `INSERT INTO handovers(owner_id,body,rule,built_at,stale,depends,input_hash)
 VALUES($1,$2,$3,clock_timestamp(),$4 IS DISTINCT FROM library_handover_hash($1),'[]',$4)
 ON CONFLICT(owner_id) DO UPDATE SET body=excluded.body,rule=excluded.rule,built_at=excluded.built_at,stale=excluded.stale,depends='[]',input_hash=excluded.input_hash`, string(u.job.OwnerID), body, HandoverVersion, inputHash)
	return err
}

func (u handoverUnit) DiscardResult() error { return discardPaidResultTx(u.ctx, u.tx, u.job) }
func (u handoverUnit) Acknowledge() error   { return kernel.Acknowledge(u.ctx, u.tx, u.job) }
