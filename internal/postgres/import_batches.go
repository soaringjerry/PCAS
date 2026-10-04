package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/connectors"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/worker"
)

var ImportChunkSize = 500

func init() {
	if raw := os.Getenv("PCAS_IMPORT_CHUNK_SIZE"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err == nil && n > 0 {
			ImportChunkSize = n
		} else {
			slog.Warn("invalid import chunk size", "stage", "import_startup", "error_type", "invalid_import_chunk_size")
		}
	}
}

func (s *Store) PreviewArchive(ctx context.Context, scope memory.Scope, name string, r io.Reader) (connectors.ArchivePreview, error) {
	if err := requireOwner(scope); err != nil {
		return connectors.ArchivePreview{}, err
	}
	archive, err := connectors.OpenArchive(ctx, name, r)
	if err != nil {
		return connectors.ArchivePreview{}, err
	}
	defer archive.Close()
	err = s.selectArchive(ctx, scope, "archive-records", archive)

	return archive.Preview, err
}

func (s *Store) ImportArchiveReader(ctx context.Context, scope memory.Scope, name string, r io.Reader) (connectors.Result, error) {
	return s.ImportArchiveReaderWithOrganizing(ctx, scope, name, r, "later")
}
func (s *Store) ImportArchiveReaderWithOrganizing(ctx context.Context, scope memory.Scope, name string, r io.Reader, organize string) (connectors.Result, error) {
	if err := requireOwner(scope); err != nil {
		return connectors.Result{}, err
	}
	if organize != "" && organize != "later" && organize != "now" {
		return connectors.Result{}, memory.ErrInvalid
	}
	return s.importArchiveReader(ctx, scope, name, r, "archive", organize != "now")
}
func (s *Store) importArchiveReader(ctx context.Context, scope memory.Scope, name string, r io.Reader, namespace string, holdOrganizing bool) (connectors.Result, error) {
	out := connectors.Result{Refs: []memory.Ref{}, Gaps: []string{}}
	if err := requireOwner(scope); err != nil {
		return out, err
	}
	if strings.TrimSpace(name) == "" || len(name) > 2000 {
		return out, memory.ErrInvalid
	}
	if s.blobs == nil {
		return out, memory.ErrUnavailable
	}
	archive, err := connectors.OpenArchive(ctx, name, r)
	if err != nil {
		return out, err
	}
	defer archive.Close()
	recordNamespace := "archive-records"
	if strings.HasPrefix(namespace, "connection-archive:") {
		recordNamespace = "connection:" + strings.TrimPrefix(namespace, "connection-archive:")
	}
	if err = s.selectArchive(ctx, scope, recordNamespace, archive); err != nil {
		return out, err
	}
	// Avoid even a redundant blob upload for an identical archive.
	var ref memory.Ref
	ref.Kind = memory.SourceKind
	err = s.pool.QueryRow(ctx, `SELECT s.id::text,v.version,b.id::text FROM sources s
  JOIN source_versions v ON v.owner_id=s.owner_id AND v.source_id=s.id AND v.external_version='1'
  JOIN import_batches b ON b.owner_id=s.owner_id AND b.archive_id=s.id
  JOIN memory_records r ON r.owner_id=s.owner_id AND r.id=s.id AND r.state='active'
  WHERE s.owner_id=$1 AND s.connector=$2 AND s.external_id=$3`, string(scope.OwnerID), namespace, archive.Hash).Scan(&ref.ID, &ref.Version, &out.BatchID)
	if err == nil {
		out.Refs = append(out.Refs, ref)
		out.Duplicates = 1
		out.Gaps = append(archive.Preview.Gaps, "原始归档已保留，记录正在后台解析；处理进度可在来源中查看")
		return out, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return out, err
	}
	key, err := s.putArchiveBlob(ctx, scope, archive.Original())
	if err != nil {
		return out, err
	}
	in := memory.IngestRequest{Connector: namespace, ExternalID: archive.Hash, ExternalVersion: "1", Title: name, MediaType: "application/x-pcas-archive", Text: "[附件原件；正文尚未解析。校验标识：" + archive.Hash + "]"}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.ensureOwner(ctx, tx, scope); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "SELECT 1 FROM workspace_owners WHERE owner_id=$1 FOR UPDATE", string(scope.OwnerID)); err != nil {
			return err
		}
		// Serialize the file identity before rechecking. Concurrent uploads
		// of the same bytes may have different names, but share one archive.
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", fmt.Sprintf("%s:%x", scope.OwnerID, sourceKey(namespace, archive.Hash))); err != nil {
			return err
		}
		var existing memory.Ref
		existing.Kind = memory.SourceKind
		err := tx.QueryRow(ctx, `SELECT s.id::text,r.version,b.id::text FROM sources s
         JOIN memory_records r ON r.owner_id=s.owner_id AND r.id=s.id AND r.state='active'
         JOIN import_batches b ON b.owner_id=s.owner_id AND b.archive_id=s.id
         WHERE s.owner_id=$1 AND s.connector=$2 AND s.external_id=$3`, string(scope.OwnerID), namespace, archive.Hash).Scan(&existing.ID, &existing.Version, &out.BatchID)
		if err == nil {
			out.Refs = append(out.Refs, existing)
			out.Duplicates = 1
			_, err = tx.Exec(ctx, "INSERT INTO blob_cleanup_jobs(owner_id,blob_key) VALUES($1,$2) ON CONFLICT DO NOTHING", string(scope.OwnerID), key)
			return err
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		result, err := s.ingestAttachmentTx(ctx, tx, scope, in, key)
		if err != nil {
			return err
		}
		out.Refs = append(out.Refs, result.Ref)
		if result.Duplicate {
			out.Duplicates = 1
		} else {
			out.Imported = 1
		}
		err = tx.QueryRow(ctx, "SELECT id::text FROM import_batches WHERE owner_id=$1 AND archive_id=$2", string(scope.OwnerID), string(result.ID)).Scan(&out.BatchID)
		if err == nil {
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		out.BatchID = memory.NewID()
		// Existing messages are already stored, but must belong to this archive
		// for progress, pause and deletion to use the same durable ledger.
		stored, err := linkImportedArchiveTx(ctx, tx, scope, recordNamespace, result.Ref, archive)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO import_batches(owner_id,id,archive_id,name,state,total,stored,left_out,earliest,latest,hold_organizing)
   VALUES($1,$2,$3,$4,'importing',$5,$9,$6,$7,$8,$10)`, string(scope.OwnerID), string(out.BatchID), string(result.ID), name, archive.Len(), archive.Preview.LeftOut, archive.Preview.Earliest, archive.Preview.Latest, stored, holdOrganizing)
		if err != nil {
			return err
		}
		gaps := archive.Preview.Gaps
		_, err = tx.Exec(ctx, `INSERT INTO source_contexts(owner_id,source_id,source_version,gaps) VALUES($1,$2,$3,$4)
   ON CONFLICT(owner_id,source_id,source_version) DO UPDATE SET gaps=excluded.gaps`, string(scope.OwnerID), string(result.ID), result.Version, asJSON(gaps))
		return err
	})
	if err != nil {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_, _ = s.pool.Exec(cleanupCtx, "INSERT INTO blob_cleanup_jobs(owner_id,blob_key) VALUES($1,$2) ON CONFLICT DO NOTHING", string(scope.OwnerID), key)
	}
	out.Gaps = append(archive.Preview.Gaps, "原始归档已保留，记录正在后台解析；处理进度可在来源中查看")
	return out, err
}

type ImportBatch = connectors.ImportBatch

// importBatchSelect resolves organization progress from the extraction ledger.
const importBatchSelect = `SELECT b.id::text,b.archive_id::text,r.version,b.name,b.state,b.total,b.stored,
 ledger.organized,b.hold_organizing,b.left_out,b.earliest,b.latest,b.error_code,b.created_at,b.updated_at,
 jobs.prepared,jobs.indexed,jobs.vectorized,coalesce(jobs.active_stage,''),ledger.failed,coalesce(jobs.budget_wait,false)
 FROM import_batches b JOIN memory_records r ON r.owner_id=b.owner_id AND r.id=b.archive_id
 CROSS JOIN LATERAL (
   SELECT count(*) FILTER (WHERE x.state IN ('done','empty')) AS organized,
     count(*) FILTER (WHERE x.state='failed') AS failed
   FROM archive_entries e JOIN source_extractions x ON
     (x.owner_id,x.source_id,x.source_version)=(e.owner_id,e.source_id,e.source_version)
   WHERE e.owner_id=b.owner_id AND e.archive_id=b.archive_id
 ) ledger
 CROSS JOIN LATERAL (
   SELECT count(*) FILTER (WHERE j.stage='source.chunk' AND j.state='done') AS prepared,
     count(*) FILTER (WHERE j.stage='source.tokenize' AND j.state='done') AS indexed,
     count(*) FILTER (WHERE j.stage='source.embed' AND j.state='done') AS vectorized,
     (array_agg(j.stage ORDER BY (j.stage LIKE 'source.extract:conversation:%') DESC,j.updated_at DESC,j.id) FILTER (WHERE j.state='leased'))[1] AS active_stage,
     bool_and(j.error_code='budget_deferred' AND j.available_at>now()) FILTER
       (WHERE j.stage LIKE 'source.extract:conversation:%' AND j.state='queued') AS budget_wait
   FROM archive_entries e JOIN memory_jobs j ON
     (j.owner_id,j.record_id,j.record_version)=(e.owner_id,e.source_id,e.source_version)
   WHERE e.owner_id=b.owner_id AND e.archive_id=b.archive_id
 ) jobs`

func scanImportBatch(row pgx.Row) (ImportBatch, error) {
	var b ImportBatch
	var stage string
	var budgetWait bool
	err := row.Scan(&b.ID, &b.ArchiveID, &b.ArchiveVersion, &b.Name, &b.State, &b.Total, &b.Stored, &b.Organized, &b.OrganizeLater, &b.LeftOut, &b.Earliest, &b.Latest, &b.ErrorCode, &b.CreatedAt, &b.UpdatedAt, &b.Prepared, &b.Indexed, &b.Vectorized, &stage, &b.OrganizingFailed, &budgetWait)
	if b.ErrorCode != "" {
		b.Error = importErrorMessage(b.ErrorCode)
	}
	switch {
	case b.State == "paused":
		b.Activity = "paused"
	case b.State == "failed":
		b.Activity = "failed"
	case b.State == "importing":
		b.Activity = "storing"
	case strings.HasPrefix(stage, conversationExtractionPrefix):
		b.Activity = "organizing"
	case stage == "source.chunk":
		b.Activity = "preparing"
	case stage == "source.embed":
		b.Activity = "vectorizing"
	case stage == "source.tokenize" || stage == "memory.summary":
		b.Activity = "indexing"
	case b.Prepared < b.Stored:
		b.Activity = "preparing"
	case b.OrganizeLater:
		b.Activity = "held"
	case b.OrganizingFailed > 0:
		b.Activity = "organizing_failed"
	case budgetWait && b.Organized < b.Total:
		b.Activity = "budget_wait"
	case b.Organized < b.Total:
		b.Activity = "queued"
	case b.Indexed < b.Total:
		b.Activity = "indexing"
	case b.Vectorized < b.Total:
		b.Activity = "vectorizing"
	default:
		b.Activity = "complete"
	}
	return b, err
}

func importErrorMessage(code string) string {
	switch code {
	case "import_storage_failed":
		return "保存中断了，已存好的记录仍在，请点继续重试。"
	case "import_source_missing":
		return "原始文件已不可用，请重新上传导出文件。"
	default:
		return (&connectors.ArchiveError{Code: code}).Message()
	}
}
func (s *Store) ListImports(ctx context.Context, scope memory.Scope) ([]ImportBatch, error) {
	out := []ImportBatch{}
	if err := requireOwner(scope); err != nil {
		return out, err
	}
	rows, err := s.pool.Query(ctx, importBatchSelect+" WHERE b.owner_id=$1 ORDER BY b.created_at DESC,b.id DESC", string(scope.OwnerID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		b, err := scanImportBatch(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

var ErrImportNotActive = &connectors.ArchiveError{Code: "import_not_active"}

// Starting organization changes only the hold. Paused imports stay paused, and
// repeated requests keep already queued or completed extraction work intact.
func (s *Store) OrganizeImport(ctx context.Context, scope memory.Scope, id memory.ID) (ImportBatch, error) {
	var out ImportBatch
	if err := requireOwner(scope); err != nil {
		return out, err
	}
	if !id.Valid() {
		return out, memory.ErrInvalid
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT 1 FROM workspace_owners WHERE owner_id=$1 FOR UPDATE", string(scope.OwnerID)); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, "UPDATE import_batches SET hold_organizing=false,updated_at=now() WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), string(id))
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return memory.ErrNotFound
		}
		out, err = scanImportBatch(tx.QueryRow(ctx, importBatchSelect+" WHERE b.owner_id=$1 AND b.id=$2", string(scope.OwnerID), string(id)))
		return err
	})
	return out, err
}

func (s *Store) PauseImport(ctx context.Context, scope memory.Scope, id memory.ID) (ImportBatch, error) {
	return s.changeImportState(ctx, scope, id, true)
}
func (s *Store) ResumeImport(ctx context.Context, scope memory.Scope, id memory.ID) (ImportBatch, error) {
	return s.changeImportState(ctx, scope, id, false)
}
func (s *Store) changeImportState(ctx context.Context, scope memory.Scope, id memory.ID, pause bool) (ImportBatch, error) {
	var out ImportBatch
	if err := requireOwner(scope); err != nil {
		return out, err
	}
	if !id.Valid() {
		return out, memory.ErrInvalid
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT 1 FROM workspace_owners WHERE owner_id=$1 FOR UPDATE", string(scope.OwnerID)); err != nil {
			return err
		}
		b, err := scanImportBatch(tx.QueryRow(ctx, importBatchSelect+" WHERE b.owner_id=$1 AND b.id=$2 FOR UPDATE OF b", string(scope.OwnerID), string(id)))
		if errors.Is(err, pgx.ErrNoRows) {
			return memory.ErrNotFound
		}
		if err != nil {
			return err
		}
		if pause && b.State != "importing" || !pause && b.State != "paused" && b.State != "failed" {
			return ErrImportNotActive
		}
		state := "importing"
		if pause {
			state = "paused"
		}
		if _, err = tx.Exec(ctx, "UPDATE import_batches SET state=$3,error_code='',updated_at=now() WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), string(id), state); err != nil {
			return err
		}
		if pause {
			_, err = tx.Exec(ctx, `UPDATE memory_jobs SET state='blocked',error_code='import_paused',lease_token=NULL,lease_until=NULL,updated_at=now()
    WHERE owner_id=$1 AND record_id=$2 AND stage='source.parse' AND state IN ('queued','leased')`, string(scope.OwnerID), string(b.ArchiveID))
		} else {
			_, err = tx.Exec(ctx, `UPDATE memory_jobs SET state='queued',attempts=0,error_code='',available_at=now(),lease_token=NULL,lease_until=NULL,updated_at=now()
    WHERE owner_id=$1 AND record_id=$2 AND stage='source.parse'`, string(scope.OwnerID), string(b.ArchiveID))
		}
		if err != nil {
			return err
		}
		out, err = scanImportBatch(tx.QueryRow(ctx, importBatchSelect+" WHERE b.owner_id=$1 AND b.id=$2", string(scope.OwnerID), string(id)))
		return err
	})
	return out, err
}

func (s *Store) processArchiveImport(ctx context.Context, j worker.Job, title string, r io.Reader) (err error) {
	defer func() { err = s.recordImportFailure(ctx, j, err) }()
	archive, err := connectors.OpenArchive(ctx, title, r)
	if err != nil {
		return err
	}
	defer archive.Close()
	scope := memory.Scope{OwnerID: j.OwnerID, PrincipalID: "worker", IsOwner: true}
	var connector string
	if err = s.pool.QueryRow(ctx, "SELECT connector FROM sources WHERE owner_id=$1 AND id=$2", string(j.OwnerID), string(j.Record.ID)).Scan(&connector); err != nil {
		return err
	}
	namespace := "archive-records"
	connectionID := strings.TrimPrefix(connector, "connection-archive:")
	if strings.HasPrefix(connector, "connection-archive:") && memory.ID(connectionID).Valid() {
		namespace = "connection:" + connectionID
	}
	if err = s.selectArchive(ctx, scope, namespace, archive); err != nil {
		return err
	}
	total := archive.Len()
	if err = s.pendingArchive(ctx, scope, namespace, j.Record, archive); err != nil {
		return err
	}
	completed := total - archive.Len()
	next := 0
	for {
		if err = ctx.Err(); err != nil {
			return err
		}
		stopped := false
		err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
			if err := s.ensureOwner(ctx, tx, scope); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, "SELECT 1 FROM workspace_owners WHERE owner_id=$1 FOR UPDATE", string(j.OwnerID)); err != nil {
				return err
			}
			var id, state string
			var stored, total int
			err := tx.QueryRow(ctx, "SELECT id::text,state,stored,total FROM import_batches WHERE owner_id=$1 AND archive_id=$2 FOR UPDATE", string(j.OwnerID), string(j.Record.ID)).Scan(&id, &state, &stored, &total)
			if errors.Is(err, pgx.ErrNoRows) {
				// Attachments created before batches existed get a ledger on first parse.
				id = string(memory.NewID())
				state = "importing"
				total = archive.Len()
				_, err = tx.Exec(ctx, `INSERT INTO import_batches(owner_id,id,archive_id,name,state,total,stored,left_out,earliest,latest)
     VALUES($1,$2,$3,$4,'importing',$5,0,$6,$7,$8)`, string(j.OwnerID), id, string(j.Record.ID), title, total, archive.Preview.LeftOut, archive.Preview.Earliest, archive.Preview.Latest)
				if err == nil {
					_, err = tx.Exec(ctx, `INSERT INTO source_contexts(owner_id,source_id,source_version,gaps) VALUES($1,$2,$3,$4)
                  ON CONFLICT(owner_id,source_id,source_version) DO UPDATE SET gaps=excluded.gaps`, string(j.OwnerID), string(j.Record.ID), j.Record.Version, asJSON(archive.Preview.Gaps))
				}
			}
			if err != nil {
				return err
			}
			if err = lockJob(ctx, tx, j); err != nil {
				return err
			}
			if state != "importing" {
				stopped = true
				if state == "done" {
					return acknowledge(ctx, tx, j)
				}
				_, err = tx.Exec(ctx, `UPDATE memory_jobs SET state='blocked',error_code='import_paused',lease_token=NULL,lease_until=NULL,updated_at=now() WHERE id=$1 AND lease_token=$2`, string(j.ID), string(j.LeaseToken))
				return err
			}
			// Stored includes pre-existing messages at arbitrary positions. The
			// archive membership ledger, rather than Stored as a file offset,
			// reconstructs the remaining sequence after a restart.
			stored = completed + next
			total = completed + archive.Len()
			records, err := archive.Records(next, max(1, ImportChunkSize))
			if err != nil {
				return err
			}
			result, err := s.importBatchTx(ctx, tx, scope, namespace, connectors.Batch{Records: records, Gaps: archive.Preview.Gaps}, &j.Record)
			if err != nil {
				return err
			}
			if result.Blocked > 0 {
				identities := make([]connectors.ArchiveIdentity, len(records))
				for i, r := range records {
					identities[i] = connectors.ArchiveIdentity{ID: r.ID, Version: r.Version}
				}
				allowed, _, err := classifyArchiveTx(ctx, tx, scope, namespace, identities)
				if err != nil {
					return err
				}
				if err = archive.Drop(next, allowed); err != nil {
					return err
				}
				total -= result.Blocked
			}
			if namespace != "archive-records" {
				if _, err = tx.Exec(ctx, "UPDATE connector_configs SET imported=imported+$3,gaps=$4 WHERE owner_id=$1 AND id=$2", string(j.OwnerID), connectionID, result.Imported, asJSON(result.Gaps)); err != nil {
					return err
				}
			}
			stored += len(records) - result.Blocked
			next += len(records) - result.Blocked
			state = "importing"
			if stored >= total {
				state = "done"
				stopped = true
			}
			if _, err = tx.Exec(ctx, "UPDATE import_batches SET stored=$3,state=$4,total=$5,error_code='',updated_at=now() WHERE owner_id=$1 AND id=$2", string(j.OwnerID), id, stored, state, total); err != nil {
				return err
			}
			if stopped {
				if archive.Preview.Blocked > 0 {
					if err = redactArchivesTx(ctx, tx, scope, []string{string(j.Record.ID)}); err != nil {
						return err
					}
				}
				return acknowledge(ctx, tx, j)
			}
			// Finish before the worker's four-minute deadline, returning the durable
			// parse job to the queue without using up the crash/retry attempt budget.
			if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < 30*time.Second {
				stopped = true
				_, err = tx.Exec(ctx, `UPDATE memory_jobs SET state='queued',attempts=0,available_at=now(),lease_token=NULL,lease_until=NULL,updated_at=now() WHERE id=$1 AND lease_token=$2`, string(j.ID), string(j.LeaseToken))
				return err
			}
			_, err = tx.Exec(ctx, "UPDATE memory_jobs SET lease_until=now()+interval '5 minutes' WHERE id=$1 AND lease_token=$2", string(j.ID), string(j.LeaseToken))
			return err
		})
		if err != nil || stopped {
			return err
		}
	}
}

// Import reader endpoints use this narrower API without changing the existing
// connector port implemented by older adapters and tests.
var _ interface {
	PreviewArchive(context.Context, memory.Scope, string, io.Reader) (connectors.ArchivePreview, error)
	ImportArchiveReader(context.Context, memory.Scope, string, io.Reader) (connectors.Result, error)
	ImportArchiveReaderWithOrganizing(context.Context, memory.Scope, string, io.Reader, string) (connectors.Result, error)
	OrganizeImport(context.Context, memory.Scope, memory.ID) (connectors.ImportBatch, error)
} = (*Store)(nil)

// The initial attachment open can fail before parsing begins, so it shares the
// same durable status update as failures between chunks.
func (s *Store) recordImportFailure(ctx context.Context, j worker.Job, err error) error {
	if err == nil || errors.Is(err, worker.ErrLeaseLost) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	code := "import_storage_failed"
	var failure *connectors.ArchiveError
	if errors.As(err, &failure) {
		code = failure.Code
	}
	if errors.Is(err, memory.ErrNotFound) {
		code = "import_source_missing"
	}
	slog.WarnContext(ctx, "archive import failed", "stage", "import_records", "error_type", code, "job_id", j.ID)
	failureCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	tag, updateErr := s.pool.Exec(failureCtx, `UPDATE import_batches b SET state='failed',error_code=$4,updated_at=now()
   WHERE b.owner_id=$1 AND b.archive_id=$2 AND b.state='importing'
   AND EXISTS(SELECT 1 FROM memory_jobs j WHERE j.owner_id=b.owner_id AND j.record_id=b.archive_id AND j.id=$3 AND j.lease_token=$5 AND j.state='leased')`, string(j.OwnerID), string(j.Record.ID), string(j.ID), code, string(j.LeaseToken))
	if updateErr != nil {
		slog.WarnContext(failureCtx, "cannot save import failure", "stage", "import_status", "error_type", "import_storage_failed", "job_id", j.ID)
	}
	if updateErr == nil && tag.RowsAffected() == 0 {
		return err
	}
	return &worker.JobError{Code: code, Retry: false}
}

// Implementations may offer the archive-specific limit without changing the
// ordinary attachment port or widening other attachment uploads.
func (s *Store) putArchiveBlob(ctx context.Context, scope memory.Scope, r io.Reader) (string, error) {
	if store, ok := s.blobs.(interface {
		PutArchive(context.Context, memory.Scope, io.Reader) (string, error)
	}); ok {
		return store.PutArchive(ctx, scope, r)
	}
	return s.blobs.Put(ctx, scope, r)
}

func (s *Store) selectArchive(ctx context.Context, scope memory.Scope, namespace string, archive *connectors.Archive) error {
	archive.Preview.AlreadyImported = 0
	return pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		return archive.Select(ctx, func(identities []connectors.ArchiveIdentity) ([]bool, error) {
			allowed, imported, err := classifyArchiveTx(ctx, tx, scope, namespace, identities)
			if err == nil {
				archive.Preview.AlreadyImported += imported
			}
			return allowed, err
		})
	})
}

// The identity policy mirrors ingestTx: owner/connector/external id controls
// deletion blocking, while the external version controls duplicate detection.
func classifyArchiveTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, namespace string, identities []connectors.ArchiveIdentity) ([]bool, int, error) {
	type key struct {
		Index   int    `json:"idx"`
		ID      string `json:"id"`
		Version string `json:"version"`
		Hash    string `json:"hash"`
	}
	keys := make([]key, len(identities))
	allowed := make([]bool, len(identities))
	for i, r := range identities {
		keys[i] = key{Index: i, ID: r.ID, Version: r.Version, Hash: hex.EncodeToString(sourceKey(namespace, r.ID))}
	}
	rows, err := tx.Query(ctx, `SELECT k.idx,
  EXISTS(SELECT 1 FROM reimport_blocks b WHERE b.owner_id=$1 AND b.source_key_hash=decode(k.hash,'hex'))
  OR EXISTS(SELECT 1 FROM sources s JOIN memory_records r ON r.owner_id=s.owner_id AND r.id=s.id
   WHERE s.owner_id=$1 AND s.connector=$2 AND s.external_id=k.id AND r.state!='active') AS blocked,
  EXISTS(SELECT 1 FROM sources s JOIN source_versions v ON v.owner_id=s.owner_id AND v.source_id=s.id
   JOIN memory_records r ON r.owner_id=s.owner_id AND r.id=s.id AND r.state='active'
   JOIN record_versions rv ON (rv.owner_id,rv.record_id,rv.version)=(v.owner_id,v.source_id,v.version) AND rv.state='active'
   WHERE s.owner_id=$1 AND s.connector=$2 AND s.external_id=k.id AND v.external_version=k.version) AS imported
  FROM jsonb_to_recordset($3::jsonb) AS k(idx integer,id text,version text,hash text) ORDER BY k.idx`, string(scope.OwnerID), namespace, asJSON(keys))
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	imported := 0
	for rows.Next() {
		var index int
		var blocked, duplicate bool
		if err = rows.Scan(&index, &blocked, &duplicate); err != nil {
			return nil, 0, err
		}
		allowed[index] = !blocked
		if !blocked && duplicate {
			imported++
		}
	}
	return allowed, imported, rows.Err()
}

// Pre-existing messages count as stored immediately. Link their source versions
// without rewriting their text or enqueuing work, preserving ingestTx's content
// and context conflict checks. The caller holds the owner lock against deletion.
func linkImportedArchiveTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, namespace string, ref memory.Ref, archive *connectors.Archive) (int, error) {
	type key struct {
		ID           string   `json:"id"`
		Version      string   `json:"version"`
		ContentHash  string   `json:"content_hash"`
		Conversation string   `json:"conversation"`
		Parent       string   `json:"parent"`
		Role         string   `json:"role"`
		Branch       string   `json:"branch"`
		Gaps         []string `json:"gaps"`
	}
	stored := 0
	for start := 0; start < archive.Len(); {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		records, err := archive.Records(start, 500)
		if err != nil {
			return 0, err
		}
		identities := make([]connectors.ArchiveIdentity, len(records))
		for i, r := range records {
			identities[i] = connectors.ArchiveIdentity{ID: r.ID, Version: r.Version}
		}
		allowed, imported, err := classifyArchiveTx(ctx, tx, scope, namespace, identities)
		if err != nil {
			return 0, err
		}
		if err = archive.Drop(start, allowed); err != nil {
			return 0, err
		}
		keys := make([]key, 0, len(records))
		kept := 0
		for i, r := range records {
			if !allowed[i] {
				continue
			}
			kept++
			if imported == 0 {
				continue
			}
			body, err := json.Marshal(archiveIngestRequest(namespace, r))
			if err != nil {
				return 0, err
			}
			hash := sha256.Sum256(body)
			conversation := ""
			if r.ConversationID != "" {
				conversation = namespace + ":" + r.ConversationID
			}
			gaps := r.MissingAttachments
			if gaps == nil {
				gaps = []string{}
			}
			keys = append(keys, key{r.ID, r.Version, hex.EncodeToString(hash[:]), conversation, r.ParentID, r.Role, r.Branch, gaps})
		}
		if imported > 0 {
			var matched, inserted int
			var conflict bool
			err = tx.QueryRow(ctx, `WITH existing AS (
  SELECT s.id,v.version,
   v.content_hash=decode(k.content_hash,'hex') AND
   (c.source_id IS NULL OR (c.conversation_key=k.conversation AND c.parent_key=k.parent
     AND c.role=k.role AND c.branch=k.branch AND c.gaps=k.gaps)) AS matches
  FROM jsonb_to_recordset($5::jsonb) AS k(id text,version text,content_hash text,conversation text,parent text,role text,branch text,gaps jsonb)
  JOIN sources s ON s.owner_id=$1 AND s.connector=$2 AND s.external_id=k.id
  JOIN source_versions v ON v.owner_id=s.owner_id AND v.source_id=s.id AND v.external_version=k.version
  JOIN memory_records r ON r.owner_id=s.owner_id AND r.id=s.id AND r.state='active'
  JOIN record_versions rv ON (rv.owner_id,rv.record_id,rv.version)=(v.owner_id,v.source_id,v.version) AND rv.state='active'
  LEFT JOIN source_contexts c ON (c.owner_id,c.source_id,c.source_version)=(v.owner_id,v.source_id,v.version)
 ), inserted AS (
  INSERT INTO archive_entries(owner_id,archive_id,archive_version,source_id,source_version)
  SELECT $1,$3,$4,id,version FROM existing WHERE matches ON CONFLICT DO NOTHING RETURNING 1
 ) SELECT count(*)::integer,coalesce(bool_or(NOT matches),false),(SELECT count(*)::integer FROM inserted)
 FROM existing`, string(scope.OwnerID), namespace, string(ref.ID), ref.Version, asJSON(keys)).Scan(&matched, &conflict, &inserted)
			if err != nil {
				return 0, err
			}
			if conflict {
				return 0, memory.ErrConflict
			}
			stored += matched
		}
		start += kept
	}
	archive.Preview.AlreadyImported = stored
	return stored, nil
}

func (s *Store) pendingArchive(ctx context.Context, scope memory.Scope, namespace string, ref memory.Ref, archive *connectors.Archive) error {
	return pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		return archive.Filter(ctx, func(identities []connectors.ArchiveIdentity) ([]bool, error) {
			type key struct {
				Index   int    `json:"idx"`
				ID      string `json:"id"`
				Version string `json:"version"`
			}
			keys := make([]key, len(identities))
			keep := make([]bool, len(identities))
			for i, r := range identities {
				keys[i] = key{i, r.ID, r.Version}
			}
			rows, err := tx.Query(ctx, `SELECT k.idx,NOT EXISTS(
   SELECT 1 FROM sources s JOIN source_versions v ON v.owner_id=s.owner_id AND v.source_id=s.id
   JOIN archive_entries e ON (e.owner_id,e.source_id,e.source_version)=(v.owner_id,v.source_id,v.version)
   WHERE s.owner_id=$1 AND s.connector=$2 AND s.external_id=k.id AND v.external_version=k.version
    AND e.archive_id=$3 AND e.archive_version=$4)
  FROM jsonb_to_recordset($5::jsonb) AS k(idx integer,id text,version text) ORDER BY k.idx`, string(scope.OwnerID), namespace, string(ref.ID), ref.Version, asJSON(keys))
			if err != nil {
				return nil, err
			}
			defer rows.Close()
			for rows.Next() {
				var index int
				var pending bool
				if err = rows.Scan(&index, &pending); err != nil {
					return nil, err
				}
				keep[index] = pending
			}
			return keep, rows.Err()
		})
	})
}
