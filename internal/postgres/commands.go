package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/soaringjerry/PCAS/internal/postgres/itemstore"
	"github.com/soaringjerry/PCAS/internal/postgres/kernel"
	"github.com/soaringjerry/PCAS/internal/postgres/ownerstore"
	"github.com/soaringjerry/PCAS/internal/workspace/itemcommand"
	"github.com/soaringjerry/PCAS/internal/workspace/settings"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func (s *Store) commandTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, c workspace.Command) error {
	switch c.Type {
	case "completeDeadline":
		if !memory.ID(c.ID).Valid() {
			return memory.ErrInvalid
		}
		var claim string
		var version int
		if err := tx.QueryRow(ctx, "SELECT claim_id::text,claim_version FROM deadlines WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), c.ID).Scan(&claim, &version); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return memory.ErrNotFound
			}
			return err
		}
		if c.MemoryID != "" && c.MemoryID != claim {
			return memory.ErrInvalid
		}
		if err := activeClaim(ctx, tx, scope, claim); err != nil {
			return err
		}
		return completeDeadlineTx(ctx, tx, scope, claim, version, c.Reason)
	case "restoreMemory":
		return restoreMemoryTx(ctx, tx, scope, c.ID)
	case "undoEntityMerge":
		return s.undoEntityCommandTx(ctx, tx, scope, c.ID)
	case "undoAction":
		return s.undoActionTx(ctx, tx, scope, c.ID)
	case "delegateTask":
		// One explicit delegation atomically creates the work and queues its run.
		// Execute's request receipt fences retries; a stable item ID also fences
		// replays under a new request ID after a lost response/reload.
		if !memory.ID(c.ID).Valid() || kernel.RequireText(c.Prompt) != nil {
			return memory.ErrInvalid
		}
		agent, err := kernel.QueryDocument[workspace.Agent](ctx, tx, "SELECT document FROM workspace_agents WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), c.AgentID)
		if err != nil || !agent.Enabled || agent.Channel == "manual" || s.Models == nil || !s.Models.Available(agent.ID) {
			return memory.ErrUnavailable
		}
		create := c
		create.Type = "addTask"
		create.Text = c.Prompt
		if err := s.commandTx(ctx, tx, scope, create); err != nil {
			return err
		}
		run := c
		run.Type, run.ID, run.ThingID, run.Kind = "requestRun", "", c.ID, c.Kind
		if run.Kind == "" {
			run.Kind = "draft"
		}
		return s.runCommandTx(ctx, tx, scope, run)
	case "capture":
		if err := kernel.RequireText(c.Text); err != nil {
			return err
		}
		in, err := s.ingestTx(ctx, tx, scope, memory.IngestRequest{Connector: "capture", ExternalID: c.RequestID, ExternalVersion: "1", Title: "快速记录", Text: c.Text, MediaType: "text/plain"})
		if err != nil {
			return err
		}
		candidate := workspace.Candidate{ID: string(memory.NewID()), Kind: "unknown", Text: c.Text, Confidence: 0, Source: workspace.SourceRef{SourceID: string(in.ID), Version: in.Version, Label: "快速记录", Excerpt: c.Text, At: kernel.Stamp()}, State: "pending", CreatedAt: kernel.Stamp()}
		return saveCandidate(ctx, tx, scope, candidate)
	case "acceptCandidate", "ignoreCandidate", "restoreCandidate", "mergeCandidate":
		if !memory.ID(c.ID).Valid() {
			return memory.ErrInvalid
		}
		v, err := kernel.QueryDocument[workspace.Candidate](ctx, tx, "SELECT document FROM capture_candidates WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), c.ID)
		if err != nil {
			return err
		}
		if err := itemcommand.ReviewCandidate(ctx, &v, c, candidateWrites{store: s, tx: tx, scope: scope}); err != nil {
			return err
		}
		return saveCandidate(ctx, tx, scope, v)
	case "addTask", "addIdea", "addProject":
		origin := itemcommand.Origin{Actor: kernel.ActorFromContext(ctx), Collected: ctx.Value(secretaryHistoryKey{}) != nil}
		if original, ok := ctx.Value(secretaryCreationKey{}).(workspace.SourceRef); ok {
			origin.SecretarySource = &original
			if log, ok := ctx.Value(actionLogKey{}).(actionLog); ok {
				origin.ActionID = log.id
			}
		}
		item, err := itemcommand.New(ctx, c, origin, itemEffects{tx: tx, scope: scope})
		if err != nil {
			return err
		}
		id, err := uuidOrNew(c.ID)
		if err != nil {
			return err
		}
		item.ID = id
		var exists bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM work_items WHERE owner_id=$1 AND id=$2)", string(scope.OwnerID), id).Scan(&exists); err != nil {
			return err
		}
		if exists {
			return memory.ErrConflict
		}
		return s.saveAction(ctx, tx, scope, item, "创建")
	case "bulkStatus", "bulkDefer", "bulkMove", "bulkAccept", "bulkIgnore":
		commands, err := itemcommand.Expand(c, func(id string) (workspace.Candidate, error) {
			return kernel.QueryDocument[workspace.Candidate](ctx, tx, "SELECT document FROM capture_candidates WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), id)
		})
		if err != nil {
			return err
		}
		for _, next := range commands {
			if err := s.commandTx(ctx, tx, scope, next); err != nil {
				return err
			}
		}
		return nil
	case "editMemory", "confirmMemory", "deleteMemory", "setMemoryVisibility", "pinMemory":
		return s.memoryCommandTx(ctx, tx, scope, c)
	case "requestRun", "pasteRunResult", "adoptRun", "discardRun":
		return s.runCommandTx(ctx, tx, scope, c)
	case "createDoc", "updateDoc", "deleteDoc":
		var doc workspace.Doc
		if c.Type == "createDoc" {
			if c.Doc == nil {
				return memory.ErrInvalid
			}
			doc = *c.Doc
			doc.Version = 0
			doc.BasedOn = nil
			if !memory.ID(doc.ID).Valid() || doc.By != "user" || doc.RunID != "" {
				return memory.ErrInvalid
			}
			if _, err := itemstore.GetItem(ctx, tx, scope, doc.ThingID); err != nil {
				return err
			}
			var exists bool
			if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM work_documents WHERE owner_id=$1 AND id=$2)", string(scope.OwnerID), doc.ID).Scan(&exists); err != nil {
				return err
			}
			if exists {
				return memory.ErrConflict
			}
			doc.CreatedAt = kernel.Stamp()
		} else {
			if !memory.ID(c.ID).Valid() {
				return memory.ErrInvalid
			}
			var err error
			doc, err = kernel.QueryDocument[workspace.Doc](ctx, tx, "SELECT document FROM work_documents WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), c.ID)
			if err != nil {
				return err
			}
			if c.Type == "deleteDoc" {
				_, err := tx.Exec(ctx, "DELETE FROM work_documents WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), c.ID)
				return err
			}
			base := doc.Version
			doc.BasedOn = &base
			doc.By = kernel.ActorFromContext(ctx)
			if err := patchAllowed(&doc, c.Patch, "title", "body"); err != nil {
				return err
			}
		}
		if err := kernel.RequireText(doc.Title); err != nil {
			return err
		}
		doc.UpdatedAt = kernel.Stamp()
		return saveDoc(ctx, tx, scope, doc)
	case "updateSettings":
		if err := settings.CheckZone(c.Patch); err != nil {
			return err
		}
		current, err := kernel.QueryDocument[workspace.Settings](ctx, tx, "SELECT settings FROM workspace_owners WHERE owner_id=$1", string(scope.OwnerID))
		if err != nil {
			return err
		}
		next, replan, err := settings.Apply(current, c.Patch)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, "UPDATE workspace_owners SET settings=$2 WHERE owner_id=$1", string(scope.OwnerID), kernel.JSON(next)); err != nil {
			return err
		}
		if replan {
			return refreshTaskPlansTx(ctx, tx, scope)
		}
		return nil
	case "updateAgent":
		current, err := kernel.QueryDocument[workspace.Agent](ctx, tx, "SELECT document FROM workspace_agents WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), c.ID)
		if err != nil {
			return err
		}
		a, initialize, err := settings.ApplyAgent(current, c.Patch)
		if err != nil {
			return err
		}
		if initialize {
			if err := ownerstore.InitializeAgentMemoriesTx(ctx, tx, scope.OwnerID, a.ID); err != nil {
				return err
			}
			a.MemoryInitialized = true
		}
		_, err = tx.Exec(ctx, "UPDATE workspace_agents SET document=$3 WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), c.ID, kernel.JSON(a))
		return err
	case "toggleContextMemory":
		if _, err := itemstore.GetItem(ctx, tx, scope, c.ThingID); err != nil {
			return err
		}
		if !memory.ID(c.MemoryID).Valid() {
			return memory.ErrInvalid
		}
		if err := activeClaim(ctx, tx, scope, c.MemoryID); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, "DELETE FROM context_exclusions WHERE owner_id=$1 AND thing_id=$2 AND memory_id=$3", string(scope.OwnerID), c.ThingID, c.MemoryID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			_, err = tx.Exec(ctx, "INSERT INTO context_exclusions(owner_id,thing_id,memory_id) VALUES($1,$2,$3)", string(scope.OwnerID), c.ThingID, c.MemoryID)
		}
		return err
	case "setSampleState":
		if !memory.ID(c.ID).Valid() || !kernel.OneOf(c.State, "included", "excluded", "candidate") {
			return memory.ErrInvalid
		}
		tag, err := tx.Exec(ctx, "UPDATE training_samples SET state=$3 WHERE owner_id=$1 AND id=$2 AND (NOT stale OR $3!='included')", string(scope.OwnerID), c.ID, c.State)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return memory.ErrConflict
		}
		return nil
	case "retryJob":
		if !memory.ID(c.ID).Valid() {
			return memory.ErrInvalid
		}
		tag, err := tx.Exec(ctx, "UPDATE memory_jobs SET state='queued',attempts=0,error_code='',available_at=now(),updated_at=now() WHERE owner_id=$1 AND id=$2 AND state IN ('failed','blocked')", string(scope.OwnerID), c.ID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return memory.ErrConflict
		}
		// Only this explicit owner command releases a terminal recovery pause.
		// Keep its old attempt records and link the next bounded chain to them.
		return (backgroundCalls{store: s}).authorizeRecoveryTx(ctx, tx, scope.OwnerID, memory.ID(c.ID), c.RequestID)
	case "deleteThing":
		return s.deleteThingTx(ctx, tx, scope, c.ID)
	}
	// Remaining commands operate on a single action record.
	item, err := itemstore.GetItem(ctx, tx, scope, itemcommand.Target(c))
	if err != nil {
		return err
	}
	summary, err := itemcommand.Apply(ctx, &item, c, itemEffects{tx: tx, scope: scope})
	if err != nil {
		return err
	}
	return s.saveAction(ctx, tx, scope, item, summary)
}

// candidateWrites gives the review of a candidate its writes.
type candidateWrites struct {
	store *Store
	tx    pgx.Tx
	scope memory.Scope
}

func (w candidateWrites) Remember(ctx context.Context, text, nature, projectID string, source workspace.SourceRef) (string, error) {
	ref, err := w.store.rememberTx(ctx, w.tx, w.scope, statement{Text: text, Nature: nature, ProjectID: projectID, Source: memory.Ref{ID: memory.ID(source.SourceID), Version: source.Version, Kind: memory.SourceKind}})
	return string(ref.ID), err
}

func (w candidateWrites) Item(ctx context.Context, id string) (workspace.Item, error) {
	return itemstore.GetItem(ctx, w.tx, w.scope, id)
}

func (w candidateWrites) SaveItem(ctx context.Context, item workspace.Item) error {
	return itemstore.SaveItem(ctx, w.tx, w.scope, item)
}

// itemEffects gives an item command the owner's time zone and the writes of
// an idea that becomes a task.
type itemEffects struct {
	tx    pgx.Tx
	scope memory.Scope
}

func (e itemEffects) Location(ctx context.Context) (*time.Location, error) {
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

func (e itemEffects) PromoteIdea(ctx context.Context, ideaID string, task workspace.Item) error {
	if err := itemstore.SaveItem(ctx, e.tx, e.scope, task); err != nil {
		return err
	}
	return promoteArtifactsTx(ctx, e.tx, e.scope, ideaID, task.ID)
}

func saveCandidate(ctx context.Context, tx pgx.Tx, scope memory.Scope, v workspace.Candidate) error {
	_, err := tx.Exec(ctx, `INSERT INTO capture_candidates(owner_id,id,source_id,source_version,state,document) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(owner_id,id) DO UPDATE SET state=excluded.state,document=excluded.document`, string(scope.OwnerID), v.ID, v.Source.SourceID, v.Source.Version, v.State, kernel.JSON(v))
	return err
}
func saveDoc(ctx context.Context, tx pgx.Tx, scope memory.Scope, doc workspace.Doc) error {
	var exists bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM work_documents WHERE owner_id=$1 AND id=$2)", string(scope.OwnerID), doc.ID).Scan(&exists); err != nil {
		return err
	}
	if exists {
		_, err := tx.Exec(ctx, "UPDATE work_documents SET document=$3 WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), doc.ID, kernel.JSON(doc))
		return err
	}
	_, err := tx.Exec(ctx, "INSERT INTO work_documents(owner_id,id,thing_id,document) VALUES($1,$2,$3,$4)", string(scope.OwnerID), doc.ID, doc.ThingID, kernel.JSON(doc))
	return err
}
func (s *Store) saveAction(ctx context.Context, tx pgx.Tx, scope memory.Scope, item workspace.Item, summary string) error {
	if history, ok := ctx.Value(secretaryHistoryKey{}).(*secretaryHistory); ok {
		history.record(item.ID, summary)
		return itemstore.SaveItem(ctx, tx, scope, item)
	}
	revision := workspace.Revision{At: kernel.Stamp(), By: kernel.ActorFromContext(ctx), Summary: summary}
	item.History = append(item.History, revision)
	if item.Kind == "idea" {
		item.Evolution = append(item.Evolution, revision)
	}
	if smokeID(ctx) != "" {
		return itemstore.SaveItem(ctx, tx, scope, item)
	}
	// Action records remain authoritative for execution; memory retains a versioned receipt.
	source, err := s.ingestTx(ctx, tx, scope, memory.IngestRequest{Connector: "actions", ExternalID: item.ID, ExternalVersion: fmt.Sprint(item.Version), Title: item.Title, Text: string(kernel.JSON(item)), MediaType: "text/plain"})
	if err != nil {
		return err
	}
	item.Sources = append(item.Sources, workspace.SourceRef{SourceID: string(source.ID), Version: source.Version, Label: summary, At: revision.At})
	return itemstore.SaveItem(ctx, tx, scope, item)
}

func activeClaim(ctx context.Context, tx pgx.Tx, scope memory.Scope, id string) error {
	var exists bool
	err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM memory_records WHERE owner_id=$1 AND id=$2 AND kind='claim' AND state='active')", string(scope.OwnerID), id).Scan(&exists)
	if err != nil {
		return err
	}
	if !exists {
		return memory.ErrNotFound
	}
	return nil
}
func sampleTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, sample workspace.Sample) error {
	_, err := tx.Exec(ctx, "INSERT INTO training_samples(owner_id,id,run_id,memory_id,state,stale,document) VALUES($1,$2,$3,$4,$5,$6,$7)", string(scope.OwnerID), sample.ID, kernel.NullString(sample.Origin.RunID), kernel.NullString(sample.Origin.MemoryID), sample.State, sample.Stale, kernel.JSON(sample))
	return err
}

// The wire format is deliberately a small explicit command vocabulary. Client
// supplied run outputs, costs, provenance, and processing states are not trusted.

// deleteThingTx removes a to-do, idea or empty project for good: its
// reminders, documents and agent runs go with it, other to-dos stop depending
// on it, and the receipts memory kept of its changes are deleted like any
// other material. It is confirmed in the interface and cannot be undone.
func (s *Store) deleteThingTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, id string) error {
	item, err := itemstore.GetItem(ctx, tx, scope, id)
	if err != nil {
		return err
	}
	var children, running bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM work_items WHERE owner_id=$1 AND project_id=$2),
		EXISTS(SELECT 1 FROM agent_runs WHERE owner_id=$1 AND thing_id=$2 AND status='running')`, string(scope.OwnerID), item.ID).Scan(&children, &running); err != nil {
		return err
	}
	if children {
		return memory.ErrConflict
	}
	if running {
		return workspace.ErrWorkStarted
	}
	var receipt memory.Ref
	err = tx.QueryRow(ctx, `SELECT s.id::text,r.version FROM sources s JOIN memory_records r ON (r.owner_id,r.id)=(s.owner_id,s.id)
		WHERE s.owner_id=$1 AND s.connector='actions' AND s.external_id=$2 AND r.state='active'`, string(scope.OwnerID), item.ID).Scan(&receipt.ID, &receipt.Version)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if err == nil {
		receipt.Kind = memory.SourceKind
		if err := s.deleteRecordsTx(ctx, tx, scope, memory.DeleteRequest{Targets: []memory.Ref{receipt}}); err != nil {
			return err
		}
	}
	if err := itemstore.RemoveItemDependency(ctx, tx, scope.OwnerID, item.ID); err != nil {
		return err
	}
	// Undo keeps earlier versions of the item, its documents, its agent runs
	// and the samples drawn from them. A deletion empties those too: the
	// trigger does it for the item and its documents once this setting names
	// the owner, and the runs and samples are named here.
	var previous string
	if err := tx.QueryRow(ctx, "SELECT coalesce(current_setting('pcas.expire_actions',true),'')").Scan(&previous); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "SELECT set_config('pcas.expire_actions',$1,true)", string(scope.OwnerID)); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE action_log SET changes='[]'::jsonb,expired_at=now()
		WHERE owner_id=$1 AND expired_at IS NULL AND EXISTS(SELECT 1 FROM jsonb_array_elements(changes) c WHERE
		 (c->>'table'='agent_runs' AND c->>'id' IN (SELECT id::text FROM agent_runs WHERE owner_id=$1 AND thing_id=$2))
		 OR (c->>'table'='training_samples' AND c->>'id' IN (SELECT t.id::text FROM training_samples t JOIN agent_runs r ON (r.owner_id,r.id)=(t.owner_id,t.run_id) WHERE r.owner_id=$1 AND r.thing_id=$2))
		 OR (c->>'table'='work_items' AND c->>'id'=$2::text))`, string(scope.OwnerID), item.ID); err != nil {
		return err
	}
	// Samples drawn from this item's agent runs refer to those runs and go first.
	if _, err := tx.Exec(ctx, "DELETE FROM training_samples WHERE owner_id=$1 AND run_id IN (SELECT id FROM agent_runs WHERE owner_id=$1 AND thing_id=$2)", string(scope.OwnerID), item.ID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "DELETE FROM agent_runs WHERE owner_id=$1 AND thing_id=$2", string(scope.OwnerID), item.ID); err != nil {
		return err
	}
	if err := itemstore.DeleteItem(ctx, tx, scope.OwnerID, item.ID); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, "SELECT set_config('pcas.expire_actions',$1,true)", previous)
	return err
}
