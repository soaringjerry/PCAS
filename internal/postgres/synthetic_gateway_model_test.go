package postgres

import (
	"context"
	"encoding/json"

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
)

// A business-contract model can produce prescribed or invalid decisions in
// every mode. Its HTTP oracle is synthetic, not an OpenAI capability claim.
// Real capability and Codex transport tests use an unwrapped ai.Registry.
type syntheticGatewayModel struct{ *ai.Registry }

func (m syntheticGatewayModel) CheckGeneration(_ ai.Provider, mode ai.GenerationMode) error {
	if len(mode.Schema) > 0 && !json.Valid(mode.Schema) {
		return memory.ErrInvalid
	}
	return nil
}

func (m syntheticGatewayModel) GenerateProvider(ctx context.Context, provider ai.Provider, instructions, prompt string, _ ...ai.GenerationMode) (ai.Result, error) {
	// Only the synthetic decision oracle handles this request. Production mode
	// enforcement and provider transports are tested separately, without this stub.
	return m.Registry.GenerateProvider(ctx, provider, instructions, prompt)
}

func useSyntheticGateway(s *Store) {
	s.SetModelsWithGatewayProviders(s.models, syntheticGatewayModel{s.models})
}

// UseSyntheticGatewayForTest exposes the same test-only model to external
// business acceptance fixtures. Production builds do not include this helper.
func UseSyntheticGatewayForTest(s *Store) { useSyntheticGateway(s) }
