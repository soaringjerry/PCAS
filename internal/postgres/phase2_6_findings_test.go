package postgres

import (
	"encoding/json"
	"os"
	"testing"
)

// Findings at integrations 5c2479a and e1e6271. Skips are OUTSTANDING acceptance blockers,
// never passes. Reproduce with PCAS_PHASE26_RUN_FINDINGS=1; the coordinator owns
// implementation changes and removal of the finding gates (process.md section 2).
//
// S-P26-001 A2/A9: all five actual subscription calls settle estimated positive
// charges and daily budget blocks all five stages without another provider call.
// Health has deferred=1 per stage but no budget_deferred reason anywhere.
//
// S-P26-002 T2/A2/A8/043: with ANALYZE statistics, scheduler+processor+snapshot+
// library+command+secretary overlap repeatedly returns background_write_busy in
// alias scan/handover scheduling. First analyzed round: 8 and 12 scheduler errors;
// user requests ~100-300ms (reads/writes) and ~2s secretary. SQL logs identify
// workspace_owners FOR KEY SHARE 100ms timeouts. A 1200-member positive merge
// initially yields on aliases INSERT -> bump_library_version owner-row UPDATE.
// Paid output remains cached. The realistic worker-style retry test additionally
// measures completion under continuing user load; any failure remains outstanding.
// No SQLSTATE 40P01 observed so far; that is not proof that acceptance passed.
//
// S-P26-003 A2/G1: scheduling records the Go type schedule_*worker.JobError in
// lastFailure instead of the concrete background_write_busy cause. Reproduced
// twice in the comparison overlap log; reasons must identify the actionable cause.
//
// S-P26-004 A2/G2: four invalid comparison calls remain pending correctly,
// but health reports success=4 as well as failure=4. Four invalid classification
// calls similarly report success=4/failure=160. Queue acknowledgement is being
// counted as semantic success even though no valid result was produced.
//
// S-P26-005 G1/C6: fifteen valid secretary actions apply the first ten. Only
// one generic skipped receipt is returned and no count of the five omitted
// actions appears in health. The execution loop breaks at index 10; the tail
// has neither per-action receipts nor observable overflow accounting.
//
// Diagnosed fixture issues, NOT implementation findings: newly loaded databases
// without ANALYZE caused 2s result-write cancellation and 30s secretary deadline;
// ANALYZE restored 125 backfill calls and ~2s secretary. Raw unrestricted rules
// are 26 (including duplicate copies); the frozen UNIQUE oracle is 24. Agent grants
// must be an explicit C-test precondition. The 40-call single-edit test exhausted a 512 MiB tmpfs with real WAL (53100);
// its owned disposable storage ceiling is now 2 GiB. Browser ERR_NETWORK_CHANGED
// during Docker network creation/removal is an environment failure: rerun browsers
// after database/container work finishes. All such setup errors were corrected.
func phase26Finding(t *testing.T, id string) {
	t.Helper()
	if os.Getenv("PCAS_PHASE26_RUN_FINDINGS") != "1" {
		t.Skip("finding " + id + "; outstanding independent acceptance, set PCAS_PHASE26_RUN_FINDINGS=1 to reproduce")
	}
}

func phase26InvalidHealth(t *testing.T, health json.RawMessage, stage string) {
	t.Helper()
	t.Run("invalid_calls_are_not_successes", func(t *testing.T) {
		phase26Finding(t, "S-P26-004")
		var got struct {
			Stages []struct {
				Stage   string
				Success int
			}
		}
		if err := json.Unmarshal(health, &got); err != nil {
			t.Fatal(err)
		}
		for _, s := range got.Stages {
			if s.Stage == stage {
				if s.Success != 0 {
					t.Errorf("all actual calls had invalid output but stage %s reports success=%d", stage, s.Success)
				}
				return
			}
		}
		t.Fatalf("missing stage %s", stage)
	})
}
