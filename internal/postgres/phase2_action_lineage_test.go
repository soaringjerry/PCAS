package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

const phase2ActionTitle = "DERIVED_ACTION_TITLE_731"
const phase2ActionNotes = "DERIVED_ACTION_NOTES_842"
const phase2ActionCheck = "DERIVED_ACTION_CHECK_953"
const phase2ActionOwnerNotes = "OWNER_INDEPENDENT_NOTES_164"
const phase2ActionOwnerCheck = "OWNER_INDEPENDENT_CHECK_275"

func phase2ActionEvidence(t *testing.T, name string, value any) {
	t.Helper()
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("phase2-action-lineage-%s=%s", name, body)
	if dir := os.Getenv("PCAS_PHASE2_ACTION_EVIDENCE_DIR"); dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name+".json"), append(body, '\n'), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

// This is an independently frozen public DeskTurn -> public requestRun ->
// fresh ManualRunPackage scenario. It does not call the delegate action.
func TestPhase2ActionLineageSecretaryOnlyFieldsDoNotReachManual(t *testing.T) {
	if os.Getenv("PCAS_TEST_DATABASE_URL") != "postgres://phase2_c:phase2-synthetic-only@127.0.0.1:33273/phase2_c?sslmode=disable" {
		t.Fatal("minimal repro requires the prescribed disposable loopback phase2_c database; no skip")
	}
	ctx := context.Background()
	store, scope, capture := phase2RTSetup(t)
	source, err := memory.NewService(store).Ingest(ctx, scope, memory.IngestRequest{Connector: "phase2-action-lineage-synthetic", ExternalID: "ha-tang-actions", ExternalVersion: "v1", Title: "合成海棠行动资料", Text: "合成海棠行动资料原始安排：标题 " + phase2ActionTitle + "；说明 " + phase2ActionNotes + "；步骤 " + phase2ActionCheck + "。", MediaType: "text/plain"})
	if err != nil {
		t.Fatal(err)
	}
	grant, err := store.SetSourceAuthorization(ctx, scope, memory.SourceAuthorizationRequest{RequestID: string(memory.NewID()), Source: source.Ref, Recipient: memory.Recipient{PrincipalID: "phase2-model", Role: "secretary"}, Purpose: memory.KnowledgePurpose, Scope: memory.HardScope{Kind: memory.UnscopedContextScope}})
	if err != nil || grant.Authorization.Recipient.Role != "secretary" {
		t.Fatalf("formal secretary-only source grant failed: %+v %v", grant, err)
	}
	policies, err := store.SourceAuthorizations(ctx, scope, source.ID)
	if err != nil || len(policies) != 1 || policies[0].Recipient.Role != "secretary" {
		t.Fatalf("secretary-only policy positive boundary missing: %+v %v", policies, err)
	}
	var claims int
	if err := store.pool.QueryRow(ctx, "SELECT count(*) FROM claims WHERE owner_id=$1", string(scope.OwnerID)).Scan(&claims); err != nil || claims != 0 {
		t.Fatalf("synthetic source must have no claims: %d %v", claims, err)
	}
	reply, err := json.Marshal(map[string]any{"reply": "合成模型已整理字段", "used": []string{"M1"}, "links": []string{}, "show": []string{}, "remember": false, "ask": nil, "actions": []any{map[string]any{"op": "create_task", "title": phase2ActionTitle, "notes": phase2ActionNotes}, map[string]any{"op": "add_steps", "ref": "N1", "steps": []string{phase2ActionCheck}}}})
	if err != nil {
		t.Fatal(err)
	}
	capture.mu.Lock()
	capture.Reply = string(reply)
	capture.mu.Unlock()
	request := workspace.DeskTurnRequest{RequestID: string(memory.NewID()), AgentID: "phase2-model", Text: "根据合成海棠行动资料创建待办，并把安排加入说明和步骤"}
	out, err := store.DeskTurn(ctx, scope, request)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Turn.Receipts) != 2 || out.Turn.Receipts[0].Status != "done" || out.Turn.Receipts[1].Status != "done" || out.Turn.Receipts[0].ThingID == nil {
		t.Fatalf("real secretary actions did not persist: %+v", out.Turn.Receipts)
	}
	if capture.count() != 1 {
		t.Fatalf("secretary actual model calls=%d want=1", capture.count())
	}
	secretaryBody := capture.request(t, 0)
	for _, marker := range []string{phase2ActionTitle, phase2ActionNotes, phase2ActionCheck} {
		if !bytes.Contains(secretaryBody, []byte(marker)) {
			t.Fatalf("secretary raw positive-control atom absent from actual HTTP body: %s", marker)
		}
	}
	attempts, err := store.ContextAttempts(ctx, scope, request.RequestID)
	if err != nil || len(attempts) != 1 || len(attempts[0].Manifest.Input) != 1 || attempts[0].Manifest.Input[0].Ref != source.Ref {
		t.Fatalf("secretary exact source Input positive control missing: %+v %v", attempts, err)
	}
	thingID := *out.Turn.Receipts[0].ThingID
	state, err := store.Snapshot(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	var created workspace.Item
	for _, item := range state.Tasks {
		if item.ID == thingID {
			created = item
		}
	}
	if created.Title != phase2ActionTitle || created.Notes != phase2ActionNotes || len(created.Checklist) != 1 || created.Checklist[0].Text != phase2ActionCheck {
		t.Fatalf("actual owner state did not contain all derived fields: %+v", created)
	}
	state = workspaceCommand(t, store, scope, workspace.Command{Type: "setNotes", ID: thingID, Text: created.Notes + "\n" + phase2ActionOwnerNotes})
	state = workspaceCommand(t, store, scope, workspace.Command{Type: "addCheck", ID: thingID, Text: phase2ActionOwnerCheck})
	ownerState := state
	manualID := string(memory.NewID())
	state = workspaceCommand(t, store, scope, workspace.Command{Type: "requestRun", ID: manualID, ThingID: thingID, AgentID: "manual", Kind: "ask", Prompt: "查看这件事的可用内容，给一条普通建议", ManualRecipient: &memory.Recipient{Provider: "phase2-model"}})
	var selected workspace.Run
	for _, run := range state.Runs {
		if run.ID == manualID {
			selected = run
		}
	}
	if selected.ContextTask == nil || selected.ContextTask.Recipient.Role != "manual" || selected.ContextTask.Recipient.Provider != "phase2-model" || selected.Status != "waiting" {
		t.Fatalf("manual trusted target precondition missing: %+v", selected)
	}
	pkg, packageErr := store.ManualRunPackage(ctx, scope, manualID)
	phase2ActionEvidence(t, "minimal-repro", map[string]any{"source": source.Ref, "source_policies": policies, "claims": claims, "secretary_request": request, "secretary_actual_http_body": json.RawMessage(secretaryBody), "secretary_attempt": attempts[0], "secretary_model_response": json.RawMessage(reply), "secretary_receipts": out.Turn.Receipts, "owner_state_after_independent_edits": ownerState, "manual_run": selected, "manual_package": pkg, "manual_package_error": packageErr != nil, "third_party_external_receipt": "unknown"})
	if packageErr != nil {
		t.Fatalf("manual could not produce safe owner-only content: %v", packageErr)
	}
	body, ok := pkg["package"].(string)
	if !ok {
		t.Fatal("actual manual package body missing")
	}
	for _, marker := range []string{phase2ActionTitle, phase2ActionNotes, phase2ActionCheck} {
		if strings.Contains(body, marker) {
			t.Errorf("secretary-only source-derived field reached unauthorized manual package: %s", marker)
		}
	}
	for _, marker := range []string{phase2ActionOwnerNotes, phase2ActionOwnerCheck} {
		if !strings.Contains(body, marker) {
			t.Errorf("independent owner text lost from manual package: %s", marker)
		}
	}
	attempt, ok := pkg["attempt"].(memory.ContextAttempt)
	if !ok {
		t.Fatal("actual manual package attempt missing")
	}
	for _, input := range attempt.Manifest.Input {
		if input.Ref == source.Ref {
			t.Error("unauthorized manual manifest lists secretary-only source as directly supplied")
		}
	}
	for _, indirect := range attempt.Manifest.IndirectDependencies {
		if indirect.Ref == source.Ref {
			t.Error("unauthorized manual manifest lists secretary-only source as authorized indirect input")
		}
	}
	if capture.count() != 1 {
		t.Error("manual package unexpectedly dispatched to a model")
	}
}
