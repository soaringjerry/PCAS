package deputystore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/soaringjerry/PCAS/internal/contextbuilder"
	"github.com/soaringjerry/PCAS/internal/modelcall"
	"github.com/soaringjerry/PCAS/internal/postgres/accessstore"
	"github.com/soaringjerry/PCAS/internal/postgres/actionlog"
	"github.com/soaringjerry/PCAS/internal/postgres/budgetstore"
	"github.com/soaringjerry/PCAS/internal/postgres/callstore"
	"github.com/soaringjerry/PCAS/internal/postgres/itemstore"
	"github.com/soaringjerry/PCAS/internal/postgres/kernel"
	"github.com/soaringjerry/PCAS/internal/postgres/usestore"
	"github.com/soaringjerry/PCAS/internal/worker"
	"github.com/soaringjerry/PCAS/internal/workspace/deputy"
	"github.com/soaringjerry/PCAS/internal/workspace/secretary"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func (s *Deputy) RunCommandTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, c workspace.Command) error {
	if c.Type == "requestRun" {
		var prepared *deputy.Prepared
		if p, ok := ctx.Value(RunContextKey{}).(deputy.Prepared); ok {
			prepared = &p
		}
		return deputy.Request(ctx, &requestUnit{store: s, tx: tx, scope: scope}, c, prepared, s.BusinessNow(), time.Now())
	}
	if !memory.ID(c.ID).Valid() {
		return memory.ErrInvalid
	}
	run, err := kernel.QueryDocument[workspace.Run](ctx, tx, "SELECT document FROM agent_runs WHERE owner_id=$1 AND id=$2 FOR UPDATE", string(scope.OwnerID), c.ID)
	if err != nil {
		return err
	}
	switch c.Type {
	case "pasteRunResult":
		if err := deputy.CanPaste(run); err != nil {
			return err
		}
		if run.Kind == "revise" {
			reason, err := manualReviseReasonTx(ctx, tx, scope, run, c.Output)
			if err != nil {
				return err
			}
			if reason != "" {
				run.Error = reason
				if err := kernel.StageEvent(ctx, tx, scope.OwnerID, "deputy", "failure", "revise_invalid_output", 1); err != nil {
					return err
				}
				_, err = tx.Exec(ctx, "UPDATE agent_runs SET document=$3 WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), run.ID, kernel.JSON(run))
				return err
			}
		}
		if err := deputy.Paste(&run, c.Output, time.Now()); err != nil {
			return err
		}
	case "discardRun":
		if err := deputy.Discard(&run, time.Now()); err != nil {
			return err
		}
	case "adoptRun":
		if err := s.adoptRunTx(ctx, tx, scope, &run, c.As, c.Text, c.RequestID, false); err != nil {
			return err
		}
	}
	_, err = tx.Exec(ctx, "UPDATE agent_runs SET status=$3,document=$4 WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), run.ID, run.Status, kernel.JSON(run))
	if err != nil {
		return err
	}
	if c.Type == "pasteRunResult" {
		return s.AutoAdoptRunTx(ctx, tx, scope, &run)
	}
	return nil
}

func (s *Deputy) adoptRunTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, run *workspace.Run, as, text, actionID string, auto bool) error {
	if run.SmokeID != "" {
		ctx = actionlog.WithSmoke(ctx, run.SmokeID)
	}
	return deputy.Adopt(ctx, &adoptUnit{store: s, tx: tx, scope: scope}, run, as, text, actionID, auto, time.Now())
}

// RunAgentJobs owns costly jobs: an ambiguous provider timeout/crash is marked
// failed and is never automatically submitted again (which could double bill).
func (s *Deputy) RunAgentJobs(ctx context.Context, logger *slog.Logger, steps deputy.Steps) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if recovery, err := s.RecoverInteractiveCallsOnce(ctx); err != nil && ctx.Err() == nil {
				logger.Warn("interactive accounting recovery failed", "error_type", fmt.Sprintf("%T", err), "pending_results", recovery.PendingResults, "pending_accounting", recovery.PendingAccounting, "interrupted_calls", recovery.Interrupted, "counts_known", recovery.CountsKnown)
			}
			if err := s.RunAgent(ctx, steps); err != nil && ctx.Err() == nil {
				logger.Warn("agent job failed", "error_type", fmt.Sprintf("%T", err))
			}
		}
	}
}

// RunDeputy makes the single worker pass for bounded evaluation.
// Selection, leases, generation, and application remain in the normal workflow.
func (s *Deputy) RunDeputy(ctx context.Context, steps deputy.Steps) error {
	return s.RunAgent(ctx, steps)
}

func (s *Deputy) RunAgent(ctx context.Context, steps deputy.Steps) (resultErr error) {
	started := time.Now()
	ctx, timing := budgetstore.NewExecutionTimer(ctx, "deputy", started)
	ctx, persistCancel := context.WithDeadline(ctx, started.Add(secretary.TurnTimeout))
	defer persistCancel()
	s.PendingPaid.Range(func(key, value any) bool {
		var snap deputy.Snapshot
		paid, ok := value.(*callstore.PaidModelResult)
		if !ok || json.Unmarshal(paid.Prompt, &snap) != nil || snap.Run.Kind != "revise" {
			return true
		}
		_, _ = s.Pool.Exec(ctx, `UPDATE agent_runs SET status='queued',lease_token=NULL,lease_until=NULL WHERE id=$1 AND document->>'createdAt'=$2 AND agent_id=$3 AND thing_id=$4 AND status='running' AND lease_until<clock_timestamp()`, string(key.(memory.ID)), snap.Run.CreatedAt, snap.Run.AgentID, snap.Run.ThingID)
		return true
	})
	// A durable returned result, or a recorded pre-submission failure, can resume.
	// Input-only started calls cannot authorize another paid submission.
	s.PendingInteractive.Range(func(_, value any) bool {
		entry := value.(*callstore.PendingInteractiveResult)
		policy := entry.Request.Policy.(callstore.InteractiveCallPolicy)
		if policy.BudgetOwner == "deputy_run" {
			_, _ = s.Pool.Exec(ctx, `UPDATE agent_runs SET status='queued',lease_token=NULL,lease_until=NULL WHERE owner_id=$1 AND id=$2 AND document->>'createdAt'=$3 AND status='running' AND lease_until<clock_timestamp()`, string(entry.Request.OwnerID), string(entry.Request.ExecutionID), policy.Origin)
		}
		return true
	})
	recoverableSQL := `EXISTS(SELECT 1 FROM background_model_results b WHERE b.owner_id=agent_runs.owner_id AND b.job_id=agent_runs.id AND b.purpose='revise') OR EXISTS(SELECT 1 FROM model_calls c WHERE c.owner_id=agent_runs.owner_id AND c.execution_id=agent_runs.id AND c.stage='answer' AND c.input_manifest->>'budgetOwner'='deputy_run' AND c.input_manifest->>'origin'=agent_runs.document->>'createdAt' AND ((c.result_receipt->'billing' IS NOT NULL AND c.result_receipt->'billing'<>'null'::jsonb) OR (c.outcome='failed' AND c.accounting_state IN ('not_reserved','settled'))))`
	_, err := s.Pool.Exec(ctx, `UPDATE agent_runs SET status='failed',lease_until=NULL,lease_token=NULL,document=document||jsonb_build_object('status','failed','error',$1::text,'finishedAt',now()) WHERE status='running' AND lease_until<now() AND NOT (`+recoverableSQL+`)`, deputy.Interrupted)
	if err != nil {
		return err
	}
	if err := (callstore.InteractiveCalls{Store: s.Calls}).RecordInterruptedDeputyApplications(ctx); err != nil {
		return err
	}
	if _, err := s.Pool.Exec(ctx, `UPDATE agent_runs SET status='queued',lease_until=NULL,lease_token=NULL WHERE status='running' AND lease_until<now() AND (`+recoverableSQL+`)`); err != nil {
		return err
	}
	var cached *callstore.PaidModelResult
	var run workspace.Run
	var scope memory.Scope
	var token string
	var savedRow *callstore.InteractiveCallRow
	var savedInput *deputy.Snapshot
	err = pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		var ownerID, id string
		var data []byte
		err := tx.QueryRow(ctx, `SELECT owner_id::text,id::text,document FROM agent_runs WHERE status='queued' AND ($1::boolean OR (`+recoverableSQL+`)) ORDER BY created_at FOR UPDATE SKIP LOCKED LIMIT 1`, s.Models != nil).Scan(&ownerID, &id, &data)
		if err != nil {
			return err
		}
		scope = memory.Scope{OwnerID: memory.ID(ownerID), PrincipalID: "owner", IsOwner: true}
		if err := modelcall.DecodeStrict(data, &run); err != nil {
			return err
		}
		run.MemoryTier = deputy.Tier(run.MemoryTier)
		if run.Kind == "revise" {
			cached, err = s.PaidModelResult(ctx, worker.Job{ID: memory.ID(run.ID), OwnerID: scope.OwnerID})
			if err != nil {
				return err
			}
			if cached != nil {
				var original deputy.Snapshot
				if json.Unmarshal(cached.Prompt, &original) != nil || original.Run.ID != run.ID || original.Run.CreatedAt != run.CreatedAt || original.Run.AgentID != run.AgentID || original.Run.ThingID != run.ThingID {
					cached = nil
				}
			}
		}
		savedRow, savedInput, err = (callstore.InteractiveCalls{Store: s.Calls}).DeputyInput(ctx, tx, scope.OwnerID, run)
		if err != nil {
			return err
		}
		if cached == nil && savedRow == nil {
			if err := usestore.CheckQueuedUseRunPromptTx(ctx, tx, scope, run); err != nil {
				run.Status = "failed"
				run.Cost = 0
				run.Error = deputy.ContextChanged
				_, err = tx.Exec(ctx, "UPDATE agent_runs SET status='failed',reserved_cost=0,document=$3 WHERE owner_id=$1 AND id=$2", ownerID, id, kernel.JSON(run))
				return err
			}
		}
		if run.Kind == "revise" && cached == nil && savedRow == nil {
			if err := validateReviseTargetTx(ctx, tx, scope, run); err != nil {
				run.Status = "failed"
				run.Error = deputy.TargetChanged
				_, e := tx.Exec(ctx, "UPDATE agent_runs SET status='failed',document=$3,reserved_cost=0 WHERE owner_id=$1 AND id=$2", ownerID, id, kernel.JSON(run))
				return e
			}
		}
		token = string(memory.NewID())
		_, err = tx.Exec(ctx, "UPDATE agent_runs SET status='running',lease_token=$3,lease_until=now()+interval '5 minutes' WHERE owner_id=$1 AND id=$2", ownerID, id, token)
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil || token == "" {
		return err
	}
	timing.ID = run.ID
	timing.Tier = run.MemoryTier
	defer s.FinishExecutionTiming(ctx, scope.OwnerID, timing)
	if cached != nil && run.Kind == "revise" {
		return s.finishPaidRevise(ctx, scope, run, token, cached)
	}
	// A write failure makes only the saved execution available for the next pass.
	defer func() {
		if resultErr == nil {
			return
		}
		persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), modelcall.PersistenceTimeout)
		defer cancel()
		_, _ = s.Pool.Exec(persist, `UPDATE agent_runs SET lease_until=clock_timestamp(),document=document||jsonb_build_object('error',$5::text) WHERE owner_id=$1 AND id=$2 AND document->>'createdAt'=$3 AND lease_token=$4 AND status='running'`, string(scope.OwnerID), run.ID, run.CreatedAt, token, deputy.WriteIncomplete)
	}()
	execution := modelcall.Request{OwnerID: scope.OwnerID, ExecutionID: memory.ID(run.ID), RootExecutionID: memory.ID(run.ID), Function: "deputy", Policy: callstore.InteractiveCallPolicy{Kind: "deputy", Token: token, Origin: run.CreatedAt, AgentID: run.AgentID, ThingID: run.ThingID}}
	ctx = modelcall.WithExecution(ctx, execution)
	workCtx, cancel := context.WithDeadline(ctx, started.Add(secretary.TurnTimeout-10*time.Second))
	defer cancel()
	if s.Models != nil {
		defer s.Models.EndJob()
	}
	if savedInput != nil {
		run = savedInput.Run
	}
	session := &runSession{store: s, scope: scope, token: token, run: &run, started: started, timing: timing, savedInput: savedInput}
	if savedRow != nil {
		session.saved = callRecord(savedRow)
	}
	return deputy.Conduct(ctx, workCtx, session, steps, secretary.ErrorType)
}

// runSession is one leased run. The deputy's domain package conducts the
// run through it.
type runSession struct {
	store      *Deputy
	scope      memory.Scope
	token      string
	run        *workspace.Run
	started    time.Time
	timing     *budgetstore.ExecutionTimer
	saved      *deputy.CallRecord
	savedInput *deputy.Snapshot
}

func callRecord(row *callstore.InteractiveCallRow) *deputy.CallRecord {
	if row == nil {
		return nil
	}
	record := &deputy.CallRecord{ID: row.ID, ErrorCode: row.ErrorCode, Unknown: row.Outcome == "unknown", Reservation: row.Reservation, Reserved: row.Mode.Reserved}
	if row.Receipt.Billing != nil {
		record.Billed = &row.Receipt.Billing.Cost
	}
	return record
}

func (r *runSession) Owner() memory.ID          { return r.scope.OwnerID }
func (r *runSession) Token() string             { return r.token }
func (r *runSession) Run() *workspace.Run       { return r.run }
func (r *runSession) Started() time.Time        { return r.started }
func (r *runSession) Saved() *deputy.CallRecord { return r.saved }
func (r *runSession) InputSaved() bool          { return r.savedInput != nil }
func (r *runSession) Preparing()                { r.timing.BeginPrepare() }
func (r *runSession) Prepared()                 { r.timing.FinishPrepare() }

func (r *runSession) SavedContext() (bool, *contextbuilder.Coverage) {
	return r.savedInput.MemoryReady, r.savedInput.Coverage
}

func (r *runSession) Prepare(ctx context.Context) (contextbuilder.UseContext, workspace.Agent, error) {
	return r.store.DeputyUseContext(ctx, r.scope, r.run)
}

func (r *runSession) VerifyPrompt(ctx context.Context) error {
	return pgx.BeginFunc(ctx, r.store.Pool, func(tx pgx.Tx) error { return usestore.CheckUseRunPromptTx(ctx, tx, r.scope, *r.run) })
}

func (r *runSession) Call(ctx context.Context, request modelcall.Request) (*deputy.CallRecord, error) {
	row, err := callstore.LatestInteractiveCall(ctx, r.store.Pool, request)
	return callRecord(row), err
}

func (r *runSession) FinishRevision(ctx context.Context, paid *modelcall.PaidResult) error {
	return r.store.finishPaidRevise(ctx, r.scope, *r.run, r.token, paid)
}

func (r *runSession) SettleCost(ctx context.Context, cost float64) error {
	return r.store.SettleRunCost(ctx, r.scope.OwnerID, *r.run, cost)
}

func (r *runSession) Selection(ctx context.Context, answer memory.ID) (*deputy.Selection, string, error) {
	return (callstore.InteractiveCalls{Store: r.store.Calls}).DeputySelection(ctx, r.scope.OwnerID, *r.run, answer)
}

func (r *runSession) ReviewAttempted(ctx context.Context) (memory.ID, bool, error) {
	var review memory.ID
	err := r.store.Pool.QueryRow(ctx, `SELECT id::text FROM model_calls WHERE owner_id=$1 AND execution_id=$2 AND stage='selfcheck' AND input_manifest->>'origin'=$3 ORDER BY created_at DESC,id DESC LIMIT 1`, string(r.scope.OwnerID), r.run.ID, r.run.CreatedAt).Scan(&review)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	return review, err == nil, err
}

func (r *runSession) VerifySelfcheck(ctx context.Context) error {
	return r.store.CheckDeputySelfcheckContext(ctx, r.scope, *r.run, r.token)
}

func (r *runSession) SaveSelection(ctx context.Context, request modelcall.Request, selection deputy.Selection) error {
	return (callstore.InteractiveCalls{Store: r.store.Calls}).SaveDeputySelection(ctx, request, selection)
}

// Finish writes the end of the run in one transaction under the owner lock.
// A run whose lease was lost is left as it is.
func (r *runSession) Finish(ctx context.Context, end deputy.End) error {
	s, scope, run, token, timing := r.store, r.scope, *r.run, r.token, r.timing
	timing.BeginWrite(run.MemoryTier)
	return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT 1 FROM workspace_owners WHERE owner_id=$1 FOR UPDATE", string(scope.OwnerID)); err != nil {
			return err
		}
		current, err := kernel.QueryDocument[workspace.Run](ctx, tx, "SELECT document FROM agent_runs WHERE owner_id=$1 AND id=$2 AND lease_token=$3 AND lease_until>now() AND document->>'createdAt'=$4 FOR UPDATE", string(scope.OwnerID), run.ID, token, run.CreatedAt)
		if errors.Is(err, memory.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		for _, ref := range run.ContextVersions {
			if _, err := tx.Exec(ctx, "INSERT INTO run_dependencies(owner_id,run_id,memory_id,memory_version) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING", string(scope.OwnerID), run.ID, string(ref.ID), ref.Version); err != nil {
				return err
			}
		}
		ending := deputy.Finish(ctx, finishUnit{tx: tx, scope: scope}, current, run, end, time.Now())
		current = ending.Run
		if _, err := tx.Exec(ctx, "UPDATE agent_runs SET status=$4,reserved_cost=$5,document=$6,lease_until=NULL,lease_token=NULL WHERE owner_id=$1 AND id=$2 AND lease_token=$3", string(scope.OwnerID), run.ID, token, current.Status, current.Cost, kernel.JSON(current)); err != nil {
			return err
		}
		if err := s.AutoAdoptRunTx(ctx, tx, scope, &current); err != nil {
			return err
		}
		calls := callstore.InteractiveCalls{Store: s.Calls}
		if err := calls.RecordDeputyApplicationTx(ctx, tx, scope.OwnerID, current, end.Answer, ending.Answer.Outcome, ending.Answer.Reason); err != nil {
			return err
		}
		if ending.RecordReview {
			if err := calls.RecordDeputyApplicationTx(ctx, tx, scope.OwnerID, current, end.Review, ending.Review.Outcome, ending.Review.Reason); err != nil {
				return err
			}
		}
		if ending.SelfcheckFailure != "" {
			if err := kernel.StageEvent(ctx, tx, scope.OwnerID, "selfcheck", "failure", ending.SelfcheckFailure, 1); err != nil {
				return err
			}
		}
		// Delivery triggers expose background results without changing the
		// revision used to authorize checkbox, reminder and bulk commands.
		_, err = tx.Exec(ctx, `INSERT INTO workspace_notices(owner_id,thing_id,trigger_id,due_at,reason) VALUES($1,$2,$3,now(),$4) ON CONFLICT DO NOTHING`,
			string(scope.OwnerID), current.ThingID, kernel.RunNoticePrefix+current.ID, ending.Notice)
		return err
	})
}

// finishUnit checks a run at its end in the transaction of its last write.
type finishUnit struct {
	tx    pgx.Tx
	scope memory.Scope
}

func (u finishUnit) PromptCurrent(ctx context.Context, run workspace.Run) bool {
	return usestore.CheckUseRunPromptTx(ctx, u.tx, u.scope, run) == nil
}

func (u finishUnit) Permitted(ctx context.Context, run workspace.Run) bool {
	return accessstore.VerifyRunAccessTx(ctx, u.tx, u.scope, run) == nil
}

func (s *Deputy) AutoAdoptRunTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, run *workspace.Run) error {
	if !deputy.AutoAdoptable(*run) {
		return nil
	}
	// Completion and billing are already saved in the outer transaction. Only
	// adoption writes may be rolled back, including SQL errors that abort a tx.
	savepoint, err := tx.Begin(ctx)
	if err != nil {
		return err
	}
	defer savepoint.Rollback(ctx)
	// adoptRunTx sets Adopted before its final writes. Keep that mutation out of
	// the caller's completed run until the savepoint has successfully committed.
	adopted := *run
	if err := s.autoAdoptResultTx(ctx, savepoint, scope, &adopted); err != nil {
		rollbackErr := savepoint.Rollback(ctx)
		// SQL/provider error messages can contain private text. Log only IDs and
		// the error's type, never the output, prompt, or raw error message.
		slog.WarnContext(ctx, "assistant result auto-adoption failed", "run_id", run.ID, "error_type", fmt.Sprintf("%T", err))
		// A failed rollback leaves the outer transaction unusable. Ordinary
		// adoption failures return nil so completion can still commit.
		return rollbackErr
	}
	if err := savepoint.Commit(ctx); err != nil {
		return err
	}
	*run = adopted
	return nil
}

func (s *Deputy) autoAdoptResultTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, run *workspace.Run) error {
	if run.SmokeID != "" {
		ctx = actionlog.WithSmoke(ctx, run.SmokeID)
	}
	item, err := itemstore.GetItem(ctx, tx, scope, run.ThingID)
	if err != nil {
		return err
	}
	as := deputy.AdoptionFor(*run, run.Output)
	summary := deputy.AdoptionSummary(*run, item, as)
	id := string(memory.NewID())
	ctx = kernel.WithActor(ctx, "assistant")
	ctx = actionlog.WithActionLog(ctx, id, "worker", "", summary)
	if err := actionlog.BeginActionLogTx(ctx, tx); err != nil {
		return err
	}
	if err := s.adoptRunTx(ctx, tx, scope, run, as, run.Output, id, true); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "UPDATE agent_runs SET document=$3 WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), run.ID, kernel.JSON(run)); err != nil {
		return err
	}
	return actionlog.FlushActionLog(ctx, tx, scope)
}
