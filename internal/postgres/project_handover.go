package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/worker"
	"github.com/soaringjerry/PCAS/internal/workspace"
	"github.com/soaringjerry/PCAS/internal/workspace/effort"
	"github.com/soaringjerry/PCAS/internal/workspace/projecthandover"
)

const ProjectHandoverStage = projecthandover.Stage
const EffortStage = effort.Stage

// The input assembly below is the context builder of the project handover service.
type projectInputPart = projecthandover.Part
type projectHandoverInput = projecthandover.Input

func studioInstalledTx(ctx context.Context, tx pgx.Tx) (bool, error) {
	var ok bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema() AND c.relname='project_handovers')`).Scan(&ok)
	return ok, err
}
func emptyProjectHandover(id string) workspace.ProjectHandover { return projecthandover.Empty(id) }

func projectHandoverTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, id string) (workspace.ProjectHandover, error) {
	out := emptyProjectHandover(id)
	var raw json.RawMessage
	var written *time.Time
	err := tx.QueryRow(ctx, `SELECT h.body,h.written_at,h.input_events<>(SELECT count(*) FROM project_input_events e WHERE e.owner_id=h.owner_id AND e.project_id=h.project_id)
 FROM project_handovers h WHERE h.owner_id=$1 AND h.project_id=$2`, string(scope.OwnerID), id).Scan(&raw, &written, &out.Stale)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM project_input_events WHERE owner_id=$1 AND project_id=$2)`, string(scope.OwnerID), id).Scan(&out.Stale)
		return out, err
	}
	if err != nil {
		return out, err
	}
	stale := out.Stale
	if err = json.Unmarshal(raw, &out); err != nil {
		return out, err
	}
	out.ProjectID = id
	out.Stale = stale
	if written != nil {
		at := written.UTC().Format(time.RFC3339Nano)
		out.WrittenAt = &at
	}
	return out, nil
}
func (s *Store) ReadProjectHandover(ctx context.Context, scope memory.Scope, id string) (workspace.ProjectHandover, error) {
	out := emptyProjectHandover(id)
	if err := requireOwner(scope); err != nil {
		return out, err
	}
	if !memory.ID(id).Valid() {
		return out, memory.ErrInvalid
	}
	err := pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		item, err := getItem(ctx, tx, scope, id)
		if err != nil {
			return err
		}
		if item.Kind != "project" {
			return memory.ErrInvalid
		}
		out, err = projectHandoverTx(ctx, tx, scope, id)
		return err
	})
	return out, err
}
func enqueueProjectHandoversTx(ctx context.Context, tx pgx.Tx, owner memory.ID, anchor memory.Ref, now time.Time) (int, error) {
	installed, err := studioInstalledTx(ctx, tx)
	if err != nil || !installed {
		return 0, err
	}
	type candidate struct {
		id      string
		written *time.Time
	}
	candidates := []candidate{}
	rows, err := tx.Query(ctx, `SELECT w.id::text,h.written_at FROM work_items w
 LEFT JOIN project_handovers h ON(h.owner_id,h.project_id)=(w.owner_id,w.id)
 WHERE w.owner_id=$1 AND w.kind='project' AND w.status<>'done'
 AND coalesce(h.input_events,-1)<>(SELECT count(*) FROM project_input_events e WHERE (e.owner_id,e.project_id)=(w.owner_id,w.id))
 AND NOT EXISTS(SELECT 1 FROM memory_jobs j WHERE j.owner_id=w.owner_id AND j.stage LIKE $2||':'||w.id::text||':%' AND j.state IN('queued','leased'))
 ORDER BY coalesce(h.written_at,w.created_at),w.id`, string(owner), ProjectHandoverStage)
	if err != nil {
		return 0, err
	}
	for rows.Next() {
		var c candidate
		if err = rows.Scan(&c.id, &c.written); err != nil {
			rows.Close()
			return 0, err
		}
		candidates = append(candidates, c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	for _, c := range candidates {
		due := now
		if c.written != nil {
			due = maxTime(due, c.written.Add(time.Hour))
		}
		if _, err = tx.Exec(ctx, `INSERT INTO memory_jobs(id,owner_id,record_id,record_version,stage,priority,available_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, string(memory.NewID()), string(owner), string(anchor.ID), anchor.Version, ProjectHandoverStage+":"+c.id+":"+string(memory.NewID()), HandoverPriority, due); err != nil {
			return 0, err
		}
	}
	return len(candidates), nil
}
func (s *Store) projectHandoverInputTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, id string) (projectHandoverInput, []memory.Ref, error) {
	out := projectHandoverInput{Parts: []projectInputPart{}, Deadlines: []workspace.LibraryDeadline{}}
	var err error
	out.Project, err = getItem(ctx, tx, scope, id)
	if err != nil {
		return out, nil, err
	}
	// These legacy overview fields are retired and never model input.
	out.Project.Progress = ""
	out.Project.NextSteps = nil
	out.Project.ProjectHandover = nil
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM project_input_events WHERE owner_id=$1 AND project_id=$2`, string(scope.OwnerID), id).Scan(&out.Events); err != nil {
		return out, nil, err
	}
	items, err := queryDocuments[workspace.Item](ctx, tx, `SELECT document FROM work_items WHERE owner_id=$1 AND (id=$2 OR project_id=$2) ORDER BY id`, string(scope.OwnerID), id)
	if err != nil {
		return out, nil, err
	}
	for _, item := range items {
		item.Progress = ""
		item.NextSteps = nil
		item.ProjectHandover = nil
		out.Parts = append(out.Parts, projectInputPart{Evidence: workspace.ProjectEvidence{Kind: "item", ID: item.ID}, Content: item})
	}
	docs, err := queryDocuments[workspace.Doc](ctx, tx, `SELECT d.document FROM work_documents d JOIN work_items w ON(w.owner_id,w.id)=(d.owner_id,d.thing_id) WHERE d.owner_id=$1 AND (w.id=$2 OR w.project_id=$2) ORDER BY d.id`, string(scope.OwnerID), id)
	if err != nil {
		return out, nil, err
	}
	for _, doc := range docs {
		version := doc.Version
		if version == 0 {
			version = 1
		}
		out.Parts = append(out.Parts, projectInputPart{Evidence: workspace.ProjectEvidence{Kind: "documentVersion", ID: doc.ID, Version: version}, Content: doc})
	}
	runs, err := queryDocuments[workspace.Run](ctx, tx, `SELECT document FROM (SELECT a.document,a.created_at,a.id,row_number() OVER(ORDER BY a.created_at DESC,a.id DESC) AS ordinal FROM agent_runs a JOIN work_items w ON(w.owner_id,w.id)=(a.owner_id,a.thing_id) WHERE a.owner_id=$1 AND (w.id=$2 OR w.project_id=$2) AND a.status='done') r ORDER BY created_at,id`, string(scope.OwnerID), id)
	if err != nil {
		return out, nil, err
	}
	latest := ""
	for _, run := range runs {
		if run.Adopted == nil {
			latest = run.ID
		}
	}
	for _, run := range runs {
		if run.Adopted != nil || run.ID == latest {
			out.Parts = append(out.Parts, projectInputPart{Evidence: workspace.ProjectEvidence{Kind: "run", ID: run.ID}, Content: map[string]any{"output": run.Output, "adopted": run.Adopted, "createdAt": run.CreatedAt, "finishedAt": run.FinishedAt}})
		}
	}
	ids, err := queryDocuments[string](ctx, tx, `SELECT to_jsonb(r.id::text) FROM memory_records r JOIN claims cl ON(cl.owner_id,cl.id)=(r.owner_id,r.id) JOIN claim_revisions c ON(c.owner_id,c.claim_id,c.version)=(r.owner_id,r.id,r.version)
 WHERE r.owner_id=$1 AND r.state='active' AND cl.retired='' AND claim_source_is_current(r.owner_id,r.id,r.version,now()) AND (
 c.scope->>'project_id'=$2 OR EXISTS(SELECT 1 FROM status_current_members m JOIN entity_versions ev ON ev.owner_id=m.owner_id AND 'entity:'||ev.entity_id::text=m.key JOIN memory_records er ON(er.owner_id,er.id,er.version)=(ev.owner_id,ev.entity_id,ev.version) WHERE m.owner_id=r.owner_id AND m.claim_id=r.id AND ev.disambiguation->>'work_item_id'=$2)) ORDER BY r.id`, string(scope.OwnerID), id)
	if err != nil {
		return out, nil, err
	}
	if ids == nil {
		ids = []string{}
	}
	memories, err := s.readMemoriesTx(ctx, tx, scope, false, memoryReadOptions{ids: ids})
	if err != nil {
		return out, nil, err
	}
	refs := []memory.Ref{}
	for _, m := range memories {
		out.Parts = append(out.Parts, projectInputPart{Evidence: workspace.ProjectEvidence{Kind: "memory", ID: m.ID, Version: m.Version}, Content: m})
		refs = append(refs, memory.Ref{ID: memory.ID(m.ID), Version: m.Version, Kind: memory.ClaimKind})
	}
	deadlines, err := s.libraryDeadlinesTx(ctx, tx, scope, workspace.DeadlineQuery{})
	if err != nil {
		return out, nil, err
	}
	set := map[string]bool{}
	for _, id := range ids {
		set[id] = true
	}
	for _, d := range deadlines.Items {
		if set[d.MemoryID] {
			out.Deadlines = append(out.Deadlines, d)
		}
	}
	hash := sha256.Sum256(asJSON(out))
	out.Hash = hex.EncodeToString(hash[:])
	return out, refs, nil
}

// ProjectHandoverStore gives the project handover service its reads and writes.
func (s *Store) ProjectHandoverStore() projecthandover.Store { return projectHandoverStore{s} }

type projectHandoverStore struct{ store *Store }

func (a projectHandoverStore) Exclusive(ctx context.Context) (func(), error) {
	return statusCallLock(ctx, a.store)
}

func (a projectHandoverStore) RetryLater(j worker.Job, code string) error {
	return &worker.JobError{Code: code, Until: time.Now().Add(retryDelay(j.Attempts)), NoAttempt: false}
}

func (a projectHandoverStore) Read(ctx context.Context, j worker.Job, fn func(projecthandover.Reader) error) error {
	return pgx.BeginTxFunc(ctx, a.store.pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead}, func(tx pgx.Tx) error {
		if err := lockJob(ctx, tx, j); err != nil {
			return err
		}
		return fn(projectHandoverUnit{ctx: ctx, tx: tx, job: j, store: a.store, scope: memory.Scope{OwnerID: j.OwnerID, PrincipalID: "worker", IsOwner: true}})
	})
}

func (a projectHandoverStore) Apply(ctx context.Context, j worker.Job, fn func(projecthandover.Unit) error) error {
	return backgroundResultTx(ctx, a.store.pool, j.OwnerID, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT 1 FROM workspace_owners WHERE owner_id=$1 FOR UPDATE", string(j.OwnerID)); err != nil {
			return err
		}
		if err := lockJob(ctx, tx, j); err != nil {
			return err
		}
		return fn(projectHandoverUnit{ctx: ctx, tx: tx, job: j, store: a.store, scope: memory.Scope{OwnerID: j.OwnerID, PrincipalID: "worker", IsOwner: true}})
	})
}

type projectHandoverUnit struct {
	ctx   context.Context
	tx    pgx.Tx
	job   worker.Job
	store *Store
	scope memory.Scope
}

func (u projectHandoverUnit) Project(id string) (workspace.Item, bool, error) {
	item, err := getItem(u.ctx, u.tx, u.scope, id)
	if errors.Is(err, memory.ErrNotFound) {
		return item, false, nil
	}
	return item, err == nil, err
}

func (u projectHandoverUnit) Handover(project string) (workspace.ProjectHandover, error) {
	return projectHandoverTx(u.ctx, u.tx, u.scope, project)
}

func (u projectHandoverUnit) Input(project string) (projecthandover.Input, []memory.Ref, error) {
	return u.store.projectHandoverInputTx(u.ctx, u.tx, u.scope, project)
}

func (u projectHandoverUnit) CurrentEvidence(input projecthandover.Input, handover workspace.ProjectHandover) (projecthandover.Input, error) {
	return currentProjectEvidenceTx(u.ctx, u.tx, u.scope, input, handover)
}

func (u projectHandoverUnit) Event(outcome, reason string, count int) error {
	return stageEventTx(u.ctx, u.tx, u.job.OwnerID, ProjectHandoverStage, outcome, reason, count)
}

func (u projectHandoverUnit) SaveHandover(project string, handover workspace.ProjectHandover, input projecthandover.Input) error {
	_, err := u.tx.Exec(u.ctx, `INSERT INTO project_handovers(owner_id,project_id,body,written_at,input_events,input_hash) VALUES($1,$2,$3,clock_timestamp(),$4,$5) ON CONFLICT(owner_id,project_id) DO UPDATE SET body=excluded.body,written_at=excluded.written_at,input_events=excluded.input_events,input_hash=excluded.input_hash`, string(u.job.OwnerID), project, asJSON(handover), input.Events, input.Hash)
	return err
}

func (u projectHandoverUnit) DiscardResult() error { return discardPaidResultTx(u.ctx, u.tx, u.job) }
func (u projectHandoverUnit) Acknowledge() error   { return acknowledge(u.ctx, u.tx, u.job) }

func currentProjectEvidenceTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, input projectHandoverInput, out workspace.ProjectHandover) (projectHandoverInput, error) {
	current := projectHandoverInput{Project: input.Project, Parts: []projectInputPart{}}
	checked := map[workspace.ProjectEvidence]bool{}
	sections := [][]workspace.ProjectSentence{out.Conclusion, out.Blockers, out.NextSteps}
	for _, section := range sections {
		for _, sentence := range section {
			for _, ref := range sentence.Evidence {
				if checked[ref] {
					continue
				}
				checked[ref] = true
				var query string
				args := []any{string(scope.OwnerID), ref.ID}
				switch ref.Kind {
				case "item":
					query = `SELECT 1 FROM work_items WHERE owner_id=$1 AND id=$2 AND (id=$3 OR project_id=$3) FOR SHARE`
					args = append(args, input.Project.ID)
				case "documentVersion":
					query = `SELECT 1 FROM work_documents d JOIN work_items w ON(w.owner_id,w.id)=(d.owner_id,d.thing_id) WHERE d.owner_id=$1 AND d.id=$2 AND (w.id=$3 OR w.project_id=$3) AND greatest(coalesce((d.document->>'version')::int,1),1)=$4 FOR SHARE OF d,w`
					args = append(args, input.Project.ID, ref.Version)
				case "run":
					query = `SELECT 1 FROM agent_runs r JOIN work_items w ON(w.owner_id,w.id)=(r.owner_id,r.thing_id) WHERE r.owner_id=$1 AND r.id=$2 AND r.status='done' AND (w.id=$3 OR w.project_id=$3) FOR SHARE OF r,w`
					args = append(args, input.Project.ID)
				case "memory":
					query = `SELECT 1 FROM memory_records r JOIN claims cl ON(cl.owner_id,cl.id)=(r.owner_id,r.id) WHERE r.owner_id=$1 AND r.id=$2 AND r.version=$3 AND r.state='active' AND cl.retired='' AND claim_source_is_current(r.owner_id,r.id,r.version,now()) FOR SHARE OF r,cl`
					args = append(args, ref.Version)
				default:
					continue
				}
				var exists int
				err := tx.QueryRow(ctx, query, args...).Scan(&exists)
				if errors.Is(err, pgx.ErrNoRows) {
					continue
				}
				if err != nil {
					return current, err
				}
				current.Parts = append(current.Parts, projectInputPart{Evidence: ref})
			}
		}
	}
	return current, nil
}

func (s *Store) adoptProjectMemoryTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, run *workspace.Run, item workspace.Item, text, actionID string, auto bool) error {
	source, err := s.ingestTx(ctx, tx, scope, memory.IngestRequest{Connector: "capture", ExternalID: "project-adoption:" + run.ID + ":" + actionID, ExternalVersion: "1", Title: "采纳项目进展", Text: text, MediaType: "text/plain"})
	if err != nil {
		return err
	}
	actor, acquisition, confirmation := "user", "direct", "confirmed"
	if auto {
		actor, acquisition, confirmation = "ai", "inferred", "adopted"
	}
	_, err = s.rememberTx(ctx, tx, scope, statement{Text: text, Nature: "fact", ProjectID: item.ID, Actor: actor, Acquisition: acquisition, Confirmation: confirmation, Source: source.Ref})
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO project_adoption_sources(owner_id,run_id,action_id,source_id) VALUES($1,$2,$3,$4)`, string(scope.OwnerID), run.ID, actionID, string(source.ID))
	return err
}
