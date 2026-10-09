package postgres

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/modelcall"
	"github.com/soaringjerry/PCAS/internal/prompts"
)

func TestUnsupportedRequiredSchemaHasCallRecordWithoutSpending(t *testing.T) {
	s, scope := testStore(t), owner()
	var invocations atomic.Int32
	secretaryModel(t, s, func(http.ResponseWriter, *http.Request) { invocations.Add(1) })
	organizeTestMemory(t, s, scope, "Fictitious unfinished report.")
	job := organizeTestJob(t, s, scope)
	request := modelcall.Request{OwnerID: scope.OwnerID, ExecutionID: job.ID, RootExecutionID: job.ID, Function: "organize", Stage: job.Stage, ProviderID: s.models.ExtractionID(), Instructions: prompts.Must("organize"), Schema: prompts.MustSchema("use-groups"), ContextBuilderVersion: "fictitious-context-v1", Prompt: asJSON(map[string]string{"rawPrompt": "Fictitious input."}), Policy: job}
	ctx := context.Background()
	if _, err := s.calls.Call(ctx, request); !errors.Is(err, ai.ErrUnsupportedCapability) {
		t.Fatal("unsupported schema did not fail explicitly", err)
	}
	var outcome, code, accounting, schemaName, schemaHash, builder string
	var capabilities []string
	var actualMode map[string]any
	var reservations, usages int
	err := s.pool.QueryRow(ctx, `SELECT outcome,error_code,accounting_state,schema_name,schema_hash,context_builder_version,required_capabilities,actual_mode,
 (SELECT count(*) FROM background_usage WHERE owner_id=$1),
 (SELECT count(*) FROM model_usage WHERE owner_id=$1)
 FROM model_calls WHERE owner_id=$1 AND execution_id=$2`, string(scope.OwnerID), string(job.ID)).Scan(&outcome, &code, &accounting, &schemaName, &schemaHash, &builder, &capabilities, &actualMode, &reservations, &usages)
	if err != nil || outcome != "failed" || code != ai.ErrUnsupportedCapability.Error() || accounting != "not_reserved" || schemaName != request.Schema.Name() || schemaHash != request.Schema.Hash() || builder != request.ContextBuilderVersion || !reflect.DeepEqual(capabilities, request.RequiredCapabilities()) || reservations != 0 || usages != 0 || invocations.Load() != 0 || actualMode["schema"] != "none" || actualMode["unsupportedCapability"] != "output_schema" {
		t.Fatal(outcome, code, accounting, schemaName, schemaHash, builder, capabilities, reservations, usages, invocations.Load(), actualMode, err)
	}
}

func TestSelectedCodexCallRecordsRegisteredModeAndExactUsageLink(t *testing.T) {
	s, scope := testStore(t), owner()
	// The shared memory fixture grants its existing "model" principal.
	secretaryModel(t, s, func(http.ResponseWriter, *http.Request) { t.Error("unexpected fixture model call") })
	organizeTestMemory(t, s, scope, "Fictitious unfinished report.")
	job := organizeTestJob(t, s, scope)
	dir := secretaryRetryCodex(t, s, 0, 0, "")
	request := modelcall.Request{OwnerID: scope.OwnerID, ExecutionID: job.ID, RootExecutionID: job.ID, Function: "organize", Stage: job.Stage, ProviderID: "chatgpt", Instructions: prompts.Must("organize"), Schema: prompts.MustSchema("secretary-output"), Search: true, ContextBuilderVersion: "fictitious-context-v1", Prompt: asJSON(map[string]string{"rawPrompt": "Fictitious input."}), Policy: job}
	ctx := context.Background()
	paid, err := s.calls.Call(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	var outcome, accounting, schemaHash, model string
	var mode map[string]any
	var usageCount int
	if err := s.pool.QueryRow(ctx, `SELECT outcome,accounting_state,schema_hash,model,actual_mode,
 (SELECT count(*) FROM model_usage u WHERE u.owner_id=c.owner_id AND u.id=c.usage_id AND u.id=c.reservation_id)
 FROM model_calls c WHERE owner_id=$1 AND id=$2`, string(scope.OwnerID), string(paid.InvocationID)).Scan(&outcome, &accounting, &schemaHash, &model, &mode, &usageCount); err != nil {
		t.Fatal(err)
	}
	if outcome != "returned" || accounting != "settled" || schemaHash != request.Schema.Hash() || model != paid.Model || mode["search"] != true || mode["schema"] != request.Schema.Name() || mode["output"] != "structured" || usageCount != 1 || len(secretaryRetryCalls(t, dir)) != 1 {
		t.Fatal(outcome, accounting, schemaHash, mode, usageCount)
	}
	// Search evidence must survive restart as part of the original result.
	paid.Searches = []string{"Fictitious public query."}
	if err := (backgroundCalls{store: s}).Save(ctx, request, paid); err != nil {
		t.Fatal(err)
	}
	peer, err := Open(ctx, s.pool.Config().ConnConfig.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	recovered, err := peer.paidModelResult(ctx, job)
	if err != nil || recovered == nil || !reflect.DeepEqual(recovered.Searches, paid.Searches) || recovered.InvocationID != paid.InvocationID || recovered.Reservation != paid.Reservation {
		t.Fatal("saved search evidence did not survive restart", recovered, err)
	}
}
