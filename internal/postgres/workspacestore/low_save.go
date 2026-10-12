package workspacestore

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/postgres/actionlog"
	"github.com/soaringjerry/PCAS/internal/postgres/intakestore"
	"github.com/soaringjerry/PCAS/internal/postgres/itemstore"
	"github.com/soaringjerry/PCAS/internal/postgres/kernel"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func SaveDoc(ctx context.Context, tx pgx.Tx, scope memory.Scope, doc workspace.Doc) error {
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

func (s *Workspace) SaveAction(ctx context.Context, tx pgx.Tx, scope memory.Scope, item workspace.Item, summary string) error {
	if history, ok := ctx.Value(kernel.SecretaryHistoryKey{}).(*intakestore.SecretaryHistory); ok {
		history.Record(item.ID, summary)
		return itemstore.SaveItem(ctx, tx, scope, item)
	}
	revision := workspace.Revision{At: kernel.Stamp(), By: kernel.ActorFromContext(ctx), Summary: summary}
	item.History = append(item.History, revision)
	if item.Kind == "idea" {
		item.Evolution = append(item.Evolution, revision)
	}
	if actionlog.SmokeID(ctx) != "" {
		return itemstore.SaveItem(ctx, tx, scope, item)
	}
	// Action records remain authoritative for execution; memory retains a versioned receipt.
	source, err := s.IngestTx(ctx, tx, scope, memory.IngestRequest{Connector: "actions", ExternalID: item.ID, ExternalVersion: fmt.Sprint(item.Version), Title: item.Title, Text: string(kernel.JSON(item)), MediaType: "text/plain"})
	if err != nil {
		return err
	}
	item.Sources = append(item.Sources, workspace.SourceRef{SourceID: string(source.ID), Version: source.Version, Label: summary, At: revision.At})
	return itemstore.SaveItem(ctx, tx, scope, item)
}
