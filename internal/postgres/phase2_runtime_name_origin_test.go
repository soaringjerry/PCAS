package postgres

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func phase2RTNameItem(t *testing.T, state workspace.State, id string) workspace.Item {
	t.Helper()
	for _, item := range append(append([]workspace.Item{}, state.Tasks...), state.Ideas...) {
		if item.ID == id {
			return item
		}
	}
	t.Fatal("actual named item missing from owner current state", id)
	return workspace.Item{}
}

func phase2RTNameCreate(t *testing.T, s *Store, scope memory.Scope, capture *phase2RTCapture, source memory.Ref, kind string) (workspace.Item, memory.ID) {
	t.Helper()
	gold := phase2RTGoldRead(t)
	capture.mu.Lock()
	capture.Reply = string(asJSON(map[string]any{"reply": "NAME-CREATE-RECEIPT-680", "used": []any{}, "links": []any{}, "show": []any{}, "remember": false, "ask": nil, "actions": []any{map[string]any{"op": "create_" + kind, "title": "源名称 " + gold.Records[0].Atoms[0]}}}))
	capture.mu.Unlock()
	request := workspace.DeskTurnRequest{RequestID: string(memory.NewID()), AgentID: "phase2-model", Text: gold.Cases[0].Query + "请按资料创建一个" + kind}
	if strings.Contains(request.Text, gold.Records[0].Atoms[0]) {
		t.Fatal("Name owner query must not contain source atom")
	}
	before := capture.count()
	out, err := s.DeskTurn(context.Background(), scope, request)
	if err != nil || capture.count() != before+1 {
		t.Fatal("real secretary Name creation did not call model", err, capture.count()-before)
	}
	payload := capture.request(t, before)
	phase2RTAtoms(t, payload, gold.Cases[0].Required, true)
	phase2RTSnapshotForSource(t, s, scope, source, "secretary", request.RequestID, payload)
	var receipt *workspace.DeskReceipt
	for i := range out.Turn.Receipts {
		if out.Turn.Receipts[i].Op == "create_"+kind {
			if receipt != nil {
				t.Fatal("ambiguous real create receipts")
			}
			receipt = &out.Turn.Receipts[i]
		}
	}
	if receipt == nil || receipt.Status != "done" || receipt.ThingID == nil || receipt.ActionID == nil || !memory.ID(*receipt.ActionID).Valid() {
		t.Fatal("source-derived Name has no actual successful action receipt", out.Turn.Receipts)
	}
	id := *receipt.ThingID
	var actualTurn string
	var originalTask []byte
	if err := s.pool.QueryRow(context.Background(), "SELECT turn_id::text,context_task FROM action_log WHERE owner_id=$1 AND id=$2 AND source='desk'", string(scope.OwnerID), *receipt.ActionID).Scan(&actualTurn, &originalTask); err != nil || actualTurn != out.Turn.ID || len(originalTask) == 0 {
		t.Fatal("Name create origin not a real committed secretary action", err, actualTurn)
	}
	item := phase2RTNameItem(t, out.State, id)
	canonical := phase2RTNameCanonical(t, s, scope, id)
	for _, current := range []workspace.Item{item, canonical} {
		if current.Kind != kind || current.Title != "源名称 "+gold.Records[0].Atoms[0] || current.Name != current.Title {
			t.Fatal("source-derived canonical Title and Name positive control missing", current)
		}
	}
	phase2RTEvidence(t, "name-real-create", map[string]any{"request": request, "payload": json.RawMessage(payload), "source": source, "response": out, "canonical": canonical, "action_id": *receipt.ActionID, "original_task": json.RawMessage(originalTask)})
	return item, memory.ID(*receipt.ActionID)
}

func phase2RTNameCanonical(t *testing.T, s *Store, scope memory.Scope, id string) workspace.Item {
	t.Helper()
	var body []byte
	if err := s.pool.QueryRow(context.Background(), "SELECT document FROM work_items WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), id).Scan(&body); err != nil {
		t.Fatal(err)
	}
	var item workspace.Item
	if err := json.Unmarshal(body, &item); err != nil {
		t.Fatal(err)
	}
	return item
}

func phase2RTNameCleared(t *testing.T, s *Store, scope memory.Scope, id, atom, ownerTitle string) {
	t.Helper()
	state, err := s.Snapshot(context.Background(), scope)
	if err != nil {
		t.Fatal(err)
	}
	exported := phase2RTExportState(t, s, scope)
	views := map[string]workspace.Item{"canonical": phase2RTNameCanonical(t, s, scope, id), "snapshot": phase2RTNameItem(t, state, id), "export": phase2RTNameItem(t, exported, id)}
	for name, item := range views {
		if strings.Contains(string(asJSON(item)), atom) {
			t.Errorf("%s current item retained invalid source Name/body after owner renamed Title: %s", name, asJSON(item))
		}
		if ownerTitle != "" && item.Title != ownerTitle {
			t.Errorf("%s lost independent owner Title on source invalidation: %q want %q", name, item.Title, ownerTitle)
		}
	}
	phase2RTEvidence(t, "name-current-views", views)
}

func phase2RTNameDeliverOrdinary(t *testing.T, s *Store, scope memory.Scope, capture *phase2RTCapture, id, atom string) {
	t.Helper()
	ctx := context.Background()
	const prompt = "NAME-OWNER-REQUEST-684 请给普通工作建议"
	manualID := string(memory.NewID())
	st := workspaceCommand(t, s, scope, workspace.Command{Type: "requestRun", ID: manualID, ThingID: id, AgentID: "manual", Kind: "ask", Prompt: prompt, ManualRecipient: &memory.Recipient{Provider: "phase2-model"}})
	var manual workspace.Run
	for _, run := range st.Runs {
		if run.ID == manualID {
			manual = run
		}
	}
	if manual.ID != manualID || manual.Status != "waiting" {
		t.Fatal("new ordinary manual request not actually prepared", manual)
	}
	before := capture.count()
	pkg := phase2RTGetPackage(t, s, scope, manual)
	if strings.Contains(pkg.Text, atom) || !strings.Contains(pkg.Text, prompt) || capture.count() != before {
		t.Error("actual manual delivery leaked old Name or lost ordinary owner work", pkg.Text, capture.count()-before)
	}
	capture.mu.Lock()
	capture.Reply = string(asJSON(map[string]any{"reply": "NAME-ORDINARY-DEPUTY-RESULT-685", "used": []any{}}))
	capture.mu.Unlock()
	deputyID := string(memory.NewID())
	workspaceCommand(t, s, scope, workspace.Command{Type: "requestRun", ID: deputyID, ThingID: id, AgentID: "phase2-model", Kind: "ask", Prompt: prompt})
	if err := s.runAgentOnce(ctx); err != nil {
		t.Fatal(err)
	}
	finished := phase2RTWaitRun(t, s, scope, deputyID)
	if finished.Status != "done" || finished.Output != "NAME-ORDINARY-DEPUTY-RESULT-685" || capture.count() != before+1 {
		t.Fatal("invalid Name blocked legitimate ordinary deputy work or no actual HTTP", finished, capture.count()-before)
	}
	payload := capture.request(t, before)
	if strings.Contains(string(payload), atom) || !strings.Contains(string(payload), "NAME-OWNER-REQUEST-684") {
		t.Error("actual ordinary deputy HTTP leaked Name or lost owner question", string(payload))
	}
	phase2RTEvidence(t, "name-real-ordinary-delivery", map[string]any{"manual_package": pkg, "deputy_run": finished, "deputy_http": json.RawMessage(payload)})
}

func TestPhase2RuntimeCanonicalNameOriginSurvivesOwnerTitleRewrite(t *testing.T) {
	var gold struct {
		Cases []struct {
			ID, Kind   string
			OwnerTitle string `json:"owner_title"`
		}
	}
	phase2RTReadJSON(t, "name-origin-sequences.json", &gold)
	if len(gold.Cases) != 2 {
		t.Fatal("Name two-kind independent gold missing")
	}
	for _, tc := range gold.Cases {
		t.Run(tc.Kind, func(t *testing.T) {
			s, scope, capture := phase2RTSetup(t)
			ctx := context.Background()
			source := phase2RTSource(t, s, scope)
			secretary, _ := phase2RTAuthorize(t, s, scope, source.Ref, "phase2-model", "secretary", phase2RTUnscoped())
			item, createID := phase2RTNameCreate(t, s, scope, capture, source.Ref, tc.Kind)
			atom := phase2RTGoldRead(t).Records[0].Atoms[0]
			_, renameID := approvedUndoCommand(t, s, scope, workspace.Command{Type: "renameThing", ID: item.ID, Title: tc.OwnerTitle})
			renamed := phase2RTNameCanonical(t, s, scope, item.ID)
			if renamed.Title != tc.OwnerTitle || !strings.Contains(renamed.Name, atom) {
				t.Fatal("owner Title rewrite did not isolate unchanged source Name", renamed)
			}
			var changes []byte
			if err := s.pool.QueryRow(ctx, "SELECT changes FROM action_log WHERE owner_id=$1 AND id=$2 AND source='command'", string(scope.OwnerID), renameID).Scan(&changes); err != nil {
				t.Fatal("real owner rename action ID missing", err, renameID)
			}
			var old []actionChange
			if err := json.Unmarshal(changes, &old); err != nil {
				t.Fatal(err)
			}
			found := false
			for _, change := range old {
				if change.Table == "work_items" && change.ID == item.ID {
					var before workspace.Item
					if err := json.Unmarshal(change.Before, &before); err != nil {
						t.Fatal(err)
					}
					found = before.Name == item.Name && before.Title == item.Title
				}
			}
			if !found {
				t.Fatal("actual owner rename undo snapshot lacked source Title/Name positive control", string(changes))
			}
			phase2RTEvidence(t, "name-owner-rename", map[string]any{"create_action_id": createID, "owner_rename_action_id": renameID, "canonical": renamed, "changes": json.RawMessage(changes)})
			phase2RTUpdatePolicy(t, s, scope, source.Ref, secretary, true)
			phase2RTNameCleared(t, s, scope, item.ID, atom, tc.OwnerTitle)
			state, err := s.Snapshot(ctx, scope)
			if err != nil {
				t.Fatal(err)
			}
			_, err = s.Execute(ctx, scope, workspace.Command{Type: "undoAction", ID: renameID, RequestID: string(memory.NewID()), ExpectedRevision: state.Revision})
			if err != nil {
				t.Fatal("eligible real owner rename Undo must succeed after proven causal source scrub", err)
			}
			var undone bool
			if err := s.pool.QueryRow(ctx, "SELECT undone_at IS NOT NULL FROM action_log WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), renameID).Scan(&undone); err != nil || !undone {
				t.Fatal("real owner rename Undo not committed", err, undone)
			}
			phase2RTNameCleared(t, s, scope, item.ID, atom, "")
			phase2RTNameDeliverOrdinary(t, s, scope, capture, item.ID, atom)
		})
	}
}

func TestPhase2RuntimeOwnerPromptDoesNotInheritGeneratedPromptOrigins(t *testing.T) {
	s, scope, capture := phase2RTSetup(t)
	ctx := context.Background()
	source := phase2RTSource(t, s, scope)
	secretary, _ := phase2RTAuthorize(t, s, scope, source.Ref, "phase2-model", "secretary", phase2RTUnscoped())
	deputy, _ := phase2RTAuthorize(t, s, scope, source.Ref, "phase2-model", "deputy", phase2RTUnscoped())
	item, createID := phase2RTNameCreate(t, s, scope, capture, source.Ref, "task")
	const prompt = "OWNER-INDEPENDENT-PROMPT-683 请整理这件事"
	runID := string(memory.NewID())
	st := workspaceCommand(t, s, scope, workspace.Command{Type: "requestRun", ID: runID, ThingID: item.ID, AgentID: "phase2-model", Kind: "ask", Prompt: prompt})
	var queued workspace.Run
	for _, run := range st.Runs {
		if run.ID == runID {
			queued = run
		}
	}
	origins := phase2RTDeskActionIDs(t, queued)
	containsCreate := false
	for _, origin := range origins {
		containsCreate = containsCreate || origin == createID
	}
	if !containsCreate || queued.Prompt != prompt || len(phase2RTPromptOriginIDs(t, queued)) != 0 {
		t.Fatal("owner request distinction positive control missing: genuine general origins but no generated Prompt origins", queued, origins)
	}
	phase2RTUpdatePolicy(t, s, scope, source.Ref, secretary, true)
	policies, err := s.SourceAuthorizations(ctx, scope, source.ID)
	if err != nil {
		t.Fatal(err)
	}
	allow := false
	for _, policy := range policies {
		if policy.ID == deputy.Authorization.ID {
			allow = !policy.Revoked && policy.Revision == deputy.Authorization.Revision && policy.Recipient == deputy.Authorization.Recipient
		}
	}
	if !allow {
		t.Fatal("owner Prompt control accidentally revoked independent target role")
	}
	reader := phase2RTTask(t, s, scope, "phase2-model", "deputy", phase2RTUnscoped())
	if raw, err := s.GetSource(ctx, reader, source.ID, source.Version); err != nil || raw.Source.Text != phase2RTGoldRead(t).Records[0].Text {
		t.Fatal("independent deputy source allow positive control absent", err)
	}
	state, err := s.Snapshot(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	exported := phase2RTExportState(t, s, scope)
	for name, view := range map[string]workspace.State{"snapshot": state, "export": exported} {
		found := false
		for _, run := range view.Runs {
			if run.ID == runID {
				found = true
				if run.Prompt != prompt || len(phase2RTPromptOriginIDs(t, run)) != 0 {
					t.Error("inherited general origins scrubbed independently written owner Prompt", name, run)
				}
			}
		}
		if !found {
			t.Error("owner Prompt vanished with invalidated inherited fields", name, runID)
		}
	}
	var canonical []byte
	if err := s.pool.QueryRow(ctx, "SELECT document FROM agent_runs WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), runID).Scan(&canonical); err != nil {
		t.Fatal(err)
	}
	var current workspace.Run
	if err := json.Unmarshal(canonical, &current); err != nil || current.Prompt != prompt || len(phase2RTPromptOriginIDs(t, current)) != 0 {
		t.Error("canonical owner Prompt was scrubbed or falsely labelled generated", err, current)
	}
	phase2RTEvidence(t, "name-independent-owner-prompt", map[string]any{"create_action_id": createID, "before": queued, "canonical_after": current, "snapshot": state, "export": exported, "target_policy_still_allow": deputy.Authorization})
}

// Formal owner rename is independent only when the actual new title does not
// reuse known derived content. These fixed copying examples inspect durable
// origins and real delivery, not a second implementation of text matching.
func TestPhase2RuntimeOwnerRenameCannotLaunderKnownSourceText(t *testing.T) {
	var gold struct {
		Cases []struct{ Kind string }
		Reuse []struct {
			ID        string
			Title     string `json:"new_title"`
			Forbidden string `json:"forbidden_fragment"`
		} `json:"rename_reuse_cases"`
	}
	phase2RTReadJSON(t, "name-origin-sequences.json", &gold)
	if len(gold.Cases) != 2 || len(gold.Reuse) != 4 {
		t.Fatal("independent two-kind/four-target rename reuse gold missing")
	}
	for _, kind := range gold.Cases {
		for _, reuse := range gold.Reuse {
			t.Run(kind.Kind+"/"+reuse.ID, func(t *testing.T) {
				s, scope, capture := phase2RTSetup(t)
				ctx := context.Background()
				source := phase2RTSource(t, s, scope)
				secretary, _ := phase2RTAuthorize(t, s, scope, source.Ref, "phase2-model", "secretary", phase2RTUnscoped())
				item, createID := phase2RTNameCreate(t, s, scope, capture, source.Ref, kind.Kind)
				if reuse.Forbidden != "LANTERN-482" || !strings.Contains(item.Title, reuse.Forbidden) || !strings.Contains(reuse.Title, reuse.Forbidden) {
					t.Fatal("fixed actual source fragment reuse positive control missing", item.Title, reuse)
				}
				_, renameID := approvedUndoCommand(t, s, scope, workspace.Command{Type: "renameThing", ID: item.ID, Title: reuse.Title})
				renamed := phase2RTNameCanonical(t, s, scope, item.ID)
				if renamed.Title != reuse.Title || renamed.Name != item.Name {
					t.Fatal("actual owner rename target did not persist or independently changed Name", renamed, reuse.Title)
				}
				var auditSource string
				var changes []byte
				if err := s.pool.QueryRow(ctx, "SELECT source,changes FROM action_log WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), renameID).Scan(&auditSource, &changes); err != nil || auditSource != "command" || len(changes) == 0 {
					t.Fatal("owner reuse rename lacks real formal command action audit", err, auditSource, renameID)
				}
				var blocks []artifactBlock
				if err := s.pool.QueryRow(ctx, "SELECT blocks FROM artifact_fields WHERE owner_id=$1 AND thing_id=$2 AND field='title'", string(scope.OwnerID), item.ID).Scan(&blocks); err != nil {
					t.Fatal(err)
				}
				knownOrigin := false
				for _, block := range blocks {
					for _, origin := range block.DeskActions {
						knownOrigin = knownOrigin || origin == string(createID)
					}
				}
				if !knownOrigin {
					t.Error("formal owner rename washed known derived title origin despite actual source text reuse", reuse.ID, createID, blocks)
				}
				phase2RTEvidence(t, "name-reuse-actual-owner-rename", map[string]any{"case": reuse, "create_action_id": createID, "owner_action_id": renameID, "canonical": renamed, "actual_title_blocks": blocks, "changes": json.RawMessage(changes)})
				phase2RTUpdatePolicy(t, s, scope, source.Ref, secretary, true)
				// Partial-PIN tests check the actual LANTERN-482 fragment in
				// every view/body; checking only the whole PIN could miss it.
				phase2RTNameCleared(t, s, scope, item.ID, reuse.Forbidden, "")
				phase2RTNameDeliverOrdinary(t, s, scope, capture, item.ID, reuse.Forbidden)
			})
		}
	}
}
