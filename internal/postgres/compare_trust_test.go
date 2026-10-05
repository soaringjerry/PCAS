package postgres

import (
	"context"
	"strings"
	"testing"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func TestCompareTrustPrecedenceAndConversationEvidence(t *testing.T) {
	for _, tc := range []struct {
		name, text, acquisition, role string
		groups                        []string
		want                          string
	}{
		{"direct", "云杉喜欢虚构松林", "direct", "user", []string{"a"}, "stated"},
		{"same conversation", "云杉喜欢虚构松林", "direct", "user", []string{"a", "a"}, "stated"},
		{"different conversations", "云杉喜欢虚构松林", "direct", "user", []string{"a", "b"}, "repeated"},
		{"qualified", "云杉可能考虑虚构松林", "direct", "user", []string{"a", "b"}, "tentative"},
		{"reported", "小陈说他可能去虚构松林", "reported", "user", []string{"a", "b"}, "reported"},
		{"assistant", "云杉喜欢虚构松林", "direct", "assistant", []string{"a", "b"}, "inferred"},
		{"system", "云杉喜欢虚构松林", "reported", "system", []string{"a"}, "inferred"},
		{"tool", "云杉喜欢虚构松林", "direct", "tool", []string{"a"}, "inferred"},
		{"inferred", "云杉喜欢虚构松林", "inferred", "user", []string{"a", "b"}, "inferred"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := testStore(t)
			scope := owner()
			b1Model(t, s, `{}`)
			srcs := []memory.Ref{}
			for _, group := range tc.groups {
				src := b1Source(t, s, scope, "虚构聊天", tc.text, "archive")
				srcs = append(srcs, src)
				if _, err := s.pool.Exec(context.Background(), "INSERT INTO source_contexts(owner_id,source_id,source_version,conversation_key,role,branch) VALUES($1,$2,1,$3,$4,'active')", string(scope.OwnerID), string(src.ID), group, tc.role); err != nil {
					t.Fatal(err)
				}
			}
			ref := b1Claim(t, s, scope, tc.text, "fact", "candidate", srcs...)
			if _, err := s.pool.Exec(context.Background(), "UPDATE claim_revisions SET acquisition=$3 WHERE owner_id=$1 AND claim_id=$2", string(scope.OwnerID), string(ref.ID), tc.acquisition); err != nil {
				t.Fatal(err)
			}
			got := compareDetail(t, s, scope, ref)
			if got.Trust != tc.want || got.Confirmation != "candidate" || got.Epistemic != "inferred" {
				t.Fatal(got)
			}
		})
	}
}
func TestCompareImportedHumanMemoryUsableWithoutConfirmation(t *testing.T) {
	s := testStore(t)
	scope := owner()
	f := b1Model(t, s, `{"reply":"云杉喜欢虚构松林。","used":["M1"],"actions":[]}`)
	src := b1Source(t, s, scope, "虚构导入聊天", "我喜欢虚构松林。", "archive")
	if _, err := s.pool.Exec(context.Background(), "INSERT INTO source_contexts(owner_id,source_id,source_version,role,conversation_key,branch) VALUES($1,$2,1,'user','fictional-import','active')", string(scope.OwnerID), string(src.ID)); err != nil {
		t.Fatal(err)
	}
	ref := b1Claim(t, s, scope, "云杉喜欢虚构松林。", "preference", "candidate", src)
	workspaceCommand(t, s, scope, workspace.Command{Type: "updateAgent", ID: "model", Patch: asJSON(map[string]any{"includeInferred": false})})
	req := turnRequest("云杉喜欢虚构松林吗")
	out := mustTurn(t, s, scope, req)
	if out.Turn.Reply != "云杉喜欢虚构松林。" || !strings.Contains(f.last(t).Prompt, "trust=stated") {
		t.Fatal(out.Turn, f.last(t).Prompt)
	}
	b1HasRef(t, b1Refs(t, s, scope, req.RequestID), ref, true)
	// The same agent must still refuse a genuinely AI-authored memory.
	if _, err := s.pool.Exec(context.Background(), "UPDATE source_contexts SET role='assistant' WHERE owner_id=$1 AND source_id=$2", string(scope.OwnerID), string(src.ID)); err != nil {
		t.Fatal(err)
	}
	f.set(`{"reply":"没有本人依据。","actions":[]}`)
	second := turnRequest("云杉喜欢虚构松林吗")
	mustTurn(t, s, scope, second)
	b1HasRef(t, b1Refs(t, s, scope, second.RequestID), ref, false)
	if strings.Contains(f.last(t).Prompt, "trust=inferred") {
		t.Fatal("AI memory bypassed includeInferred=false")
	}
}
