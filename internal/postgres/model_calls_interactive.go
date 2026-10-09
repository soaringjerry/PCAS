package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/modelcall"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// interactiveCallPolicy binds an existing admitted request or leased deputy.
// Token is an execution fence, not a fabricated queue lease. Origin survives
// reacquisition and excludes transient presentation-turn and usage identities.
type interactiveCallPolicy struct {
	Kind        string
	Token       string
	Origin      string
	RequestHash []byte
	AgentID     string
	ThingID     string
	Usage       modelUsage
	RetryOf     memory.ID
}

type interactiveCalls struct{ store *Store }

// modelCallStorage routes the existing gateway ports to their execution owner.
// It never invokes providers or makes business decisions.
type modelCallStorage struct {
	background  backgroundCalls
	interactive interactiveCalls
}

func (a modelCallStorage) Load(ctx context.Context, r modelcall.Request) (*modelcall.PaidResult, error) {
	if _, ok := r.Policy.(interactiveCallPolicy); ok {
		return a.interactive.Load(ctx, r)
	}
	return a.background.Load(ctx, r)
}
func (a modelCallStorage) Save(ctx context.Context, r modelcall.Request, p *modelcall.PaidResult) error {
	if _, ok := r.Policy.(interactiveCallPolicy); ok {
		return a.interactive.Save(ctx, r, p)
	}
	return a.background.Save(ctx, r, p)
}
func (a modelCallStorage) Forget(ctx context.Context, r modelcall.Request, p *modelcall.PaidResult) error {
	if _, ok := r.Policy.(interactiveCallPolicy); ok {
		return a.interactive.Forget(ctx, r, p)
	}
	return a.background.Forget(ctx, r, p)
}
func (a modelCallStorage) Reserve(ctx context.Context, r modelcall.Request, cost float64) (string, error) {
	if _, ok := r.Policy.(interactiveCallPolicy); ok {
		return a.interactive.Reserve(ctx, r, cost)
	}
	return a.background.Reserve(ctx, r, cost)
}
func (a modelCallStorage) Record(ctx context.Context, r modelcall.Request, p *modelcall.PaidResult) error {
	if _, ok := r.Policy.(interactiveCallPolicy); ok {
		return a.interactive.Record(ctx, r, p)
	}
	return a.background.Record(ctx, r, p)
}
func (a modelCallStorage) Settle(ctx context.Context, r modelcall.Request, p *modelcall.PaidResult) error {
	if _, ok := r.Policy.(interactiveCallPolicy); ok {
		return a.interactive.Settle(ctx, r, p)
	}
	return a.background.Settle(ctx, r, p)
}
func (a modelCallStorage) Prepare(ctx context.Context, r modelcall.Request, p ai.Provider) (memory.ID, error) {
	if _, ok := r.Policy.(interactiveCallPolicy); ok {
		return a.interactive.Prepare(ctx, r, p)
	}
	return a.background.Prepare(ctx, r, p)
}
func (a modelCallStorage) Start(ctx context.Context, r modelcall.Request, p *modelcall.PaidResult) error {
	if _, ok := r.Policy.(interactiveCallPolicy); ok {
		return a.interactive.Start(ctx, r, p)
	}
	return a.background.Start(ctx, r, p)
}
func (a modelCallStorage) PreparationFailed(ctx context.Context, r modelcall.Request, id memory.ID, err error) error {
	if _, ok := r.Policy.(interactiveCallPolicy); ok {
		return a.interactive.PreparationFailed(ctx, r, id, err)
	}
	return a.background.PreparationFailed(ctx, r, id, err)
}
func (a modelCallStorage) AccountingState(ctx context.Context, r modelcall.Request, p *modelcall.PaidResult, state string) error {
	if _, ok := r.Policy.(interactiveCallPolicy); ok {
		return a.interactive.AccountingState(ctx, r, p, state)
	}
	return a.background.AccountingState(ctx, r, p, state)
}

type interactiveReceipt struct {
	Kind         string                `json:"kind"`
	OutputHash   string                `json:"outputHash,omitempty"`
	InvocationID memory.ID             `json:"invocationId"`
	Available    bool                  `json:"availableForApplication"`
	Billing      *modelcall.PaidResult `json:"billing,omitempty"`
	Searches     []string              `json:"searches,omitempty"`
}

type interactiveCallRow struct {
	ID          memory.ID `json:"id"`
	Provider    string    `json:"provider_id"`
	Model       string    `json:"model"`
	Outcome     string    `json:"outcome"`
	ErrorCode   string    `json:"error_code"`
	Accounting  string    `json:"accounting_state"`
	Recovery    string    `json:"recovery_state"`
	Attempt     int       `json:"attempt_number"`
	Limit       int       `json:"attempt_limit"`
	Reservation string    `json:"reservation_id"`
	Manifest    struct {
		BindingHash string `json:"bindingHash"`
	} `json:"input_manifest"`
	Mode struct {
		Usage       modelUsage `json:"originalUsage"`
		Unsupported string     `json:"unsupportedCapability"`
	} `json:"actual_mode"`
	Receipt interactiveReceipt `json:"result_receipt"`
}

func interactiveBinding(r modelcall.Request) (string, error) {
	p, ok := r.Policy.(interactiveCallPolicy)
	if !ok || !r.OwnerID.Valid() || !r.ExecutionID.Valid() || !r.RootExecutionID.Valid() || !memory.ID(p.Token).Valid() || p.Origin == "" || p.AgentID == "" || r.ProviderID == "" || r.ContextBuilderVersion == "" || p.Usage.JobID != "" || !json.Valid(r.Prompt) || (p.Kind != "secretary" && p.Kind != "deputy") {
		return "", memory.ErrInvalid
	}
	if p.Kind == "secretary" && (len(p.RequestHash) != sha256.Size || p.Origin != fmt.Sprintf("%x", p.RequestHash)) {
		return "", memory.ErrInvalid
	}
	refs := r.Refs
	if refs == nil {
		refs = []memory.Ref{}
	}
	for _, ref := range refs {
		if !ref.ID.Valid() || ref.Version < 1 {
			return "", memory.ErrInvalid
		}
	}
	binding := struct {
		Owner, Execution, Root, Cause                                                        memory.ID
		Function, Stage, Provider, Instruction, InstructionHash, Schema, SchemaHash, Builder string
		Search                                                                               bool
		InputHash                                                                            string
		Refs                                                                                 []memory.Ref
		Kind, Origin, Agent, Thing                                                           string
	}{r.OwnerID, r.ExecutionID, r.RootExecutionID, r.CausationID, r.Function, r.Stage, r.ProviderID, r.Instructions.Name(), r.Instructions.Hash(), r.Schema.Name(), r.Schema.Hash(), r.ContextBuilderVersion, r.Search, fmt.Sprintf("%x", sha256.Sum256(r.Prompt)), refs, p.Kind, p.Origin, p.AgentID, p.ThingID}
	return fmt.Sprintf("%x", sha256.Sum256(asJSON(binding))), nil
}

func latestInteractiveCall(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, r modelcall.Request) (*interactiveCallRow, error) {
	var row interactiveCallRow
	err := q.QueryRow(ctx, `SELECT to_jsonb(c) FROM model_calls c WHERE owner_id=$1 AND execution_id=$2 AND stage=$3 ORDER BY created_at DESC,id DESC LIMIT 1`, string(r.OwnerID), string(r.ExecutionID), r.Stage).Scan(&row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &row, err
}

// The secretary holds its admission row lock across its business transaction.
// Reading its committed fence avoids acquiring that lock from another connection.
func interactiveFence(ctx context.Context, tx pgx.Tx, r modelcall.Request) error {
	p := r.Policy.(interactiveCallPolicy)
	var allowed bool
	var err error
	if p.Kind == "secretary" {
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM desk_turn_order WHERE owner_id=$1 AND request_id=$2 AND creator_id=$3 AND request_hash=$4 AND status='pending' AND expires_at>clock_timestamp())`, string(r.OwnerID), string(r.ExecutionID), p.Token, p.RequestHash).Scan(&allowed)
	} else {
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agent_runs WHERE owner_id=$1 AND id=$2 AND lease_token=$3 AND document->>'createdAt'=$4 AND status='running' AND lease_until>clock_timestamp() AND agent_id=$5 AND thing_id=$6)`, string(r.OwnerID), string(r.ExecutionID), p.Token, p.Origin, p.AgentID, p.ThingID).Scan(&allowed)
	}
	if err != nil {
		return err
	}
	if !allowed {
		return modelcall.ErrNotApplicable
	}
	return nil
}

func (a interactiveCalls) Load(ctx context.Context, r modelcall.Request) (*modelcall.PaidResult, error) {
	hash, err := interactiveBinding(r)
	if err != nil {
		return nil, err
	}
	row, err := latestInteractiveCall(ctx, a.store.pool, r)
	if err != nil || row == nil {
		return nil, err
	}
	if row.Manifest.BindingHash != hash {
		return nil, memory.ErrConflict
	}
	if p := r.Policy.(interactiveCallPolicy); p.RetryOf != "" {
		if p.RetryOf != row.ID {
			return nil, memory.ErrConflict
		}
		return nil, nil
	}
	if value, ok := a.store.pendingInteractive.Load(row.ID); ok {
		return cloneInteractivePaid(value.(*modelcall.PaidResult)), nil
	}
	if row.Receipt.Billing == nil {
		if row.Mode.Unsupported != "" {
			return nil, &ai.CapabilityError{Capability: row.Mode.Unsupported}
		}
		if row.Outcome == "failed" && row.Accounting == "not_reserved" {
			return nil, &modelcall.Failure{Code: row.ErrorCode, InvocationID: row.ID}
		}
		return nil, modelcall.ErrOutcomeUnknown
	}
	paid := row.Receipt.Billing
	// JSON null in the body-free receipt is an absent payload, not model input.
	paid.Prompt = nil
	paid.Output = ""
	if paid.InvocationID != row.ID || paid.Reservation != row.Reservation || paid.Provider != row.Provider || paid.Model != row.Model || paid.CallErrorCode != row.ErrorCode {
		return nil, memory.ErrConflict
	}
	if !row.Receipt.Available {
		paid.NotApplicable = true
		return paid, nil
	}
	var prompt json.RawMessage
	err = a.store.pool.QueryRow(ctx, `SELECT prompt,output FROM background_model_results WHERE owner_id=$1 AND job_id=$2 AND reservation_id=$3 AND provider_id=$4 AND model=$5`, string(r.OwnerID), string(row.ID), paid.Reservation, paid.Provider, paid.Model).Scan(&prompt, &paid.Output)
	if errors.Is(err, pgx.ErrNoRows) {
		paid.NotApplicable = true
		return paid, nil
	}
	if err != nil {
		return nil, err
	}
	// jsonb changes whitespace. Compare the original payload's recorded binding;
	// application uses the caller's exactly bound bytes, never database reformatting.
	if row.Receipt.OutputHash != fmt.Sprintf("%x", sha256.Sum256([]byte(paid.Output))) {
		return nil, memory.ErrConflict
	}
	paid.Prompt = append(json.RawMessage(nil), r.Prompt...)
	paid.Searches = append([]string(nil), row.Receipt.Searches...)
	return paid, nil
}

func (a interactiveCalls) Prepare(ctx context.Context, r modelcall.Request, provider ai.Provider) (memory.ID, error) {
	hash, err := interactiveBinding(r)
	if err != nil {
		return "", err
	}
	id := memory.NewID()
	p := r.Policy.(interactiveCallPolicy)
	err = pgx.BeginFunc(ctx, a.store.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended(current_database()||':'||current_schema()||':interactive:'||$1||':'||$2||':'||$3,0))`, string(r.OwnerID), string(r.ExecutionID), r.Stage); err != nil {
			return err
		}
		if err := interactiveFence(ctx, tx, r); err != nil {
			return err
		}
		if err := verifyRunTx(ctx, tx, memory.Scope{OwnerID: r.OwnerID, PrincipalID: p.AgentID}, workspace.Run{AgentID: p.AgentID, ThingID: p.ThingID, ContextVersions: r.Refs}); err != nil {
			return err
		}
		previous, err := latestInteractiveCall(ctx, tx, r)
		if err != nil {
			return err
		}
		attempt, limit := 1, 1
		if p.Kind == "secretary" && r.Stage == "answer" {
			limit = secretaryModelAttempts
		}
		if previous != nil {
			if previous.Manifest.BindingHash != hash {
				return memory.ErrConflict
			}
			if p.RetryOf != previous.ID {
				return modelcall.ErrOutcomeUnknown
			}
			if p.Kind != "secretary" || r.Stage != "answer" || previous.Attempt >= previous.Limit {
				return modelcall.ErrRetryExhausted
			}
			if previous.Receipt.Billing == nil || (previous.Accounting != "settled" && previous.Accounting != "held") {
				return modelcall.ErrOutcomeUnknown
			}
			failure := &modelcall.Failure{Code: previous.ErrorCode, Details: previous.Receipt.Billing.FailureDetails}
			if !retryableSecretaryModelError(failure) {
				return failure
			}
			attempt = previous.Attempt + 1
			limit = previous.Limit
			if _, err := tx.Exec(ctx, `UPDATE model_calls SET recovery_state='replaced',recovery_reason='secretary_transient_retry',updated_at=clock_timestamp() WHERE owner_id=$1 AND id=$2 AND recovery_state='active'`, string(r.OwnerID), string(previous.ID)); err != nil {
				return err
			}
		} else if p.RetryOf != "" {
			return memory.ErrConflict
		}
		usage := p.Usage
		usage.OwnerID = r.OwnerID
		usage.AgentID = provider.ID
		usage.Model = provider.Model
		usage.MemoryRefs = nil
		usage.Plan = asJSON(safeUsePlan(usage.Plan))

		refs := r.Refs
		if refs == nil {
			refs = []memory.Ref{}
		}
		manifest := map[string]any{"version": "interactive-input-v1", "bindingHash": hash, "inputHash": fmt.Sprintf("%x", sha256.Sum256(r.Prompt)), "inputBytes": len(r.Prompt), "memoryRefs": refs, "executionKind": p.Kind, "origin": p.Origin, "agentId": p.AgentID, "thingId": p.ThingID}
		mode := map[string]any{"output": "text", "schema": "none", "search": false, "executionToken": p.Token, "originalUsage": usage, "usageComplete": false}
		_, err = tx.Exec(ctx, `INSERT INTO model_calls(owner_id,id,execution_id,root_execution_id,causation_id,retry_of_id,attempt_number,attempt_limit,function_name,stage,provider_id,model,prompt_name,instruction_hash,schema_name,schema_hash,context_builder_version,input_manifest,required_capabilities,actual_mode,outcome,accounting_state) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,'prepared','not_reserved')`, string(r.OwnerID), string(id), string(r.ExecutionID), string(r.RootExecutionID), nullString(string(r.CausationID)), nullString(string(p.RetryOf)), attempt, limit, r.Function, r.Stage, provider.ID, provider.Model, r.Instructions.Name(), r.Instructions.Hash(), nullString(r.Schema.Name()), nullString(r.Schema.Hash()), r.ContextBuilderVersion, asJSON(manifest), asJSON(r.RequiredCapabilities()), asJSON(mode))
		return err
	})
	return id, err
}

func (a interactiveCalls) Reserve(ctx context.Context, r modelcall.Request, cost float64) (string, error) {
	return a.store.reserveModelCostID(ctx, r.OwnerID, cost, nil, func(ctx context.Context, tx pgx.Tx, id string) error {
		if err := interactiveFence(ctx, tx, r); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `UPDATE model_calls SET reservation_id=$4,accounting_state='reserved',actual_mode=actual_mode||jsonb_build_object('reservationEstimate',$5::double precision),updated_at=clock_timestamp() WHERE owner_id=$1 AND execution_id=$2 AND stage=$3 AND outcome='prepared' AND recovery_state='active' AND actual_mode->>'executionToken'=$6`, string(r.OwnerID), string(r.ExecutionID), r.Stage, id, cost, r.Policy.(interactiveCallPolicy).Token)
		if err == nil && tag.RowsAffected() != 1 {
			return modelcall.ErrOutcomeUnknown
		}
		return err
	})
}

func (a interactiveCalls) Start(ctx context.Context, r modelcall.Request, paid *modelcall.PaidResult) error {
	return pgx.BeginFunc(ctx, a.store.pool, func(tx pgx.Tx) error {
		if err := interactiveFence(ctx, tx, r); err != nil {
			return err
		}
		mode := map[string]any{"search": r.Search}
		if r.Schema.Name() != "" {
			mode["schema"] = r.Schema.Name()
			mode["output"] = "structured"
		}
		tag, err := tx.Exec(ctx, `UPDATE model_calls SET outcome='started',started_at=clock_timestamp(),updated_at=clock_timestamp(),actual_mode=actual_mode||$4::jsonb WHERE owner_id=$1 AND id=$2 AND reservation_id=$3 AND outcome='prepared' AND recovery_state='active'`, string(r.OwnerID), string(paid.InvocationID), paid.Reservation, asJSON(mode))
		if err == nil && tag.RowsAffected() != 1 {
			return modelcall.ErrOutcomeUnknown
		}
		return err
	})
}

func (a interactiveCalls) PreparationFailed(ctx context.Context, r modelcall.Request, id memory.ID, err error) error {
	return (backgroundCalls{store: a.store}).PreparationFailed(ctx, r, id, err)
}

func (a interactiveCalls) Save(ctx context.Context, r modelcall.Request, paid *modelcall.PaidResult) error {
	a.store.pendingInteractive.Store(paid.InvocationID, cloneInteractivePaid(paid))
	for {
		persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), modelcall.PersistenceTimeout)
		err := pgx.BeginFunc(persist, a.store.pool, func(tx pgx.Tx) error {
			var manifestHash, provider, model, reservation string
			var prior interactiveReceipt
			if err := tx.QueryRow(persist, `SELECT input_manifest->>'bindingHash',provider_id,model,coalesce(reservation_id::text,''),coalesce(result_receipt,'{}'::jsonb) FROM model_calls WHERE owner_id=$1 AND id=$2 FOR UPDATE`, string(r.OwnerID), string(paid.InvocationID)).Scan(&manifestHash, &provider, &model, &reservation, &prior); err != nil {
				return err
			}
			expected, err := interactiveBinding(r)
			if err != nil {
				return err
			}
			if len(paid.Prompt) > 0 && !bytes.Equal(paid.Prompt, r.Prompt) {
				return memory.ErrConflict
			}
			if manifestHash != expected || provider != paid.Provider || model != paid.Model || reservation != paid.Reservation || !bytes.Equal(asJSON(uniqueRefs(paid.Refs)), asJSON(uniqueRefs(r.Refs))) {
				return memory.ErrConflict
			}
			accessible := !paid.NotApplicable
			var deleted bool
			if err := tx.QueryRow(persist, `SELECT coalesce((actual_mode->>'inputDeleted')::boolean,false) FROM model_calls WHERE owner_id=$1 AND id=$2`, string(r.OwnerID), string(paid.InvocationID)).Scan(&deleted); err != nil {
				return err
			}
			accessible = accessible && !deleted
			// Share locks serialize raw-result retention with the source owner's deletion.
			// The model has already returned; no provider call occurs inside this transaction.
			for _, ref := range paid.Refs {
				var version int
				err := tx.QueryRow(persist, `SELECT version FROM memory_records WHERE owner_id=$1 AND id=$2 AND state='active' FOR SHARE`, string(r.OwnerID), string(ref.ID)).Scan(&version)
				if errors.Is(err, pgx.ErrNoRows) {
					accessible = false
					break
				}
				if err != nil {
					return err
				}
				if version != ref.Version {
					accessible = false
					break
				}
			}
			p := r.Policy.(interactiveCallPolicy)
			if accessible {
				err := verifyRunTx(persist, tx, memory.Scope{OwnerID: r.OwnerID, PrincipalID: p.AgentID}, workspace.Run{AgentID: p.AgentID, ThingID: p.ThingID, ContextVersions: paid.Refs})
				if err != nil {
					if errors.Is(err, memory.ErrConflict) || errors.Is(err, memory.ErrForbidden) || errors.Is(err, memory.ErrNotFound) {
						accessible = false
					} else {
						return err
					}
				}
			}
			available := accessible
			if err := interactiveFence(persist, tx, r); err != nil {
				if errors.Is(err, modelcall.ErrNotApplicable) {
					available = false
				} else {
					return err
				}
			}
			paid.NotApplicable = paid.NotApplicable || !available
			if accessible {
				_, err = tx.Exec(persist, `INSERT INTO background_model_results(owner_id,job_id,purpose,prompt,output,reservation_id,provider_id,model,input_tokens,output_tokens,cost,refs,input_estimated,output_estimated,cost_estimated,duration_ms) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16) ON CONFLICT(job_id) DO NOTHING`, string(r.OwnerID), string(paid.InvocationID), r.Function, paid.Prompt, paid.Output, paid.Reservation, paid.Provider, paid.Model, paid.InputTokens, paid.OutputTokens, paid.Cost, asJSON(paid.Refs), paid.InputEstimated, paid.OutputEstimated, paid.CostEstimated, paid.DurationMS)
				if err != nil {
					return err
				}
			}
			billing := *paid
			billing.Prompt = nil
			billing.Output = ""
			billing.Searches = nil
			if prior.Billing != nil {
				original := *prior.Billing
				original.NotApplicable = false
				compare := billing
				compare.NotApplicable = false
				if !bytes.Equal(asJSON(original), asJSON(compare)) {
					return memory.ErrConflict
				}
				available = available && prior.Available
				paid.NotApplicable = paid.NotApplicable || !available
				billing.NotApplicable = paid.NotApplicable
			}
			outputHash := fmt.Sprintf("%x", sha256.Sum256([]byte(paid.Output)))
			if paid.NotApplicable && paid.Output == "" && prior.OutputHash != "" {
				outputHash = prior.OutputHash
			}
			if prior.OutputHash != "" && outputHash != prior.OutputHash {
				return memory.ErrConflict
			}
			receipt := interactiveReceipt{OutputHash: outputHash, Kind: "interactive_invocation", InvocationID: paid.InvocationID, Available: available, Billing: &billing, Searches: append([]string(nil), paid.Searches...)}
			outcome := "returned"
			if paid.CallErrorCode != "" {
				outcome = "failed"
			}
			if paid.CallErrorCode == modelcall.ErrOutcomeUnknown.Error() {
				outcome = "unknown"
			}
			var originalUsage modelUsage
			if err := tx.QueryRow(persist, `SELECT actual_mode->'originalUsage' FROM model_calls WHERE owner_id=$1 AND id=$2`, string(r.OwnerID), string(paid.InvocationID)).Scan(&originalUsage); err != nil {
				return err
			}
			if originalUsage.At.IsZero() {
				if err := tx.QueryRow(persist, `SELECT coalesce(finished_at,clock_timestamp()) FROM model_calls WHERE owner_id=$1 AND id=$2`, string(r.OwnerID), string(paid.InvocationID)).Scan(&originalUsage.At); err != nil {
					return err
				}
				if _, err := tx.Exec(persist, `UPDATE model_calls SET actual_mode=jsonb_set(actual_mode,'{originalUsage}',$3::jsonb) WHERE owner_id=$1 AND id=$2`, string(r.OwnerID), string(paid.InvocationID), asJSON(originalUsage)); err != nil {
					return err
				}
			}
			usageID := originalUsage.ID
			if usageID == "" {
				usageID = memory.ID(paid.Reservation)
			}
			var usage any = string(usageID)
			if paid.CallErrorCode == "provider_unavailable" {
				usage = nil
			}
			_, err = tx.Exec(persist, `UPDATE model_calls SET outcome=$3,error_code=$4,finished_at=coalesce(finished_at,clock_timestamp()),updated_at=clock_timestamp(),usage_id=$5,result_receipt=$6,actual_mode=actual_mode||jsonb_build_object('usageComplete',$7::boolean),accounting_state=CASE WHEN accounting_state IN ('settled','held') THEN accounting_state ELSE 'pending' END WHERE owner_id=$1 AND id=$2`, string(r.OwnerID), string(paid.InvocationID), outcome, paid.CallErrorCode, usage, asJSON(receipt), outcome != "unknown")
			return err
		})
		cancel()
		if err == nil {
			a.store.pendingInteractive.Delete(paid.InvocationID)
			return nil
		}
		if errors.Is(err, memory.ErrConflict) || errors.Is(err, memory.ErrInvalid) {
			return err
		}
		select {
		case <-ctx.Done():
			return err
		case <-time.After(time.Second):
		}
	}
}

func (a interactiveCalls) Record(ctx context.Context, r modelcall.Request, paid *modelcall.PaidResult) error {
	persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), modelcall.PersistenceTimeout)
	defer cancel()
	var usage modelUsage
	if err := a.store.pool.QueryRow(persist, `SELECT actual_mode->'originalUsage' FROM model_calls WHERE owner_id=$1 AND id=$2`, string(r.OwnerID), string(paid.InvocationID)).Scan(&usage); err != nil {
		return err
	}
	usage.OwnerID = r.OwnerID
	if usage.ID == "" {
		usage.ID = memory.ID(paid.Reservation)
	}
	usage.AgentID = paid.Provider
	usage.Model = paid.Model
	usage.MemoryRefs = paid.Refs
	usage.DurationMS = paid.DurationMS
	usage.InputTokens = paid.InputTokens
	usage.OutputTokens = paid.OutputTokens
	usage.Cost = paid.Cost
	usage.InputEstimated = paid.InputEstimated
	usage.OutputEstimated = paid.OutputEstimated
	usage.CostEstimated = paid.CostEstimated
	return a.store.recordUsage(persist, usage)
}
func (a interactiveCalls) Settle(ctx context.Context, r modelcall.Request, paid *modelcall.PaidResult) error {
	return (backgroundCalls{store: a.store}).Settle(ctx, r, paid)
}
func (a interactiveCalls) AccountingState(ctx context.Context, r modelcall.Request, paid *modelcall.PaidResult, state string) error {
	return (backgroundCalls{store: a.store}).AccountingState(ctx, r, paid, state)
}

// Interactive failures remain recoverable for accounting and caller policy.
// They cannot become a new invocation unless Prepare validates an explicit retry.
func (a interactiveCalls) Forget(context.Context, modelcall.Request, *modelcall.PaidResult) error {
	return nil
}

// The data owner calls this in its existing deletion transaction. Keep billing
// metadata, but remove interactive bodies and invalidate their application receipt.
func (a interactiveCalls) scrubInputsTx(ctx context.Context, tx pgx.Tx, owner memory.ID, ids []string) error {
	var exists bool
	if err := tx.QueryRow(ctx, "SELECT to_regclass('model_calls') IS NOT NULL").Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return nil
	}

	rows, err := tx.Query(ctx, `SELECT c.id::text FROM model_calls c WHERE c.owner_id=$1 AND c.input_manifest->>'version'='interactive-input-v1' AND (EXISTS(SELECT 1 FROM jsonb_array_elements(c.input_manifest->'memoryRefs') ref WHERE ref->>'id'=ANY($2::text[])) OR EXISTS(SELECT 1 FROM sources s WHERE s.owner_id=$1 AND s.id=ANY($2::uuid[]) AND s.connector IN ('desk','capture','desk-incomplete') AND lower(s.external_id)=c.execution_id::text)) FOR UPDATE`, string(owner), ids)
	if err != nil {
		return err
	}
	calls := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		calls = append(calls, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if len(calls) == 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, `DELETE FROM background_model_results WHERE owner_id=$1 AND job_id=ANY($2::uuid[])`, string(owner), calls); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE model_calls SET result_receipt=CASE WHEN result_receipt IS NULL THEN NULL ELSE jsonb_set(result_receipt - 'searches','{availableForApplication}','false'::jsonb) END,actual_mode=actual_mode||'{"inputDeleted":true}'::jsonb,updated_at=clock_timestamp() WHERE owner_id=$1 AND id=ANY($2::uuid[])`, string(owner), calls)
	return err
}

func cloneInteractivePaid(paid *modelcall.PaidResult) *modelcall.PaidResult {
	copy := *paid
	copy.Prompt = append(json.RawMessage(nil), paid.Prompt...)
	copy.Refs = append([]memory.Ref(nil), paid.Refs...)
	copy.Searches = append([]string(nil), paid.Searches...)
	if paid.FailureDetails != nil {
		details := *paid.FailureDetails
		if details.Codex != nil {
			details.Codex = details.Codex.SafeMetadata()
		}
		copy.FailureDetails = &details
	}
	if paid.DurationMS != nil {
		duration := *paid.DurationMS
		copy.DurationMS = &duration
	}
	return &copy
}
