package postgres

import (
	"context"
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
	type key struct {
		ID      string `json:"id"`
		Version string `json:"version"`
	}
	err = pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		keys := make([]key, 0, 500)
		flush := func() error {
			if len(keys) == 0 {
				return nil
			}
			var n int
			err := tx.QueryRow(ctx, `SELECT count(*) FROM jsonb_to_recordset($2::jsonb) AS k(id text,version text)
   JOIN sources s ON s.owner_id=$1 AND s.connector='archive-records' AND s.external_id=k.id
   JOIN source_versions v ON v.owner_id=s.owner_id AND v.source_id=s.id AND v.external_version=k.version
   JOIN memory_records r ON r.owner_id=s.owner_id AND r.id=s.id AND r.state='active'
   JOIN record_versions rv ON (rv.owner_id,rv.record_id,rv.version)=(v.owner_id,v.source_id,v.version) AND rv.state='active'`, string(scope.OwnerID), asJSON(keys)).Scan(&n)
			if err != nil {
				return err
			}
			archive.Preview.AlreadyImported += n
			keys = keys[:0]
			return nil
		}
		err := archive.Walk(ctx, func(r connectors.Record) error {
			keys = append(keys, key{ID: r.ID, Version: r.Version})
			if len(keys) == cap(keys) {
				return flush()
			}
			return nil
		})
		if err == nil {
			err = flush()
		}
		return err
	})
	return archive.Preview, err
}

func (s *Store) ImportArchiveReader(ctx context.Context, scope memory.Scope, name string, r io.Reader) (connectors.Result, error) {
	return s.importArchiveReader(ctx, scope, name, r, "archive")
}
func (s *Store) importArchiveReader(ctx context.Context, scope memory.Scope, name string, r io.Reader, namespace string) (connectors.Result, error) {
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
		out.Gaps = archive.Preview.Gaps
		return out, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return out, err
	}
	key, err := s.blobs.Put(ctx, scope, archive.Original())
	if err != nil {
		return out, err
	}
	in := memory.IngestRequest{Connector: namespace, ExternalID: archive.Hash, ExternalVersion: "1", Title: name, MediaType: "application/x-pcas-archive", Text: "[附件原件；正文尚未解析。校验标识：" + archive.Hash + "]"}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.ensureOwner(ctx, tx, scope); err != nil {
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
		_, err = tx.Exec(ctx, `INSERT INTO import_batches(owner_id,id,archive_id,name,state,total,stored,left_out,earliest,latest)
   VALUES($1,$2,$3,$4,'importing',$5,0,$6,$7,$8)`, string(scope.OwnerID), string(out.BatchID), string(result.ID), name, archive.Len(), archive.Preview.LeftOut, archive.Preview.Earliest, archive.Preview.Latest)
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
	out.Gaps = archive.Preview.Gaps
	return out, err
}

type ImportBatch = connectors.ImportBatch

// importBatchSelect resolves organization progress from the extraction ledger.
const importBatchSelect = `SELECT b.id::text,b.archive_id::text,r.version,b.name,b.state,b.total,b.stored,
 (SELECT count(*) FROM archive_entries e WHERE e.owner_id=b.owner_id AND e.archive_id=b.archive_id
  AND EXISTS(SELECT 1 FROM source_extractions x WHERE (x.owner_id,x.source_id,x.source_version)=(e.owner_id,e.source_id,e.source_version) AND x.state IN ('done','empty'))),
 b.left_out,b.earliest,b.latest,b.error_code,b.created_at,b.updated_at
 FROM import_batches b JOIN memory_records r ON r.owner_id=b.owner_id AND r.id=b.archive_id`

func scanImportBatch(row pgx.Row) (ImportBatch, error) {
	var b ImportBatch
	err := row.Scan(&b.ID, &b.ArchiveID, &b.ArchiveVersion, &b.Name, &b.State, &b.Total, &b.Stored, &b.Organized, &b.LeftOut, &b.Earliest, &b.Latest, &b.ErrorCode, &b.CreatedAt, &b.UpdatedAt)
	if b.ErrorCode != "" {
		b.Error = importErrorMessage(b.ErrorCode)
	}
	return b, err
}
func importErrorMessage(code string) string {
	switch code {
	case "import_storage_failed":
		return "保存中断了，已存好的记录仍在，请点继续重试。"
	case "reimport_blocked":
		return "归档里包含已删除的记录，请移除这些记录后重新导入。"
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
			records, err := archive.Records(stored, max(1, ImportChunkSize))
			if err != nil {
				return err
			}
			var connector string
			if err = tx.QueryRow(ctx, "SELECT connector FROM sources WHERE owner_id=$1 AND id=$2", string(j.OwnerID), string(j.Record.ID)).Scan(&connector); err != nil {
				return err
			}
			namespace := "archive-records"
			connectionID := strings.TrimPrefix(connector, "connection-archive:")
			if strings.HasPrefix(connector, "connection-archive:") && memory.ID(connectionID).Valid() {
				namespace = "connection:" + connectionID
			}
			result, err := s.importBatchTx(ctx, tx, scope, namespace, connectors.Batch{Records: records, Gaps: archive.Preview.Gaps}, &j.Record)
			if err != nil {
				return err
			}
			if result.Blocked > 0 {
				return memory.ErrBlocked
			}
			if namespace != "archive-records" {
				if _, err = tx.Exec(ctx, "UPDATE connector_configs SET imported=imported+$3,gaps=$4 WHERE owner_id=$1 AND id=$2", string(j.OwnerID), connectionID, result.Imported, asJSON(result.Gaps)); err != nil {
					return err
				}
			}
			stored += len(records)
			state = "importing"
			if stored >= total {
				state = "done"
				stopped = true
			}
			if _, err = tx.Exec(ctx, "UPDATE import_batches SET stored=$3,state=$4,error_code='',updated_at=now() WHERE owner_id=$1 AND id=$2", string(j.OwnerID), id, stored, state); err != nil {
				return err
			}
			if stopped {
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
	if errors.Is(err, memory.ErrBlocked) {
		code = "reimport_blocked"
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
