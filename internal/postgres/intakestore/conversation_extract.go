package intakestore

import (
	"context"
	"errors"
	"strconv"

	"github.com/soaringjerry/PCAS/internal/interpretation/extraction"
	"github.com/soaringjerry/PCAS/internal/postgres/claimstore"
	"github.com/soaringjerry/PCAS/internal/postgres/kernel"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/worker"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

const ConversationExtractionPrefix = extraction.ConversationPrefix
const ConversationExtractorVersion = extraction.ConversationVersion

type (
	conversationMessage = extraction.ConversationMessage
	conversationSegment = extraction.ConversationSegment
	conversationOutput  = extraction.ConversationOutput
)

func EnqueueConversationTx(ctx context.Context, tx pgx.Tx, owner memory.ID, anchor memory.Ref, run string, segment, next int) error {
	_, err := tx.Exec(ctx, `INSERT INTO memory_jobs(id,owner_id,record_id,record_version,stage,priority)
 VALUES($1,$2,$3,$4,$5,9) ON CONFLICT(owner_id,record_id,record_version,stage) DO NOTHING`,
		string(memory.NewID()), string(owner), string(anchor.ID), anchor.Version, extraction.ConversationStage(run, segment, next))
	return err
}

func earlierConversationMemoriesTx(ctx context.Context, tx pgx.Tx, owner memory.ID, run string) ([]extraction.EarlierMemory, error) {
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
	out := []extraction.EarlierMemory{}
	for rows.Next() {
		item := extraction.EarlierMemory{Claim: memory.Ref{Kind: memory.ClaimKind}}
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
	extraction.ConversationSegmentWork
	store *Intake
	job   worker.Job
}

// Apply writes the answer of the segment in one transaction.
func (w *conversationWork) Apply(ctx context.Context, output conversationOutput) error {
	return w.store.inConversation(ctx, w.job, func(u *conversationUnit) error {
		return extraction.ApplyConversation(ctx, u, w.ConversationSegmentWork, output)
	})
}

// prepareConversation reads the job's segment under the job's lease. It
// returns nil when the job finished without a segment to extract.
func (s *Intake) prepareConversation(ctx context.Context, j worker.Job, conversation string) (*conversationWork, error) {
	var work *extraction.ConversationSegmentWork
	err := s.inConversation(ctx, j, func(u *conversationUnit) (err error) {
		work, err = extraction.PrepareConversation(ctx, u, j, conversation)
		return err
	})
	if err != nil || work == nil {
		return nil, err
	}
	return &conversationWork{ConversationSegmentWork: *work, store: s, job: j}, nil
}

// inConversation runs fn in one transaction under the owner lock and the
// job's lease.
func (s *Intake) inConversation(ctx context.Context, j worker.Job, fn func(*conversationUnit) error) error {
	return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		if err := kernel.OwnerLock(ctx, tx, j.OwnerID); err != nil {
			return err
		}
		if err := kernel.LockJob(ctx, tx, j); err != nil {
			return err
		}
		return fn(&conversationUnit{store: s, tx: tx, job: j, scope: memory.Scope{OwnerID: j.OwnerID, PrincipalID: "worker", IsOwner: true}})
	})
}

// conversationUnit is the storage of one conversation job. The extraction
// domain decides; conversationUnit reads and writes.
type conversationUnit struct {
	store *Intake
	tx    pgx.Tx
	job   worker.Job
	scope memory.Scope
}

var _ extraction.ConversationUnit = (*conversationUnit)(nil)

func (u *conversationUnit) Sources(ctx context.Context, conversation string) ([]memory.SourceResult, error) {
	return ArchiveConversationSources(ctx, u.tx, u.job.OwnerID, conversation)
}

func (u *conversationUnit) Extracted(ctx context.Context, conversation string) (bool, error) {
	var complete bool
	err := u.tx.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM source_contexts c
 JOIN memory_records r ON(r.owner_id,r.id,r.version)=(c.owner_id,c.source_id,c.source_version)
 WHERE c.owner_id=$1 AND c.conversation_key=$2 AND r.state='active'
 AND NOT EXISTS(SELECT 1 FROM source_extractions x WHERE(x.owner_id,x.source_id,x.source_version)=(c.owner_id,c.source_id,c.source_version) AND x.extractor>=$3 AND x.state IN('done','empty')))`, string(u.job.OwnerID), conversation, ConversationExtractorVersion).Scan(&complete)
	return complete, err
}

func (u *conversationUnit) Enqueue(ctx context.Context, anchor memory.Ref, run string, segment, next int) error {
	return EnqueueConversationTx(ctx, u.tx, u.job.OwnerID, anchor, run, segment, next)
}

func (u *conversationUnit) ReplaceMemories(ctx context.Context, sources []memory.SourceResult) error {
	return u.store.replaceConversationMemoriesTx(ctx, u.tx, u.scope, sources)
}

func (u *conversationUnit) Withdraw(ctx context.Context, claim memory.Ref) error {
	return u.store.DeleteRecordsTx(context.WithValue(ctx, RetainUndoneAnswersKey{}, true), u.tx, u.scope, memory.DeleteRequest{Targets: []memory.Ref{claim}})
}

func (u *conversationUnit) WriteStates(ctx context.Context, targets []extraction.StateTarget, state string) error {
	return writeConversationStatesTx(ctx, u.tx, u.job.OwnerID, targets, state)
}

func (u *conversationUnit) RetireJobs(ctx context.Context, conversation, run string) error {
	return retireConversationJobsTx(ctx, u.tx, u.job, conversation, run)
}

func (u *conversationUnit) Acknowledge(ctx context.Context) error {
	return kernel.Acknowledge(ctx, u.tx, u.job)
}

func (u *conversationUnit) DiscardPaidResult(ctx context.Context) error {
	return kernel.DiscardPaidResult(ctx, u.tx, u.job)
}

func (u *conversationUnit) Earlier(ctx context.Context, run string) ([]extraction.EarlierMemory, error) {
	return earlierConversationMemoriesTx(ctx, u.tx, u.job.OwnerID, run)
}

func (u *conversationUnit) Timezone(ctx context.Context) (string, error) {
	settings, err := kernel.QueryDocument[workspace.Settings](ctx, u.tx, "SELECT settings FROM workspace_owners WHERE owner_id=$1", string(u.job.OwnerID))
	return settings.Timezone, err
}

func (u *conversationUnit) Imported(ctx context.Context, source memory.Ref) (bool, error) {
	var imported bool
	err := u.tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM archive_entries WHERE owner_id=$1 AND source_id=$2)", string(u.job.OwnerID), string(source.ID)).Scan(&imported)
	return imported, err
}

func (u *conversationUnit) Capture(ctx context.Context, source memory.SourceResult, item extraction.Item, imported bool) error {
	return u.store.captureActionTx(ctx, u.tx, u.scope, source, item, imported, "")
}

func (u *conversationUnit) Remember(ctx context.Context, decision extraction.Decision, source memory.SourceResult, run string, next int, newAtQuote bool) (bool, error) {
	item := decision.Item
	ref, err := u.store.RememberTx(ctx, u.tx, u.scope, claimstore.Statement{Text: item.Text, Nature: item.Nature, Subject: decision.Subject, SubjectType: decision.SubjectType, Predicate: item.Predicate, Confirmation: decision.Confirmation, Acquisition: item.Acquisition, Actor: "ai", Quote: item.Quote, Source: source.Source.Ref, Structured: true, ExpressedAt: extraction.ExpressedAt(source), ExtractionRun: run, ExtractionRef: next,
		EventFrom: decision.EventFrom, EventTo: decision.EventTo, EventPrecision: decision.EventPrecision, Mentions: decision.Mentions, AllowNewAtQuote: newAtQuote})
	if err != nil {
		return false, err
	}
	var assigned bool
	err = u.tx.QueryRow(ctx, `SELECT coalesce(scope->>'conversation_extraction'=$4 AND scope->>'conversation_ref'=$5,false) FROM claim_revisions WHERE owner_id=$1 AND claim_id=$2 AND version=$3`, string(u.job.OwnerID), string(ref.ID), ref.Version, run, strconv.Itoa(next)).Scan(&assigned)
	return assigned, err
}

func retireConversationJobsTx(ctx context.Context, tx pgx.Tx, job worker.Job, conversation, run string) error {
	_, err := tx.Exec(ctx, `UPDATE memory_jobs j SET state='done',lease_token=NULL,lease_until=NULL,error_code='',updated_at=now()
 WHERE j.owner_id=$1 AND j.id<>$4 AND j.state<>'done' AND (j.stage='source.extract' OR (j.stage LIKE 'source.extract:%' AND j.stage NOT LIKE $3))
 AND EXISTS(SELECT 1 FROM source_contexts c WHERE(c.owner_id,c.source_id,c.source_version)=(j.owner_id,j.record_id,j.record_version) AND c.conversation_key=$2)`, string(job.OwnerID), conversation, ConversationExtractionPrefix+run+":%", string(job.ID))
	return err
}

func writeConversationStatesTx(ctx context.Context, tx pgx.Tx, owner memory.ID, targets []extraction.StateTarget, state string) error {
	ids, versions, eligible := []string{}, []int{}, []bool{}
	for _, target := range targets {
		ids = append(ids, string(target.Source.ID))
		versions = append(versions, target.Source.Version)
		eligible = append(eligible, target.Counted)
	}
	_, err := tx.Exec(ctx, `INSERT INTO source_extractions(owner_id,source_id,source_version,extractor,state,items)
 SELECT $1,input.id,input.version,$6,CASE WHEN $4<>'' THEN $4 WHEN count(DISTINCT r.id)>0 THEN 'done' ELSE 'empty' END,count(DISTINCT r.id)
 FROM unnest($2::uuid[],$3::int[],$5::bool[]) input(id,version,eligible)
 LEFT JOIN evidence e ON input.eligible AND e.owner_id=$1 AND e.source_id=input.id AND e.source_version=input.version AND e.stance='supports'
 LEFT JOIN memory_records r ON(r.owner_id,r.id)=(e.owner_id,e.target_id) AND r.kind='claim' AND r.state='active'
 GROUP BY input.id,input.version
 ON CONFLICT(owner_id,source_id,source_version) DO UPDATE SET extractor=excluded.extractor,state=excluded.state,items=excluded.items,updated_at=now()`, string(owner), ids, versions, state, eligible, ConversationExtractorVersion)
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
	return extraction.FailedConversation(ctx, &conversationUnit{tx: tx, job: j, scope: memory.Scope{OwnerID: j.OwnerID, PrincipalID: "worker", IsOwner: true}}, j, conversation)
}

// Read normalized message originals, never an archive blob or another owner's
// conversation. Historical branches remain available for progress bookkeeping;
// they are not model inputs or evidence for a current conversation extraction.
func ArchiveConversationSources(ctx context.Context, tx pgx.Tx, owner memory.ID, conversation string) ([]memory.SourceResult, error) {
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
func (s *Intake) replaceConversationMemoriesTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, sources []memory.SourceResult) error {
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
	ctx = context.WithValue(ctx, RetainUndoneAnswersKey{}, true)
	for start := 0; start < len(refs); start += 100 {
		if err := s.DeleteRecordsTx(ctx, tx, scope, memory.DeleteRequest{Targets: refs[start:min(start+100, len(refs))]}); err != nil {
			return err
		}
	}
	return nil
}
