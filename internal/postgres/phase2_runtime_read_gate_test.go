package postgres

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func phase2RTDerivedViewsContain(state workspace.State, atom string) bool {
	return strings.Contains(string(asJSON(state.Runs)), atom) || strings.Contains(string(asJSON(state.Docs)), atom) || strings.Contains(string(asJSON(state.Samples)), atom)
}

func phase2RTExportState(t *testing.T, s *Store, scope memory.Scope) workspace.State {
	t.Helper()
	body, err := s.Export(context.Background(), scope, false, false)
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		State workspace.State `json:"state"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	phase2RTEvidence(t, "export-derived-view", out.State)
	return out.State
}

func TestPhase2RuntimeStateExportAndDerivedBlocksRecheckCurrentRecipient(t *testing.T) {
	for _, mutation := range []string{"route", "disabled"} {
		t.Run(mutation, func(t *testing.T) {
			s, scope, capture := phase2RTSetup(t)
			source := phase2RTSource(t, s, scope)
			phase2RTAuthorize(t, s, scope, source.Ref, "phase2-model", "deputy", phase2RTUnscoped())
			derived := phase2RTDerivedReply(t, capture)
			run := phase2RTPrepareRun(t, s, scope, "phase2-model", phase2RTGoldRead(t).Cases[0].Query)
			phase2RTStartRunner(t, s)
			finished := phase2RTWaitRun(t, s, scope, run.ID)
			if finished.Status != "done" || !strings.Contains(finished.Output, derived) {
				t.Fatal("positive source-derived real result absent")
			}
			state := workspaceCommand(t, s, scope, workspace.Command{Type: "adoptRun", ID: run.ID, As: "doc", Text: finished.Output})
			var docID string
			for _, doc := range state.Docs {
				if doc.RunID == run.ID && strings.Contains(doc.Body, derived) {
					docID = doc.ID
				}
			}
			if docID == "" {
				t.Fatal("actual adoption did not create positive derived document")
			}
			var samples []string
			for _, sample := range state.Samples {
				if sample.Origin.RunID == run.ID && strings.Contains(sample.Response, derived) {
					samples = append(samples, sample.ID)
				}
			}
			if len(samples) == 0 {
				t.Fatal("actual derived training sample absent")
			}
			for _, id := range samples {
				workspaceCommand(t, s, scope, workspace.Command{Type: "setSampleState", ID: id, State: "included"})
			}
			training, err := s.Export(context.Background(), scope, true, false)
			if err != nil || !strings.Contains(string(training), derived) {
				t.Fatal("positive authorized training export absent", err)
			}
			original := "OWNER-ORIGINAL-DOC-918"
			originalID := string(memory.NewID())
			workspaceCommand(t, s, scope, workspace.Command{Type: "createDoc", Doc: &workspace.Doc{ID: originalID, ThingID: run.ThingID, Title: "原始用户文档", Body: original, By: "user"}})
			if mutation == "route" {
				capture.Registry.Config.Providers[0].Model = "phase2-route-rebound"
			} else {
				workspaceCommand(t, s, scope, workspace.Command{Type: "updateAgent", ID: "phase2-model", Patch: asJSON(map[string]bool{"enabled": false})})
			}
			state, err = s.Snapshot(context.Background(), scope)
			if err != nil {
				t.Fatal(err)
			}
			if phase2RTDerivedViewsContain(state, derived) || phase2RTDerivedViewsContain(phase2RTExportState(t, s, scope), derived) {
				t.Error("route-only/disable change exposed obsolete source-derived Run/Doc/Sample without source mutation")
			}
			originalFound := false
			for _, doc := range state.Docs {
				if doc.ID == originalID && doc.Body == original {
					originalFound = true
				}
			}
			if !originalFound {
				t.Error("derived read gate hid unrelated original owner document")
			}
			training, err = s.Export(context.Background(), scope, true, false)
			if err != nil || strings.Contains(string(training), derived) {
				t.Error("training export bypassed current recipient validation", err)
			}
			audit, err := s.GetSource(context.Background(), scope, source.ID, source.Version)
			if err != nil || audit.Source.Text != phase2RTGoldRead(t).Records[0].Text {
				t.Error("derived gate changed lawful owner original source audit", err)
			}
			phase2RTEvidence(t, "read-gate", map[string]any{"mutation": mutation, "run_id": run.ID, "derived_doc_id": docID, "derived_sample_ids": samples, "original_doc_retained": originalFound, "state": state, "source_audit_ref": audit.Source.Ref, "source_mutation": "none"})
		})
	}
}

func TestPhase2RuntimeLegacyPureClaimReadControlAndExplicitDeny(t *testing.T) {
	s, scope, capture := phase2RTSetup(t)
	gold := phase2RTGoldRead(t)
	source, claim := phase2RTClaimFixture(t, s, scope, gold.Records[1].Text, gold.Records[1].Claim, "fact")
	derived := phase2RTDerivedReply(t, capture)
	run := phase2RTPrepareRun(t, s, scope, "phase2-model", gold.Cases[2].Query)
	phase2RTStartRunner(t, s)
	finished := phase2RTWaitRun(t, s, scope, run.ID)
	if finished.Status != "done" || !strings.Contains(finished.Output, derived) {
		t.Fatal("pure-claim actual result positive control absent")
	}
	claimFound := false
	for _, ref := range finished.ContextVersions {
		if ref.Kind != memory.ClaimKind {
			t.Fatal("legacy pure-claim fixture contains raw or nonclaim dependency", ref)
		}
		if ref == claim {
			claimFound = true
		}
	}
	if !claimFound {
		t.Fatal("legacy control lacks exact independently authorized claim")
	}
	// The single SQL adaptation models an old, verifiable claim-only row. It
	// preserves the actual output and exact claim refs without inventing a route.
	legacy := finished
	legacy.ContextTask, legacy.ContextDependencies, legacy.ContextCandidates, legacy.ContextSourceSpans = nil, nil, nil, nil
	legacy.ContextAttemptID = ""
	if _, err := s.pool.Exec(context.Background(), "UPDATE agent_runs SET document=$3 WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), run.ID, asJSON(legacy)); err != nil {
		t.Fatal(err)
	}
	capture.Registry.Config.Providers[0].Model = "phase2-current-claim-route"
	state, err := s.Snapshot(context.Background(), scope)
	if err != nil || !phase2RTDerivedViewsContain(state, derived) || !phase2RTDerivedViewsContain(phase2RTExportState(t, s, scope), derived) {
		t.Error("lawful legacy pure-claim read was lost under server current task validation", err)
	}
	policy, _ := phase2RTAuthorize(t, s, scope, source, "phase2-model", "deputy", phase2RTUnscoped())
	denied := phase2RTUpdatePolicy(t, s, scope, source, policy, true)
	state, err = s.Snapshot(context.Background(), scope)
	if err != nil || phase2RTDerivedViewsContain(state, derived) || phase2RTDerivedViewsContain(phase2RTExportState(t, s, scope), derived) {
		t.Error("explicit source deny exposed evidence-derived legacy claim result", err)
	}
	phase2RTUpdatePolicy(t, s, scope, source, denied, false)
	state, err = s.Snapshot(context.Background(), scope)
	if err != nil || phase2RTDerivedViewsContain(state, derived) {
		t.Error("regrant reconstructed an old body already cleared by invalidation", err)
	}
	phase2RTEvidence(t, "legacy-claim-read", map[string]any{"run_id": run.ID, "claim": claim, "source": source, "legacy_fixture": "actual output/claim refs; ContextTask nil, no fabricated historical recipient", "state_after_regrant": state, "actual_provider_requests": capture.count()})
}
