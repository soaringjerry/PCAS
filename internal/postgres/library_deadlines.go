package postgres

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
)

// Program validation does not attempt to understand the original date language.
// Unusable model dates are retained as unclear with their original wording.
func writeOrganizedDeadlinesTx(ctx context.Context, tx pgx.Tx, owner memory.ID, m cardMemory, output []cardDeadline, timezone string) error {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return err
	}
	normalized := []cardDeadline{}
	for _, d := range output {
		if !oneOf(d.Kind, "deadline", "appointment", "recurring", "unclear") || strings.TrimSpace(d.Title) == "" {
			return memory.ErrInvalid
		}
		if d.Kind == "recurring" {
			d.At = nil
			if strings.TrimSpace(d.Recurrence) == "" {
				d.Kind = "unclear"
				d.TimeNote = "未说明周期；" + d.TimeNote
			}
		} else if d.Kind == "unclear" {
			d.At = nil
		} else {
			valid := false
			if d.At != nil {
				at, e := time.Parse(time.RFC3339Nano, *d.At)
				if e != nil {
					at, e = time.ParseInLocation("2006-01-02", *d.At, loc)
				}
				said, se := time.Parse(time.RFC3339Nano, m.ExpressedAt)
				if e == nil && (se != nil || !at.Before(said)) {
					v := at.UTC().Format(time.RFC3339Nano)
					d.At = &v
					valid = true
				}
			}
			if !valid {
				d.Kind = "unclear"
				d.At = nil
				d.TimeNote = "日期无法校验；" + d.TimeNote
			}
		}
		normalized = append(normalized, d)
	}
	if _, err := tx.Exec(ctx, "DELETE FROM deadlines WHERE owner_id=$1 AND claim_id=$2", string(owner), string(m.Ref.ID)); err != nil {
		return err
	}
	for _, d := range normalized {
		if _, err := tx.Exec(ctx, `INSERT INTO deadlines(owner_id,id,claim_id,claim_version,kind,at,recurrence,title,time_note,original_text)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT(owner_id,id) DO NOTHING`, string(owner), organizedDeadlineID(owner, m, d), string(m.Ref.ID), m.Ref.Version, d.Kind, d.At, d.Recurrence, d.Title, d.TimeNote, m.Text); err != nil {
			return err
		}
	}
	return nil
}

func organizedDeadlineID(owner memory.ID, m cardMemory, d cardDeadline) string {
	sum := sha256.Sum256([]byte(statusDeadlineID(owner, m, d) + "/" + d.Title + "/" + d.TimeNote))
	return fmt.Sprintf("%x-%x-%x-%x-%x", sum[0:4], sum[4:6], sum[6:8], sum[8:10], sum[10:16])
}
