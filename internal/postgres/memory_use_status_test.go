package postgres

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// A fast turn read must preserve the production status contract, including
// empty eligible cards, subject-only membership and the evidence trust rules.
func TestB4StatusFastReadMatchesSharedContract(t *testing.T) {
	s := testStore(t)
	scope := owner()
	ctx := context.Background()
	var eid string
	if err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		id, err := entityTx(ctx, tx, scope.OwnerID, "person", "虚构人物晚杉")
		eid = string(id)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	ms := []workspace.Memory{b4Memory(t, s, scope, "虚构晚杉可能去南岸"), b4Memory(t, s, scope, "虚构晚杉住在南岸"), b4Memory(t, s, scope, "虚构晚杉每周二开会"), b4Memory(t, s, scope, "虚构晚杉刚完成季度汇报")}
	b4Card(t, s, scope, "entity:"+eid, "person", "晚杉", ms...)
	rule := b4Memory(t, s, scope, "虚构要求发信前看一眼")
	rule.AppliesTo = "起草邮件"
	b4Card(t, s, scope, "self:rule", "self", "对助手的要求", rule)
	// Directory identity comes from key; entity_id is nullable in the contract.
	b4Exec(t, s, "UPDATE status_cards SET entity_id=NULL WHERE owner_id=$1 AND key=$2", scope.OwnerID, "entity:"+eid)
	b4Exec(t, s, "DELETE FROM claim_mentions WHERE owner_id=$1 AND claim_id=$2", scope.OwnerID, ms[1].ID)
	b4Exec(t, s, "UPDATE claim_revisions SET subject_id=$3 WHERE owner_id=$1 AND claim_id=$2", scope.OwnerID, ms[1].ID, eid)
	b4Exec(t, s, "UPDATE claim_revisions SET acquisition='reported' WHERE owner_id=$1 AND claim_id=$2", scope.OwnerID, ms[2].ID)
	b4Exec(t, s, `UPDATE source_contexts SET role='assistant' WHERE owner_id=$1 AND source_id=ANY(SELECT source_id FROM evidence WHERE owner_id=$1 AND target_id=$2)`, scope.OwnerID, ms[3].ID)
	compare := func() {
		t.Helper()
		err := pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
			original, err := s.StatusCardIndexTx(ctx, tx, scope)
			if err != nil {
				return err
			}
			fastCtx := context.WithValue(ctx, useStatusReadKey{}, useStatusRead{})
			optimized, err := s.StatusCardIndexTx(fastCtx, tx, scope)
			if err != nil {
				return err
			}
			if !reflect.DeepEqual(original, optimized) {
				t.Fatalf("directory mismatch: original=%+v fast=%+v", original, optimized)
			}
			keys := []string{}
			for _, ref := range original {
				keys = append(keys, ref.Key)
			}
			a, err := s.StatusCardsTx(ctx, tx, scope, keys)
			if err != nil {
				return err
			}
			b, err := s.StatusCardsTx(fastCtx, tx, scope, keys)
			if err != nil {
				return err
			}
			for _, cards := range [][]workspace.StatusCard{a, b} {
				for ci := range cards {
					for fi := range cards[ci].Fields {
						for mi := range cards[ci].Fields[fi].Items {
							cards[ci].Fields[fi].Items[mi].Exposure = 0
						}
					}
				}
			}
			if !reflect.DeepEqual(a, b) {
				t.Fatalf("card mismatch: original=%+v fast=%+v", a, b)
			}
			// The cached turn index cannot resurrect a group reduced below three.
			cachedCtx := context.WithValue(ctx, useStatusReadKey{}, useStatusRead{Index: optimized})
			cached, err := s.StatusCardsTx(cachedCtx, tx, scope, keys)
			if err != nil {
				return err
			}
			for ci := range cached {
				for fi := range cached[ci].Fields {
					for mi := range cached[ci].Fields[fi].Items {
						cached[ci].Fields[fi].Items[mi].Exposure = 0
					}
				}
			}
			if !reflect.DeepEqual(a, cached) {
				t.Fatalf("cached card mismatch")
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	compare()
	// One retired member leaves three, so the card remains with that row omitted.
	b4Exec(t, s, "UPDATE claims SET retired='superseded',retired_by=$3 WHERE owner_id=$1 AND id=$2", scope.OwnerID, ms[0].ID, ms[1].ID)
	compare()
	// Even with no valid items the eligible card remains an empty card.
	b4Exec(t, s, "DELETE FROM status_card_items WHERE owner_id=$1 AND key=$2", scope.OwnerID, "entity:"+eid)
	compare()
	// Two members: neither directory nor card may return this group.
	b4Exec(t, s, "UPDATE claims SET retired='duplicate',retired_by=$3 WHERE owner_id=$1 AND id=$2", scope.OwnerID, ms[2].ID, ms[1].ID)
	compare()
	// Entity retirement must also hide membership.
	b4Exec(t, s, "UPDATE memory_records SET state='withdrawn' WHERE owner_id=$1 AND id=$2", scope.OwnerID, eid)
	compare()
}

func TestB4BatchedEvidencePreservesWindows(t *testing.T) {
	s := testStore(t)
	scope := owner()
	ctx := context.Background()
	// Six distinct anchors exercise the unchanged four-window budget and gaps.
	claims := []evidenceContextClaim{}
	for i := 0; i < 6; i++ {
		m := b4Memory(t, s, scope, fmt.Sprintf("虚构那位同事的季度汇报第%d项", i))
		if len(m.Sources) == 0 {
			t.Fatal("fictional memory has no evidence")
		}
		source := m.Sources[0]
		b4Exec(t, s, `INSERT INTO source_contexts(owner_id,source_id,source_version,conversation_key,role,branch) VALUES($1,$2,$3,$4,'user','active')
  ON CONFLICT(owner_id,source_id,source_version) DO UPDATE SET conversation_key=excluded.conversation_key,role='user',branch='active'`, scope.OwnerID, source.SourceID, source.Version, fmt.Sprintf("fictional-b4-%d", i))
		claims = append(claims, evidenceContextClaim{Label: fmt.Sprintf("M%d", i), Ref: memory.Ref{ID: memory.ID(m.ID), Version: m.Version, Kind: memory.ClaimKind}, Text: m.Text})
	}
	// Same-anchor deduplication, then an unavailable anchor with an explicit gap.
	claims = append(claims, claims[0], evidenceContextClaim{Label: "M_missing", Ref: memory.Ref{ID: memory.NewID(), Version: 1, Kind: memory.ClaimKind}, Text: "虚构那位同事"})
	err := pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		a, ag, err := evidenceContextsTx(ctx, tx, scope, "", nil, claims)
		if err != nil {
			return err
		}
		b, bg, err := evidenceContextsTx(context.WithValue(ctx, useEvidenceBatchKey{}, true), tx, scope, "", nil, claims)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(a, b) || !reflect.DeepEqual(ag, bg) {
			t.Fatalf("batch evidence changed windows/gaps: original=%+v %v batch=%+v %v", a, ag, b, bg)
		}
		if len(a) != 4 || len(ag) != 3 {
			t.Fatalf("unexpected budget/gap fixture: %d %v", len(a), ag)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
