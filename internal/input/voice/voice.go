// Package voice transcribes the voice messages of a channel.
package voice

import (
	"context"
	"errors"
	"io"

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/modelcall"
)

// Transcriber transcribes voice messages with the configured provider.
type Transcriber struct {
	// Models is nil when no model is configured.
	Models *ai.Registry
	Calls  *modelcall.Gateway
}

// TranscribeVoice transcribes a channel's voice message of a known length.
// The call is recorded with its cost. The audio and the text are not stored here.
// A missing provider or capability returns memory.ErrUnavailable.
func (t Transcriber) TranscribeVoice(ctx context.Context, scope memory.Scope, audio io.Reader, name string, seconds float64) (string, error) {
	if t.Models == nil || t.Calls == nil {
		return "", memory.ErrUnavailable
	}
	provider, ok := t.Models.Get(t.Models.Config.Transcription)
	if !ok {
		return "", memory.ErrUnavailable
	}
	// The reservation holds one more second than the measured length.
	request := modelcall.DirectRequest{OwnerID: scope.OwnerID, ExecutionID: memory.NewID(), Function: "transcription", Stage: "voice", Provider: provider, Estimate: (seconds + 1) / 60 * provider.AudioPerMinute}
	request.RootExecutionID = request.ExecutionID
	text, _, _, err := t.Calls.CallTranscription(ctx, request, audio, name, seconds)
	var failure *modelcall.Failure
	if errors.Is(err, modelcall.ErrNotAvailable) || errors.Is(err, ai.ErrUnsupportedCapability) || errors.As(err, &failure) && failure.Code == modelcall.ErrNotAvailable.Error() {
		return "", memory.ErrUnavailable
	}
	return text, err
}
