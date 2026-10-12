package secretary

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/soaringjerry/PCAS/internal/contextbuilder"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// Limits are the time limits of one turn.
type Limits struct {
	// Model is the longest time of one model call.
	Model time.Duration
	// HeavyReader is how long after the start of the turn the reading before
	// the answer can last.
	HeavyReader time.Duration
	// Total is the wall clock limit of a turn with heavy reading. The last
	// ten seconds of it are kept for the writes.
	Total time.Duration
}

// Asking is the state of a turn while the model is asked. The reads and the
// answer change it.
type Asking struct {
	// Tier is how much memory the turn reads: light, medium, or heavy.
	Tier string
	Use  contextbuilder.UseContext
	// Sent are the memories that the turn showed, by their alias.
	Sent map[string]workspace.Memory
	// Input is the model input.
	Input        string
	Dependencies []memory.Ref
	// Text is the owner's sentence, and This is the open item or nil.
	Text     string
	This     *workspace.Item
	Location *time.Location
	// Started is the start of the turn.
	Started time.Time
}

// Model is what the turn asks: the readers and the model calls. Each call
// is recorded and paid by the caller's gateway.
type Model interface {
	// HeavyRead reads groups of memory for the task. A zero deadline means
	// the caller's own limit.
	HeavyRead(ctx context.Context, deadline time.Time, task string, state *Asking, stage string) (picked []workspace.Memory, refs []memory.Ref, groups []string)
	// Prepared marks the end of the preparation, and Reading adds reading
	// time to it. They feed the timing record only.
	Prepared()
	Reading(d time.Duration)
	// Generate makes the answer call. stage names where a failure happened.
	// failure is a failure of the turn that falls back to keeping the
	// sentence; err is an error that ends the request.
	Generate(ctx context.Context, state *Asking) (text, stage string, failure, err error)
	// Check lets the model check its answer within the given time.
	Check(ctx context.Context, timeout time.Duration, state *Asking, answer Output) (Output, error)
	// SetTier records the tier that the turn reached.
	SetTier(ctx context.Context, tier string) error
}

// Asked is the result of asking the model.
type Asked struct {
	Answer Output
	// Failure is the failure of the turn, or nil. Stage names its place.
	Failure error
	Stage   string
	// Generated is true when the answer call was made, and Checked when the
	// self-check was tried. CheckFailure is the failure of the self-check.
	Generated, Checked bool
	CheckFailure       error
}

// TierFor is the tier that a turn can read at: heavy reading needs the
// current state to be ready.
func TierFor(tier string, ready bool) string {
	if tier == "heavy" && !ready {
		return "medium"
	}
	return tier
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

// Ask asks the model for the answer of a turn. A heavy turn reads before
// the answer. An answer that asks for more depth is followed by one more
// reading. An answer that is not light, that lacks key information, or that
// deletes or sends is checked by the model before it is applied.
// An answer that cannot be read as an object is kept as text without
// actions.
func Ask(ctx context.Context, m Model, st *Asking, limits Limits) (Asked, error) {
	out := Asked{}
	if st.Tier == "heavy" && st.Use.Ready {
		picked, refs, groups := m.HeavyRead(ctx, st.Started.Add(limits.HeavyReader), HeavyTaskText(st.Text, st.This), st, "reader:prepare")
		st.Use.Groups = groups
		st.Dependencies = uniqueRefs(append(st.Dependencies, refs...))
		st.Input += AddRead(st.Sent, picked, st.Location, true)
	}
	m.Prepared()
	modelStarted := time.Now()
	out.Generated = true
	text, stage, failure, err := m.Generate(ctx, st)
	out.Stage = stage
	if err != nil {
		return out, err
	}
	if failure != nil {
		out.Failure = failure
		return out, nil
	}
	answer, parseErr := ParseOutput(text)
	if parseErr != nil {
		slog.WarnContext(ctx, "secretary output fallback", "stage", "parse", "error_type", ErrorType("parse", parseErr))
		// A completed model call still answered. Keep it as text, with
		// no actions, citations or questions from a partial decode.
		out.Answer = Output{Reply: strings.TrimSpace(text)}
		return out, nil
	}
	slog.InfoContext(ctx, "secretary output parsed", "stage", "parse", "error_type", "none")
	if answer.MemoryPlan != nil && oneOf(answer.MemoryPlan.Depth, "medium", "heavy") {
		readHeavy := st.Tier != "heavy"
		st.Tier = TierFor(answer.MemoryPlan.Depth, st.Use.Ready)
		if st.Tier == "heavy" && readHeavy {
			st.Use.RequiredGroups = PlannedGroups(answer.MemoryPlan.Groups, st.Use.Index)
			st.Use.Selected = true
			readingStarted := time.Now()
			picked, refs, groups := m.HeavyRead(ctx, time.Time{}, st.Text, st, "reader:plan")
			m.Reading(time.Since(readingStarted))
			st.Use.Groups = groups
			st.Dependencies = uniqueRefs(append(st.Dependencies, refs...))
			st.Input += AddRead(st.Sent, picked, st.Location, false)
		}
	}
	if st.Tier == "light" && (answer.MissingKeyInfo || NeedsCheck(answer)) {
		st.Tier = "medium"
	}
	out.Answer = answer
	if st.Tier != "light" {
		// All heavy stages fit the same wall clock budget.
		remaining := limits.Total - 10*time.Second - time.Since(st.Started)
		if st.Tier == "heavy" && remaining < time.Since(modelStarted) {
			modelStarted = time.Now().Add(-remaining)
		}
		out.Checked = true
		out.Answer, out.CheckFailure = m.Check(ctx, min(time.Since(modelStarted), limits.Model), st, answer)
		if err := m.SetTier(ctx, st.Tier); err != nil {
			return out, err
		}
	}
	return out, nil
}
