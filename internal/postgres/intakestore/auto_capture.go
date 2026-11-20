package intakestore

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/interpretation/extraction"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/postgres/actionlog"
	"github.com/soaringjerry/PCAS/internal/postgres/itemstore"
	"github.com/soaringjerry/PCAS/internal/postgres/kernel"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// captureActionTx makes the task or the idea of an extracted item, or keeps
// it as a candidate. extraction.CaptureItem decides.
func (s *Intake) captureActionTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, source memory.SourceResult, item ExtractedItem, imported bool, project string) error {
	return extraction.CaptureItem(ctx, captureUnit{tx: tx, scope: scope}, source, item, imported, project, time.Now())
}

// captureUnit is the storage of the capture of one item.
type captureUnit struct {
	tx    pgx.Tx
	scope memory.Scope
}

func (u captureUnit) Captured(ctx context.Context, source memory.Ref, kind, quote, text string) (bool, error) {
	var duplicate bool
	err := u.tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM capture_candidates WHERE owner_id=$1 AND source_id=$2
 AND document->>'kind'=$3 AND (document->'source'->>'excerpt'=$4 OR document->>'text'=$5))`, string(u.scope.OwnerID), string(source.ID), kind, quote, text).Scan(&duplicate)
	return duplicate, err
}

func (u captureUnit) AutomaticTasks(ctx context.Context, source memory.Ref, conversation string) (int, error) {
	var count int
	err := u.tx.QueryRow(ctx, `SELECT count(*) FROM capture_candidates c WHERE c.owner_id=$1 AND c.document->>'kind'='task' AND c.state='accepted'
 AND (c.source_id=$2 OR $3<>'' AND EXISTS(SELECT 1 FROM source_contexts sc WHERE sc.owner_id=c.owner_id AND sc.source_id=c.source_id AND sc.conversation_key=$3))`, string(u.scope.OwnerID), string(source.ID), conversation).Scan(&count)
	return count, err
}

func (u captureUnit) Overflow(ctx context.Context, reason string, count int) error {
	return kernel.StageEvent(ctx, u.tx, u.scope.OwnerID, "source.extract", "overflow", reason, count)
}

func (u captureUnit) Create(ctx context.Context, item workspace.Item, action, summary string) error {
	actionCtx := actionlog.WithActionLog(kernel.WithActor(ctx, "ai"), action, "background_extraction", "", summary)
	if err := actionlog.BeginActionLogTx(actionCtx, u.tx); err != nil {
		return err
	}
	if err := itemstore.SaveItem(actionCtx, u.tx, u.scope, item); err != nil {
		return err
	}
	return actionlog.FlushActionLog(actionCtx, u.tx, u.scope)
}

func (u captureUnit) SaveCandidate(ctx context.Context, candidate workspace.Candidate) error {
	return SaveCandidate(ctx, u.tx, u.scope, candidate)
}
