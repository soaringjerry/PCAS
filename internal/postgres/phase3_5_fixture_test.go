package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/httpapi"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// Every finding was resolved on the integration branch (2026-10-08, see
// docs/tasks/phase3_5/phase3_5_acceptance.md), so the gate is open: these run
// by default. The id stays at each call site as the record of what it guards.
func phase35Finding(t *testing.T, id string) { t.Helper() }

type phase35Deadline struct {
	ID, Claim, Kind, Original, Recurrence string
	At                                    *time.Time
	DateOnly                              bool
}
type phase35Fixture struct {
	*phase26LoadedFixture
	Deadlines []phase35Deadline
	Topics    []string
	Inputs    []extractedItem
}

func phase35Items(n int) []extractedItem {
	out := []extractedItem{}
	for i := 0; i < n; i++ {
		kind := "task"
		if i%2 == 1 {
			kind = "idea"
		}
		text := fmt.Sprintf("虚构青岚待办 %02d：寄送独立陶瓷标本。", i)
		if i%2 == 1 {
			text = fmt.Sprintf("Fictitious Azure idea %02d: compile an independent sound atlas.", i)
		}
		it := extractedItem{Kind: kind, Text: text, Quote: text, Nature: "intention", Subject: "我", Predicate: "虚构事项", Explicit: true, Confidence: 1, Acquisition: "direct", Qualification: "asserted"}
		switch i % 6 {
		case 2:
			it.Acquisition = "reported"
			it.Qualification = "quoted"
			it.Explicit = false
		case 3:
			it.Qualification = "tentative"
			it.Explicit = false
		case 4:
			it.Qualification = "ai_suggestion"
			it.Explicit = false
		case 5:
			it.Confidence = .97
		}
		out = append(out, it)
	}
	return out
}
func phase35Load(t *testing.T) *phase35Fixture {
	t.Helper()
	f := &phase35Fixture{phase26LoadedFixture: phase26LoadFixture(t), Inputs: phase35Items(30)}
	// Initialize owner/agents outside the read-only assertion interval.
	if _, err := f.Store.Snapshot(f.Context, f.Scope); err != nil {
		t.Fatal(err)
	}
	phase26Exec(t, f.phase26LoadedFixture, `UPDATE workspace_owners SET settings=settings||'{"timezone":"UTC","autoAccept":false}'::jsonb WHERE owner_id=$1`, f.Scope.OwnerID)
	for i := 0; i < 200; i++ {
		d := phase35Deadline{ID: string(memory.NewID()), Claim: string(f.Claims[2000+i]), Original: f.Corpus.Memories[2000+i].Text, Kind: "deadline"}
		at := time.Date(2026, 10, 8+i%24, 14+i%3, 0, 0, 0, time.UTC)
		switch i % 4 {
		case 0:
			d.Kind = "appointment"
			d.At = &at
		case 1:
			at = time.Date(2026, 9, 30, 17, 0, 0, 0, time.UTC)
			d.At = &at
		case 2:
			d.Kind = "recurring"
			d.Recurrence = "每天 09:00"
			at = time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
			d.At = &at
		case 3:
			d.Kind = "unclear"
			d.At = nil
		}
		if i == 0 {
			d.Claim = string(f.Claims[1400])
			d.Original = f.Corpus.Memories[1400].Text
		} // qualifying topic's deadline input
		f.Deadlines = append(f.Deadlines, d)
	}
	err := pgx.BeginFunc(f.Context, f.Store.pool, func(tx pgx.Tx) error {
		for _, d := range f.Deadlines {
			if _, err := tx.Exec(f.Context, `INSERT INTO deadlines(owner_id,id,claim_id,claim_version,kind,at,recurrence,title,time_note,original_text) VALUES($1,$2,$3,1,$4,$5,$6,$7,$8,$9)`, f.Scope.OwnerID, d.ID, d.Claim, d.Kind, d.At, d.Recurrence, "虚构期限 "+d.ID, "虚构来源的时间说明", d.Original); err != nil {
				return err
			}
		}
		// Three topics are deliberately isolated from the base corpus memberships.
		// 12 is within the contract's expected N range, not a guessed published N.
		// Actual N boundary tests use the eventual backend publication separately.
		for j, index := range []int{14, 18, 22} {
			id := string(f.Entities[index])
			f.Topics = append(f.Topics, "entity:"+id)
			if _, err := tx.Exec(f.Context, `DELETE FROM claim_mentions WHERE owner_id=$1 AND entity_id=$2`, f.Scope.OwnerID, id); err != nil {
				return err
			}
			for k := 0; k < 12; k++ {
				claim := f.Claims[1400+j*12+k]
				category := "progress"
				if j != 1 && k == 0 {
					category = "goal"
				}
				if _, err := tx.Exec(f.Context, `UPDATE claim_revisions SET category=$3 WHERE owner_id=$1 AND claim_id=$2;`, f.Scope.OwnerID, claim, category); err != nil {
					return err
				}
				// Spoken of a minute ago: a group nobody has mentioned for 45 days is not made a project, and of the rest the two spoken of last are queued first.
				if _, err := tx.Exec(f.Context, `UPDATE record_versions SET expressed_at=now()-interval '1 minute' WHERE owner_id=$1 AND record_id=$2`, f.Scope.OwnerID, claim); err != nil {
					return err
				}
				if _, err := tx.Exec(f.Context, `INSERT INTO claim_mentions(owner_id,claim_id,claim_version,entity_id,role) VALUES($1,$2,1,$3,'topic')`, f.Scope.OwnerID, claim, id); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// T1 candidates are pre-existing extraction inputs, not fabricated work results.
	src := phase35Source(t, f.Store, f.Scope, f.Inputs)
	err = pgx.BeginFunc(f.Context, f.Store.pool, func(tx pgx.Tx) error {
		for _, it := range f.Inputs {
			v := workspace.Candidate{ID: string(memory.NewID()), Kind: it.Kind, Text: it.Text, Confidence: it.Confidence, State: "pending", CreatedAt: stamp(), Source: workspace.SourceRef{SourceID: string(src.ID), Version: src.Version, Excerpt: it.Quote, Label: "虚构抽取候选", At: stamp()}}
			if err := saveCandidate(f.Context, tx, f.Scope, v); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return f
}
func phase35Source(t *testing.T, s *Store, scope memory.Scope, items []extractedItem) memory.Ref {
	t.Helper()
	texts := []string{}
	for _, it := range items {
		texts = append(texts, it.Quote)
	}
	r, err := s.Ingest(context.Background(), scope, memory.IngestRequest{Connector: "capture", ExternalID: string(memory.NewID()), ExternalVersion: "1", Title: "虚构青岚原话", Text: strings.Join(texts, "\n")})
	if err != nil {
		t.Fatal(err)
	}
	return r.Ref
}
func phase35HTTP(t *testing.T, s *Store, scope memory.Scope) *phase3HTTP {
	t.Helper()
	// Complete real route wiring, including endpoints used only on page mount.
	api := httpapi.New(s, s, b1Auth{scope}, s.Ping, slog.New(slog.NewTextHandler(io.Discard, nil)), httpapi.Options{Workspace: s, Continuity: s, Connectors: s, Editor: s, Writer: s, Activity: s, Attachments: s, Models: s.models})
	srv := httptest.NewServer(api)
	t.Cleanup(srv.Close)
	return &phase3HTTP{Base: srv.URL, Client: srv.Client()}
}
func phase35Digest(t *testing.T, f *phase35Fixture) map[string]string {
	return phase3AllTableDigest(t, &phase3LoadedFixture{phase26LoadedFixture: f.phase26LoadedFixture})
}
func phase35Snapshot(t *testing.T, s *Store, scope memory.Scope) workspace.State {
	t.Helper()
	st, err := s.Snapshot(context.Background(), scope)
	if err != nil {
		t.Fatal(err)
	}
	return st
}
func TestPhase35T1FrozenInputsAndLoadedScale(t *testing.T) {
	TestPhase26T1CorpusOracle(t)
	if len(phase35FrozenRules) != 23 || len(phase35FrozenSequences) != 10 {
		t.Fatal("frozen coverage changed")
	}
	if !reflect.DeepEqual(phase35Items(30), phase35Items(30)) {
		t.Fatal("unstable input oracle")
	}
	f := phase35Load(t)
	for table, want := range map[string]int{"claims": 5000, "entities": 1000, "deadlines": 200, "capture_candidates": 30} {
		var n int
		if err := f.Store.pool.QueryRow(f.Context, "SELECT count(*) FROM "+pgx.Identifier{table}.Sanitize()+" WHERE owner_id=$1", f.Scope.OwnerID).Scan(&n); err != nil || n != want {
			t.Fatalf("%s=%d want=%d err=%v", table, n, want, err)
		}
	}
	var groups struct{ Items []json.RawMessage }
	phase35HTTP(t, f.Store, f.Scope).get(t, f.Context, "/v1/workspace/memory-groups", &groups)
	if len(groups.Items) != 300 {
		t.Fatalf("groups=%d want 300", len(groups.Items))
	}
	for i, key := range f.Topics {
		var n, goals, dates int
		err := f.Store.pool.QueryRow(f.Context, `SELECT count(*),count(*) FILTER(WHERE cr.category='goal'),count(*) FILTER(WHERE EXISTS(SELECT 1 FROM deadlines d WHERE d.owner_id=m.owner_id AND d.claim_id=m.claim_id)) FROM status_current_members m JOIN claim_revisions cr ON (cr.owner_id,cr.claim_id,cr.version)=(m.owner_id,m.claim_id,m.claim_version) WHERE m.owner_id=$1 AND m.key=$2`, f.Scope.OwnerID, key).Scan(&n, &goals, &dates)
		if err != nil || n != 12 || (i == 1 && goals != 0) || (i == 2 && dates != 0) || (i == 0 && (goals != 1 || dates != 1)) {
			t.Fatalf("topic %d members=%d goals=%d deadlines=%d err=%v", i, n, goals, dates, err)
		}
	}
	t.Log("T1: 5000 memories / 300 groups / 1000 entities + 200 deadlines / 30 candidates / 3 controlled topics")
}
