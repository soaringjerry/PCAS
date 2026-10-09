package telegram

import (
	"context"
	"encoding/json"

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/postgres"
)

// Delivery contracts prescribe decisions through the real gateway journal.
// The HTTP oracle does not establish production schema or search support.
type syntheticGatewayModel struct{ *ai.Registry }

func (m syntheticGatewayModel) CheckGeneration(_ ai.Provider, mode ai.GenerationMode) error {
	if len(mode.Schema) > 0 && !json.Valid(mode.Schema) {
		return memory.ErrInvalid
	}
	return nil
}

func (m syntheticGatewayModel) GenerateProvider(ctx context.Context, provider ai.Provider, instructions, prompt string, _ ...ai.GenerationMode) (ai.Result, error) {
	return m.Registry.GenerateProvider(ctx, provider, instructions, prompt)
}

func configureSyntheticGateway(store *postgres.Store, registry *ai.Registry) {
	store.SetModelsWithGatewayProviders(registry, syntheticGatewayModel{registry})
}
