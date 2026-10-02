// pcas-eval is an independent, synthetic-only evaluation driver. No product
// strategy or gold answer is added to production code.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/soaringjerry/PCAS/cmd/pcas-eval/fixture"
	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

type Report struct {
	EmbeddingPrecomputeTokens int                       `json:"embedding_precompute_tokens"`
	EmbeddingPrecomputeCost   float64                   `json:"embedding_precompute_cost_cny"`
	SchemaVersion             int                       `json:"schema_version"`
	Revision                  string                    `json:"revision"`
	CorpusSHA256              string                    `json:"corpus_sha256"`
	Anchor                    string                    `json:"anchor"`
	Fake                      bool                      `json:"fake_model"`
	Model                     string                    `json:"model"`
	VectorMode                string                    `json:"vector_mode"`
	EmbeddingModel            string                    `json:"embedding_model,omitempty"`
	NativeStructuredSchema    bool                      `json:"native_structured_schema"`
	TokenBound                string                    `json:"token_bound"`
	MaxInputTokens            int                       `json:"max_input_tokens"`
	Baseline                  fixture.Baseline          `json:"baseline"`
	Summaries                 []Summary                 `json:"summaries"`
	Answers                   []AnswerRow               `json:"answers"`
	Extraction                []fixture.ExtractionScore `json:"extraction"`
	ExtractionTotals          fixture.ExtractionScore   `json:"extraction_totals"`
	ExtractionInputTokens     int                       `json:"extraction_input_tokens"`
	ExtractionCost            float64                   `json:"extraction_cost_cny"`
	ExtractionMilliseconds    float64                   `json:"extraction_milliseconds"`
	Notes                     []string                  `json:"notes"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func envFloat(key string) (float64, error) {
	v := os.Getenv(key)
	if v == "" {
		return 0, nil
	}
	n, err := strconv.ParseFloat(v, 64)
	if err != nil || n < 0 || math.IsNaN(n) || math.IsInf(n, 0) {
		return 0, fmt.Errorf("invalid %s", key)
	}
	return n, nil
}
func run() error {
	dsn := flag.String("database-url", "", "empty local disposable PostgreSQL DSN (mandatory)")
	dir := flag.String("fixtures", "testdata/phase2/eval", "synthetic corpus directory")
	output := flag.String("output", "/tmp/pcas-eval", "output file prefix (.md and .json)")
	fakeEmbedding := flag.Bool("fake-embedding", false, "include deterministic synthetic vectors in fake mode")
	fake := flag.Bool("fake", false, "start an in-process synthetic model; no real model calls")
	anchorFlag := flag.String("anchor", "", "base date YYYY-MM-DD; defaults to today in corpus timezone")
	revision := flag.String("revision", "unknown", "evaluated git revision")
	top := flag.Int("top-k", 6, "pure vector/keyword baseline document count")
	maxInput := flag.Int("max-input-tokens", 0, "model input allowance, excluding output; conservative UTF-8 byte bound")
	flag.Parse()
	if *dsn == "" {
		return fmt.Errorf("eval_database_required: pass -database-url for an empty local temporary database")
	}
	if *top < 1 {
		return fmt.Errorf("top-k must be positive")
	}
	// Parse corpus timezone before expanding date placeholders.
	raw, err := os.ReadFile(filepath.Join(*dir, "corpus.json"))
	if err != nil {
		return err
	}
	var header struct{ Timezone string }
	if err := json.Unmarshal(raw, &header); err != nil {
		return err
	}
	loc, err := time.LoadLocation(header.Timezone)
	if err != nil {
		return err
	}
	anchor := time.Now().In(loc)
	anchor = time.Date(anchor.Year(), anchor.Month(), anchor.Day(), 0, 0, 0, 0, loc)
	if *anchorFlag != "" {
		anchor, err = time.ParseInLocation("2006-01-02", *anchorFlag, loc)
		if err != nil {
			return err
		}
	}
	c, baseline, err := fixture.Load(*dir, anchor)
	if err != nil {
		return err
	}
	observed := &fixture.Observer{}
	models := &ai.Registry{HTTP: &http.Client{Transport: observed, Timeout: 3 * time.Minute}, Config: ai.Configuration{Extraction: "eval"}}
	provider := ai.Provider{ID: "eval", Name: "回忆评测", Protocol: "openai", BaseURL: os.Getenv("PCAS_EVAL_BASE_URL"), Model: os.Getenv("PCAS_EVAL_MODEL"), KeyEnv: "PCAS_EVAL_API_KEY", MaxOutput: 4096}
	if *fake {
		server := fixture.FakeModel()
		defer server.Close()
		observed.Base = server.Client().Transport
		provider.BaseURL = server.URL
		provider.Model = "synthetic-reflector"
		provider.KeyEnv = ""
		provider.CostMode = "free"
		if *maxInput == 0 {
			*maxInput = 32000
		}
	} else {
		if provider.BaseURL == "" || provider.Model == "" || os.Getenv(provider.KeyEnv) == "" {
			return fmt.Errorf("eval_model_not_configured: set PCAS_EVAL_BASE_URL, PCAS_EVAL_MODEL and PCAS_EVAL_API_KEY")
		}
		if *maxInput <= 0 {
			return fmt.Errorf("eval_model_input_allowance_required: pass -max-input-tokens from the provider context limit minus output allowance")
		}
		provider.InputPerMillion, err = envFloat("PCAS_EVAL_INPUT_CNY_PER_MILLION")
		if err != nil {
			return err
		}
		provider.OutputPerMillion, err = envFloat("PCAS_EVAL_OUTPUT_CNY_PER_MILLION")
		if err != nil {
			return err
		}
		if provider.InputPerMillion+provider.OutputPerMillion == 0 {
			return fmt.Errorf("eval_model_prices_required: supply model prices to measure cost")
		}
	}
	if *maxInput <= 0 {
		return fmt.Errorf("max-input-tokens must be positive")
	}
	models.Config.Providers = []ai.Provider{provider}
	if *fakeEmbedding && !*fake {
		return fmt.Errorf("fake-embedding requires -fake")
	}
	vectorPrice, err := envFloat("PCAS_EVAL_EMBEDDING_INPUT_CNY_PER_MILLION")
	if err != nil {
		return err
	}
	vectorMode := "keyword-fallback"
	embeddingModel := ""
	if *fakeEmbedding || !*fake && os.Getenv("PCAS_EVAL_EMBEDDING_MODEL") != "" {
		embeddingModel = os.Getenv("PCAS_EVAL_EMBEDDING_MODEL")
		if *fakeEmbedding {
			embeddingModel = "synthetic-hash64"
			vectorPrice = 0
		} else if vectorPrice <= 0 {
			return fmt.Errorf("eval_embedding_price_required: set PCAS_EVAL_EMBEDDING_INPUT_CNY_PER_MILLION")
		}
		base := os.Getenv("PCAS_EVAL_EMBEDDING_BASE_URL")
		if base == "" {
			base = provider.BaseURL
		}
		keyEnv := "PCAS_EVAL_EMBEDDING_API_KEY"
		if os.Getenv(keyEnv) == "" {
			keyEnv = provider.KeyEnv
		}
		if *fakeEmbedding {
			base = provider.BaseURL
			keyEnv = ""
		}
		models.Config.Providers = append(models.Config.Providers, ai.Provider{ID: "eval-vector", Name: "评测向量", Protocol: "openai", BaseURL: base, Model: embeddingModel, KeyEnv: keyEnv, Embedding: true, CostMode: "free", InputPerMillion: vectorPrice, EmbeddingQueryPrefix: os.Getenv("PCAS_EVAL_EMBEDDING_QUERY_PREFIX")})
		models.Config.Embedding = "eval-vector"
		vectorMode = "cosine-vector"
	}
	ctx := context.Background()
	store, pool, cleanup, err := openTemporary(ctx, *dsn)
	if err != nil {
		return err
	}
	defer cleanup()
	store.SetModels(models)
	seeded, err := fixture.Seed(ctx, store, pool, c, anchor, "eval", true)
	if err != nil {
		return err
	}
	report := Report{SchemaVersion: 1, Revision: *revision, CorpusSHA256: fmt.Sprintf("%x", sha256.Sum256(raw)), Anchor: anchor.Format("2006-01-02"), Fake: *fake, Model: provider.Model, VectorMode: vectorMode, EmbeddingModel: embeddingModel, NativeStructuredSchema: seeded.NativeStructuredSchema, MaxInputTokens: *maxInput, TokenBound: "UTF-8 bytes <= input tokens allowance (conservative); provider usage used for measured token counts", Baseline: baseline, Notes: []string{
		"All answer methods use the same model and captured secretary system/framing. All gold facts are scoring-only.",
		"Cost is CNY from explicit per-million token prices; answer query embedding costs are included; shared corpus embedding costs are reported separately.",
		"Extraction uses the tool's R1 schema prompt, not the product extractor prompt; measures model extraction against frozen source gold.",
		"People/places use set micro-F1; time/nature use quote-aligned item accuracy, charging extra/missing items.",
		"String/alias answer matching does not check negation or reasoning; repeated-context fake results only test plumbing.",
	}}
	if *fake {
		report.Notes = append(report.Notes, "Fake input/output tokens are synthetic rune/4 counts; fake cost is zero and latency is local.")
	}
	if vectorMode == "keyword-fallback" {
		report.Notes = append(report.Notes, "No embedding model configured: vector-only is explicitly replaced with keyword overlap ranking.")
	}
	docs := c.Documents()
	var vectors []memory.Embedding
	if vectorMode == "cosine-vector" {
		// Precompute both pure vector documents and canonical product vectors using
		// the same embedding space. No vector reranking is used by the pure baseline.
		for _, d := range docs {
			v, err := models.Embed(ctx, []string{d.Text})
			if err != nil {
				return err
			}
			vectors = append(vectors, v[0])
		}
		rows, err := pool.Query(ctx, `SELECT t.id::text,t.version,t.body FROM memory_text t WHERE t.owner_id=$1 UNION ALL SELECT id::text,version,body FROM chunks WHERE owner_id=$1`, string(seeded.Scope.OwnerID))
		if err != nil {
			return err
		}
		type textRef struct {
			id      string
			version int
			text    string
		}
		var texts []textRef
		for rows.Next() {
			var t textRef
			if err := rows.Scan(&t.id, &t.version, &t.text); err != nil {
				rows.Close()
				return err
			}
			texts = append(texts, t)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, t := range texts {
			v, err := models.Embed(ctx, []string{t.text})
			if err != nil {
				return err
			}
			if _, err := pool.Exec(ctx, `INSERT INTO embeddings(owner_id,record_id,record_version,model,dimensions,embedding) VALUES($1,$2,$3,$4,$5,$6::vector) ON CONFLICT DO NOTHING`, string(seeded.Scope.OwnerID), t.id, t.version, v[0].Model, len(v[0].Values), string(encoded(v[0].Values))); err != nil {
				return err
			}
		}
	}
	for _, v := range observed.Embeddings() {
		report.EmbeddingPrecomputeTokens += v.InputTokens
	}
	report.EmbeddingPrecomputeCost = float64(report.EmbeddingPrecomputeTokens) * vectorPrice / 1e6
	for n, q := range c.Queries() {
		fmt.Fprintf(os.Stderr, "question %d/%d %s\n", n+1, len(c.Queries()), q.ID)
		beforeVector := len(observed.Embeddings())
		before := len(observed.Calls())
		start := time.Now()
		request := string(memory.NewID())
		out, err := store.DeskTurn(ctx, seeded.Scope, workspace.DeskTurnRequest{RequestID: request, AgentID: "eval", Text: q.Text})
		if err != nil {
			return fmt.Errorf("%s product secretary: %w", q.ID, err)
		}
		elapsed := time.Since(start)
		calls := observed.Calls()
		if len(calls) != before+1 {
			return fmt.Errorf("%s: product did not make exactly one model call", q.ID)
		}
		call := calls[before]
		result := ai.Result{InputTokens: call.InputTokens, OutputTokens: call.OutputTokens, Cost: (float64(call.InputTokens)*provider.InputPerMillion + float64(call.OutputTokens)*provider.OutputPerMillion) / 1e6}
		r := row(q, "product", out.Turn.Reply, result, elapsed)
		for _, v := range observed.Embeddings()[beforeVector:] {
			r.EmbeddingInputTokens += v.InputTokens
		}
		r.Cost += float64(r.EmbeddingInputTokens) * vectorPrice / 1e6
		received, err := fixture.Received(ctx, pool, seeded, request)
		if err != nil {
			return err
		}
		score := fixture.ScoreRetrievalIdentified(c, q, fixture.ContextSections(call.Prompt), received)
		r.Retrieval = &score
		report.Answers = append(report.Answers, r)
		for _, method := range []string{"all-context", "vector-only"} {
			start := time.Now()
			beforeVector := len(observed.Embeddings())
			selected := docs
			if method == "vector-only" {
				selected, err = rankDocuments(ctx, models, docs, vectors, q.Text, *top)
				if err != nil {
					return err
				}
			}
			empty, err := replaceContext(call.Prompt, "")
			if err != nil {
				return err
			}
			available := *maxInput - len(call.System) - len(empty)
			if available < 0 {
				return fmt.Errorf("eval_framing_exceeds_allowance: increase model input allowance")
			}
			context, truncated, full, dropped := boundedContext(selected, anchor, available)
			prompt, err := replaceContext(call.Prompt, context)
			if err != nil {
				return err
			}
			result, err := models.Generate(ctx, "eval", call.System, prompt)
			if err != nil {
				return fmt.Errorf("%s %s: model call failed", q.ID, method)
			}
			answer, err := factAnswer(result.Text)
			if err != nil {
				return err
			}
			r := row(q, method, answer, result, time.Since(start))
			for _, v := range observed.Embeddings()[beforeVector:] {
				r.EmbeddingInputTokens += v.InputTokens
			}
			r.Cost += float64(r.EmbeddingInputTokens) * vectorPrice / 1e6
			r.Truncated = truncated
			r.Documents = full
			r.DocumentsDropped = dropped
			report.Answers = append(report.Answers, r)
		}
	}
	// Independent schema-constrained extraction. Role-excluded messages are
	// evaluated as empty without sending them to the model, as contract R10 says.
	for n, d := range docs {
		fmt.Fprintf(os.Stderr, "extraction %d/%d %s\n", n+1, len(docs), d.ID)
		var pred fixture.Extraction
		if d.Role == "user" {
			prompt := string(encoded(map[string]any{"source": d.Text, "expressed_at": anchor.AddDate(0, 0, d.ExpressedDays).Add(9 * time.Hour).Format(time.RFC3339), "recorded_at": anchor.AddDate(0, 0, d.RecordedDays).Format(time.RFC3339), "timezone": c.Timezone, "role": d.Role}))
			start := time.Now()
			result, err := models.Generate(ctx, "eval", extractionSystem, prompt)
			if err != nil {
				return fmt.Errorf("%s extraction: model call failed", d.ID)
			}
			report.ExtractionMilliseconds += float64(time.Since(start)) / float64(time.Millisecond)
			report.ExtractionInputTokens += result.InputTokens
			report.ExtractionCost += result.Cost
			value := strings.TrimSpace(result.Text)
			value = strings.TrimPrefix(value, "```json")
			value = strings.TrimPrefix(value, "```")
			value = strings.TrimSuffix(value, "```")
			if err := json.Unmarshal([]byte(strings.TrimSpace(value)), &pred); err != nil {
				return fmt.Errorf("%s eval_extraction_invalid: expected items JSON", d.ID)
			}
		}
		score := fixture.CompareExtraction(d, pred.Items)
		report.Extraction = append(report.Extraction, score)
		addCounts(&report.ExtractionTotals.People, score.People)
		addCounts(&report.ExtractionTotals.Places, score.Places)
		addCounts(&report.ExtractionTotals.Time, score.Time)
		addCounts(&report.ExtractionTotals.Nature, score.Nature)
	}
	report.Summaries = summarize(report.Answers)
	if err := writeReport(*output, report); err != nil {
		return err
	}
	fmt.Printf("saved %s.md and %s.json\n", *output, *output)
	return nil
}
func encoded(v any) []byte                          { b, _ := json.Marshal(v); return b }
func addCounts(a *fixture.Counts, b fixture.Counts) { a.Correct += b.Correct; a.Total += b.Total }
func writeReport(prefix string, r Report) error {
	if err := os.MkdirAll(filepath.Dir(prefix), 0755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(prefix+".json", append(raw, '\n'), 0644); err != nil {
		return err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# PCAS synthetic recall evaluation\n\nRevision `%s`; anchor %s; model `%s`; fake=%t; corpus SHA256 `%s`.\n\nVector baseline: **%s**. Native structured schema: %t. Baseline %.4f (%s).\n\n%s; input allowance %d.\n\n| Method | Fact hits | Fact rate | Input tokens | Cost CNY | Total ms | Truncated queries |\n|---|---:|---:|---:|---:|---:|---:|\n", r.Revision, r.Anchor, r.Model, r.Fake, r.CorpusSHA256, r.VectorMode, r.NativeStructuredSchema, r.Baseline.MinimumRecall, r.Baseline.Status, r.TokenBound, r.MaxInputTokens)
	for _, s := range r.Summaries {
		fmt.Fprintf(&b, "| %s | %d/%d | %.4f | %d | %.6f | %.2f | %d |\n", s.Method, s.FactsHit, s.FactsTotal, s.FactRate, s.InputTokens, s.Cost, s.Milliseconds, s.TruncatedQueries)
	}
	fmt.Fprintf(&b, "\nShared embedding precompute: %d input tokens, CNY %.6f. Answer costs include query embeddings.\n", r.EmbeddingPrecomputeTokens, r.EmbeddingPrecomputeCost)
	e := r.ExtractionTotals
	fmt.Fprintf(&b, "\n| Extraction dimension | Correct/total | Accuracy |\n|---|---:|---:|\n")
	for _, d := range []struct {
		name string
		c    fixture.Counts
	}{{"People (micro-F1)", e.People}, {"Places (micro-F1)", e.Places}, {"Time", e.Time}, {"Nature", e.Nature}} {
		fmt.Fprintf(&b, "| %s | %d/%d | %.4f |\n", d.name, d.c.Correct, d.c.Total, d.c.Accuracy())
	}
	fmt.Fprintf(&b, "\nExtraction input tokens %d; cost CNY %.6f; total ms %.2f.\n\n| Question | Method | Fact rate | Input tokens | Cost CNY | ms | Truncated |\n|---|---|---:|---:|---:|---:|---|\n", r.ExtractionInputTokens, r.ExtractionCost, r.ExtractionMilliseconds)
	for _, row := range r.Answers {
		fmt.Fprintf(&b, "| %s | %s | %.4f | %d | %.6f | %.2f | %t |\n", row.Query, row.Method, row.FactRate, row.InputTokens, row.Cost, row.Milliseconds, row.Truncated)
	}
	b.WriteString("\nNotes:\n\n")
	for _, note := range r.Notes {
		fmt.Fprintf(&b, "- %s\n", note)
	}
	return os.WriteFile(prefix+".md", []byte(b.String()), 0644)
}
