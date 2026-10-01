package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// The secretary's controlled output contains a source atom absent from the
// owner's question. Used=[] deliberately cannot erase actual input lineage.
func phase2RTDelegateFromSource(t *testing.T, s *Store, scope memory.Scope, capture *phase2RTCapture, source memory.Ref) workspace.DeskTurnResponse {
	t.Helper()
	gold := phase2RTGoldRead(t)
	atom := gold.Records[0].Atoms[0]
	query := gold.Cases[0].Query + "请让副手整理这份资料的安排"
	if strings.Contains(query, atom) {
		t.Fatal("delegation question must not contain the source atom")
	}
	capture.mu.Lock()
	capture.Reply = string(asJSON(map[string]any{
		"reply": "DELEGATE-RESULT-893 " + atom, "used": []any{}, "links": []any{}, "show": []any{}, "remember": false, "ask": nil,
		"actions": []any{map[string]any{"op": "delegate", "ref": "new", "kind": "draft", "title": "副手安排 " + atom, "prompt": "整理成都预约资料，保留预约码 " + atom}},
	}))
	capture.mu.Unlock()
	request := workspace.DeskTurnRequest{RequestID: string(memory.NewID()), AgentID: "phase2-model", Text: query}
	before := capture.count()
	out, err := s.DeskTurn(context.Background(), scope, request)
	phase2RTEvidence(t, "source-derived-delegation", map[string]any{"request": request, "source": source, "response": out, "error": err != nil})
	if err != nil {
		t.Fatal(err)
	}
	if capture.count() != before+1 {
		t.Fatalf("secretary positive control expected one request, actual delta=%d", capture.count()-before)
	}
	payload := capture.request(t, before)
	phase2RTAtoms(t, payload, gold.Cases[0].Required, true)
	phase2RTSnapshotForSource(t, s, scope, source, "secretary", request.RequestID, payload)
	return out
}

// JSON reads the root-approved server fields while this independent harness
// compiles on the earlier baseline. No client command can supply these fields.
type phase2RTDeskActionWire struct {
	DeskActions []memory.ID `json:"desk_actions"`
}

func phase2RTDeskActionIDs(t *testing.T, run workspace.Run) []memory.ID {
	t.Helper()
	var wire struct {
		Actions []memory.ID             `json:"contextDeskActions"`
		Task    *phase2RTDeskActionWire `json:"contextTask"`
	}
	if err := json.Unmarshal(asJSON(run), &wire); err != nil {
		t.Fatal(err)
	}
	if len(wire.Actions) == 0 || wire.Task == nil {
		t.Fatal("real queued Run has no committed action origins/task")
	}
	left, right := append([]memory.ID(nil), wire.Actions...), append([]memory.ID(nil), wire.Task.DeskActions...)
	sort.Slice(left, func(i, j int) bool { return left[i] < left[j] })
	sort.Slice(right, func(i, j int) bool { return right[i] < right[j] })
	if string(asJSON(left)) != string(asJSON(right)) {
		t.Fatal("Run and trusted Task disagree on actual action origins", left, right)
	}
	return wire.Actions
}

func phase2RTOriginDeskTurn(t *testing.T, s *Store, scope memory.Scope, capture *phase2RTCapture, source memory.Ref, path string) workspace.DeskTurnResponse {
	t.Helper()
	gold := phase2RTGoldRead(t)
	atom := gold.Records[0].Atoms[0]
	actions := []any{map[string]any{"op": "delegate", "ref": "new", "title": "D4 副手安排 " + atom, "prompt": "整理资料预约码 " + atom, "kind": "draft"}}
	if path == "create_N1_delegate" {
		actions = []any{
			map[string]any{"op": "create_task", "title": "D4 新安排 " + atom, "notes": "来自资料的预约码 " + atom},
			map[string]any{"op": "delegate", "ref": "N1", "prompt": "整理资料预约码 " + atom, "kind": "draft"},
		}
	}
	capture.mu.Lock()
	capture.Reply = string(asJSON(map[string]any{"reply": "D4-DERIVED-RESULT-729 " + atom, "used": []any{}, "links": []any{}, "show": []any{}, "remember": false, "ask": nil, "actions": actions}))
	capture.mu.Unlock()
	request := workspace.DeskTurnRequest{RequestID: string(memory.NewID()), AgentID: "phase2-model", Text: gold.Cases[0].Query + "请让副手整理这份资料的安排"}
	if strings.Contains(request.Text, atom) {
		t.Fatal("D4 owner question contains source atom")
	}
	before := capture.count()
	out, err := s.DeskTurn(context.Background(), scope, request)
	if err != nil || capture.count() != before+1 {
		t.Fatal("D4 actual secretary request absent", err, capture.count()-before)
	}
	payload := capture.request(t, before)
	phase2RTAtoms(t, payload, gold.Cases[0].Required, true)
	phase2RTSnapshotForSource(t, s, scope, source, "secretary", request.RequestID, payload)
	phase2RTEvidence(t, "D4-secretary-"+request.RequestID, map[string]any{"request": request, "response": out, "path": path})
	return out
}

func phase2RTOriginReceipts(t *testing.T, s *Store, scope memory.Scope, out workspace.DeskTurnResponse, source memory.Ref, secretary phase2RTAuthResult, path string) []memory.ID {
	t.Helper()
	var ids []memory.ID
	var create, delegate int
	for _, receipt := range out.Turn.Receipts {
		if receipt.Op != "create_task" && receipt.Op != "delegate" {
			continue
		}
		if receipt.Status != "done" || receipt.ActionID == nil || !memory.ID(*receipt.ActionID).Valid() {
			t.Fatal("D4 origin is not a successful committed action receipt", receipt)
		}
		id := memory.ID(*receipt.ActionID)
		ids = append(ids, id)
		if receipt.Op == "create_task" {
			create++
		} else {
			delegate++
		}
		var turnID string
		var raw []byte
		if err := s.pool.QueryRow(context.Background(), "SELECT turn_id::text,context_task FROM action_log WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), string(id)).Scan(&turnID, &raw); err != nil || turnID != out.Turn.ID {
			t.Fatal("action origin ID not owned by the actual committed secretary turn", err, turnID, id)
		}
		var task memory.TrustedTaskContext
		if err := json.Unmarshal(raw, &task); err != nil || task.Recipient != secretary.Authorization.Recipient {
			t.Fatal("action origin fabricated or lost original secretary recipient", err, task.Recipient)
		}
		var stamped int
		if err := s.pool.QueryRow(context.Background(), `SELECT count(*) FROM context_artifact_dependencies WHERE owner_id=$1 AND parent_kind='artifact' AND parent_id=$2 AND parent_version=1 AND dependency_id=$3 AND dependency_version=$4 AND dependency_kind='source' AND policy_id=$5 AND policy_revision=$6`, string(scope.OwnerID), string(id), string(source.ID), source.Version, string(secretary.Authorization.ID), secretary.Authorization.Revision).Scan(&stamped); err != nil || stamped != 1 {
			t.Fatal("real action origin lacks durable exact source and original secretary policy stamp", err, stamped)
		}
	}
	if delegate != 1 || path == "delegate_new" && create != 0 || path == "create_N1_delegate" && create != 1 {
		t.Fatal("D4 successful action receipt positive control changed", path, create, delegate)
	}
	return ids
}

func phase2RTRequireActualOrigins(t *testing.T, run workspace.Run, required, knownParents []memory.ID) []memory.ID {
	t.Helper()
	ids := phase2RTDeskActionIDs(t, run)
	known := map[memory.ID]bool{}
	for _, id := range append(append([]memory.ID(nil), knownParents...), required...) {
		known[id] = true
	}
	found := map[memory.ID]bool{}
	for _, id := range ids {
		if !id.Valid() || !known[id] || found[id] {
			t.Fatal("Run origin is fabricated/unrelated/duplicated instead of successful actual action", id, ids)
		}
		found[id] = true
	}
	for _, id := range required {
		if !found[id] {
			t.Fatal("final queued Run failed to bind this turn's committed create/delegate action", id, ids)
		}
	}
	return ids
}

func TestPhase2RuntimeQueuedDelegationRequiresOriginalSecretaryActions(t *testing.T) {
	for _, path := range []string{"delegate_new", "create_N1_delegate"} {
		t.Run(path, func(t *testing.T) {
			s, scope, capture := phase2RTSetup(t)
			ctx := context.Background()
			gold := phase2RTGoldRead(t)
			source := phase2RTSource(t, s, scope)
			secretary, _ := phase2RTAuthorize(t, s, scope, source.Ref, "phase2-model", "secretary", phase2RTUnscoped())
			deputy, _ := phase2RTAuthorize(t, s, scope, source.Ref, "phase2-model", "deputy", phase2RTUnscoped())
			first := phase2RTOriginDeskTurn(t, s, scope, capture, source.Ref, path)
			firstIDs := phase2RTOriginReceipts(t, s, scope, first, source.Ref, secretary, path)
			complete := phase2RTDelegatedRun(t, first)
			origins := phase2RTRequireActualOrigins(t, complete, firstIDs, nil)
			if err := s.runAgentOnce(ctx); err != nil {
				t.Fatal(err)
			}
			finished := phase2RTWaitRun(t, s, scope, complete.ID)
			if finished.Status != "done" || finished.Adopted == nil || finished.Output != "D4-DERIVED-RESULT-729 "+gold.Records[0].Atoms[0] {
				t.Fatal("D4 real completed result/adoption positive control absent", finished)
			}
			attempt := phase2RTSnapshotForSource(t, s, scope, source.Ref, "deputy", complete.ID, capture.request(t, 1))
			phase2RTDelegationLineage(t, attempt, source.Ref, deputy)
			var manifest phase2RTDeskActionWire
			if err := json.Unmarshal(asJSON(attempt.Manifest), &manifest); err != nil {
				t.Fatal(err)
			}
			manifestIDs := append([]memory.ID(nil), manifest.DeskActions...)
			sort.Slice(manifestIDs, func(i, j int) bool { return manifestIDs[i] < manifestIDs[j] })
			frozenIDs := append([]memory.ID(nil), origins...)
			sort.Slice(frozenIDs, func(i, j int) bool { return frozenIDs[i] < frozenIDs[j] })
			if string(asJSON(manifestIDs)) != string(asJSON(frozenIDs)) {
				t.Fatal("actual manifest failed to freeze real Run action origins", manifestIDs, frozenIDs)
			}
			undoAutoAdoption(t, s, scope, complete.ID)
			second := phase2RTOriginDeskTurn(t, s, scope, capture, source.Ref, path)
			secondIDs := phase2RTOriginReceipts(t, s, scope, second, source.Ref, secretary, path)
			queued := phase2RTDelegatedRun(t, second)
			queueOrigins := phase2RTRequireActualOrigins(t, queued, secondIDs, firstIDs)
			var status string
			if err := s.pool.QueryRow(ctx, "SELECT status FROM agent_runs WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), queued.ID).Scan(&status); err != nil || status != "queued" || capture.count() != 3 {
				t.Fatal("D4 work was not truly queued before mutation", err, status, capture.count())
			}
			phase2RTUpdatePolicy(t, s, scope, source.Ref, secretary, true)
			// The destination remains independently allowed. Prove the policy
			// itself, its actual raw read, and later HTTP new-work positive control.
			policyResponse := phase2RTHTTP(t, s, scope, http.MethodGet, "/v1/memory/sources/"+string(source.ID)+"/authorization", nil)
			var policies struct {
				Authorizations []memory.SourceAuthorization `json:"authorizations"`
			}
			if err := json.Unmarshal(policyResponse.Body.Bytes(), &policies); err != nil || policyResponse.Code != http.StatusOK {
				t.Fatal("D4 real policy read failed", err, policyResponse.Code)
			}
			allow := false
			for _, policy := range policies.Authorizations {
				if policy.ID == deputy.Authorization.ID {
					allow = !policy.Revoked && policy.Revision == deputy.Authorization.Revision && policy.Recipient == deputy.Authorization.Recipient
				}
			}
			if !allow {
				t.Fatal("D4 accidentally revoked/changed deputy's own canonical source policy")
			}
			reader := phase2RTTask(t, s, scope, "phase2-model", "deputy", phase2RTUnscoped())
			original, err := s.GetSource(ctx, reader, source.ID, source.Version)
			if err != nil || original.Source.Text != gold.Records[0].Text {
				t.Fatal("D4 deputy raw authorization positive control absent", err)
			}
			if err := s.runAgentOnce(ctx); err != nil {
				t.Fatal(err)
			}
			if capture.count() != 3 {
				t.Fatal("queued work dispatched after original secretary action authorization failed", capture.count())
			}
			state, err := s.Snapshot(ctx, scope)
			if err != nil {
				t.Fatal(err)
			}
			for _, old := range state.Runs {
				if old.ID == queued.ID || old.ID == complete.ID {
					if !old.StaleContext || old.Output != "" || old.Brief != "" || strings.Contains(old.Prompt, gold.Records[0].Atoms[0]) || old.Adopted != nil {
						t.Error("old action-origin work remains readable/applicable despite secretary-only revoke", old)
					}
				}
			}
			_, err = s.Execute(ctx, scope, workspace.Command{Type: "adoptRun", ID: complete.ID, As: "doc", Text: finished.Output, RequestID: string(memory.NewID()), ExpectedRevision: state.Revision})
			if !errors.Is(err, memory.ErrConflict) && !errors.Is(err, memory.ErrForbidden) {
				t.Fatal("completed old-origin result could be adopted or returned uncontrolled error", err)
			}
			phase2RTEvidence(t, "D4-secretary-only-revoke", map[string]any{"path": path, "completed_run": complete.ID, "queued_run": queued.ID, "completed_origins": origins, "queued_origins": queueOrigins, "deputy_policy_still_allow": deputy.Authorization, "state": state, "queued_http_requests": capture.count() - 3})
			capture.mu.Lock()
			capture.Reply = string(asJSON(map[string]any{"reply": "D4-NEW-INDEPENDENT-RESULT-730", "used": []any{}}))
			capture.mu.Unlock()
			fresh := phase2RTPrepareRun(t, s, scope, "phase2-model", gold.Cases[0].Query)
			if err := s.runAgentOnce(ctx); err != nil {
				t.Fatal(err)
			}
			newResult := phase2RTWaitRun(t, s, scope, fresh.ID)
			if newResult.Status != "done" || newResult.Output != "D4-NEW-INDEPENDENT-RESULT-730" || capture.count() != 4 || fresh.ID == complete.ID || fresh.ID == queued.ID {
				t.Fatal("independently lawful deputy source question was lost after secretary-only revoke", newResult, capture.count())
			}
			payload := capture.request(t, 3)
			phase2RTAtoms(t, payload, gold.Cases[0].Required, true)
			phase2RTSnapshotForSource(t, s, scope, source.Ref, "deputy", fresh.ID, payload)
		})
	}
}

func phase2RTDelegatedRun(t *testing.T, out workspace.DeskTurnResponse) workspace.Run {
	t.Helper()
	var id string
	for _, receipt := range out.Turn.Receipts {
		if receipt.Op == "delegate" && receipt.Status != "skipped" && receipt.ThingID != nil {
			id = *receipt.ThingID
		}
	}
	if id == "" {
		t.Fatalf("lawful delegation did not commit a real target: %+v", out.Turn.Receipts)
	}
	for _, run := range out.State.Runs {
		if run.ThingID == id && run.AgentID == "phase2-model" {
			return run
		}
	}
	t.Fatal("committed delegation has no actual queued run")
	return workspace.Run{}
}

func phase2RTDelegationLineage(t *testing.T, attempt memory.ContextAttempt, source memory.Ref, grant phase2RTAuthResult) {
	t.Helper()
	found := false
	for _, dep := range attempt.Manifest.IndirectDependencies {
		if dep.Ref != source {
			continue
		}
		found = true
		if dep.Authorization == nil || dep.Authorization.PolicyID != grant.Authorization.ID || dep.Authorization.Revision != grant.Authorization.Revision || dep.Purpose != grant.Authorization.Purpose || dep.Scope != grant.Authorization.Scope {
			t.Error("action lineage lacks the deputy's own current authorization stamp")
		}
	}
	if !found {
		t.Error("secretary-derived title/prompt lost exact source indirect lineage, including when direct Input overlaps")
	}
	if attempt.Manifest.Recipient != grant.Authorization.Recipient {
		t.Error("deputy attempt borrowed secretary recipient authorization")
	}
}

func TestPhase2RuntimeSourceDerivedDelegationRoleIsolationAndABA(t *testing.T) {
	for _, sequence := range []string{"D1_secretary_only", "D2_both_roles", "D3_revoke_and_regrant"} {
		t.Run(sequence, func(t *testing.T) {
			s, scope, capture := phase2RTSetup(t)
			gold := phase2RTGoldRead(t)
			source := phase2RTSource(t, s, scope)
			secretary, _ := phase2RTAuthorize(t, s, scope, source.Ref, "phase2-model", "secretary", phase2RTUnscoped())
			var deputy phase2RTAuthResult
			if sequence != "D1_secretary_only" {
				deputy, _ = phase2RTAuthorize(t, s, scope, source.Ref, "phase2-model", "deputy", phase2RTUnscoped())
				if deputy.Authorization.ID == secretary.Authorization.ID {
					t.Fatal("two roles must have independent exact policies")
				}
			}
			out := phase2RTDelegateFromSource(t, s, scope, capture, source.Ref)
			if sequence == "D1_secretary_only" {
				denied := false
				for _, receipt := range out.Turn.Receipts {
					denied = denied || receipt.Op == "delegate" && receipt.Status == "skipped"
				}
				if !denied {
					t.Error("missing deputy raw authorization did not reject delegation")
				}
				if len(out.State.Runs) != 0 {
					phase2RTStartRunner(t, s)
					for _, run := range out.State.Runs {
						finished := phase2RTWaitRun(t, s, scope, run.ID)
						if finished.Status == "done" || finished.Adopted != nil {
							t.Error("unauthorized source-derived delegation produced an applicable result")
						}
					}
				}
				if capture.count() != 1 {
					t.Errorf("deputy must dispatch zero HTTP requests, total secretary+deputy=%d", capture.count())
				}
				if strings.Contains(string(asJSON(out.State.Tasks)), gold.Records[0].Atoms[0]) || strings.Contains(string(asJSON(out.State.Runs)), gold.Records[0].Atoms[0]) {
					t.Error("blocked delegation persisted raw title/prompt as unrestricted current work")
				}
				return
			}
			run := phase2RTDelegatedRun(t, out)
			phase2RTStartRunner(t, s)
			finished := phase2RTWaitRun(t, s, scope, run.ID)
			if finished.Status != "done" || finished.Output != "DELEGATE-RESULT-893 "+gold.Records[0].Atoms[0] || finished.Adopted == nil {
				t.Fatalf("authorized delegated result/adoption positive control absent: %+v", finished)
			}
			if capture.count() != 2 {
				t.Fatalf("one secretary and one deputy request expected, actual=%d", capture.count())
			}
			payload := capture.request(t, 1)
			phase2RTAtoms(t, payload, []string{gold.Records[0].Atoms[0]}, true)
			attempt := phase2RTSnapshotForSource(t, s, scope, source.Ref, "deputy", run.ID, payload)
			phase2RTDelegationLineage(t, attempt, source.Ref, deputy)
			durable := false
			for _, dep := range finished.ContextDependencies {
				if dep.Ref == source.Ref && dep.Authorization != nil && dep.Authorization.PolicyID == deputy.Authorization.ID && dep.Authorization.Revision == deputy.Authorization.Revision {
					durable = true
				}
			}
			if !durable {
				t.Error("completed delegated run lost durable exact source dependency with its own policy")
			}
			var claims int
			if err := s.pool.QueryRow(context.Background(), "SELECT count(*) FROM claims WHERE owner_id=$1", string(scope.OwnerID)).Scan(&claims); err != nil || claims != 0 {
				t.Fatal("delegation control must not mint claims to authorize source", claims, err)
			}
			if sequence == "D2_both_roles" {
				return
			}

			// Undo real auto-adoption before denial, so an existing adoption cannot
			// be the independent reason an old-result adoption fails.
			undoAutoAdoption(t, s, scope, run.ID)
			revoked := phase2RTUpdatePolicy(t, s, scope, source.Ref, deputy, true)
			for _, stage := range []string{"revoked", "regranted"} {
				if stage == "regranted" {
					deputy = phase2RTUpdatePolicy(t, s, scope, source.Ref, revoked, false)
				}
				reader := phase2RTTask(t, s, scope, "phase2-model", "secretary", phase2RTUnscoped())
				original, err := s.GetSource(context.Background(), reader, source.ID, source.Version)
				if err != nil || original.Source.Text != gold.Records[0].Text {
					t.Fatal(stage+": deputy revoke changed independent secretary permission", err)
				}
				state, err := s.Snapshot(context.Background(), scope)
				if err != nil {
					t.Fatal(err)
				}
				for _, old := range state.Runs {
					if old.ID == run.ID && (strings.Contains(old.Output, "DELEGATE-RESULT-893") || strings.Contains(old.Brief, gold.Records[0].Atoms[0])) {
						t.Error(stage + ": old delegated body exposed by current read")
					}
				}
				exported := phase2RTExportState(t, s, scope)
				for _, old := range exported.Runs {
					if old.ID == run.ID && strings.Contains(string(asJSON(old)), "DELEGATE-RESULT-893") {
						t.Error(stage + ": export revived old delegated output")
					}
				}
				_, err = s.Execute(context.Background(), scope, workspace.Command{Type: "adoptRun", ID: run.ID, As: "doc", Text: finished.Output, RequestID: string(memory.NewID()), ExpectedRevision: state.Revision})
				adoptionErr := err
				if !errors.Is(err, memory.ErrForbidden) && !errors.Is(err, memory.ErrConflict) {
					t.Errorf("%s: old adoption must be controlled forbidden/stale conflict, actual=%v", stage, err)
				}
				attempts, err := s.ContextAttempts(context.Background(), scope, run.ID)
				if err != nil {
					t.Fatal(err)
				}
				invalid := false
				for _, old := range attempts {
					if old.ID == attempt.ID {
						invalid = old.State == memory.AttemptInvalidated
						body, err := s.ContextAttemptSnapshot(context.Background(), scope, old.ID)
						if !errors.Is(err, memory.ErrUnavailable) || len(body) != 0 {
							t.Error("revoked delegated diagnostic body remains readable", err)
						}
					}
				}
				if !invalid {
					t.Error(stage + ": original deputy attempt did not remain invalidated")
				}
				phase2RTEvidence(t, "delegation-"+stage, map[string]any{"run_id": run.ID, "attempts": attempts, "adoption_error": adoptionErr != nil, "state": state})
			}
			fresh := phase2RTPrepareRun(t, s, scope, "phase2-model", gold.Cases[0].Query)
			newResult := phase2RTWaitRun(t, s, scope, fresh.ID)
			if fresh.ID == run.ID || newResult.Status != "done" {
				t.Fatal("new work after regrant cannot rebuild lawful current context")
			}
			phase2RTSnapshotForSource(t, s, scope, source.Ref, "deputy", fresh.ID, capture.request(t, 2))
			currentStamp := false
			for _, dep := range newResult.ContextDependencies {
				if dep.Ref == source.Ref {
					currentStamp = dep.Authorization != nil && dep.Authorization.PolicyID == deputy.Authorization.ID && dep.Authorization.Revision == deputy.Authorization.Revision
				}
			}
			if !currentStamp {
				t.Error("new current input uses pre-regrant authorization revision")
			}
			phase2RTEvidence(t, "delegation-new-work", json.RawMessage(asJSON(newResult)))
		})
	}
}
