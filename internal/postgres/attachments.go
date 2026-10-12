package postgres

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/input/attachment"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/modelcall"
	"github.com/soaringjerry/PCAS/internal/postgres/kernel"
	"github.com/soaringjerry/PCAS/internal/worker"
)

func (s *Store) IngestAttachment(ctx context.Context, scope memory.Scope, in memory.IngestRequest, r io.Reader) (memory.IngestResult, error) {
	var result memory.IngestResult
	if err := kernel.RequireOwner(scope); err != nil {
		return result, err
	}
	if s.Blobs == nil {
		return result, memory.ErrUnavailable
	}
	if !kernel.OneOf(in.MediaType, "application/x-pcas-archive", "application/pdf", "image/png", "image/jpeg", "image/webp", "image/tiff", "audio/mpeg", "audio/wav", "audio/mp4", "audio/ogg") || kernel.RequireText(in.Title) != nil || len(in.Title) > 2000 || in.Connector == "" || in.ExternalID == "" || in.ExternalVersion == "" {
		return result, memory.ErrInvalid
	}
	key, err := s.Blobs.Put(ctx, scope, r)
	if err != nil {
		return result, err
	}
	in.Text = "[附件原件；正文尚未解析。校验标识：" + strings.Split(strings.Split(key, "/")[1], "-")[0] + "]"
	err = pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		var err error
		result, err = s.ingestAttachmentTx(ctx, tx, scope, in, key)
		return err
	})
	if err != nil { // An uncommitted upload is cleaned only when no committed source uses it.
		_, _ = s.Pool.Exec(context.WithoutCancel(ctx), "INSERT INTO blob_cleanup_jobs(owner_id,blob_key) VALUES($1,$2) ON CONFLICT DO NOTHING", string(scope.OwnerID), key)
	}
	return result, err
}

// ingestAttachmentTx lets archive creation and its batch share one transaction.
func (s *Store) ingestAttachmentTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, in memory.IngestRequest, key string) (memory.IngestResult, error) {
	result, err := s.IngestTx(ctx, tx, scope, in)
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
	err = kernel.Enqueue(ctx, tx, scope.OwnerID, result.ID, result.Version, "source.parse")
	return result, err
}
func (s *Store) OpenAttachment(ctx context.Context, scope memory.Scope, id memory.ID, version int) (io.ReadCloser, string, string, error) {
	if s.Blobs == nil {
		return nil, "", "", memory.ErrUnavailable
	}
	source, err := s.GetSource(ctx, scope, id, version)
	if err != nil {
		return nil, "", "", err
	}
	var key *string
	if err := s.Pool.QueryRow(ctx, "SELECT blob_key FROM source_versions WHERE owner_id=$1 AND source_id=$2 AND version=$3", string(scope.OwnerID), string(id), source.Source.Version).Scan(&key); err != nil {
		return nil, "", "", err
	}
	if key == nil {
		return nil, "", "", memory.ErrNotFound
	}
	file, err := s.Blobs.Open(ctx, scope, *key)
	if os.IsNotExist(err) {
		err = memory.ErrNotFound
	}
	return file, source.Source.Title, source.Source.MediaType, err
}

// ProcessAttachmentWith handles the job of one uploaded file. read parses an
// original file under the lock of that original.
func (s *Store) ProcessAttachmentWith(ctx context.Context, j worker.Job, read attachment.Read) error {
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
	if _, err := s.readAttachmentWith(ctx, scope, j.Record, &j, read); err != nil {
		var specific *worker.JobError
		if errors.As(err, &specific) {
			return err
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, worker.ErrLeaseLost) {
			return err
		}
		return &worker.JobError{Code: "file_parse_failed", Retry: true}
	}
	return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		if err := kernel.LockJob(ctx, tx, j); err != nil {
			return err
		}
		return kernel.Acknowledge(ctx, tx, j)
	})
}

type parsedAttachment = attachment.Parsed

// Serialize parsing per original across API and worker processes. Existing OCR
// derivatives are reused, never upgraded or reread by this change.
func (s *Store) readAttachmentWith(ctx context.Context, scope memory.Scope, ref memory.Ref, job *worker.Job, read attachment.Read) (parsedAttachment, error) {
	var parsed parsedAttachment
	select {
	case s.secretarySlots <- struct{}{}:
	case <-ctx.Done():
		return parsed, ctx.Err()
	}
	defer func() { <-s.secretarySlots }()
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
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
		parsed, err = s.parseAttachment(ctx, scope, ref, job, read)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "SELECT 1 FROM workspace_owners WHERE owner_id=$1 FOR UPDATE", string(scope.OwnerID)); err != nil {
			return err
		}
		if job != nil {
			if err := kernel.LockJob(ctx, tx, *job); err != nil {
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
		derived, err := s.IngestTx(ctx, tx, scope, memory.IngestRequest{Connector: "attachment-text", ExternalID: fmt.Sprintf("%s:%d", ref.ID, ref.Version), ExternalVersion: "parser-1", Title: parsed.Title + " · " + parsed.Representation, Text: parsed.Text, MediaType: "text/plain"})
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "UPDATE source_versions SET representation=$4,derived_from_id=$5,derived_from_version=$6 WHERE owner_id=$1 AND source_id=$2 AND version=$3", string(scope.OwnerID), string(derived.ID), derived.Version, parsed.Representation, string(ref.ID), ref.Version); err != nil {
			return err
		}
		if raw := original.Source.Scope["project_id"]; raw != nil {
			if _, err = tx.Exec(ctx, `UPDATE source_versions SET scope=$4 WHERE owner_id=$1 AND source_id=$2 AND version=$3`, string(scope.OwnerID), string(derived.ID), derived.Version, kernel.JSON(original.Source.Scope)); err != nil {
				return err
			}
		}
		_, err = tx.Exec(ctx, "UPDATE workspace_owners SET revision=revision+1 WHERE owner_id=$1", string(scope.OwnerID))
		return err
	})
	return parsed, err
}
func (s *Store) parseAttachment(ctx context.Context, scope memory.Scope, ref memory.Ref, job *worker.Job, read attachment.Read) (parsedAttachment, error) {
	file, title, media, err := s.OpenAttachment(ctx, scope, ref.ID, ref.Version)
	if err != nil {
		return parsedAttachment{}, err
	}
	defer file.Close()
	return read(ctx, attachmentReceipts{store: s, scope: scope, ref: ref, job: job}, scope, ref, job, file, title, media)
}

// attachmentReceipts keeps the paid readings of one original file with the
// file.
type attachmentReceipts struct {
	store *Store
	scope memory.Scope
	ref   memory.Ref
	job   *worker.Job
}

func (a attachmentReceipts) Saved(ctx context.Context, component, purpose string) (string, bool, error) {
	saved, err := a.store.attachmentReceipt(ctx, a.scope, a.ref, component)
	if err != nil || saved == nil {
		return "", false, err
	}
	if err := a.store.accountAttachmentReceipt(ctx, a.scope, a.ref, saved, purpose, a.job); err != nil {
		return "", false, err
	}
	return saved.Output, true, nil
}

func (a attachmentReceipts) Save(ctx context.Context, component string, paid *modelcall.PaidResult) error {
	return a.store.saveAttachmentReceipt(ctx, a.scope, a.ref, component, paid)
}

func (s *Store) CleanupBlobs(ctx context.Context) error {
	if s.Blobs == nil {
		return nil
	}
	rows, err := s.Pool.Query(ctx, "SELECT owner_id::text,blob_key FROM blob_cleanup_jobs ORDER BY created_at LIMIT 100")
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
		if err := s.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM source_versions WHERE owner_id=$1 AND blob_key=$2)", string(job.Owner), job.Key).Scan(&used); err != nil {
			return err
		}
		if !used {
			if err := s.Blobs.Delete(ctx, memory.Scope{OwnerID: job.Owner, PrincipalID: "worker", IsOwner: true}, job.Key); err != nil {
				return err
			}
		}
		if _, err := s.Pool.Exec(ctx, "DELETE FROM blob_cleanup_jobs WHERE owner_id=$1 AND blob_key=$2", string(job.Owner), job.Key); err != nil {
			return err
		}
	}
	return nil
}
