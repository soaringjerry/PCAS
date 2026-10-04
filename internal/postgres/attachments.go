package postgres

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
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
		return err
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := lockJob(ctx, tx, j); err != nil {
			return err
		}
		return acknowledge(ctx, tx, j)
	})
}

type parsedAttachment struct{ Text, Representation, Title, Media string }

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
		_, err = tx.Exec(ctx, "UPDATE workspace_owners SET revision=revision+1 WHERE owner_id=$1", string(scope.OwnerID))
		return err
	})
	return parsed, err
}
func (s *Store) parseAttachment(ctx context.Context, scope memory.Scope, ref memory.Ref, job *worker.Job) (parsedAttachment, error) {
	var parsed parsedAttachment
	file, title, media, err := s.OpenAttachment(ctx, scope, ref.ID, ref.Version)
	if err != nil {
		return parsed, err
	}
	defer file.Close()
	parsed.Title, parsed.Media = title, media
	dir, err := os.MkdirTemp("", "pcas-parse-")
	if err != nil {
		return parsed, err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "original")
	input, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return parsed, err
	}
	_, err = io.Copy(input, file)
	closeErr := input.Close()
	if err != nil {
		return parsed, err
	}
	if closeErr != nil {
		return parsed, closeErr
	}
	var text, representation string
	if strings.HasPrefix(media, "audio/") {
		if s.models == nil {
			return parsed, memory.ErrUnavailable
		}
		provider, ok := s.models.Get(s.models.Config.Transcription)
		if !ok {
			return parsed, memory.ErrUnavailable
		}
		durationText, durationErr := parserOutput(ctx, "ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "default=noprint_wrappers=1:nokey=1", path)
		if durationErr != nil {
			return parsed, memory.ErrUnavailable
		}
		seconds, durationErr := strconv.ParseFloat(strings.TrimSpace(durationText), 64)
		if durationErr != nil || seconds <= 0 || seconds > 4*3600 {
			return parsed, memory.ErrUnavailable
		}
		reservationID, reserveErr := s.reserveModelCostID(ctx, scope.OwnerID, (seconds+1)/60*provider.AudioPerMinute, job)
		if reserveErr != nil {
			return parsed, reserveErr
		}
		audio, err := os.Open(path)
		if err != nil {
			if settleErr := s.settleModelCost(ctx, scope.OwnerID, reservationID, 0); settleErr != nil {
				return parsed, settleErr
			}
			return parsed, err
		}
		text, err = s.models.Transcribe(ctx, audio, title)
		audio.Close()
		// This adapter reports text, not billing. Use the measured duration
		// rather than the extra second held during reservation.
		cost := seconds / 60 * provider.AudioPerMinute
		if err != nil && strings.TrimSpace(text) == "" {
			cost = 0
		}
		if settleErr := s.settleModelCost(ctx, scope.OwnerID, reservationID, cost); settleErr != nil {
			return parsed, settleErr
		}
		if err != nil {
			return parsed, memory.ErrUnavailable
		}
		representation = "transcript"
	} else if media == "application/pdf" {
		text, representation, err = parsePDFWithReader(ctx, dir, path, func(ctx context.Context, path string) (string, string, error) {
			return s.readImage(ctx, scope, ref, path, "image/png", job)
		})
	} else {
		text, representation, err = s.readImage(ctx, scope, ref, path, media, job)
	}
	if err != nil {
		return parsed, err
	}
	if strings.TrimSpace(text) == "" || len(text) > 1<<20 {
		return parsed, memory.ErrUnavailable
	}
	parsed.Text, parsed.Representation = text, representation
	return parsed, nil
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
	return parsePDFWithReader(ctx, dir, path, func(ctx context.Context, path string) (string, string, error) {
		text, err := ocrImage(ctx, path)
		return text, "ocr", err
	})
}
func parsePDFWithReader(ctx context.Context, dir, path string, read func(context.Context, string) (string, string, error)) (string, string, error) {
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
			var pageRepresentation string
			text, pageRepresentation, err = read(ctx, prefix+".png")
			if err != nil {
				return "", "", err
			}
			if pageRepresentation == "ocr" {
				representation = "ocr"
			} else if representation != "ocr" {
				representation = pageRepresentation
			}
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
