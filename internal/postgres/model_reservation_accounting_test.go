package postgres

import (
	"context"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
	"math"
	"strings"
	"testing"
	"time"
)

func secretaryCancellationHold(t *testing.T, s *Store, scope memory.Scope, requestID string) (float64, float64) {
	t.Helper()
	var retainedEstimate float64
	// A canceled submitted call has unknown actual usage. Wait for its
	// accounting receipt, then check the hold by invocation identity.
	waitCtx, stop := context.WithTimeout(t.Context(), 5*time.Second)
	defer stop()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		var held bool
		var partialCost float64
		err := s.pool.QueryRow(waitCtx, `SELECT c.outcome='unknown' AND c.accounting_state='held'
 AND NOT (c.actual_mode->>'usageComplete')::boolean AND c.usage_id=c.reservation_id
 AND EXISTS(SELECT 1 FROM model_usage u WHERE u.owner_id=c.owner_id AND u.id=c.usage_id),
 (c.actual_mode->>'reservationEstimate')::double precision,
 (SELECT u.cost FROM model_usage u WHERE u.owner_id=c.owner_id AND u.id=c.usage_id)
 FROM model_calls c WHERE c.owner_id=$1 AND c.execution_id=$2 AND c.stage='answer'`, string(scope.OwnerID), requestID).Scan(&held, &retainedEstimate, &partialCost)
		if err == nil && held {
			return retainedEstimate, partialCost
		}
		select {
		case <-ticker.C:
		case <-waitCtx.Done():
			t.Fatal("canceled invocation lost its original accounting hold", err)
		}
	}
}

func TestSecretarySettlesOnlyItsReservation(t *testing.T) {
	for _, mode := range []string{"success", "invalid", "failure", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			s, scope := testStore(t), owner()
			f := b4Model(t, s)
			f.set(`{"reply":"R1 reply","actions":[]}`, 200)
			if mode == "invalid" {
				f.set("R1 plain reply", 200)
			}
			if mode == "failure" {
				f.set("", 500)
			}
			workspaceCommand(t, s, scope, workspace.Command{Type: "updateSettings", Patch: asJSON(map[string]any{"dailyBudget": 10})})
			// Another reservation must survive settlement of this call unchanged.
			if _, err := s.pool.Exec(context.Background(), "INSERT INTO background_usage(owner_id,reserved_cost) VALUES($1,0.03)", string(scope.OwnerID)); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var gate *b4ModelGate
			var firstReservation float64
			if mode == "cancel" || mode == "success" {
				gate = b4HoldModel(t, f)
				defer gate.unblock()
			}
			done := make(chan error, 1)
			request := turnRequest("R1 budget question")
			go func() { _, err := s.DeskTurn(ctx, scope, request); done <- err }()
			if gate != nil {
				select {
				case <-gate.started:
					if mode == "cancel" {
						cancel()
					} else {
						if err := s.pool.QueryRow(context.Background(), "SELECT sum(reserved_cost)-0.03 FROM background_usage WHERE owner_id=$1", string(scope.OwnerID)).Scan(&firstReservation); err != nil {
							t.Fatal(err)
						}
						gate.unblock()
					}
				case <-time.After(10 * time.Second):
					t.Fatal("model not started")
				}
				// Only cancellation withholds the response until the invocation ends.
			}
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(60 * time.Second):
				t.Fatal("secretary did not finish")
			}
			var retainedEstimate float64
			if mode == "cancel" {
				retainedEstimate, _ = secretaryCancellationHold(t, s, scope, request.RequestID)
			}
			var reserved float64
			if err := s.pool.QueryRow(context.Background(), "SELECT sum(reserved_cost) FROM background_usage WHERE owner_id=$1", string(scope.OwnerID)).Scan(&reserved); err != nil {
				t.Fatal(err)
			}
			want := 0.03
			{
				rows := b4Usage(t, s, scope)
				if len(rows) != 1 {
					t.Fatal(rows)
				}
				if mode == "cancel" {
					if retainedEstimate < rows[0].Cost {
						t.Fatal("held estimate fell below recorded partial usage", retainedEstimate, rows)
					}
					want += retainedEstimate
				} else {
					want += rows[0].Cost
				}
				if rows[0].InputTokens <= 0 || rows[0].Cost <= 0 {
					t.Fatal("submitted secretary call lacks accounted input usage", rows)
				}
				if mode == "failure" || mode == "cancel" {
					r1RequireEstimatedInput(t, s, scope)
				}
			}
			if math.Abs(reserved-want) > 1e-9 {
				t.Errorf("budget reserved=%g want applicable settlement=%g", reserved, want)
			}
			// A new request under the remaining budget must actually reach the provider.
			if mode == "success" {
				actual := want - 0.03
				if firstReservation <= actual {
					t.Fatal("fixture needs a reservation above actual usage", firstReservation, actual)
				}
				// C1 expanded the prompt: the old fixed 0.07 also blocked a
				// correctly settled call. Keep half the released reservation as
				// headroom for the next turn's history, while leaving insufficient
				// budget if the first invocation's full reservation were retained.
				nextBudget := want + firstReservation + (firstReservation-actual)/2
				t.Logf("pending first reservation=%g settled=%g next budget=%g", firstReservation, actual, nextBudget)
				workspaceCommand(t, s, scope, workspace.Command{Type: "updateSettings", Patch: asJSON(map[string]any{"dailyBudget": nextBudget})})
				before := len(f.all())
				mustTurn(t, s, scope, turnRequest("R1 next question"))
				if len(f.all()) != before+1 {
					t.Error("settled budget blocked next call")
				}
			}
		})
	}
}

func TestFailedInvocationsUseApplicableBudgetSettlement(t *testing.T) {
	for _, kind := range []string{"deputy", "extraction", "invalid-extraction", "cancel-deputy", "cancel-extraction"} {
		t.Run(kind, func(t *testing.T) {
			s, scope := testStore(t), owner()
			f := b4Model(t, s)
			f.set("", 500)
			if kind == "invalid-extraction" {
				f.set("R1 invalid extraction", 200)
			}
			if _, err := s.Snapshot(context.Background(), scope); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var call func() error
			if strings.Contains(kind, "deputy") {
				st := workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "Failure"})
				workspaceCommand(t, s, scope, workspace.Command{Type: "requestRun", ThingID: st.Tasks[0].ID, AgentID: "model", Kind: "draft", Prompt: "Fail"})
				call = func() error { return s.runAgentOnce(ctx) }
			} else {
				source := mustIngest(t, s, scope, memory.IngestRequest{Connector: "manual", ExternalID: "r1", ExternalVersion: "1", Title: "R1", Text: "R1 source", MediaType: "text/plain"})
				job := leaseStage(t, s, scope, source.Ref, "source.extract")
				call = func() error { return s.ProcessExtraction(ctx, job) }
			}
			done := make(chan error, 1)
			var gate *b4ModelGate
			var originalReservation float64
			if strings.HasPrefix(kind, "cancel-") {
				gate = b4HoldModel(t, f)
				defer gate.unblock()
			}
			go func() { done <- call() }()
			if gate != nil {
				select {
				case <-gate.started:
					if err := s.pool.QueryRow(context.Background(), `SELECT coalesce((SELECT sum(reserved_cost) FROM agent_runs WHERE owner_id=$1),0)+coalesce((SELECT sum(reserved_cost) FROM background_usage WHERE owner_id=$1),0)`, string(scope.OwnerID)).Scan(&originalReservation); err != nil {
						t.Fatal(err)
					}
					cancel()
				case <-time.After(10 * time.Second):
					t.Fatal("model not started")
				}
				// Withhold the response until the canceled invocation ends.
			}
			select {
			case err := <-done:
				if kind == "deputy" && err != nil {
					t.Fatal(err)
				}
			case <-time.After(60 * time.Second):
				t.Fatal("call did not end")
			}
			var reserved float64
			if err := s.pool.QueryRow(context.Background(), "SELECT coalesce((SELECT sum(reserved_cost) FROM agent_runs WHERE owner_id=$1),0)+coalesce((SELECT sum(reserved_cost) FROM background_usage WHERE owner_id=$1),0)", string(scope.OwnerID)).Scan(&reserved); err != nil {
				t.Fatal(err)
			}
			want := 0.0
			{
				rows := b4Usage(t, s, scope)
				if len(rows) != 1 {
					t.Fatal(rows)
				}
				want = rows[0].Cost
				if rows[0].InputTokens <= 0 || want <= 0 {
					t.Fatal("submitted call lacks accounted input usage", rows)
				}
				if kind != "invalid-extraction" {
					r1RequireEstimatedInput(t, s, scope)
				}
			}
			if strings.HasPrefix(kind, "cancel-") {
				// Partial usage cannot release the unobserved reservation.
				// Deputy keeps its admitted hold; extraction keeps its own hold.
				if originalReservation <= want {
					t.Fatal("fixture must distinguish unknown budget from partial usage", originalReservation, want)
				}
				want = originalReservation
				var outcome, accounting string
				var complete bool
				if err := s.pool.QueryRow(context.Background(), `SELECT outcome,accounting_state,(actual_mode->>'usageComplete')::boolean FROM model_calls WHERE owner_id=$1`, string(scope.OwnerID)).Scan(&outcome, &accounting, &complete); err != nil || outcome != "unknown" || accounting != "held" || complete {
					t.Fatal("cancellation must retain visible unknown accounting", outcome, accounting, complete, err)
				}
			}
			if math.Abs(reserved-want) > 1e-9 {
				t.Errorf("finished %s reserved=%g want=%g", kind, reserved, want)
			}
		})
	}
}

func r1RequireEstimatedInput(t *testing.T, s *Store, scope memory.Scope) {
	t.Helper()
	var estimated bool
	if err := s.pool.QueryRow(context.Background(), `SELECT input_estimated FROM model_usage WHERE owner_id=$1`, scope.OwnerID).Scan(&estimated); err != nil || !estimated {
		t.Fatal("missing estimated input accounting for a submitted failed call", estimated, err)
	}
}

func TestConcurrentSecretaryReservationsAreIndependent(t *testing.T) {
	s, scope := testStore(t), owner()
	f := b4Model(t, s)
	f.set(`{"reply":"R1 returned reply","actions":[]}`, 200)
	workspaceCommand(t, s, scope, workspace.Command{Type: "updateSettings", Patch: asJSON(map[string]any{"dailyBudget": 10})})
	gate := b4HoldModel(t, f)
	defer gate.unblock()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	request := turnRequest("R1 first conversation")
	go func() { _, err := s.DeskTurn(ctx, scope, request); done <- err }()
	select {
	case <-gate.started:
	case <-time.After(10 * time.Second):
		t.Fatal("first call not started")
	}
	f.mu.Lock()
	f.gate = nil
	f.mu.Unlock()
	second := mustTurn(t, s, scope, turnRequest("R1 second conversation"))
	if second.Turn.Reply == "" {
		t.Fatal("second call did not return")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("first call did not stop")
	}
	estimate, partial := secretaryCancellationHold(t, s, scope, request.RequestID)
	rows := b4Usage(t, s, scope)
	if len(rows) != 2 {
		t.Fatal("concurrent invocations lost distinct usage receipts", rows)
	}
	want := estimate - partial
	for _, row := range rows {
		want += row.Cost
	}
	var cost float64
	if err := s.pool.QueryRow(context.Background(), "SELECT sum(reserved_cost) FROM background_usage WHERE owner_id=$1", string(scope.OwnerID)).Scan(&cost); err != nil {
		t.Fatal(err)
	}
	if math.Abs(cost-want) > 1e-9 {
		t.Errorf("concurrent calls changed each other's settlement: budget=%g usage=%g", cost, want)
	}
}

func TestDeletedQueuedRunBudgetIsOwnerScoped(t *testing.T) {
	s := testStore(t)
	f := b4Model(t, s)
	sharedRunID := string(memory.NewID())
	firstOwner, secondOwner := owner(), owner()
	for _, scope := range []memory.Scope{firstOwner, secondOwner, firstOwner} {
		source := b1Source(t, s, scope, "R1 queued reference", "R1 queued reference", "manual")
		claim := b1Claim(t, s, scope, "R1 queued constraint", "preference", "confirmed", source)
		state := workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "R1 queued task"})
		state = workspaceCommand(t, s, scope, workspace.Command{Type: "requestRun", ID: sharedRunID, ThingID: state.Tasks[0].ID, AgentID: "model", Kind: "draft", Prompt: "Write"})
		b1HasRef(t, state.Runs[0].ContextVersions, claim, true)
		state = workspaceCommand(t, s, scope, workspace.Command{Type: "deleteMemory", ID: string(claim.ID)})
		if len(state.Runs) != 0 || state.BudgetUsage != 0 {
			t.Errorf("deleted queued run retained reservation: runs=%d budget=%g", len(state.Runs), state.BudgetUsage)
		}
	}
	if len(f.all()) != 0 {
		t.Error("queued cancellation must not call the model")
	}
}

func TestDeputySettlesReturnedUsageAfterLeaseLoss(t *testing.T) {
	s, scope := testStore(t), owner()
	f := b4Model(t, s)
	f.set("R1 paid result", 200)
	state := workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "R1 lease loss"})
	state = workspaceCommand(t, s, scope, workspace.Command{Type: "requestRun", ThingID: state.Tasks[0].ID, AgentID: "model", Kind: "draft", Prompt: "Write"})
	run := state.Runs[0]
	gate := b4HoldModel(t, f)
	defer gate.unblock()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.runAgentOnce(ctx) }()
	select {
	case <-gate.started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	_, err := s.pool.Exec(context.Background(), "UPDATE agent_runs SET status='failed',lease_token=NULL,lease_until=NULL,document=jsonb_set(document,'{status}','\"failed\"') WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	gate.unblock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	state, err = s.Snapshot(context.Background(), scope)
	if err != nil {
		t.Fatal(err)
	}
	rows := b4Usage(t, s, scope)
	if len(rows) != 1 {
		t.Fatal("returned usage lost", rows)
	}
	if math.Abs(state.BudgetUsage-rows[0].Cost) > 1e-9 {
		t.Errorf("lost result lease retained maximum reservation: budget=%g actual=%g", state.BudgetUsage, rows[0].Cost)
	}
	if state.Runs[0].Status != "failed" || state.Runs[0].Output != "" {
		t.Error("billing resurrected failed result")
	}
}

func TestUnavailableRetryKeepsPreviouslyReturnedCost(t *testing.T) {
	s, scope := testStore(t), owner()
	f := b4Model(t, s)
	f.set("R1 invalid returned extraction", 200)
	source := mustIngest(t, s, scope, memory.IngestRequest{Connector: "manual", ExternalID: "r1-retry", ExternalVersion: "1", Title: "R1 retry", Text: "R1 retry source", MediaType: "text/plain"})
	job := leaseStage(t, s, scope, source.Ref, "source.extract")
	if err := s.ProcessExtraction(context.Background(), job); err == nil {
		t.Fatal("expected invalid returned extraction")
	}
	usage := b4Usage(t, s, scope)
	if len(usage) != 1 {
		t.Fatal(usage)
	}
	// An explicit retry can reserve a new call. If it is unavailable before
	// submitting anything, only that new reservation may be removed.
	reservationID, err := s.reserveModelCostID(context.Background(), scope.OwnerID, 0.01, &job)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.releaseUnavailableReservation(context.Background(), job, reservationID); err != nil {
		t.Fatal(err)
	}
	state, err := s.Snapshot(context.Background(), scope)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(state.BudgetUsage-usage[0].Cost) > 1e-9 {
		t.Errorf("unavailable retry erased previous spending: budget=%g actual=%g", state.BudgetUsage, usage[0].Cost)
	}
	if len(f.all()) != 1 {
		t.Error("unavailable retry made another model call")
	}
}
