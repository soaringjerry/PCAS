package postgres

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/soaringjerry/PCAS/internal/interpretation/extraction"
	"github.com/soaringjerry/PCAS/internal/postgres/claimstore"
	"github.com/soaringjerry/PCAS/internal/postgres/kernel"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/worker"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

const conversationExtractionPrefix = extraction.ConversationPrefix
const conversationExtractorVersion = extraction.ConversationVersion

type (
	conversationMessage = extraction.ConversationMessage
	conversationSegment = extraction.ConversationSegment
	conversationOutput  = extraction.ConversationOutput
)

type earlierConversationMemory struct {
	Ref   int        `json:"ref"`
	Text  string     `json:"text"`
	Claim memory.Ref `json:"-"`
}

func enqueueConversationTx(ctx context.Context, tx pgx.Tx, owner memory.ID, anchor memory.Ref, run string, segment, next int) error {
	_, err := tx.Exec(ctx, `INSERT INTO memory_jobs(id,owner_id,record_id,record_version,stage,priority)
 VALUES($1,$2,$3,$4,$5,9) ON CONFLICT(owner_id,record_id,record_version,stage) DO NOTHING`,
		string(memory.NewID()), string(owner), string(anchor.ID), anchor.Version, extraction.ConversationStage(run, segment, next))
	return err
}

func earlierConversationMemoriesTx(ctx context.Context, tx pgx.Tx, owner memory.ID, run string) ([]earlierConversationMemory, error) {
	rows, err := tx.Query(ctx, `SELECT (c.scope->>'conversation_ref')::int,c.value #>> '{}',r.id::text,r.version
 FROM claim_revisions c JOIN memory_records r ON(r.owner_id,r.id,r.version)=(c.owner_id,c.claim_id,c.version)
 JOIN record_versions rv ON(rv.owner_id,rv.record_id,rv.version)=(r.owner_id,r.id,r.version)
 WHERE c.owner_id=$1 AND c.scope->>'conversation_extraction'=$2 AND r.state='active'
 AND rv.actor='ai' AND c.confirmation<>'confirmed'
 AND NOT EXISTS(SELECT 1 FROM record_versions edited WHERE edited.owner_id=r.owner_id AND edited.record_id=r.id AND edited.actor='user')
 ORDER BY (c.scope->>'conversation_ref')::int,r.id`, string(owner), run)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []earlierConversationMemory{}
	for rows.Next() {
		item := earlierConversationMemory{Claim: memory.Ref{Kind: memory.ClaimKind}}
		if err := rows.Scan(&item.Ref, &item.Text, &item.Claim.ID, &item.Claim.Version); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// conversationWork is one prepared segment of an imported conversation. The
// extraction service asks the model between its preparation and Apply.
type conversationWork struct {
	store            *Store
	job              worker.Job
	scope            memory.Scope
	conversation     string
	run              string
	sources, current []memory.SourceResult
	segments         []conversationSegment
	earlier          []earlierConversationMemory
	settings         workspace.Settings
	ordinal, next    int
}

func (w *conversationWork) segment() conversationSegment { return w.segments[w.ordinal] }

func (w *conversationWork) Prompt() string {
	segment := w.segment()
	input := map[string]any{"messages": segment.Messages, "context_messages": segment.Context, "timezone": w.settings.Timezone}
	if w.ordinal > 0 {
		input["earlier_memories"] = w.earlier
	}
	return string(kernel.JSON(input))
}

func (w *conversationWork) HasOwnerText() bool {
	for _, message := range w.segment().Messages {
		if message.Role == "user" && strings.TrimSpace(message.Text) != "" {
			return true
		}
	}
	return false
}

func (w *conversationWork) Refs() []memory.Ref {
	segment := w.segment()
	refs := []memory.Ref{}
	for _, message := range append(append([]conversationMessage{}, segment.Context...), segment.Messages...) {
		refs = append(refs, w.current[message.Index-1].Source.Ref)
	}
	for _, item := range w.earlier {
		refs = append(refs, item.Claim)
	}
	return refs
}

// prepareConversation reads the job's segment under the job's lease. It
// returns nil when the job finished without a segment to extract.
func (s *Store) prepareConversation(ctx context.Context, j worker.Job, conversation string) (*conversationWork, error) {
	scope := memory.Scope{OwnerID: j.OwnerID, PrincipalID: "worker", IsOwner: true}
	var sources, current []memory.SourceResult
	var segments []conversationSegment
	var earlier []earlierConversationMemory
	var settings workspace.Settings
	var run string
	var ordinal, next int
	prepared := false
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		if err := kernel.OwnerLock(ctx, tx, j.OwnerID); err != nil {
			return err
		}
		if err := kernel.LockJob(ctx, tx, j); err != nil {
			return err
		}
		var err error
		sources, err = archiveConversationSources(ctx, tx, j.OwnerID, conversation)
		if err != nil {
			return err
		}
		current = extraction.CurrentBranch(sources)
		segments = extraction.SplitConversation(current)
		run = extraction.ConversationRun(sources)
		if !strings.HasPrefix(j.Stage, conversationExtractionPrefix) {
			if len(segments) == 0 {
				if err := s.replaceConversationMemoriesTx(ctx, tx, scope, sources); err != nil {
					return err
				}
				if err := writeConversationStatesTx(ctx, tx, j.OwnerID, sources, ""); err != nil {
					return err
				}
				if err := retireConversationJobsTx(ctx, tx, j, conversation, run); err != nil {
					return err
				}
				return kernel.Acknowledge(ctx, tx, j)
			}
			var complete bool
			if err := tx.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM source_contexts c
 JOIN memory_records r ON(r.owner_id,r.id,r.version)=(c.owner_id,c.source_id,c.source_version)
 WHERE c.owner_id=$1 AND c.conversation_key=$2 AND r.state='active'
 AND NOT EXISTS(SELECT 1 FROM source_extractions x WHERE(x.owner_id,x.source_id,x.source_version)=(c.owner_id,c.source_id,c.source_version) AND x.extractor>=$3 AND x.state IN('done','empty')))`, string(j.OwnerID), conversation, conversationExtractorVersion).Scan(&complete); err != nil {
				return err
			}
			if !complete {
				// Only one segment is runnable at a time. Its successor is scheduled
				// in the same transaction as its results, acknowledgement and ref counter.
				if err := enqueueConversationTx(ctx, tx, j.OwnerID, current[0].Source.Ref, run, 0, 1); err != nil {
					return err
				}
			}
			// Segment continuation is durable now; sibling message entrances no
			// longer need to wait at the head of the queue until the final segment.
			if err := retireConversationJobsTx(ctx, tx, j, conversation, run); err != nil {
				return err
			}
			return kernel.Acknowledge(ctx, tx, j)
		}
		jobRun, segment, nextRef, err := extraction.ParseConversationStage(j.Stage)
		if err != nil {
			return err
		}
		if jobRun != run || segment >= len(segments) {
			// A changed conversation gets a new manifest, never stale model results.
			if len(current) > 0 {
				if err := enqueueConversationTx(ctx, tx, j.OwnerID, current[0].Source.Ref, run, 0, 1); err != nil {
					return err
				}
			}
			return kernel.Acknowledge(ctx, tx, j)
		}
		ordinal, next = segment, nextRef
		earlier, err = earlierConversationMemoriesTx(ctx, tx, j.OwnerID, run)
		if err != nil {
			return err
		}
		settings, err = kernel.QueryDocument[workspace.Settings](ctx, tx, "SELECT settings FROM workspace_owners WHERE owner_id=$1", string(j.OwnerID))
		prepared = err == nil
		return err
	})
	if err != nil || !prepared {
		return nil, err
	}
	return &conversationWork{store: s, job: j, scope: scope, conversation: conversation, run: run, sources: sources, current: current, segments: segments, earlier: earlier, settings: settings, ordinal: ordinal, next: next}, nil
}

// Apply writes the statements of the segment, queues the next segment, and
// finishes the job. The output is empty for a segment that needed no call.
func (w *conversationWork) Apply(ctx context.Context, output conversationOutput) error {
	s, j, scope, conversation, run := w.store, w.job, w.scope, w.conversation, w.run
	sources, current, segments, segment, ordinal, next := w.sources, w.current, w.segments, w.segment(), w.ordinal, w.next
	loc, err := time.LoadLocation(w.settings.Timezone)
	if err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		if err := kernel.OwnerLock(ctx, tx, j.OwnerID); err != nil {
			return err
		}
		if err := kernel.LockJob(ctx, tx, j); err != nil {
			return err
		}
		fresh, err := archiveConversationSources(ctx, tx, j.OwnerID, conversation)
		if err != nil {
			return err
		}
		if extraction.ConversationRun(fresh) != run {
			if live := extraction.CurrentBranch(fresh); len(live) > 0 {
				if err := enqueueConversationTx(ctx, tx, j.OwnerID, live[0].Source.Ref, extraction.ConversationRun(fresh), 0, 1); err != nil {
					return err
				}
			}
			return kernel.Acknowledge(ctx, tx, j)
		}
		if ordinal == 0 {
			if err := s.replaceConversationMemoriesTx(ctx, tx, scope, sources); err != nil {
				return err
			}
		} else {
			// Re-read so a user confirmation/edit during generation is protected.
			eligible, err := earlierConversationMemoriesTx(ctx, tx, j.OwnerID, run)
			if err != nil {
				return err
			}
			withdraw := map[int]bool{}
			for _, ref := range output.Withdraw {
				withdraw[ref] = true
			}
			for _, item := range eligible {
				if withdraw[item.Ref] {
					if err := s.deleteRecordsTx(context.WithValue(ctx, retainUndoneAnswersKey{}, true), tx, scope, memory.DeleteRequest{Targets: []memory.Ref{item.Claim}}); err != nil {
						return err
					}
				}
			}
		}
		grounding := ""
		allowed := map[int]conversationMessage{}
		for _, message := range segment.Messages {
			allowed[message.Index] = message
			grounding += "\n" + message.Text
		}
		for _, message := range segment.Context {
			grounding += "\n" + message.Text
		}
		seenOutputQuotes := map[string]bool{}
		for _, raw := range output.Items {
			decision, source := extraction.DecideConversation(raw, allowed, current, grounding, loc)
			item := decision.Item
			if decision.Action == extraction.Skip {
				continue
			}
			if decision.Action == extraction.Capture {
				var imported bool
				if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM archive_entries WHERE owner_id=$1 AND source_id=$2)", string(j.OwnerID), string(source.Source.Ref.ID)).Scan(&imported); err != nil {
					return err
				}
				if err := s.captureActionTx(ctx, tx, scope, source, item, imported, ""); err != nil {
					return err
				}
				continue
			}
			in := claimstore.Statement{Text: item.Text, Nature: item.Nature, Subject: decision.Subject, SubjectType: decision.SubjectType, Predicate: item.Predicate, Confirmation: decision.Confirmation, Acquisition: item.Acquisition, Actor: "ai", Quote: item.Quote, Source: source.Source.Ref, Structured: true, ExpressedAt: extraction.ExpressedAt(source), ExtractionRun: run, ExtractionRef: next,
				EventFrom: decision.EventFrom, EventTo: decision.EventTo, EventPrecision: decision.EventPrecision, Mentions: decision.Mentions}
			// The model can extract several independent memories from one
			// quotation. Later siblings must not overwrite the first item.
			quoteKey := string(in.Source.ID) + "/" + in.Quote
			in.AllowNewAtQuote = seenOutputQuotes[quoteKey]
			seenOutputQuotes[quoteKey] = true
			ref, err := s.RememberTx(ctx, tx, scope, in)
			if errors.Is(err, memory.ErrBlocked) {
				continue
			}
			if err != nil {
				return err
			}
			var assigned bool
			if err := tx.QueryRow(ctx, `SELECT coalesce(scope->>'conversation_extraction'=$4 AND scope->>'conversation_ref'=$5,false) FROM claim_revisions WHERE owner_id=$1 AND claim_id=$2 AND version=$3`, string(j.OwnerID), string(ref.ID), ref.Version, run, strconv.Itoa(next)).Scan(&assigned); err != nil {
				return err
			}
			if assigned {
				next++
			}
		}
		processed := []memory.SourceResult{}
		for _, message := range segment.Messages {
			if message.Part == message.Parts {
				processed = append(processed, current[message.Index-1])
			}
		}
		for _, source := range sources {
			if source.Context.Branch == "historical" {
				processed = append(processed, source)
			}
		}
		if err := writeConversationStatesTx(ctx, tx, j.OwnerID, processed, ""); err != nil {
			return err
		}
		if err := retireConversationJobsTx(ctx, tx, j, conversation, run); err != nil {
			return err
		}
		if ordinal+1 < len(segments) {
			anchor := current[segments[ordinal+1].Messages[0].Index-1].Source.Ref
			if err := enqueueConversationTx(ctx, tx, j.OwnerID, anchor, run, ordinal+1, next); err != nil {
				return err
			}
		} else {
			// Withdrawals may change earlier sources' item counts. Refresh the
			// completed conversation, and retire redundant per-message jobs.
			if err := writeConversationStatesTx(ctx, tx, j.OwnerID, sources, ""); err != nil {
				return err
			}
			if err := retireConversationJobsTx(ctx, tx, j, conversation, run); err != nil {
				return err
			}
		}
		if err := kernel.DiscardPaidResult(ctx, tx, j); err != nil {
			return err
		}
		return kernel.Acknowledge(ctx, tx, j)
	})
}

func retireConversationJobsTx(ctx context.Context, tx pgx.Tx, job worker.Job, conversation, run string) error {
	_, err := tx.Exec(ctx, `UPDATE memory_jobs j SET state='done',lease_token=NULL,lease_until=NULL,error_code='',updated_at=now()
 WHERE j.owner_id=$1 AND j.id<>$4 AND j.state<>'done' AND (j.stage='source.extract' OR (j.stage LIKE 'source.extract:%' AND j.stage NOT LIKE $3))
 AND EXISTS(SELECT 1 FROM source_contexts c WHERE(c.owner_id,c.source_id,c.source_version)=(j.owner_id,j.record_id,j.record_version) AND c.conversation_key=$2)`, string(job.OwnerID), conversation, conversationExtractionPrefix+run+":%", string(job.ID))
	return err
}

func writeConversationStatesTx(ctx context.Context, tx pgx.Tx, owner memory.ID, sources []memory.SourceResult, state string) error {
	ids, versions, eligible := []string{}, []int{}, []bool{}
	for _, source := range sources {
		ids = append(ids, string(source.Source.ID))
		versions = append(versions, source.Source.Version)
		eligible = append(eligible, source.Context.Role == "user" && source.Context.Branch != "historical")
	}
	_, err := tx.Exec(ctx, `INSERT INTO source_extractions(owner_id,source_id,source_version,extractor,state,items)
 SELECT $1,input.id,input.version,$6,CASE WHEN $4<>'' THEN $4 WHEN count(DISTINCT r.id)>0 THEN 'done' ELSE 'empty' END,count(DISTINCT r.id)
 FROM unnest($2::uuid[],$3::int[],$5::bool[]) input(id,version,eligible)
 LEFT JOIN evidence e ON input.eligible AND e.owner_id=$1 AND e.source_id=input.id AND e.source_version=input.version AND e.stance='supports'
 LEFT JOIN memory_records r ON(r.owner_id,r.id)=(e.owner_id,e.target_id) AND r.kind='claim' AND r.state='active'
 GROUP BY input.id,input.version
 ON CONFLICT(owner_id,source_id,source_version) DO UPDATE SET extractor=excluded.extractor,state=excluded.state,items=excluded.items,updated_at=now()`, string(owner), ids, versions, state, eligible, conversationExtractorVersion)
	return err
}

func conversationExtractionStateTx(ctx context.Context, tx pgx.Tx, j worker.Job, state string) error {
	if state != "failed" {
		return nil
	}
	var conversation string
	err := tx.QueryRow(ctx, "SELECT conversation_key FROM source_contexts WHERE owner_id=$1 AND source_id=$2 AND source_version=$3", string(j.OwnerID), string(j.Record.ID), j.Record.Version).Scan(&conversation)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	sources, err := archiveConversationSources(ctx, tx, j.OwnerID, conversation)
	if err != nil {
		return err
	}
	run, ordinal, _, err := extraction.ParseConversationStage(j.Stage)
	if err != nil || run != extraction.ConversationRun(sources) {
		return err
	}
	current := extraction.CurrentBranch(sources)
	segments := extraction.SplitConversation(current)
	if ordinal >= len(segments) {
		return nil
	}
	remaining := []memory.SourceResult{}
	for _, segment := range segments[ordinal:] {
		for _, message := range segment.Messages {
			remaining = append(remaining, current[message.Index-1])
		}
	}
	return writeConversationStatesTx(ctx, tx, j.OwnerID, remaining, state)
}

// Read normalized message originals, never an archive blob or another owner's
// conversation. Historical branches remain available for progress bookkeeping;
// they are not model inputs or evidence for a current conversation extraction.
func archiveConversationSources(ctx context.Context, tx pgx.Tx, owner memory.ID, conversation string) ([]memory.SourceResult, error) {
	rows, err := tx.Query(ctx, `SELECT r.id::text,r.version,s.connector,s.external_id,v.external_version,v.title,v.body,v.media_type,
 rv.expressed_at,rv.recorded_at,c.parent_key,c.role,c.branch
 FROM source_contexts c JOIN memory_records r ON(r.owner_id,r.id,r.version)=(c.owner_id,c.source_id,c.source_version)
 JOIN record_versions rv ON(rv.owner_id,rv.record_id,rv.version)=(r.owner_id,r.id,r.version)
 JOIN sources s ON(s.owner_id,s.id)=(r.owner_id,r.id)
 JOIN source_versions v ON(v.owner_id,v.source_id,v.version)=(r.owner_id,r.id,r.version)
 WHERE c.owner_id=$1 AND c.conversation_key=$2 AND r.state='active' AND rv.state='active'
 AND EXISTS(SELECT 1 FROM archive_entries ae WHERE(ae.owner_id,ae.source_id,ae.source_version)=(r.owner_id,r.id,r.version))
 ORDER BY coalesce(rv.expressed_at,rv.recorded_at),s.external_id,r.id
 FOR SHARE OF r,rv,v,c`, string(owner), conversation)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []memory.SourceResult{}
	for rows.Next() {
		source := memory.SourceResult{Context: &memory.SourceContext{Conversation: conversation}}
		source.Source.Kind = memory.SourceKind
		if err := rows.Scan(&source.Source.ID, &source.Source.Version, &source.Source.Connector, &source.Source.ExternalID, &source.Source.ExternalVersion, &source.Source.Title, &source.Source.Text, &source.Source.MediaType, &source.Source.ExpressedAt, &source.Source.RecordedAt, &source.Context.Parent, &source.Context.Role, &source.Context.Branch); err != nil {
			return nil, err
		}
		out = append(out, source)
	}
	return out, rows.Err()
}

// A replacement is not a user's explicit deletion: preserve dependent answers
// and avoid reimport blocks. Confirmed or user-edited memories are independent
// of automatic organization and must survive it.
func (s *Store) replaceConversationMemoriesTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, sources []memory.SourceResult) error {
	ids := make([]string, 0, len(sources))
	for _, source := range sources {
		ids = append(ids, string(source.Source.ID))
	}
	rows, err := tx.Query(ctx, `SELECT DISTINCT r.id::text,r.version FROM evidence e
 JOIN memory_records r ON(r.owner_id,r.id)=(e.owner_id,e.target_id)
 JOIN claim_revisions c ON(c.owner_id,c.claim_id,c.version)=(r.owner_id,r.id,r.version)
 JOIN record_versions rv ON(rv.owner_id,rv.record_id,rv.version)=(r.owner_id,r.id,r.version)
 WHERE e.owner_id=$1 AND e.source_id=ANY($2::uuid[]) AND r.state='active' AND rv.actor='ai' AND c.confirmation<>'confirmed'
 AND NOT EXISTS(SELECT 1 FROM record_versions edited WHERE edited.owner_id=r.owner_id AND edited.record_id=r.id AND edited.actor='user')
 AND NOT EXISTS(SELECT 1 FROM evidence outside WHERE outside.owner_id=r.owner_id AND outside.target_id=r.id AND NOT(outside.source_id=ANY($2::uuid[])))
 ORDER BY r.id::text`, string(scope.OwnerID), ids)
	if err != nil {
		return err
	}
	refs := []memory.Ref{}
	for rows.Next() {
		var ref memory.Ref
		ref.Kind = memory.ClaimKind
		if err := rows.Scan(&ref.ID, &ref.Version); err != nil {
			rows.Close()
			return err
		}
		refs = append(refs, ref)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	ctx = context.WithValue(ctx, retainUndoneAnswersKey{}, true)
	for start := 0; start < len(refs); start += 100 {
		if err := s.deleteRecordsTx(ctx, tx, scope, memory.DeleteRequest{Targets: refs[start:min(start+100, len(refs))]}); err != nil {
			return err
		}
	}
	return nil
}
