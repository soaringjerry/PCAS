package postgres

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/blob"
	"github.com/soaringjerry/PCAS/internal/memory"
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
	project, err := currentStudioProjectTx(ctx, tx, scope, item)
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
	if err := requireOwner(scope); err != nil {
		return out, err
	}
	if s.blobs == nil {
		return out, memory.ErrUnavailable
	}
	if strings.TrimSpace(name) == "" || len(name) > 2000 || input == nil {
		return out, memory.ErrInvalid
	}
	if parsed, _, err := mime.ParseMediaType(media); err == nil {
		media = parsed
	} else {
		media = "application/octet-stream"
	}
	// Validate the destination before writing a blob, then recheck in the commit.
	if err := pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error { _, err := studioFileProjectTx(ctx, tx, scope, item); return err }); err != nil {
		return out, err
	}
	count := &fileCountingReader{Reader: input}
	key, err := s.blobs.Put(ctx, scope, count)
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
			_, _ = s.pool.Exec(context.WithoutCancel(ctx), "INSERT INTO blob_cleanup_jobs(owner_id,blob_key) VALUES($1,$2) ON CONFLICT DO NOTHING", string(scope.OwnerID), key)
		}
	}()
	hash := strings.Split(strings.Split(key, "/")[1], "-")[0]
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
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
		in := memory.IngestRequest{Connector: "project-file", ExternalID: project + ":" + hash, ExternalVersion: "1", Title: name, MediaType: media, Text: "[附件原件；正文尚未解析。校验标识：" + hash + "]"}
		result, err := s.ingestAttachmentTx(ctx, tx, scope, in, key)
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
	extracting, done := false, false
	for rows.Next() {
		var job, stage, state, code string
		if err = rows.Scan(&job, &stage, &state, &code); err != nil {
			return out, err
		}
		if (state == "failed" || state == "blocked") && out.Status != "failed" {
			out.Status = "failed"
			out.JobID = &job
			out.FailureReason = fileProcessingReason(code)
		}
		if state != "done" {
			extracting = true
		}
		if strings.HasPrefix(stage, "source.extract") && state == "done" {
			done = true
		}
	}
	if err = rows.Err(); err != nil {
		return out, err
	}
	if out.Status != "failed" && done && !extracting {
		out.Status = "extracted"
	}
	return out, nil
}
func fileProcessingReason(code string) string {
	switch code {
	case "file_parser_unsupported":
		return "此文件类型尚无文字抽取器；原件已保存，可打开或下载，安装相应处理器后在此重试"
	case "file_pdf_page_limit":
		return "PDF超过现有100页解析上限；原件保留，请拆分为每份不超过100页后上传"
	case "file_pdf_invalid":
		return "PDF没有可读页；原件保留，请检查文件完整性后重试"
	case "file_audio_duration_limit":
		return "音频超过现有4小时转录上限；原件保留，请拆分后上传"
	case "file_audio_probe_failed":
		return "无法识别音频时长；原件保留，请检查音频格式或ffprobe处理器后重试"
	case "file_text_too_large":
		return "抽取文字超过现有1MiB解析上限；原件保留，请分成较小文件后上传"
	case "file_text_invalid":
		return "文件不是有效UTF-8文字或正文为空；原件保留，请换成UTF-8文字文件后重试"
	case "provider_not_configured", "provider_unavailable":
		return "模型通道未配置或暂不可用；原件保留，通道恢复后重试"
	case "model_output_invalid":
		return "模型抽取结果格式不合格；原件保留，可在此重试"
	case "file_parse_failed":
		return "文件解析失败；原件保留，请检查文件完整性和解析器后重试"
	default:
		return "处理未完成（" + code + "）；原件保留，可在此重试"
	}
}
func (s *Store) ListProjectFiles(ctx context.Context, scope memory.Scope, item string) (workspace.ProjectFileList, error) {
	out := workspace.ProjectFileList{Items: []workspace.ProjectFile{}}
	if err := requireOwner(scope); err != nil {
		return out, err
	}
	err := pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{AccessMode: pgx.ReadOnly, IsoLevel: pgx.RepeatableRead}, func(tx pgx.Tx) error {
		project, err := studioFileProjectTx(ctx, tx, scope, item)
		if err != nil {
			return err
		}
		ids, err := queryDocuments[string](ctx, tx, `SELECT to_jsonb(f.source_id::text) FROM project_files f JOIN memory_records r ON(r.owner_id,r.id)=(f.owner_id,f.source_id) WHERE f.owner_id=$1 AND f.project_id=$2 AND r.state='active' ORDER BY f.created_at DESC,f.source_id`, string(scope.OwnerID), project)
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
	if err := requireOwner(scope); err != nil {
		return err
	}
	if !confirmed {
		return workspace.ErrFileConfirmation
	}
	if !memory.ID(source).Valid() {
		return memory.ErrInvalid
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
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
		if err = s.deleteTx(ctx, tx, scope, memory.DeleteRequest{BlockReimport: true, Targets: []memory.Ref{{ID: memory.ID(source), Version: version, Kind: memory.SourceKind}}}); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, "UPDATE workspace_owners SET revision=revision+1 WHERE owner_id=$1", string(scope.OwnerID))
		return err
	})
}
