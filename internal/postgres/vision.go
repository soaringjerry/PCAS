package postgres

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/modelcall"
	"github.com/soaringjerry/PCAS/internal/prompts"
	"github.com/soaringjerry/PCAS/internal/worker"
)

// A failed or unavailable model falls back to the existing OCR processor.
// Logs contain stages and error classes, never images, transcripts or keys.
func (s *Store) readImage(ctx context.Context, scope memory.Scope, ref memory.Ref, path, media string, job *worker.Job) (string, string, error) {
	originalData, err := os.ReadFile(path)
	if err != nil {
		return "", "", err
	}
	component := fmt.Sprintf("vision:%s:%x", filepath.Base(path), sha256.Sum256(originalData))
	cached, err := s.attachmentReceipt(ctx, scope, ref, component)
	if err != nil {
		return "", "", err
	}
	if cached != nil {
		if err = s.accountAttachmentReceipt(ctx, scope, ref, cached, "vision", job); err != nil {
			return "", "", err
		}
		return cached.Output, "vision", nil
	}
	p, ok := s.models.VisionProvider()
	stage, kind := "select", "unavailable"
	if ok {
		visionPath, visionMedia := path, media
		if media == "image/tiff" {
			visionPath = filepath.Join(filepath.Dir(path), "vision.png")
			if _, err := parserOutput(ctx, "ffmpeg", "-v", "error", "-i", path, "-frames:v", "1", "-y", visionPath); err != nil {
				slog.WarnContext(ctx, "attachment vision fallback", "stage", "convert", "error_type", "processor_unavailable")
				text, err := ocrImage(ctx, path)
				return text, "ocr", err
			}
			visionMedia = "image/png"
		}
		data, err := os.ReadFile(visionPath)
		if err != nil {
			return "", "", err
		}
		image := ai.Image{MediaType: visionMedia, Data: data}
		// The image's job is named in the usage record. Its queue budget rules
		// do not apply, because one file can need a call for each page.
		instructions := prompts.Must("vision")
		request := modelcall.DirectRequest{OwnerID: scope.OwnerID, ExecutionID: memory.NewID(), Function: "vision", Stage: "image", Provider: p, Estimate: p.ReserveVision(instructions.Text(), image), Instructions: instructions, Refs: []memory.Ref{ref}}
		request.RootExecutionID = request.ExecutionID
		if job != nil {
			request.RootExecutionID, request.CausationID, request.UsageJob = job.ID, job.ID, job.ID
		}
		work, cancel := context.WithTimeout(ctx, 45*time.Second)
		result, call, err := s.calls.CallVision(work, request, image)
		cancel()
		if err == nil {
			paid := &paidModelResult{Output: result.Text, Reservation: call.Reservation, Provider: p.ID, Model: p.Model, InputTokens: result.InputTokens, OutputTokens: result.OutputTokens, InputEstimated: result.InputEstimated, OutputEstimated: result.OutputEstimated, CostEstimated: result.CostEstimated, Cost: result.Cost}
			if err = s.saveAttachmentReceipt(ctx, scope, ref, component, paid); err != nil {
				return "", "", err
			}
			return paid.Output, "vision", nil
		}
		var persistence *modelcall.PersistenceError
		if errors.As(err, &persistence) {
			return "", "", err
		}
		// The gateway recorded a submitted call and its usage. A reservation
		// that failed submitted nothing.
		stage = "budget"
		var failure *modelcall.Failure
		if errors.As(err, &failure) {
			stage = "model"
		}
		kind = secretaryErrorType(stage, err)
	}
	slog.WarnContext(ctx, "attachment vision fallback", "stage", stage, "error_type", kind)
	text, err := ocrImage(ctx, path)
	return text, "ocr", err
}
