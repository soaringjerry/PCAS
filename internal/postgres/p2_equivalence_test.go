package postgres

import (
	"bytes"
	"context"
	"crypto/md5"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func p2ID(kind string, n int) string {
	h := fmt.Sprintf("%x", md5.Sum([]byte(fmt.Sprintf("p2-%s-%d", kind, n))))
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}
func p2Bytes(t *testing.T, label string, want, got any) {
	t.Helper()
	a, b := asJSON(want), asJSON(got)
	if !bytes.Equal(a, b) {
		t.Fatalf("%s differs byte for byte: old=%s new=%s", label, a, b)
	}
}

// Snapshot takes an owner row lock in production. Both oracles read the same
// repeatable-read snapshot here; PostgreSQL forbids FOR SHARE in a read-only
// transaction, so remove only that identical lock clause on both paths.
type p2ReadOnlyTx struct{ pgx.Tx }

func (tx p2ReadOnlyTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if sql == "SELECT revision,settings FROM workspace_owners WHERE owner_id=$1 FOR SHARE" {
		sql = strings.TrimSuffix(sql, " FOR SHARE")
	}
	return tx.Tx.QueryRow(ctx, sql, args...)
}

func p2AssertReads(t *testing.T, s *Store, scope memory.Scope) {
	t.Helper()
	ctx := context.Background()
	err := pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		tx = p2ReadOnlyTx{tx}
		for _, principal := range []string{"model", "ungranted"} {
			for _, thing := range []*string{nil, stringPointer(p2ID("task", 4))} {
				read := func(predicate string) ([]byte, error) {
					var out []byte
					err := tx.QueryRow(ctx, "SELECT coalesce(jsonb_agg(jsonb_build_object('id',r.id,'allowed',"+predicate+") ORDER BY r.id),'[]') FROM memory_records r WHERE r.owner_id=$1 AND r.kind='source'", scope.OwnerID, principal, thing).Scan(&out)
					return out, err
				}
				a, err := read(p2LegacyTeamSourceVisibleSQL("$1", "r.id", "$2", "$3"))
				if err != nil {
					return err
				}
				b, err := read(teamSourceVisibleSQL("$1", "r.id", "$2", "$3"))
				if err != nil {
					return err
				}
				p2Bytes(t, "source visibility/temporal/exclusion", string(a), string(b))
			}
		}
		for _, readerScope := range []memory.Scope{scope, {OwnerID: scope.OwnerID, PrincipalID: "model"}, {OwnerID: scope.OwnerID, PrincipalID: "ungranted"}} {
			for _, current := range []bool{false, true} {
				for _, opts := range []memoryReadOptions{{}, {limit: 17}, {id: p2ID("claim", 5)}, {query: workspace.MemoryQuery{Q: "季度", Nature: "fact", Category: "unknown"}}, {query: workspace.MemoryQuery{Agent: "model", Epistemic: "sourced"}}} {
					old, err := s.p2LegacyReadMemoriesTx(ctx, tx, readerScope, current, opts)
					if err != nil {
						return err
					}
					got, err := s.readMemoriesTx(ctx, tx, readerScope, current, opts)
					if err != nil {
						return err
					}
					p2Bytes(t, fmt.Sprintf("reader owner=%t principal=%s current=%t", readerScope.IsOwner, readerScope.PrincipalID, current), old, got)
				}
			}
		}
		items, err := queryDocuments[workspace.Item](ctx, tx, "SELECT document FROM work_items WHERE owner_id=$1 ORDER BY id", scope.OwnerID)
		if err != nil {
			return err
		}
		for _, item := range items {
			for _, agent := range []string{"model", "ungranted"} {
				a, ar, err := p2LegacySanitizeItemTx(ctx, tx, scope, agent, item)
				if err != nil {
					return err
				}
				b, br, err := sanitizeItemTx(ctx, tx, scope, agent, item)
				if err != nil {
					return err
				}
				p2Bytes(t, "artifact body", a, b)
				p2Bytes(t, "artifact refs", ar, br)
			}
		}
		for _, rs := range []memory.Scope{scope, {OwnerID: scope.OwnerID, PrincipalID: "model"}, {OwnerID: scope.OwnerID, PrincipalID: "model", Team: true}} {
			for _, mode := range []memory.RecallMode{memory.Remember, memory.History, memory.Continue} {
				for _, q := range []string{"季度", "云杉", "查无这份虚构资料", ""} {
					in := memory.RecallRequest{Query: q, Mode: mode, Context: memory.WorkingContext{Objects: []memory.ID{memory.ID(p2ID("claim", 5))}}}
					budget := memory.Budget{Candidates: 15, Tokens: 4000, Edges: 15, Hops: 1}
					tokens := memory.SearchTokens(q)
					terms := []string{}
					for _, token := range tokens {
						if len([]rune(token)) > 1 || len(tokens) == 1 {
							terms = append(terms, "'"+strings.ReplaceAll(token, "'", "''")+"'")
						}
					}
					fts := strings.Join(terms, " | ")
					a := memory.RecallResult{Memories: []memory.Ref{}, Evidence: []memory.Evidence{}, Coverage: coverage()}
					b := a
					if err := s.p2LegacyRecallTx(ctx, tx, rs, in, budget, q, fts, nil, "", 0, "same-snapshot", tokens, &a); err != nil {
						return err
					}
					if err := s.recallTx(ctx, tx, rs, in, budget, q, fts, nil, "", 0, "same-snapshot", tokens, &b); err != nil {
						return err
					}
					p2Bytes(t, "recall rank/text/context/cursor", a, b)
				}
			}
		}
		old, err := s.p2LegacySnapshotTx(ctx, tx, scope)
		if err != nil {
			return err
		}
		got, err := s.snapshotTx(ctx, tx, scope)
		if err != nil {
			return err
		}
		p2Bytes(t, "full workspace snapshot", old, got)
		req := turnRequest("霜叶的季度汇报准备得怎么样？")
		oldContext, err := s.p2LegacySecretaryContextTx(ctx, tx, scope, req, string(memory.NewID()))
		if err != nil {
			return err
		}
		newContext, err := s.secretaryContextTx(ctx, tx, scope, req, oldContext.ConversationID)
		if err != nil {
			return err
		}
		op, om, err := s.p2LegacySecretaryPrompt(ctx, tx, scope, req, &oldContext)
		if err != nil {
			return err
		}
		np, nm, err := s.secretaryPrompt(ctx, tx, scope, req, &newContext)
		if err != nil {
			return err
		}
		p2Bytes(t, "secretary prompt", op, np)
		p2Bytes(t, "secretary sent memories", om, nm)
		p2Bytes(t, "secretary dependencies", oldContext.Dependencies, newContext.Dependencies)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestP2QueriesAreByteIdentical(t *testing.T) {
	s, scope := p2Fixture(t, 120)
	p2AssertReads(t, s, scope)
	ctx := context.Background()
	// Mix permissions, future validity, historical known-time and source withdrawal.
	statements := []string{
		`DELETE FROM record_grants WHERE owner_id=$1 AND record_id=$2 AND principal_id='model'`,
		`UPDATE memory_records SET state='withdrawn' WHERE owner_id=$1 AND id=$3`,
		`UPDATE record_versions SET valid_from=now()+interval '2 days' WHERE owner_id=$1 AND record_id=$4`,
		`UPDATE record_versions SET valid_to=now()-interval '1 hour' WHERE owner_id=$1 AND record_id=$5`,
		`INSERT INTO context_exclusions VALUES($1,$6,$7)`,
	}
	for i, sql := range statements {
		args := []any{scope.OwnerID, p2ID("claim", 2), p2ID("source", 3), p2ID("claim", 4), p2ID("claim", 5), p2ID("task", 4), p2ID("claim", 4)}
		// Each mutation has one bound target and must not leave unused parameters.
		switch i {
		case 0:
			args = args[:2]
		case 1:
			sql = strings.ReplaceAll(sql, "$3", "$2")
			args = []any{scope.OwnerID, p2ID("source", 3)}
		case 2:
			sql = strings.ReplaceAll(sql, "$4", "$2")
			args = []any{scope.OwnerID, p2ID("claim", 4)}
		case 3:
			sql = strings.ReplaceAll(sql, "$5", "$2")
			args = []any{scope.OwnerID, p2ID("claim", 5)}
		case 4:
			sql = strings.ReplaceAll(strings.ReplaceAll(sql, "$6", "$2"), "$7", "$3")
			args = []any{scope.OwnerID, p2ID("task", 4), p2ID("claim", 4)}
		}
		if _, err := s.pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	p2AssertReads(t, s, scope)
	// User correction survives source withdrawal and is not the same as the old
	// AI revision. The old temporal resolver remains the oracle for both views.
	// This editor path consumes the key's hexadecimal spelling as well as its
	// identity; use a synthetic 32-byte key for the corrected claim.
	if _, err := s.pool.Exec(ctx, "UPDATE claim_source_keys SET claim_key=md5(claim_key)||md5('synthetic-'||claim_key) WHERE owner_id=$1 AND claim_id=$2", scope.OwnerID, p2ID("claim", 3)); err != nil {
		t.Fatal(err)
	}
	workspaceCommand(t, s, scope, workspace.Command{Type: "editMemory", ID: p2ID("claim", 3), Text: "虚构人物云杉纠正后的季度汇报说明。"})
	p2AssertReads(t, s, scope)
}

func TestP2IndexDoesNotChangeBytes(t *testing.T) {
	s, scope := p2Fixture(t, 120)
	ctx := context.Background()
	if _, err := s.pool.Exec(ctx, "DROP INDEX claim_source_keys_claim_idx, memory_records_active_updated_idx"); err != nil {
		t.Fatal(err)
	}
	err := pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		tx = p2ReadOnlyTx{tx}
		before, err := s.p2LegacySnapshotTx(ctx, tx, scope)
		if err != nil {
			return err
		}
		effective, err := s.p2LegacyReadMemoriesTx(ctx, tx, memory.Scope{OwnerID: scope.OwnerID, PrincipalID: "model"}, true, memoryReadOptions{})
		if err != nil {
			return err
		}
		body, err := migrations.ReadFile("migrations/036_turn_source_lookup.sql")
		if err != nil {
			return err
		}
		if _, err := s.pool.Exec(ctx, string(body)); err != nil {
			return err
		}
		after, err := s.p2LegacySnapshotTx(ctx, tx, scope)
		if err != nil {
			return err
		}
		current, err := s.p2LegacyReadMemoriesTx(ctx, tx, memory.Scope{OwnerID: scope.OwnerID, PrincipalID: "model"}, true, memoryReadOptions{})
		if err != nil {
			return err
		}
		p2Bytes(t, "index full snapshot", before, after)
		p2Bytes(t, "index current memories", effective, current)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// No current-version cache is stored. Still vary temporal/source/permission
// state to guard the new bounded lookups against the unchanged temporal oracle.
func TestP2RandomVisibilityAndSourceSequence(t *testing.T) {
	s, scope := p2Fixture(t, 120)
	ctx := context.Background()
	rng := rand.New(rand.NewSource(20261005))
	for step := 0; step < 80; step++ {
		n := 1 + rng.Intn(119)
		claim, source := p2ID("claim", n), p2ID("source", n)
		switch rng.Intn(8) {
		case 0:
			_, err := s.pool.Exec(ctx, "DELETE FROM record_grants WHERE owner_id=$1 AND record_id=$2 AND principal_id='model'", scope.OwnerID, claim)
			if err != nil {
				t.Fatal(err)
			}
		case 1:
			_, err := s.pool.Exec(ctx, "INSERT INTO record_grants VALUES($1,$2,'model') ON CONFLICT DO NOTHING", scope.OwnerID, claim)
			if err != nil {
				t.Fatal(err)
			}
		case 2:
			state := []string{"active", "withdrawn"}[rng.Intn(2)]
			_, err := s.pool.Exec(ctx, "UPDATE memory_records SET state=$3 WHERE owner_id=$1 AND id=$2", scope.OwnerID, source, state)
			if err != nil {
				t.Fatal(err)
			}
		case 3:
			actor := []string{"ai", "user"}[rng.Intn(2)]
			_, err := s.pool.Exec(ctx, "UPDATE record_versions SET actor=$3 WHERE owner_id=$1 AND record_id=$2", scope.OwnerID, claim, actor)
			if err != nil {
				t.Fatal(err)
			}
		case 4:
			hours := []int{-48, 48}[rng.Intn(2)]
			_, err := s.pool.Exec(ctx, "UPDATE record_versions SET valid_from=now()+$3::int*interval '1 hour' WHERE owner_id=$1 AND record_id=$2", scope.OwnerID, claim, hours)
			if err != nil {
				t.Fatal(err)
			}
		case 5:
			err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
				var version int
				if err := tx.QueryRow(ctx, "SELECT version FROM memory_records WHERE owner_id=$1 AND id=$2 FOR UPDATE", scope.OwnerID, source).Scan(&version); err != nil {
					return err
				}
				version++
				if _, err := tx.Exec(ctx, "INSERT INTO record_versions(owner_id,record_id,version,actor,expressed_at) VALUES($1,$2,$3,'import',now())", scope.OwnerID, source, version); err != nil {
					return err
				}
				if _, err := tx.Exec(ctx, `INSERT INTO source_versions(owner_id,source_id,version,external_version,content_hash,title,body,media_type)
 SELECT owner_id,source_id,$3::int,$3::int::text,content_hash,title,'虚构替换后的原话','text/plain' FROM source_versions WHERE owner_id=$1 AND source_id=$2 AND version=1`, scope.OwnerID, source, version); err != nil {
					return err
				}
				_, err := tx.Exec(ctx, "UPDATE memory_records SET version=$3 WHERE owner_id=$1 AND id=$2", scope.OwnerID, source, version)
				return err
			})
			if err != nil {
				t.Fatal(err)
			}
		case 6:
			_, err := s.pool.Exec(ctx, "INSERT INTO context_exclusions VALUES($1,$2,$3) ON CONFLICT DO NOTHING", scope.OwnerID, p2ID("task", 4), claim)
			if err != nil {
				t.Fatal(err)
			}
		case 7:
			_, err := s.pool.Exec(ctx, "UPDATE claim_revisions SET acquisition='inferred',confirmation='candidate' WHERE owner_id=$1 AND claim_id=$2", scope.OwnerID, claim)
			if err != nil {
				t.Fatal(err)
			}
		}
		err := pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
			for _, current := range []bool{false, true} {
				for _, sc := range []memory.Scope{scope, {OwnerID: scope.OwnerID, PrincipalID: "model"}} {
					old, err := s.p2LegacyReadMemoriesTx(ctx, tx, sc, current, memoryReadOptions{})
					if err != nil {
						return err
					}
					wanted := []workspace.Memory{}
					for _, m := range old {
						if m.ID == claim {
							wanted = append(wanted, m)
						}
					}
					got, err := s.readMemoriesTx(ctx, tx, sc, current, memoryReadOptions{ids: []string{claim}})
					if err != nil {
						return err
					}
					p2Bytes(t, fmt.Sprintf("random selected memory step %d", step), wanted, got)
				}
			}
			item, err := getItem(ctx, tx, scope, p2ID("task", 1+step%5))
			if err != nil {
				return err
			}
			a, ar, err := p2LegacySanitizeItemTx(ctx, tx, scope, "model", item)
			if err != nil {
				return err
			}
			b, br, err := sanitizeItemTx(ctx, tx, scope, "model", item)
			if err != nil {
				return err
			}
			p2Bytes(t, "random artifact", a, b)
			p2Bytes(t, "random artifact dependencies", ar, br)
			var wantedSourceVisibility bool
			for index, predicate := range []string{p2LegacyTeamSourceVisibleSQL("$1", "$2", "$3", "$4"), teamSourceVisibleSQL("$1", "$2", "$3", "$4")} {
				var allowed bool
				if err := tx.QueryRow(ctx, "SELECT "+predicate, scope.OwnerID, source, "model", p2ID("task", 4)).Scan(&allowed); err != nil {
					return err
				}
				if index == 0 {
					wantedSourceVisibility = allowed
				} else {
					p2Bytes(t, "random source visibility", wantedSourceVisibility, allowed)
				}
			}
			request := memory.RecallRequest{Query: "云杉", Mode: memory.Remember, Context: memory.WorkingContext{Objects: []memory.ID{}}}
			budget := memory.Budget{Candidates: 10, Tokens: 4000, Edges: 10, Hops: 1}
			oldRecall := memory.RecallResult{Coverage: coverage()}
			newRecall := oldRecall
			sc := memory.Scope{OwnerID: scope.OwnerID, PrincipalID: "model", Team: true}
			if err := s.p2LegacyRecallTx(ctx, tx, sc, request, budget, "云杉", "'云杉'", nil, "", 0, "random-sequence", []string{"云杉"}, &oldRecall); err != nil {
				return err
			}
			if err := s.recallTx(ctx, tx, sc, request, budget, "云杉", "'云杉'", nil, "", 0, "random-sequence", []string{"云杉"}, &newRecall); err != nil {
				return err
			}
			p2Bytes(t, "random recall", oldRecall, newRecall)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

// The large oracle is opt-in because its old recall intentionally retains the
// slow original plan. SQL now() is fixed by this one read-only transaction.
func TestP2LargeResultsAreByteIdentical(t *testing.T) {
	if os.Getenv("PCAS_P2_LARGE_EQ") == "" {
		t.Skip("set PCAS_P2_LARGE_EQ for the 10,000-claim legacy comparison")
	}
	s, scope := p2Fixture(t, 10000)
	ctx := context.Background()
	err := pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		tx = p2ReadOnlyTx{tx}
		a, err := s.p2LegacySnapshotTx(ctx, tx, scope)
		if err != nil {
			return err
		}
		b, err := s.snapshotTx(ctx, tx, scope)
		if err != nil {
			return err
		}
		p2Bytes(t, "10,000-claim full snapshot", a, b)
		old, err := s.p2LegacyReadMemoriesTx(ctx, tx, memory.Scope{OwnerID: scope.OwnerID, PrincipalID: "model"}, true, memoryReadOptions{})
		if err != nil {
			return err
		}
		ids := []string{p2ID("claim", 5), p2ID("claim", 97), p2ID("claim", 9991)}
		selected, err := s.readMemoriesTx(ctx, tx, memory.Scope{OwnerID: scope.OwnerID, PrincipalID: "model"}, true, memoryReadOptions{ids: ids})
		if err != nil {
			return err
		}
		wanted := []workspace.Memory{}
		for _, m := range old {
			if oneOf(m.ID, ids...) {
				wanted = append(wanted, m)
			}
		}
		p2Bytes(t, "10,000-claim selected rows", wanted, selected)
		in := memory.RecallRequest{Query: "霜叶的季度汇报准备得怎么样？", Mode: memory.Remember}
		budget := memory.Budget{Candidates: 20, Tokens: 4000, Edges: 20, Hops: 1}
		tokens := memory.SearchTokens(in.Query)
		ra := memory.RecallResult{Memories: []memory.Ref{}, Evidence: []memory.Evidence{}, Coverage: coverage()}
		rb := ra
		rs := memory.Scope{OwnerID: scope.OwnerID, PrincipalID: "model", Team: true}
		if err := s.p2LegacyRecallTx(ctx, tx, rs, in, budget, in.Query, "", nil, "", 0, "large-oracle", tokens, &ra); err != nil {
			return err
		}
		if err := s.recallTx(ctx, tx, rs, in, budget, in.Query, "", nil, "", 0, "large-oracle", tokens, &rb); err != nil {
			return err
		}
		p2Bytes(t, "10,000-claim recall", ra, rb)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestP2RecallTemporalAndVectorBytes(t *testing.T) {
	s, scope := p2Fixture(t, 120)
	ctx := context.Background()
	if _, err := s.pool.Exec(ctx, `INSERT INTO embeddings(owner_id,record_id,record_version,model,dimensions,embedding)
 SELECT owner_id,record_id,version,'p2-fictional-vector',2,'[1,0]'::vector FROM record_versions
 WHERE owner_id=$1 AND record_id=ANY($2::uuid[])`, scope.OwnerID, []string{p2ID("claim", 5), p2ID("claim", 97), p2ID("source", 5)}); err != nil {
		t.Fatal(err)
	}
	// A source revision, a future-valid claim and a user correction exercise the
	// original resolver with historical known-time and current source identity.
	if _, err := s.pool.Exec(ctx, "UPDATE record_versions SET valid_from=now()+interval '1 day' WHERE owner_id=$1 AND record_id=$2", scope.OwnerID, p2ID("claim", 5)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, "UPDATE claim_source_keys SET claim_key=md5(claim_key)||md5('synthetic-'||claim_key) WHERE owner_id=$1 AND claim_id=$2", scope.OwnerID, p2ID("claim", 97)); err != nil {
		t.Fatal(err)
	}
	workspaceCommand(t, s, scope, workspace.Command{Type: "editMemory", ID: p2ID("claim", 97), Text: "虚构人物云杉纠正后的季度汇报说明。"})
	err := pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		var now time.Time
		if err := tx.QueryRow(ctx, "SELECT now()").Scan(&now); err != nil {
			return err
		}
		past, future := now.Add(-36*time.Hour), now.Add(48*time.Hour)
		for _, times := range [][2]*time.Time{{nil, nil}, {&past, &past}, {&future, &now}, {&now, &past}} {
			for _, mode := range []memory.RecallMode{memory.Remember, memory.History, memory.Continue} {
				for _, offset := range []int{0, 3} {
					in := memory.RecallRequest{Query: "云杉", Mode: mode, Context: memory.WorkingContext{ValidAt: times[0], KnownAt: times[1]}}
					budget := memory.Budget{Candidates: 10, Tokens: 3000, Edges: 10, Hops: 1}
					rs := memory.Scope{OwnerID: scope.OwnerID, PrincipalID: "model", Team: true}
					a := memory.RecallResult{Memories: []memory.Ref{}, Evidence: []memory.Evidence{}, Coverage: coverage()}
					b := a
					if err := s.p2LegacyRecallTx(ctx, tx, rs, in, budget, in.Query, "'云杉'", []byte("[1,0]"), "p2-fictional-vector", offset, "temporal-vector", memory.SearchTokens(in.Query), &a); err != nil {
						return err
					}
					if err := s.recallTx(ctx, tx, rs, in, budget, in.Query, "'云杉'", []byte("[1,0]"), "p2-fictional-vector", offset, "temporal-vector", memory.SearchTokens(in.Query), &b); err != nil {
						return err
					}
					p2Bytes(t, "historical/valid-time/vector/pagination", a, b)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
