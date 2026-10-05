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
	if fallback == "light" && containsAny(text, "仔细", "认真", "好好查", "都翻出来") {
		return "medium"
	}
	return fallback
}

// Without a status layer there is no directory to select or read. Keep
// medium's answer/selfcheck contract over the existing retrieval instead.
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

type useContext struct {
	Tier             string
	Location         *time.Location
	Handover         workspace.Handover
	Index            []workspace.StatusCardRef
	Cards            []workspace.StatusCard
	Rules            []workspace.Memory
	DeadlineMemories map[string]workspace.Memory
	Deadlines        []workspace.Deadline
	Supplemental     []workspace.Memory
	Groups           []string
	RequiredGroups   []string
	Dependencies     []memory.Ref
	Ready            bool
}

func (s *Store) startUseContextTx(ctx context.Context, tx pgx.Tx, scope memory.Scope) (useContext, error) {
	var u useContext
	var installed, built bool
	if err := tx.QueryRow(ctx, "SELECT to_regclass('status_cards') IS NOT NULL AND to_regclass('status_current_members') IS NOT NULL").Scan(&installed); err != nil {
		return u, err
	}
	if !installed {
		return u, nil
	}
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM status_cards WHERE owner_id=$1 AND built_at IS NOT NULL)
 OR EXISTS(SELECT 1 FROM handovers WHERE owner_id=$1 AND btrim(body)!='')`, string(scope.OwnerID)).Scan(&built); err != nil {
		return u, err
	}
	if !built {
		return u, nil
	}
	var err error
	u.Handover, err = s.HandoverTx(ctx, tx, scope)
	if err != nil {
		return u, err
	}
	u.Index, err = s.StatusCardIndexTx(context.WithValue(ctx, useStatusReadKey{}, useStatusRead{}), tx, scope)
	if err != nil {
		return u, err
	}
	u.Ready = strings.TrimSpace(u.Handover.Body) != ""
	for _, ref := range u.Index {
		if ref.Count > 0 && ref.BuiltAt != "" {
			u.Ready = true
			break
		}
	}
	return u, nil
}
func chooseUseGroups(text string, index []workspace.StatusCardRef, ranked []workspace.Memory, aliases map[string][]string, limit int, namedOnly bool) []string {
	scores := map[string]float64{}
	for i, m := range ranked {
		if i >= 40 {
			break
		}
		seen := map[string]bool{}
		add := func(key string) {
			if !seen[key] {
				scores[key] += 1 / float64(60+i+1)
				seen[key] = true
			}
		}
		for _, mention := range m.Mentions {
			add("entity:" + mention.EntityID)
		}
		for _, g := range m.Groups {
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
	named := map[string]bool{}
	lower := strings.ToLower(text)
	for _, g := range candidates {
		if g.Key == "self:rule" {
			continue
		}
		for _, name := range append([]string{g.Name}, aliases[g.Key]...) {
			if name != "" && strings.Contains(lower, strings.ToLower(name)) {
				named[g.Key] = true
				break
			}
		}
	}
	selected := []string{}
	for _, g := range candidates {
		if named[g.Key] && len(selected) < limit {
			selected = append(selected, g.Key)
		}
	}
	if namedOnly {
		return selected
	}
	scored := 0
	for _, g := range candidates {
		if g.Key == "self:rule" || named[g.Key] || scores[g.Key] <= 0 {
			continue
		}
		if scored >= 4 || len(selected) >= limit {
			break
		}
		scored++
		selected = append(selected, g.Key)
	}
	return selected
}

func useMemoryAllowed(m workspace.Memory, agent workspace.Agent) bool {
	return m.Retired == "" && oneOf(m.Kind, agent.MemoryKinds...) && (agent.IncludeInferred || m.Trust != "inferred" && m.Acquisition != "inferred")
}
func (s *Store) finishUseContextTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, agent workspace.Agent, thing *string, text string, ranked []workspace.Memory, u *useContext) error {
	if !u.Ready {
		return nil
	}
	aliases := map[string][]string{}
	rows, err := tx.Query(ctx, `SELECT 'entity:'||entity_id::text,array_agg(alias ORDER BY alias) FROM aliases a JOIN memory_records r ON (r.owner_id,r.id,r.version)=(a.owner_id,a.entity_id,a.entity_version) WHERE a.owner_id=$1 AND r.state='active' GROUP BY entity_id`, string(scope.OwnerID))
	if err != nil {
		return err
	}
	for rows.Next() {
		var key string
		var names []string
		if err = rows.Scan(&key, &names); err != nil {
			rows.Close()
			return err
		}
		aliases[key] = names
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	visibleKeys, err := queryDocuments[string](ctx, tx, `SELECT DISTINCT to_jsonb(i.key) FROM status_card_items i JOIN memory_records r ON (r.owner_id,r.id)=(i.owner_id,i.claim_id) JOIN claim_revisions cr ON (cr.owner_id,cr.claim_id,cr.version)=(i.owner_id,i.claim_id,i.claim_version) JOIN claims c ON (c.owner_id,c.id)=(r.owner_id,r.id) WHERE i.owner_id=$1 AND r.state='active' AND c.retired='' AND cr.nature=ANY($3::text[]) AND ($4 OR cr.acquisition!='inferred') AND NOT EXISTS(SELECT 1 FROM context_exclusions ex WHERE ex.owner_id=i.owner_id AND ex.thing_id=$5::uuid AND ex.memory_id=i.claim_id) AND ($5::uuid IS NULL OR coalesce(cr.scope->>'project_id','')='' OR cr.scope->>'project_id'=coalesce((SELECT CASE WHEN w.kind='project' THEN w.id::text ELSE w.project_id::text END FROM work_items w WHERE w.owner_id=$1 AND w.id=$5),'')) AND EXISTS(SELECT 1 FROM record_grants g WHERE g.owner_id=i.owner_id AND g.record_id=i.claim_id AND g.principal_id=$2)`, string(scope.OwnerID), agent.ID, agent.MemoryKinds, agent.IncludeInferred, thing)
	if err != nil {
		return err
	}
	visibleIndex := []workspace.StatusCardRef{}
	for _, g := range u.Index {
		if oneOf(g.Key, visibleKeys...) {
			visibleIndex = append(visibleIndex, g)
		}
	}
	u.Index = visibleIndex
	rankIDs := []string{}
	for _, m := range ranked {
		rankIDs = append(rankIDs, m.ID)
	}
	rows, err = tx.Query(ctx, `SELECT claim_id::text,version,subject_id::text FROM claim_revisions WHERE owner_id=$1 AND claim_id=ANY($2::uuid[])`, string(scope.OwnerID), rankIDs)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id, subject string
		var version int
		if err = rows.Scan(&id, &version, &subject); err != nil {
			rows.Close()
			return err
		}
		for i, m := range ranked {
			if m.ID == id && m.Version == version {
				exists := false
				for _, g := range m.Groups {
					if g.EntityID == subject {
						exists = true
					}
				}
				if !exists {
					ranked[i].Groups = append(append([]workspace.MemoryGroup{}, m.Groups...), workspace.MemoryGroup{EntityID: subject})
				}
			}
		}
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	limit := 6
	if u.Tier == "heavy" {
		limit = 12
	}
	u.Groups = chooseUseGroups(text, u.Index, ranked, aliases, limit, false)
	u.RequiredGroups = chooseUseGroups(text, u.Index, ranked, aliases, 12, true)
	keys := append(append([]string{}, u.Groups...), "self:rule")
	cards, err := s.StatusCardsTx(context.WithValue(ctx, useStatusReadKey{}, useStatusRead{Index: u.Index}), tx, scope, keys)
	if err != nil {
		return err
	}
	// Re-read only supplied identities: preserve P2's bounded current-version path.
	ids := []string{}
	for _, c := range cards {
		for _, f := range c.Fields {
			for _, m := range f.Items {
				ids = append(ids, m.ID)
			}
		}
	}
	u.Deadlines, err = s.DeadlinesTx(ctx, tx, scope, time.Now(), 15)
	if err != nil {
		return err
	}
	for _, d := range u.Deadlines {
		ids = append(ids, d.MemoryID)
	}
	supplied := map[string]workspace.Memory{}
	for _, card := range cards {
		for _, field := range card.Fields {
			for _, m := range field.Items {
				supplied[m.ID] = m
			}
		}
	}
	for _, m := range ranked {
		ids = append(ids, m.ID)
		if previous, ok := supplied[m.ID]; !ok || previous.Version < m.Version {
			supplied[m.ID] = m
		}
	}
	extraIDs := []string{}
	for _, d := range u.Deadlines {
		if _, ok := supplied[d.MemoryID]; !ok {
			extraIDs = append(extraIDs, d.MemoryID)
		}
	}
	if len(extraIDs) > 0 {
		extra, err := s.readMemoriesTx(ctx, tx, memory.Scope{OwnerID: scope.OwnerID, PrincipalID: agent.ID}, true, memoryReadOptions{useCurrent: true, ids: extraIDs})
		if err != nil {
			return err
		}
		for _, m := range extra {
			supplied[m.ID] = m
		}
	}
	// The read contract already supplies complete current Memory objects. Check
	// their versions and access together, rather than hydrating every card twice.
	rows, err = tx.Query(ctx, `WITH applicable AS MATERIALIZED (
 SELECT claim_id,version FROM applicable_claim_versions($1,now(),now()) WHERE claim_id=ANY($2::uuid[]))
 SELECT c.claim_id::text,c.version FROM applicable a JOIN claim_revisions c ON c.owner_id=$1 AND (c.claim_id,c.version)=(a.claim_id,a.version)
 JOIN memory_records r ON (r.owner_id,r.id)=(c.owner_id,c.claim_id)
 JOIN record_versions rv ON (rv.owner_id,rv.record_id,rv.version)=(c.owner_id,c.claim_id,c.version)
 JOIN claims active ON (active.owner_id,active.id)=(c.owner_id,c.claim_id)
 WHERE r.state='active' AND rv.state='active' AND active.retired='' AND claim_source_is_current($1,c.claim_id,c.version,now())
 AND EXISTS(SELECT 1 FROM record_grants g WHERE g.owner_id=c.owner_id AND g.record_id=c.claim_id AND g.principal_id=$3)
 AND NOT EXISTS(SELECT 1 FROM context_exclusions ex WHERE ex.owner_id=c.owner_id AND ex.thing_id=$4::uuid AND ex.memory_id=c.claim_id)
 AND ($4::uuid IS NULL OR coalesce(c.scope->>'project_id','')='' OR c.scope->>'project_id'=coalesce((SELECT CASE WHEN w.kind='project' THEN w.id::text ELSE w.project_id::text END FROM work_items w WHERE w.owner_id=$1 AND w.id=$4),''))`, string(scope.OwnerID), ids, agent.ID, thing)
	if err != nil {
		return err
	}
	valid := map[string]workspace.Memory{}
	for rows.Next() {
		var id string
		var version int
		if err = rows.Scan(&id, &version); err != nil {
			rows.Close()
			return err
		}
		if m, ok := supplied[id]; ok && m.Version == version && useMemoryAllowed(m, agent) {
			valid[id] = m
		}
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	ruleScopes := map[string]string{}
	for _, card := range cards {
		if card.Key == "self:rule" {
			for _, field := range card.Fields {
				for _, m := range field.Items {
					ruleScopes[m.ID] = m.AppliesTo
				}
			}
		}
	}
	for ci := range cards {
		if cards[ci].Key == "self:rule" {
			rules := []workspace.Memory{}
			for _, f := range cards[ci].Fields {
				rules = append(rules, f.Items...)
			}
			sort.SliceStable(rules, func(i, j int) bool {
				return ruleRelevance(ruleScopes[rules[i].ID], text) > ruleRelevance(ruleScopes[rules[j].ID], text)
			})
			cards[ci].Fields = []workspace.StatusCardField{{Field: "preference", Items: rules}}
		}
	}
	positions := map[string]int{}
	for i, key := range u.Groups {
		positions[key] = i
	}
	sort.SliceStable(cards, func(i, j int) bool { return positions[cards[i].Key] < positions[cards[j].Key] })
	u.DeadlineMemories = map[string]workspace.Memory{}
	// Track the raw input references behind a handover, including omitted card
	// entries, so correction/retirement cannot preserve a stale summary silently.
	if u.Handover.Body != "" {
		handoverRefs, err := queryDocuments[memory.Ref](ctx, tx, `SELECT jsonb_build_object('id',i.claim_id,'version',i.claim_version,'kind','claim') FROM handovers h CROSS JOIN LATERAL jsonb_array_elements(h.depends) d JOIN status_card_items i ON i.owner_id=h.owner_id AND i.key=d->>'key' WHERE h.owner_id=$1`, string(scope.OwnerID))
		if err != nil {
			return err
		}
		handoverIDs := []string{}
		for _, ref := range handoverRefs {
			handoverIDs = append(handoverIDs, string(ref.ID))
		}
		var retired bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM claims WHERE owner_id=$1 AND id=ANY($2::uuid[]) AND retired!='')", string(scope.OwnerID), handoverIDs).Scan(&retired); err != nil {
			return err
		}
		handoverRun := workspace.Run{AgentID: agent.ID, ContextVersions: handoverRefs}
		if thing != nil {
			handoverRun.ThingID = *thing
		}
		if retired || verifyRunTx(ctx, tx, scope, handoverRun) != nil {
			u.Handover.Body = ""
		} else {
			u.Dependencies = append(u.Dependencies, handoverRefs...)
		}
	}
	seen := map[string]bool{}
	accept := func(m workspace.Memory) bool {
		if !useMemoryAllowed(m, agent) {
			return false
		}
		ref := memory.Ref{ID: memory.ID(m.ID), Version: m.Version, Kind: memory.ClaimKind}
		// The batch above applies the same current/access/project/exclusion
		// checks to cards, deadlines and ranked supplements, including THIS.
		if v, ok := valid[m.ID]; !ok || v.Version != m.Version {
			return false
		}
		u.Dependencies = append(u.Dependencies, ref)
		return true
	}
	for _, card := range cards {
		copyCard := card
		copyCard.Fields = nil
		for _, f := range card.Fields {
			field := workspace.StatusCardField{Field: f.Field, Items: []workspace.Memory{}}
			for _, m := range f.Items {
				v, ok := valid[m.ID]
				if card.Key == "self:rule" && (len(u.Rules) >= 12 || ruleRelevance(ruleScopes[m.ID], text) == 0) {
					continue
				}
				if !ok || v.Version != m.Version || !accept(v) {
					continue
				}
				if card.Key == "self:rule" {
					if len(u.Rules) < 12 {
						u.Rules = append(u.Rules, v)
						seen[v.ID] = true
					}
				} else {
					field.Items = append(field.Items, v)
					seen[v.ID] = true
				}
			}
			if len(field.Items) > 0 {
				copyCard.Fields = append(copyCard.Fields, field)
			}
		}
		if card.Key != "self:rule" {
			u.Cards = append(u.Cards, copyCard)
		}
	}
	deadlineIDs := []string{}
	deadlineVersions := map[string]int{}
	for _, d := range u.Deadlines {
		deadlineIDs = append(deadlineIDs, d.ID)
	}
	rows, err = tx.Query(ctx, "SELECT id::text,claim_version FROM deadlines WHERE owner_id=$1 AND id=ANY($2::uuid[])", string(scope.OwnerID), deadlineIDs)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id string
		var version int
		if err = rows.Scan(&id, &version); err != nil {
			rows.Close()
			return err
		}
		deadlineVersions[id] = version
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	deadlines := []workspace.Deadline{}
	for _, d := range u.Deadlines {
		if m, ok := valid[d.MemoryID]; ok && m.Version == deadlineVersions[d.ID] && accept(m) {
			u.DeadlineMemories[m.ID] = m
			deadlines = append(deadlines, d)
			seen[m.ID] = true
		}
	}
	u.Deadlines = deadlines
	for _, m := range ranked {
		if len(u.Supplemental) >= 15 {
			break
		}
		if !seen[m.ID] && accept(m) {
			u.Supplemental = append(u.Supplemental, m)
			seen[m.ID] = true
		}
	}
	u.Dependencies = uniqueRefs(u.Dependencies)
	return nil
}
func writeUseContext(b *strings.Builder, u useContext, loc *time.Location, write func(workspace.Memory)) {
	fmt.Fprintln(b, "\n交接说明：")
	fmt.Fprintln(b, u.Handover.Body)
	fmt.Fprintln(b, "\n必须遵守的要求（每一条只要沾边就要落实）：")
	for _, m := range u.Rules {
		write(m)
	}
	fmt.Fprintln(b, "\n相关的现状卡：")
	for _, c := range u.Cards {
		fmt.Fprintf(b, "〔%s·%s〕\n", c.Kind, c.Name)
		for _, f := range c.Fields {
			label := map[string]string{"status": "现状", "deadline": "期限", "decided": "定过的事", "blocker": "卡点", "next": "下一步", "preference": "偏好与要求", "people": "相关的人"}[f.Field]
			if label == "" {
				label = f.Field
			}
			fmt.Fprintf(b, "%s：\n", label)
			for _, m := range f.Items {
				write(m)
			}
		}
	}
	fmt.Fprintln(b, "\n期限和固定安排：")
	for _, d := range u.Deadlines {
		at := ""
		if d.At != nil {
			at = *d.At
			if parsed, err := time.Parse(time.RFC3339Nano, at); err == nil {
				at = parsed.In(loc).Format("2006-01-02 15:04")
			}
		}
		fmt.Fprintf(b, "%s：%s %s %s（%s）\n", d.Kind, at, d.Recurrence, d.Title, d.TimeNote)
		if m, ok := u.DeadlineMemories[d.MemoryID]; ok {
			write(m)
		}
	}
	fmt.Fprintln(b, "\n补充记忆：")
	for _, m := range u.Supplemental {
		write(m)
	}
}

// Empty scope means global. A short scope phrase matches by topic words and
// the common outward-message family; it never becomes an authorization gate.
func ruleRelevance(applies, text string) int {
	if strings.TrimSpace(applies) == "" || oneOf(strings.ToLower(applies), "all", "global", "不限", "通用", "所有") {
		return 1
	}
	if strings.Contains(strings.ToLower(text), strings.ToLower(applies)) {
		return 2
	}
	for _, token := range memory.SearchTokens(applies) {
		if len([]rune(token)) > 1 && strings.Contains(strings.ToLower(text), strings.ToLower(token)) {
			return 2
		}
	}
	if containsAny(applies, "邮件", "发信", "发送", "发出去", "对外") && containsAny(text, "邮件", "发信", "发送", "发出去", "对外", "起草") {
		return 2
	}
	return 0
}

// The initial Snapshot return value was discarded. Ready turns need its owner
// initialization and configuration checks, then their normal context reads;
// the final response still reads a complete snapshot. Unbuilt status preserves
// the established initial Snapshot path as well as the legacy model context.
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
