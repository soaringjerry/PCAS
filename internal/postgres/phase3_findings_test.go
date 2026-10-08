package postgres

import (
	"encoding/json"
	"os"
	"testing"
)

// Finding inventory at integration a6ba66a (no phase3 skeleton or implementation).
// These are missing-capability blockers, not diagnoses of the pending backend/UI.
// The coordinator assigns fixes/removes gates. PCAS_PHASE3_RUN_FINDINGS=1 runs
// assertion bodies and exposes the real failure; skipped cases are NOT passes.
//
// S-P3-001 H1-H8/G1-G7: no project handover reader, scheduler, processor or budget.
// S-P3-002 D1-D7: writes replace work_documents.document; no version/diff readers,
// revise work, retained version deletion or revision-adoption rollback.
// S-P3-003 S1-S3: project handover and actual document-version/handover provenance
// absent from studio secretary/deputy work records; P0 conversation context pending.
// S-P3-004 F1-F5: no per-item file interface or project source scope. Existing
// attachment mechanism alone does not certify upload confirmation/preview/retry.
// S-P3-005 P1-P4/G1-G7: no effort stage/budget, user authority, start date or timeline.
// S-P3-006 U/T5: studio still edits progress/nextSteps; no three-part project
// handover, version browser/diff, files or start-date timeline. Golden paths 4/5
// and opening-to-readable timing cannot be certified on this integration.
//
// Reproduced on a6ba66a, 2026-10-07 (all DBs freshly owned/disposable):
//
//	PCAS_PHASE3_RUN_FINDINGS=1 go test ./internal/postgres -timeout 30m -v
//	  -run '^TestPhase3T2G6T4ReadOnlyHTTPScale/(handover|versions|diff|timeline|files)$'
//	Handover/versions/timeline/files returned HTTP 404; no timings certified.
//	PCAS_PHASE3_RUN_FINDINGS=1 go test ./internal/postgres -timeout 30m -v
//	  -run '^TestPhase3H7'
//	"progress adoption is not one project memory": actual count=0.
//	node web/tests/phase3_browser_run.mjs --boundary
//	Project page rendered legacy progress; "结论" not visible within 9 seconds.
//	This boundary probe proves missing U only, not a golden-path replay pass.
//
// Passed evidence: initial suite 581.578s (new capability cases SKIPPED);
// five existing stages all had held provider + actual HTTP + scheduler overlap;
// G3 classification/global handover durable-result retry after Store restart;
// old-stage actual final-slot limits; extended T1 cardinalities/content; workspace
// latest 12 reads 135.925..163.587 ms, all public table content/xmin unchanged.
// Latest T1 + five-stage overlap + read-only group rerun passed in 248.156s;
// five new read cases skipped explicitly, every old stage paid exactly once.
// New-stage names/model-wire translation and future UI selectors are adapters,
// not frozen behavior. P scheduler/processor signatures are not yet published.
// Version-retention and project-file-scope checks stay gated: the initial
// integration's fixture loader cannot create capabilities that do not exist.
// T4 single-memory regression: actual comparison calls=40; 506 unaffected batch
// receipts unchanged; processing elapsed=145.162s (setup excluded).
// Forced seven-budget assertion found no project_handover stage in the baseline.
// Neither fake providers nor boundary UI mocks certify real-channel model quality.
// New PRs #262/#263/#264 are pending; findings concern the integration baseline,
// not a verdict on those unmerged PRs. E and online checks belong to coordinator.
//
// Reserved ceilings sum to 138; 140 is the global ceiling, with the same two
// unassigned calls from 2.6. Spare global capacity never permits borrowing.
func phase3Finding(t *testing.T, id string) {
	t.Helper()
	if os.Getenv("PCAS_PHASE3_RUN_FINDINGS") == "1" {
		return
	}
	switch id {
	case "S-P3-001":
		t.Skip("finding S-P3-001")
	case "S-P3-002":
		t.Skip("finding S-P3-002")
	case "S-P3-003":
		t.Skip("finding S-P3-003")
	case "S-P3-004":
		t.Skip("finding S-P3-004")
	case "S-P3-005":
		t.Skip("finding S-P3-005")
	case "S-P3-006":
		t.Skip("finding S-P3-006")
	default:
		t.Fatalf("unknown finding %s", id)
	}
}
func decodePhase3(t *testing.T, raw []byte, out any) {
	t.Helper()
	if err := json.Unmarshal(raw, out); err != nil {
		t.Fatal(err)
	}
}
