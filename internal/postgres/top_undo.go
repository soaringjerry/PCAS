package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/postgres/actionlog"
	"github.com/soaringjerry/PCAS/internal/postgres/intakestore"
	"github.com/soaringjerry/PCAS/internal/postgres/itemstore"
	"github.com/soaringjerry/PCAS/internal/postgres/kernel"
	"github.com/soaringjerry/PCAS/internal/postgres/workspacestore"
	"github.com/soaringjerry/PCAS/internal/postgres/workstore"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func (s *Store) undoActionTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, id string) error {
	if handled, err := s.UndoTopicProjectTx(ctx, tx, scope, id); handled {
		return err
	}
	if handled, err := undoComparisonActionTx(ctx, tx, scope, id); handled {
		return err
	}
	ctx = kernel.WithActor(ctx, "user")
	if !memory.ID(id).Valid() {
		return memory.ErrInvalid
	}
	var data []byte
	var summary string
	var undone *string
	var expired bool
	var created time.Time
	var order *int64
	var source string
	var turnID *string
	err := tx.QueryRow(ctx, "SELECT changes,summary,undone_at::text,expired_at IS NOT NULL OR created_at<now()-interval '30 days',created_at,action_order,source,turn_id::text FROM action_log WHERE owner_id=$1 AND id=$2 FOR UPDATE", string(scope.OwnerID), id).Scan(&data, &summary, &undone, &expired, &created, &order, &source, &turnID)
	if errors.Is(err, pgx.ErrNoRows) {
		return memory.ErrNotFound
	}
	if err != nil {
		return err
	}
	{
		var checkID string
		if e := tx.QueryRow(ctx, "SELECT smoke_id::text FROM desk_smoke_actions WHERE owner_id=$1 AND action_id=$2", string(scope.OwnerID), id).Scan(&checkID); e == nil {
			ctx = actionlog.WithSmoke(ctx, checkID)
		} else if !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
	}
	if undone != nil {
		return workspace.ErrAlreadyUndone
	}
	// Enforce the window even before another action flushes old snapshots.
	if expired {
		return workspace.ErrExpired
	}
	var changes []actionlog.ActionChange
	if err = json.Unmarshal(data, &changes); err != nil {
		return err
	}
	// Recorded successors take precedence over external fingerprint changes and
	// work-start checks. The owner lock serializes all logged document writers.
	if err = workspacestore.CheckActionSuccessors(ctx, tx, scope, id, data, created, order, source, turnID); err != nil {
		return err
	}
	// Lock all rows first. The owner lock serializes commands and undo; run locks
	// fence the worker, which does not take the owner lock when claiming work.
	for _, c := range changes {
		if c.Table == "deadline_completion" {
			if err := workstore.CheckDeadlineCompletionTx(ctx, tx, scope, c); err != nil {
				return err
			}
			continue
		}
		if c.Table == "source_attachments" {
			var ref memory.Ref
			if json.Unmarshal(c.Before, &ref) != nil || ref.Kind != memory.SourceKind || string(ref.ID) != c.ID || ref.Version < 1 {
				return memory.ErrInvalid
			}
			var active bool
			err := tx.QueryRow(ctx, "SELECT r.state='active' AND r.version=$3 AND v.blob_key IS NOT NULL FROM memory_records r JOIN source_versions v ON(v.owner_id,v.source_id,v.version)=(r.owner_id,r.id,r.version) WHERE r.owner_id=$1 AND r.id=$2 FOR UPDATE OF r", string(scope.OwnerID), c.ID, ref.Version).Scan(&active)
			if errors.Is(err, pgx.ErrNoRows) || err == nil && !active {
				return workspace.ErrExpired
			}
			if err != nil {
				return err
			}
			continue
		}
		if !kernel.OneOf(c.Table, "work_items", "work_documents", "agent_runs", "training_samples") {
			return memory.ErrInvalid
		}
		if c.Table == "agent_runs" && string(c.Before) == "null" {
			var status string
			e := tx.QueryRow(ctx, "SELECT status FROM agent_runs WHERE owner_id=$1 AND id=$2 FOR UPDATE", string(scope.OwnerID), c.ID).Scan(&status)
			if e != nil && !errors.Is(e, pgx.ErrNoRows) {
				return e
			}
			if e == nil && status != "queued" {
				return workspace.ErrWorkStarted
			}
		}
		if c.Table == "work_items" && string(c.Before) == "null" {
			rows, err := tx.Query(ctx, "SELECT status FROM agent_runs WHERE owner_id=$1 AND thing_id=$2 FOR UPDATE", string(scope.OwnerID), c.ID)
			if err != nil {
				return err
			}
			started := false
			for rows.Next() {
				var status string
				if err = rows.Scan(&status); err != nil {
					rows.Close()
					return err
				}
				if status != "queued" {
					started = true
				}
			}
			rows.Close()
			if err = rows.Err(); err != nil {
				return err
			}
			if started {
				return workspace.ErrWorkStarted
			}
		}
		// Use the trigger's content fingerprint; retain full-document matching for
		// actions collected before migration 019, whose after snapshots are absent.
		var hash, legacyHash string
		err = tx.QueryRow(ctx, "SELECT action_document_hash(document),encode(sha256(convert_to(document::text,'UTF8')),'hex') FROM "+c.Table+" WHERE owner_id=$1 AND id=$2 FOR UPDATE", string(scope.OwnerID), c.ID).Scan(&hash, &legacyHash)
		if errors.Is(err, pgx.ErrNoRows) {
			if c.AfterHash != nil {
				return workspace.ErrChangedSince
			}
		} else if err != nil {
			return err
		} else if c.AfterHash == nil || (hash != *c.AfterHash && legacyHash != *c.AfterHash) {
			return workspace.ErrChangedSince
		}
	}
	for i := len(changes) - 1; i >= 0; i-- {
		c := changes[i]
		if c.Table == "deadline_completion" {
			if err := workstore.RestoreDeadlineCompletionTx(ctx, tx, scope, c); err != nil {
				return err
			}
			continue
		}
		if c.Table == "source_attachments" {
			var ref memory.Ref
			if err := json.Unmarshal(c.Before, &ref); err != nil {
				return err
			}
			if err := s.DeleteTx(context.WithValue(ctx, intakestore.RetainUndoneAnswersKey{}, true), tx, scope, memory.DeleteRequest{Targets: []memory.Ref{ref}}); err != nil {
				return err
			}
			continue
		}
		if string(c.Before) == "null" {
			if c.Table == "work_items" {
				var referenced bool
				if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM work_items WHERE owner_id=$1 AND project_id=$2)", string(scope.OwnerID), c.ID).Scan(&referenced); err != nil {
					return err
				}
				if referenced {
					project, e := itemstore.GetItem(ctx, tx, scope, c.ID)
					if e != nil {
						return e
					}
					if project.Kind != "project" || project.Creation == nil || project.Creation.By != "secretary" {
						return workspace.ErrChangedSince
					}
					items, e := kernel.QueryDocuments[workspace.Item](ctx, tx, "SELECT document FROM work_items WHERE owner_id=$1 AND project_id=$2 ORDER BY id FOR UPDATE", string(scope.OwnerID), c.ID)
					if e != nil {
						return e
					}
					for _, item := range items {
						item.ProjectID = ""
						item.Version++
						item.UpdatedAt = kernel.Stamp()
						if e = s.SaveAction(ctx, tx, scope, item, "撤销项目，事项回到无项目"); e != nil {
							return e
						}
					}
				}
				// Removing queued rows releases their reserved_cost from the daily sum.
				if _, err = tx.Exec(ctx, "DELETE FROM agent_runs WHERE owner_id=$1 AND thing_id=$2", string(scope.OwnerID), c.ID); err != nil {
					return err
				}
			}
			if c.Table == "work_items" {
				err = itemstore.DeleteItem(ctx, tx, scope.OwnerID, c.ID)
			} else {
				_, err = tx.Exec(ctx, "DELETE FROM "+c.Table+" WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), c.ID)
			}
		} else {
			switch c.Table {
			case "work_items":
				var item workspace.Item
				if err = json.Unmarshal(c.Before, &item); err != nil {
					return err
				}
				current, e := itemstore.GetItem(ctx, tx, scope, c.ID)
				if e != nil {
					return e
				}
				if err = workspacestore.SuppressRestoredRemindersTx(ctx, tx, scope, current, item, time.Now()); err != nil {
					return err
				}
				item.Version = current.Version + 1
				item.UpdatedAt = kernel.Stamp()
				err = s.SaveAction(ctx, tx, scope, item, "撤销："+summary)
			case "work_documents":
				var doc workspace.Doc
				if err = json.Unmarshal(c.Before, &doc); err == nil {
					err = workspacestore.RestoreDocumentTx(ctx, tx, scope, doc)
				}
			case "agent_runs":
				var run workspace.Run
				if err = json.Unmarshal(c.Before, &run); err == nil {
					_, err = tx.Exec(ctx, "UPDATE agent_runs SET document=$3,status=$4 WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), c.ID, c.Before, run.Status)
				}
			}
		}
		if err != nil {
			return err
		}
	}
	_, err = tx.Exec(ctx, "UPDATE action_log SET undone_at=now() WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), id)
	if err != nil {
		return err
	}
	if err := intakestore.RefreshSmokeChangesTx(ctx, tx, scope, changes); err != nil {
		return err
	}
	if err := s.DeleteUndoneAdoptionSourcesTx(ctx, tx, scope, id); err != nil {
		return err
	}
	if source != "desk" || turnID == nil {
		return nil
	}
	return s.DeleteUndoneTurnMemoriesTx(ctx, tx, scope, *turnID)
}

func (s *Store) Undo(ctx context.Context, scope memory.Scope, actionID string) (workspace.State, error) {
	var out workspace.State
	if err := kernel.RequireOwner(scope); err != nil {
		return out, err
	}
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		if err := s.EnsureOwner(ctx, tx, scope); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "SELECT 1 FROM workspace_owners WHERE owner_id=$1 FOR UPDATE", string(scope.OwnerID)); err != nil {
			return err
		}
		if err := s.undoActionTx(ctx, tx, scope, actionID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "UPDATE workspace_owners SET revision=revision+1 WHERE owner_id=$1", string(scope.OwnerID)); err != nil {
			return err
		}
		var err error
		out, err = s.SnapshotTx(ctx, tx, scope)
		return err
	})
	return out, err
}
