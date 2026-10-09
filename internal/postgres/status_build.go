package postgres

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/worker"
)

const (
	StatusInterval   = time.Minute
	CardStage        = "memory.card"
	HandoverStage    = "memory.handover"
	CardPriority     = ComparePriority
	SelfCardPriority = ComparePriority - 1
	HandoverPriority = 14
)

type cardMemory struct {
	AppliesTo   string     `json:"appliesTo,omitempty"`
	N           int        `json:"n"`
	Text        string     `json:"text"`
	ExpressedAt string     `json:"expressedAt"`
	Category    string     `json:"category"`
	Durable     *bool      `json:"durable,omitempty"`
	Trust       string     `json:"trust"`
	Ref         memory.Ref `json:"-"`
}
type cardRule struct {
	N         int    `json:"n"`
	AppliesTo string `json:"appliesTo"`
}
type cardDeadline struct {
	ScheduleRule *scheduleRule `json:"scheduleRule,omitempty"`
	DateOnly     *bool         `json:"dateOnly,omitempty"`
	N            int           `json:"n"`
	Kind         string        `json:"kind"`
	At           *string       `json:"at"`
	Recurrence   string        `json:"recurrence"`
	Title        string        `json:"title"`
	TimeNote     string        `json:"timeNote"`
}
type cardOutput struct {
	Fields    map[string][]int `json:"fields"`
	Rules     []cardRule       `json:"rules"`
	Deadlines []cardDeadline   `json:"deadlines"`
}
type cardGroup struct {
	Key, Kind, Name string
	Entity          *string
	Count           int
	Built           *time.Time
	Rule            int
	Stale           bool
}

func (s *Store) RunStatus(ctx context.Context, logger *slog.Logger) {
	timer := time.NewTicker(StatusInterval)
	defer timer.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		if _, err := s.ScheduleStatus(ctx, time.Now()); err != nil && ctx.Err() == nil {
			s.recordScheduleFailure(ctx, HandoverStage, err)
		}
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
	}
}

func (s *Store) ScheduleStatus(ctx context.Context, now time.Time) (int, error) {
	if now.IsZero() {
		return 0, memory.ErrInvalid
	}
	p, ok := s.models.Get(s.models.ExtractionID())
	if !ok || p.Embedding || p.Transcription {
		return 0, nil
	}
	count := 0
	rows, err := s.pool.Query(ctx, "SELECT owner_id::text FROM workspace_owners ORDER BY owner_id")
	if err != nil {
		return 0, err
	}
	owners := []memory.ID{}
	for rows.Next() {
		var owner memory.ID
		if err := rows.Scan(&owner); err != nil {
			rows.Close()
			return count, err
		}
		owners = append(owners, owner)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return count, err
	}
	for _, owner := range owners {
		err := backgroundResultTx(ctx, s.pool, owner, func(ctx context.Context, tx pgx.Tx) error {
			anchor, err := seedOrganizeGroupsTx(ctx, tx, owner)
			if err != nil {
				return err
			}
			topicCount, err := enqueueTopicProjectsTx(ctx, tx, owner, now)
			if err != nil {
				return err
			}
			count += topicCount
			tidyCount, err := enqueueDateTidyTx(ctx, tx, owner, now)
			if err != nil {
				return err
			}
			count += tidyCount
			n, err := enqueueStatusHandoversTx(ctx, tx, map[memory.ID]memory.Ref{owner: anchor}, now)
			if err != nil {
				return err
			}
			count += n
			n, err = enqueueProjectHandoversTx(ctx, tx, owner, anchor, now)
			count += n
			if err != nil {
				return err
			}
			n, err = enqueueEffortTx(ctx, tx, owner, anchor, now)
			count += n
			return err
		})
		if err != nil {
			if s.scheduleYielded(ctx, owner, HandoverStage, err) {
				continue
			}
			return count, err
		}
	}
	return count, nil
}

func statusScheduleBusy(err error) bool {
	var busy *worker.JobError
	return errors.As(err, &busy) && busy.Code == "background_write_busy"
}

func statusCardPriority(key string) int {
	if strings.HasPrefix(key, "self:") {
		return SelfCardPriority
	}
	return CardPriority
}

func statusCallLock(ctx context.Context, s *Store) (func(), error) {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	var locked bool
	err = conn.QueryRow(ctx, "SELECT pg_try_advisory_lock(hashtextextended(current_database() || ':' || current_schema() || ':organize-call',0))").Scan(&locked)
	if err != nil || !locked {
		conn.Release()
		if err != nil {
			return nil, err
		}
		return nil, &worker.JobError{Code: "status_busy", Until: time.Now().Add(StatusInterval), NoAttempt: true}
	}
	return func() {
		clean, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := conn.Exec(clean, "SELECT pg_advisory_unlock(hashtextextended(current_database() || ':' || current_schema() || ':organize-call',0))"); err != nil {
			_ = conn.Conn().Close(clean)
		}
		conn.Release()
	}, nil
}

// Kept as a compatibility handler for pre-upgrade queue slots. Cards are frozen.
func (s *Store) ProcessCard(ctx context.Context, j worker.Job) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := lockJob(ctx, tx, j); err != nil {
			return err
		}
		return acknowledge(ctx, tx, j)
	})
}
