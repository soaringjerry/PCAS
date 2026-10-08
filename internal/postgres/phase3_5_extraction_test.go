package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/worker"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func phase35Model(t *testing.T, s *Store, reply func(http.ResponseWriter, *http.Request)) *atomic.Int32 {
	t.Helper()
	calls := &atomic.Int32{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); reply(w, r) }))
	t.Cleanup(srv.Close)
	s.SetModels(&ai.Registry{HTTP: srv.Client(), Config: ai.Configuration{Extraction: "model", Providers: []ai.Provider{{ID: "model", Name: "虚构青岚验收", Protocol: "openai", BaseURL: srv.URL, Model: "phase35-fiction", CostMode: "free", MaxOutput: 8192}}}})
	return calls
}
func phase35Extract(t *testing.T, s *Store, scope memory.Scope, ref memory.Ref) {
	t.Helper()
	if err := s.ProcessExtraction(context.Background(), leaseStage(t, s, scope, ref, "source.extract")); err != nil {
		t.Fatal(err)
	}
}
func phase35Action(t *testing.T, s *Store, scope memory.Scope, item string) string {
	t.Helper()
	var id string
	err := s.pool.QueryRow(context.Background(), `SELECT id::text FROM action_log WHERE owner_id=$1 AND EXISTS(SELECT 1 FROM jsonb_array_elements(changes) c WHERE c->>'table'='work_items' AND c->>'id'=$2) AND undone_at IS NULL ORDER BY created_at LIMIT 1`, scope.OwnerID, item).Scan(&id)
	if err != nil {
		t.Fatal("automatic creation lacks undo action", err)
	}
	return id
}
func phase35Direct(text, kind string) extractedItem {
	return extractedItem{Kind: kind, Text: text, Quote: text, Nature: "intention", Subject: "我", Predicate: "虚构目标", Explicit: true, Confidence: 1, Acquisition: "direct", Qualification: "asserted"}
}
func phase35Work(st workspace.State) []workspace.Item {
	out := append([]workspace.Item{}, st.Tasks...)
	return append(out, st.Ideas...)
}
func phase35RequireCreation(t *testing.T, s *Store, scope memory.Scope, it workspace.Item, ref memory.Ref) string {
	t.Helper()
	var wire struct {
		Creation *struct {
			By     string
			Source *workspace.SourceRef
		}
	}
	if err := json.Unmarshal(asJSON(it), &wire); err != nil {
		t.Fatal(err)
	}
	if wire.Creation == nil || wire.Creation.By != "background_extraction" || wire.Creation.Source == nil || wire.Creation.Source.SourceID != string(ref.ID) || wire.Creation.Source.Excerpt == "" {
		t.Fatalf("automatic work lost actor/evidence provenance %+v", it)
	}
	action := phase35Action(t, s, scope, it.ID)
	st := phase35Snapshot(t, s, scope)
	found := false
	for _, a := range st.Activity {
		if a.ID == action {
			found = true
		}
	}
	if !found {
		t.Fatalf("no away receipt for action %s", action)
	}
	return action
}
func TestPhase35B1B2B6G5TrustMatrix(t *testing.T) {
	phase35Finding(t, "S-P35-003")
	s, ctx := phase26DisposableStore(t)
	for _, tc := range []struct {
		name, kind, acquisition, qualification, role, branch string
		confidence                                           float64
		explicit, want                                       bool
		reason                                               string
	}{
		{"direct task", "task", "direct", "asserted", "user", "current", 1, true, true, ""},
		{"direct idea", "idea", "direct", "asserted", "user", "current", 1, true, true, ""},
		{"reported", "task", "reported", "quoted", "user", "current", 1, false, false, "reported"},
		{"qualified", "idea", "direct", "tentative", "user", "current", 1, false, false, "qualified"},
		{"AI suggestion", "task", "direct", "ai_suggestion", "user", "current", 1, false, false, "ai_suggestion"},
		{"uncertain", "idea", "inferred", "unknown", "user", "current", .99, false, false, "uncertain"},
		{"threshold below", "task", "direct", "asserted", "user", "current", .979, true, false, "uncertain"},
		{"threshold at", "idea", "direct", "asserted", "user", "current", .98, true, true, ""},
		{"historical", "task", "direct", "asserted", "user", "historical", 1, true, false, "historical"},
		{"imported archive", "idea", "direct", "asserted", "user", "archive", 1, true, false, "historical"},
		{"assistant", "task", "direct", "asserted", "assistant", "current", 1, true, false, ""},
		{"tool", "idea", "direct", "asserted", "tool", "current", 1, true, false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {

			scope := owner()
			phase35Snapshot(t, s, scope)
			it := phase35Direct("虚构：我把书名定为「可能世界」，这是确定的名字。", tc.kind)
			it.Acquisition = tc.acquisition
			it.Qualification = tc.qualification
			it.Confidence = tc.confidence
			it.Explicit = tc.explicit
			ref := phase35Source(t, s, scope, []extractedItem{it})
			if tc.branch == "archive" {
				archive := b4bImport(t, s, scope, b4bMessages(t, []string{"user"}, []string{it.Quote}))
				ref = archive.Sources[0]
			} else if _, err := s.pool.Exec(ctx, `INSERT INTO source_contexts(owner_id,source_id,source_version,role,branch) VALUES($1,$2,1,$3,$4)`, scope.OwnerID, ref.ID, tc.role, tc.branch); err != nil {
				t.Fatal(err)
			}
			calls := phase35Model(t, s, func(w http.ResponseWriter, r *http.Request) {
				if tc.branch == "archive" {
					var item map[string]any
					_ = json.Unmarshal(asJSON(it), &item)
					item["message_index"] = 1
					secretaryModelReply(w, map[string]any{"items": []any{item}})
				} else {
					secretaryModelReply(w, extracted{Items: []extractedItem{it}})
				}
			})
			phase35Extract(t, s, scope, ref)
			if tc.branch == "archive" {
				for step := 0; step < 8; step++ {
					var record memory.ID
					var version int
					var stage string
					err := s.pool.QueryRow(ctx, `SELECT record_id::text,record_version,stage FROM memory_jobs WHERE owner_id=$1 AND state='queued' AND stage LIKE 'source.extract:conversation:%' ORDER BY created_at LIMIT 1`, scope.OwnerID).Scan(&record, &version, &stage)
					if errors.Is(err, pgx.ErrNoRows) {
						break
					}
					if err != nil {
						t.Fatal(err)
					}
					if err := s.ProcessExtraction(ctx, leaseStage(t, s, scope, memory.Ref{ID: record, Version: version, Kind: memory.SourceKind}, stage)); err != nil {
						t.Fatal(err)
					}
				}
			}
			st := phase35Snapshot(t, s, scope)
			work := phase35Work(st)
			want := 0
			if tc.want {
				want = 1
			}
			if len(work) != want {
				t.Fatalf("work=%d want=%d; model fields must determine semantics", len(work), want)
			}
			if tc.want {
				phase35RequireCreation(t, s, scope, work[0], ref)
				if calls.Load() != 1 {
					t.Fatalf("provider calls=%d", calls.Load())
				}
			}
			if tc.reason != "" {
				var rows []struct {
					Reasons []string
					Reason  string
				}
				r, err := s.pool.Query(ctx, `SELECT document FROM capture_candidates WHERE owner_id=$1 AND state='pending'`, scope.OwnerID)
				if err != nil {
					t.Fatal(err)
				}
				for r.Next() {
					var raw []byte
					if err := r.Scan(&raw); err != nil {
						t.Fatal(err)
					}
					var row struct {
						Reasons []string
						Reason  string
					}
					if err := json.Unmarshal(raw, &row); err != nil {
						t.Fatal(err)
					}
					rows = append(rows, row)
				}
				r.Close()
				if len(rows) != 1 || !oneOf(tc.reason, rows[0].Reasons...) || rows[0].Reason == "" {
					t.Fatalf("candidate must retain reason %s: %+v", tc.reason, rows)
				}
			}
		})
	}
}
func TestPhase35B5G1OverflowPreservedAndObservable(t *testing.T) {
	phase35Finding(t, "S-P35-003")
	s, ctx := phase26DisposableStore(t)
	scope := owner()
	phase35Snapshot(t, s, scope)
	items := []extractedItem{}
	for i := 0; i < 25; i++ {
		items = append(items, phase35Direct(fmt.Sprintf("虚构独立待办 %02d", i), "task"))
	}
	ref := phase35Source(t, s, scope, items)
	calls := phase35Model(t, s, func(w http.ResponseWriter, r *http.Request) { secretaryModelReply(w, extracted{Items: items}) })
	phase35Extract(t, s, scope, ref)
	st := phase35Snapshot(t, s, scope)
	pending := []workspace.Candidate{}
	for _, c := range st.Candidates {
		if c.State == "pending" {
			pending = append(pending, c)
		}
	}
	if len(st.Tasks) != 20 || len(pending) != 5 {
		t.Fatalf("20 creates + 5 retained candidates: tasks=%d candidates=%d", len(st.Tasks), len(pending))
	}
	for _, c := range pending {
		var wire struct{ Reasons []string }
		_ = json.Unmarshal(asJSON(c), &wire)
		if !oneOf("overflow", wire.Reasons...) {
			t.Fatal("overflow missing reason", c)
		}
	}
	var overflow int
	err := s.pool.QueryRow(ctx, `SELECT coalesce(sum(count),0) FROM background_stage_events WHERE owner_id=$1 AND stage='source.extract' AND outcome='overflow' AND reason='automatic_task_limit'`, scope.OwnerID).Scan(&overflow)
	if err != nil || overflow != 5 {
		t.Fatalf("observable overflow=%d err=%v", overflow, err)
	}
	if calls.Load() != 1 {
		t.Fatal("extra model call for confidence", calls.Load())
	}
}
func TestPhase35B3B4T2UndoAndReplaySequences(t *testing.T) {
	phase35Finding(t, "S-P35-003")
	s, ctx := phase26DisposableStore(t)
	for _, sequence := range []string{"continuous", "jump", "interleaved", "deleted", "replay", "cross-channel"} {
		t.Run(sequence, func(t *testing.T) {
			scope := owner()
			phase35Snapshot(t, s, scope)
			items := []extractedItem{phase35Direct("虚构做一本青岚声音手册", "idea"), phase35Direct("虚构给青岚书店寄陶瓷标本", "task")}
			ref := phase35Source(t, s, scope, items)
			phase35Model(t, s, func(w http.ResponseWriter, r *http.Request) { secretaryModelReply(w, extracted{Items: items}) })
			phase35Extract(t, s, scope, ref)
			st := phase35Snapshot(t, s, scope)
			if len(st.Tasks) != 1 || len(st.Ideas) != 1 {
				t.Fatalf("precondition automatic creates=%v", phase35Work(st))
			}
			a := phase35RequireCreation(t, s, scope, st.Ideas[0], ref)
			b := phase35RequireCreation(t, s, scope, st.Tasks[0], ref)
			h := phase35HTTP(t, s, scope)
			switch sequence {
			case "interleaved":
				h.command(t, ctx, map[string]any{"type": "renameThing", "id": st.Ideas[0].ID, "title": "虚构用户后续编辑"})
				_, err := s.Undo(ctx, scope, a)
				if err == nil {
					t.Fatal("undo silently discarded user edit")
				}
				after := phase35Snapshot(t, s, scope)
				if after.Ideas[0].Title != "虚构用户后续编辑" || len(after.Tasks) != 1 {
					t.Fatal("refused undo changed content")
				}
				return
			case "deleted":
				h.command(t, ctx, map[string]any{"type": "deleteThing", "id": st.Ideas[0].ID})
				_, _ = s.Undo(ctx, scope, a)
				if len(phase35Snapshot(t, s, scope).Ideas) != 0 || len(phase35Snapshot(t, s, scope).Tasks) != 1 {
					t.Fatal("undo after deletion resurrected work or touched unrelated task")
				}
				return
			case "jump":
				if _, err := s.Undo(ctx, scope, a); err != nil {
					t.Fatal(err)
				}
				if len(phase35Snapshot(t, s, scope).Tasks) != 1 {
					t.Fatal("jump undo touched unrelated task")
				}
				if _, err := s.Undo(ctx, scope, b); err != nil {
					t.Fatal(err)
				}
			case "cross-channel":
				// The Telegram callback uses this same Undo service; HTTP creation/reads
				// and callback service reversal verify the state seam, not Telegram delivery.
				if _, err := s.Undo(withActor(ctx, "telegram"), scope, b); err != nil {
					t.Fatal(err)
				}
				if _, err := s.Undo(ctx, scope, a); err != nil {
					t.Fatal(err)
				}
			default:
				phase35Extract(t, s, scope, ref)
				if len(phase35Work(phase35Snapshot(t, s, scope))) != 2 {
					t.Fatal("replay duplicated work")
				}
				h.command(t, ctx, map[string]any{"type": "undoAction", "id": b})
				h.command(t, ctx, map[string]any{"type": "undoAction", "id": a})
			}
			if len(phase35Work(phase35Snapshot(t, s, scope))) != 0 {
				t.Fatal("creation reversal retained work/provenance")
			}
			phase35Extract(t, s, scope, ref)
			if len(phase35Work(phase35Snapshot(t, s, scope))) != 0 {
				t.Fatal("replay after undo recreated work")
			}
			src, err := s.GetSource(ctx, scope, ref.ID, ref.Version)
			if err != nil || !strings.Contains(src.Source.Text, items[0].Quote) {
				t.Fatal("undo deleted original speech", err)
			}
		})
	}
}
func TestPhase35G2ExtractionFailurePreservesGoodResultAndCounts(t *testing.T) {
	s, ctx := phase26DisposableStore(t)
	scope := owner()
	phase35Snapshot(t, s, scope)
	_, err := s.Execute(ctx, scope, workspace.Command{Type: "addTask", RequestID: string(memory.NewID()), Title: "虚构已有好结果"})
	if err != nil {
		t.Fatal(err)
	}
	before := phase35Work(phase35Snapshot(t, s, scope))
	for _, mode := range []string{"model failure", "invalid output"} {
		t.Run(mode, func(t *testing.T) {
			ref := phase35Source(t, s, scope, []extractedItem{phase35Direct("虚构待办失败保留", "task")})
			phase35Model(t, s, func(w http.ResponseWriter, r *http.Request) {
				if mode == "model failure" {
					w.WriteHeader(500)
				} else {
					secretaryModelReply(w, "unusable output")
				}
			})
			for attempt := 1; attempt <= 3; attempt++ {
				j := leaseStage(t, s, scope, ref, "source.extract")
				j.Attempts = attempt
				if _, err := s.pool.Exec(ctx, `UPDATE memory_jobs SET attempts=$2 WHERE id=$1`, j.ID, attempt); err != nil {
					t.Fatal(err)
				}
				err := s.ProcessExtraction(ctx, j)
				var je *worker.JobError
				if !errors.As(err, &je) || je.Code == "" {
					t.Fatalf("failure not named: %v", err)
				}
				if err := s.Retry(ctx, j, je.Code); err != nil {
					t.Fatal(err)
				}
				var recorded int
				var reason string
				if err := s.pool.QueryRow(ctx, `SELECT coalesce(sum(count),0),coalesce(max(reason),'') FROM background_stage_events WHERE owner_id=$1 AND stage='source.extract' AND outcome='failure'`, scope.OwnerID).Scan(&recorded, &reason); err != nil || recorded < attempt || reason == "" {
					t.Fatalf("failure counts=%d reason=%s err=%v", recorded, reason, err)
				}
				var state string
				if err := s.pool.QueryRow(ctx, `SELECT state FROM memory_jobs WHERE id=$1`, j.ID).Scan(&state); err != nil || state == "done" {
					t.Fatalf("failed result marked processed state=%s err=%v", state, err)
				}
				if !reflect.DeepEqual(before, phase35Work(phase35Snapshot(t, s, scope))) {
					t.Fatal("G2 changed existing good work")
				}
			}
		})
	}
}
func TestPhase35G3PaidExtractionRetryAfterPersistenceFailure(t *testing.T) {
	phase35Finding(t, "S-P35-003")
	s, ctx := phase26DisposableStore(t)
	scope := owner()
	phase35Snapshot(t, s, scope)
	it := phase35Direct("虚构只付费一次的自动待办", "task")
	ref := phase35Source(t, s, scope, []extractedItem{it})
	j := leaseStage(t, s, scope, ref, "source.extract")
	calls := phase35Model(t, s, func(w http.ResponseWriter, r *http.Request) {
		secretaryModelReply(w, extracted{Items: []extractedItem{it}})
	})
	// Injection targets only the owned DB business row, AFTER paid result storage.
	_, err := s.pool.Exec(ctx, `CREATE FUNCTION phase35_reject_work() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'phase35 injected business write failure'; END $$; CREATE TRIGGER phase35_reject_work BEFORE INSERT ON work_items FOR EACH ROW EXECUTE FUNCTION phase35_reject_work()`)
	if err != nil {
		t.Fatal(err)
	}
	err = s.ProcessExtraction(ctx, j)
	if err == nil {
		t.Fatal("write fault did not reach real business transaction")
	}
	var n int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM background_model_results WHERE job_id=$1`, j.ID).Scan(&n); err != nil || n != 1 {
		t.Fatal("paid result not retained", n, err)
	}
	if _, err := s.pool.Exec(ctx, `DROP TRIGGER phase35_reject_work ON work_items; DROP FUNCTION phase35_reject_work()`); err != nil {
		t.Fatal(err)
	}
	peer, err := Open(ctx, s.pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	peer.SetModels(s.models)
	if err := peer.ProcessExtraction(ctx, j); err != nil {
		t.Fatal("durable retry", err)
	}
	if calls.Load() != 1 || len(phase35Snapshot(t, s, scope).Tasks) != 1 {
		t.Fatalf("G3 provider attempts=%d", calls.Load())
	}
}
func TestPhase35G4SingleSourceChangeLeavesUnrelatedPaidResult(t *testing.T) {
	s, ctx := phase26DisposableStore(t)
	scope := owner()
	phase35Snapshot(t, s, scope)
	refs := []memory.Ref{}
	for _, text := range []string{"虚构独立原话A", "虚构独立原话B"} {
		refs = append(refs, phase35Source(t, s, scope, []extractedItem{phase35Direct(text, "task")}))
	}
	calls := phase35Model(t, s, func(w http.ResponseWriter, r *http.Request) {
		secretaryModelReply(w, extracted{Items: []extractedItem{}})
	})
	phase35Extract(t, s, scope, refs[0])
	phase35Extract(t, s, scope, refs[1])
	var before string
	if err := s.pool.QueryRow(ctx, `SELECT md5(to_jsonb(j)::text||xmin::text) FROM memory_jobs j WHERE owner_id=$1 AND record_id=$2 AND stage='source.extract'`, scope.OwnerID, refs[1].ID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	source, err := s.GetSource(ctx, scope, refs[0].ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.Ingest(ctx, scope, memory.IngestRequest{Connector: source.Source.Connector, ExternalID: source.Source.ExternalID, ExternalVersion: "2", Text: "虚构修改A", Title: source.Source.Title})
	if err != nil {
		t.Fatal(err)
	}
	phase35Extract(t, s, scope, r.Ref)
	var after string
	if err := s.pool.QueryRow(ctx, `SELECT md5(to_jsonb(j)::text||xmin::text) FROM memory_jobs j WHERE owner_id=$1 AND record_id=$2 AND stage='source.extract'`, scope.OwnerID, refs[1].ID).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if before != after || calls.Load() != 3 {
		t.Fatal("single source edit reprocessed unrelated paid result", calls.Load())
	}
}

// New topic-stage budgets are certified separately once their skeleton is published.
func TestPhase35G7ExistingIndependentStageBudgets(t *testing.T) {
	s, ctx := phase26DisposableStore(t)
	scope := owner()
	phase35Snapshot(t, s, scope)
	for stage, want := range map[string]int{OrganizeStage: 40, CompareStage: 40, EntityCompareStage: 30, EntityCandidatesStage: 6, HandoverStage: 2} {
		if backgroundHourlyBudgets[stage] != want {
			t.Fatalf("%s budget=%d want=%d", stage, backgroundHourlyBudgets[stage], want)
		}
		if _, err := s.pool.Exec(ctx, `INSERT INTO background_usage(id,owner_id,reserved_cost,stage) SELECT gen_random_uuid(),$1,0,$2 FROM generate_series(1,$3)`, scope.OwnerID, stage, want); err != nil {
			t.Fatal(err)
		}
		err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error { return backgroundHourlyTx(ctx, tx, stage, "phase35_hourly_limit") })
		var e *worker.JobError
		if !errors.As(err, &e) || !e.NoAttempt || e.Until.IsZero() {
			t.Fatalf("%s did not enforce independent budget: %v", stage, err)
		}
		if _, err := s.pool.Exec(ctx, `DELETE FROM background_usage WHERE owner_id=$1 AND stage=$2`, scope.OwnerID, stage); err != nil {
			t.Fatal(err)
		}
		if err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error { return backgroundHourlyTx(ctx, tx, stage, "phase35_hourly_limit") }); err != nil {
			t.Fatal("unused own capacity unavailable", err)
		}
	}
}

func TestPhase35B6LegacySettingWriteIsIgnored(t *testing.T) {
	phase35Finding(t, "S-P35-007")
	s, ctx := phase26DisposableStore(t)
	scope := owner()
	phase35Snapshot(t, s, scope)
	h := phase35HTTP(t, s, scope)
	for _, v := range []bool{false, true} {
		h.command(t, ctx, map[string]any{"type": "updateSettings", "patch": map[string]any{"autoAccept": v}})
		if phase35Snapshot(t, s, scope).Settings.AutoAccept {
			t.Fatal("old setting regained meaning")
		}
	}
}

func TestPhase35T1T2ThirtyTrustInputsAtScale(t *testing.T) {
	phase35Finding(t, "S-P35-003")
	f := phase35Load(t)
	s := f.Store
	ref := phase35Source(t, s, f.Scope, f.Inputs)
	calls := phase35Model(t, s, func(w http.ResponseWriter, r *http.Request) { secretaryModelReply(w, extracted{Items: f.Inputs}) })
	phase35Extract(t, s, f.Scope, ref)
	st := phase35Snapshot(t, s, f.Scope)
	work := phase35Work(st)
	if len(st.Tasks) != 5 || len(st.Ideas) != 5 {
		t.Fatalf("30 frozen inputs must create 5 direct tasks+5 direct ideas: %d/%d", len(st.Tasks), len(st.Ideas))
	}
	for _, it := range work {
		phase35RequireCreation(t, s, f.Scope, it, ref)
	}
	pending := 0
	for _, c := range st.Candidates {
		if c.Source.SourceID == string(ref.ID) && c.State == "pending" {
			pending++
		}
	}
	if pending != 20 || calls.Load() != 1 {
		t.Fatalf("reserved/reported/AI/uncertain candidates=%d model_calls=%d want 20/1", pending, calls.Load())
	}
}
