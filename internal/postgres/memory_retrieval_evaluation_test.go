package postgres

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/cmd/pcas-eval/fixture"
	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/testsupport"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func TestMemoryRetrievalEvaluation(t *testing.T) {
	anchor := testsupport.DateFromToday(t, "Asia/Shanghai", 0, 0, 0)
	full := os.Getenv("PCAS_EVAL_FULL") == "1"
	for _, tier := range []string{"basic", "hard"} {
		c, baseline, err := fixture.LoadTier("../../testdata/phase2/eval", tier, anchor)
		if err != nil {
			t.Fatal(err)
		}
		queries := c.Queries()
		if tier == "hard" && !full {
			cfg, err := fixture.LoadHardConfig("../../testdata/phase2/eval")
			if err != nil {
				t.Fatal(err)
			}
			queries = fixture.CIQueries(c, cfg.CICasesPerType)
		}
		t.Run(tier, func(t *testing.T) {
			// Keep both original basic diagnostics. Hard CI evaluates the canonical
			// gold data once; --mode retrieval --raw-only provides a manual comparator.
			modes := []bool{true}
			if tier == "basic" {
				modes = []bool{false, true}
			}
			for _, gold := range modes {
				name := "raw-only"
				if gold {
					name = "gold-structured"
				}
				t.Run(name, func(t *testing.T) { evalRetrieval(t, c, baseline, tier, anchor, gold, queries) })
			}
		})
	}
}
func evalRetrieval(t *testing.T, c fixture.Corpus, baseline fixture.Baseline, tier string, anchor time.Time, gold bool, queries []fixture.Query) {
	start := time.Now()
	s := testStore(t)
	server := fixture.FakeModel()
	t.Cleanup(server.Close)
	observed := &fixture.Observer{Base: server.Client().Transport}
	s.SetModels(&ai.Registry{HTTP: &http.Client{Transport: observed}, Config: ai.Configuration{Extraction: "eval", Providers: []ai.Provider{{ID: "eval", Name: "评测假模型", Protocol: "openai", BaseURL: server.URL, Model: "synthetic-reflector", MaxOutput: 4096, CostMode: "free"}}}})
	useSyntheticGateway(s)
	seeded, err := fixture.Seed(context.Background(), s, s.pool, c, anchor, "eval", gold)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("tier=%s native structured migration=%t baseline=%s docs=%d queries=%d/%d seed+setup=%s", tier, seeded.NativeStructuredSchema, baseline.Status, len(c.Documents()), len(queries), len(c.Queries()), time.Since(start))
	t.Log("| question | type | delivered/required | recall | distractor pairs | interference | memories/sources sent |\n|---|---|---:|---:|---:|---:|---:|")
	var scores []fixture.RetrievalScore
	for _, q := range queries {
		before := len(observed.Calls())
		request := string(memory.NewID())
		_, err := s.DeskTurn(context.Background(), seeded.Scope, workspace.DeskTurnRequest{RequestID: request, AgentID: "eval", Text: q.Text})
		if err != nil {
			t.Fatalf("%s secretary entry: %v", q.ID, err)
		}
		calls := observed.Calls()
		if len(calls) != before+1 {
			t.Fatalf("%s: expected one HTTP call, got %d", q.ID, len(calls)-before)
		}
		sentContext := fixture.ContextSections(calls[before].Prompt)
		if sentContext == "" {
			t.Fatal("secretary context headings changed; evaluator needs adaptation")
		}
		received, err := fixture.Received(context.Background(), s.pool, seeded, request)
		if err != nil {
			t.Fatal(err)
		}
		score := fixture.ScoreRetrievalIdentified(c, q, sentContext, received)
		scores = append(scores, score)
		t.Logf("| %s | %s | %d/%d | %.4f | %d/%d | %.4f | %d/%d |", q.ID, fixture.QueryType(c, q), score.Delivered, score.Required, score.Recall, score.Inversions, score.Pairs, score.Interference, score.MemoriesSent, score.SourcesSent)
	}
	for _, g := range fixture.AggregateRetrieval(tier, c, scores) {
		t.Logf("%s TYPE=%s queries=%d recall=%.6f interference=%.6f evidence=%d/%d inversions=%d/%d elapsed=%s", tier, g.Type, g.Queries, g.Recall, g.Interference, g.Delivered, g.Required, g.Inversions, g.Pairs, time.Since(start))
		if g.Type == "all" && gold && g.Recall < baseline.MinimumRecall {
			t.Fatalf("%s recall %s below approved baseline %.6f", tier, fmt.Sprintf("%.6f", g.Recall), baseline.MinimumRecall)
		}
	}
}
