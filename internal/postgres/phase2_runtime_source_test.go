package postgres

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func phase2RTUnscoped() memory.HardScope { return memory.HardScope{Kind: memory.UnscopedContextScope} }

func phase2RTScopeSet(t *testing.T, s *Store, scope memory.Scope, source memory.Ref, expected int, assignments []memory.SourceScopeAssignment) memory.SourceScopeResult {
	t.Helper()
	in := memory.SourceScopeRequest{RequestID: string(memory.NewID()), Source: source, ExpectedRevision: expected, Assignments: assignments}
	w := phase2RTHTTP(t, s, scope, http.MethodPut, "/v1/memory/sources/"+string(source.ID)+"/scope", in)
	if w.Code < 200 || w.Code >= 300 {
		t.Fatalf("formal scope assignment failed: %d %s", w.Code, w.Body.String())
	}
	var out memory.SourceScopeResult
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Source != source || out.Revision <= expected {
		t.Error("scope result lost source identity/version or revision")
	}
	return out
}

func phase2RTClaimFixture(t *testing.T, s *Store, scope memory.Scope, text, claim, kind string) (memory.Ref, memory.Ref) {
	t.Helper()
	st := workspaceCommand(t, s, scope, workspace.Command{Type: "capture", Text: text})
	if len(st.Candidates) == 0 {
		t.Fatal("ordinary capture fixture missing")
	}
	candidate := st.Candidates[0]
	source := memory.Ref{ID: memory.ID(candidate.Source.SourceID), Version: candidate.Source.Version, Kind: memory.SourceKind}
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "acceptCandidate", ID: candidate.ID, Kind: "memory", MemoryKind: kind, Text: claim})
	for _, m := range st.Memories {
		if m.Text == claim {
			return source, memory.Ref{ID: memory.ID(m.ID), Version: m.Version, Kind: memory.ClaimKind}
		}
	}
	t.Fatal("independent confirmed claim fixture absent")
	return memory.Ref{}, memory.Ref{}
}

func TestPhase2RuntimeSourcePublicAuthorizationExactVersion(t *testing.T) {
	s, ownerScope, _ := phase2RTSetup(t)
	source := phase2RTSource(t, s, ownerScope)
	policy, _ := phase2RTAuthorize(t, s, ownerScope, source.Ref, "phase2-model", "secretary", phase2RTUnscoped())
	reader := phase2RTTask(t, s, ownerScope, "phase2-model", "secretary", phase2RTUnscoped())
	if policy.Authorization.Recipient != reader.Task.Recipient {
		t.Error("public partial selection did not bind actual server route")
	}
	out, err := s.GetSource(context.Background(), reader, source.ID, source.Version)
	if err != nil {
		t.Fatal(err)
	}
	phase2RTAtoms(t, out, phase2RTGoldRead(t).Cases[0].Required, true)
	recall := phase2RTRecall(t, s, reader, phase2RTGoldRead(t).Cases[0].Query)
	found := false
	for _, ref := range recall.Memories {
		if ref == source.Ref {
			found = true
		}
	}
	if !found {
		t.Error("authorized zero-claim source exact ref missing from Recall")
	}
	expanded, err := s.Expand(context.Background(), reader, memory.ExpandRequest{Refs: []memory.Ref{source.Ref}, Evidence: true, Budget: memory.Budget{Tokens: 4000}})
	if err != nil {
		t.Fatal(err)
	}
	phase2RTAtoms(t, expanded, phase2RTGoldRead(t).Cases[0].Required, true)
	for _, kind := range []memory.Kind{memory.ClaimKind, memory.SummaryKind, memory.Kind("unknown")} {
		t.Run("wrong_kind_"+string(kind), func(t *testing.T) {
			bad := source.Ref
			bad.Kind = kind
			result, err := s.Expand(context.Background(), reader, memory.ExpandRequest{Refs: []memory.Ref{bad}, Budget: memory.Budget{Tokens: 4000}})
			phase2RTEvidence(t, "wrong-kind", map[string]any{"ref": bad, "result": result, "error": err != nil})
			phase2RTAtoms(t, result, phase2RTGoldRead(t).Cases[0].Required, false)
		})
	}
	var replacement struct {
		Replacement struct {
			ExternalVersion string   `json:"external_version"`
			Text            string   `json:"text"`
			Atoms           []string `json:"atoms"`
		} `json:"replacement"`
	}
	phase2RTReadJSON(t, "runtime-sequences.json", &replacement)
	next := mustIngest(t, s, ownerScope, memory.IngestRequest{Connector: "phase2-runtime-synthetic", ExternalID: phase2RTGoldRead(t).Records[0].ID, ExternalVersion: replacement.Replacement.ExternalVersion, Title: "成都预约资料", Text: replacement.Replacement.Text, MediaType: "text/plain"})
	if next.ID != source.ID || next.Version != source.Version+1 {
		t.Fatal("source update changed stable identity or exact version")
	}
	current, err := s.GetSource(context.Background(), reader, next.ID, next.Version)
	if err != nil {
		t.Fatal(err)
	}
	phase2RTAtoms(t, current, replacement.Replacement.Atoms, true)
	phase2RTAtoms(t, current, phase2RTGoldRead(t).Cases[0].Required, false)
	reader.Task.View.Mode = memory.History
	history, err := s.GetSource(context.Background(), reader, source.ID, source.Version)
	if err != nil {
		t.Fatal(err)
	}
	if history.Source.Ref != source.Ref || history.Source.Text != phase2RTGoldRead(t).Records[0].Text {
		t.Error("old source ref was hydrated with current body/version")
	}
	var knownAt time.Time
	if err := s.pool.QueryRow(context.Background(), "SELECT known_from FROM record_versions WHERE owner_id=$1 AND record_id=$2 AND version=$3", string(ownerScope.OwnerID), string(source.ID), source.Version).Scan(&knownAt); err != nil {
		t.Fatal(err)
	}
	reader.Task.View.KnownAt = &knownAt
	if _, err := s.GetSource(context.Background(), reader, next.ID, next.Version); err == nil {
		t.Error("exact version newer than trusted KnownAt was supplied")
	}
}

func TestPhase2RuntimeNonownerRawAPIsRequireTrustedTask(t *testing.T) {
	s, ownerScope, _ := phase2RTSetup(t)
	source := phase2RTSource(t, s, ownerScope)
	phase2RTAuthorize(t, s, ownerScope, source.Ref, "phase2-model", "secretary", phase2RTUnscoped())
	without := memory.Scope{OwnerID: ownerScope.OwnerID, PrincipalID: "phase2-model"}
	for _, entry := range []string{"GetSource", "Recall", "Expand", "Summarize"} {
		t.Run(entry, func(t *testing.T) {
			var out any
			var err error
			switch entry {
			case "GetSource":
				out, err = s.GetSource(context.Background(), without, source.ID, source.Version)
			case "Recall":
				out, err = s.Recall(context.Background(), without, memory.RecallRequest{Query: phase2RTGoldRead(t).Cases[0].Query, Mode: memory.Remember, Budget: memory.Budget{Candidates: 15, Tokens: 4000}})
			case "Expand":
				out, err = s.Expand(context.Background(), without, memory.ExpandRequest{Refs: []memory.Ref{source.Ref}, Evidence: true, Budget: memory.Budget{Tokens: 4000}})
			case "Summarize":
				out, err = s.Summarize(context.Background(), without, memory.SummaryRequest{ID: source.ID, Version: source.Version, Tokens: 4000})
			}
			if err == nil {
				t.Error("nonowner raw API accepted missing trusted Task")
			}
			phase2RTAtoms(t, out, phase2RTGoldRead(t).Cases[0].Required, false)
			phase2RTEvidence(t, "missing-task", map[string]any{"entry": entry, "error": err != nil, "result": out})
		})
	}
	if _, err := s.GetSource(context.Background(), ownerScope, source.ID, source.Version); err != nil {
		t.Fatal("owner audit was incorrectly coupled to model Task", err)
	}
	w := phase2RTHTTP(t, s, ownerScope, http.MethodPost, "/v1/memory/recall", map[string]any{"query": "成都预约资料", "mode": "remember", "task": map[string]any{"owner_id": ownerScope.OwnerID, "recipient": map[string]string{"principal_id": "phase2-model"}}})
	if w.Code != http.StatusBadRequest {
		t.Errorf("client Task injection HTTP=%d want 400", w.Code)
	}
}

func TestPhase2RuntimeSourcePolicyReplayRouteAndDisabledRevoke(t *testing.T) {
	s, scope, capture := phase2RTSetup(t)
	source := phase2RTSource(t, s, scope)
	grant, original := phase2RTAuthorize(t, s, scope, source.Ref, "phase2-model", "secretary", phase2RTUnscoped())
	revoked := phase2RTUpdatePolicy(t, s, scope, source.Ref, grant, true)
	w := phase2RTHTTP(t, s, scope, http.MethodPost, "/v1/memory/sources/"+string(source.ID)+"/authorization", original)
	if w.Code != http.StatusConflict {
		t.Errorf("old authorization replay after revoke status=%d want409", w.Code)
	}
	var revision int
	var deny, rev bool
	if err := s.pool.QueryRow(context.Background(), "SELECT revision,revoked,explicit_deny FROM source_authorizations WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), string(grant.Authorization.ID)).Scan(&revision, &rev, &deny); err != nil {
		t.Fatal(err)
	}
	if revision != revoked.Authorization.Revision || !rev || !deny {
		t.Error("old request replay revived revoked policy")
	}
	allowed := phase2RTUpdatePolicy(t, s, scope, source.Ref, revoked, false)
	// This independent policy has no successor revision. Its replay isolates
	// actual route binding from the original policy's revoke/regrant conflict.
	routeSource := mustIngest(t, s, scope, memory.IngestRequest{Connector: "phase2-runtime-synthetic", ExternalID: "route-only-control", ExternalVersion: "v1", Title: "独立路由资料", Text: phase2RTGoldRead(t).Records[0].Text, MediaType: "text/plain"})
	routeGrant, routeOriginal := phase2RTAuthorize(t, s, scope, routeSource.Ref, "phase2-model", "secretary", phase2RTUnscoped())
	capture.Registry.Config.Providers[0].Model = "phase2-new-route-model"
	routeReplay := phase2RTHTTP(t, s, scope, http.MethodPost, "/v1/memory/sources/"+string(routeSource.ID)+"/authorization", routeOriginal)
	if routeReplay.Code != http.StatusConflict {
		t.Errorf("route-only replay without successor status=%d want409", routeReplay.Code)
	}
	var routePolicies, routeRevision int
	var routeModel string
	if err := s.pool.QueryRow(context.Background(), "SELECT count(*),max(revision),max(model) FROM source_authorizations WHERE owner_id=$1 AND source_id=$2", string(scope.OwnerID), string(routeSource.ID)).Scan(&routePolicies, &routeRevision, &routeModel); err != nil {
		t.Fatal(err)
	}
	if routePolicies != 1 || routeRevision != routeGrant.Authorization.Revision || routeModel != routeGrant.Authorization.Recipient.Model {
		t.Error("route-only replay changed canonical original permission")
	}
	w = phase2RTHTTP(t, s, scope, http.MethodPost, "/v1/memory/sources/"+string(source.ID)+"/authorization", original)
	if w.Code != http.StatusConflict {
		t.Errorf("old partial-recipient replay on new route status=%d want409", w.Code)
	}
	current := phase2RTTask(t, s, scope, "phase2-model", "secretary", phase2RTUnscoped())
	if _, err := s.GetSource(context.Background(), current, source.ID, source.Version); err == nil {
		t.Error("old policy implicitly authorized changed actual model/route")
	}
	workspaceCommand(t, s, scope, workspace.Command{Type: "updateAgent", ID: "phase2-model", Patch: asJSON(map[string]bool{"enabled": false})})
	phase2RTUpdatePolicy(t, s, scope, source.Ref, allowed, true)
}

func TestPhase2RuntimeSourceScopeHardBoundary(t *testing.T) {
	s, scope, _ := phase2RTSetup(t)
	source := phase2RTSource(t, s, scope)
	st := workspaceCommand(t, s, scope, workspace.Command{Type: "addProject", Title: "Studio A"})
	a := memory.ID(st.Projects[0].ID)
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "addProject", Title: "Studio B"})
	var b memory.ID
	for _, p := range st.Projects {
		if p.Title == "Studio B" {
			b = memory.ID(p.ID)
		}
	}
	assigned := phase2RTScopeSet(t, s, scope, source.Ref, 0, []memory.SourceScopeAssignment{{Kind: memory.StudioSourceScope, StudioID: a}})
	hardA := memory.HardScope{Kind: memory.StudioContextScope, StudioID: a, IncludeGlobalConstraints: true}
	hardB := hardA
	hardB.StudioID = b
	phase2RTAuthorize(t, s, scope, source.Ref, "phase2-model", "secretary", hardA)
	// B has an independent legitimate receiver policy; source membership alone
	// must still reject B and cannot be widened by Objects hints.
	phase2RTAuthorize(t, s, scope, source.Ref, "phase2-model", "secretary", hardB)
	readerA := phase2RTTask(t, s, scope, "phase2-model", "secretary", hardA)
	if _, err := s.GetSource(context.Background(), readerA, source.ID, source.Version); err != nil {
		t.Fatal("lawful studio A read", err)
	}
	readerB := phase2RTTask(t, s, scope, "phase2-model", "secretary", hardB)
	if _, err := s.GetSource(context.Background(), readerB, source.ID, source.Version); err == nil {
		t.Error("studio B read studio A raw source")
	}
	out, err := s.Recall(context.Background(), readerB, memory.RecallRequest{Query: phase2RTGoldRead(t).Cases[0].Query, Context: memory.WorkingContext{Objects: []memory.ID{a, source.ID}}, Mode: memory.Remember, Budget: memory.Budget{Candidates: 15, Tokens: 4000}})
	if err != nil {
		t.Fatal(err)
	}
	phase2RTAtoms(t, out, phase2RTGoldRead(t).Cases[0].Required, false)
	phase2RTScopeSet(t, s, scope, source.Ref, assigned.Revision, []memory.SourceScopeAssignment{})
	if _, err := s.GetSource(context.Background(), readerA, source.ID, source.Version); err == nil {
		t.Error("scope shrink left old source access valid")
	}
}

func TestPhase2RuntimeClaimNatureDoesNotGrantGlobalScope(t *testing.T) {
	for _, kind := range []string{"preference", "decision"} {
		t.Run(kind, func(t *testing.T) {
			s, scope, _ := phase2RTSetup(t)
			var sequence struct {
				ScopeControls map[string]struct{ Label, Text, Claim, Atom string } `json:"scope_controls"`
			}
			phase2RTReadJSON(t, "runtime-sequences.json", &sequence)
			gold := sequence.ScopeControls[kind]
			if gold.Atom == "" {
				t.Fatal("independent scope control gold missing")
			}
			source, _ := phase2RTClaimFixture(t, s, scope, gold.Text, gold.Claim, kind)
			st := workspaceCommand(t, s, scope, workspace.Command{Type: "addProject", Title: "isolated studio"})
			studio := memory.ID(st.Projects[0].ID)
			hard := memory.HardScope{Kind: memory.StudioContextScope, StudioID: studio, IncludeGlobalConstraints: true}
			reader := phase2RTTask(t, s, scope, "phase2-model", "secretary", hard)
			phase2RTAtoms(t, phase2RTRecall(t, s, reader, gold.Label), []string{gold.Atom}, false)
			phase2RTScopeSet(t, s, scope, source, 0, []memory.SourceScopeAssignment{{Kind: memory.GlobalConstraintSourceScope}})
			phase2RTAtoms(t, phase2RTRecall(t, s, reader, gold.Label), []string{gold.Atom}, true)
			if _, err := s.GetSource(context.Background(), reader, source.ID, source.Version); err == nil {
				t.Error("global claim permission implicitly granted raw source policy")
			}
		})
	}
}

func TestPhase2RuntimeExplicitDenyAndUndoFirstGrant(t *testing.T) {
	s, scope, _ := phase2RTSetup(t)
	gold := phase2RTGoldRead(t)
	source, claim := phase2RTClaimFixture(t, s, scope, gold.Records[1].Text, gold.Records[1].Claim, "fact")
	reader := phase2RTTask(t, s, scope, "phase2-model", "secretary", phase2RTUnscoped())
	phase2RTAtoms(t, phase2RTRecall(t, s, reader, gold.Cases[2].Query), gold.Cases[2].Required, true)
	if _, err := s.GetSource(context.Background(), reader, source.ID, source.Version); err == nil {
		t.Error("absence of policy authorized raw source")
	}
	grant, _ := phase2RTAuthorize(t, s, scope, source, "phase2-model", "secretary", phase2RTUnscoped())
	if !grant.Undoable || grant.ActionID == "" {
		t.Fatal("real public grant did not return actionable undo receipt")
	}
	workspaceCommand(t, s, scope, workspace.Command{Type: "undoAction", ID: grant.ActionID})
	var revision int
	var revoked, deny bool
	if err := s.pool.QueryRow(context.Background(), "SELECT revision,revoked,explicit_deny FROM source_authorizations WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), string(grant.Authorization.ID)).Scan(&revision, &revoked, &deny); err != nil {
		t.Fatal(err)
	}
	if !revoked || deny || revision <= grant.Authorization.Revision {
		t.Error("undo first grant failed to restore absence semantics with monotonic revision")
	}
	phase2RTAtoms(t, phase2RTRecall(t, s, reader, gold.Cases[2].Query), gold.Cases[2].Required, true)
	if _, err := s.GetSource(context.Background(), reader, source.ID, source.Version); err == nil {
		t.Error("undo grant left raw source readable")
	}
	state := phase2RTAuthResult{Authorization: grant.Authorization}
	state.Authorization.Revision = revision
	state.Authorization.Revoked = true
	state = phase2RTUpdatePolicy(t, s, scope, source, state, false)
	state = phase2RTUpdatePolicy(t, s, scope, source, state, true)
	phase2RTAtoms(t, phase2RTRecall(t, s, reader, gold.Cases[2].Query), gold.Cases[2].Required, false)
	expanded, err := s.Expand(context.Background(), reader, memory.ExpandRequest{Refs: []memory.Ref{claim}, Budget: memory.Budget{Tokens: 4000}})
	phase2RTAtoms(t, expanded, gold.Cases[2].Required, false)
	phase2RTEvidence(t, "deny", map[string]any{"claim": claim, "expand_error": err != nil, "revoked_policy": state})
	if !state.Undoable || state.ActionID == "" {
		t.Fatal("explicit revoke undo receipt missing")
	}
	workspaceCommand(t, s, scope, workspace.Command{Type: "undoAction", ID: state.ActionID})
	phase2RTAtoms(t, phase2RTRecall(t, s, reader, gold.Cases[2].Query), gold.Cases[2].Required, true)
	if _, err := s.GetSource(context.Background(), reader, source.ID, source.Version); err != nil {
		t.Fatal("undo revoke did not restore current allow", err)
	}
}

func TestPhase2RuntimePublicRecipientMismatchIsRejected(t *testing.T) {
	s, scope, _ := phase2RTSetup(t)
	source := phase2RTSource(t, s, scope)
	in := memory.SourceAuthorizationRequest{RequestID: string(memory.NewID()), Source: source.Ref, Recipient: memory.Recipient{PrincipalID: "phase2-model", Role: "secretary", Model: "invented-model", RouteFingerprint: strings.Repeat("0", 64)}, Purpose: memory.KnowledgePurpose, Scope: phase2RTUnscoped()}
	w := phase2RTHTTP(t, s, scope, http.MethodPost, "/v1/memory/sources/"+string(source.ID)+"/authorization", in)
	if w.Code < 400 || w.Code >= 500 {
		t.Errorf("mismatching supplied route must reject as client error, got %d", w.Code)
	}
	var policies int
	if err := s.pool.QueryRow(context.Background(), "SELECT count(*) FROM source_authorizations WHERE owner_id=$1", string(scope.OwnerID)).Scan(&policies); err != nil {
		t.Fatal(err)
	}
	if policies != 0 {
		t.Error("mismatched recipient mutation committed policy")
	}
}
