package statestore

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/interpretation/organize"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/postgres/kernel"
)

// Program validation does not attempt to understand the original date language.
// Unusable model dates are retained as unclear with their original wording.
func WriteOrganizedDeadlinesTx(ctx context.Context, tx pgx.Tx, owner memory.ID, m kernel.CardMemory, output []organize.Deadline, timezone string) error {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return err
	}
	normalized, err := organize.NormalizeDeadlines(output, m.ExpressedAt, loc)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "DELETE FROM deadlines WHERE owner_id=$1 AND claim_id=$2", string(owner), string(m.Ref.ID)); err != nil {
		return err
	}
	for _, d := range normalized {
		if _, err := tx.Exec(ctx, `INSERT INTO deadlines(owner_id,id,claim_id,claim_version,kind,at,recurrence,title,time_note,original_text,schedule_rule,date_only)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) ON CONFLICT(owner_id,id) DO NOTHING`, string(owner), organizedDeadlineID(owner, m, d), string(m.Ref.ID), m.Ref.Version, d.Kind, d.At, d.Recurrence, d.Title, d.TimeNote, m.Text, optionalScheduleRule(d.ScheduleRule), d.DateOnly != nil && *d.DateOnly); err != nil {
			return err
		}
	}
	return nil
}
