package deskstore

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/postgres/actionlog"
	"github.com/soaringjerry/PCAS/internal/postgres/intakestore"
	"github.com/soaringjerry/PCAS/internal/postgres/itemstore"
	"github.com/soaringjerry/PCAS/internal/postgres/kernel"
	"github.com/soaringjerry/PCAS/internal/postgres/workstore"
	"github.com/soaringjerry/PCAS/internal/workspace"
	"github.com/soaringjerry/PCAS/internal/workspace/secretary"
)

type (
	SecretaryAction = secretary.Action
	SecretaryOutput = secretary.Output
)

// The memories this turn's prompt showed, by their M* alias; close_date
// resolves its ref through them and nothing else.
type secretaryMemoriesKey struct{}

type secretaryProjectReceiptsKey struct{}

func (s *Desk) secretaryProjectTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, value string, aliases map[string]workspace.Item) (string, error) {
	if value == "" || value == "none" {
		return "", nil
	}
	if p, ok := aliases[value]; ok && p.Kind == "project" {
		return p.ID, nil
	}
	if strings.HasPrefix(value, "new:") {
		name := strings.TrimSpace(strings.TrimPrefix(value, "new:"))
		if !workspace.ValidTitle(name) {
			return "", memory.ErrInvalid
		}
		var id string
		err := tx.QueryRow(ctx, "SELECT id::text FROM work_items WHERE owner_id=$1 AND kind='project' AND lower(btrim(title))=lower($2) ORDER BY created_at,id LIMIT 1", string(scope.OwnerID), name).Scan(&id)
		if err == nil {
			return id, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return "", err
		}
		id = string(memory.NewID())
		// A companion project is a distinct reversible action. Restore the
		// enclosing task buffer so task undo does not also remove its project.
		var buffer string
		if err = tx.QueryRow(ctx, "SELECT current_setting('pcas.action_changes',true)").Scan(&buffer); err != nil {
			return "", err
		}
		log, _ := ctx.Value(actionlog.ActionLogKey{}).(actionlog.ActionLog)
		actionID := string(memory.NewID())
		projectCtx := actionlog.WithActionLog(ctx, actionID, "desk", log.TurnID, "建了项目「"+name+"」")
		projectCtx = context.WithValue(projectCtx, kernel.SecretaryHistoryKey{}, nil)
		if err = actionlog.BeginActionLogTx(projectCtx, tx); err != nil {
			return "", err
		}
		if err = s.CommandTx(projectCtx, tx, scope, workspace.Command{Type: "addProject", ID: id, Name: name}); err != nil {
			return "", err
		}
		if err = actionlog.FlushActionLog(projectCtx, tx, scope); err != nil {
			return "", err
		}
		if _, err = tx.Exec(ctx, "SELECT set_config('pcas.action_changes',$1,true)", buffer); err != nil {
			return "", err
		}
		if receipts, ok := ctx.Value(secretaryProjectReceiptsKey{}).(*[]workspace.DeskReceipt); ok {
			*receipts = append(*receipts, workspace.DeskReceipt{ActionID: &actionID, Op: "create_project", Text: "建了项目「" + name + "」", ThingID: &id, Undoable: true, Status: "done"})
		}
		return id, nil
	}
	return "", memory.ErrInvalid
}

// executeSecretaryActionTx does one action of the answer in the action's
// transaction. The rules of each action are in the secretary package.
func (s *Desk) ExecuteSecretaryActionTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, a SecretaryAction, aliases map[string]workspace.Item, agent workspace.Agent, loc *time.Location, currentThingID string) (workspace.DeskReceipt, error) {
	w := &secretaryWorkspace{store: s, tx: tx, scope: scope, history: &intakestore.SecretaryHistory{Summaries: map[string]string{}}}
	// The documents and the delegate history of the turn travel in its context.
	w.documents, _ = ctx.Value(secretaryDocumentsKey{}).(map[string]workspace.Doc)
	if history, ok := ctx.Value(kernel.DelegateHistoryKey{}).(intakestore.DelegateHistoryContext); ok {
		w.delegateTurns = append([]string{}, history.IDs...)
	}
	return secretary.Execute(ctx, w, a, secretary.Env{Aliases: aliases, Agent: agent, Location: loc, CurrentThingID: currentThingID})
}

// secretaryWorkspace gives one action its reads and writes in the action's
// transaction. Internal commands still persist in order for validation and
// undo collection; their history is collected here and published at the end.
type secretaryWorkspace struct {
	store         *Desk
	tx            pgx.Tx
	scope         memory.Scope
	history       *intakestore.SecretaryHistory
	documents     map[string]workspace.Doc
	delegateTurns []string
}

// collecting returns a context in which item saves note their history here.
func (w *secretaryWorkspace) collecting(ctx context.Context) context.Context {
	return context.WithValue(ctx, kernel.SecretaryHistoryKey{}, w.history)
}

func (w *secretaryWorkspace) Command(ctx context.Context, c workspace.Command) error {
	return w.store.CommandTx(w.collecting(ctx), w.tx, w.scope, c)
}

func (w *secretaryWorkspace) Item(ctx context.Context, id string) (workspace.Item, error) {
	return itemstore.GetItem(ctx, w.tx, w.scope, id)
}

func (w *secretaryWorkspace) SaveItem(ctx context.Context, item workspace.Item) error {
	return itemstore.SaveItem(ctx, w.tx, w.scope, item)
}

func (w *secretaryWorkspace) NoteChange(ctx context.Context, item workspace.Item, summary string) error {
	return w.store.SaveAction(w.collecting(ctx), w.tx, w.scope, item, summary)
}

func (w *secretaryWorkspace) Recorded(id string) bool {
	_, recorded := w.history.Summaries[id]
	return recorded
}

func (w *secretaryWorkspace) Changed() []secretary.Change {
	out := []secretary.Change{}
	for _, id := range w.history.IDs {
		out = append(out, secretary.Change{ID: id, Summary: w.history.Summaries[id]})
	}
	return out
}

func (w *secretaryWorkspace) Publish(ctx context.Context, item workspace.Item, summary string) error {
	return w.store.SaveAction(ctx, w.tx, w.scope, item, summary)
}

func (w *secretaryWorkspace) ProjectNamed(ctx context.Context, title string) (bool, error) {
	var existing string
	err := w.tx.QueryRow(ctx, "SELECT id::text FROM work_items WHERE owner_id=$1 AND kind='project' AND lower(btrim(title))=lower($2) ORDER BY id LIMIT 1", string(w.scope.OwnerID), title).Scan(&existing)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (w *secretaryWorkspace) CurrentProject(ctx context.Context, thingID string) (string, error) {
	return intakestore.CurrentStudioProjectTx(ctx, w.tx, w.scope, thingID)
}

func (w *secretaryWorkspace) ResolveProject(ctx context.Context, value string, aliases map[string]workspace.Item) (string, error) {
	return w.store.secretaryProjectTx(w.collecting(ctx), w.tx, w.scope, value, aliases)
}

func (w *secretaryWorkspace) CloseDate(ctx context.Context, a SecretaryAction) (workspace.DeskReceipt, error) {
	return w.store.closeDateActionTx(w.collecting(ctx), w.tx, w.scope, a)
}

func (w *secretaryWorkspace) Document(alias string) (workspace.Doc, bool) {
	doc, ok := w.documents[alias]
	return doc, ok
}

func (w *secretaryWorkspace) DelegateTurns() []string { return w.delegateTurns }

func (s *Desk) closeDateActionTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, a SecretaryAction) (workspace.DeskReceipt, error) {
	receipt := workspace.DeskReceipt{Op: a.Op, Status: "done"}
	if actionlog.SmokeID(ctx) != "" {
		// A check-only turn is cleaned up by restoring item documents; a mark on
		// a memory is outside what that cleanup can put back, so it is not made.
		return secretary.Skipped(a.Op, "检查模式下不改记忆上的日期"), nil
	}
	as, ok := workstore.ValidDeadlineClose(a.As)
	if !ok {
		return secretary.Skipped(a.Op, "没说清是做完了还是不要了"), nil
	}
	memories, _ := ctx.Value(secretaryMemoriesKey{}).(map[string]workspace.Memory)
	m, ok := memories[a.Ref]
	if !ok {
		return secretary.Skipped(a.Op, "找不到说的是哪一条"), nil
	}
	var title string
	var version int
	err := tx.QueryRow(ctx, "SELECT title,claim_version FROM deadlines WHERE owner_id=$1 AND claim_id=$2 ORDER BY id LIMIT 1", string(scope.OwnerID), m.ID).Scan(&title, &version)
	if errors.Is(err, pgx.ErrNoRows) {
		return secretary.Skipped(a.Op, "这条记忆上没有期限或安排"), nil
	}
	if err != nil {
		return receipt, err
	}
	if err = intakestore.ActiveClaim(ctx, tx, scope, m.ID); err != nil {
		return secretary.Skipped(a.Op, "这条记忆已经不在了"), nil
	}
	if err = workstore.CompleteDeadlineTx(ctx, tx, scope, m.ID, version, as); errors.Is(err, memory.ErrConflict) {
		return secretary.Skipped(a.Op, "这条记忆刚改过，再说一遍"), nil
	} else if err != nil {
		return receipt, err
	}
	receipt.Text = map[string]string{"done": "记下做完了：", "dropped": "不再提：", "task": "转成了待办，不再单独提："}[as] + title
	return receipt, nil
}
