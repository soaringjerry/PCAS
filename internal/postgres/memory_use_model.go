package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"log/slog"
	"strings"
	"time"

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/modelcall"
	"github.com/soaringjerry/PCAS/internal/prompts"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

type usePlan struct {
	Depth     string   `json:"depth,omitempty"`
	Mentioned []string `json:"mentioned,omitempty"`
	Adopted   []string `json:"adopted,omitempty"`
	Groups    []string `json:"groups"`
}

func safeUsePlan(raw json.RawMessage) any {
	var p usePlan
	if json.Unmarshal(raw, &p) != nil {
		return nil
	}
	keys := []string{}
	for _, key := range p.Groups {
		if oneOf(key, "self:identity", "self:taste", "self:rule", "self:goal") || strings.HasPrefix(key, "entity:") && memory.ID(strings.TrimPrefix(key, "entity:")).Valid() {
			if !oneOf(key, keys...) {
				keys = append(keys, key)
			}
		}
	}
	if !oneOf(p.Depth, "light", "medium", "heavy") {
		p.Depth = ""
	}
	return json.RawMessage(asJSON(usePlan{Groups: keys, Depth: p.Depth}))
}
func secretaryNeedsCheck(out secretaryOutput) bool {
	for _, a := range out.Actions {
		if containsAny(a.Op, "delete", "send") {
			return true
		}
	}
	return false
}

// Slots stay in their original order so N references cannot be rebound. A
// rejected revision keeps the original slot; the checker cannot add authority.
func constrainSecretaryCheck(before, after secretaryOutput) secretaryOutput {
	actions := append([]secretaryAction{}, before.Actions...)
	for i := len(after.Actions); i < len(actions); i++ {
		actions[i].selfcheckDropped = true
	}
	for i, a := range after.Actions {
		if i >= len(actions) {
			break
		}
		old := before.Actions[i]
		if a.Op == "skip" {
			actions[i].selfcheckDropped = true
			continue
		}
		if old.Op == "delegate" && (old.Kind != a.Kind || pointerValue(old.DocumentID) != pointerValue(a.DocumentID) || intPointerValue(old.BaseVersion) != intPointerValue(a.BaseVersion)) || old.Op == "add_steps" && len(a.Steps) > len(old.Steps) || old.Op == "close_date" && old.As != a.As {
			continue
		}
		if old.parseErr != nil || a.parseErr != nil || old.Op != a.Op || old.Ref != a.Ref || pointerValue(old.Project) != pointerValue(a.Project) {
			continue
		}
		// Changing a project in an update redirects the action to another object.
		if string(old.Set["project"]) != string(a.Set["project"]) {
			continue
		}
		actions[i] = a
	}
	after.Actions = actions
	after.MemoryPlan = before.MemoryPlan
	after.Remember = before.Remember
	return after
}
func (s *Store) useModelCall(ctx, persist context.Context, scope memory.Scope, agent, stage string, instructions prompts.Definition, prompt string, schema prompts.Schema, usage modelUsage) (ai.Result, error) {
	request, ok := ctx.Value(interactiveExecutionKey{}).(modelcall.Request)
	if !ok || request.OwnerID != scope.OwnerID || !request.ExecutionID.Valid() {
		return ai.Result{}, memory.ErrInvalid
	}
	policy, ok := request.Policy.(interactiveCallPolicy)
	if !ok {
		return ai.Result{}, memory.ErrInvalid
	}
	policy.AgentID, policy.Usage = agent, usage
	request.Policy, request.Stage, request.ProviderID = policy, stage, agent
	request.Instructions, request.Schema = instructions, schema
	request.ContextBuilderVersion = "memory-use-v1"
	request.Prompt = asJSON(map[string]string{"rawPrompt": prompt})
	request.Refs = uniqueRefs(usage.MemoryRefs)
	type returned struct {
		paid *modelcall.PaidResult
		err  error
	}
	// The buffered result cannot hold the answer past its original deadline.
	// The gateway continues durable persistence and accounting after cancellation.
	finished := make(chan returned, 1)
	go func() {
		paid, err := s.calls.Call(executionCallContext(ctx, usage.Purpose), request)
		if err != nil {
			slog.WarnContext(persist, "memory model stage failed", "stage", stage, "error_type", secretaryErrorType(usage.Purpose, err))
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
func (s *Store) checkSecretary(ctx, persist context.Context, scope memory.Scope, c secretaryContext, prompt string, before secretaryOutput, turn string) (secretaryOutput, error) {
	if ctx.Err() != nil {
		return before, ctx.Err()
	}
	if err := pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error { return s.checkSecretaryUseContextTx(ctx, tx, scope, c) }); err != nil {
		return before, err
	}
	raw, err := s.useModelCall(ctx, persist, scope, c.Agent.ID, "selfcheck", prompts.Must("secretary-selfcheck"), prompt+"\n待自查的草稿：\n"+string(asJSON(before)), prompts.MustSchema("secretary-check"), modelUsage{Purpose: "selfcheck", Tier: c.Tier, TurnID: turn, MemoryRefs: c.Dependencies, Plan: asJSON(usePlan{Groups: c.Use.Groups})})
	if err == nil {
		var after secretaryOutput
		after, err = parseSecretaryOutput(raw.Text)
		if err == nil {
			return constrainSecretaryCheck(before, after), nil
		}
	}
	slog.WarnContext(persist, "memory selfcheck fallback", "stage", "selfcheck", "error_type", secretaryErrorType("selfcheck", err))
	return before, err
}

const heavyUseTimeout = 3 * time.Minute
const heavyReaderBudget = 90 * time.Second

func memoryRefs(ms []workspace.Memory) []memory.Ref {
	refs := []memory.Ref{}
	for _, m := range ms {
		refs = append(refs, memory.Ref{ID: memory.ID(m.ID), Version: m.Version, Kind: memory.ClaimKind})
	}
	return uniqueRefs(refs)
}
func writeReaderMemories(ms []workspace.Memory, loc *time.Location) string {
	if loc == nil {
		loc = time.UTC
	}
	var b strings.Builder
	for _, m := range ms {
		fmt.Fprintf(&b, "[%s@%d / trust=%s] %s\n", m.ID, m.Version, m.Trust, m.Text+memoryPromptSuffix(m, loc))
	}
	return b.String()
}

// Registered bytes retain the skip-action slot and original field order.
var secretaryCheckSchema = prompts.MustSchema("secretary-check").Bytes()

func intPointerValue(v *int) int {
	if v == nil {
		return 0
	}
	return *v
}
