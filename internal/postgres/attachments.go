package postgres

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/connectors"
	"github.com/soaringjerry/PCAS/internal/memory"
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
	if !oneOf(in.MediaType, "application/x-pcas-archive", "application/pdf", "image/png", "image/jpeg", "image/webp", "image/tiff", "audio/mpeg", "audio/wav", "audio/mp4") || requireText(in.Title) != nil || len(in.Title) > 2000 || in.Connector == "" || in.ExternalID == "" || in.ExternalVersion == "" {
		return result, memory.ErrInvalid
	}
	key, err := s.blobs.Put(ctx, scope, r)
	if err != nil {
		return result, err
	}
	in.Text = "[附件原件；正文尚未解析。校验标识：" + strings.Split(strings.Split(key, "/")[1], "-")[0] + "]"
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		result, err = s.ingestTx(ctx, tx, scope, in)
		if err != nil {
			return err
		}
		if result.Duplicate {
			_, err := tx.Exec(ctx, "INSERT INTO blob_cleanup_jobs(owner_id,blob_key) VALUES($1,$2) ON CONFLICT DO NOTHING", string(scope.OwnerID), key)
			return err
		}
		if _, err := tx.Exec(ctx, "UPDATE source_versions SET blob_key=$4,body='' WHERE owner_id=$1 AND source_id=$2 AND version=$3", string(scope.OwnerID), string(result.ID), result.Version, key); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "DELETE FROM memory_jobs WHERE owner_id=$1 AND record_id=$2 AND record_version=$3 AND stage='source.chunk' AND state='queued'", string(scope.OwnerID), string(result.ID), result.Version); err != nil {
			return err
		}
		return enqueue(ctx, tx, scope.OwnerID, result.ID, result.Version, "source.parse")
	})
	if err != nil { // An uncommitted upload is cleaned only when no committed source uses it.
		_, _ = s.pool.Exec(context.WithoutCancel(ctx), "INSERT INTO blob_cleanup_jobs(owner_id,blob_key) VALUES($1,$2) ON CONFLICT DO NOTHING", string(scope.OwnerID), key)
	}
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
		return err
	}
	defer file.Close()
	if media == "application/x-pcas-archive" {
		data, err := io.ReadAll(io.LimitReader(file, 20<<20+1))
		if err != nil || len(data) > 20<<20 {
			return memory.ErrInvalid
		}
		batch, err := connectors.DecodeArchive(title, data)
		if err != nil {
			return memory.ErrInvalid
		}
		return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
			if err := lockJob(ctx, tx, j); err != nil {
				return err
			}
			var connector string
			if err := tx.QueryRow(ctx, "SELECT connector FROM sources WHERE owner_id=$1 AND id=$2", string(j.OwnerID), string(j.Record.ID)).Scan(&connector); err != nil {
				return err
			}
			namespace := "archive-records"
			connectionID := strings.TrimPrefix(connector, "connection-archive:")
			if strings.HasPrefix(connector, "connection-archive:") && memory.ID(connectionID).Valid() {
				namespace = "connection:" + connectionID
			}
			gaps := batch.Gaps
			if gaps == nil {
				gaps = []string{}
			}
			if _, err := tx.Exec(ctx, `INSERT INTO source_contexts(owner_id,source_id,source_version,gaps) VALUES($1,$2,$3,$4) ON CONFLICT(owner_id,source_id,source_version) DO UPDATE SET gaps=excluded.gaps`, string(j.OwnerID), string(j.Record.ID), j.Record.Version, asJSON(gaps)); err != nil {
				return err
			}
			result, err := s.importBatchTx(ctx, tx, scope, namespace, batch, &j.Record)
			if err != nil {
				return err
			}
			if namespace != "archive-records" {
				if _, err := tx.Exec(ctx, "UPDATE connector_configs SET imported=imported+$3,gaps=$4 WHERE owner_id=$1 AND id=$2", string(j.OwnerID), connectionID, result.Imported, asJSON(result.Gaps)); err != nil {
					return err
				}
			}
			if result.Blocked > 0 {
				if err := redactArchivesTx(ctx, tx, scope, []string{string(j.Record.ID)}); err != nil {
					return err
				}
			}
			return acknowledge(ctx, tx, j)
		})
	}

	dir, err := os.MkdirTemp("", "pcas-parse-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "original")
	input, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = io.Copy(input, file)
	closeErr := input.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	var text, representation string
	if strings.HasPrefix(media, "audio/") {
		if s.models == nil {
			return memory.ErrUnavailable
		}
		provider, ok := s.models.Get(s.models.Config.Transcription)
		if !ok {
			return memory.ErrUnavailable
		}
		durationText, durationErr := parserOutput(ctx, "ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "default=noprint_wrappers=1:nokey=1", path)
		if durationErr != nil {
			return memory.ErrUnavailable
		}
		seconds, durationErr := strconv.ParseFloat(strings.TrimSpace(durationText), 64)
		if durationErr != nil || seconds <= 0 || seconds > 4*3600 {
			return memory.ErrUnavailable
		}
		if err := s.reserveBackgroundCost(ctx, j, (seconds+1)/60*provider.AudioPerMinute); err != nil {
			return err
		}
		audio, err := os.Open(path)
		if err != nil {
			return err
		}
		text, err = s.models.Transcribe(ctx, audio, title)
		audio.Close()
		if err != nil {
			return memory.ErrUnavailable
		}
		representation = "transcript"
	} else if media == "application/pdf" {
		text, representation, err = parsePDF(ctx, dir, path)
	} else {
		text, err = ocrImage(ctx, path)
		representation = "ocr"
	}
	if err != nil {
		return err
	}
	if strings.TrimSpace(text) == "" || len(text) > 1<<20 {
		return memory.ErrUnavailable
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.ensureOwner(ctx, tx, scope); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "SELECT 1 FROM workspace_owners WHERE owner_id=$1 FOR UPDATE", string(j.OwnerID)); err != nil {
			return err
		}
		if err := lockJob(ctx, tx, j); err != nil {
			return err
		}
		derived, err := s.ingestTx(ctx, tx, scope, memory.IngestRequest{Connector: "attachment-text", ExternalID: fmt.Sprintf("%s:%d", j.Record.ID, j.Record.Version), ExternalVersion: "parser-1", Title: title + " · " + representation, Text: text, MediaType: "text/plain"})
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "UPDATE source_versions SET representation=$4,derived_from_id=$5,derived_from_version=$6 WHERE owner_id=$1 AND source_id=$2 AND version=$3", string(j.OwnerID), string(derived.ID), derived.Version, representation, string(j.Record.ID), j.Record.Version); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "UPDATE workspace_owners SET revision=revision+1 WHERE owner_id=$1", string(j.OwnerID)); err != nil {
			return err
		}
		return acknowledge(ctx, tx, j)
	})
}
func parserOutput(ctx context.Context, name string, args ...string) (string, error) {
	if _, err := exec.LookPath(name); err != nil {
		return "", memory.ErrUnavailable
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stderr = io.Discard
	var out boundedOutput
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("attachment parser failed")
	}
	return out.String(), nil
}

type boundedOutput struct{ strings.Builder }

func (b *boundedOutput) Write(data []byte) (int, error) {
	if b.Len()+len(data) > 1<<20 {
		return 0, memory.ErrInvalid
	}
	return b.Builder.Write(data)
}
func ocrImage(ctx context.Context, path string) (string, error) {
	return parserOutput(ctx, "tesseract", path, "stdout", "-l", "chi_sim+eng")
}
func parsePDF(ctx context.Context, dir, path string) (string, string, error) {
	info, err := parserOutput(ctx, "pdfinfo", path)
	if err != nil {
		return "", "", err
	}
	pages := 0
	for _, line := range strings.Split(info, "\n") {
		if strings.HasPrefix(line, "Pages:") {
			pages, _ = strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "Pages:")))
		}
	}
	if pages < 1 || pages > 100 {
		return "", "", memory.ErrUnavailable
	}
	var out strings.Builder
	representation := "extracted"
	for page := 1; page <= pages; page++ {
		n := strconv.Itoa(page)
		text, err := parserOutput(ctx, "pdftotext", "-f", n, "-l", n, "-layout", path, "-")
		if err != nil {
			return "", "", err
		}
		if strings.TrimSpace(text) == "" {
			prefix := filepath.Join(dir, "page")
			if _, err := parserOutput(ctx, "pdftoppm", "-f", n, "-l", n, "-singlefile", "-scale-to", "1800", "-png", path, prefix); err != nil {
				return "", "", err
			}
			text, err = ocrImage(ctx, prefix+".png")
			if err != nil {
				return "", "", err
			}
			representation = "ocr"
		}
		fmt.Fprintf(&out, "\n[第 %d 页]\n%s", page, text)
		if out.Len() > 1<<20 {
			return "", "", memory.ErrUnavailable
		}
	}
	return out.String(), representation, nil
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
