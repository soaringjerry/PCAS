package attachment

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/modelcall"
	"github.com/soaringjerry/PCAS/internal/prompts"
	"github.com/soaringjerry/PCAS/internal/worker"
)

// Receipts keeps the paid readings of one original file with the file.
type Receipts interface {
	// Saved returns the paid reading of a component of the file. It records
	// the usage and the cost of a saved reading again; both writes are
	// idempotent. purpose names the reading in the usage record.
	Saved(ctx context.Context, component, purpose string) (output string, found bool, err error)
	// Save keeps a paid reading before any derived write. A failed write of
	// the derived text uses the kept reading, also after a restart.
	Save(ctx context.Context, component string, paid *modelcall.PaidResult) error
}

// Read parses one original file. receipts keeps the paid readings of that
// file. job is the job that pays, or nil for a reading inside a turn.
type Read func(ctx context.Context, receipts Receipts, scope memory.Scope, ref memory.Ref, job *worker.Job, file io.Reader, title, media string) (Parsed, error)

// Reader parses original files with the configured models.
type Reader struct {
	// Models is nil when no model is configured.
	Models *ai.Registry
	Calls  *modelcall.Gateway
	// ErrorType gives the fixed category of a failure.
	ErrorType func(stage string, err error) string
}

// Read parses one original file. It is a Read.
func (r Reader) Read(ctx context.Context, receipts Receipts, scope memory.Scope, ref memory.Ref, job *worker.Job, file io.Reader, title, media string) (Parsed, error) {
	return Parser{Calls: modelCalls{reader: r, receipts: receipts, scope: scope, ref: ref, job: job}}.Parse(ctx, file, title, media)
}

// modelCalls makes the paid calls of one original file through the gateway.
type modelCalls struct {
	reader   Reader
	receipts Receipts
	scope    memory.Scope
	ref      memory.Ref
	job      *worker.Job
}

func (a modelCalls) SavedTranscript(ctx context.Context) (string, bool, error) {
	models := a.reader.Models
	if models == nil {
		return "", false, memory.ErrUnavailable
	}
	saved, found, err := a.receipts.Saved(ctx, "transcript", "transcription")
	if err != nil || found {
		return saved, found, err
	}
	if _, ok := models.Get(models.Config.Transcription); !ok {
		return "", false, memory.ErrUnavailable
	}
	return "", false, nil
}

func (a modelCalls) Transcribe(ctx context.Context, audio io.Reader, title string, seconds float64) (string, error) {
	models := a.reader.Models
	provider, ok := models.Get(models.Config.Transcription)
	if !ok {
		return "", memory.ErrUnavailable
	}
	// The reservation holds one more second than the measured length.
	request := modelcall.DirectRequest{OwnerID: a.scope.OwnerID, ExecutionID: memory.NewID(), Function: "transcription", Stage: "audio", Provider: provider, Estimate: (seconds + 1) / 60 * provider.AudioPerMinute, Refs: []memory.Ref{a.ref}, Job: a.job}
	request.RootExecutionID = request.ExecutionID
	if a.job != nil {
		request.RootExecutionID, request.CausationID = a.job.ID, a.job.ID
	}
	text, usage, call, err := a.reader.Calls.CallTranscription(ctx, request, audio, title, seconds)
	if err == nil {
		paid := &modelcall.PaidResult{Output: text, Reservation: call.Reservation, Provider: provider.ID, Model: provider.Model, InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens, InputEstimated: true, OutputEstimated: true, Cost: usage.Cost, CostEstimated: usage.CostEstimated}
		return text, a.receipts.Save(ctx, "transcript", paid)
	}
	// A submitted call is recorded with its usage. Other errors come from
	// the reservation or the accounting write and keep their own meaning.
	var failure *modelcall.Failure
	if errors.As(err, &failure) || errors.Is(err, modelcall.ErrNotAvailable) || errors.Is(err, ai.ErrUnsupportedCapability) {
		return "", memory.ErrUnavailable
	}
	return "", err
}

func (a modelCalls) SavedReading(ctx context.Context, component string) (string, bool, error) {
	return a.receipts.Saved(ctx, component, "vision")
}

func (a modelCalls) CanReadImages() bool {
	_, ok := a.reader.Models.VisionProvider()
	return ok
}

func (a modelCalls) ReadImage(ctx context.Context, component string, image ai.Image) (string, error) {
	p, ok := a.reader.Models.VisionProvider()
	if !ok {
		return "", &Fallback{Stage: "select", Kind: "unavailable"}
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
	result, call, err := a.reader.Calls.CallVision(work, request, image)
	cancel()
	if err == nil {
		paid := &modelcall.PaidResult{Output: result.Text, Reservation: call.Reservation, Provider: p.ID, Model: p.Model, InputTokens: result.InputTokens, OutputTokens: result.OutputTokens, InputEstimated: result.InputEstimated, OutputEstimated: result.OutputEstimated, CostEstimated: result.CostEstimated, Cost: result.Cost}
		if err = a.receipts.Save(ctx, component, paid); err != nil {
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
	return "", &Fallback{Stage: stage, Kind: a.reader.ErrorType(stage, err)}
}
