package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

type phase2RTPackage struct {
	RunID           string                 `json:"run_id"`
	AttemptID       memory.ID              `json:"attempt_id"`
	Text            string                 `json:"text"`
	Manifest        memory.ContextManifest `json:"manifest"`
	ExternalReceipt string                 `json:"external_receipt"`
}

func phase2RTPrepareRun(t *testing.T, s *Store, scope memory.Scope, agent, query string) workspace.Run {
	t.Helper()
	state := workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "独立runtime验收任务"})
	thing := state.Tasks[0].ID
	command := workspace.Command{Type: "requestRun", ThingID: thing, AgentID: agent, Kind: "ask", Prompt: query}
	if agent == "manual" {
		command.ManualRecipient = &memory.Recipient{Provider: "phase2-model"}
	}
	state = workspaceCommand(t, s, scope, command)
	for _, run := range state.Runs {
		if run.ThingID == thing {
			phase2RTEvidence(t, "prepared-run", run)
			return run
		}
	}
	t.Fatal("actual requestRun did not persist run")
	return workspace.Run{}
}

func phase2RTStartRunner(t *testing.T, s *Store) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	done := make(chan error, 1)
	go func() { done <- s.RunAgents(ctx, slog.New(slog.NewTextHandler(io.Discard, nil))) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil && !errors.Is(err, context.Canceled) {
				t.Error(err)
			}
		case <-time.After(3 * time.Second):
			t.Error("actual RunAgents did not stop after cancellation")
		}
	})
}

func phase2RTWaitRun(t *testing.T, s *Store, scope memory.Scope, id string) workspace.Run {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		state, err := s.Snapshot(ctx, scope)
		if err != nil {
			t.Fatal(err)
		}
		for _, run := range state.Runs {
			if run.ID == id && (run.Status == "done" || run.Status == "failed") {
				phase2RTEvidence(t, "finished-run", run)
				return run
			}
		}
		select {
		case <-ctx.Done():
			t.Fatal("actual queued run did not reach terminal state")
		case <-ticker.C:
		}
	}
}

func phase2RTGetPackage(t *testing.T, s *Store, scope memory.Scope, run workspace.Run) phase2RTPackage {
	t.Helper()
	w := phase2RTHTTP(t, s, scope, http.MethodGet, "/v1/workspace/runs/"+run.ID+"/package", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("lawful manual package failed: %d %s", w.Code, w.Body.String())
	}
	var wire struct {
		RunID   string                `json:"run_id"`
		Package string                `json:"package"`
		Attempt memory.ContextAttempt `json:"attempt"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &wire); err != nil {
		t.Fatal(err)
	}
	out := phase2RTPackage{RunID: wire.RunID, AttemptID: wire.Attempt.ID, Text: wire.Package, Manifest: wire.Attempt.Manifest, ExternalReceipt: wire.Attempt.ExternalReceipt}
	if wire.Attempt.DeliveredAt == nil || wire.Attempt.DispatchedAt != nil {
		t.Error("actual manual package response lacks PCAS delivery or fabricates network dispatch")
	}
	if out.RunID != run.ID || out.AttemptID == "" || out.Text == "" || out.ExternalReceipt != "unknown" {
		t.Error("manual package identity/payload/unknown receipt incorrect")
	}
	return out
}

func phase2RTSnapshotForSource(t *testing.T, s *Store, scope memory.Scope, source memory.Ref, role, operationID string, payload []byte) memory.ContextAttempt {
	t.Helper()
	diagnostics, ok := any(s).(phase2RTDiagnostics)
	if !ok {
		t.Fatal("actual diagnostic read/cleanup API missing")
	}
	attempts, err := diagnostics.ContextAttempts(context.Background(), scope, operationID)
	if err != nil {
		t.Fatal(err)
	}
	var chosen *memory.ContextAttempt
	for i := range attempts {
		attempt := &attempts[i]
		if attempt.Manifest.Recipient.Role != role {
			continue
		}
		for _, input := range attempt.Manifest.Input {
			if input.Ref == source && (chosen == nil || attempt.CreatedAt.After(chosen.CreatedAt)) {
				chosen = attempt
				break
			}
		}
	}
	if chosen == nil {
		t.Fatal("actual source supply has no diagnostic attempt/input entry")
	}
	actual, err := diagnostics.ContextAttemptSnapshot(context.Background(), scope, chosen.ID)
	if err != nil {
		t.Fatal(err)
	}
	phase2RTEvidence(t, "attempt", chosen)
	phase2RTEvidence(t, "snapshot", map[string]any{"attempt_id": chosen.ID, "snapshot_base64": json.RawMessage(asJSON(actual)), "snapshot_bytes": len(actual), "exact_payload_equal": bytes.Equal(actual, payload)})
	if !bytes.Equal(actual, payload) {
		t.Error("retained final snapshot differs byte-for-byte from actual observed provider/manual payload")
	}
	if chosen.Manifest.InputBytes != len(actual) {
		t.Error("manifest final input byte count does not equal exact retained payload")
	}
	if chosen.Manifest.InputTokens.Method == "actual" && chosen.Manifest.InputTokens.Value == nil {
		t.Error("actual token claim missing measured value")
	}
	if chosen.Manifest.InputTokens.Method != "actual" && chosen.Manifest.InputTokens.Method != "estimated" && chosen.Manifest.InputTokens.Method != "unknown" {
		t.Error("input token count missing actual/estimated/unknown classification")
	}
	candidate := false
	for _, entry := range chosen.Manifest.Candidates {
		if entry.Ref == source {
			candidate = true
		}
	}
	if !candidate {
		t.Error("actual raw source input lost independently recorded retrieval candidate")
	}
	if len(chosen.Manifest.Used) != 0 {
		t.Error("Used=[] synthetic answer was rewritten as memory use")
	}
	if role == "manual" {
		if chosen.DeliveredAt == nil || chosen.ExternalReceipt != "unknown" || chosen.DispatchedAt != nil {
			t.Error("manual PCAS delivery confused with external dispatch/receipt")
		}
		if chosen.Manifest.ObservationLayer != "manual_package" {
			t.Error("manual snapshot recorded as wrong observable layer")
		}
	} else {
		if chosen.DispatchedAt == nil {
			t.Error("actual provider HTTP request has no observed dispatch timestamp")
		}
		if chosen.Manifest.ObservationLayer != "serialized_request" {
			t.Error("observable HTTP snapshot must record serialized-request layer")
		}
	}
	gold := phase2RTGoldRead(t)
	var mapped strings.Builder
	for _, input := range chosen.Manifest.Input {
		if input.Ref != source {
			continue
		}
		if input.SourceSpan == nil || input.SourceSpan.Source != source || !input.SourceSpan.Valid() {
			t.Error("source input lost exact version/rune span")
			continue
		}
		if input.SourceSpan.EndRune > len([]rune(gold.Records[0].Text)) {
			t.Error("source rune span exceeds exact original body")
		}
		if len(input.PayloadSpans) == 0 {
			t.Error("source supply missing final payload byte mapping")
		}
		for _, span := range input.PayloadSpans {
			if span.StartByte < 0 || span.EndByte <= span.StartByte || span.EndByte > len(actual) {
				t.Error("invalid final payload byte span")
				continue
			}
			mapped.Write(actual[span.StartByte:span.EndByte])
		}
	}
	phase2RTAtoms(t, []byte(mapped.String()), gold.Cases[0].Required, true)
	phase2RTAtoms(t, chosen.Manifest, gold.Cases[0].Required, false)
	if strings.Contains(string(asJSON(chosen.Manifest)), "成都预约资料") {
		t.Error("long-lived manifest metadata copied private source title")
	}
	var dependencies int
	if err := s.pool.QueryRow(context.Background(), "SELECT count(*) FROM attempt_typed_dependencies WHERE owner_id=$1 AND attempt_id=$2 AND dependency_id=$3 AND dependency_version=$4 AND dependency_kind='source'", string(scope.OwnerID), string(chosen.ID), string(source.ID), source.Version).Scan(&dependencies); err != nil {
		t.Fatal(err)
	}
	if dependencies != 1 {
		t.Errorf("source input exact typed dependency count=%d want1", dependencies)
	}
	return *chosen
}

func TestPhase2RuntimeZeroClaimNaturalAndPublicActualInputs(t *testing.T) {
	for _, entry := range []string{"natural_DeskTurn", "public_DeskTurn", "public_RunAgents", "public_manual"} {
		t.Run(entry, func(t *testing.T) {
			s, scope, capture := phase2RTSetup(t)
			source := phase2RTSource(t, s, scope)
			gold := phase2RTGoldRead(t)
			var claims, pending int
			if err := s.pool.QueryRow(context.Background(), "SELECT (SELECT count(*) FROM claims WHERE owner_id=$1),(SELECT count(*) FROM memory_jobs WHERE owner_id=$1 AND record_id=$2 AND state='queued')", string(scope.OwnerID), string(source.ID)).Scan(&claims, &pending); err != nil {
				t.Fatal(err)
			}
			phase2RTEvidence(t, "fixture", map[string]any{"source": source.Ref, "claims": claims, "pending_jobs": pending})
			if claims != 0 || pending < 1 {
				t.Fatal("zero-claim pending source condition absent")
			}
			role, agent := "secretary", "phase2-model"
			if entry == "public_RunAgents" {
				role = "deputy"
			}
			if entry == "public_manual" {
				role, agent = "manual", "manual"
			}
			if entry == "natural_DeskTurn" {
				before := capture.count()
				out, err := s.DeskTurn(context.Background(), scope, workspace.DeskTurnRequest{RequestID: string(memory.NewID()), AgentID: agent, Text: "让秘书能用《成都预约资料》"})
				if err != nil {
					t.Fatal(err)
				}
				phase2RTEvidence(t, "natural-authorization", out)
				for i := before; i < capture.count(); i++ {
					phase2RTAtoms(t, capture.request(t, i), gold.Cases[0].Required, false)
				}
			} else {
				phase2RTAuthorize(t, s, scope, source.Ref, agent, role, phase2RTUnscoped())
			}
			before := capture.count()
			var payload []byte
			operationID := string(memory.NewID())
			if strings.HasSuffix(entry, "DeskTurn") {
				out, err := s.DeskTurn(context.Background(), scope, workspace.DeskTurnRequest{RequestID: operationID, AgentID: agent, Text: gold.Cases[0].Query})
				if err != nil {
					t.Fatal(err)
				}
				phase2RTEvidence(t, "desk", out)
				if capture.count() != before+1 {
					t.Errorf("one generation expected, actual request delta=%d", capture.count()-before)
				}
				payload = capture.request(t, before)
			} else {
				run := phase2RTPrepareRun(t, s, scope, agent, gold.Cases[0].Query)
				operationID = run.ID
				if agent == "manual" {
					pkg := phase2RTGetPackage(t, s, scope, run)
					payload = []byte(pkg.Text)
					if capture.count() != before {
						t.Error("manual preparation unexpectedly dispatched external provider")
					}
				}
				if agent != "manual" {
					phase2RTStartRunner(t, s)
					finished := phase2RTWaitRun(t, s, scope, run.ID)
					if finished.Status != "done" {
						t.Error("lawful automated run did not finish")
					}
					payload = capture.request(t, before)
				}
			}
			phase2RTAtoms(t, payload, gold.Cases[0].Required, true)
			phase2RTSnapshotForSource(t, s, scope, source.Ref, role, operationID, payload)
		})
	}
}

func TestPhase2RuntimeManualPackageInvalidationAndRegrantABA(t *testing.T) {
	for _, mutation := range []string{"revoke", "delete"} {
		t.Run(mutation, func(t *testing.T) {
			s, scope, capture := phase2RTSetup(t)
			source := phase2RTSource(t, s, scope)
			grant, _ := phase2RTAuthorize(t, s, scope, source.Ref, "manual", "manual", phase2RTUnscoped())
			run := phase2RTPrepareRun(t, s, scope, "manual", phase2RTGoldRead(t).Cases[0].Query)
			pkg := phase2RTGetPackage(t, s, scope, run)
			phase2RTAtoms(t, []byte(pkg.Text), phase2RTGoldRead(t).Cases[0].Required, true)
			phase2RTSnapshotForSource(t, s, scope, source.Ref, "manual", run.ID, []byte(pkg.Text))
			var revoked phase2RTAuthResult
			if mutation == "revoke" {
				revoked = phase2RTUpdatePolicy(t, s, scope, source.Ref, grant, true)
			} else {
				if err := s.Delete(context.Background(), scope, memory.DeleteRequest{Targets: []memory.Ref{source.Ref}, IncludeSources: true, BlockReimport: true}); err != nil {
					t.Fatal(err)
				}
			}
			diagnostics, ok := any(s).(phase2RTDiagnostics)
			if !ok {
				t.Fatal("actual diagnostic API missing")
			}
			oldSnapshot, snapshotErr := diagnostics.ContextAttemptSnapshot(context.Background(), scope, pkg.AttemptID)
			if len(oldSnapshot) != 0 {
				t.Error("delete/revoke left invalid manual diagnostic body retained")
			}
			phase2RTEvidence(t, "invalid-manual-snapshot", map[string]any{"attempt_id": pkg.AttemptID, "body_bytes": len(oldSnapshot), "read_error": snapshotErr != nil})
			for _, path := range []string{"/v1/workspace/runs/" + run.ID + "/package", "/v1/workspace"} {
				w := phase2RTHTTP(t, s, scope, http.MethodGet, path, nil)
				phase2RTAtoms(t, w.Body.Bytes(), phase2RTGoldRead(t).Cases[0].Required, false)
			}
			exported, err := s.Export(context.Background(), scope, false, false)
			if err != nil {
				t.Fatal(err)
			}
			// Owner raw source remains lawful after revoke. Check only stored run
			// copies; a complete owner export may still contain its original source.
			var exportedObject map[string]json.RawMessage
			if err := json.Unmarshal(exported, &exportedObject); err != nil {
				t.Fatal(err)
			}
			for key, value := range exportedObject {
				if strings.Contains(strings.ToLower(key), "run") {
					phase2RTAtoms(t, []byte(value), phase2RTGoldRead(t).Cases[0].Required, false)
				}
			}
			if mutation == "delete" {
				phase2RTAtoms(t, exported, phase2RTGoldRead(t).Cases[0].Required, false)
			}
			st, err := s.Snapshot(context.Background(), scope)
			if err != nil {
				t.Fatal(err)
			}
			_, err = s.Execute(context.Background(), scope, workspace.Command{Type: "pasteRunResult", ID: run.ID, Output: "synthetic invalid old result", RequestID: string(memory.NewID()), ExpectedRevision: st.Revision})
			if err == nil {
				t.Error("invalid manual result was accepted")
			}
			if mutation == "revoke" {
				phase2RTUpdatePolicy(t, s, scope, source.Ref, revoked, false)
				w := phase2RTHTTP(t, s, scope, http.MethodGet, "/v1/workspace/runs/"+run.ID+"/package", nil)
				phase2RTAtoms(t, w.Body.Bytes(), phase2RTGoldRead(t).Cases[0].Required, false)
				st, err = s.Snapshot(context.Background(), scope)
				if err != nil {
					t.Fatal(err)
				}
				_, err = s.Execute(context.Background(), scope, workspace.Command{Type: "pasteRunResult", ID: run.ID, Output: "synthetic old ABA result", RequestID: string(memory.NewID()), ExpectedRevision: st.Revision})
				if err == nil {
					t.Error("regrant ABA restored old manual result validity")
				}
				fresh := phase2RTPrepareRun(t, s, scope, "manual", phase2RTGoldRead(t).Cases[0].Query)
				phase2RTAtoms(t, []byte(phase2RTGetPackage(t, s, scope, fresh).Text), phase2RTGoldRead(t).Cases[0].Required, true)
			}
			if capture.count() != 0 {
				t.Error("manual lifecycle called external provider")
			}
		})
	}
}
