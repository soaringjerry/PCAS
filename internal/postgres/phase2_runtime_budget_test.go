package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
)

func TestPhase2RuntimeFinalUTF8EstimatedInputBudgetBoundary(t *testing.T) {
	var gold struct {
		Budget struct {
			TotalTokens int `json:"total_input_tokens"`
			Cases       []struct {
				Bytes     int  `json:"payload_bytes"`
				Estimated int  `json:"estimated_tokens"`
				Allowed   bool `json:"allowed"`
			} `json:"cases"`
		} `json:"input_budget_control"`
	}
	phase2RTReadJSON(t, "runtime-sequences.json", &gold)
	if len(gold.Budget.Cases) != 3 || gold.Budget.TotalTokens != 8000 {
		t.Fatal("independent estimated budget gold shape changed")
	}
	for _, boundary := range gold.Budget.Cases {
		name := "allowed"
		if !boundary.Allowed {
			name = "refused"
		}
		t.Run(fmt.Sprintf("%s_%d_bytes", name, boundary.Bytes), func(t *testing.T) {
			s, scope, capture := phase2RTSetup(t)
			ctx := context.Background()
			const system = "合成预算固定说明"
			const probe = "中文\n引号\"反斜线\\和<&>。"
			// A single actual adapter probe measures encoding overhead. It is
			// fixture calibration, not a retry of the bounded generation. No copy
			// of provider serialization or tokenizer is maintained in this test.
			if _, err := capture.Registry.Generate(ctx, "phase2-model", system, probe); err != nil {
				t.Fatal("synthetic wire calibration failed", err)
			}
			calibration := capture.request(t, 0)
			padding := boundary.Bytes - len(calibration)
			if padding < 0 {
				t.Fatal("independent final-byte boundary shorter than observed fixed request")
			}
			prompt := probe + strings.Repeat("x", padding)
			var task memory.TrustedTaskContext
			if err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
				var err error
				task, err = s.trustedTaskContextTx(ctx, tx, scope, "phase2-model", "secretary", phase2RTUnscoped(), nil)
				return err
			}); err != nil {
				t.Fatal("actual server task construction", err)
			}
			if task.TotalInputTokens != gold.Budget.TotalTokens {
				t.Fatal("actual server task total input default differs from frozen 8000", task.TotalInputTokens)
			}
			operation := string(memory.NewID())
			before := capture.count()
			_, attempt, err := s.generateContext(ctx, scope, operation, task, nil, nil, nil, nil, system, prompt, nil)
			delta := capture.count() - before
			if !boundary.Allowed {
				if !errors.Is(err, memory.ErrRecordCapacity) || delta != 0 {
					t.Errorf("estimated overbudget error=%v actual target HTTP=%d", err, delta)
				}
			} else {
				if err != nil || delta != 1 {
					t.Fatalf("at/below budget failed: error=%v target HTTP=%d", err, delta)
				}
				actual := capture.request(t, before)
				if len(actual) != boundary.Bytes {
					t.Errorf("actual calibrated UTF8 bytes=%d want=%d", len(actual), boundary.Bytes)
				}
				if attempt.Manifest.InputTokens.Method != "estimated" || attempt.Manifest.InputTokens.Value == nil || *attempt.Manifest.InputTokens.Value != boundary.Estimated || attempt.Manifest.InputBytes != boundary.Bytes {
					t.Error("final actual bytes disagree with independent estimated token gold or are falsely marked actual")
				}
				if attempt.Manifest.InputTokens.Model != task.Recipient.Model {
					t.Error("estimated count lost actual model identity")
				}
				actualSnapshot, err := s.ContextAttemptSnapshot(ctx, scope, attempt.ID)
				if err != nil || string(actualSnapshot) != string(actual) {
					t.Error("estimated budget changed exact final payload retention", err)
				}
			}
			phase2RTEvidence(t, "estimated-budget", map[string]any{"final_payload_bytes": boundary.Bytes, "expected_estimated_tokens": boundary.Estimated, "total_input_tokens": task.TotalInputTokens, "target_provider_requests": delta, "calibration_provider_requests": 1, "error": err != nil, "attempt": attempt, "count_semantics": "ceil(final UTF8 bytes/3), estimated; not tokenizer/hidden context/billing"})
		})
	}
}
