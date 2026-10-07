package postgres

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

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
	if tier, _ := ctx.Value(memoryTierKey{}).(string); oneOf(tier, "light", "medium", "heavy") {
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
func containsAny(text string, words ...string) bool {
	for _, w := range words {
		if strings.Contains(text, w) {
			return true
		}
	}
	return false
}

type useCoverage struct{ Skipped []string }
type useContext struct {
	Tier                   string
	Location               *time.Location
	Handover               workspace.Handover
	Index                  []workspace.StatusCardRef // Compatibility DTO; populated from the live directory, never cards.
	Cards                  []workspace.StatusCard    // Retained for old offline fixtures only.
	Rules                  []workspace.Memory
	RequirementScopes      map[string]string
	DeadlineMemories       map[string]workspace.Memory
	Deadlines              []workspace.Deadline
	DeadlineStates         map[string]string
	Supplemental           []workspace.Memory
	Groups, RequiredGroups []string
	Dependencies           []memory.Ref
	Installed              bool
	Ready                  bool
	Coverage               *useCoverage
	Selected               bool
}

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
func useMemoryAllowed(m workspace.Memory, a workspace.Agent) bool {
	return m.Retired == "" && oneOf(m.Kind, a.MemoryKinds...) && (a.IncludeInferred || m.Trust != "inferred" && m.Acquisition != "inferred")
}

// Access, exclusions and destination scope are checked in one set, not per memory.
func (s *Store) useMemoriesTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, agent workspace.Agent, thing *string, ids []string) ([]workspace.Memory, error) {
	// Ungrouped retrieved claims use the same policy; the membership view is only
	// used for group enumeration, not as an access condition.
	allowed, err := queryDocuments[string](ctx, tx, `SELECT to_jsonb(c.claim_id::text) FROM claim_revisions c JOIN memory_records r ON(r.owner_id,r.id,r.version)=(c.owner_id,c.claim_id,c.version)
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
		if useMemoryAllowed(m, agent) {
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
	visible, err := queryDocuments[workspace.StatusCardRef](ctx, tx, `WITH members AS MATERIALIZED(SELECT * FROM status_current_members WHERE owner_id=$1)
 SELECT jsonb_build_object('key',m.key,'kind',m.kind,'name',m.name,'count',count(*)) FROM members m JOIN claim_revisions c ON(c.owner_id,c.claim_id,c.version)=(m.owner_id,m.claim_id,m.claim_version)
 WHERE m.owner_id=$1 AND c.nature=ANY($2::text[]) AND ($3 OR c.acquisition!='inferred')
 AND EXISTS(SELECT 1 FROM record_grants g WHERE g.owner_id=m.owner_id AND g.record_id=m.claim_id AND g.principal_id=$4)
 AND NOT EXISTS(SELECT 1 FROM context_exclusions ex WHERE ex.owner_id=m.owner_id AND ex.thing_id=$5::uuid AND ex.memory_id=m.claim_id)
 AND ($5::uuid IS NULL OR coalesce(c.scope->>'project_id','')='' OR c.scope->>'project_id'=coalesce((SELECT CASE WHEN w.kind='project' THEN w.id::text ELSE w.project_id::text END FROM work_items w WHERE w.owner_id=$1 AND w.id=$5),'')) GROUP BY m.key,m.kind,m.name ORDER BY m.kind,m.name,m.key`, string(scope.OwnerID), agent.MemoryKinds, agent.IncludeInferred, agent.ID, thing)
	if err != nil {
		return err
	}
	u.Index = visible
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
	repeated, err := queryDocuments[string](ctx, tx, `SELECT to_jsonb(e.target_id::text) FROM evidence e JOIN memory_records er ON(er.owner_id,er.id,er.version)=(e.owner_id,e.target_id,e.target_version) LEFT JOIN source_contexts sc ON(sc.owner_id,sc.source_id,sc.source_version)=(e.owner_id,e.source_id,e.source_version) WHERE e.owner_id=$1 AND e.target_id=ANY($2::uuid[]) AND e.stance='supports' GROUP BY e.target_id HAVING count(DISTINCT coalesce(nullif(sc.conversation_key,''),e.source_id::text))>=2`, string(scope.OwnerID), ids)
	if err != nil {
		return err
	}
	sort.SliceStable(requirements.Items, func(i, j int) bool {
		a, b := byID[requirements.Items[i].MemoryID], byID[requirements.Items[j].MemoryID]
		if (a.Confirmation == "confirmed") != (b.Confirmation == "confirmed") {
			return a.Confirmation == "confirmed"
		}
		if oneOf(a.ID, repeated...) != oneOf(b.ID, repeated...) {
			return oneOf(a.ID, repeated...)
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
	u.Dependencies = uniqueRefs(u.Dependencies)
	return nil
}
func writeUseContext(b *strings.Builder, u useContext, loc *time.Location, write func(workspace.Memory)) {
	if !u.Ready && len(u.Rules) == 0 && len(u.Deadlines) == 0 {
		for _, m := range u.Supplemental {
			write(m)
		}
		return
	}
	if u.Handover.Body != "" {
		fmt.Fprintf(b, "\n交接说明（写于 %s，过期=%t；重写期间照常参考上一份）：\n%s\n", u.Handover.BuiltAt, u.Handover.Stale, u.Handover.Body)
	}
	if u.Coverage != nil && len(u.Coverage.Skipped) > 0 {
		fmt.Fprintln(b, "这几组没来得及看："+strings.Join(u.Coverage.Skipped, "、"))
	}
	// A requirement shapes how this sentence is answered. Left unsaid, the model
	// carried out old one-off requests filed as requirements and edited unrelated items.
	fmt.Fprintln(b, "\n对助手的要求（每轮都落实）：")
	fmt.Fprintln(b, "这些要求只约束你怎么回应「这句话」，本身不是这一轮要办的事：不得因为某条要求去新建、修改或完成任何事项。要求里如果是过去某一次的具体请求（例如某天几点提醒做某事），已不适用，忽略。")
	for _, m := range u.Rules {
		fmt.Fprintln(b, u.RequirementScopes[m.ID])
		write(m)
	}
	fmt.Fprintln(b, "\n期限和固定安排（仅供参考，不是这一轮要办的事；已过期不代表完成，日期没说清保留原话）：")
	for _, d := range u.Deadlines {
		at := ""
		if d.At != nil {
			at = *d.At
		}
		fmt.Fprintf(b, "%s：%s %s %s %s（%s）\n", u.DeadlineStates[d.ID], d.Kind, at, d.Recurrence, d.Title, d.TimeNote)
		if m, ok := u.DeadlineMemories[d.MemoryID]; ok {
			write(m)
		}
	}
	fmt.Fprintln(b, "\n补充记忆：")
	for _, m := range u.Supplemental {
		write(m)
	}
	if u.Ready {
		fmt.Fprintln(b, "\n分组目录（在本次输出 memoryPlan 判断点名分组、所需深度和用户主动提及的记忆；不要凭字面匹配）：")
		for i, g := range u.Index {
			fmt.Fprintf(b, "G%d：%s（%s，%d条）\n", i+1, g.Name, g.Kind, g.Count)
		}
	}
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
		settings, err := queryDocument[workspace.Settings](ctx, tx, "SELECT settings FROM workspace_owners WHERE owner_id=$1 FOR SHARE", string(scope.OwnerID))
		if err != nil {
			return err
		}
		_, err = time.LoadLocation(settings.Timezone)
		return err
	})
	return ready, err
}
