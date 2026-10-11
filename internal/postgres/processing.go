package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/soaringjerry/PCAS/internal/interpretation/extraction"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/modelcall"
	"github.com/soaringjerry/PCAS/internal/worker"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func lockJob(ctx context.Context, tx pgx.Tx, j worker.Job) error {
	var valid bool
	// The same statement names this job as the cause of the work that the
	// transaction queues. The queue records the root and the depth from it.
	err := tx.QueryRow(ctx, `SELECT set_config('pcas.cause_job',id::text,true) IS NOT NULL FROM memory_jobs WHERE id=$1 AND owner_id=$2 AND record_id=$3 AND record_version=$4 AND lease_token=$5 AND state='leased' AND lease_until>clock_timestamp() FOR UPDATE`, string(j.ID), string(j.OwnerID), string(j.Record.ID), j.Record.Version, string(j.LeaseToken)).Scan(&valid)
	if errors.Is(err, pgx.ErrNoRows) {
		return worker.ErrLeaseLost
	}
	return err
}
func acknowledge(ctx context.Context, tx pgx.Tx, j worker.Job) error {
	tag, err := tx.Exec(ctx, "UPDATE memory_jobs SET state='done',lease_until=NULL,lease_token=NULL,error_code='',updated_at=now() WHERE id=$1 AND lease_token=$2 AND state='leased' AND lease_until>clock_timestamp()", string(j.ID), string(j.LeaseToken))
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return worker.ErrLeaseLost
	}
	return nil
}
func (s *Store) ProcessIndex(ctx context.Context, j worker.Job) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := lockJob(ctx, tx, j); err != nil {
			return err
		}
		var text string
		err := tx.QueryRow(ctx, "SELECT body FROM memory_text WHERE owner_id=$1 AND id=$2 AND version=$3", string(j.OwnerID), string(j.Record.ID), j.Record.Version).Scan(&text)
		if errors.Is(err, pgx.ErrNoRows) {
			return acknowledge(ctx, tx, j)
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
		if err := enqueue(ctx, tx, j.OwnerID, j.Record.ID, j.Record.Version, "memory.summary"); err != nil {
			return err
		}
		if j.Stage == "memory.index" {
			if err := enqueue(ctx, tx, j.OwnerID, j.Record.ID, j.Record.Version, "memory.embed"); err != nil {
				return err
			}
		}
		return acknowledge(ctx, tx, j)
	})
}
func (s *Store) ProcessEmbedding(ctx context.Context, j worker.Job) (err error) {
	if s.models == nil || s.models.EmbeddingID() == "" {
		return errors.Join(memory.ErrUnavailable, &worker.JobError{Code: "provider_not_configured"})
	}
	provider, ok := s.models.Get(s.models.EmbeddingID())
	if !ok {
		return errors.Join(memory.ErrUnavailable, &worker.JobError{Code: "provider_not_configured"})
	}
	if !s.models.Available(provider.ID) {
		return errors.Join(memory.ErrUnavailable, &worker.JobError{Code: "provider_unavailable", Retry: true})
	}
	var text string
	err = s.pool.QueryRow(ctx, `SELECT t.body FROM memory_text t JOIN memory_records r ON (r.owner_id,r.id,r.version)=(t.owner_id,t.id,t.version) WHERE t.owner_id=$1 AND t.id=$2 AND t.version=$3 AND r.state='active'`, string(j.OwnerID), string(j.Record.ID), j.Record.Version).Scan(&text)
	if errors.Is(err, pgx.ErrNoRows) {
		return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
			if err := lockJob(ctx, tx, j); err != nil {
				return err
			}
			return acknowledge(ctx, tx, j)
		})
	}
	if err != nil {
		return err
	}
	// Bound provider input. Sources are embedded as chunks; claims as a whole.
	refs := []memory.Ref{j.Record}
	texts := []string{text}
	if j.Record.Kind == memory.SourceKind {
		refs = []memory.Ref{}
		texts = []string{}
		rows, err := s.pool.Query(ctx, "SELECT id::text,version,body FROM chunks WHERE owner_id=$1 AND source_id=$2 AND source_version=$3 ORDER BY ordinal", string(j.OwnerID), string(j.Record.ID), j.Record.Version)
		if err != nil {
			return err
		}
		for rows.Next() {
			ref := memory.Ref{Kind: memory.ChunkKind}
			var body string
			if err := rows.Scan(&ref.ID, &ref.Version, &body); err != nil {
				rows.Close()
				return err
			}
			refs = append(refs, ref)
			texts = append(texts, body)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
	}
	// Original ingestion and a model backfill can overlap. Skip vectors already
	// committed for this model, and embed only missing chunks of a partial job.
	ids := make([]string, len(refs))
	for i, ref := range refs {
		ids[i] = string(ref.ID)
	}
	rows, err := s.pool.Query(ctx, "SELECT record_id::text,record_version FROM embeddings WHERE owner_id=$1 AND model=$2 AND record_id=ANY($3::uuid[])", string(j.OwnerID), provider.ID+":"+provider.Model, ids)
	if err != nil {
		return err
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
			return err
		}
		existing[vectorRef{id, version}] = true
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
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
		return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
			if err := lockJob(ctx, tx, j); err != nil {
				return err
			}
			return acknowledge(ctx, tx, j)
		})
	}
	var cost float64
	for _, text := range texts {
		cost += float64(len(text)+16) * provider.InputPerMillion / 1e6
	}
	// The call is its own execution. Its root and cause name the paying job.
	vectors, err := s.calls.CallEmbedding(ctx, modelcall.DirectRequest{OwnerID: j.OwnerID, ExecutionID: memory.NewID(), RootExecutionID: j.ID, CausationID: j.ID, Function: "embedding", Stage: j.Stage, Provider: provider, Refs: refs, Estimate: cost, Job: &j}, texts)
	var failure *modelcall.Failure
	var persistence *modelcall.PersistenceError
	switch {
	case err == nil:
	case errors.As(err, &persistence):
		return err
	case errors.Is(err, modelcall.ErrNotAvailable), errors.Is(err, ai.ErrUnsupportedCapability), errors.As(err, &failure) && failure.Code == modelcall.ErrNotAvailable.Error():
		return errors.Join(memory.ErrUnavailable, &worker.JobError{Code: "provider_unavailable", Retry: true})
	case errors.As(err, &failure):
		// A paid call is not submitted again. A free call can use the queue's retry.
		return &worker.JobError{Code: "model_call_failed", Retry: cost == 0}
	default:
		return err
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := lockJob(ctx, tx, j); err != nil {
			return err
		}
		var current int
		err := tx.QueryRow(ctx, "SELECT version FROM memory_records WHERE owner_id=$1 AND id=$2 AND state='active' FOR SHARE", string(j.OwnerID), string(j.Record.ID)).Scan(&current)
		if errors.Is(err, pgx.ErrNoRows) || current != j.Record.Version {
			return acknowledge(ctx, tx, j)
		}
		if err != nil {
			return err
		}
		for i, ref := range refs {
			v := vectors[i]
			if _, err := tx.Exec(ctx, "INSERT INTO embeddings(owner_id,record_id,record_version,model,dimensions,embedding) VALUES($1,$2,$3,$4,$5,$6::vector) ON CONFLICT(owner_id,record_id,record_version,model) DO UPDATE SET embedding=excluded.embedding,dimensions=excluded.dimensions", string(j.OwnerID), string(ref.ID), ref.Version, v.Model, len(v.Values), string(asJSON(v.Values))); err != nil {
				return err
			}
		}
		return acknowledge(ctx, tx, j)
	})
}

type (
	extractedItem = extraction.Item
	extracted     = extraction.Output
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
func (s *Store) ExtractionStore() extraction.Store { return extractionStore{s} }

type extractionStore struct{ store *Store }

func workerScope(owner memory.ID) memory.Scope {
	return memory.Scope{OwnerID: owner, PrincipalID: "worker", IsOwner: true}
}

func (a extractionStore) Source(ctx context.Context, j worker.Job) (memory.SourceResult, bool, error) {
	source, err := a.store.GetSource(ctx, workerScope(j.OwnerID), j.Record.ID, j.Record.Version)
	if err != nil {
		return source, false, err
	}
	var imported bool
	err = a.store.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM archive_entries WHERE owner_id=$1 AND source_id=$2 AND source_version=$3)", string(j.OwnerID), string(j.Record.ID), j.Record.Version).Scan(&imported)
	return source, imported, err
}

func (a extractionStore) Current(ctx context.Context, j worker.Job) (bool, error) {
	var current bool
	err := pgx.BeginFunc(ctx, a.store.pool, func(tx pgx.Tx) error {
		if err := lockJob(ctx, tx, j); err != nil {
			return err
		}
		var err error
		current, err = currentExtractionSource(ctx, tx, j)
		if err != nil || current {
			return err
		}
		return acknowledge(ctx, tx, j)
	})
	return current, err
}

// extractionTx runs write under the owner lock and the job's lease. It
// acknowledges the job without the write when the source version is no
// longer current.
func (a extractionStore) extractionTx(ctx context.Context, j worker.Job, write func(pgx.Tx) error) error {
	return pgx.BeginFunc(ctx, a.store.pool, func(tx pgx.Tx) error {
		if err := extractionOwnerLock(ctx, tx, j.OwnerID); err != nil {
			return err
		}
		if err := lockJob(ctx, tx, j); err != nil {
			return err
		}
		current, err := currentExtractionSource(ctx, tx, j)
		if err != nil {
			return err
		}
		if !current {
			return acknowledge(ctx, tx, j)
		}
		return write(tx)
	})
}

func (a extractionStore) FinishEmpty(ctx context.Context, j worker.Job) error {
	return a.extractionTx(ctx, j, func(tx pgx.Tx) error { return completeExtractionTx(ctx, tx, j, "empty") })
}

func (a extractionStore) Conversation(ctx context.Context, j worker.Job, conversation string) error {
	return a.store.processConversationExtraction(ctx, j, conversation)
}

func (a extractionStore) SplitWindows(ctx context.Context, j worker.Job, starts []int, imported bool) error {
	return a.extractionTx(ctx, j, func(tx pgx.Tx) error {
		for _, start := range starts {
			if err := enqueue(ctx, tx, j.OwnerID, j.Record.ID, j.Record.Version, "source.extract:"+strconv.Itoa(start)); err != nil {
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
	if a.store.models == nil || a.store.models.ExtractionID() == "" {
		return &worker.JobError{Code: "provider_not_configured"}
	}
	return nil
}

func (a extractionStore) Ready() error {
	p, ok := a.store.models.Get(a.store.models.ExtractionID())
	if !ok || p.Embedding || p.Transcription {
		return &worker.JobError{Code: "provider_not_configured"}
	}
	if !a.store.models.Available(p.ID) {
		return &worker.JobError{Code: "provider_unavailable", Retry: true}
	}
	return nil
}

func (a extractionStore) Unpaid(text string) bool {
	p, ok := a.store.models.Get(a.store.models.ExtractionID())
	return ok && p.Reserve(text) == 0
}

func (a extractionStore) Input(ctx context.Context, j worker.Job, source memory.SourceResult) (extraction.Input, error) {
	s, scope := a.store, workerScope(j.OwnerID)
	var in extraction.Input
	var err error
	if in.Conditions, err = s.pendingConditions(ctx, scope); err != nil {
		return in, err
	}
	if in.Adjacent, err = s.adjacentContext(ctx, scope, source); err != nil {
		return in, err
	}
	var settings workspace.Settings
	if err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.ensureOwner(ctx, tx, scope); err != nil {
			return err
		}
		var err error
		settings, err = queryDocument[workspace.Settings](ctx, tx, "SELECT settings FROM workspace_owners WHERE owner_id=$1", string(j.OwnerID))
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
		err = s.pool.QueryRow(ctx, `SELECT coalesce(CASE WHEN w.kind='project' THEN w.id::text ELSE w.project_id::text END,'') FROM desk_turns t JOIN work_items w ON(w.owner_id,w.id)=(t.owner_id,t.thing_id) WHERE t.owner_id=$1 AND t.request_id::text=$2`, string(scope.OwnerID), source.Source.ExternalID).Scan(&in.Project)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return in, err
		}
	}
	return in, nil
}

func (a extractionStore) DiscardResult(ctx context.Context, j worker.Job) error {
	return pgx.BeginFunc(ctx, a.store.pool, func(tx pgx.Tx) error { return discardPaidResultTx(ctx, tx, j) })
}

func (a extractionStore) Failed(ctx context.Context, j worker.Job) error {
	return a.store.extractionFailed(ctx, j)
}

func (a extractionStore) Apply(ctx context.Context, j worker.Job, result extraction.Result) error {
	s, scope, source := a.store, workerScope(j.OwnerID), result.Source
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT 1 FROM workspace_owners WHERE owner_id=$1 FOR UPDATE", string(j.OwnerID)); err != nil {
			return err
		}
		if err := lockJob(ctx, tx, j); err != nil {
			return err
		}
		current, err := currentExtractionSource(ctx, tx, j)
		if err != nil {
			return err
		}
		if !current {
			return acknowledge(ctx, tx, j)
		}
		facts := extraction.Facts{Source: source, MentionText: result.MentionText, Location: result.Location}
		// Recheck under the same owner lock used by undo, after generation: an
		// undo during the model call must not resurrect the extracted memories.
		if source.Source.Connector == "desk" {
			facts.Undone, err = deskTurnFullyUndoneTx(ctx, tx, j.OwnerID, source.Source.ExternalID)
			if err != nil {
				return err
			}
			if facts.Undone {
				facts.Remembered, err = deskTurnRememberedTx(ctx, tx, j.OwnerID, source.Source.ExternalID)
				if err != nil {
					return err
				}
			}
		}
		settings, err := queryDocument[workspace.Settings](ctx, tx, "SELECT settings FROM workspace_owners WHERE owner_id=$1", string(j.OwnerID))
		if err != nil {
			return err
		}
		// An uploaded archive is a history migration. What it says becomes memory
		// and reviewable candidates, never a present-day task or a woken idea:
		// "明天" in a 2025 conversation is not tomorrow.
		if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM archive_entries WHERE owner_id=$1 AND source_id=$2 AND source_version=$3)", string(j.OwnerID), string(j.Record.ID), j.Record.Version).Scan(&facts.Imported); err != nil {
			return err
		}
		accepted := 0
		seenOutputQuotes := map[string]bool{}
		for _, raw := range result.Output.Items {
			decision := extraction.Decide(raw, facts)
			item := decision.Item
			if decision.Action == extraction.Skip {
				continue
			}
			if decision.Action == extraction.Capture {
				if err := s.captureActionTx(ctx, tx, scope, source, item, facts.Imported, result.Project); err != nil {
					return err
				}
				accepted++
				continue
			}
			var duplicate bool
			if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM capture_candidates WHERE owner_id=$1 AND source_id=$2 AND source_version=$3 AND document->>'text'=$4 AND document->>'kind'=$5)", string(j.OwnerID), string(j.Record.ID), j.Record.Version, item.Text, item.Kind).Scan(&duplicate); err != nil {
				return err
			}
			if duplicate && item.Kind != "memory" {
				continue
			}
			v := workspace.Candidate{ID: string(memory.NewID()), Kind: item.Kind, Text: item.Text, MemoryKind: item.Nature, Confidence: item.Confidence, Source: workspace.SourceRef{SourceID: string(j.Record.ID), Version: j.Record.Version, Label: source.Source.Title, Excerpt: item.Quote, At: stamp()}, State: "pending", CreatedAt: stamp()}
			if decision.Action == extraction.Remember {
				in := statement{ProjectID: result.Project, Text: item.Text, Nature: item.Nature, Subject: decision.Subject, SubjectType: decision.SubjectType, Predicate: item.Predicate, Confirmation: decision.Confirmation, Acquisition: item.Acquisition, Actor: "ai", Quote: item.Quote, Source: j.Record, Structured: true, ExpressedAt: extraction.ExpressedAt(source),
					EventFrom: decision.EventFrom, EventTo: decision.EventTo, EventPrecision: decision.EventPrecision, Mentions: decision.Mentions}
				// The model can extract several independent memories from one
				// quotation. Later siblings must not overwrite the first item.
				quoteKey := string(in.Source.ID) + "/" + in.Quote
				in.AllowNewAtQuote = seenOutputQuotes[quoteKey]
				seenOutputQuotes[quoteKey] = true
				ref, err := s.rememberTx(ctx, tx, scope, in)
				if errors.Is(err, memory.ErrBlocked) {
					continue
				}
				if err != nil {
					return err
				}
				v.ResolvedInto = string(ref.ID)
				v.State = "accepted"
			}
			if duplicate {
				accepted++
				continue
			}
			if err := saveCandidate(ctx, tx, scope, v); err != nil {
				return err
			}
			accepted++
		}
		if settings.WakeIdeas {
			if err := s.applySignalsTx(ctx, tx, scope, source.Source, extraction.UsableSignals(result.Output, source, facts.Imported)); err != nil {
				return err
			}
		}
		// The original unclassified capture remains until extraction is successful.
		if accepted > 0 {
			if _, err := tx.Exec(ctx, "UPDATE capture_candidates SET state='merged',document=jsonb_set(document,'{state}','\"merged\"') WHERE owner_id=$1 AND source_id=$2 AND state='pending' AND document->>'kind'='unknown' AND (document->>'confidence')::numeric=0", string(j.OwnerID), string(j.Record.ID)); err != nil {
				return err
			}
		}
		if err := discardPaidResultTx(ctx, tx, j); err != nil {
			return err
		}
		return completeExtractionTx(ctx, tx, j, "")
	})
}

// ErrUnavailable means no request was sent. Release its reservation under the
// job fence so a metered retry cannot be mistaken for an ambiguous paid call.
func (s *Store) releaseUnavailableReservation(ctx context.Context, j worker.Job, reservationID string) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := lockJob(ctx, tx, j); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, "DELETE FROM background_usage WHERE owner_id=$1 AND job_id=$2 AND id=$3", string(j.OwnerID), string(j.ID), reservationID)
		return err
	})
}

func extractionOwnerLock(ctx context.Context, tx pgx.Tx, owner memory.ID) error {
	_, err := tx.Exec(ctx, "SELECT 1 FROM workspace_owners WHERE owner_id=$1 FOR UPDATE", string(owner))
	return err
}

func completeExtractionTx(ctx context.Context, tx pgx.Tx, j worker.Job, state string) error {
	if err := acknowledge(ctx, tx, j); err != nil {
		return err
	}
	return extractionStateTx(ctx, tx, j, state)
}

// Aggregate fenced segment completions under the owner lock. The parent job is
// done after scheduling windows; only all completed windows finish the source.
// E2 also calls this after a job permanently stops or exhausts its lease.
func extractionStateTx(ctx context.Context, tx pgx.Tx, j worker.Job, state string) error {
	if strings.HasPrefix(j.Stage, conversationExtractionPrefix) {
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

func (s *Store) extractionFailed(ctx context.Context, j worker.Job) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := extractionOwnerLock(ctx, tx, j.OwnerID); err != nil {
			return err
		}
		if err := lockJob(ctx, tx, j); err != nil {
			return err
		}
		return extractionStateTx(ctx, tx, j, "failed")
	})
}
