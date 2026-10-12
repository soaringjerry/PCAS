package postgres

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/soaringjerry/PCAS/internal/contextbuilder"
	"github.com/soaringjerry/PCAS/internal/postgres/kernel"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

type memoryTierKey struct{}

// WithMemoryTier fixes the tier for offline evaluation. There is no user switch.
func WithMemoryTier(ctx context.Context, tier string) context.Context {
	return context.WithValue(ctx, memoryTierKey{}, tier)
}
func memoryTier(ctx context.Context, text, fallback string) string {
	if tier, _ := ctx.Value(memoryTierKey{}).(string); kernel.OneOf(tier, "light", "medium", "heavy") {
		return tier
	}
	return fallback
}
func memoryTierForStatus(tier string, ready bool) string {
	if tier == "heavy" && !ready {
		return "medium"
	}
	return tier
}

type (
	useCoverage = contextbuilder.Coverage
	useContext  = contextbuilder.UseContext
)

func (s *Store) startUseContextTx(ctx context.Context, tx pgx.Tx, scope memory.Scope) (useContext, error) {
	u := useContext{Coverage: &useCoverage{}}
	// Legacy migration fixtures have no live library columns in their schema.
	// Do not accidentally resolve a newer view from the public search path.
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_attribute a JOIN pg_class c ON c.oid=a.attrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema() AND c.relname='claim_revisions' AND a.attname='category' AND NOT a.attisdropped)`).Scan(&u.Installed); err != nil {
		return u, err
	}
	if !u.Installed {
		return u, nil
	}
	var err error
	u.Handover, err = s.HandoverTx(ctx, tx, scope)
	if err != nil {
		return u, err
	}
	groups, err := s.memoryGroupsTx(ctx, tx, scope)
	if err != nil {
		return u, err
	}
	for _, g := range groups.Items {
		u.Index = append(u.Index, workspace.StatusCardRef{Key: g.Key, Kind: g.Kind, Name: g.Name, Count: g.Count})
	}
	u.Ready = strings.TrimSpace(u.Handover.Body) != ""
	return u, nil
}

// Retrieval proposes groups by membership. Named groups and depth are selected
// semantically in the existing secretary/reader model call.
func chooseUseGroups(text string, index []workspace.StatusCardRef, ranked []workspace.Memory, aliases map[string][]string, limit int, namedOnly bool) []string {
	if namedOnly {
		return []string{}
	}
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
		if kernel.OneOf(m.Category, "identity", "taste", "rule", "goal") {
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

// Access, exclusions and destination scope are checked in one set, not per memory.
func (s *Store) useMemoriesTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, agent workspace.Agent, thing *string, ids []string) ([]workspace.Memory, error) {
	// Ungrouped retrieved claims use the same policy; the membership view is only
	// used for group enumeration, not as an access condition.
	allowed, err := kernel.QueryDocuments[string](ctx, tx, `SELECT to_jsonb(c.claim_id::text) FROM claim_revisions c JOIN memory_records r ON(r.owner_id,r.id,r.version)=(c.owner_id,c.claim_id,c.version)
 WHERE c.owner_id=$1 AND c.claim_id=ANY($2::uuid[]) AND NOT EXISTS(SELECT 1 FROM context_exclusions ex WHERE ex.owner_id=c.owner_id AND ex.thing_id=$3::uuid AND ex.memory_id=c.claim_id)
 AND ($3::uuid IS NULL OR coalesce(c.scope->>'project_id','')='' OR c.scope->>'project_id'=coalesce((SELECT CASE WHEN w.kind='project' THEN w.id::text ELSE w.project_id::text END FROM work_items w WHERE w.owner_id=$1 AND w.id=$3),''))`, string(scope.OwnerID), ids, thing)
	if err != nil {
		return nil, err
	}
	if allowed == nil {
		allowed = []string{}
	}
	ms, err := s.readMemoriesTx(ctx, tx, memory.Scope{OwnerID: scope.OwnerID, PrincipalID: agent.ID}, true, memoryReadOptions{useCurrent: true, ids: allowed})
	if err != nil {
		return nil, err
	}
	out := []workspace.Memory{}
	for _, m := range ms {
		if contextbuilder.MemoryAllowed(m, agent) {
			out = append(out, m)
		}
	}
	return out, nil
}

const globalRequirementRunes = 6000 // Dedicated space: about 3k Chinese tokens; overflow is counted.
func (s *Store) finishUseContextTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, agent workspace.Agent, thing *string, text string, ranked []workspace.Memory, u *useContext) error {
	if !u.Installed {
		u.Supplemental = ranked
		for _, m := range ranked {
			u.Dependencies = append(u.Dependencies, memory.Ref{ID: memory.ID(m.ID), Version: m.Version, Kind: memory.ClaimKind})
		}
		return nil
	}
	visible, err := kernel.QueryDocuments[workspace.StatusCardRef](ctx, tx, `WITH members AS MATERIALIZED(SELECT * FROM status_current_members WHERE owner_id=$1)
 SELECT jsonb_build_object('key',m.key,'kind',m.kind,'name',m.name,'count',count(*)) FROM members m JOIN claim_revisions c ON(c.owner_id,c.claim_id,c.version)=(m.owner_id,m.claim_id,m.claim_version)
 WHERE m.owner_id=$1 AND c.nature=ANY($2::text[]) AND ($3 OR c.acquisition!='inferred')
 AND EXISTS(SELECT 1 FROM record_grants g WHERE g.owner_id=m.owner_id AND g.record_id=m.claim_id AND g.principal_id=$4)
 AND NOT EXISTS(SELECT 1 FROM context_exclusions ex WHERE ex.owner_id=m.owner_id AND ex.thing_id=$5::uuid AND ex.memory_id=m.claim_id)
 AND ($5::uuid IS NULL OR coalesce(c.scope->>'project_id','')='' OR c.scope->>'project_id'=coalesce((SELECT CASE WHEN w.kind='project' THEN w.id::text ELSE w.project_id::text END FROM work_items w WHERE w.owner_id=$1 AND w.id=$5),'')) GROUP BY m.key,m.kind,m.name ORDER BY m.kind,m.name,m.key`, string(scope.OwnerID), agent.MemoryKinds, agent.IncludeInferred, agent.ID, thing)
	if err != nil {
		return err
	}
	u.Index = visible
	if thing != nil {
		installed, e := studioInstalledTx(ctx, tx)
		if e != nil {
			return e
		}
		if installed {
			item, e := getItem(ctx, tx, scope, *thing)
			if e != nil {
				return e
			}
			projectID := item.ProjectID
			if item.Kind == "project" {
				projectID = item.ID
			}
			if projectID != "" {
				h, e := projectHandoverTx(ctx, tx, scope, projectID)
				if e != nil {
					return e
				}
				u.ProjectHandover = &h
				u.Ready = u.Ready || h.WrittenAt != nil
				keys, e := kernel.QueryDocuments[string](ctx, tx, `SELECT to_jsonb('entity:'||ev.entity_id::text) FROM entity_versions ev JOIN memory_records r ON(r.owner_id,r.id,r.version)=(ev.owner_id,ev.entity_id,ev.version) WHERE ev.owner_id=$1 AND ev.disambiguation->>'work_item_id'=$2 ORDER BY ev.entity_id`, string(scope.OwnerID), projectID)
				if e != nil {
					return e
				}
				for _, key := range keys {
					for _, g := range visible {
						if g.Key == key {
							u.ProjectGroup = key
							break
						}
					}
				}
			}
		}
	}
	requirements, err := s.assistantRequirementsTx(ctx, tx, scope)
	if err != nil {
		return err
	}
	deadlines, err := s.libraryDeadlinesTx(ctx, tx, scope, workspace.DeadlineQuery{})
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
	ms, err := s.useMemoriesTx(ctx, tx, scope, agent, thing, ids)
	if err != nil {
		return err
	}
	byID := map[string]workspace.Memory{}
	for _, m := range ms {
		byID[m.ID] = m
	}
	// Repeated is evidence from distinct conversations/sources, not number of mentions.
	repeated, err := kernel.QueryDocuments[string](ctx, tx, `SELECT to_jsonb(e.target_id::text) FROM evidence e JOIN memory_records er ON(er.owner_id,er.id,er.version)=(e.owner_id,e.target_id,e.target_version) LEFT JOIN source_contexts sc ON(sc.owner_id,sc.source_id,sc.source_version)=(e.owner_id,e.source_id,e.source_version) WHERE e.owner_id=$1 AND e.target_id=ANY($2::uuid[]) AND e.stance='supports' GROUP BY e.target_id HAVING count(DISTINCT coalesce(nullif(sc.conversation_key,''),e.source_id::text))>=2`, string(scope.OwnerID), ids)
	if err != nil {
		return err
	}
	sort.SliceStable(requirements.Items, func(i, j int) bool {
		a, b := byID[requirements.Items[i].MemoryID], byID[requirements.Items[j].MemoryID]
		if (a.Confirmation == "confirmed") != (b.Confirmation == "confirmed") {
			return a.Confirmation == "confirmed"
		}
		if kernel.OneOf(a.ID, repeated...) != kernel.OneOf(b.ID, repeated...) {
			return kernel.OneOf(a.ID, repeated...)
		}
		if a.ExpressedAt != b.ExpressedAt {
			return a.ExpressedAt > b.ExpressedAt
		}
		return a.ID < b.ID
	})
	u.RequirementScopes = map[string]string{}
	seen := map[string]bool{}
	used, omitted, scoped := 0, 0, 0
	accept := func(m workspace.Memory) {
		u.Dependencies = append(u.Dependencies, memory.Ref{ID: memory.ID(m.ID), Version: m.Version, Kind: memory.ClaimKind})
		seen[m.ID] = true
	}
	for _, r := range requirements.Items {
		m, ok := byID[r.MemoryID]
		if !ok {
			continue
		}
		if r.Unrestricted {
			n := len([]rune(m.Text))
			if used+n > globalRequirementRunes {
				omitted++
				continue
			}
			used += n
			u.RequirementScopes[m.ID] = "不限范围：每轮落实"
		} else {
			// Handing every scoped requirement to the model each turn (335 on
			// the live library, 42,000 characters) buried the user's sentence
			// and had old one-off requests carried out. A scoped requirement
			// now reaches a turn only when recall finds it relevant.
			scoped++
			continue
		}
		u.Rules = append(u.Rules, m)
		accept(m)
	}
	if scoped > 0 {
		if err := stageEventTx(ctx, tx, scope.OwnerID, "secretary", "overflow", "scoped_requirement_left_to_recall", scoped); err != nil {
			return err
		}
	}
	if omitted > 0 {
		if err := stageEventTx(ctx, tx, scope.OwnerID, "secretary", "overflow", "global_requirement_char_budget", omitted); err != nil {
			return err
		}
	}
	u.DeadlineMemories = map[string]workspace.Memory{}
	u.DeadlineStates = map[string]string{}
	for _, d := range deadlines.Items {
		if m, ok := byID[d.MemoryID]; ok {
			u.Deadlines = append(u.Deadlines, d.Deadline)
			u.DeadlineStates[d.ID] = d.DateStatus
			u.DeadlineMemories[m.ID] = m
			accept(m)
		}
	}
	// Requirements and fixed arrangements never compete with retrieval slots.
	for _, m := range ranked {
		if v, ok := byID[m.ID]; ok && !seen[m.ID] {
			u.Supplemental = append(u.Supplemental, v)
			accept(v)
		}
	}
	u.Groups = chooseUseGroups(text, u.Index, ranked, nil, len(u.Index), false)
	if u.ProjectGroup != "" {
		u.Groups = append([]string{u.ProjectGroup}, u.Groups...)
	}
	u.Dependencies = kernel.UniqueRefs(u.Dependencies)
	return nil
}

// Compatibility helper; semantic applicability is exclusively model-owned.
func ruleRelevance(applies, text string) int {
	if strings.TrimSpace(applies) == "" {
		return 1
	}
	return 0
}

func (s *Store) prepareUseOwner(ctx context.Context, scope memory.Scope) (bool, error) {
	ready := false
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.ensureOwner(ctx, tx, scope); err != nil {
			return err
		}
		u, err := s.startUseContextTx(ctx, tx, scope)
		if err != nil {
			return err
		}
		ready = u.Ready
		if !ready {
			return nil
		}
		settings, err := kernel.QueryDocument[workspace.Settings](ctx, tx, "SELECT settings FROM workspace_owners WHERE owner_id=$1 FOR SHARE", string(scope.OwnerID))
		if err != nil {
			return err
		}
		_, err = time.LoadLocation(settings.Timezone)
		return err
	})
	return ready, err
}
