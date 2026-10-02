package postgres

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/soaringjerry/PCAS/cmd/pcas-eval/fixture"
	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/testsupport"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func TestPhase2EvalRetrieval(t *testing.T) {
	anchor := testsupport.DateFromToday(t, "Asia/Shanghai", 0, 0, 0)
	c, baseline, err := fixture.Load("../../testdata/phase2/eval", anchor)
	if err != nil {
		t.Fatal(err)
	}
	for _, gold := range []bool{false, true} {
		name := "raw-only"
		if gold {
			name = "gold-structured"
		}
		t.Run(name, func(t *testing.T) {
			s := testStore(t)
			server := fixture.FakeModel()
			t.Cleanup(server.Close)
			observed := &fixture.Observer{Base: server.Client().Transport}
			s.SetModels(&ai.Registry{HTTP: &http.Client{Transport: observed}, Config: ai.Configuration{Extraction: "eval", Providers: []ai.Provider{{ID: "eval", Name: "评测假模型", Protocol: "openai", BaseURL: server.URL, Model: "synthetic-reflector", MaxOutput: 4096, CostMode: "free"}}}})
			seeded, err := fixture.Seed(context.Background(), s, s.pool, c, anchor, "eval", gold)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("native structured migration: %t; baseline status: %s", seeded.NativeStructuredSchema, baseline.Status)
			required, delivered, pairs, inversions := 0, 0, 0, 0
			t.Log("| question | delivered/required | recall | distractor pairs | interference |\n|---|---:|---:|---:|---:|")
			for _, q := range c.Queries() {
				before := len(observed.Calls())
				request := string(memory.NewID())
				_, err := s.DeskTurn(context.Background(), seeded.Scope, workspace.DeskTurnRequest{RequestID: request, AgentID: "eval", Text: q.Text})
				if err != nil {
					t.Fatalf("%s secretary entry: %v", q.ID, err)
				}
				calls := observed.Calls()
				if len(calls) != before+1 {
					t.Fatalf("%s: expected one real HTTP call, got %d", q.ID, len(calls)-before)
				}
				sentContext := fixture.ContextSections(calls[before].Prompt)
				if sentContext == "" {
					t.Fatal("secretary context headings changed; evaluator needs explicit adaptation")
				}
				received, err := fixture.Received(context.Background(), s.pool, seeded, request)
				if err != nil {
					t.Fatal(err)
				}
				score := fixture.ScoreRetrievalIdentified(c, q, sentContext, received)
				required += score.Required
				delivered += score.Delivered
				pairs += score.Pairs
				inversions += score.Inversions
				t.Logf("| %s | %d/%d | %.4f | %d/%d | %.4f |", q.ID, score.Delivered, score.Required, score.Recall, score.Inversions, score.Pairs, score.Interference)
			}
			recall := float64(delivered) / float64(required)
			interference := float64(inversions) / float64(pairs)
			t.Logf("%s TOTAL recall=%s interference=%s evidence=%d/%d inversions=%d/%d", name, fmt.Sprintf("%.6f", recall), fmt.Sprintf("%.6f", interference), delivered, required, inversions, pairs)
			// Raw-only is a diagnostic comparator; the ratcheted floor applies to the
			// product supplied with canonical gold extraction, never to raw-only.
			if gold && recall < baseline.MinimumRecall {
				t.Fatalf("recall %.6f below approved baseline %.6f", recall, baseline.MinimumRecall)
			}
		})
	}
}
