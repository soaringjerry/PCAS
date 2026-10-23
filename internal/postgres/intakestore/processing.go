package intakestore

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/soaringjerry/PCAS/internal/interpretation/extraction"
	"github.com/soaringjerry/PCAS/internal/postgres/claimstore"
	"github.com/soaringjerry/PCAS/internal/postgres/kernel"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/retrieval"
	"github.com/soaringjerry/PCAS/internal/worker"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func (s *Intake) ProcessIndex(ctx context.Context, j worker.Job) error {
	return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		if err := kernel.LockJob(ctx, tx, j); err != nil {
			return err
		}
		var text string
		err := tx.QueryRow(ctx, "SELECT body FROM memory_text WHERE owner_id=$1 AND id=$2 AND version=$3", string(j.OwnerID), string(j.Record.ID), j.Record.Version).Scan(&text)
		if errors.Is(err, pgx.ErrNoRows) {
			return kernel.Acknowledge(ctx, tx, j)
		}
		if err != nil {
			return err
		}
		segmented := strings.Join(memory.SearchTokens(text), " ")
		if _, err := tx.Exec(ctx, "INSERT INTO record_search(owner_id,record_id,record_version,search_text) VALUES($1,$2,$3,$4) ON CONFLICT(owner_id,record_id,record_version) DO UPDATE SET search_text=excluded.search_text", string(j.OwnerID), string(j.Record.ID), j.Record.Version, segmented); err != nil {
			return err
		}
		if j.Record.Kind == memory.SourceKind {
			rows, err := tx.Query(ctx, "SELECT id::text,body FROM chunks WHERE owner_id=$1 AND source_id=$2 AND source_version=$3", string(j.OwnerID), string(j.Record.ID), j.Record.Version)
			if err != nil {
				return err
			}
			texts := map[string]string{}
			for rows.Next() {
				var id, body string
				if err := rows.Scan(&id, &body); err != nil {
					rows.Close()
					return err
				}
				texts[id] = body
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return err
			}
			for id, body := range texts {
				if _, err := tx.Exec(ctx, "UPDATE chunks SET search_text=$3 WHERE owner_id=$1 AND id=$2", string(j.OwnerID), id, strings.Join(memory.SearchTokens(body), " ")); err != nil {
					return err
				}
			}
		}
		if err := kernel.Enqueue(ctx, tx, j.OwnerID, j.Record.ID, j.Record.Version, "memory.summary"); err != nil {
			return err
		}
		if j.Stage == "memory.index" {
			if err := kernel.Enqueue(ctx, tx, j.OwnerID, j.Record.ID, j.Record.Version, "memory.embed"); err != nil {
				return err
			}
		}
		return kernel.Acknowledge(ctx, tx, j)
	})
}

// IndexStore gives the vector indexer the texts and the vectors of a job.
func (s *Intake) IndexStore() retrieval.IndexStore { return indexStore{s} }

type indexStore struct{ store *Intake }

func (a indexStore) Missing(ctx context.Context, j worker.Job, model string) ([]memory.Ref, []string, bool, error) {
	s := a.store
	finish := func() ([]memory.Ref, []string, bool, error) {
		return nil, nil, false, pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
			if err := kernel.LockJob(ctx, tx, j); err != nil {
				return err
			}
			return kernel.Acknowledge(ctx, tx, j)
		})
	}
	var text string
	err := s.Pool.QueryRow(ctx, `SELECT t.body FROM memory_text t JOIN memory_records r ON (r.owner_id,r.id,r.version)=(t.owner_id,t.id,t.version) WHERE t.owner_id=$1 AND t.id=$2 AND t.version=$3 AND r.state='active'`, string(j.OwnerID), string(j.Record.ID), j.Record.Version).Scan(&text)
	if errors.Is(err, pgx.ErrNoRows) {
		return finish()
	}
	if err != nil {
		return nil, nil, false, err
	}
	// Bound provider input. Sources are embedded as chunks; claims as a whole.
	refs := []memory.Ref{j.Record}
	texts := []string{text}
	if j.Record.Kind == memory.SourceKind {
		refs = []memory.Ref{}
		texts = []string{}
		rows, err := s.Pool.Query(ctx, "SELECT id::text,version,body FROM chunks WHERE owner_id=$1 AND source_id=$2 AND source_version=$3 ORDER BY ordinal", string(j.OwnerID), string(j.Record.ID), j.Record.Version)
		if err != nil {
			return nil, nil, false, err
		}
		for rows.Next() {
			ref := memory.Ref{Kind: memory.ChunkKind}
			var body string
			if err := rows.Scan(&ref.ID, &ref.Version, &body); err != nil {
				rows.Close()
				return nil, nil, false, err
			}
			refs = append(refs, ref)
			texts = append(texts, body)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, nil, false, err
		}
	}
	// Original ingestion and a model backfill can overlap. Skip vectors already
	// committed for this model, and embed only missing chunks of a partial job.
	ids := make([]string, len(refs))
	for i, ref := range refs {
		ids[i] = string(ref.ID)
	}
	rows, err := s.Pool.Query(ctx, "SELECT record_id::text,record_version FROM embeddings WHERE owner_id=$1 AND model=$2 AND record_id=ANY($3::uuid[])", string(j.OwnerID), model, ids)
	if err != nil {
		return nil, nil, false, err
	}
	type vectorRef struct {
		id      memory.ID
		version int
	}
	existing := map[vectorRef]bool{}
	for rows.Next() {
		var id memory.ID
		var version int
		if err := rows.Scan(&id, &version); err != nil {
			rows.Close()
			return nil, nil, false, err
		}
		existing[vectorRef{id, version}] = true
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, nil, false, err
	}
	missingRefs := refs[:0]
	missingTexts := texts[:0]
	for i, ref := range refs {
		if !existing[vectorRef{ref.ID, ref.Version}] {
			missingRefs = append(missingRefs, ref)
			missingTexts = append(missingTexts, texts[i])
		}
	}
	refs, texts = missingRefs, missingTexts
	if len(texts) == 0 {
		return finish()
	}
	return refs, texts, true, nil
}

func (a indexStore) Save(ctx context.Context, j worker.Job, refs []memory.Ref, vectors []memory.Embedding) error {
	s := a.store
	return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		if err := kernel.LockJob(ctx, tx, j); err != nil {
			return err
		}
		var current int
		err := tx.QueryRow(ctx, "SELECT version FROM memory_records WHERE owner_id=$1 AND id=$2 AND state='active' FOR SHARE", string(j.OwnerID), string(j.Record.ID)).Scan(&current)
		if errors.Is(err, pgx.ErrNoRows) || current != j.Record.Version {
			return kernel.Acknowledge(ctx, tx, j)
		}
		if err != nil {
			return err
		}
		for i, ref := range refs {
			v := vectors[i]
			if _, err := tx.Exec(ctx, "INSERT INTO embeddings(owner_id,record_id,record_version,model,dimensions,embedding) VALUES($1,$2,$3,$4,$5,$6::vector) ON CONFLICT(owner_id,record_id,record_version,model) DO UPDATE SET embedding=excluded.embedding,dimensions=excluded.dimensions", string(j.OwnerID), string(ref.ID), ref.Version, v.Model, len(v.Values), string(kernel.JSON(v.Values))); err != nil {
				return err
			}
		}
		return kernel.Acknowledge(ctx, tx, j)
	})
}

type (
	ExtractedItem = extraction.Item
	Extracted     = extraction.Output
)

func currentExtractionSource(ctx context.Context, tx pgx.Tx, j worker.Job) (bool, error) {
	var version int
	err := tx.QueryRow(ctx, "SELECT version FROM memory_records WHERE owner_id=$1 AND id=$2 AND state='active' FOR SHARE", string(j.OwnerID), string(j.Record.ID)).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return version == j.Record.Version, err
}

// ExtractionStore gives the extraction service its reads and writes.
func (s *Intake) ExtractionStore() extraction.Store { return extractionStore{s} }

type extractionStore struct{ store *Intake }

func workerScope(owner memory.ID) memory.Scope {
	return memory.Scope{OwnerID: owner, PrincipalID: "worker", IsOwner: true}
}

func (a extractionStore) Source(ctx context.Context, j worker.Job) (memory.SourceResult, bool, error) {
	source, err := a.store.GetSource(ctx, workerScope(j.OwnerID), j.Record.ID, j.Record.Version)
	if err != nil {
		return source, false, err
	}
	var imported bool
	err = a.store.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM archive_entries WHERE owner_id=$1 AND source_id=$2 AND source_version=$3)", string(j.OwnerID), string(j.Record.ID), j.Record.Version).Scan(&imported)
	return source, imported, err
}

func (a extractionStore) Current(ctx context.Context, j worker.Job) (bool, error) {
	var current bool
	err := pgx.BeginFunc(ctx, a.store.Pool, func(tx pgx.Tx) error {
		if err := kernel.LockJob(ctx, tx, j); err != nil {
			return err
		}
		var err error
		current, err = currentExtractionSource(ctx, tx, j)
		if err != nil || current {
			return err
		}
		return kernel.Acknowledge(ctx, tx, j)
	})
	return current, err
}

// extractionTx runs write under the owner lock and the job's lease. It
// acknowledges the job without the write when the source version is no
// longer current.
func (a extractionStore) extractionTx(ctx context.Context, j worker.Job, write func(pgx.Tx) error) error {
	return pgx.BeginFunc(ctx, a.store.Pool, func(tx pgx.Tx) error {
		if err := kernel.OwnerLock(ctx, tx, j.OwnerID); err != nil {
			return err
		}
		if err := kernel.LockJob(ctx, tx, j); err != nil {
			return err
		}
		current, err := currentExtractionSource(ctx, tx, j)
		if err != nil {
			return err
		}
		if !current {
			return kernel.Acknowledge(ctx, tx, j)
		}
		return write(tx)
	})
}

func (a extractionStore) FinishEmpty(ctx context.Context, j worker.Job) error {
	return a.extractionTx(ctx, j, func(tx pgx.Tx) error { return completeExtractionTx(ctx, tx, j, "empty") })
}

func (a extractionStore) Conversation(ctx context.Context, j worker.Job, conversation string) (extraction.ConversationWork, bool, error) {
	work, err := a.store.prepareConversation(ctx, j, conversation)
	if err != nil || work == nil {
		return nil, false, err
	}
	return work, true, nil
}

func (a extractionStore) SplitWindows(ctx context.Context, j worker.Job, starts []int, imported bool) error {
	return a.extractionTx(ctx, j, func(tx pgx.Tx) error {
		for _, start := range starts {
			if err := kernel.Enqueue(ctx, tx, j.OwnerID, j.Record.ID, j.Record.Version, "source.extract:"+strconv.Itoa(start)); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE memory_jobs SET priority=CASE WHEN $4 THEN 10 ELSE (SELECT priority FROM memory_jobs WHERE id=$5) END
				WHERE owner_id=$1 AND record_id=$2 AND record_version=$3 AND stage LIKE 'source.extract:%'`, string(j.OwnerID), string(j.Record.ID), j.Record.Version, imported, string(j.ID)); err != nil {
			return err
		}
		return completeExtractionTx(ctx, tx, j, "")
	})
}

func (a extractionStore) Configured() error {
	if a.store.Models == nil || a.store.Models.ExtractionID() == "" {
		return &worker.JobError{Code: "provider_not_configured"}
	}
	return nil
}

func (a extractionStore) Ready() error {
	p, ok := a.store.TextModel()
	if !ok {
		return &worker.JobError{Code: "provider_not_configured"}
	}
	if !a.store.ModelAvailable(p.ID) {
		return &worker.JobError{Code: "provider_unavailable", Retry: true}
	}
	return nil
}

func (a extractionStore) Unpaid(text string) bool {
	if a.store.Models == nil {
		return false
	}
	id := a.store.Models.ExtractionID()
	_, ok := a.store.Model(id)
	return ok && a.store.Models.Estimate(id, text) == 0
}

func (a extractionStore) Input(ctx context.Context, j worker.Job, source memory.SourceResult) (extraction.Input, error) {
	s, scope := a.store, workerScope(j.OwnerID)
	var in extraction.Input
	var err error
	if in.Conditions, err = s.PendingConditions(ctx, scope); err != nil {
		return in, err
	}
	if in.Adjacent, err = s.adjacentContext(ctx, scope, source); err != nil {
		return in, err
	}
	var settings workspace.Settings
	if err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		if err := s.EnsureOwner(ctx, tx, scope); err != nil {
			return err
		}
		var err error
		settings, err = kernel.QueryDocument[workspace.Settings](ctx, tx, "SELECT settings FROM workspace_owners WHERE owner_id=$1", string(j.OwnerID))
		return err
	}); err != nil {
		return in, err
	}
	in.Timezone = settings.Timezone
	if raw := source.Source.Scope["project_id"]; raw != nil {
		if json.Unmarshal(raw, &in.Project) != nil {
			return in, memory.ErrInvalid
		}
	}
	if source.Source.Connector == "desk" {
		err = s.Pool.QueryRow(ctx, `SELECT coalesce(CASE WHEN w.kind='project' THEN w.id::text ELSE w.project_id::text END,'') FROM desk_turns t JOIN work_items w ON(w.owner_id,w.id)=(t.owner_id,t.thing_id) WHERE t.owner_id=$1 AND t.request_id::text=$2`, string(scope.OwnerID), source.Source.ExternalID).Scan(&in.Project)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return in, err
		}
	}
	return in, nil
}

func (a extractionStore) DiscardResult(ctx context.Context, j worker.Job) error {
	return pgx.BeginFunc(ctx, a.store.Pool, func(tx pgx.Tx) error { return kernel.DiscardPaidResult(ctx, tx, j) })
}

func (a extractionStore) Failed(ctx context.Context, j worker.Job) error {
	return a.store.extractionFailed(ctx, j)
}

func (a extractionStore) Apply(ctx context.Context, j worker.Job, result extraction.Result) error {
	s := a.store
	return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT 1 FROM workspace_owners WHERE owner_id=$1 FOR UPDATE", string(j.OwnerID)); err != nil {
			return err
		}
		if err := kernel.LockJob(ctx, tx, j); err != nil {
			return err
		}
		return extraction.ApplySource(ctx, sourceUnit{captureUnit: captureUnit{tx: tx, scope: workerScope(j.OwnerID)}, store: s, job: j}, j.Record, result, time.Now())
	})
}

// sourceUnit is the storage of the answer for one source, under the owner
// lock and the job's lease. extraction.ApplySource decides.
type sourceUnit struct {
	captureUnit
	store *Intake
	job   worker.Job
}

func (u sourceUnit) owner() string { return string(u.job.OwnerID) }

func (u sourceUnit) Current(ctx context.Context) (bool, error) {
	return currentExtractionSource(ctx, u.tx, u.job)
}

func (u sourceUnit) Acknowledge(ctx context.Context) error {
	return kernel.Acknowledge(ctx, u.tx, u.job)
}

// DeskTurn is read under the owner lock that undo uses, after generation: an
// undo during the model call must not bring the extracted memories back.
func (u sourceUnit) DeskTurn(ctx context.Context, external string) (bool, bool, error) {
	undone, err := DeskTurnFullyUndoneTx(ctx, u.tx, u.job.OwnerID, external)
	if err != nil || !undone {
		return undone, false, err
	}
	remembered, err := DeskTurnRememberedTx(ctx, u.tx, u.job.OwnerID, external)
	return true, remembered, err
}

func (u sourceUnit) WakeIdeas(ctx context.Context) (bool, error) {
	settings, err := kernel.QueryDocument[workspace.Settings](ctx, u.tx, "SELECT settings FROM workspace_owners WHERE owner_id=$1", u.owner())
	return settings.WakeIdeas, err
}

func (u sourceUnit) Imported(ctx context.Context) (bool, error) {
	var imported bool
	err := u.tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM archive_entries WHERE owner_id=$1 AND source_id=$2 AND source_version=$3)", u.owner(), string(u.job.Record.ID), u.job.Record.Version).Scan(&imported)
	return imported, err
}

func (u sourceUnit) CandidateExists(ctx context.Context, kind, text string) (bool, error) {
	var duplicate bool
	err := u.tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM capture_candidates WHERE owner_id=$1 AND source_id=$2 AND source_version=$3 AND document->>'text'=$4 AND document->>'kind'=$5)", u.owner(), string(u.job.Record.ID), u.job.Record.Version, text, kind).Scan(&duplicate)
	return duplicate, err
}

func (u sourceUnit) Remember(ctx context.Context, decision extraction.Decision, source memory.SourceResult, project string, newAtQuote bool) (memory.ID, error) {
	item := decision.Item
	ref, err := u.store.RememberTx(ctx, u.tx, u.scope, claimstore.Statement{ProjectID: project, Text: item.Text, Nature: item.Nature, Subject: decision.Subject, SubjectType: decision.SubjectType, Predicate: item.Predicate, Confirmation: decision.Confirmation, Acquisition: item.Acquisition, Actor: "ai", Quote: item.Quote, Source: u.job.Record, Structured: true, ExpressedAt: extraction.ExpressedAt(source),
		EventFrom: decision.EventFrom, EventTo: decision.EventTo, EventPrecision: decision.EventPrecision, Mentions: decision.Mentions, AllowNewAtQuote: newAtQuote})
	return ref.ID, err
}

func (u sourceUnit) ApplySignals(ctx context.Context, source memory.Source, signals []extraction.Signal) error {
	return u.store.ApplySignalsTx(ctx, u.tx, u.scope, source, signals)
}

func (u sourceUnit) MergeCapture(ctx context.Context) error {
	_, err := u.tx.Exec(ctx, "UPDATE capture_candidates SET state='merged',document=jsonb_set(document,'{state}','\"merged\"') WHERE owner_id=$1 AND source_id=$2 AND state='pending' AND document->>'kind'='unknown' AND (document->>'confidence')::numeric=0", u.owner(), string(u.job.Record.ID))
	return err
}

func (u sourceUnit) DiscardPaidResult(ctx context.Context) error {
	return kernel.DiscardPaidResult(ctx, u.tx, u.job)
}

func (u sourceUnit) Complete(ctx context.Context) error {
	return completeExtractionTx(ctx, u.tx, u.job, "")
}

func completeExtractionTx(ctx context.Context, tx pgx.Tx, j worker.Job, state string) error {
	if err := kernel.Acknowledge(ctx, tx, j); err != nil {
		return err
	}
	return ExtractionStateTx(ctx, tx, j, state)
}

// Aggregate fenced segment completions under the owner lock. The parent job is
// done after scheduling windows; only all completed windows finish the source.
// E2 also calls this after a job permanently stops or exhausts its lease.
func ExtractionStateTx(ctx context.Context, tx pgx.Tx, j worker.Job, state string) error {
	if strings.HasPrefix(j.Stage, ConversationExtractionPrefix) {
		return conversationExtractionStateTx(ctx, tx, j, state)
	}
	if !strings.HasPrefix(j.Stage, "source.extract") {
		return nil
	}
	current, err := currentExtractionSource(ctx, tx, j)
	if err != nil || !current {
		return err
	}
	items := 0
	if state == "" {
		var pending, failed bool
		if err := tx.QueryRow(ctx, `SELECT coalesce(bool_or(state IN ('queued','leased')),false),coalesce(bool_or(state IN ('failed','blocked')),false)
			FROM memory_jobs WHERE owner_id=$1 AND record_id=$2 AND record_version=$3 AND (stage='source.extract' OR stage LIKE 'source.extract:%')`, string(j.OwnerID), string(j.Record.ID), j.Record.Version).Scan(&pending, &failed); err != nil {
			return err
		}
		if failed {
			state = "failed"
		} else if pending {
			return nil
		}
	}
	if state != "empty" {
		if err := tx.QueryRow(ctx, `SELECT count(DISTINCT e.target_id) FROM evidence e
			JOIN claims c ON (c.owner_id,c.id)=(e.owner_id,e.target_id)
			JOIN memory_records r ON (r.owner_id,r.id)=(c.owner_id,c.id)
			WHERE e.owner_id=$1 AND e.source_id=$2 AND e.source_version=$3 AND e.stance='supports' AND r.state='active'`, string(j.OwnerID), string(j.Record.ID), j.Record.Version).Scan(&items); err != nil {
			return err
		}
		if state == "" {
			state = "empty"
			if items > 0 {
				state = "done"
			}
		}
	}
	_, err = tx.Exec(ctx, `INSERT INTO source_extractions(owner_id,source_id,source_version,extractor,state,items)
		VALUES($1,$2,$3,2,$4,$5) ON CONFLICT(owner_id,source_id,source_version)
		DO UPDATE SET extractor=2,state=excluded.state,items=excluded.items,updated_at=now()`, string(j.OwnerID), string(j.Record.ID), j.Record.Version, state, items)
	return err
}

func (s *Intake) extractionFailed(ctx context.Context, j worker.Job) error {
	return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		if err := kernel.OwnerLock(ctx, tx, j.OwnerID); err != nil {
			return err
		}
		if err := kernel.LockJob(ctx, tx, j); err != nil {
			return err
		}
		return ExtractionStateTx(ctx, tx, j, "failed")
	})
}
