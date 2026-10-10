package postgres

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/soaringjerry/PCAS/internal/worker"
)

func TestHourlyLimitDeferralLeavesNoModelCallRecord(t *testing.T) {
	s, scope, ctx := testStore(t), owner(), context.Background()
	var calls atomic.Int32
	secretaryModel(t, s, func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); secretaryModelReply(w, "unused") })
	organizeTestMemory(t, s, scope, "Fictitious unfinished report.")
	j := organizeTestJob(t, s, scope)
	if _, err := s.pool.Exec(ctx, `INSERT INTO background_usage(id,owner_id,reserved_cost,stage) SELECT gen_random_uuid(),$1,0,$2 FROM generate_series(1,$3)`, string(scope.OwnerID), OrganizeStage, backgroundHourlyBudgets[OrganizeStage]); err != nil {
		t.Fatal(err)
	}
	_, err := s.generatePaid(ctx, j, "organize", organizeInstructions, asJSON(map[string]string{"test": "fictitious"}), nil)
	var deferred *worker.JobError
	if !errors.As(err, &deferred) || deferred.Code != "organize_hourly_limit" || !deferred.NoAttempt || deferred.Until.IsZero() || calls.Load() != 0 {
		t.Fatal(err, calls.Load())
	}
	var records int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM model_calls WHERE owner_id=$1`, string(scope.OwnerID)).Scan(&records); err != nil || records != 0 {
		t.Fatal("a deferral was recorded as a model call", records, err)
	}
}
