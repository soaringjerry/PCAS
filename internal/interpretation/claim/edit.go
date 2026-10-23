package claim

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// EditUnit is the storage of one memory command of the owner, in the
// transaction of the command. Each method reads or writes; none decides.
type EditUnit interface {
	// Active fails when the memory is gone.
	Active(ctx context.Context, id string) error
	// Delete removes the memory and blocks its reimport; includeSources
	// removes its originals too.
	Delete(ctx context.Context, id string, includeSources bool) error
	TogglePin(ctx context.Context, id string) error
	AgentExists(ctx context.Context, id string) (bool, error)
	// SetVisibility gives the memory to exactly these agents.
	SetVisibility(ctx context.Context, id string, agents []string) error
	Read(ctx context.Context, id string) (memory.Claim, error)
	// Correct writes the next version of the memory as the owner's.
	Correct(ctx context.Context, request memory.CorrectRequest) (memory.Ref, error)
	Sample(ctx context.Context, sample workspace.Sample) error
	RecordUse(ctx context.Context, event memory.UseEvent) error
}

// Edit applies a memory command of the owner: delete, pin, set who may
// read it, edit, or confirm. An edit or a confirmation makes the memory the
// owner's own, confirmed and said directly. An edit is kept as a sample of a
// correction; a confirmation is a use of the memory.
func Edit(ctx context.Context, u EditUnit, c workspace.Command, now time.Time) error {
	if !memory.ID(c.ID).Valid() {
		return memory.ErrInvalid
	}
	if err := u.Active(ctx, c.ID); err != nil {
		return err
	}
	switch c.Type {
	case "deleteMemory":
		return u.Delete(ctx, c.ID, c.IncludeSources)
	case "pinMemory":
		return u.TogglePin(ctx, c.ID)
	case "setMemoryVisibility":
		for _, id := range c.AgentIDs {
			exists, err := u.AgentExists(ctx, id)
			if err != nil {
				return err
			}
			if !exists {
				return memory.ErrInvalid
			}
		}
		return u.SetVisibility(ctx, c.ID, c.AgentIDs)
	}
	claim, err := u.Read(ctx, c.ID)
	if err != nil {
		return err
	}
	prior := claim
	if c.Type == "editMemory" {
		if strings.TrimSpace(c.Text) == "" || len(c.Text) > 1<<20 {
			return memory.ErrInvalid
		}
		claim.Value, _ = json.Marshal(c.Text)
	}
	claim.Confirmation = "confirmed"
	claim.Acquisition = "direct"
	ref, err := u.Correct(ctx, memory.CorrectRequest{Target: claim.Ref, Replacement: claim, Reason: c.Reason})
	if err != nil {
		return err
	}
	if c.Type == "editMemory" {
		var before, after string
		_ = json.Unmarshal(prior.Value, &before)
		_ = json.Unmarshal(claim.Value, &after)
		return u.Sample(ctx, workspace.Sample{ID: string(memory.NewID()), Kind: "correction", Prompt: before, Response: after, Origin: workspace.Origin{Label: "记忆纠正", MemoryID: c.ID}, Version: ref.Version, State: "candidate", Epistemic: "confirmed", CreatedAt: now.UTC().Format(time.RFC3339Nano)})
	}
	return u.RecordUse(ctx, memory.UseEvent{Ref: ref, EventID: c.RequestID, Kind: "confirmation", At: now})
}

// CheckCorrection checks the replacement of a correction and fills its
// defaults: the change is a correction unless named, a scope is empty, and
// a time is of unknown precision.
func CheckCorrection(in memory.CorrectRequest) (memory.Claim, string, error) {
	c := in.Replacement
	changeType := in.ChangeType
	if changeType == "" {
		changeType = "correction"
	}
	if !oneOf(changeType, "correction", "change", "supplement", "evidence") {
		return c, changeType, memory.ErrInvalid
	}
	if !c.SubjectID.Valid() || strings.TrimSpace(c.Predicate) == "" || len(c.Predicate) > 1<<20 || !json.Valid(c.Value) || !oneOf(c.Nature, "fact", "preference", "intention", "plan", "decision") || !oneOf(c.Acquisition, "direct", "reported", "inferred", "execution") || !oneOf(c.Confirmation, "unknown", "candidate", "adopted", "confirmed", "disputed") {
		return c, changeType, memory.ErrInvalid
	}
	if c.Scope == nil {
		c.Scope = map[string]json.RawMessage{}
	}
	if c.ValidTime.Precision == "" {
		c.ValidTime.Precision = "unknown"
	}
	if !oneOf(c.ValidTime.Precision, "unknown", "instant", "day", "month", "year", "range") || c.ValidTime.From != nil && c.ValidTime.To != nil && c.ValidTime.From.After(*c.ValidTime.To) {
		return c, changeType, memory.ErrInvalid
	}
	return c, changeType, nil
}
