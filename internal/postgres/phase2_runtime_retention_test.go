package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func phase2RTMeasuredMetadata(t *testing.T, s *Store, scope memory.Scope, id memory.ID) int {
	t.Helper()
	var measured, stored int
	err := s.pool.QueryRow(context.Background(), `SELECT a.metadata_bytes,
	 octet_length((to_jsonb(a)-'snapshot'-'metadata_bytes')::text)+
	 coalesce((SELECT sum(octet_length(to_jsonb(d)::text)) FROM attempt_typed_dependencies d WHERE d.owner_id=a.owner_id AND d.attempt_id=a.id),0)
	 FROM context_attempts a WHERE a.owner_id=$1 AND a.id=$2`, string(scope.OwnerID), string(id)).Scan(&stored, &measured)
	if err != nil {
		t.Fatal(err)
	}
	if stored != measured || measured > 65536 {
		t.Errorf("whole logical metadata measured=%d stored=%d limit=65536", measured, stored)
	}
	phase2RTEvidence(t, "whole-metadata", map[string]any{"attempt_id": id, "measured_bytes": measured, "stored_bytes": stored, "includes": "all attempt row JSON except snapshot/metadata_bytes; all typed dependency row JSON", "physical_pg_bytes": "not measured"})
	return measured
}

func TestPhase2RuntimeRetentionKeepsDurableInvalidation(t *testing.T) {
	s, scope, capture := phase2RTSetup(t)
	source := phase2RTSource(t, s, scope)
	policy, _ := phase2RTAuthorize(t, s, scope, source.Ref, "phase2-model", "deputy", phase2RTUnscoped())
	derived := phase2RTDerivedReply(t, capture)
	run := phase2RTPrepareRun(t, s, scope, "phase2-model", phase2RTGoldRead(t).Cases[0].Query)
	phase2RTStartRunner(t, s)
	finished := phase2RTWaitRun(t, s, scope, run.ID)
	if finished.Status != "done" || !strings.Contains(string(asJSON(finished)), derived) {
		t.Fatal("real source-derived run did not create the positive-control business result")
	}
	attempt := phase2RTSnapshotForSource(t, s, scope, source.Ref, "deputy", capture.request(t, 0))
	diagnostics, ok := any(s).(phase2RTDiagnostics)
	if !ok {
		t.Fatal("actual diagnostics read/cleanup API missing")
	}
	for _, limit := range []struct {
		name string
		got  time.Duration
		want time.Duration
	}{{"body", attempt.BodyExpiresAt.Sub(attempt.CreatedAt), 7 * 24 * time.Hour}, {"metadata", attempt.MetadataExpiresAt.Sub(attempt.CreatedAt), 30 * 24 * time.Hour}} {
		if limit.got < limit.want-time.Second || limit.got > limit.want+time.Second {
			t.Errorf("%s expiry=%s want=%s engineering default", limit.name, limit.got, limit.want)
		}
	}
	phase2RTMeasuredMetadata(t, s, scope, attempt.ID)
	ctx := context.Background()
	durable := func() string {
		var rows string
		if err := s.pool.QueryRow(ctx, `SELECT coalesce(jsonb_agg(to_jsonb(d) ORDER BY dependency_id,dependency_version,dependency_kind)::text,'[]') FROM context_artifact_dependencies d WHERE owner_id=$1 AND parent_kind='run' AND parent_id=$2`, string(scope.OwnerID), run.ID).Scan(&rows); err != nil {
			t.Fatal(err)
		}
		return rows
	}
	before := durable()
	if before == "[]" || !strings.Contains(before, string(source.ID)) {
		t.Fatal("real run was stored without durable source lineage")
	}
	// Only diagnostic deadlines are a SQL clock fixture. The body is the exact
	// real provider request; all reads, cleanup and policy changes use product APIs.
	if _, err := s.pool.Exec(ctx, `UPDATE context_attempts SET body_expires_at=now()-interval '1 second',created_at=least(created_at,now()-interval '2 seconds') WHERE owner_id=$1 AND id=$2`, string(scope.OwnerID), string(attempt.ID)); err != nil {
		t.Fatal(err)
	}
	if body, err := diagnostics.ContextAttemptSnapshot(ctx, scope, attempt.ID); len(body) != 0 {
		t.Errorf("expired body returned before cleanup, bytes=%d error=%v", len(body), err)
	}
	if err := diagnostics.CleanupContextAttempts(ctx, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	var body []byte
	var state string
	if err := s.pool.QueryRow(ctx, "SELECT snapshot,snapshot_state FROM context_attempts WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), string(attempt.ID)).Scan(&body, &state); err != nil {
		t.Fatal(err)
	}
	if len(body) != 0 || state != string(memory.SnapshotExpired) {
		t.Error("body expiry lost controlled skeleton reason or retained raw bytes")
	}
	if durable() != before {
		t.Error("diagnostic body expiry changed live business lineage")
	}
	if err := diagnostics.CleanupContextAttempts(ctx, attempt.MetadataExpiresAt.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	var attemptRows, dependencyRows int
	if err := s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM context_attempts WHERE owner_id=$1 AND id=$2),(SELECT count(*) FROM attempt_typed_dependencies WHERE owner_id=$1 AND attempt_id=$2)`, string(scope.OwnerID), string(attempt.ID)).Scan(&attemptRows, &dependencyRows); err != nil {
		t.Fatal(err)
	}
	if attemptRows != 0 || dependencyRows != 0 || durable() != before {
		t.Errorf("metadata expiry rows=%d diagnostic deps=%d durable preserved=%t", attemptRows, dependencyRows, durable() == before)
	}
	phase2RTUpdatePolicy(t, s, scope, source.Ref, policy, true)
	st, err := s.Snapshot(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	for _, current := range st.Runs {
		if current.ID == run.ID && strings.Contains(string(asJSON(current)), derived) {
			t.Error("source revocation after diagnostic expiry left old derived result visible")
		}
	}
	if _, err := s.Execute(ctx, scope, workspace.Command{Type: "adoptRun", ID: run.ID}); err == nil {
		t.Error("source-derived result adopted after revoke when its diagnostic no longer exists")
	}
	phase2RTEvidence(t, "retention", map[string]any{"attempt_id": attempt.ID, "run_id": run.ID, "body_state_after_expiry": state, "attempt_rows_after_metadata_expiry": attemptRows, "diagnostic_dependency_rows_after_metadata_expiry": dependencyRows, "durable_before": before, "durable_after_cleanup": before, "clock": "explicit Cleanup time; diagnostic SQL expiry fixture", "provider_requests": capture.count()})
}
