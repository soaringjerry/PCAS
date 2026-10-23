package commandstore

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/postgres/accessstore"
	"github.com/soaringjerry/PCAS/internal/postgres/actionlog"
	"github.com/soaringjerry/PCAS/internal/postgres/callstore"
	"github.com/soaringjerry/PCAS/internal/postgres/claimstore"
	"github.com/soaringjerry/PCAS/internal/postgres/intakestore"
	"github.com/soaringjerry/PCAS/internal/postgres/itemstore"
	"github.com/soaringjerry/PCAS/internal/postgres/kernel"
	"github.com/soaringjerry/PCAS/internal/postgres/ownerstore"
	"github.com/soaringjerry/PCAS/internal/postgres/workspacestore"
	"github.com/soaringjerry/PCAS/internal/postgres/workstore"
	"github.com/soaringjerry/PCAS/internal/workspace"
	"github.com/soaringjerry/PCAS/internal/workspace/command"
	"github.com/soaringjerry/PCAS/internal/workspace/itemcommand"
)

// commandUnit is the storage of one workspace command. command.Apply
// decides; commandUnit reads and writes in the transaction of the command.
type commandUnit struct {
	store *Commands
	tx    pgx.Tx
	scope memory.Scope
}

var _ command.Unit = (*commandUnit)(nil)

func (u *commandUnit) owner() string { return string(u.scope.OwnerID) }

func (u *commandUnit) RestoreMemory(ctx context.Context, id string) error {
	return restoreMemoryTx(ctx, u.tx, u.scope, id)
}

func (u *commandUnit) UndoEntityMerge(ctx context.Context, id string) error {
	return u.store.undoEntityCommandTx(ctx, u.tx, u.scope, id)
}

func (u *commandUnit) UndoAction(ctx context.Context, id string) error {
	return u.store.UndoActionTx(ctx, u.tx, u.scope, id)
}

func (u *commandUnit) MemoryCommand(ctx context.Context, c workspace.Command) error {
	return u.store.MemoryCommandTx(ctx, u.tx, u.scope, c)
}

func (u *commandUnit) RunCommand(ctx context.Context, c workspace.Command) error {
	return u.store.RunCommandTx(ctx, u.tx, u.scope, c)
}

func (u *commandUnit) Deadline(ctx context.Context, id string) (string, int, error) {
	var claim string
	var version int
	err := u.tx.QueryRow(ctx, "SELECT claim_id::text,claim_version FROM deadlines WHERE owner_id=$1 AND id=$2", u.owner(), id).Scan(&claim, &version)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", 0, memory.ErrNotFound
	}
	return claim, version, err
}

func (u *commandUnit) ActiveMemory(ctx context.Context, id string) error {
	return intakestore.ActiveClaim(ctx, u.tx, u.scope, id)
}

func (u *commandUnit) CompleteDeadline(ctx context.Context, claim string, version int, reason string) error {
	return workstore.CompleteDeadlineTx(ctx, u.tx, u.scope, claim, version, reason)
}

func (u *commandUnit) Agent(ctx context.Context, id string) (workspace.Agent, error) {
	return kernel.QueryDocument[workspace.Agent](ctx, u.tx, "SELECT document FROM workspace_agents WHERE owner_id=$1 AND id=$2", u.owner(), id)
}

func (u *commandUnit) Available(agent string) bool { return u.store.ModelAvailable(agent) }

func (u *commandUnit) SaveAgent(ctx context.Context, agent workspace.Agent) error {
	_, err := u.tx.Exec(ctx, "UPDATE workspace_agents SET document=$3 WHERE owner_id=$1 AND id=$2", u.owner(), agent.ID, kernel.JSON(agent))
	return err
}

func (u *commandUnit) InitializeAgentMemories(ctx context.Context, agent string) error {
	return ownerstore.InitializeAgentMemoriesTx(ctx, u.tx, u.scope.OwnerID, agent)
}

func (u *commandUnit) Capture(ctx context.Context, request memory.IngestRequest) (memory.Ref, error) {
	in, err := u.store.IngestTx(ctx, u.tx, u.scope, request)
	return memory.Ref{ID: in.ID, Version: in.Version, Kind: memory.SourceKind}, err
}

func (u *commandUnit) Candidate(ctx context.Context, id string) (workspace.Candidate, error) {
	return kernel.QueryDocument[workspace.Candidate](ctx, u.tx, "SELECT document FROM capture_candidates WHERE owner_id=$1 AND id=$2", u.owner(), id)
}

func (u *commandUnit) SaveCandidate(ctx context.Context, candidate workspace.Candidate) error {
	return intakestore.SaveCandidate(ctx, u.tx, u.scope, candidate)
}

func (u *commandUnit) Origin(ctx context.Context) itemcommand.Origin {
	origin := itemcommand.Origin{Actor: kernel.ActorFromContext(ctx), Collected: ctx.Value(kernel.SecretaryHistoryKey{}) != nil}
	if original, ok := ctx.Value(kernel.SecretaryCreationKey{}).(workspace.SourceRef); ok {
		origin.SecretarySource = &original
		if log, ok := ctx.Value(actionlog.ActionLogKey{}).(actionlog.ActionLog); ok {
			origin.ActionID = log.ID
		}
	}
	return origin
}

func (u *commandUnit) Actor(ctx context.Context) string { return kernel.ActorFromContext(ctx) }

func (u *commandUnit) ItemExists(ctx context.Context, id string) (bool, error) {
	var exists bool
	err := u.tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM work_items WHERE owner_id=$1 AND id=$2)", u.owner(), id).Scan(&exists)
	return exists, err
}

func (u *commandUnit) SaveAction(ctx context.Context, item workspace.Item, summary string) error {
	return u.store.SaveAction(ctx, u.tx, u.scope, item, summary)
}

func (u *commandUnit) ItemUse(ctx context.Context, id string) (children, running bool, err error) {
	err = u.tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM work_items WHERE owner_id=$1 AND project_id=$2),
		EXISTS(SELECT 1 FROM agent_runs WHERE owner_id=$1 AND thing_id=$2 AND status='running')`, u.owner(), id).Scan(&children, &running)
	return children, running, err
}

func (u *commandUnit) DeleteItem(ctx context.Context, item workspace.Item) error {
	return u.store.deleteItemTx(ctx, u.tx, u.scope, item)
}

func (u *commandUnit) Document(ctx context.Context, id string) (workspace.Doc, error) {
	return kernel.QueryDocument[workspace.Doc](ctx, u.tx, "SELECT document FROM work_documents WHERE owner_id=$1 AND id=$2", u.owner(), id)
}

func (u *commandUnit) DocumentExists(ctx context.Context, id string) (bool, error) {
	var exists bool
	err := u.tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM work_documents WHERE owner_id=$1 AND id=$2)", u.owner(), id).Scan(&exists)
	return exists, err
}

func (u *commandUnit) DeleteDocument(ctx context.Context, id string) error {
	_, err := u.tx.Exec(ctx, "DELETE FROM work_documents WHERE owner_id=$1 AND id=$2", u.owner(), id)
	return err
}

func (u *commandUnit) SaveDoc(ctx context.Context, doc workspace.Doc) error {
	return workspacestore.SaveDoc(ctx, u.tx, u.scope, doc)
}

func (u *commandUnit) Settings(ctx context.Context) (workspace.Settings, error) {
	return kernel.QueryDocument[workspace.Settings](ctx, u.tx, "SELECT settings FROM workspace_owners WHERE owner_id=$1", u.owner())
}

func (u *commandUnit) SaveSettings(ctx context.Context, settings workspace.Settings) error {
	_, err := u.tx.Exec(ctx, "UPDATE workspace_owners SET settings=$2 WHERE owner_id=$1", u.owner(), kernel.JSON(settings))
	return err
}

func (u *commandUnit) RefreshTaskPlans(ctx context.Context) error {
	return workstore.RefreshTaskPlansTx(ctx, u.tx, u.scope)
}

func (u *commandUnit) ToggleExclusion(ctx context.Context, item, memory string) error {
	tag, err := u.tx.Exec(ctx, "DELETE FROM context_exclusions WHERE owner_id=$1 AND thing_id=$2 AND memory_id=$3", u.owner(), item, memory)
	if err != nil || tag.RowsAffected() > 0 {
		return err
	}
	_, err = u.tx.Exec(ctx, "INSERT INTO context_exclusions(owner_id,thing_id,memory_id) VALUES($1,$2,$3)", u.owner(), item, memory)
	return err
}

func (u *commandUnit) SetSampleState(ctx context.Context, id, state string) (bool, error) {
	tag, err := u.tx.Exec(ctx, "UPDATE training_samples SET state=$3 WHERE owner_id=$1 AND id=$2 AND (NOT stale OR $3!='included')", u.owner(), id, state)
	return err == nil && tag.RowsAffected() > 0, err
}

func (u *commandUnit) RetryJob(ctx context.Context, id string) (bool, error) {
	tag, err := u.tx.Exec(ctx, "UPDATE memory_jobs SET state='queued',attempts=0,error_code='',available_at=now(),updated_at=now() WHERE owner_id=$1 AND id=$2 AND state IN ('failed','blocked')", u.owner(), id)
	return err == nil && tag.RowsAffected() > 0, err
}

func (u *commandUnit) AuthorizeRecovery(ctx context.Context, job, request string) error {
	return (callstore.BackgroundCalls{Store: u.store.Calls}).AuthorizeRecoveryTx(ctx, u.tx, u.scope.OwnerID, memory.ID(job), request)
}

func (w *commandUnit) Remember(ctx context.Context, text, nature, projectID string, source workspace.SourceRef) (string, error) {
	ref, err := w.store.RememberTx(ctx, w.tx, w.scope, claimstore.Statement{Text: text, Nature: nature, ProjectID: projectID, Source: memory.Ref{ID: memory.ID(source.SourceID), Version: source.Version, Kind: memory.SourceKind}})
	return string(ref.ID), err
}

func (w *commandUnit) Item(ctx context.Context, id string) (workspace.Item, error) {
	return itemstore.GetItem(ctx, w.tx, w.scope, id)
}

func (w *commandUnit) SaveItem(ctx context.Context, item workspace.Item) error {
	return itemstore.SaveItem(ctx, w.tx, w.scope, item)
}

func (e *commandUnit) Location(ctx context.Context) (*time.Location, error) {
	settings, err := kernel.QueryDocument[workspace.Settings](ctx, e.tx, "SELECT settings FROM workspace_owners WHERE owner_id=$1", string(e.scope.OwnerID))
	if err != nil {
		return nil, err
	}
	loc, err := time.LoadLocation(settings.Timezone)
	if err != nil {
		loc = time.UTC
	}
	return loc, nil
}

func (e *commandUnit) PromoteIdea(ctx context.Context, ideaID string, task workspace.Item) error {
	if err := itemstore.SaveItem(ctx, e.tx, e.scope, task); err != nil {
		return err
	}
	return accessstore.PromoteArtifactsTx(ctx, e.tx, e.scope, ideaID, task.ID)
}
