package postgres

import (
	"context"
	"encoding/json"
	"errors"
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
