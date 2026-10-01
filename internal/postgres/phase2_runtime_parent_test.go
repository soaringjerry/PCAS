package postgres

import (
	"context"
	"crypto/sha256"
	"testing"

	"github.com/soaringjerry/PCAS/internal/memory"
)

func TestPhase2RuntimeParentAllowAbsenceAndExplicitDeny(t *testing.T) {
	for _, edge := range []string{"derived_from", "archive"} {
		t.Run(edge, func(t *testing.T) {
			s, scope, _ := phase2RTSetup(t)
			var gold struct {
				Ancestry struct {
					ParentText string `json:"parent_text"`
					ChildText  string `json:"child_text"`
					ChildClaim string `json:"child_claim"`
					ChildAtom  string `json:"child_atom"`
				} `json:"ancestry_control"`
			}
			phase2RTReadJSON(t, "runtime-sequences.json", &gold)
			parent := mustIngest(t, s, scope, memory.IngestRequest{Connector: "phase2-ancestry-synthetic", ExternalID: "parent", ExternalVersion: "v1", Title: "合成父资料", Text: gold.Ancestry.ParentText, MediaType: "text/plain"})
			child, claim := phase2RTClaimFixture(t, s, scope, gold.Ancestry.ChildText, gold.Ancestry.ChildClaim, "fact")
			// Only the ancestry relation is a SQL fixture. All permission changes and
			// reads use the actual owner policy API and product verifier.
			ctx := context.Background()
			if edge == "derived_from" {
				if _, err := s.pool.Exec(ctx, "UPDATE source_versions SET derived_from_id=$3,derived_from_version=$4 WHERE owner_id=$1 AND source_id=$2 AND version=$5", string(scope.OwnerID), string(child.ID), string(parent.ID), parent.Version, child.Version); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := s.pool.Exec(ctx, "INSERT INTO archive_entries(owner_id,archive_id,archive_version,source_id,source_version) VALUES($1,$2,$3,$4,$5)", string(scope.OwnerID), string(parent.ID), parent.Version, string(child.ID), child.Version); err != nil {
					t.Fatal(err)
				}
			}
			parentGrant, _ := phase2RTAuthorize(t, s, scope, parent.Ref, "phase2-model", "secretary", phase2RTUnscoped())
			reader := phase2RTTask(t, s, scope, "phase2-model", "secretary", phase2RTUnscoped())
			if _, err := s.GetSource(ctx, reader, child.ID, child.Version); err == nil {
				t.Error("parent allow automatically granted child raw")
			}
			childGrant, _ := phase2RTAuthorize(t, s, scope, child, "phase2-model", "secretary", phase2RTUnscoped())
			parentRevoked := phase2RTUpdatePolicy(t, s, scope, parent.Ref, parentGrant, true)
			// Undo the first parent grant/revoke history through regrant below would
			// leave a policy row. A separate root with no policy tests true absence.
			if _, err := s.GetSource(ctx, reader, child.ID, child.Version); err == nil {
				t.Error("parent explicit deny did not block independently allowed child")
			}
			phase2RTAtoms(t, phase2RTRecall(t, s, reader, "合成子资料"), []string{gold.Ancestry.ChildAtom}, false)
			out, err := s.Expand(ctx, reader, memory.ExpandRequest{Refs: []memory.Ref{claim}, Budget: memory.Budget{Tokens: 4000}})
			phase2RTAtoms(t, out, []string{gold.Ancestry.ChildAtom}, false)
			phase2RTEvidence(t, "denied-claim", map[string]any{"claim": claim, "error": err != nil, "result": out})
			parentAllowed := phase2RTUpdatePolicy(t, s, scope, parent.Ref, parentRevoked, false)
			if _, err := s.GetSource(ctx, reader, child.ID, child.Version); err != nil {
				t.Fatal("new child context after lawful parent regrant", err)
			}
			// A prepared synthetic diagnostic records no external send. Its source
			// dependency tests the real invalidation hook independently of consumers.
			attempt := string(memory.NewID())
			body := []byte(gold.Ancestry.ChildText)
			hash := sha256.Sum256(body)
			manifest := memory.ContextManifest{Version: 1, AttemptID: memory.ID(attempt), Recipient: childGrant.Authorization.Recipient, Purpose: memory.KnowledgePurpose, Scope: phase2RTUnscoped(), Input: []memory.InputRecord{{Ref: child, SourceSpan: &memory.SourceSpan{Source: child, StartRune: 0, EndRune: len([]rune(gold.Ancestry.ChildText))}, PayloadSpans: []memory.PayloadSpan{{StartByte: 0, EndByte: len(body)}}}}, InputBytes: len(body), ObservationLayer: "adapter_arguments", InputTokens: memory.TokenCount{Method: "unknown"}}
			manifestJSON, recipientJSON := asJSON(manifest), asJSON(childGrant.Authorization.Recipient)
			if _, err := s.pool.Exec(ctx, `INSERT INTO context_attempts(owner_id,id,operation_id,ordinal,state,recipient,purpose,manifest,metadata_bytes,snapshot,snapshot_state,snapshot_bytes,input_bytes,payload_hash,observation_layer,body_expires_at,metadata_expires_at,execution_expires_at) VALUES($1,$2::uuid,$2::text,1,'prepared',$3,'knowledge',$4,octet_length($3::jsonb::text)+octet_length($4::jsonb::text)+2048,$5,'retained',$6,$6,$7,'adapter_arguments',now()+interval '7 days',now()+interval '30 days',now()+interval '5 minutes')`, string(scope.OwnerID), attempt, recipientJSON, manifestJSON, body, len(body), hash[:]); err != nil {
				t.Fatal("prepared diagnostic fixture", err)
			}
			if _, err := s.pool.Exec(ctx, `INSERT INTO attempt_typed_dependencies(owner_id,attempt_id,dependency_id,dependency_version,dependency_kind,purpose,hard_scope,policy_id,policy_revision) VALUES($1,$2,$3,$4,'source','knowledge',$5,$6,$7)`, string(scope.OwnerID), attempt, string(child.ID), child.Version, asJSON(phase2RTUnscoped()), string(childGrant.Authorization.ID), childGrant.Authorization.Revision); err != nil {
				t.Fatal(err)
			}
			parentDenied := phase2RTUpdatePolicy(t, s, scope, parent.Ref, parentAllowed, true)
			var state string
			var snapshot []byte
			if err := s.pool.QueryRow(ctx, "SELECT state,snapshot FROM context_attempts WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), attempt).Scan(&state, &snapshot); err != nil {
				t.Fatal(err)
			}
			if state != "invalidated" || len(snapshot) != 0 {
				t.Error("ancestor deny did not invalidate/erase child prepared diagnostic")
			}
			phase2RTUpdatePolicy(t, s, scope, parent.Ref, parentDenied, false)
			if err := s.pool.QueryRow(ctx, "SELECT state,snapshot FROM context_attempts WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), attempt).Scan(&state, &snapshot); err != nil {
				t.Fatal(err)
			}
			if state != "invalidated" || len(snapshot) != 0 {
				t.Error("parent regrant resurrected old child diagnostic")
			}
			phase2RTEvidence(t, "ancestor", map[string]any{"edge": edge, "parent": parent.Ref, "child": child, "claim": claim, "fixture_attempt": attempt, "state_after_regrant": state, "snapshot_bytes_after_regrant": len(snapshot), "external_send": "none; prepared SQL fixture"})
		})
	}
	t.Run("parent_absence", func(t *testing.T) {
		s, scope, _ := phase2RTSetup(t)
		var gold struct {
			Ancestry struct {
				ParentText string `json:"parent_text"`
				ChildText  string `json:"child_text"`
				ChildClaim string `json:"child_claim"`
				ChildAtom  string `json:"child_atom"`
			} `json:"ancestry_control"`
		}
		phase2RTReadJSON(t, "runtime-sequences.json", &gold)
		parent := mustIngest(t, s, scope, memory.IngestRequest{Connector: "phase2-ancestry-synthetic", ExternalID: "ungranted-parent", ExternalVersion: "v1", Title: "合成未授权父资料", Text: gold.Ancestry.ParentText, MediaType: "text/plain"})
		child, _ := phase2RTClaimFixture(t, s, scope, gold.Ancestry.ChildText, gold.Ancestry.ChildClaim, "fact")
		if _, err := s.pool.Exec(context.Background(), "UPDATE source_versions SET derived_from_id=$3,derived_from_version=$4 WHERE owner_id=$1 AND source_id=$2", string(scope.OwnerID), string(child.ID), string(parent.ID), parent.Version); err != nil {
			t.Fatal(err)
		}
		phase2RTAuthorize(t, s, scope, child, "phase2-model", "secretary", phase2RTUnscoped())
		reader := phase2RTTask(t, s, scope, "phase2-model", "secretary", phase2RTUnscoped())
		out, err := s.GetSource(context.Background(), reader, child.ID, child.Version)
		if err != nil {
			t.Fatal("parent absence blocked independent child allow", err)
		}
		phase2RTAtoms(t, out, []string{gold.Ancestry.ChildAtom}, true)
	})
}
