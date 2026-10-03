package postgres

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/notify"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// Exercise the real HTTP provider while its response is withheld. Corrections
// must not erase a paid answer; loss of access must dominate any correction.
func TestR1R2CompletionRechecksDependencyAccess(t *testing.T) {
	for _, change := range []string{"correct", "source-correct", "revoke", "correct-and-revoke", "correct-and-delete", "delete"} {
		t.Run(change, func(t *testing.T) {
			s, scope := testStore(t), owner()
			f := b4Model(t, s)
			sourceInput := memory.IngestRequest{Connector: "manual", ExternalID: string(memory.NewID()), ExternalVersion: "1", Title: "R1 reference", Text: "R1 lighthouse reference", MediaType: "text/plain"}
			source := mustIngest(t, s, scope, sourceInput).Ref
			claim := b1Claim(t, s, scope, "R1 lighthouse fact", "fact", "adopted", source)
			second := b1Claim(t, s, scope, "R1 lighthouse constraint", "preference", "confirmed", source)
			f.set("R1 returned draft", 200)
			st := workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "R1 lighthouse"})
			st = workspaceCommand(t, s, scope, workspace.Command{Type: "requestRun", ThingID: st.Tasks[0].ID, AgentID: "model", Kind: "draft", Prompt: "R1 lighthouse"})
			run := st.Runs[0]
			b1HasRef(t, run.ContextVersions, claim, true)
			b1HasRef(t, run.ContextVersions, second, true)
			gate := b4HoldModel(t, f)
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- s.runAgentOnce(ctx) }()
			defer gate.unblock()
			select {
			case <-gate.started:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			switch change {
			case "correct", "correct-and-revoke", "correct-and-delete":
				b1Correct(t, s, scope, claim, "R1 corrected lighthouse fact")
			case "source-correct":
				sourceInput.ExternalVersion, sourceInput.Text = "2", "R1 corrected reference"
				if _, err := s.Ingest(context.Background(), scope, sourceInput); err != nil {
					t.Fatal(err)
				}
			}
			if change == "revoke" || change == "correct-and-revoke" {
				workspaceCommand(t, s, scope, workspace.Command{Type: "setMemoryVisibility", ID: string(second.ID), AgentIDs: []string{}})
			}
			if change == "correct-and-delete" {
				// A missing dependency in the final verification must win even if an
				// earlier dependency was corrected. Full deletion is covered below too.
				if _, err := s.pool.Exec(context.Background(), "UPDATE memory_records SET state='withdrawn' WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), string(second.ID)); err != nil {
					t.Fatal(err)
				}
			}
			if change == "delete" {
				workspaceCommand(t, s, scope, workspace.Command{Type: "deleteMemory", ID: string(second.ID)})
			}
			gate.unblock()
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			st, err := s.Snapshot(context.Background(), scope)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, r := range st.Runs {
				if r.ID != run.ID {
					continue
				}
				found = true
				if change == "correct" || change == "source-correct" {
					if r.Status != "done" || r.Output != "R1 returned draft" || !r.StaleContext || r.Adopted != nil {
						t.Errorf("correction must retain completed unadopted result: %+v", r)
					}
				} else if r.Status != "failed" || r.Output != "" || r.Adopted != nil {
					t.Errorf("inaccessible dependency retained output: %+v", r)
				}
			}
			if !found && change != "delete" {
				t.Error("run disappeared")
			}
			if len(st.Docs) != 0 {
				t.Error("changed result automatically adopted")
			}
			rows := b4Usage(t, s, scope)
			if len(rows) != 1 || rows[0].Cost <= 0 {
				t.Errorf("returned usage lost: %+v", rows)
			} else if math.Abs(st.BudgetUsage-rows[0].Cost) > 1e-9 {
				t.Errorf("completion did not settle budget after %s: budget=%g cost=%g", change, st.BudgetUsage, rows[0].Cost)
			}
		})
	}
}

func TestR1R5ContinuationUsesCurrentDocuments(t *testing.T) {
	s, scope := testStore(t), owner()
	f := b4Model(t, s)
	st := workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "Writing"})
	thing := st.Tasks[0].ID
	f.set("R1 obsolete original draft", 200)
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "requestRun", ThingID: thing, AgentID: "model", Kind: "draft", Prompt: "Write"})
	if err := s.runAgentOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	st, err := s.Snapshot(context.Background(), scope)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Docs) != 1 {
		t.Fatal("missing adopted doc", st.Docs)
	}
	workspaceCommand(t, s, scope, workspace.Command{Type: "updateDoc", ID: st.Docs[0].ID, Patch: asJSON(map[string]string{"body": "R1 owner edited draft"})})
	workspaceCommand(t, s, scope, workspace.Command{Type: "createDoc", Doc: &workspace.Doc{ID: string(memory.NewID()), ThingID: thing, Title: "Owner notes", Body: "R1 owner created notes", By: "user"}})
	unrelated := workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "Unrelated"}).Tasks[0].ID
	workspaceCommand(t, s, scope, workspace.Command{Type: "createDoc", Doc: &workspace.Doc{ID: string(memory.NewID()), ThingID: unrelated, Title: "Other notes", Body: "R1 unrelated secret", By: "user"}})
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "requestRun", ThingID: thing, AgentID: "model", Kind: "draft", Prompt: "Continue and revise"})
	brief := st.Runs[0].Brief
	for _, want := range []string{"R1 owner edited draft", "R1 owner created notes"} {
		if !strings.Contains(brief, want) {
			t.Errorf("current document missing: %s", want)
		}
	}
	for _, bad := range []string{"R1 obsolete original draft", "R1 unrelated secret", "上一次的结果："} {
		if strings.Contains(brief, bad) {
			t.Errorf("stale or unrelated content in brief: %s", bad)
		}
	}
	if err := s.runAgentOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.last(t).Prompt, "R1 owner edited draft") {
		t.Error("provider did not receive edited document")
	}
}

func TestR1R6SecretarySettlesOnlyItsReservation(t *testing.T) {
	for _, mode := range []string{"success", "invalid", "failure", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			s, scope := testStore(t), owner()
			f := b4Model(t, s)
			f.set(`{"reply":"R1 reply","actions":[]}`, 200)
			if mode == "invalid" {
				f.set("R1 plain reply", 200)
			}
			if mode == "failure" {
				f.set("", 500)
			}
			workspaceCommand(t, s, scope, workspace.Command{Type: "updateSettings", Patch: asJSON(map[string]any{"dailyBudget": 10})})
			// Another reservation must survive settlement of this call unchanged.
			if _, err := s.pool.Exec(context.Background(), "INSERT INTO background_usage(owner_id,reserved_cost) VALUES($1,0.03)", string(scope.OwnerID)); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var gate *b4ModelGate
			if mode == "cancel" {
				gate = b4HoldModel(t, f)
				defer gate.unblock()
			}
			done := make(chan error, 1)
			go func() { _, err := s.DeskTurn(ctx, scope, turnRequest("R1 budget question")); done <- err }()
			if gate != nil {
				select {
				case <-gate.started:
					cancel()
				case <-time.After(10 * time.Second):
					t.Fatal("model not started")
				}
				gate.unblock()
			}
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(60 * time.Second):
				t.Fatal("secretary did not finish")
			}
			var reserved float64
			if err := s.pool.QueryRow(context.Background(), "SELECT sum(reserved_cost) FROM background_usage WHERE owner_id=$1", string(scope.OwnerID)).Scan(&reserved); err != nil {
				t.Fatal(err)
			}
			want := 0.03
			if mode == "success" || mode == "invalid" {
				rows := b4Usage(t, s, scope)
				if len(rows) != 1 {
					t.Fatal(rows)
				}
				want += rows[0].Cost
			}
			if math.Abs(reserved-want) > 1e-9 {
				t.Errorf("budget reserved=%g want actual=%g", reserved, want)
			}
			// A new request under the remaining budget must actually reach the provider.
			if mode == "success" {
				workspaceCommand(t, s, scope, workspace.Command{Type: "updateSettings", Patch: asJSON(map[string]any{"dailyBudget": 0.07})})
				before := len(f.all())
				mustTurn(t, s, scope, turnRequest("R1 next question"))
				if len(f.all()) != before+1 {
					t.Error("settled budget blocked next call")
				}
			}
		})
	}
}

func TestR1R6DeputyAndExtractionReleaseFailedReservations(t *testing.T) {
	for _, kind := range []string{"deputy", "extraction", "invalid-extraction", "cancel-deputy", "cancel-extraction"} {
		t.Run(kind, func(t *testing.T) {
			s, scope := testStore(t), owner()
			f := b4Model(t, s)
			f.set("", 500)
			if kind == "invalid-extraction" {
				f.set("R1 invalid extraction", 200)
			}
			if _, err := s.Snapshot(context.Background(), scope); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var call func() error
			if strings.Contains(kind, "deputy") {
				st := workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "Failure"})
				workspaceCommand(t, s, scope, workspace.Command{Type: "requestRun", ThingID: st.Tasks[0].ID, AgentID: "model", Kind: "draft", Prompt: "Fail"})
				call = func() error { return s.runAgentOnce(ctx) }
			} else {
				source := mustIngest(t, s, scope, memory.IngestRequest{Connector: "manual", ExternalID: "r1", ExternalVersion: "1", Title: "R1", Text: "R1 source", MediaType: "text/plain"})
				job := leaseStage(t, s, scope, source.Ref, "source.extract")
				call = func() error { return s.ProcessExtraction(ctx, job) }
			}
			done := make(chan error, 1)
			var gate *b4ModelGate
			if strings.HasPrefix(kind, "cancel-") {
				gate = b4HoldModel(t, f)
				defer gate.unblock()
			}
			go func() { done <- call() }()
			if gate != nil {
				select {
				case <-gate.started:
					cancel()
				case <-time.After(10 * time.Second):
					t.Fatal("model not started")
				}
				gate.unblock()
			}
			select {
			case err := <-done:
				if kind == "deputy" && err != nil {
					t.Fatal(err)
				}
			case <-time.After(60 * time.Second):
				t.Fatal("call did not end")
			}
			var reserved float64
			if err := s.pool.QueryRow(context.Background(), "SELECT coalesce((SELECT sum(reserved_cost) FROM agent_runs WHERE owner_id=$1),0)+coalesce((SELECT sum(reserved_cost) FROM background_usage WHERE owner_id=$1),0)", string(scope.OwnerID)).Scan(&reserved); err != nil {
				t.Fatal(err)
			}
			want := 0.0
			if kind == "invalid-extraction" {
				rows := b4Usage(t, s, scope)
				if len(rows) != 1 {
					t.Fatal(rows)
				}
				want = rows[0].Cost
			}
			if math.Abs(reserved-want) > 1e-9 {
				t.Errorf("finished %s reserved=%g want=%g", kind, reserved, want)
			}
		})
	}
}

func TestR1R7RescheduledNoticeIsInvalidated(t *testing.T) {
	for _, change := range []string{"reschedule", "due-command", "remove", "inactive", "guard", "between-channels"} {
		t.Run(change, func(t *testing.T) {
			s, scope := testStore(t), owner()
			now := time.Now().Add(time.Second)
			notice := dueNotice(t, s, scope, now)
			changeItem := func() {
				if change == "due-command" {
					err := pgx.BeginFunc(context.Background(), s.pool, func(tx pgx.Tx) error {
						item, err := getItem(context.Background(), tx, scope, notice.ThingID)
						if err != nil {
							return err
						}
						item.Triggers[0].Offset = "at"
						return saveItem(context.Background(), tx, scope, item)
					})
					if err != nil {
						t.Fatal(err)
					}
					workspaceCommand(t, s, scope, workspace.Command{Type: "updateTask", ID: notice.ThingID, Patch: asJSON(map[string]string{"due": now.Add(24 * time.Hour).UTC().Format(time.RFC3339)})})
					return
				}
				err := pgx.BeginFunc(context.Background(), s.pool, func(tx pgx.Tx) error {
					item, err := getItem(context.Background(), tx, scope, notice.ThingID)
					if err != nil {
						return err
					}
					switch change {
					case "remove":
						item.Triggers = nil
					case "inactive":
						item.Triggers[0].Active = false
					case "guard":
						item.Triggers[0].Guard = "waiting"
					default:
						item.Triggers[0].NextAt = now.Add(24 * time.Hour).UTC().Format(time.RFC3339)
					}
					return saveItem(context.Background(), tx, scope, item)
				})
				if err != nil {
					t.Fatal(err)
				}
			}
			ch := &fakeNotifyChannel{name: "test"}
			channels := []notify.Channel{ch}
			if change == "between-channels" {
				channels = append([]notify.Channel{&fakeNotifyChannel{name: "first", before: changeItem}}, channels...)
			} else {
				changeItem()
				state, err := s.Snapshot(context.Background(), scope)
				if err != nil {
					t.Fatal(err)
				}
				for _, n := range state.Notices {
					if n.ID == notice.ID && n.DismissedAt == "" {
						t.Error("obsolete notice pinned before dispatch")
					}
				}
			}
			if err := s.DispatchNotices(context.Background(), now, channels); err != nil {
				t.Fatal(err)
			}
			if len(ch.calls) != 0 {
				t.Error("obsolete occurrence sent")
			}
			st, err := s.Snapshot(context.Background(), scope)
			if err != nil {
				t.Fatal(err)
			}
			for _, n := range st.Notices {
				if n.ID == notice.ID && n.DismissedAt == "" {
					t.Error("obsolete reminder still pinned")
				}
			}
			var dismissed bool
			if err := s.pool.QueryRow(context.Background(), "SELECT dismissed_at IS NOT NULL FROM workspace_notices WHERE id=$1", notice.ID).Scan(&dismissed); err != nil || !dismissed {
				t.Error("obsolete notice not durably invalidated", err)
			}
			if change == "reschedule" {
				if err := s.CheckReminders(context.Background(), now.Add(25*time.Hour)); err != nil {
					t.Fatal(err)
				}
				st, err = s.Snapshot(context.Background(), scope)
				if err != nil {
					t.Fatal(err)
				}
				active := 0
				for _, n := range st.Notices {
					if n.DismissedAt == "" {
						active++
					}
				}
				if active != 1 {
					t.Error("new reminder occurrence missing", active)
				}
			}
		})
	}
}

func TestR1R7RunNoticeDoesNotRequireReminderTrigger(t *testing.T) {
	s, scope := testStore(t), owner()
	now := time.Now().Add(time.Second)
	st := workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "Result"})
	_, err := s.pool.Exec(context.Background(), "INSERT INTO workspace_notices(owner_id,thing_id,trigger_id,due_at,reason) VALUES($1,$2,$3,$4,'Done')", string(scope.OwnerID), st.Tasks[0].ID, runNoticePrefix+string(memory.NewID()), now)
	if err != nil {
		t.Fatal(err)
	}
	ch := &fakeNotifyChannel{name: "test"}
	if err := s.DispatchNotices(context.Background(), now, []notify.Channel{ch}); err != nil {
		t.Fatal(err)
	}
	if len(ch.calls) != 1 {
		t.Error("run notice suppressed")
	}
}

func TestR1R5DocumentsRespectPermissionsAndLength(t *testing.T) {
	for _, change := range []string{"revoke", "exclude", "correct", "delete-doc", "long-doc"} {
		t.Run(change, func(t *testing.T) {
			s, scope := testStore(t), owner()
			f := b4Model(t, s)
			source := b1Source(t, s, scope, "R1 doc reference", "R1 doc reference", "manual")
			claim := b1Claim(t, s, scope, "R1 doc restriction", "preference", "confirmed", source)
			st := workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "R1 document"})
			thing := st.Tasks[0].ID
			f.set("R1 restricted document content", 200)
			workspaceCommand(t, s, scope, workspace.Command{Type: "requestRun", ThingID: thing, AgentID: "model", Kind: "draft", Prompt: "Write"})
			if err := s.runAgentOnce(context.Background()); err != nil {
				t.Fatal(err)
			}
			st, err := s.Snapshot(context.Background(), scope)
			if err != nil {
				t.Fatal(err)
			}
			doc := st.Docs[0]
			switch change {
			case "revoke":
				workspaceCommand(t, s, scope, workspace.Command{Type: "setMemoryVisibility", ID: string(claim.ID), AgentIDs: []string{}})
			case "exclude":
				workspaceCommand(t, s, scope, workspace.Command{Type: "toggleContextMemory", ThingID: thing, MemoryID: string(claim.ID)})
			case "correct":
				b1Correct(t, s, scope, claim, "R1 corrected document restriction")
			case "delete-doc":
				workspaceCommand(t, s, scope, workspace.Command{Type: "deleteDoc", ID: doc.ID})
			case "long-doc":
				workspaceCommand(t, s, scope, workspace.Command{Type: "updateDoc", ID: doc.ID, Patch: asJSON(map[string]string{"body": "R1 current long document\n" + strings.Repeat("正文", 20000) + "R1 truncated end"})})
			}
			st = workspaceCommand(t, s, scope, workspace.Command{Type: "requestRun", ThingID: thing, AgentID: "model", Kind: "draft", Prompt: "Continue"})
			brief := st.Runs[0].Brief
			if strings.Contains(brief, "R1 restricted document content") {
				t.Error("inaccessible or obsolete output was reused")
			}
			if change == "long-doc" {
				if !strings.Contains(brief, "R1 current long document") || strings.Contains(brief, "R1 truncated end") || !utf8.ValidString(brief) || len(brief) > 30500 {
					t.Error("document length bound or UTF8 failed", len(brief))
				}
			}
		})
	}
}

func TestR1R6ConcurrentSecretaryReservationsAreIndependent(t *testing.T) {
	s, scope := testStore(t), owner()
	f := b4Model(t, s)
	f.set(`{"reply":"R1 returned reply","actions":[]}`, 200)
	workspaceCommand(t, s, scope, workspace.Command{Type: "updateSettings", Patch: asJSON(map[string]any{"dailyBudget": 10})})
	gate := b4HoldModel(t, f)
	defer gate.unblock()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := s.DeskTurn(ctx, scope, turnRequest("R1 first conversation")); done <- err }()
	select {
	case <-gate.started:
	case <-time.After(10 * time.Second):
		t.Fatal("first call not started")
	}
	f.mu.Lock()
	f.gate = nil
	f.mu.Unlock()
	second := mustTurn(t, s, scope, turnRequest("R1 second conversation"))
	if second.Turn.Reply == "" {
		t.Fatal("second call did not return")
	}
	cancel()
	gate.unblock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("first call did not stop")
	}
	rows := b4Usage(t, s, scope)
	want := 0.0
	for _, row := range rows {
		want += row.Cost
	}
	var cost float64
	if err := s.pool.QueryRow(context.Background(), "SELECT sum(reserved_cost) FROM background_usage WHERE owner_id=$1", string(scope.OwnerID)).Scan(&cost); err != nil {
		t.Fatal(err)
	}
	if math.Abs(cost-want) > 1e-9 {
		t.Errorf("concurrent calls changed each other's settlement: budget=%g usage=%g", cost, want)
	}
}

func TestR1R7EquivalentInstantKeepsCurrentNotice(t *testing.T) {
	s, scope := testStore(t), owner()
	now := time.Now().Add(time.Second)
	notice := dueNotice(t, s, scope, now)
	err := pgx.BeginFunc(context.Background(), s.pool, func(tx pgx.Tx) error {
		item, err := getItem(context.Background(), tx, scope, notice.ThingID)
		if err != nil {
			return err
		}
		at, err := time.Parse(time.RFC3339, item.Triggers[0].NextAt)
		if err != nil {
			return err
		}
		item.Triggers[0].NextAt = at.In(time.FixedZone("Test", 2*3600)).Format(time.RFC3339)
		return saveItem(context.Background(), tx, scope, item)
	})
	if err != nil {
		t.Fatal(err)
	}
	ch := &fakeNotifyChannel{name: "test"}
	if err := s.DispatchNotices(context.Background(), now, []notify.Channel{ch}); err != nil {
		t.Fatal(err)
	}
	if len(ch.calls) != 1 {
		t.Error("same instant suppressed because its timezone spelling changed")
	}
}
