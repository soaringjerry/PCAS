package usestore

import (
	"context"
	"strings"
	"time"

	"github.com/soaringjerry/PCAS/internal/contextbuilder"
	"github.com/soaringjerry/PCAS/internal/postgres/itemstore"
	"github.com/soaringjerry/PCAS/internal/postgres/kernel"
	"github.com/soaringjerry/PCAS/internal/postgres/memoryread"
	"github.com/soaringjerry/PCAS/internal/postgres/workspacestore"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

type memoryTierKey struct{}

// WithMemoryTier fixes the tier for offline evaluation. There is no user switch.
func WithMemoryTier(ctx context.Context, tier string) context.Context {
	return context.WithValue(ctx, memoryTierKey{}, tier)
}
func MemoryTier(ctx context.Context, text, fallback string) string {
	if tier, _ := ctx.Value(memoryTierKey{}).(string); kernel.OneOf(tier, "light", "medium", "heavy") {
		return tier
	}
	return fallback
}

type (
	UseCoverage = contextbuilder.Coverage
	UseContext  = contextbuilder.UseContext
)

func (s *Use) StartUseContextTx(ctx context.Context, tx pgx.Tx, scope memory.Scope) (UseContext, error) {
	u := UseContext{Coverage: &UseCoverage{}}
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
	groups, err := s.MemoryGroupsTx(ctx, tx, scope)
	if err != nil {
		return u, err
	}
	for _, g := range groups.Items {
		u.Index = append(u.Index, workspace.StatusCardRef{Key: g.Key, Kind: g.Kind, Name: g.Name, Count: g.Count})
	}
	u.Ready = strings.TrimSpace(u.Handover.Body) != ""
	return u, nil
}

// Access, exclusions and destination scope are checked in one set, not per memory.
func (s *Use) UseMemoriesTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, agent workspace.Agent, thing *string, ids []string) ([]workspace.Memory, error) {
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
	ms, err := s.ReadMemoriesTx(ctx, tx, memory.Scope{OwnerID: scope.OwnerID, PrincipalID: agent.ID}, true, memoryread.MemoryReadOptions{UseCurrent: true, IDs: allowed})
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

// FinishUseContextTx completes the memory that a turn or a run is given.
// contextbuilder.FinishUse decides; useUnit reads.
func (s *Use) FinishUseContextTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, agent workspace.Agent, thing *string, _ string, ranked []workspace.Memory, u *UseContext) error {
	return contextbuilder.FinishUse(ctx, useUnit{store: s, tx: tx, scope: scope}, agent, thing, ranked, u)
}

// useUnit is the storage of the memory that one turn or run is given.
type useUnit struct {
	store *Use
	tx    pgx.Tx
	scope memory.Scope
}

func (u useUnit) owner() string { return string(u.scope.OwnerID) }

func (u useUnit) VisibleGroups(ctx context.Context, agent workspace.Agent, thing *string) ([]workspace.StatusCardRef, error) {
	return kernel.QueryDocuments[workspace.StatusCardRef](ctx, u.tx, `WITH members AS MATERIALIZED(SELECT * FROM status_current_members WHERE owner_id=$1)
 SELECT jsonb_build_object('key',m.key,'kind',m.kind,'name',m.name,'count',count(*)) FROM members m JOIN claim_revisions c ON(c.owner_id,c.claim_id,c.version)=(m.owner_id,m.claim_id,m.claim_version)
 WHERE m.owner_id=$1 AND c.nature=ANY($2::text[]) AND ($3 OR c.acquisition!='inferred')
 AND EXISTS(SELECT 1 FROM record_grants g WHERE g.owner_id=m.owner_id AND g.record_id=m.claim_id AND g.principal_id=$4)
 AND NOT EXISTS(SELECT 1 FROM context_exclusions ex WHERE ex.owner_id=m.owner_id AND ex.thing_id=$5::uuid AND ex.memory_id=m.claim_id)
 AND ($5::uuid IS NULL OR coalesce(c.scope->>'project_id','')='' OR c.scope->>'project_id'=coalesce((SELECT CASE WHEN w.kind='project' THEN w.id::text ELSE w.project_id::text END FROM work_items w WHERE w.owner_id=$1 AND w.id=$5),'')) GROUP BY m.key,m.kind,m.name ORDER BY m.kind,m.name,m.key`, u.owner(), agent.MemoryKinds, agent.IncludeInferred, agent.ID, thing)
}

func (u useUnit) Studio(ctx context.Context) (bool, error) {
	return workspacestore.StudioInstalledTx(ctx, u.tx)
}

func (u useUnit) Item(ctx context.Context, id string) (workspace.Item, error) {
	return itemstore.GetItem(ctx, u.tx, u.scope, id)
}

func (u useUnit) ProjectHandover(ctx context.Context, project string) (workspace.ProjectHandover, error) {
	return workspacestore.ProjectHandoverTx(ctx, u.tx, u.scope, project)
}

func (u useUnit) ProjectGroups(ctx context.Context, project string) ([]string, error) {
	return kernel.QueryDocuments[string](ctx, u.tx, `SELECT to_jsonb('entity:'||ev.entity_id::text) FROM entity_versions ev JOIN memory_records r ON(r.owner_id,r.id,r.version)=(ev.owner_id,ev.entity_id,ev.version) WHERE ev.owner_id=$1 AND ev.disambiguation->>'work_item_id'=$2 ORDER BY ev.entity_id`, u.owner(), project)
}

func (u useUnit) Requirements(ctx context.Context) (workspace.AssistantRequirementList, error) {
	return u.store.AssistantRequirementsTx(ctx, u.tx, u.scope)
}

func (u useUnit) Deadlines(ctx context.Context) (workspace.DeadlineList, error) {
	return u.store.LibraryDeadlinesTx(ctx, u.tx, u.scope, workspace.DeadlineQuery{})
}

func (u useUnit) Memories(ctx context.Context, agent workspace.Agent, thing *string, ids []string) ([]workspace.Memory, error) {
	return u.store.UseMemoriesTx(ctx, u.tx, u.scope, agent, thing, ids)
}

// Repeated is evidence from distinct conversations or sources, not the number
// of mentions.
func (u useUnit) Repeated(ctx context.Context, ids []string) ([]string, error) {
	return kernel.QueryDocuments[string](ctx, u.tx, `SELECT to_jsonb(e.target_id::text) FROM evidence e JOIN memory_records er ON(er.owner_id,er.id,er.version)=(e.owner_id,e.target_id,e.target_version) LEFT JOIN source_contexts sc ON(sc.owner_id,sc.source_id,sc.source_version)=(e.owner_id,e.source_id,e.source_version) WHERE e.owner_id=$1 AND e.target_id=ANY($2::uuid[]) AND e.stance='supports' GROUP BY e.target_id HAVING count(DISTINCT coalesce(nullif(sc.conversation_key,''),e.source_id::text))>=2`, u.owner(), ids)
}

func (u useUnit) Overflow(ctx context.Context, reason string, count int) error {
	return kernel.StageEvent(ctx, u.tx, u.scope.OwnerID, "secretary", "overflow", reason, count)
}

// Compatibility helper; semantic applicability is exclusively model-owned.
func RuleRelevance(applies, text string) int {
	if strings.TrimSpace(applies) == "" {
		return 1
	}
	return 0
}

func (s *Use) PrepareUseOwner(ctx context.Context, scope memory.Scope) (bool, error) {
	ready := false
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		if err := s.EnsureOwner(ctx, tx, scope); err != nil {
			return err
		}
		u, err := s.StartUseContextTx(ctx, tx, scope)
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
