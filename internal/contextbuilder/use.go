package contextbuilder

import (
	"context"
	"sort"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// globalRequirementRunes is the room of the requirements that apply
// everywhere: about 3,000 Chinese tokens. Each one beyond it is counted.
const globalRequirementRunes = 6000

// unrestrictedScope is the scope that the model reads for a requirement that
// applies everywhere.
const unrestrictedScope = "不限范围：每轮落实"

// UseUnit is the storage of the memory that one turn or run is given, in its
// transaction. Each method reads or writes; none decides.
type UseUnit interface {
	// VisibleGroups are the groups of memory that the agent may read for the
	// item, with their member counts.
	VisibleGroups(ctx context.Context, agent workspace.Agent, thing *string) ([]workspace.StatusCardRef, error)
	// Studio is true when the project handovers are installed.
	Studio(ctx context.Context) (bool, error)
	Item(ctx context.Context, id string) (workspace.Item, error)
	ProjectHandover(ctx context.Context, project string) (workspace.ProjectHandover, error)
	// ProjectGroups are the group keys of the entities of a project.
	ProjectGroups(ctx context.Context, project string) ([]string, error)
	Requirements(ctx context.Context) (workspace.AssistantRequirementList, error)
	Deadlines(ctx context.Context) (workspace.DeadlineList, error)
	// Memories are the current memories with the IDs that the agent may use
	// for the item.
	Memories(ctx context.Context, agent workspace.Agent, thing *string, ids []string) ([]workspace.Memory, error)
	// Repeated are the memories said in two conversations or sources at least.
	Repeated(ctx context.Context, ids []string) ([]string, error)
	// Overflow records the count of what did not fit, by reason.
	Overflow(ctx context.Context, reason string, count int) error
}

// FinishUse completes the memory that a turn or a run is given: the groups
// the agent may read, the project's handover and group, the requirements
// that apply everywhere within their room (confirmed first, then repeated,
// then the latest), the remembered dates, and the recalled memories in
// their order. A requirement with a scope reaches the input only when recall
// finds it. Each memory given is a dependency of the input.
func FinishUse(ctx context.Context, u UseUnit, agent workspace.Agent, thing *string, ranked []workspace.Memory, use *UseContext) error {
	if !use.Installed {
		use.Supplemental = ranked
		for _, m := range ranked {
			use.Dependencies = append(use.Dependencies, claimRef(m))
		}
		return nil
	}
	visible, err := u.VisibleGroups(ctx, agent, thing)
	if err != nil {
		return err
	}
	use.Index = visible
	if thing != nil {
		if err := projectContext(ctx, u, *thing, use); err != nil {
			return err
		}
	}
	requirements, err := u.Requirements(ctx)
	if err != nil {
		return err
	}
	deadlines, err := u.Deadlines(ctx)
	if err != nil {
		return err
	}
	ids := []string{}
	for _, r := range requirements.Items {
		ids = append(ids, r.MemoryID)
	}
	for _, d := range deadlines.Items {
		ids = append(ids, d.MemoryID)
	}
	for _, m := range ranked {
		ids = append(ids, m.ID)
	}
	ms, err := u.Memories(ctx, agent, thing, ids)
	if err != nil {
		return err
	}
	byID := map[string]workspace.Memory{}
	for _, m := range ms {
		byID[m.ID] = m
	}
	repeatedIDs, err := u.Repeated(ctx, ids)
	if err != nil {
		return err
	}
	repeated := map[string]bool{}
	for _, id := range repeatedIDs {
		repeated[id] = true
	}
	sort.SliceStable(requirements.Items, func(i, j int) bool {
		a, b := byID[requirements.Items[i].MemoryID], byID[requirements.Items[j].MemoryID]
		if (a.Confirmation == "confirmed") != (b.Confirmation == "confirmed") {
			return a.Confirmation == "confirmed"
		}
		if repeated[a.ID] != repeated[b.ID] {
			return repeated[a.ID]
		}
		if a.ExpressedAt != b.ExpressedAt {
			return a.ExpressedAt > b.ExpressedAt
		}
		return a.ID < b.ID
	})
	use.RequirementScopes = map[string]string{}
	seen := map[string]bool{}
	accept := func(m workspace.Memory) {
		use.Dependencies = append(use.Dependencies, claimRef(m))
		seen[m.ID] = true
	}
	used, omitted, scoped := 0, 0, 0
	for _, r := range requirements.Items {
		m, ok := byID[r.MemoryID]
		if !ok {
			continue
		}
		if !r.Unrestricted {
			// Handing every scoped requirement to the model each turn (335 on
			// the live library, 42,000 characters) buried the user's sentence
			// and had old one-off requests carried out.
			scoped++
			continue
		}
		n := len([]rune(m.Text))
		if used+n > globalRequirementRunes {
			omitted++
			continue
		}
		used += n
		use.RequirementScopes[m.ID] = unrestrictedScope
		use.Rules = append(use.Rules, m)
		accept(m)
	}
	for _, overflow := range []struct {
		reason string
		count  int
	}{{"scoped_requirement_left_to_recall", scoped}, {"global_requirement_char_budget", omitted}} {
		if overflow.count > 0 {
			if err := u.Overflow(ctx, overflow.reason, overflow.count); err != nil {
				return err
			}
		}
	}
	use.DeadlineMemories = map[string]workspace.Memory{}
	use.DeadlineStates = map[string]string{}
	for _, d := range deadlines.Items {
		if m, ok := byID[d.MemoryID]; ok {
			use.Deadlines = append(use.Deadlines, d.Deadline)
			use.DeadlineStates[d.ID] = d.DateStatus
			use.DeadlineMemories[m.ID] = m
			accept(m)
		}
	}
	// Requirements and fixed arrangements never compete with recall slots.
	for _, m := range ranked {
		if v, ok := byID[m.ID]; ok && !seen[m.ID] {
			use.Supplemental = append(use.Supplemental, v)
			accept(v)
		}
	}
	use.Groups = ChooseGroups(use.Index, ranked, len(use.Index))
	if use.ProjectGroup != "" {
		use.Groups = append([]string{use.ProjectGroup}, use.Groups...)
	}
	use.Dependencies = uniqueRefs(use.Dependencies)
	return nil
}

// projectContext gives the use the handover and the group of the item's
// project.
func projectContext(ctx context.Context, u UseUnit, thing string, use *UseContext) error {
	installed, err := u.Studio(ctx)
	if err != nil || !installed {
		return err
	}
	item, err := u.Item(ctx, thing)
	if err != nil {
		return err
	}
	project := item.ProjectID
	if item.Kind == "project" {
		project = item.ID
	}
	if project == "" {
		return nil
	}
	h, err := u.ProjectHandover(ctx, project)
	if err != nil {
		return err
	}
	use.ProjectHandover = &h
	use.Ready = use.Ready || h.WrittenAt != nil
	keys, err := u.ProjectGroups(ctx, project)
	if err != nil {
		return err
	}
	for _, key := range keys {
		for _, g := range use.Index {
			if g.Key == key {
				use.ProjectGroup = key
				break
			}
		}
	}
	return nil
}

// ChooseGroups proposes at most limit groups by the recalled memories that
// belong to them, by reciprocal rank. The rule group is never proposed: its
// requirements have their own place. Named groups and depth are chosen by
// the model.
func ChooseGroups(index []workspace.StatusCardRef, ranked []workspace.Memory, limit int) []string {
	scores := map[string]float64{}
	for i, m := range ranked {
		seen := map[string]bool{}
		add := func(k string) {
			if !seen[k] {
				scores[k] += 1 / float64(61+i)
				seen[k] = true
			}
		}
		for _, g := range m.Groups {
			add("entity:" + g.EntityID)
		}
		for _, g := range m.Mentions {
			add("entity:" + g.EntityID)
		}
		if oneOf(m.Category, "identity", "taste", "rule", "goal") {
			add("self:" + m.Category)
		}
	}
	candidates := append([]workspace.StatusCardRef{}, index...)
	sort.SliceStable(candidates, func(i, j int) bool {
		if scores[candidates[i].Key] == scores[candidates[j].Key] {
			return candidates[i].Key < candidates[j].Key
		}
		return scores[candidates[i].Key] > scores[candidates[j].Key]
	})
	out := []string{}
	for _, g := range candidates {
		if g.Key != "self:rule" && scores[g.Key] > 0 && len(out) < limit {
			out = append(out, g.Key)
		}
	}
	return out
}

func claimRef(m workspace.Memory) memory.Ref {
	return memory.Ref{ID: memory.ID(m.ID), Version: m.Version, Kind: memory.ClaimKind}
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
