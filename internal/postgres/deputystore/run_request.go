package deputystore

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/contextbuilder"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/postgres/accessstore"
	"github.com/soaringjerry/PCAS/internal/postgres/actionlog"
	"github.com/soaringjerry/PCAS/internal/postgres/evidencestore"
	"github.com/soaringjerry/PCAS/internal/postgres/intakestore"
	"github.com/soaringjerry/PCAS/internal/postgres/itemstore"
	"github.com/soaringjerry/PCAS/internal/postgres/kernel"
	"github.com/soaringjerry/PCAS/internal/postgres/memoryread"
	"github.com/soaringjerry/PCAS/internal/postgres/usestore"
	"github.com/soaringjerry/PCAS/internal/postgres/workspacestore"
	"github.com/soaringjerry/PCAS/internal/retrieval"
	"github.com/soaringjerry/PCAS/internal/workspace"
	"github.com/soaringjerry/PCAS/internal/workspace/deputy"
)

// requestUnit is the storage of one run request in the transaction of the
// command. deputy.Request decides; requestUnit reads and writes.
type requestUnit struct {
	store *Deputy
	tx    pgx.Tx
	scope memory.Scope
}

var _ deputy.RequestUnit = (*requestUnit)(nil)

func (u *requestUnit) Item(ctx context.Context, id string) (workspace.Item, error) {
	return itemstore.GetItem(ctx, u.tx, u.scope, id)
}

func (u *requestUnit) Agent(ctx context.Context, id string) (workspace.Agent, error) {
	return kernel.QueryDocument[workspace.Agent](ctx, u.tx, "SELECT document FROM workspace_agents WHERE owner_id=$1 AND id=$2", string(u.scope.OwnerID), id)
}

func (u *requestUnit) Sanitize(ctx context.Context, agent string, item workspace.Item) (workspace.Item, []memory.Ref, error) {
	return accessstore.SanitizeItemTx(ctx, u.tx, u.scope, agent, item)
}

func (u *requestUnit) History(ctx context.Context, c workspace.Command, item workspace.Item) ([]deputy.Turn, error) {
	return readRunHistoryTx(ctx, u.tx, u.scope, c, item)
}

func (u *requestUnit) Memories(ctx context.Context, agent string) ([]workspace.Memory, error) {
	return u.store.ReadMemoriesTx(ctx, u.tx, memory.Scope{OwnerID: u.scope.OwnerID, PrincipalID: agent}, true, memoryread.MemoryReadOptions{UseCurrent: true})
}

func (u *requestUnit) Smoke(ctx context.Context) string { return actionlog.SmokeID(ctx) }

func (u *requestUnit) Documents(ctx context.Context, item workspace.Item, agent string) ([]workspace.Doc, []memory.Ref, error) {
	return currentRunDocsTx(ctx, u.tx, u.scope, item, agent)
}

func (u *requestUnit) Document(ctx context.Context, id string) (workspace.Doc, error) {
	return kernel.QueryDocument[workspace.Doc](ctx, u.tx, "SELECT document FROM work_documents WHERE owner_id=$1 AND id=$2", string(u.scope.OwnerID), id)
}

func (u *requestUnit) DocumentVersion(ctx context.Context, id string, version int) (workspace.DocumentVersion, error) {
	return workspacestore.DocumentVersionTx(ctx, u.tx, u.scope, id, version)
}

func (u *requestUnit) Settings(ctx context.Context) (workspace.Settings, error) {
	return kernel.QueryDocument[workspace.Settings](ctx, u.tx, "SELECT settings FROM workspace_owners WHERE owner_id=$1", string(u.scope.OwnerID))
}

func (u *requestUnit) StartUse(ctx context.Context) (contextbuilder.UseContext, error) {
	return u.store.StartUseContextTx(ctx, u.tx, u.scope)
}

func (u *requestUnit) Recall(ctx context.Context, agent string, request memory.RecallRequest, budget memory.Budget, tokens []string) (memory.RecallResult, error) {
	terms := []string{}
	for _, token := range tokens {
		if len([]rune(token)) > 1 {
			terms = append(terms, "'"+strings.ReplaceAll(token, "'", "''")+"'")
		}
	}
	result := memory.RecallResult{Coverage: retrieval.NewCoverage()}
	err := u.store.RecallTx(ctx, u.tx, memory.Scope{OwnerID: u.scope.OwnerID, PrincipalID: agent, Team: true}, request, budget, request.Query, strings.Join(terms, " | "), nil, "", 0, "", tokens, &result)
	return result, err
}

func (u *requestUnit) Tier(ctx context.Context) string { return usestore.MemoryTier(ctx, "", "") }

func (u *requestUnit) FinishUse(ctx context.Context, agent workspace.Agent, item, prompt string, ranked []workspace.Memory, use *contextbuilder.UseContext) error {
	return u.store.FinishUseContextTx(ctx, u.tx, u.scope, agent, &item, prompt, ranked, use)
}

func (u *requestUnit) Sources(ctx context.Context, agent, item string, excerpts []memory.RecallExcerpt, historyRequests []string, segments, characters int) ([]memory.RecallExcerpt, error) {
	return intakestore.TeamSourceExcerptsTx(ctx, u.tx, u.scope, agent, &item, excerpts, historyRequests, segments, characters)
}

func (u *requestUnit) Evidence(ctx context.Context, agent, item string, claims []contextbuilder.Claim, batch bool) ([]contextbuilder.EvidenceGroup, []string, error) {
	if batch {
		ctx = context.WithValue(ctx, evidencestore.UseEvidenceBatchKey{}, true)
	}
	return evidencestore.EvidenceContextsTx(ctx, u.tx, u.scope, agent, &item, claims)
}

func (u *requestUnit) Overflow(ctx context.Context, reason string, count int) error {
	return overflowEvent(ctx, u.tx, u.scope, reason, count)
}

func (u *requestUnit) Available(agent string) bool { return u.store.ModelAvailable(agent) }

func (u *requestUnit) Estimate(agent, input string) float64 {
	return u.store.Models.Estimate(agent, input)
}

func (u *requestUnit) SpentSince(ctx context.Context, start time.Time) (float64, error) {
	var spent float64
	err := u.tx.QueryRow(ctx, "SELECT coalesce((SELECT sum(reserved_cost) FROM agent_runs WHERE owner_id=$1 AND created_at >= $2),0)+coalesce((SELECT sum(reserved_cost) FROM background_usage WHERE owner_id=$1 AND created_at >= $2),0)", string(u.scope.OwnerID), start).Scan(&spent)
	return spent, err
}

func (u *requestUnit) Insert(ctx context.Context, run workspace.Run, status string, reviseTarget int) error {
	owner := string(u.scope.OwnerID)
	if _, err := u.tx.Exec(ctx, "INSERT INTO agent_runs(owner_id,id,thing_id,agent_id,status,reserved_cost,created_at,document) VALUES($1,$2,$3,$4,$5,$6,$7,$8)", owner, run.ID, run.ThingID, run.AgentID, status, run.Cost, run.CreatedAt, kernel.JSON(run)); err != nil {
		return err
	}
	if run.Kind == "revise" {
		if _, err := u.tx.Exec(ctx, `INSERT INTO revise_run_targets(owner_id,run_id,document_id,target_version) VALUES($1,$2,$3,$4)`, owner, run.ID, run.DocumentID, reviseTarget); err != nil {
			return err
		}
	}
	for _, ref := range run.ContextVersions {
		if _, err := u.tx.Exec(ctx, "INSERT INTO run_dependencies(owner_id,run_id,memory_id,memory_version) VALUES($1,$2,$3,$4)", owner, run.ID, string(ref.ID), ref.Version); err != nil {
			return err
		}
	}
	return nil
}
