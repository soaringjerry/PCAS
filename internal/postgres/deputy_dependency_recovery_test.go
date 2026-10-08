package postgres

import (
	"context"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
	"math"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// Exercise the real HTTP provider while its response is withheld. Corrections
// must not erase a paid answer; loss of access must dominate any correction.
func TestDeputyCompletionRechecksDependencyAccess(t *testing.T) {
	for _, change := range []string{"correct", "source-correct", "revoke", "correct-and-revoke", "correct-and-delete", "correct-and-full-delete", "delete"} {
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
			case "correct", "correct-and-revoke", "correct-and-delete", "correct-and-full-delete":
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
			if change == "delete" || change == "correct-and-full-delete" {
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
			if !found && change != "delete" && change != "correct-and-full-delete" {
				t.Error("run disappeared")
			}
			if len(st.Docs) != 0 {
				t.Error("changed result automatically adopted")
			}
			// C5 permits the optional check after unrelated correction. Its
			// model deadline can expire before A9's cancellation-independent
			// accounting finishes; wait for that bounded settlement, not a
			// fixed sleep or a weaker cost comparison.
			if change == "correct" || change == "source-correct" {
				deadline := time.Now().Add(21 * time.Second)
				for {
					var ledger float64
					if err := s.pool.QueryRow(context.Background(), `SELECT coalesce(sum(cost),0) FROM model_usage WHERE owner_id=$1`, scope.OwnerID).Scan(&ledger); err != nil {
						t.Fatal(err)
					}
					st, err = s.Snapshot(context.Background(), scope)
					if err != nil {
						t.Fatal(err)
					}
					if math.Abs(st.BudgetUsage-ledger) <= 1e-9 || time.Now().After(deadline) {
						break
					}
					time.Sleep(10 * time.Millisecond)
				}
			}
			rows := b4Usage(t, s, scope)
			wantMax := 1
			if change == "correct" || change == "source-correct" {
				wantMax = 2
			}
			if len(rows) < 1 || len(rows) > wantMax || rows[0].Cost <= 0 {
				t.Errorf("returned usage lost: %+v", rows)
			}
			var paid float64
			for _, row := range rows {
				paid += row.Cost
				if row.RunID == nil || *row.RunID != run.ID || !oneOf(row.Purpose, "deputy", "selfcheck") {
					t.Errorf("uncorrelated returned usage: %+v", row)
				}
			}
			if math.Abs(st.BudgetUsage-paid) > 1e-9 {
				t.Errorf("completion did not settle budget after %s: budget=%g cost=%g", change, st.BudgetUsage, paid)
			}
		})
	}
}

func TestDeputyContinuationUsesCurrentDocuments(t *testing.T) {
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

func TestDeputyDocumentsRespectPermissionsAndLength(t *testing.T) {
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
