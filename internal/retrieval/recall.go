package retrieval

import (
	"context"
	"errors"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/modelcall"
)

// QueryEmbed returns the vector of a query. gap is not empty when there is no
// vector: it is the sentence that the recall reports in its coverage, and the
// recall continues with text and attribute search. err ends the recall.
type QueryEmbed func(ctx context.Context, scope memory.Scope, query string) (values []float32, model string, gap string, err error)

// RecallStore searches memory. It calls embed once at most, after it checked
// the request.
type RecallStore interface {
	RecallWith(ctx context.Context, scope memory.Scope, in memory.RecallRequest, embed QueryEmbed) (memory.RecallResult, error)
}

// Recaller answers a recall: it requests the query's vector from the model
// and lets storage search.
type Recaller struct {
	Store RecallStore
	// Providers is nil when no model is configured.
	Providers Providers
	Calls     *modelcall.Gateway
}

// Recall returns the memories and the sources for the request.
func (r Recaller) Recall(ctx context.Context, scope memory.Scope, in memory.RecallRequest) (memory.RecallResult, error) {
	return r.Store.RecallWith(ctx, scope, in, r.Embed)
}

// Embed requests the vector of a query. The call is recorded with its cost;
// the query text and the vector are not saved. A call whose record could not
// be written ends the recall.
func (r Recaller) Embed(ctx context.Context, scope memory.Scope, query string) ([]float32, string, string, error) {
	if r.Providers == nil || r.Providers.EmbeddingID() == "" {
		return nil, "", "未配置语义索引；模糊措辞的覆盖尚不完整", nil
	}
	provider, _ := r.Providers.Get(r.Providers.EmbeddingID())
	reserved := float64(len(query)+16) * provider.InputPerMillion / 1e6
	// A query inside a secretary or deputy execution keeps that root.
	// Its own execution identity lets concurrent queries stay separate.
	request := modelcall.DirectRequest{OwnerID: scope.OwnerID, ExecutionID: memory.NewID(), Function: "query_embedding", Stage: "query", Provider: provider, Estimate: reserved}
	request.RootExecutionID = request.ExecutionID
	if parent, ok := modelcall.Execution(ctx); ok && parent.OwnerID == scope.OwnerID {
		request.RootExecutionID, request.CausationID = parent.RootExecutionID, parent.ExecutionID
	}
	embeddings, err := r.Calls.CallEmbedding(ctx, request, []string{provider.EmbeddingQueryPrefix + query})
	var persistence *modelcall.PersistenceError
	if errors.As(err, &persistence) {
		return nil, "", "", err
	}
	if err == nil && len(embeddings) == 1 {
		return embeddings[0].Values, embeddings[0].Model, "", nil
	}
	return nil, "", "语义检索暂不可用，已使用原文与属性检索", nil
}
