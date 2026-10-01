package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	dsn := os.Getenv("PCAS_TEST_DATABASE_URL")
	u, parseErr := url.Parse(dsn)
	if dsn == "" || parseErr != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		t.Fatal("requires explicitly configured synthetic PCAS_TEST_DATABASE_URL; no skip")
	}
	host, database := u.Hostname(), strings.TrimPrefix(u.Path, "/")
	ip := net.ParseIP(host)
	if (host != "localhost" && (ip == nil || !ip.IsLoopback())) || strings.Contains(database, "/") || !(strings.HasSuffix(database, "_test") || strings.HasPrefix(database, "phase2_")) {
		t.Fatal("test database must use loopback and an explicit _test suffix or phase2_ prefix")
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

// The matrix below uses public Store commands for every write of source text or
// derived business text. SQL reads inspect provenance; the expiry case only
// advances clocks on this test's own audit rows, never manufactures an origin.
type phase2ActionMatrix struct {
	t                  *testing.T
	s                  *Store
	scope              memory.Scope
	capture            *phase2RTCapture
	source             memory.IngestResult
	secretary          memory.SourceAuthorizationResult
	hard               memory.HardScope
	derived            []string
	owner              string
	runner             bool
	output             string
	secretaryPrincipal string
	workerCancel       context.CancelFunc
	workerDone         chan error
	originIDs          map[string][]string
}

func phase2ActionMatrixNew(t *testing.T) *phase2ActionMatrix {
	t.Helper()
	dsn := os.Getenv("PCAS_TEST_DATABASE_URL")
	u, err := url.Parse(dsn)
	if dsn == "" || err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		t.Fatal("explicit synthetic database required; no skip")
	}
	ip, database := net.ParseIP(u.Hostname()), strings.TrimPrefix(u.Path, "/")
	if (u.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback())) || strings.Contains(database, "/") || !(strings.HasSuffix(database, "_test") || strings.HasPrefix(database, "phase2_")) {
		t.Fatal("loopback synthetic database required")
	}
	s, scope, capture := phase2RTSetup(t)
	f := &phase2ActionMatrix{t: t, s: s, scope: scope, capture: capture, hard: phase2RTUnscoped(), owner: "OWNER independent paragraph cobalt 602", secretaryPrincipal: "phase2-model", originIDs: map[string][]string{}}
	f.derived = []string{"ACTION title scarlet 731", "ACTION notes amber 842", "ACTION step jade 953", "ACTION owed violet 364", "ACTION waiting silver 475", "ACTION condition coral 586"}
	f.source, err = memory.NewService(s).Ingest(context.Background(), scope, memory.IngestRequest{Connector: "phase2-action-matrix-synthetic", ExternalID: "raw-material", ExternalVersion: "v1", Title: "合成海棠行动资料", Text: "合成海棠行动资料：" + strings.Join(f.derived, "；"), MediaType: "text/plain"})
	if err != nil {
		t.Fatal(err)
	}
	var claims int
	if err := s.pool.QueryRow(context.Background(), "SELECT count(*) FROM claims WHERE owner_id=$1", string(scope.OwnerID)).Scan(&claims); err != nil || claims != 0 {
		t.Fatalf("source fixture created claims: %d %v", claims, err)
	}
	return f
}

func (f *phase2ActionMatrix) evidence(name string, value any) {
	f.t.Helper()
	phase2ActionEvidence(f.t, strings.ReplaceAll(f.t.Name(), "/", "__")+"-"+name, value)
}
func (f *phase2ActionMatrix) cmd(c workspace.Command) workspace.State {
	f.t.Helper()
	return workspaceCommand(f.t, f.s, f.scope, c)
}
func (f *phase2ActionMatrix) grant(principal, role string, hard memory.HardScope) memory.SourceAuthorizationResult {
	f.t.Helper()
	recipient := memory.Recipient{PrincipalID: principal, Role: role}
	if principal == "manual" {
		recipient.Provider = "phase2-model"
	}
	var manual *memory.Recipient
	if principal == "manual" {
		manual = &recipient
	}
	canonical, err := f.s.ContextRecipient(context.Background(), f.scope, principal, role, manual)
	if err != nil {
		f.t.Fatal(err)
	}
	policies, err := f.s.SourceAuthorizations(context.Background(), f.scope, f.source.ID)
	if err != nil {
		f.t.Fatal(err)
	}
	request := memory.SourceAuthorizationRequest{RequestID: string(memory.NewID()), Source: f.source.Ref, Recipient: recipient, Purpose: memory.KnowledgePurpose, Scope: hard}
	for _, policy := range policies {
		if policy.Recipient == canonical && policy.Purpose == request.Purpose && policy.Scope == hard {
			request.PolicyID = policy.ID
			request.ExpectedPolicyRevision = policy.Revision
		}
	}
	out, err := f.s.SetSourceAuthorization(context.Background(), f.scope, request)
	if err != nil {
		f.t.Fatal(err)
	}
	if !out.Authorization.Recipient.Valid() || out.Authorization.SourceID != f.source.ID || out.Authorization.Recipient.Role != role {
		f.t.Fatalf("invalid actual authorization receipt: %+v", out)
	}
	f.evidence("grant-"+role+"-"+string(memory.NewID()), out)
	return out
}
func (f *phase2ActionMatrix) grantAll() {
	f.secretary = f.grant(f.secretaryPrincipal, "secretary", f.hard)
	f.grant("phase2-model", "deputy", f.hard)
	f.grant("manual", "manual", f.hard)
}

func (f *phase2ActionMatrix) sourceScope(assignments []memory.SourceScopeAssignment) {
	f.t.Helper()
	prior, err := f.s.SourceScope(context.Background(), f.scope, f.source.ID)
	if err != nil {
		f.t.Fatal(err)
	}
	out, err := f.s.SetSourceScope(context.Background(), f.scope, memory.SourceScopeRequest{RequestID: string(memory.NewID()), Source: f.source.Ref, ExpectedRevision: prior.Revision, Assignments: assignments})
	if err != nil || out.Revision <= prior.Revision {
		f.t.Fatalf("formal scope mutation failed: %+v %v", out, err)
	}
	f.evidence("scope-"+string(memory.NewID()), out)
}
func (f *phase2ActionMatrix) reply(actions []any, used []string, text string) {
	f.capture.mu.Lock()
	f.capture.Reply = string(asJSON(map[string]any{"reply": text, "used": used, "actions": actions, "links": []string{}, "show": []string{}, "remember": false, "ask": nil}))
	f.capture.mu.Unlock()
}
func (f *phase2ActionMatrix) item(id string) workspace.Item {
	f.t.Helper()
	state, err := f.s.Snapshot(context.Background(), f.scope)
	if err != nil {
		f.t.Fatal(err)
	}
	for _, items := range [][]workspace.Item{state.Tasks, state.Ideas, state.Projects} {
		for _, item := range items {
			if item.ID == id {
				return item
			}
		}
	}
	f.t.Fatalf("actual item %s missing", id)
	return workspace.Item{}
}
func (f *phase2ActionMatrix) newOwner(kind, project string) string {
	id := string(memory.NewID())
	f.cmd(workspace.Command{Type: map[string]string{"task": "addTask", "idea": "addIdea", "project": "addProject"}[kind], ID: id, Title: "普通独立工作事项", Name: "普通独立项目", ProjectID: project})
	f.cmd(workspace.Command{Type: "setNotes", ID: id, Text: f.owner})
	return id
}
func (f *phase2ActionMatrix) generation(thing string, actions []any, used []string, expected []string) workspace.DeskTurnResponse {
	f.t.Helper()
	f.reply(actions, used, "合成动作回执")
	before := f.capture.count()
	req := workspace.DeskTurnRequest{RequestID: string(memory.NewID()), AgentID: f.secretaryPrincipal, Text: "请根据合成海棠行动资料整理这件事"}
	if thing != "" {
		req.ThingID = &thing
	}
	out, err := f.s.DeskTurn(context.Background(), f.scope, req)
	if err != nil {
		f.t.Fatal(err)
	}
	if f.capture.count() != before+1 {
		f.t.Fatal("generation must make exactly one actual HTTP request")
	}
	body := f.capture.request(f.t, before)
	for _, atom := range expected {
		if !bytes.Contains(body, []byte(atom)) {
			f.t.Errorf("secretary actual HTTP positive control lacks %q", atom)
		}
	}
	if len(out.Turn.Receipts) != len(actions) {
		f.t.Fatalf("action count %d want %d: %+v", len(out.Turn.Receipts), len(actions), out.Turn.Receipts)
	}
	for _, receipt := range out.Turn.Receipts {
		if receipt.Status != "done" || receipt.ActionID == nil {
			f.t.Fatalf("actual model action not committed: %+v", receipt)
		}
	}
	attempts, err := f.s.ContextAttempts(context.Background(), f.scope, req.RequestID)
	if err != nil || len(attempts) != 1 {
		f.t.Fatalf("generation attempt missing: %+v %v", attempts, err)
	}
	found := false
	for _, input := range attempts[0].Manifest.Input {
		if input.Ref == f.source.Ref {
			found = true
		}
	}
	if !found {
		f.t.Error("generation actual exact source Input absent")
	}
	if len(used) == 0 && len(attempts[0].Manifest.Used) != 0 {
		f.t.Error("Used=[] fixture unexpectedly has product Used records")
	}
	f.evidence("generation-"+req.RequestID, map[string]any{"request": req, "actual_http": json.RawMessage(body), "response": out, "attempt": attempts[0]})
	f.origins(out)
	return out
}
func (f *phase2ActionMatrix) origins(out workspace.DeskTurnResponse) {
	f.t.Helper()
	for _, receipt := range out.Turn.Receipts {
		var blocks []byte
		if receipt.ThingID == nil {
			f.t.Fatal("model write receipt missing actual thing ID")
		}
		f.originIDs[*receipt.ThingID] = append(f.originIDs[*receipt.ThingID], *receipt.ActionID)
		if err := f.s.pool.QueryRow(context.Background(), "SELECT coalesce(jsonb_agg(jsonb_build_object('field',field,'blocks',blocks)),'[]'::jsonb) FROM artifact_fields WHERE owner_id=$1 AND thing_id=$2", string(f.scope.OwnerID), *receipt.ThingID).Scan(&blocks); err != nil {
			f.t.Fatal(err)
		}
		if !bytes.Contains(blocks, []byte(*receipt.ActionID)) {
			f.t.Errorf("actual action ID %s missing from field origins: %s", *receipt.ActionID, blocks)
		}
		var task []byte
		var stale bool
		if err := f.s.pool.QueryRow(context.Background(), "SELECT context_task,context_stale FROM action_log WHERE owner_id=$1 AND id=$2", string(f.scope.OwnerID), *receipt.ActionID).Scan(&task, &stale); err != nil {
			f.t.Fatal(err)
		}
		if len(task) == 0 || string(task) == "null" || stale {
			f.t.Error("live actual action lacks server Task origin")
		}
		var deps int
		if err := f.s.pool.QueryRow(context.Background(), "SELECT count(*) FROM context_artifact_dependencies WHERE owner_id=$1 AND parent_kind='artifact' AND parent_id=$2 AND parent_version=1 AND dependency_id=$3 AND dependency_version=$4 AND dependency_kind='source'", string(f.scope.OwnerID), *receipt.ActionID, string(f.source.ID), f.source.Version).Scan(&deps); err != nil {
			f.t.Fatal(err)
		}
		if deps == 0 {
			f.t.Error("actual action has no durable exact source dependency")
		}
		f.evidence("origin-"+*receipt.ActionID, map[string]any{"blocks": json.RawMessage(blocks), "context_task": json.RawMessage(task), "context_stale": stale, "exact_source_dependencies": deps})
	}
}
func (f *phase2ActionMatrix) create(used []string) string {
	out := f.generation("", []any{map[string]any{"op": "create_task", "title": f.derived[0], "notes": f.derived[1], "owedTo": f.derived[3], "waitingFor": f.derived[4]}, map[string]any{"op": "add_steps", "ref": "N1", "steps": []string{f.derived[2]}}}, used, f.derived)
	id := *out.Turn.Receipts[0].ThingID
	item := f.item(id)
	if item.Title != f.derived[0] || item.Notes != f.derived[1] || len(item.Checklist) != 1 || item.Checklist[0].Text != f.derived[2] || item.OwedTo == nil || item.OwedTo.Who != f.derived[3] || item.WaitingFor != f.derived[4] {
		f.t.Fatalf("model-generated fields not actually persisted: %+v", item)
	}
	f.cmd(workspace.Command{Type: "setNotes", ID: id, Text: item.Notes + "\n" + f.owner})
	f.cmd(workspace.Command{Type: "addCheck", ID: id, Text: "OWNER independent step bronze 697"})
	return id
}
func (f *phase2ActionMatrix) manifest(manifest memory.ContextManifest, allow bool, direct *bool) {
	f.t.Helper()
	input, indirect := false, false
	for _, i := range manifest.Input {
		if i.Ref == f.source.Ref {
			input = true
		}
	}
	for _, d := range manifest.IndirectDependencies {
		if d.Ref == f.source.Ref {
			indirect = true
			if d.Authorization == nil || d.Authorization.PolicyID == "" || d.Authorization.Revision <= 0 {
				f.t.Error("indirect source missing policy stamp")
			} else if allow {
				policies, err := f.s.SourceAuthorizations(context.Background(), f.scope, f.source.ID)
				if err != nil {
					f.t.Fatal(err)
				}
				matched := false
				for _, policy := range policies {
					if !policy.Revoked && policy.Recipient == manifest.Recipient && policy.Purpose == manifest.Purpose && policy.Scope == manifest.Scope && policy.ID == d.Authorization.PolicyID && policy.Revision == d.Authorization.Revision {
						matched = true
					}
				}
				if !matched {
					f.t.Error("Indirect stamp is not the current actual recipient's exact source policy")
				}
				currentScope, err := f.s.SourceScope(context.Background(), f.scope, f.source.ID)
				if err != nil || d.ScopeRevision != currentScope.Revision {
					f.t.Errorf("Indirect scope revision=%d current=%d: %v", d.ScopeRevision, currentScope.Revision, err)
				}
			}
		}
	}
	if allow && !indirect {
		f.t.Error("actual supplied field lacks exact source Indirect, including when source already Input")
	}
	if !allow && (input || indirect) {
		f.t.Error("unauthorized source claimed by current manifest")
	}
	if direct != nil && input != *direct {
		f.t.Errorf("actual source Input=%t want=%t", input, *direct)
	}
}
func (f *phase2ActionMatrix) consume(id, role, query string, markers []string, allow bool, direct *bool) workspace.Run {
	f.t.Helper()
	answer := f.output
	if answer == "" {
		answer = "独立合成回答"
	}
	f.reply([]any{}, []string{}, answer)
	before := f.capture.count()
	operation := string(memory.NewID())
	var body []byte
	var manifest memory.ContextManifest
	var run workspace.Run
	if role == "secretary" {
		out, err := f.s.DeskTurn(context.Background(), f.scope, workspace.DeskTurnRequest{RequestID: operation, AgentID: f.secretaryPrincipal, ThingID: &id, Text: query})
		if err != nil {
			f.t.Fatal(err)
		}
		if f.capture.count() != before+1 {
			f.t.Fatal("secretary consumer actual HTTP missing")
		}
		body = f.capture.request(f.t, before)
		attempts, err := f.s.ContextAttempts(context.Background(), f.scope, operation)
		if err != nil || len(attempts) != 1 {
			f.t.Fatalf("consumer attempt missing: %v %+v", err, attempts)
		}
		manifest = attempts[0].Manifest
		f.evidence("secretary-consumer-"+operation, out)
	} else {
		agent := "phase2-model"
		if role == "manual" {
			agent = "manual"
		}
		command := workspace.Command{Type: "requestRun", ID: operation, ThingID: id, AgentID: agent, Kind: "ask", Prompt: query}
		if role == "manual" {
			command.ManualRecipient = &memory.Recipient{Provider: "phase2-model"}
		}
		state := f.cmd(command)
		for _, candidate := range state.Runs {
			if candidate.ID == operation {
				run = candidate
			}
		}
		if run.ID == "" {
			f.t.Fatal("actual consumer run missing")
		}
		if role == "manual" {
			pkg, err := f.s.ManualRunPackage(context.Background(), f.scope, operation)
			if err != nil {
				f.t.Fatal(err)
			}
			text, ok := pkg["package"].(string)
			if !ok {
				f.t.Fatal("fresh manual body missing")
			}
			body = []byte(text)
			attempt, ok := pkg["attempt"].(memory.ContextAttempt)
			if !ok {
				f.t.Fatal("fresh manual attempt missing")
			}
			manifest = attempt.Manifest
			if f.capture.count() != before {
				f.t.Error("manual package dispatched actual provider request")
			}
			if attempt.DeliveredAt == nil || attempt.ExternalReceipt != "unknown" {
				f.t.Error("manual delivery/unknown external receipt contract violated")
			}
			f.evidence("manual-consumer-"+operation, pkg)
		} else {
			f.startRunner()
			run = phase2RTWaitRun(f.t, f.s, f.scope, operation)
			if run.Status != "done" {
				f.t.Fatalf("actual deputy run failed: %+v", run)
			}
			if f.capture.count() != before+1 {
				f.t.Fatalf("deputy actual HTTP count=%d want=%d", f.capture.count(), before+1)
			}
			body = f.capture.request(f.t, before)
			attempts, err := f.s.ContextAttempts(context.Background(), f.scope, operation)
			if err != nil || len(attempts) != 1 {
				f.t.Fatalf("deputy attempt missing: %v %+v", err, attempts)
			}
			manifest = attempts[0].Manifest
			f.evidence("deputy-consumer-"+operation, run)
		}
	}
	for _, marker := range markers {
		if bytes.Contains(body, []byte(marker)) != allow {
			f.t.Errorf("%s actual derived presence for %q=%t want=%t", role, marker, bytes.Contains(body, []byte(marker)), allow)
		}
	}
	if !bytes.Contains(body, []byte(f.owner)) {
		f.t.Errorf("%s lost independent owner paragraph", role)
	}
	f.manifest(manifest, allow, direct)
	var carried struct {
		DeskActions []string `json:"desk_actions"`
	}
	if err := json.Unmarshal(asJSON(manifest), &carried); err != nil {
		f.t.Fatal(err)
	}
	for _, origin := range f.originIDs[id] {
		present := false
		for _, candidate := range carried.DeskActions {
			if candidate == origin {
				present = true
			}
		}
		if present != allow {
			f.t.Errorf("actual %s manifest origin %s present=%t want=%t", role, origin, present, allow)
		}
	}
	f.evidence(role+"-final-"+operation, map[string]any{"body": string(body), "manifest": manifest, "actual_model_calls": f.capture.count()})
	return run
}
func (f *phase2ActionMatrix) denyThree(id string, markers []string) {
	for _, role := range []string{"secretary", "deputy", "manual"} {
		f.consume(id, role, "查看这件普通事项，提供一个建议", markers, false, nil)
	}
}

func (f *phase2ActionMatrix) startRunner() {
	f.t.Helper()
	if f.runner {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	f.workerCancel = cancel
	f.workerDone = make(chan error, 1)
	done := f.workerDone
	go func() { done <- f.s.RunAgents(ctx, slog.New(slog.NewTextHandler(io.Discard, nil))) }()
	f.runner = true
	f.t.Cleanup(f.stopRunner)
}

func (f *phase2ActionMatrix) stopRunner() {
	f.t.Helper()
	if !f.runner {
		return
	}
	f.workerCancel()
	select {
	case err := <-f.workerDone:
		if err != nil && !errors.Is(err, context.Canceled) {
			f.t.Errorf("actual worker stop: %v", err)
		}
	case <-time.After(3 * time.Second):
		f.t.Error("actual worker did not stop")
	}
	f.runner = false
}

func (f *phase2ActionMatrix) undoAutomaticAdoption(run workspace.Run) workspace.Run {
	f.t.Helper()
	if run.Status != "done" || run.Adopted == nil || !run.Adopted.Auto || !memory.ID(run.Adopted.ActionID).Valid() {
		f.t.Fatalf("real automatic adoption positive control missing: %+v", run)
	}
	f.evidence("automatic-adoption-before-undo-"+run.ID, run)
	undoAutoAdoption(f.t, f.s, f.scope, run.ID)
	state, err := f.s.Snapshot(context.Background(), f.scope)
	if err != nil {
		f.t.Fatal(err)
	}
	for _, current := range state.Runs {
		if current.ID == run.ID {
			if current.Adopted != nil || current.Status != "done" || current.Output != run.Output {
				f.t.Fatalf("actual undo must preserve completed output and clear Adopted: %+v", current)
			}
			f.evidence("automatic-adoption-after-undo-"+run.ID, current)
			return current
		}
	}
	f.t.Fatal("completed run disappeared after automatic adoption undo")
	return workspace.Run{}
}

func (f *phase2ActionMatrix) prepare(id, role string) workspace.Run {
	f.t.Helper()
	agent := "phase2-model"
	if role == "manual" {
		agent = "manual"
	}
	command := workspace.Command{Type: "requestRun", ID: string(memory.NewID()), ThingID: id, AgentID: agent, Kind: "ask", Prompt: "根据合成海棠行动资料给建议"}
	if role == "manual" {
		command.ManualRecipient = &memory.Recipient{Provider: "phase2-model"}
	}
	state := f.cmd(command)
	for _, run := range state.Runs {
		if run.ID == command.ID {
			var carried struct {
				IDs  []string `json:"contextDeskActions"`
				Task struct {
					IDs []string `json:"desk_actions"`
				} `json:"contextTask"`
			}
			if err := json.Unmarshal(asJSON(run), &carried); err != nil {
				f.t.Fatal(err)
			}
			for _, origin := range f.originIDs[id] {
				inRun, inTask := false, false
				for _, candidate := range carried.IDs {
					if candidate == origin {
						inRun = true
					}
				}
				for _, candidate := range carried.Task.IDs {
					if candidate == origin {
						inTask = true
					}
				}
				if !inRun || !inTask {
					f.t.Errorf("actual prepared run/Task lost server origin %s: run=%t task=%t", origin, inRun, inTask)
				}
			}
			for _, marker := range f.derived[:3] {
				if !strings.Contains(run.Brief, marker) {
					f.t.Errorf("prepared old run lacks generated positive control %q", marker)
				}
			}
			f.evidence("old-prepared-"+role, run)
			return run
		}
	}
	f.t.Fatal("old prepared run missing")
	return workspace.Run{}
}
func (f *phase2ActionMatrix) revoke() {
	prior := f.secretary.Authorization
	f.secretary = f.revokePolicy(prior)
}

func (f *phase2ActionMatrix) revokePolicy(prior memory.SourceAuthorization) memory.SourceAuthorizationResult {
	f.t.Helper()
	out, err := f.s.SetSourceAuthorization(context.Background(), f.scope, memory.SourceAuthorizationRequest{RequestID: string(memory.NewID()), Source: f.source.Ref, PolicyID: prior.ID, ExpectedPolicyRevision: prior.Revision, Recipient: prior.Recipient, Purpose: prior.Purpose, Scope: prior.Scope, Revoke: true})
	if err != nil {
		f.t.Fatal(err)
	}
	if !out.Authorization.Revoked || out.Authorization.Revision <= prior.Revision {
		f.t.Fatal("formal revoke missing")
	}
	f.evidence("revoke-"+prior.Recipient.Role, out)
	return out
}
func (f *phase2ActionMatrix) cleared(ids []string, markers []string) {
	f.t.Helper()
	for _, id := range ids {
		var document []byte
		if err := f.s.pool.QueryRow(context.Background(), "SELECT document FROM work_items WHERE owner_id=$1 AND id=$2", string(f.scope.OwnerID), id).Scan(&document); err != nil {
			f.t.Fatal(err)
		}
		for _, marker := range markers {
			if bytes.Contains(document, []byte(marker)) {
				f.t.Errorf("canonical/contained history still retains invalidated %q", marker)
			}
		}
		if !bytes.Contains(document, []byte(f.owner)) {
			f.t.Error("canonical owner paragraph lost")
		}
		f.evidence("canonical-"+id, json.RawMessage(document))
	}
	var logs []byte
	if err := f.s.pool.QueryRow(context.Background(), "SELECT coalesce(jsonb_agg(to_jsonb(l)),'[]'::jsonb) FROM action_log l WHERE owner_id=$1 AND source='desk'", string(f.scope.OwnerID)).Scan(&logs); err != nil {
		f.t.Fatal(err)
	}
	for _, marker := range markers {
		if bytes.Contains(logs, []byte(marker)) {
			f.t.Errorf("invalidated original secretary action audit still contains %q", marker)
		}
	}
	f.evidence("secretary-audit", json.RawMessage(logs))
}

func TestPhase2ActionLineageMatrix(t *testing.T) {
	cases := []string{"create_denied_used_empty", "dual_role_direct_and_indirect", "dual_role_check_only_indirect", "mixed_update_and_stable_ids", "role_and_studio_scope", "original_route_changes", "revoke_regrant", "source_correction", "source_delete", "undo_before_blocks", "promotion_and_two_origins", "diagnostic_and_audit_expiry"}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			f := phase2ActionMatrixNew(t)
			ctx := context.Background()
			switch name {
			case "create_denied_used_empty":
				f.secretary = f.grant("phase2-model", "secretary", f.hard)
				id := f.create([]string{})
				f.consume(id, "deputy", "给这件事一个独立建议", f.derived[:5], false, nil)
				f.consume(id, "manual", "给这件事一个独立建议", f.derived[:5], false, nil)
			case "dual_role_direct_and_indirect":
				f.grantAll()
				id := f.create([]string{"M1"})
				direct := true
				for _, role := range []string{"deputy", "manual"} {
					f.consume(id, role, "根据合成海棠行动资料给建议", f.derived[:3], true, &direct)
				}
			case "dual_role_check_only_indirect":
				f.grantAll()
				id := f.newOwner("task", "")
				f.generation(id, []any{map[string]any{"op": "add_steps", "ref": "THIS", "steps": []string{f.derived[2]}}}, []string{}, f.derived)
				item := f.item(id)
				if len(item.Checklist) != 1 || item.Checklist[0].Text != f.derived[2] {
					t.Fatal("actual add_steps missing")
				}
				direct := false
				for _, role := range []string{"deputy", "manual"} {
					f.consume(id, role, "给普通事项一个建议", []string{f.derived[2]}, true, &direct)
				}
			case "mixed_update_and_stable_ids":
				f.secretary = f.grant("phase2-model", "secretary", f.hard)
				for _, kind := range []string{"task", "idea", "project"} {
					id := f.newOwner(kind, "")
					if kind == "project" {
						f.sourceScope([]memory.SourceScopeAssignment{{Kind: memory.StudioSourceScope, StudioID: memory.ID(id)}})
						f.hard = memory.HardScope{Kind: memory.StudioContextScope, StudioID: memory.ID(id), IncludeGlobalConstraints: true}
						f.secretary = f.grant("phase2-model", "secretary", f.hard)
					}
					out := f.generation(id, []any{map[string]any{"op": "update", "ref": "THIS", "set": map[string]any{"title": f.derived[0], "notesAppend": f.derived[1]}}}, []string{}, f.derived)
					_ = out
					item := f.item(id)
					content := item.Notes
					if kind == "idea" {
						content = item.Body
					}
					if kind == "project" {
						content = item.Goal
					}
					if !strings.Contains(content, f.owner) || !strings.Contains(content, f.derived[1]) {
						t.Fatal("model notesAppend did not preserve owner's original paragraph")
					}
					if kind == "task" {
						f.cmd(workspace.Command{Type: "addCheck", ID: id, Text: "OWNER removable earlier step"})
						earlier := f.item(id).Checklist[0].ID
						f.generation(id, []any{map[string]any{"op": "add_steps", "ref": "THIS", "steps": []string{f.derived[2]}}}, []string{}, f.derived)
						actual := f.item(id).Checklist[1].ID
						f.cmd(workspace.Command{Type: "removeCheck", ID: id, ItemID: earlier})
						item = f.item(id)
						if len(item.Checklist) != 1 || item.Checklist[0].ID != actual {
							t.Fatal("stable actual derived check ID changed after prior removal")
						}
						var blocks []byte
						if err := f.s.pool.QueryRow(ctx, "SELECT blocks FROM artifact_fields WHERE owner_id=$1 AND thing_id=$2 AND field=$3", string(f.scope.OwnerID), id, "check:"+actual).Scan(&blocks); err != nil {
							t.Fatal(err)
						}
						f.evidence("stable-check", json.RawMessage(blocks))
					}
					f.consume(id, "manual", "查看普通事项", []string{f.derived[0], f.derived[1]}, false, nil)
				}
				f.sourceScope([]memory.SourceScopeAssignment{{Kind: memory.UnscopedSourceScope}})
				f.hard = phase2RTUnscoped()
				f.secretary = f.grant("phase2-model", "secretary", f.hard)
				out := f.generation("", []any{map[string]any{"op": "create_idea", "title": "普通条件想法", "condition": f.derived[5]}}, []string{}, f.derived)
				id := *out.Turn.Receipts[0].ThingID
				f.cmd(workspace.Command{Type: "setNotes", ID: id, Text: f.owner})
				item := f.item(id)
				if len(item.Conditions) != 1 {
					t.Fatal("actual create_idea condition absent")
				}
				var blocks []byte
				if err := f.s.pool.QueryRow(ctx, "SELECT blocks FROM artifact_fields WHERE owner_id=$1 AND thing_id=$2 AND field=$3", string(f.scope.OwnerID), id, "condition:"+item.Conditions[0].ID).Scan(&blocks); err != nil {
					t.Fatal(err)
				}
				f.evidence("stable-condition", json.RawMessage(blocks))
				f.revoke()
				f.cleared([]string{id}, []string{f.derived[5]})
				f.consume(id, "manual", "查看普通想法", []string{f.derived[5]}, false, nil)
			case "role_and_studio_scope":
				projectA := f.newOwner("project", "")
				projectB := f.newOwner("project", "")
				f.hard = memory.HardScope{Kind: memory.StudioContextScope, StudioID: memory.ID(projectA), IncludeGlobalConstraints: true}
				f.sourceScope([]memory.SourceScopeAssignment{{Kind: memory.StudioSourceScope, StudioID: memory.ID(projectA)}})
				f.secretary = f.grant("phase2-model", "secretary", f.hard)
				id := f.newOwner("task", projectA)
				f.generation(id, []any{map[string]any{"op": "update", "ref": "THIS", "set": map[string]any{"notesAppend": f.derived[1]}}}, []string{}, f.derived)
				f.grant("manual", "deputy", f.hard)
				f.consume(id, "manual", "查看普通事项", []string{f.derived[1]}, false, nil)
				f.grant("manual", "manual", memory.HardScope{Kind: memory.StudioContextScope, StudioID: memory.ID(projectB), IncludeGlobalConstraints: true})
				f.consume(id, "manual", "查看普通事项", []string{f.derived[1]}, false, nil)
				f.grant("manual", "manual", f.hard)
				f.consume(id, "manual", "查看普通事项", []string{f.derived[1]}, true, nil)
			case "original_route_changes":
				originProvider := f.capture.Registry.Config.Providers[0]
				originProvider.ID = "phase2-origin"
				originProvider.Name = "synthetic secretary origin"
				originProvider.Model = "phase2-synthetic-secretary"
				f.capture.Registry.Config.Providers = append(f.capture.Registry.Config.Providers, originProvider)
				f.s.SetModels(f.capture.Registry)
				if _, err := f.s.Snapshot(ctx, f.scope); err != nil {
					t.Fatal(err)
				}
				f.secretaryPrincipal = originProvider.ID
				f.grantAll()
				id := f.create([]string{})
				f.output = "COMPLETED old route result turquoise 919"
				completed := f.consume(id, "deputy", "根据合成海棠行动资料给建议", f.derived[:3], true, nil)
				if completed.Output != f.output {
					t.Fatal("real completed deputy output absent before route mutation")
				}
				f.output = ""
				f.stopRunner()
				completed = f.undoAutomaticAdoption(completed)
				oldManual := f.prepare(id, "manual")
				oldDeputy := f.prepare(id, "deputy")
				if oldManual.ContextTask == nil || oldDeputy.ContextTask == nil {
					t.Fatal("prepared target own trusted Task missing")
				}
				priorTarget, err := f.s.ContextRecipient(ctx, f.scope, "phase2-model", "deputy", nil)
				if err != nil {
					t.Fatal(err)
				}
				priorManualTarget, err := f.s.ContextRecipient(ctx, f.scope, "manual", "manual", &memory.Recipient{Provider: "phase2-model"})
				if err != nil || oldManual.ContextTask.Recipient != priorManualTarget || oldDeputy.ContextTask.Recipient != priorTarget {
					t.Fatal("prepared target Task does not match its own lawful actual recipient")
				}
				priorPackage, err := f.s.ManualRunPackage(ctx, f.scope, oldManual.ID)
				if err != nil {
					t.Fatal(err)
				}
				priorBody, ok := priorPackage["package"].(string)
				if !ok {
					t.Fatal("prior lawful manual positive control absent")
				}
				for _, marker := range f.derived[:3] {
					if !strings.Contains(priorBody, marker) {
						t.Errorf("prior actual manual body lacks %q", marker)
					}
				}
				priorAttempt, ok := priorPackage["attempt"].(memory.ContextAttempt)
				if !ok || priorAttempt.Manifest.Recipient != priorManualTarget {
					t.Fatal("lawful prior package did not bind actual own manual target")
				}
				f.manifest(priorAttempt.Manifest, true, nil)
				f.evidence("old-lawful-manual-package", priorPackage)
				before := f.capture.count()
				f.capture.Registry.Config.Providers[1].Model = "phase2-synthetic-new-secretary-route"
				f.s.SetModels(f.capture.Registry)
				liveTarget, err := f.s.ContextRecipient(ctx, f.scope, "phase2-model", "deputy", nil)
				if err != nil || liveTarget != priorTarget {
					t.Fatal("test must change only original secretary route, keeping target route exact")
				}
				liveManualTarget, err := f.s.ContextRecipient(ctx, f.scope, "manual", "manual", &memory.Recipient{Provider: "phase2-model"})
				if err != nil || liveManualTarget != priorManualTarget {
					t.Fatal("manual target must also remain exact when only secretary route changes")
				}
				f.secretary = f.grant(f.secretaryPrincipal, "secretary", f.hard)
				latePackage, lateErr := f.s.ManualRunPackage(ctx, f.scope, oldManual.ID)
				f.evidence("old-manual-reget-after-origin-route", map[string]any{"package": latePackage, "rejected": lateErr != nil})
				if !errors.Is(lateErr, memory.ErrConflict) && !errors.Is(lateErr, memory.ErrForbidden) {
					t.Errorf("old prepared manual must reject invalid original route: %v", lateErr)
				}
				f.reply([]any{}, []string{}, "独立合成回答")
				f.startRunner()
				finished := phase2RTWaitRun(t, f.s, f.scope, oldDeputy.ID)
				f.evidence("old-deputy-after-origin-route", finished)
				if finished.Status == "done" || f.capture.count() != before {
					t.Error("prepared old deputy survived origin route fence or reached provider")
				}
				current, err := f.s.Snapshot(ctx, f.scope)
				if err != nil {
					t.Fatal(err)
				}
				adopted, adoptErr := f.s.Execute(ctx, f.scope, workspace.Command{Type: "adoptRun", ID: oldDeputy.ID, As: "progress", Text: "try to adopt old result", RequestID: string(memory.NewID()), ExpectedRevision: current.Revision})
				f.evidence("old-deputy-adoption-after-origin-route", map[string]any{"state": adopted, "rejected": adoptErr != nil})
				if !errors.Is(adoptErr, memory.ErrInvalid) && !errors.Is(adoptErr, memory.ErrForbidden) && !errors.Is(adoptErr, memory.ErrConflict) {
					t.Errorf("old result adoption did not return controlled refusal: %v", adoptErr)
				}
				current, err = f.s.Snapshot(ctx, f.scope)
				if err != nil {
					t.Fatal(err)
				}
				adopted, adoptErr = f.s.Execute(ctx, f.scope, workspace.Command{Type: "adoptRun", ID: completed.ID, As: "progress", Text: completed.Output, RequestID: string(memory.NewID()), ExpectedRevision: current.Revision})
				f.evidence("completed-result-adoption-after-origin-route", map[string]any{"completed_before_change": completed, "state": adopted, "rejected": adoptErr != nil})
				if !errors.Is(adoptErr, memory.ErrForbidden) && !errors.Is(adoptErr, memory.ErrConflict) {
					t.Errorf("previously completed result bypassed current original route fence: %v", adoptErr)
				}
				f.denyThree(id, f.derived[:5])
			case "revoke_regrant":
				f.grantAll()
				id := f.create([]string{})
				policies, err := f.s.SourceAuthorizations(ctx, f.scope, f.source.ID)
				if err != nil {
					t.Fatal(err)
				}
				var deputy memory.SourceAuthorization
				for _, policy := range policies {
					if policy.Recipient.Role == "deputy" && !policy.Revoked {
						deputy = policy
					}
				}
				if deputy.ID == "" {
					t.Fatal("actual deputy policy positive control missing")
				}
				f.revokePolicy(deputy)
				f.consume(id, "secretary", "查看普通事项", f.derived[:3], true, nil)
				f.consume(id, "manual", "查看普通事项", f.derived[:3], true, nil)
				f.consume(id, "deputy", "查看普通事项", f.derived[:5], false, nil)
				var staleOrigins int
				if err := f.s.pool.QueryRow(ctx, "SELECT count(*) FROM action_log WHERE owner_id=$1 AND source='desk' AND context_stale", string(f.scope.OwnerID)).Scan(&staleOrigins); err != nil || staleOrigins != 0 {
					t.Fatalf("target-only revoke permanently invalidated lawful secretary origin: %d %v", staleOrigins, err)
				}
				f.revoke()
				f.cleared([]string{id}, f.derived[:5])
				f.secretary = f.grant("phase2-model", "secretary", f.hard)
				f.denyThree(id, f.derived[:5])
			case "source_correction":
				f.grantAll()
				id := f.create([]string{})
				replacement, err := memory.NewService(f.s).Ingest(ctx, f.scope, memory.IngestRequest{Connector: "phase2-action-matrix-synthetic", ExternalID: "raw-material", ExternalVersion: "v2", Title: "合成海棠行动资料", Text: "合成海棠行动资料已更正，新安排 azure 808。", MediaType: "text/plain"})
				if err != nil {
					t.Fatal(err)
				}
				if replacement.ID != f.source.ID || replacement.Version <= f.source.Version {
					t.Fatal("real correction did not advance exact source")
				}
				f.evidence("corrected-source", replacement)
				f.cleared([]string{id}, f.derived[:5])
				f.denyThree(id, f.derived[:5])
			case "source_delete":
				f.grantAll()
				id := f.create([]string{})
				if err := f.s.Delete(ctx, f.scope, memory.DeleteRequest{Targets: []memory.Ref{f.source.Ref}, IncludeSources: true, BlockReimport: true}); err != nil {
					t.Fatal(err)
				}
				f.cleared([]string{id}, f.derived[:5])
				f.denyThree(id, f.derived[:5])
			case "undo_before_blocks":
				f.grantAll()
				id := f.create([]string{})
				editID := string(memory.NewID())
				st, err := f.s.Snapshot(ctx, f.scope)
				if err != nil {
					t.Fatal(err)
				}
				_, err = f.s.Execute(ctx, f.scope, workspace.Command{Type: "setNotes", ID: id, Text: f.derived[1] + " edited\n" + f.owner, RequestID: editID, ExpectedRevision: st.Revision})
				if err != nil {
					t.Fatal(err)
				}
				f.revoke()
				st, err = f.s.Snapshot(ctx, f.scope)
				if err != nil {
					t.Fatal(err)
				}
				undo, undoErr := f.s.Execute(ctx, f.scope, workspace.Command{Type: "undoAction", ID: editID, RequestID: string(memory.NewID()), ExpectedRevision: st.Revision})
				f.evidence("actual-undo", map[string]any{"error": undoErr != nil, "state": undo})
				if undoErr != nil && !errors.Is(undoErr, memory.ErrConflict) && !errors.Is(undoErr, memory.ErrForbidden) && !errors.Is(undoErr, workspace.ErrChangedSince) && !errors.Is(undoErr, workspace.ErrNewerAction) && !errors.Is(undoErr, workspace.ErrExpired) {
					t.Fatalf("undo did not complete or return a supported safety conflict: %v", undoErr)
				}
				if errors.Is(undoErr, workspace.ErrExpired) {
					var expired bool
					var remaining []byte
					if err := f.s.pool.QueryRow(ctx, "SELECT expired_at IS NOT NULL,changes FROM action_log WHERE owner_id=$1 AND id=$2", string(f.scope.OwnerID), editID).Scan(&expired, &remaining); err != nil || !expired || string(remaining) != "[]" {
						t.Fatalf("controlled expired undo must retain stable audit ID and erase old body: expired=%t changes=%s %v", expired, remaining, err)
					}
				}
				// A safe conflict is permitted after invalidation erased the old undo body;
				// success must still live-validate beforeBlocks. Either outcome must preserve
				// owner writing and must not restore the invalidated origin.
				f.cleared([]string{id}, f.derived[:5])
				f.denyThree(id, f.derived[:5])
			case "promotion_and_two_origins":
				f.grantAll()
				id := f.newOwner("idea", "")
				generated := f.generation(id, []any{map[string]any{"op": "update", "ref": "THIS", "set": map[string]any{"notesAppend": f.derived[1]}}}, []string{}, f.derived)
				f.output = "RUN derived azure 808"
				run := f.consume(id, "deputy", "根据合成海棠行动资料提供建议", []string{f.derived[1]}, true, nil)
				if run.Output != f.output {
					t.Fatal("actual deputy output fixture absent")
				}
				run = f.undoAutomaticAdoption(run)
				beforeAdoption := f.item(id)
				if !strings.Contains(beforeAdoption.Body, f.owner) || !strings.Contains(beforeAdoption.Body, f.derived[1]) {
					t.Fatal("undo of automatic adoption lost independent owner or secretary action body")
				}
				adoptedState := f.cmd(workspace.Command{Type: "adoptRun", ID: run.ID, As: "progress", Text: run.Output})
				var manualAdoption *workspace.Adoption
				for _, candidate := range adoptedState.Runs {
					if candidate.ID == run.ID {
						manualAdoption = candidate.Adopted
					}
				}
				if manualAdoption == nil || manualAdoption.Auto || manualAdoption.As != "progress" {
					t.Fatalf("actual manual progress adoption missing after undo: %+v", manualAdoption)
				}
				f.evidence("manual-progress-adoption-after-auto-undo", manualAdoption)
				f.output = ""
				state := f.cmd(workspace.Command{Type: "ideaPromote", ID: id})
				taskID := ""
				for _, item := range state.Tasks {
					if item.IdeaID == id {
						taskID = item.ID
					}
				}
				if taskID == "" {
					t.Fatal("real promotion produced no task")
				}
				var blocks []byte
				if err := f.s.pool.QueryRow(ctx, "SELECT blocks FROM artifact_fields WHERE owner_id=$1 AND thing_id=$2 AND field='notes'", string(f.scope.OwnerID), taskID).Scan(&blocks); err != nil {
					t.Fatal(err)
				}
				if !bytes.Contains(blocks, []byte(run.ID)) || !bytes.Contains(blocks, []byte(*generated.Turn.Receipts[0].ActionID)) || !bytes.Contains(blocks, []byte("deskActions")) {
					t.Errorf("promotion lost independent Runs/DeskActions origins: %s", blocks)
				}
				f.evidence("promotion-two-origins", json.RawMessage(blocks))
				f.originIDs[taskID] = append([]string{}, f.originIDs[id]...)
				f.revoke()
				markers := []string{f.derived[1], "RUN derived azure 808"}
				f.cleared([]string{id, taskID}, markers)
				f.denyThree(taskID, markers)
			case "diagnostic_and_audit_expiry":
				f.grantAll()
				id := f.create([]string{})
				if err := f.s.CleanupContextAttempts(ctx, time.Now().UTC().Add(31*24*time.Hour)); err != nil {
					t.Fatal(err)
				}
				var diagnosticCount int
				if err := f.s.pool.QueryRow(ctx, "SELECT count(*) FROM context_attempts WHERE owner_id=$1", string(f.scope.OwnerID)).Scan(&diagnosticCount); err != nil || diagnosticCount != 0 {
					t.Fatalf("diagnostic metadata expiry not established: %d %v", diagnosticCount, err)
				}
				if _, err := f.s.pool.Exec(ctx, "UPDATE action_log SET created_at=now()-interval '31 days' WHERE owner_id=$1 AND source='desk'", string(f.scope.OwnerID)); err != nil {
					t.Fatal(err)
				}
				f.cmd(workspace.Command{Type: "addCheck", ID: id, Text: "OWNER audit cleanup trigger"})
				var activeOldAudit int
				if err := f.s.pool.QueryRow(ctx, "SELECT count(*) FROM action_log WHERE owner_id=$1 AND source='desk' AND (expired_at IS NULL OR changes<>'[]'::jsonb)", string(f.scope.OwnerID)).Scan(&activeOldAudit); err != nil || activeOldAudit != 0 {
					t.Fatalf("existing action audit expiry not established: %d %v", activeOldAudit, err)
				}
				f.consume(id, "manual", "根据合成海棠行动资料给建议", f.derived[:3], true, nil)
				f.revoke()
				f.cleared([]string{id}, f.derived[:5])
				f.denyThree(id, f.derived[:5])
			}
			f.evidence("final-owner-state", func() workspace.State {
				st, err := f.s.Snapshot(ctx, f.scope)
				if err != nil {
					t.Fatal(err)
				}
				return st
			}())
		})
	}
}
