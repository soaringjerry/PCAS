// Package stagecall makes the calls of the child stages of an admitted
// execution: the memory readers and the self-check of a secretary turn or a
// deputy run.
package stagecall

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/modelcall"
	"github.com/soaringjerry/PCAS/internal/prompts"
)

// Caller submits stage calls through the gateway.
type Caller struct {
	Calls *modelcall.Gateway
	// ErrorType gives the fixed category of a failure for the log. An error
	// message can contain private text and does not reach the log.
	ErrorType func(stage string, err error) string
}

// Call makes one stage call of the execution that ctx carries. The call ends
// for the caller with ctx. The gateway continues the durable record and the
// accounting of a submitted call after that; persist is the context of the
// log line of such a call.
func (c Caller) Call(ctx, persist context.Context, scope memory.Scope, agent, stage string, instructions prompts.Definition, prompt string, schema prompts.Schema, usage modelcall.Usage) (ai.Result, error) {
	request, ok := modelcall.Execution(ctx)
	if !ok || request.OwnerID != scope.OwnerID || !request.ExecutionID.Valid() {
		return ai.Result{}, memory.ErrInvalid
	}
	policy, ok := request.Policy.(modelcall.InteractivePolicy)
	if !ok {
		return ai.Result{}, memory.ErrInvalid
	}
	policy.AgentID, policy.Usage = agent, usage
	request.Policy, request.Stage, request.ProviderID = policy, stage, agent
	request.Instructions, request.Schema, request.ModeFallback = instructions, schema, true
	request.ContextBuilderVersion = "memory-use-v1"
	record, _ := json.Marshal(map[string]string{"rawPrompt": prompt})
	request.Prompt, request.ModelInput = record, prompt
	request.Refs = uniqueRefs(usage.MemoryRefs)
	type returned struct {
		paid *modelcall.PaidResult
		err  error
	}
	// The buffered result cannot hold the answer past its original deadline.
	finished := make(chan returned, 1)
	go func() {
		paid, err := c.Calls.Call(modelcall.Timed(ctx, usage.Purpose), request)
		if err != nil {
			slog.WarnContext(persist, "memory model stage failed", "stage", stage, "error_type", c.ErrorType(usage.Purpose, err))
		}
		finished <- returned{paid, err}
	}()
	select {
	case <-ctx.Done():
		return ai.Result{}, ctx.Err()
	case outcome := <-finished:
		if ctx.Err() != nil {
			return ai.Result{}, ctx.Err()
		}
		if outcome.err != nil {
			return ai.Result{}, outcome.err
		}
		paid := outcome.paid
		return ai.Result{Text: paid.Output, Searches: paid.Searches, DurationMS: paid.DurationMS, InputTokens: paid.InputTokens, OutputTokens: paid.OutputTokens, InputEstimated: paid.InputEstimated, OutputEstimated: paid.OutputEstimated, CostEstimated: paid.CostEstimated, Cost: paid.Cost}, nil
	}
}

func uniqueRefs(refs []memory.Ref) []memory.Ref {
	seen := make(map[memory.Ref]bool, len(refs))
	out := make([]memory.Ref, 0, len(refs))
	for _, ref := range refs {
		if !seen[ref] {
			seen[ref] = true
			out = append(out, ref)
		}
	}
	return out
}
