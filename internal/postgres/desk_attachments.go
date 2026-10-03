package postgres

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

type deskAttachment struct {
	Ref                     memory.Ref
	Media, Context, Warning string
}

// Both Telegram and the web submit stored source references to DeskTurn.
func (s *Store) prepareDeskAttachments(ctx context.Context, scope memory.Scope, req workspace.DeskTurnRequest) ([]deskAttachment, error) {
	out := []deskAttachment{}
	for _, ref := range req.Attachments {
		original, err := s.GetSource(ctx, scope, ref.ID, ref.Version)
		if err != nil {
			return nil, err
		}
		if !original.Source.HasAttachment || original.Source.State != "active" || original.Source.MediaType == "application/x-pcas-archive" {
			return nil, memory.ErrInvalid
		}
		err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, "INSERT INTO desk_attachments(owner_id,source_id,source_version,request_id) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING", string(scope.OwnerID), string(ref.ID), ref.Version, req.RequestID)
			if err != nil {
				return err
			}
			var request string
			if err := tx.QueryRow(ctx, "SELECT request_id::text FROM desk_attachments WHERE owner_id=$1 AND source_id=$2 AND source_version=$3", string(scope.OwnerID), string(ref.ID), ref.Version).Scan(&request); err != nil {
				return err
			}
			if !strings.EqualFold(request, req.RequestID) {
				return memory.ErrConflict
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		a := deskAttachment{Ref: ref, Media: original.Source.MediaType}
		parsed, parseErr := s.readAttachment(ctx, scope, ref, nil)
		image := strings.HasPrefix(a.Media, "image/")
		if parseErr != nil || image && parsed.Representation != "vision" {
			slog.WarnContext(ctx, "secretary attachment retained", "stage", "parse", "error_type", secretaryErrorType("model", parseErr))
			if image {
				a.Warning = "图片存好了，但我现在看不了图；请检查模型通道，或把要办的内容写成文字。"
			} else {
				a.Warning = "文件存好了，但暂时没读出内容；请检查资料处理状态，或把要办的内容写成文字。"
			}
		} else {
			a.Context = fmt.Sprintf("附件 %s（%s）：\n%s", original.Source.Title, parsed.Representation, boundedAttachmentText(parsed.Text, 12000))
		}
		out = append(out, a)
	}
	return out, nil
}

func (s *Store) deskAttachmentReceiptTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, turn string, a deskAttachment) (workspace.DeskReceipt, error) {
	text := "存了一份文件"
	if strings.HasPrefix(a.Media, "image/") {
		text = "存了一张图片"
	} else if strings.HasPrefix(a.Media, "audio/") {
		text = "存了一段音频"
	}
	actionID, sourceID := string(memory.NewID()), string(a.Ref.ID)
	// A source receipt uses the regular action ID, expiry and undo endpoint; its
	// narrowly typed change deletes the source through normal propagation.
	changes := []actionChange{{Table: "source_attachments", ID: sourceID, Before: asJSON(a.Ref)}}
	_, err := tx.Exec(ctx, "INSERT INTO action_log(owner_id,id,source,turn_id,summary,changes) VALUES($1,$2,'desk',$3,$4,$5)", string(scope.OwnerID), actionID, turn, text, asJSON(changes))
	return workspace.DeskReceipt{ActionID: &actionID, SourceID: &sourceID, SourceVersion: a.Ref.Version, Op: "attachment", Text: text, Undoable: true, Status: "done"}, err
}

func boundedAttachmentText(text string, limit int) string {
	r := []rune(text)
	if len(r) > limit {
		return string(r[:limit])
	}
	return text
}
