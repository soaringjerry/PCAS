package deskstore

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/contextbuilder"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/postgres/accessstore"
	"github.com/soaringjerry/PCAS/internal/postgres/evidencestore"
	"github.com/soaringjerry/PCAS/internal/postgres/intakestore"
	"github.com/soaringjerry/PCAS/internal/postgres/itemstore"
	"github.com/soaringjerry/PCAS/internal/postgres/kernel"
	"github.com/soaringjerry/PCAS/internal/postgres/memoryread"
	"github.com/soaringjerry/PCAS/internal/postgres/usestore"
	"github.com/soaringjerry/PCAS/internal/postgres/workspacestore"
	"github.com/soaringjerry/PCAS/internal/workspace"
	"github.com/soaringjerry/PCAS/internal/workspace/secretary"
)

// inputUnit is the storage of the context and the model input of one turn.
// secretary.Gather and secretary.Compose decide; inputUnit reads and writes.
type inputUnit struct {
	store *Desk
	tx    pgx.Tx
	scope memory.Scope
}

var _ secretary.InputUnit = (*inputUnit)(nil)

func (u *inputUnit) owner() string    { return string(u.scope.OwnerID) }
func (u *inputUnit) Owner() memory.ID { return u.scope.OwnerID }

func (u *inputUnit) DefaultAgent() (string, bool) {
	if u.store.Models == nil {
		return "", false
	}
	return u.store.Models.ExtractionID(), true
}

func (u *inputUnit) Agent(ctx context.Context, id string) (workspace.Agent, error) {
	return kernel.QueryDocument[workspace.Agent](ctx, u.tx, "SELECT document FROM workspace_agents WHERE owner_id=$1 AND id=$2", u.owner(), id)
}

func (u *inputUnit) Available(agent string) bool { return u.store.ModelAvailable(agent) }

func (u *inputUnit) Settings(ctx context.Context) (workspace.Settings, error) {
	return kernel.QueryDocument[workspace.Settings](ctx, u.tx, "SELECT settings FROM workspace_owners WHERE owner_id=$1", u.owner())
}

func (u *inputUnit) OpenTasks(ctx context.Context, agent string, limit int) ([]workspace.Item, []memory.Ref, error) {
	tasks, err := kernel.QueryDocuments[workspace.Item](ctx, u.tx, "SELECT document FROM work_items WHERE owner_id=$1 AND kind='task' AND status IN ('todo','doing','waiting') ORDER BY created_at,id LIMIT $2", u.owner(), limit)
	if err != nil {
		return nil, nil, err
	}
	dependencies := []memory.Ref{}
	for i := range tasks {
		var refs []memory.Ref
		tasks[i], refs, err = accessstore.SanitizeItemTx(ctx, u.tx, u.scope, agent, tasks[i])
		if err != nil {
			return nil, nil, err
		}
		dependencies = append(dependencies, refs...)
	}
	return tasks, dependencies, nil
}

func (u *inputUnit) ActiveProjects(ctx context.Context, limit int) ([]workspace.Item, error) {
	projects, err := kernel.QueryDocuments[workspace.Item](ctx, u.tx, "SELECT document FROM work_items WHERE owner_id=$1 AND kind='project' AND status='active' ORDER BY created_at,id LIMIT $2", u.owner(), limit)
	if err != nil {
		return nil, err
	}
	goals, err := workspacestore.ProjectGoalEvidenceTx(ctx, u.tx, u.scope.OwnerID)
	if err != nil {
		return nil, err
	}
	for i := range projects {
		p := &projects[i]
		excerpt := ""
		if p.Creation != nil && p.Creation.Source != nil {
			excerpt = p.Creation.Source.Excerpt
		}
		p.Goal = workspacestore.ProjectMeaning(p.Goal, excerpt, goals[p.ID])
	}
	return projects, nil
}

func (u *inputUnit) RecentIdeas(ctx context.Context, limit int) ([]workspace.Item, error) {
	return kernel.QueryDocuments[workspace.Item](ctx, u.tx, "SELECT document FROM (SELECT document,created_at,id FROM work_items WHERE owner_id=$1 AND kind='idea' ORDER BY created_at DESC,id DESC LIMIT $2) recent ORDER BY created_at,id", u.owner(), limit)
}

func (u *inputUnit) RecentItems(ctx context.Context, conversation string, limit int) ([]workspace.Item, error) {
	return kernel.QueryDocuments[workspace.Item](ctx, u.tx, `SELECT w.document FROM work_items w JOIN (
 SELECT c->>'id' AS id,max(l.created_at) AS at FROM action_log l JOIN desk_turns t ON (t.owner_id,t.id)=(l.owner_id,l.turn_id)
 CROSS JOIN LATERAL jsonb_array_elements(l.changes) c
 WHERE l.owner_id=$1 AND t.conversation_id=$2 AND l.undone_at IS NULL AND c->>'table'='work_items' GROUP BY c->>'id'
 ) r ON r.id=w.id::text WHERE w.owner_id=$1 ORDER BY r.at DESC,w.created_at,w.id LIMIT $3`, u.owner(), conversation, limit)
}

func (u *inputUnit) Item(ctx context.Context, id string) (workspace.Item, error) {
	return itemstore.GetItem(ctx, u.tx, u.scope, id)
}

func (u *inputUnit) Sanitize(ctx context.Context, agent string, item workspace.Item) (workspace.Item, []memory.Ref, error) {
	return accessstore.SanitizeItemTx(ctx, u.tx, u.scope, agent, item)
}

func (u *inputUnit) OpenTasksByProject(ctx context.Context) (map[string]int, error) {
	return u.counts(ctx, "SELECT project_id::text,count(*) FROM work_items WHERE owner_id=$1 AND kind='task' AND status IN ('todo','doing','waiting') AND project_id IS NOT NULL GROUP BY project_id")
}

func (u *inputUnit) Open(ctx context.Context) (map[string]int, error) {
	return u.counts(ctx, `SELECT kind,count(*) FROM work_items WHERE owner_id=$1 AND ((kind='task' AND status IN ('todo','doing','waiting')) OR (kind='project' AND status='active') OR kind='idea') GROUP BY kind`)
}

// counts reads rows of a key and a count of the owner.
func (u *inputUnit) counts(ctx context.Context, sql string) (map[string]int, error) {
	rows, err := u.tx.Query(ctx, sql, u.owner())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := map[string]int{}
	for rows.Next() {
		var key string
		var count int
		if err := rows.Scan(&key, &count); err != nil {
			return nil, err
		}
		counts[key] = count
	}
	return counts, rows.Err()
}

func (u *inputUnit) Turns(ctx context.Context, conversation string, limit int, agent string, thing *string, dependencies *[]memory.Ref) ([]workspace.SecretaryTurn, error) {
	stored, err := u.store.DeskTurnsTx(ctx, u.tx, u.scope, conversation, limit, agent, thing, dependencies)
	return stored.Turns, err
}

func (u *inputUnit) RecentItemCount(ctx context.Context, conversation string) (int, error) {
	var recent int
	err := u.tx.QueryRow(ctx, `SELECT count(DISTINCT w.id) FROM action_log l JOIN desk_turns t ON(t.owner_id,t.id)=(l.owner_id,l.turn_id) CROSS JOIN LATERAL jsonb_array_elements(l.changes) c JOIN work_items w ON w.owner_id=l.owner_id AND w.id::text=c->>'id' WHERE l.owner_id=$1 AND t.conversation_id=$2 AND l.undone_at IS NULL AND c->>'table'='work_items'`, u.owner(), conversation).Scan(&recent)
	return recent, err
}

func (u *inputUnit) TurnCount(ctx context.Context, conversation string) (int, error) {
	var turns int
	err := u.tx.QueryRow(ctx, "SELECT count(*) FROM desk_turns WHERE owner_id=$1 AND conversation_id=$2", u.owner(), conversation).Scan(&turns)
	return turns, err
}

func (u *inputUnit) Overflow(ctx context.Context, reason string, count int) error {
	if count <= 0 {
		return nil
	}
	return kernel.StageEvent(ctx, u.tx, u.scope.OwnerID, "secretary", "overflow", reason, count)
}

func (u *inputUnit) Studio(ctx context.Context) (bool, error) {
	return workspacestore.StudioInstalledTx(ctx, u.tx)
}

func (u *inputUnit) Documents(ctx context.Context, thing *string) ([]workspace.Doc, error) {
	return kernel.QueryDocuments[workspace.Doc](ctx, u.tx, `SELECT d.document FROM work_documents d JOIN work_items w ON(w.owner_id,w.id)=(d.owner_id,d.thing_id)
 WHERE d.owner_id=$1 AND ($2::uuid IS NULL OR w.id=$2 OR w.project_id=$2 OR w.project_id=(SELECT CASE WHEN kind='project' THEN id ELSE project_id END FROM work_items WHERE owner_id=$1 AND id=$2)) ORDER BY d.document->>'updatedAt' DESC,d.id`, u.owner(), thing)
}

func (u *inputUnit) DocumentVersions(ctx context.Context, id string) ([]int, error) {
	return kernel.QueryDocuments[int](ctx, u.tx, `SELECT to_jsonb(version) FROM document_versions WHERE owner_id=$1 AND document_id=$2 ORDER BY version DESC`, u.owner(), id)
}

func (u *inputUnit) Tier(ctx context.Context) string { return usestore.MemoryTier(ctx, "", "") }

func (u *inputUnit) StartUse(ctx context.Context) (contextbuilder.UseContext, error) {
	return u.store.StartUseContextTx(ctx, u.tx, u.scope)
}

func (u *inputUnit) Memories(ctx context.Context, agent string, ids []string) ([]workspace.Memory, error) {
	return u.store.ReadMemoriesTx(ctx, u.tx, memory.Scope{OwnerID: u.scope.OwnerID, PrincipalID: agent}, true, memoryread.MemoryReadOptions{IDs: ids, UseCurrent: true})
}

func (u *inputUnit) FinishUse(ctx context.Context, agent workspace.Agent, thing *string, text string, ranked []workspace.Memory, use *contextbuilder.UseContext) error {
	return u.store.FinishUseContextTx(ctx, u.tx, u.scope, agent, thing, text, ranked, use)
}

func (u *inputUnit) Readable(ctx context.Context, thing, agent string, ref memory.Ref) bool {
	return accessstore.VerifyRunTx(ctx, u.tx, u.scope, workspace.Run{ThingID: thing, AgentID: agent, ContextVersions: []memory.Ref{ref}}) == nil
}

func (u *inputUnit) HistoryRequests(ctx context.Context, conversation string) ([]string, error) {
	return kernel.QueryDocuments[string](ctx, u.tx, "SELECT to_jsonb(request_id::text) FROM desk_turns WHERE owner_id=$1 AND conversation_id=$2 AND question!='' AND request_id IS NOT NULL", u.owner(), conversation)
}

func (u *inputUnit) Sources(ctx context.Context, agent string, thing *string, excerpts []memory.RecallExcerpt, historyRequests []string, segments, characters int) ([]memory.RecallExcerpt, error) {
	return intakestore.TeamSourceExcerptsTx(ctx, u.tx, u.scope, agent, thing, excerpts, historyRequests, segments, characters)
}

func (u *inputUnit) Evidence(ctx context.Context, agent string, thing *string, claims []contextbuilder.Claim, batch bool) ([]contextbuilder.EvidenceGroup, []string, error) {
	if batch {
		ctx = context.WithValue(ctx, evidencestore.UseEvidenceBatchKey{}, true)
	}
	return evidencestore.EvidenceContextsTx(ctx, u.tx, u.scope, agent, thing, claims)
}

func (u *inputUnit) UseMemories(ctx context.Context, agent workspace.Agent, thing *string, ids []string) ([]workspace.Memory, error) {
	return u.store.UseMemoriesTx(ctx, u.tx, u.scope, agent, thing, ids)
}
