package postgres

import (
	"context"
	"errors"
	"fmt"
	"github.com/soaringjerry/PCAS/internal/workspace/secretary"
	"io"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/input/attachment"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/modelcall"
	"github.com/soaringjerry/PCAS/internal/prompts"
	"github.com/soaringjerry/PCAS/internal/worker"
)

func (s *Store) IngestAttachment(ctx context.Context, scope memory.Scope, in memory.IngestRequest, r io.Reader) (memory.IngestResult, error) {
	var result memory.IngestResult
	if err := requireOwner(scope); err != nil {
		return result, err
	}
	if s.blobs == nil {
		return result, memory.ErrUnavailable
	}
	if !oneOf(in.MediaType, "application/x-pcas-archive", "application/pdf", "image/png", "image/jpeg", "image/webp", "image/tiff", "audio/mpeg", "audio/wav", "audio/mp4", "audio/ogg") || requireText(in.Title) != nil || len(in.Title) > 2000 || in.Connector == "" || in.ExternalID == "" || in.ExternalVersion == "" {
		return result, memory.ErrInvalid
	}
	key, err := s.blobs.Put(ctx, scope, r)
	if err != nil {
		return result, err
	}
	in.Text = "[附件原件；正文尚未解析。校验标识：" + strings.Split(strings.Split(key, "/")[1], "-")[0] + "]"
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		result, err = s.ingestAttachmentTx(ctx, tx, scope, in, key)
		return err
	})
	if err != nil { // An uncommitted upload is cleaned only when no committed source uses it.
		_, _ = s.pool.Exec(context.WithoutCancel(ctx), "INSERT INTO blob_cleanup_jobs(owner_id,blob_key) VALUES($1,$2) ON CONFLICT DO NOTHING", string(scope.OwnerID), key)
	}
	return result, err
}

// ingestAttachmentTx lets archive creation and its batch share one transaction.
func (s *Store) ingestAttachmentTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, in memory.IngestRequest, key string) (memory.IngestResult, error) {
	result, err := s.ingestTx(ctx, tx, scope, in)
	if err != nil {
		return result, err
	}
	if result.Duplicate {
		_, err = tx.Exec(ctx, "INSERT INTO blob_cleanup_jobs(owner_id,blob_key) VALUES($1,$2) ON CONFLICT DO NOTHING", string(scope.OwnerID), key)
		return result, err
	}
	if _, err = tx.Exec(ctx, "UPDATE source_versions SET blob_key=$4,body='' WHERE owner_id=$1 AND source_id=$2 AND version=$3", string(scope.OwnerID), string(result.ID), result.Version, key); err != nil {
		return result, err
	}
	if _, err = tx.Exec(ctx, "DELETE FROM memory_jobs WHERE owner_id=$1 AND record_id=$2 AND record_version=$3 AND stage='source.chunk' AND state='queued'", string(scope.OwnerID), string(result.ID), result.Version); err != nil {
		return result, err
	}
	err = enqueue(ctx, tx, scope.OwnerID, result.ID, result.Version, "source.parse")
	return result, err
}
func (s *Store) OpenAttachment(ctx context.Context, scope memory.Scope, id memory.ID, version int) (io.ReadCloser, string, string, error) {
	if s.blobs == nil {
		return nil, "", "", memory.ErrUnavailable
	}
	source, err := s.GetSource(ctx, scope, id, version)
	if err != nil {
		return nil, "", "", err
	}
	var key *string
	if err := s.pool.QueryRow(ctx, "SELECT blob_key FROM source_versions WHERE owner_id=$1 AND source_id=$2 AND version=$3", string(scope.OwnerID), string(id), source.Source.Version).Scan(&key); err != nil {
		return nil, "", "", err
	}
	if key == nil {
		return nil, "", "", memory.ErrNotFound
	}
	file, err := s.blobs.Open(ctx, scope, *key)
	if os.IsNotExist(err) {
		err = memory.ErrNotFound
	}
	return file, source.Source.Title, source.Source.MediaType, err
}
func (s *Store) ProcessAttachment(ctx context.Context, j worker.Job) error {
	scope := memory.Scope{OwnerID: j.OwnerID, PrincipalID: "worker", IsOwner: true}
	file, title, media, err := s.OpenAttachment(ctx, scope, j.Record.ID, j.Record.Version)
	if err != nil {
		return s.recordImportFailure(ctx, j, err)
	}
	if media == "application/x-pcas-archive" {
		defer file.Close()
		return s.processArchiveImport(ctx, j, title, file)
	}
	file.Close()
	if _, err := s.readAttachment(ctx, scope, j.Record, &j); err != nil {
		var specific *worker.JobError
		if errors.As(err, &specific) {
			return err
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, worker.ErrLeaseLost) {
			return err
		}
		return &worker.JobError{Code: "file_parse_failed", Retry: true}
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := lockJob(ctx, tx, j); err != nil {
			return err
		}
		return acknowledge(ctx, tx, j)
	})
}

type parsedAttachment = attachment.Parsed

// Serialize parsing per original across API and worker processes. Existing OCR
// derivatives are reused, never upgraded or reread by this change.
func (s *Store) readAttachment(ctx context.Context, scope memory.Scope, ref memory.Ref, job *worker.Job) (parsedAttachment, error) {
	var parsed parsedAttachment
	select {
	case s.secretarySlots <- struct{}{}:
	case <-ctx.Done():
		return parsed, ctx.Err()
	}
	defer func() { <-s.secretarySlots }()
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", "attachment:"+string(scope.OwnerID)+":"+string(ref.ID)+fmt.Sprint(ref.Version)); err != nil {
			return err
		}
		original, err := s.GetSource(ctx, scope, ref.ID, ref.Version)
		if err != nil {
			return err
		}
		if !original.Source.HasAttachment || original.Source.State != "active" {
			return memory.ErrNotFound
		}
		parsed.Title, parsed.Media = original.Source.Title, original.Source.MediaType
		err = tx.QueryRow(ctx, `SELECT v.body,v.representation FROM source_versions v JOIN memory_records r ON(r.owner_id,r.id,r.version)=(v.owner_id,v.source_id,v.version) WHERE v.owner_id=$1 AND v.derived_from_id=$2 AND v.derived_from_version=$3 AND r.state='active' ORDER BY v.version DESC LIMIT 1`, string(scope.OwnerID), string(ref.ID), ref.Version).Scan(&parsed.Text, &parsed.Representation)
		if err == nil {
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		parsed, err = s.parseAttachment(ctx, scope, ref, job)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "SELECT 1 FROM workspace_owners WHERE owner_id=$1 FOR UPDATE", string(scope.OwnerID)); err != nil {
			return err
		}
		if job != nil {
			if err := lockJob(ctx, tx, *job); err != nil {
				return err
			}
		}
		// Deletion during the external call must not resurrect derived material.
		var active bool
		if err := tx.QueryRow(ctx, "SELECT state='active' AND version=$3 FROM memory_records WHERE owner_id=$1 AND id=$2 FOR SHARE", string(scope.OwnerID), string(ref.ID), ref.Version).Scan(&active); err != nil {
			return err
		}
		if !active {
			return memory.ErrNotFound
		}
		derived, err := s.ingestTx(ctx, tx, scope, memory.IngestRequest{Connector: "attachment-text", ExternalID: fmt.Sprintf("%s:%d", ref.ID, ref.Version), ExternalVersion: "parser-1", Title: parsed.Title + " · " + parsed.Representation, Text: parsed.Text, MediaType: "text/plain"})
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "UPDATE source_versions SET representation=$4,derived_from_id=$5,derived_from_version=$6 WHERE owner_id=$1 AND source_id=$2 AND version=$3", string(scope.OwnerID), string(derived.ID), derived.Version, parsed.Representation, string(ref.ID), ref.Version); err != nil {
			return err
		}
		if raw := original.Source.Scope["project_id"]; raw != nil {
			if _, err = tx.Exec(ctx, `UPDATE source_versions SET scope=$4 WHERE owner_id=$1 AND source_id=$2 AND version=$3`, string(scope.OwnerID), string(derived.ID), derived.Version, asJSON(original.Source.Scope)); err != nil {
				return err
			}
		}
		_, err = tx.Exec(ctx, "UPDATE workspace_owners SET revision=revision+1 WHERE owner_id=$1", string(scope.OwnerID))
		return err
	})
	return parsed, err
}
func (s *Store) parseAttachment(ctx context.Context, scope memory.Scope, ref memory.Ref, job *worker.Job) (parsedAttachment, error) {
	file, title, media, err := s.OpenAttachment(ctx, scope, ref.ID, ref.Version)
	if err != nil {
		return parsedAttachment{}, err
	}
	defer file.Close()
	return attachment.Parser{Calls: attachmentCalls{store: s, scope: scope, ref: ref, job: job}}.Parse(ctx, file, title, media)
}

// attachmentCalls makes the paid calls of one original file and keeps their
// receipts with the file.
type attachmentCalls struct {
	store *Store
	scope memory.Scope
	ref   memory.Ref
	job   *worker.Job
}

func (a attachmentCalls) SavedTranscript(ctx context.Context) (string, bool, error) {
	if a.store.models == nil {
		return "", false, memory.ErrUnavailable
	}
	saved, err := a.store.attachmentReceipt(ctx, a.scope, a.ref, "transcript")
	if err != nil {
		return "", false, err
	}
	if saved != nil {
		if err := a.store.accountAttachmentReceipt(ctx, a.scope, a.ref, saved, "transcription", a.job); err != nil {
			return "", false, err
		}
		return saved.Output, true, nil
	}
	if _, ok := a.store.models.Get(a.store.models.Config.Transcription); !ok {
		return "", false, memory.ErrUnavailable
	}
	return "", false, nil
}

func (a attachmentCalls) Transcribe(ctx context.Context, audio io.Reader, title string, seconds float64) (string, error) {
	provider, ok := a.store.models.Get(a.store.models.Config.Transcription)
	if !ok {
		return "", memory.ErrUnavailable
	}
	// The reservation holds one more second than the measured length.
	request := modelcall.DirectRequest{OwnerID: a.scope.OwnerID, ExecutionID: memory.NewID(), Function: "transcription", Stage: "audio", Provider: provider, Estimate: (seconds + 1) / 60 * provider.AudioPerMinute, Refs: []memory.Ref{a.ref}, Job: a.job}
	request.RootExecutionID = request.ExecutionID
	if a.job != nil {
		request.RootExecutionID, request.CausationID = a.job.ID, a.job.ID
	}
	text, usage, call, err := a.store.calls.CallTranscription(ctx, request, audio, title, seconds)
	if err == nil {
		paid := &paidModelResult{Output: text, Reservation: call.Reservation, Provider: provider.ID, Model: provider.Model, InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens, InputEstimated: true, OutputEstimated: true, Cost: usage.Cost, CostEstimated: usage.CostEstimated}
		return text, a.store.saveAttachmentReceipt(ctx, a.scope, a.ref, "transcript", paid)
	}
	// A submitted call is recorded with its usage. Other errors come from
	// the reservation or the accounting write and keep their own meaning.
	var failure *modelcall.Failure
	if errors.As(err, &failure) || errors.Is(err, modelcall.ErrNotAvailable) || errors.Is(err, ai.ErrUnsupportedCapability) {
		return "", memory.ErrUnavailable
	}
	return "", err
}

func (a attachmentCalls) SavedReading(ctx context.Context, component string) (string, bool, error) {
	saved, err := a.store.attachmentReceipt(ctx, a.scope, a.ref, component)
	if err != nil || saved == nil {
		return "", false, err
	}
	if err := a.store.accountAttachmentReceipt(ctx, a.scope, a.ref, saved, "vision", a.job); err != nil {
		return "", false, err
	}
	return saved.Output, true, nil
}

func (a attachmentCalls) CanReadImages() bool {
	_, ok := a.store.models.VisionProvider()
	return ok
}

func (a attachmentCalls) ReadImage(ctx context.Context, component string, image ai.Image) (string, error) {
	p, ok := a.store.models.VisionProvider()
	if !ok {
		return "", &attachment.Fallback{Stage: "select", Kind: "unavailable"}
	}
	// The image's job is named in the usage record. Its queue budget rules
	// do not apply, because one file can need a call for each page.
	instructions := prompts.Must("vision")
	request := modelcall.DirectRequest{OwnerID: a.scope.OwnerID, ExecutionID: memory.NewID(), Function: "vision", Stage: "image", Provider: p, Estimate: p.ReserveVision(instructions.Text(), image), Instructions: instructions, Refs: []memory.Ref{a.ref}}
	request.RootExecutionID = request.ExecutionID
	if a.job != nil {
		request.RootExecutionID, request.CausationID, request.UsageJob = a.job.ID, a.job.ID, a.job.ID
	}
	work, cancel := context.WithTimeout(ctx, 45*time.Second)
	result, call, err := a.store.calls.CallVision(work, request, image)
	cancel()
	if err == nil {
		paid := &paidModelResult{Output: result.Text, Reservation: call.Reservation, Provider: p.ID, Model: p.Model, InputTokens: result.InputTokens, OutputTokens: result.OutputTokens, InputEstimated: result.InputEstimated, OutputEstimated: result.OutputEstimated, CostEstimated: result.CostEstimated, Cost: result.Cost}
		if err = a.store.saveAttachmentReceipt(ctx, a.scope, a.ref, component, paid); err != nil {
			return "", err
		}
		return paid.Output, nil
	}
	var persistence *modelcall.PersistenceError
	if errors.As(err, &persistence) {
		return "", err
	}
	// The gateway recorded a submitted call and its usage. A reservation
	// that failed submitted nothing.
	stage := "budget"
	var failure *modelcall.Failure
	if errors.As(err, &failure) {
		stage = "model"
	}
	return "", &attachment.Fallback{Stage: stage, Kind: secretary.ErrorType(stage, err)}
}

func (s *Store) CleanupBlobs(ctx context.Context) error {
	if s.blobs == nil {
		return nil
	}
	rows, err := s.pool.Query(ctx, "SELECT owner_id::text,blob_key FROM blob_cleanup_jobs ORDER BY created_at LIMIT 100")
	if err != nil {
		return err
	}
	type pending struct {
		Owner memory.ID
		Key   string
	}
	jobs := []pending{}
	for rows.Next() {
		var job pending
		if err := rows.Scan(&job.Owner, &job.Key); err != nil {
			rows.Close()
			return err
		}
		jobs = append(jobs, job)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, job := range jobs {
		var used bool
		if err := s.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM source_versions WHERE owner_id=$1 AND blob_key=$2)", string(job.Owner), job.Key).Scan(&used); err != nil {
			return err
		}
		if !used {
			if err := s.blobs.Delete(ctx, memory.Scope{OwnerID: job.Owner, PrincipalID: "worker", IsOwner: true}, job.Key); err != nil {
				return err
			}
		}
		if _, err := s.pool.Exec(ctx, "DELETE FROM blob_cleanup_jobs WHERE owner_id=$1 AND blob_key=$2", string(job.Owner), job.Key); err != nil {
			return err
		}
	}
	return nil
}
