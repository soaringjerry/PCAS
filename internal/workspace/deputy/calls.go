package deputy

import (
	"context"
	"encoding/json"

	"github.com/soaringjerry/PCAS/internal/contextbuilder"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/modelcall"
	"github.com/soaringjerry/PCAS/internal/prompts"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// Snapshot is the saved input of the answer call of a run. A resumed run
// reads it; it never builds a new brief from current memory.
type Snapshot struct {
	Run         workspace.Run            `json:"run"`
	RawPrompt   string                   `json:"rawPrompt,omitempty"`
	Coverage    *contextbuilder.Coverage `json:"coverage,omitempty"`
	MemoryReady bool                     `json:"memoryReady,omitempty"`
}

func asJSON(v any) json.RawMessage {
	raw, _ := json.Marshal(v)
	return raw
}

type groups struct {
	Groups []string `json:"groups"`
}

// AnswerRequest is the request of the answer call of a run. token is the
// token of the run's lease. A revision has its own instructions and output
// schema.
func AnswerRequest(owner memory.ID, token string, snapshot Snapshot) modelcall.Request {
	run := snapshot.Run
	instruction := prompts.Must("deputy")
	var schema prompts.Schema
	if run.Kind == "revise" {
		instruction = prompts.Must("document-revise")
		schema = prompts.MustSchema("document-revise-output")
	}
	return modelcall.Request{
		OwnerID: owner, ExecutionID: memory.ID(run.ID), RootExecutionID: memory.ID(run.ID), Function: "deputy", Stage: "answer",
		ProviderID: run.AgentID, Instructions: instruction, Schema: schema, Search: true, ModeFallback: true, ContextBuilderVersion: "deputy-answer-v1",
		Prompt: asJSON(snapshot), ModelInput: snapshot.RawPrompt, Refs: run.ContextVersions,
		Policy: modelcall.InteractivePolicy{Kind: "deputy", Token: token, Origin: run.CreatedAt, AgentID: run.AgentID, ThingID: run.ThingID, BudgetOwner: "deputy_run",
			Usage: modelcall.Usage{Purpose: "deputy", RunID: run.ID, Tier: run.MemoryTier, Plan: asJSON(groups{run.MemoryGroups})}},
	}
}

// SelfcheckRequest is the request of the call that checks the draft of a run.
func SelfcheckRequest(owner memory.ID, token string, run workspace.Run, draft string) modelcall.Request {
	input := SelfcheckInput(run.Brief, draft)
	return modelcall.Request{
		OwnerID: owner, ExecutionID: memory.ID(run.ID), RootExecutionID: memory.ID(run.ID),
		Function: "deputy", Stage: "selfcheck", ProviderID: run.AgentID,
		Instructions: prompts.Must("deputy-selfcheck"), ContextBuilderVersion: "deputy-selfcheck-v1",
		Prompt: asJSON(map[string]string{"rawPrompt": input}), ModelInput: input,
		Refs: run.ContextVersions,
		Policy: modelcall.InteractivePolicy{
			Kind: "deputy", Token: token, Origin: run.CreatedAt,
			AgentID: run.AgentID, ThingID: run.ThingID,
			Usage: modelcall.Usage{Purpose: "selfcheck", Tier: run.MemoryTier, RunID: run.ID, MemoryRefs: run.ContextVersions, Plan: asJSON(groups{run.MemoryGroups})},
		},
	}
}

// Calls makes the model calls of a run through the gateway. Each call is
// recorded and paid by the gateway; its time is added to the run's timing
// under the purpose of the call.
type Calls struct {
	Gateway *modelcall.Gateway
}

// Answer makes the answer call of a run.
func (c Calls) Answer(ctx context.Context, request modelcall.Request) (*modelcall.PaidResult, error) {
	return c.Gateway.Call(modelcall.Timed(ctx, "answer"), request)
}

// Selfcheck makes the call that checks the draft of a run.
func (c Calls) Selfcheck(ctx context.Context, request modelcall.Request) (*modelcall.PaidResult, error) {
	return c.Gateway.Call(modelcall.Timed(ctx, "selfcheck"), request)
}
