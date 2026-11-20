package postgres

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/blob"
	"github.com/soaringjerry/PCAS/internal/input/attachment"
	"github.com/soaringjerry/PCAS/internal/input/projectfile"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/postgres/intakestore"
	"github.com/soaringjerry/PCAS/internal/postgres/kernel"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

type fileCountingReader struct {
	io.Reader
	n int64
}

func (r *fileCountingReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	r.n += int64(n)
	return n, err
}

func studioFileProjectTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, item string) (string, error) {
	if !memory.ID(item).Valid() {
		return "", memory.ErrInvalid
	}
	project, err := intakestore.CurrentStudioProjectTx(ctx, tx, scope, item)
	if err != nil {
		return "", err
	}
	if project == "" {
		return "", fmt.Errorf("%w: file requires a project", memory.ErrInvalid)
	}
	return project, nil
}
func (s *Store) UploadProjectFile(ctx context.Context, scope memory.Scope, item, name, media string, input io.Reader) (workspace.ProjectFileUpload, error) {
	out := workspace.ProjectFileUpload{}
	if err := kernel.RequireOwner(scope); err != nil {
		return out, err
	}
	if s.Blobs == nil {
		return out, memory.ErrUnavailable
	}
	if strings.TrimSpace(name) == "" || len(name) > 2000 || input == nil {
		return out, memory.ErrInvalid
	}
	peek := bufio.NewReader(input)
	input = peek
	media = projectfile.Media(name, media, func() []byte { head, _ := peek.Peek(512); return head })
	// Validate the destination before writing a blob, then recheck in the commit.
	if err := pgx.BeginTxFunc(ctx, s.Pool, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error { _, err := studioFileProjectTx(ctx, tx, scope, item); return err }); err != nil {
		return out, err
	}
	count := &fileCountingReader{Reader: input}
	key, err := s.Blobs.Put(ctx, scope, count)
	if count.n > blob.MaxBytes {
		return out, workspace.ErrFileTooLarge
	}
	if count.n == 0 {
		return out, workspace.ErrFileEmpty
	}
	if err != nil {
		return out, err
	}
	used := false
	defer func() {
		if !used {
			_, _ = s.Pool.Exec(context.WithoutCancel(ctx), "INSERT INTO blob_cleanup_jobs(owner_id,blob_key) VALUES($1,$2) ON CONFLICT DO NOTHING", string(scope.OwnerID), key)
		}
	}()
	hash := strings.Split(strings.Split(key, "/")[1], "-")[0]
	err = pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT 1 FROM workspace_owners WHERE owner_id=$1 FOR UPDATE", string(scope.OwnerID)); err != nil {
			return err
		}
		project, err := studioFileProjectTx(ctx, tx, scope, item)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", string(scope.OwnerID)+":file:"+project+":"+hash); err != nil {
			return err
		}
		var source, state string
		err = tx.QueryRow(ctx, `SELECT f.source_id::text,r.state FROM project_files f JOIN memory_records r ON(r.owner_id,r.id)=(f.owner_id,f.source_id) WHERE f.owner_id=$1 AND f.project_id=$2 AND f.content_hash=$3`, string(scope.OwnerID), project, hash).Scan(&source, &state)
		if err == nil {
			if state != "active" {
				return memory.ErrBlocked
			}
			out.File, err = projectFileTx(ctx, tx, scope, source)
			out.AlreadyExists = true
			return err
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		in := memory.IngestRequest{Connector: "project-file", ExternalID: project + ":" + hash, ExternalVersion: "1", Title: name, MediaType: media, Text: attachment.Placeholder(hash)}
		result, err := s.IngestAttachmentTx(ctx, tx, scope, in, key)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE source_versions SET scope=jsonb_build_object('project_id',$4::text) WHERE owner_id=$1 AND source_id=$2 AND version=$3`, string(scope.OwnerID), string(result.ID), result.Version, project); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO project_files(owner_id,source_id,project_id,content_hash,size) VALUES($1,$2,$3,$4,$5)`, string(scope.OwnerID), string(result.ID), project, hash, count.n); err != nil {
			return err
		}
		out.File, err = projectFileTx(ctx, tx, scope, string(result.ID))
		return err
	})
	used = err == nil && !out.AlreadyExists
	return out, err
}

func projectFileTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, id string) (workspace.ProjectFile, error) {
	out := workspace.ProjectFile{SourceID: id, Status: "stored"}
	var at time.Time
	err := tx.QueryRow(ctx, `SELECT f.project_id::text,v.title,v.media_type,f.size,f.created_at FROM project_files f JOIN memory_records r ON(r.owner_id,r.id)=(f.owner_id,f.source_id) JOIN source_versions v ON(v.owner_id,v.source_id,v.version)=(r.owner_id,r.id,r.version) WHERE f.owner_id=$1 AND f.source_id=$2 AND r.state='active'`, string(scope.OwnerID), id).Scan(&out.ProjectID, &out.Name, &out.MediaType, &out.Size, &at)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, memory.ErrNotFound
	}
	if err != nil {
		return out, err
	}
	out.CreatedAt = at.UTC().Format(time.RFC3339Nano)
	out.OpenURL = "/v1/memory/sources/" + id + "/attachment?inline=true"
	rows, err := tx.Query(ctx, `WITH RECURSIVE family(id) AS(SELECT $2::uuid UNION SELECT v.source_id FROM source_versions v JOIN family f ON v.derived_from_id=f.id WHERE v.owner_id=$1) SELECT j.id::text,j.stage,j.state,j.error_code FROM memory_jobs j JOIN family f ON f.id=j.record_id WHERE j.owner_id=$1 AND (j.stage IN('source.parse','source.chunk') OR j.stage LIKE 'source.extract%') ORDER BY j.updated_at DESC,j.id`, string(scope.OwnerID), id)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	jobs := []projectfile.Job{}
	for rows.Next() {
		var j projectfile.Job
		if err = rows.Scan(&j.ID, &j.Stage, &j.State, &j.Code); err != nil {
			return out, err
		}
		jobs = append(jobs, j)
	}
	if err = rows.Err(); err != nil {
		return out, err
	}
	var job string
	out.Status, job, out.FailureReason = projectfile.State(jobs)
	if job != "" {
		out.JobID = &job
	}
	return out, nil
}
func (s *Store) ListProjectFiles(ctx context.Context, scope memory.Scope, item string) (workspace.ProjectFileList, error) {
	out := workspace.ProjectFileList{Items: []workspace.ProjectFile{}}
	if err := kernel.RequireOwner(scope); err != nil {
		return out, err
	}
	err := pgx.BeginTxFunc(ctx, s.Pool, pgx.TxOptions{AccessMode: pgx.ReadOnly, IsoLevel: pgx.RepeatableRead}, func(tx pgx.Tx) error {
		project, err := studioFileProjectTx(ctx, tx, scope, item)
		if err != nil {
			return err
		}
		ids, err := kernel.QueryDocuments[string](ctx, tx, `SELECT to_jsonb(f.source_id::text) FROM project_files f JOIN memory_records r ON(r.owner_id,r.id)=(f.owner_id,f.source_id) WHERE f.owner_id=$1 AND f.project_id=$2 AND r.state='active' ORDER BY f.created_at DESC,f.source_id`, string(scope.OwnerID), project)
		if err != nil {
			return err
		}
		for _, id := range ids {
			f, err := projectFileTx(ctx, tx, scope, id)
			if err != nil {
				return err
			}
			out.Items = append(out.Items, f)
		}
		return nil
	})
	return out, err
}
func (s *Store) DeleteProjectFile(ctx context.Context, scope memory.Scope, item, source string, confirmed bool) error {
	if err := kernel.RequireOwner(scope); err != nil {
		return err
	}
	if !confirmed {
		return workspace.ErrFileConfirmation
	}
	if !memory.ID(source).Valid() {
		return memory.ErrInvalid
	}
	return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT 1 FROM workspace_owners WHERE owner_id=$1 FOR UPDATE", string(scope.OwnerID)); err != nil {
			return err
		}
		project, err := studioFileProjectTx(ctx, tx, scope, item)
		if err != nil {
			return err
		}
		var version int
		err = tx.QueryRow(ctx, `SELECT r.version FROM project_files f JOIN memory_records r ON(r.owner_id,r.id)=(f.owner_id,f.source_id) WHERE f.owner_id=$1 AND f.project_id=$2 AND f.source_id=$3 AND r.state='active' FOR UPDATE OF r`, string(scope.OwnerID), project, source).Scan(&version)
		if errors.Is(err, pgx.ErrNoRows) {
			return memory.ErrNotFound
		}
		if err != nil {
			return err
		}
		if err = s.DeleteTx(ctx, tx, scope, memory.DeleteRequest{BlockReimport: true, Targets: []memory.Ref{{ID: memory.ID(source), Version: version, Kind: memory.SourceKind}}}); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, "UPDATE workspace_owners SET revision=revision+1 WHERE owner_id=$1", string(scope.OwnerID))
		return err
	})
}
