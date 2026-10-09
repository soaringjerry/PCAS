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
	return asJSON(usePlan{Groups: keys, Depth: p.Depth})
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
func (s *Store) useModelCall(ctx, persist context.Context, scope memory.Scope, agent, instructions, prompt string, schema json.RawMessage, usage modelUsage) (ai.Result, error) {
	p, _ := s.models.Get(agent)
	reservation, err := s.reserveModelCostID(ctx, scope.OwnerID, p.Reserve(instructions+prompt), nil)
	if err != nil {
		return ai.Result{}, err
	}
	ctx = executionCallContext(ctx, usage.Purpose)
	var result ai.Result
	if schema != nil {
		result, err = s.models.GenerateSchema(ctx, agent, instructions, prompt, schema)
	} else {
		result, err = s.models.Generate(ctx, agent, instructions, prompt)
	}
	cost := result.Cost
	// Accounting survives the model deadline, but does not extend the answer's
	// budget. Late calls finish their own ledger write without holding the turn.
	billed := make(chan error, 1)
	go func() {
		accountingCtx, accountingCancel := context.WithTimeout(context.WithoutCancel(persist), 20*time.Second)
		defer accountingCancel()
		report := func(err error) {
			if err != nil {
				slog.ErrorContext(accountingCtx, "model accounting failed", "stage", usage.Purpose, "error_type", backgroundFailureReason(err))
			}
			billed <- err
		}
		if e := s.settleModelCost(accountingCtx, scope.OwnerID, reservation, cost); e != nil {
			report(e)
			return
		}
		usage.OwnerID = scope.OwnerID
		usage.AgentID = agent
		usage.Model = p.Model
		usage.DurationMS = result.DurationMS
		usage.InputTokens = result.InputTokens
		usage.OutputTokens = result.OutputTokens
		usage.InputEstimated, usage.OutputEstimated, usage.CostEstimated = result.InputEstimated, result.OutputEstimated, result.CostEstimated
		usage.Cost = cost
		if e := s.recordUsage(accountingCtx, usage); e != nil {
			report(e)
			return
		}
		report(nil)
	}()
	select {
	case e := <-billed:
		if e != nil {
			return result, e
		}
	case <-ctx.Done():
		return result, ctx.Err()
	}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}

	return result, err
}
func (s *Store) checkSecretary(ctx, persist context.Context, scope memory.Scope, c secretaryContext, prompt string, before secretaryOutput, turn string) secretaryOutput {
	if ctx.Err() != nil {
		return before
	}
	if err := pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error { return s.checkSecretaryUseContextTx(ctx, tx, scope, c) }); err != nil {
		return before
	}
	instructions := prompts.Must("secretary-selfcheck").Text()
	raw, err := s.useModelCall(ctx, persist, scope, c.Agent.ID, instructions, prompt+"\n待自查的草稿：\n"+string(asJSON(before)), secretaryCheckSchema, modelUsage{Purpose: "selfcheck", Tier: c.Tier, TurnID: turn, MemoryRefs: c.Dependencies, Plan: asJSON(usePlan{Groups: c.Use.Groups})})
	if err == nil {
		var after secretaryOutput
		after, err = parseSecretaryOutput(raw.Text)
		if err == nil {
			return constrainSecretaryCheck(before, after)
		}
	}
	slog.WarnContext(persist, "memory selfcheck fallback", "stage", "selfcheck", "error_type", secretaryErrorType("selfcheck", err))
	return before
}

const heavyUseTimeout = 3 * time.Minute
const heavyReaderBudget = 90 * time.Second

var useReaderInstructions = prompts.Must("memory-reader").Text()

var useGroupsSchema = prompts.MustSchema("use-groups").Bytes()
var useReaderSchema = prompts.MustSchema("use-reader").Bytes()

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
