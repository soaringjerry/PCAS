package postgres

import (
	"context"
	"crypto/sha256"
	"strings"
	"testing"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func phase2RTQuotaFixture(t *testing.T, s *Store, scope memory.Scope, recipient memory.Recipient, count, operationPadding int, snapshot []byte) {
	t.Helper()
	manifest := asJSON(memory.ContextManifest{Version: 1, Recipient: recipient, Purpose: memory.KnowledgePurpose, Scope: phase2RTUnscoped(), InputBytes: len(snapshot), InputTokens: memory.TokenCount{Method: "unknown"}, ObservationLayer: "serialized_request"})
	state := "retained"
	if snapshot == nil {
		state = "capacity_omitted"
	}
	hash := sha256.Sum256(snapshot)
	_, err := s.pool.Exec(context.Background(), `INSERT INTO context_attempts(owner_id,id,operation_id,ordinal,state,recipient,purpose,manifest,metadata_bytes,snapshot,snapshot_state,snapshot_bytes,input_bytes,payload_hash,observation_layer,created_at,body_expires_at,metadata_expires_at,execution_expires_at)
	 SELECT $1,md5('phase2-quota-'||g)::uuid,'synthetic-old-'||g||repeat('x',$8),1,'completed',$2,'knowledge',$3,65536,$4,$5,$6,$6,$7,'serialized_request',now()-interval '1 hour'+g*interval '1 microsecond',now()+interval '6 days',now()+interval '29 days',now()+interval '5 minutes' FROM generate_series(1,$9::integer)g`, string(scope.OwnerID), asJSON(recipient), manifest, snapshot, state, len(snapshot), hash[:], operationPadding, count)
	if err != nil {
		t.Fatal("bounded synthetic quota fixture", err)
	}
	// This SQL fixture reports its actual whole metadata cost, never a forged
	// small self-report. No source/provider input is invented by these old rows.
	if _, err := s.pool.Exec(context.Background(), `UPDATE context_attempts a SET metadata_bytes=octet_length((to_jsonb(a)-'snapshot'-'metadata_bytes')::text) WHERE owner_id=$1`, string(scope.OwnerID)); err != nil {
		t.Fatal(err)
	}
}

func TestPhase2RuntimeOwnerBodyQuotaReclaimsOldestBeforeExactSend(t *testing.T) {
	s, scope, capture := phase2RTSetup(t)
	source := phase2RTSource(t, s, scope)
	policy, _ := phase2RTAuthorize(t, s, scope, source.Ref, "phase2-model", "secretary", phase2RTUnscoped())
	oldBody := make([]byte, 262144)
	for i := range oldBody {
		oldBody[i] = 'x'
	}
	phase2RTQuotaFixture(t, s, scope, policy.Authorization.Recipient, 256, 0, oldBody)
	var before int64
	if err := s.pool.QueryRow(context.Background(), "SELECT sum(snapshot_bytes) FROM context_attempts WHERE owner_id=$1", string(scope.OwnerID)).Scan(&before); err != nil || before != 67108864 {
		t.Fatalf("64MiB exact synthetic body boundary absent: bytes=%d error=%v", before, err)
	}
	operationID := string(memory.NewID())
	_, err := s.DeskTurn(context.Background(), scope, workspace.DeskTurnRequest{RequestID: operationID, AgentID: "phase2-model", Text: phase2RTGoldRead(t).Cases[0].Query})
	if err != nil {
		t.Fatal(err)
	}
	if capture.count() != 1 {
		t.Fatalf("body reclaim must permit one actual provider request; count=%d", capture.count())
	}
	attempt := phase2RTSnapshotForSource(t, s, scope, source.Ref, "secretary", operationID, capture.request(t, 0))
	phase2RTMeasuredMetadata(t, s, scope, attempt.ID)
	var after int64
	var oldestState string
	var oldestBody []byte
	if err := s.pool.QueryRow(context.Background(), "SELECT sum(snapshot_bytes) FROM context_attempts WHERE owner_id=$1", string(scope.OwnerID)).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if err := s.pool.QueryRow(context.Background(), "SELECT snapshot_state,snapshot FROM context_attempts WHERE owner_id=$1 AND id=md5('phase2-quota-1')::uuid", string(scope.OwnerID)).Scan(&oldestState, &oldestBody); err != nil {
		t.Fatal(err)
	}
	if after > 67108864 || oldestState != "capacity_omitted" || len(oldestBody) != 0 {
		t.Errorf("owner body reclaim bytes=%d oldest state=%s oldest bytes=%d", after, oldestState, len(oldestBody))
	}
	phase2RTEvidence(t, "body-quota", map[string]any{"synthetic_old_rows": 256, "before_bytes": before, "after_bytes": after, "oldest_snapshot_state": oldestState, "actual_provider_requests": capture.count(), "new_attempt": attempt.ID, "new_payload_truncation": "checked exact byte equality in diagnostic helper"})
}

func TestPhase2RuntimeMetadataAndCountCapacityRejectBeforeExternalSend(t *testing.T) {
	for _, boundary := range []string{"skeleton_count", "whole_metadata_bytes"} {
		t.Run(boundary, func(t *testing.T) {
			s, scope, capture := phase2RTSetup(t)
			source := phase2RTSource(t, s, scope)
			policy, _ := phase2RTAuthorize(t, s, scope, source.Ref, "phase2-model", "secretary", phase2RTUnscoped())
			if boundary == "skeleton_count" {
				phase2RTQuotaFixture(t, s, scope, policy.Authorization.Recipient, 10000, 0, nil)
			} else {
				// Long synthetic operation IDs consume real variable metadata. Each
				// row stays under 64KiB, and the initial owner sum stays under 64MiB.
				phase2RTQuotaFixture(t, s, scope, policy.Authorization.Recipient, 1080, 60000, nil)
				var total int
				if err := s.pool.QueryRow(context.Background(), "SELECT sum(metadata_bytes) FROM context_attempts WHERE owner_id=$1", string(scope.OwnerID)).Scan(&total); err != nil {
					t.Fatal(err)
				}
				remaining := 67108864 - total - 128
				if remaining <= 0 {
					t.Fatalf("owner metadata fixture already over limit: %d", total)
				}
				// Add enough individually bounded rows to leave exactly 128 bytes
				// for a new required skeleton, far below its mandatory recipient.
				for remaining > 0 {
					want := remaining
					if want > 62000 {
						want = 62000
					}
					if remaining-want > 0 && remaining-want < 3000 {
						want -= 3000
					}
					id := string(memory.NewID())
					_, err := s.pool.Exec(context.Background(), `INSERT INTO context_attempts SELECT owner_id,$2::uuid,$2::text||repeat('x',$3),ordinal,state,recipient,purpose,manifest,metadata_bytes,snapshot,snapshot_state,snapshot_bytes,input_bytes,payload_hash,observation_layer,external_receipt,dispatch_reserved_at,dispatched_at,delivered_at,completed_at,invalidation_reason,error_code,created_at,body_expires_at,metadata_expires_at,execution_expires_at FROM context_attempts WHERE owner_id=$1 LIMIT 1`, string(scope.OwnerID), id, 0)
					if err != nil {
						t.Fatal(err)
					}
					if _, err := s.pool.Exec(context.Background(), `UPDATE context_attempts a SET metadata_bytes=octet_length((to_jsonb(a)-'snapshot'-'metadata_bytes')::text) WHERE owner_id=$1 AND id=$2`, string(scope.OwnerID), id); err != nil {
						t.Fatal(err)
					}
					var added int
					if err := s.pool.QueryRow(context.Background(), "SELECT metadata_bytes FROM context_attempts WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), id).Scan(&added); err != nil {
						t.Fatal(err)
					}
					if want < added {
						t.Fatalf("remaining fixture row cannot fit exact measured metadata: want=%d base=%d", want, added)
					}
					extra := want - added
					if _, err := s.pool.Exec(context.Background(), "UPDATE context_attempts SET operation_id=operation_id||repeat('x',$3),metadata_bytes=metadata_bytes+$3 WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), id, extra); err != nil {
						t.Fatal(err)
					}
					added = want
					remaining -= added
				}
			}
			var count, metadata int
			if err := s.pool.QueryRow(context.Background(), "SELECT count(*),sum(metadata_bytes) FROM context_attempts WHERE owner_id=$1", string(scope.OwnerID)).Scan(&count, &metadata); err != nil {
				t.Fatal(err)
			}
			if count > 10000 || metadata > 67108864 {
				t.Fatal("synthetic owner fixture exceeds its own permitted initial bound")
			}
			out, err := s.DeskTurn(context.Background(), scope, workspace.DeskTurnRequest{RequestID: string(memory.NewID()), AgentID: "phase2-model", Text: phase2RTGoldRead(t).Cases[0].Query})
			phase2RTEvidence(t, "capacity-rejection", map[string]any{"boundary": boundary, "initial_count": count, "initial_whole_metadata_bytes": metadata, "desk_result": out, "error": err != nil, "actual_provider_requests": capture.count(), "fixture_external_send": "none"})
			feedback := string(asJSON(out))
			if err != nil {
				feedback += err.Error()
			}
			if !strings.Contains(feedback, "record_capacity") {
				t.Error("capacity refusal lacks controlled record_capacity recovery signal")
			}
			if capture.count() != 0 {
				t.Error("mandatory diagnostic skeleton capacity exhausted but actual request was externally sent")
			}
		})
	}
}
