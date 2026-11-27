package retrieval

import (
	"context"
	"errors"
	"log/slog"

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
	embeddingID := r.Providers.EmbeddingID()
	provider, ok := r.Providers.Get(embeddingID)
	if !ok {
		slog.WarnContext(ctx, "query embedding provider missing", "provider", embeddingID)
		return nil, "", "语义检索暂不可用，已使用原文与属性检索", nil
	}
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
		slog.ErrorContext(ctx, "query embedding persistence failed", "provider", provider.ID, "model", provider.Model, "execution_id", string(request.ExecutionID), "root_execution_id", string(request.RootExecutionID), "error", err)
		return nil, "", "", err
	}
	if err == nil && len(embeddings) == 1 {
		return embeddings[0].Values, embeddings[0].Model, "", nil
	}
	logQueryEmbeddingFailure(ctx, request, err, len(embeddings))
	return nil, "", "语义检索暂不可用，已使用原文与属性检索", nil
}

func logQueryEmbeddingFailure(ctx context.Context, request modelcall.DirectRequest, err error, vectors int) {
	attrs := []any{
		"provider", request.Provider.ID,
		"model", request.Provider.Model,
		"execution_id", string(request.ExecutionID),
		"root_execution_id", string(request.RootExecutionID),
	}
	if err == nil {
		attrs = append(attrs, "vectors", vectors)
		slog.WarnContext(ctx, "query embedding returned unexpected vector count", attrs...)
		return
	}
	var failure *modelcall.Failure
	if errors.As(err, &failure) {
		attrs = append(attrs, "error_code", failure.Code)
		if failure.InvocationID != "" {
			attrs = append(attrs, "invocation_id", string(failure.InvocationID))
		}
		if failure.Details != nil {
			if failure.Details.HTTPStatus != 0 {
				attrs = append(attrs, "http_status", failure.Details.HTTPStatus)
			}
			if failure.Details.Cancellation != "" {
				attrs = append(attrs, "cancellation", failure.Details.Cancellation)
			}
			if failure.Details.OutcomeUnknown {
				attrs = append(attrs, "outcome_unknown", true)
			}
			if failure.Details.Codex != nil && failure.Details.Codex.Category != "" {
				attrs = append(attrs, "codex_category", failure.Details.Codex.Category)
				if failure.Details.Codex.HTTPStatus != 0 {
					attrs = append(attrs, "codex_http_status", failure.Details.Codex.HTTPStatus)
				}
			}
		}
	}
	attrs = append(attrs, "error", err.Error())
	slog.WarnContext(ctx, "query embedding failed", attrs...)
}
