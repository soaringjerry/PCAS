package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/worker"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

var phase25B234AwaitingProtocol = errors.New("awaiting coordinator model protocol and background entrypoints")

// Protocol-neutral scenarios refer to stable claim IDs; this adapter maps
// them to the numbering and wire representation frozen by contract section 7.
type phase25B2Duplicate struct {
	kept   memory.ID
	merged []memory.ID
}
type phase25B2Supersession struct{ old, next memory.ID }
type phase25B2Comparison struct {
	duplicates    []phase25B2Duplicate
	supersessions []phase25B2Supersession
	mergePeople   [][2]memory.ID
}
type phase25B2Adapter struct {
	encode   func(phase25B2Comparison, map[memory.ID]int) (string, error)
	schedule func(context.Context, time.Time) (int, error)
	process  func(context.Context, worker.Job) error
	// The undo closure must invoke the actual action-log Undo boundary.
	restore func(context.Context, memory.Scope, memory.Ref) (undo func() error, err error)
}

func phase25B2ModelOutput(a phase25B2Adapter, result phase25B2Comparison, numbers map[memory.ID]int) (string, error) {
	if a.encode == nil {
		return "", phase25B234AwaitingProtocol
	}
	return a.encode(result, numbers)
}

func phase25B2DefaultAdapter(f *phase25B234Fixture) phase25B2Adapter {
	return phase25B2Adapter{
		schedule: f.store.ScheduleCompare,
		process: func(ctx context.Context, j worker.Job) error {
			if strings.HasPrefix(j.Stage, "memory.compare:") {
				return f.store.ProcessCompare(ctx, j)
			}
			if strings.HasPrefix(j.Stage, "memory.entity_compare:") {
				return f.store.ProcessEntityCompare(ctx, j)
			}
			return fmt.Errorf("unexpected comparison stage %s", j.Stage)
		},
		encode: func(result phase25B2Comparison, numbers map[memory.ID]int) (string, error) {
			out := phase25B2Empty()
			number := func(id memory.ID) (int, error) {
				n, ok := numbers[id]
				if !ok {
					return 0, fmt.Errorf("claim absent from captured comparison: %s", id)
				}
				return n, nil
			}
			for _, d := range result.duplicates {
				keep, e := number(d.kept)
				if e != nil {
					return "", e
				}
				members := []int{keep}
				for _, id := range d.merged {
					n, e := number(id)
					if e != nil {
						return "", e
					}
					members = append(members, n)
				}
				out.Duplicates = append(out.Duplicates, phase25B2WireDuplicate{Keep: keep, Members: members})
			}
			for _, pair := range result.supersessions {
				old, e := number(pair.old)
				if e != nil {
					return "", e
				}
				next, e := number(pair.next)
				if e != nil {
					return "", e
				}
				out.Superseded = append(out.Superseded, phase25B2WireSuperseded{Old: old, New: next})
			}
			return phase25B2JSON(out).content, nil
		},
		restore: func(ctx context.Context, scope memory.Scope, ref memory.Ref) (func() error, error) {
			state, e := f.store.Snapshot(ctx, scope)
			if e != nil {
				return nil, e
			}
			var before int64
			if e = f.db.QueryRow(ctx, `SELECT coalesce(max(action_order),0) FROM action_log WHERE owner_id=$1`, scope.OwnerID).Scan(&before); e != nil {
				return nil, e
			}
			_, e = f.store.Execute(ctx, scope, workspace.Command{Type: "restoreMemory", ID: string(ref.ID), MemoryID: string(ref.ID), RequestID: string(memory.NewID()), ExpectedRevision: state.Revision})
			if e != nil {
				return nil, e
			}
			var action string
			if e = f.db.QueryRow(ctx, `SELECT id FROM action_log WHERE owner_id=$1 AND action_order>$2 ORDER BY action_order DESC LIMIT 1`, scope.OwnerID, before).Scan(&action); e != nil {
				return nil, e
			}
			return func() error { _, e := f.store.Undo(ctx, scope, action); return e }, nil
		},
	}
}

// CompareVersion is a public constant. A version-upgrade test therefore seeds
// previous-version compared markers, the persisted state encountered by a
// newer binary, rather than changing product source to make a test hook.
