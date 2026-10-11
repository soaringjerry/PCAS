package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// Exercise the same unconfirmed imported statement at every consumer boundary.
// AI authorship and explicit inference have separate negative controls, even
// when their legacy confirmation is confirmed.
func TestCompareTrustAtEveryMemoryConsumer(t *testing.T) {
	cases := []struct {
		name, text, acquisition, role, trust string
		repeated, include, allowed           bool
	}{
		{name: "stated", text: "FictionalTrust 白鹭月报先写结论。", acquisition: "direct", role: "user", trust: "stated", allowed: true},
		{name: "repeated", text: "FictionalTrust 白鹭月报先写结论。", acquisition: "direct", role: "user", trust: "repeated", repeated: true, allowed: true},
		{name: "tentative", text: "FictionalTrust 可能先写白鹭月报结论。", acquisition: "direct", role: "user", trust: "tentative", allowed: true},
		{name: "reported", text: "FictionalTrust 陆青说白鹭月报先写结论。", acquisition: "reported", role: "user", trust: "reported", allowed: true},
		{name: "AI denied", text: "FictionalTrust 白鹭月报先写结论。", acquisition: "direct", role: "assistant", trust: "inferred"},
		{name: "AI allowed", text: "FictionalTrust 白鹭月报先写结论。", acquisition: "direct", role: "assistant", trust: "inferred", include: true, allowed: true},
		{name: "inference denied", text: "FictionalTrust 白鹭月报先写结论。", acquisition: "inferred", role: "user", trust: "inferred"},
		{name: "inference allowed", text: "FictionalTrust 白鹭月报先写结论。", acquisition: "inferred", role: "user", trust: "inferred", include: true, allowed: true},
	}
	for _, confirmation := range []string{"unknown", "candidate", "confirmed"} {
		for _, tc := range cases {
			t.Run(confirmation+"/"+tc.name, func(t *testing.T) {
				s, scope, ctx := testStore(t), owner(), context.Background()
				f := b1Model(t, s, `{}`)
				sources := []memory.Ref{}
				count := 1
				if tc.repeated {
					count = 2
				}
				for i := 0; i < count; i++ {
					src := b1Source(t, s, scope, "虚构导入聊天", tc.text, "chatgpt")
					sources = append(sources, src)
					if _, err := s.pool.Exec(ctx, `INSERT INTO source_contexts(owner_id,source_id,source_version,conversation_key,role,branch) VALUES($1,$2,1,$3,$4,'active')`, string(scope.OwnerID), string(src.ID), fmt.Sprint("fictional-conversation-", i), tc.role); err != nil {
						t.Fatal(err)
					}
				}
				ref := b1Claim(t, s, scope, tc.text, "fact", confirmation, sources...)
				if _, err := s.pool.Exec(ctx, `UPDATE claim_revisions SET acquisition=$3 WHERE owner_id=$1 AND claim_id=$2`, string(scope.OwnerID), string(ref.ID), tc.acquisition); err != nil {
					t.Fatal(err)
				}
				if _, err := s.pool.Exec(ctx, `UPDATE record_versions SET expressed_at='2026-09-12 09:00:00+00' WHERE owner_id=$1 AND record_id=$2`, string(scope.OwnerID), string(ref.ID)); err != nil {
					t.Fatal(err)
				}
				workspaceCommand(t, s, scope, workspace.Command{Type: "updateAgent", ID: "model", Patch: asJSON(map[string]any{"includeInferred": tc.include})})
				if m := compareDetail(t, s, scope, ref); m.Trust != tc.trust {
					t.Fatalf("trust=%s want=%s", m.Trust, tc.trust)
				}
				for i := 0; i < 100; i++ {
					j, err := s.ClaimIndex(ctx, time.Minute)
					if err != nil {
						t.Fatal(err)
					}
					if j == nil {
						break
					}
					if err = s.ProcessIndex(ctx, *j); err != nil {
						t.Fatal(err)
					}
					if i == 99 {
						t.Fatal("index queue did not drain")
					}
				}
				// Secretary: aliases and actual stored dependency references.
				f.set(`{"reply":"虚构秘书回答。","used":["M1"],"actions":[]}`)
				req := turnRequest("FictionalTrust")
				mustTurn(t, s, scope, req)
				b1HasRef(t, b1Refs(t, s, scope, req.RequestID), ref, tc.allowed)
				// Deputy retrieval, followed by independent completion/access checks.
				task := string(memory.NewID())
				workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", ID: task, Title: "虚构月报检查事项"})
				state := workspaceCommand(t, s, scope, workspace.Command{Type: "requestRun", ThingID: task, AgentID: "model", Kind: "ask", Prompt: "用户的 FictionalTrust"})
				run := state.Runs[0]
				b1HasRef(t, run.ContextVersions, ref, tc.allowed)
				probe := workspace.Run{ID: run.ID, ThingID: task, AgentID: "model", ContextVersions: []memory.Ref{ref}}
				if err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
					for name, check := range map[string]func() error{"completion": func() error { return verifyRunTx(ctx, tx, scope, probe) }, "access": func() error { return verifyRunAccessTx(ctx, tx, scope, probe) }} {
						if got := check(); (got == nil) != tc.allowed {
							t.Errorf("%s allowed=%v want=%v error=%v", name, got == nil, tc.allowed, got)
						}
					}
					from := time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC)
					hints := memory.TeamRecall{Plan: memory.QueryPlan{Time: &memory.PlanTime{From: from, To: from.Add(24 * time.Hour), Axis: "said"}}}
					refs, _, err := structuredRecallTx(ctx, tx, memory.Scope{OwnerID: scope.OwnerID, PrincipalID: "model"}, hints, 20)
					if err != nil {
						return err
					}
					b1HasRef(t, refs, ref, tc.allowed)
					// A persisted adopted field must obey exactly the same trust policy.
					if _, err = tx.Exec(ctx, `INSERT INTO run_dependencies(owner_id,run_id,memory_id,memory_version) VALUES($1,$2,$3,1) ON CONFLICT DO NOTHING`, string(scope.OwnerID), run.ID, string(ref.ID)); err != nil {
						return err
					}
					if _, err = tx.Exec(ctx, `INSERT INTO adopted_artifacts(owner_id,run_id,thing_id,kind,artifact_id,body) VALUES($1,$2,$3,'notes','fictional','虚构采纳文本')`, string(scope.OwnerID), run.ID, task); err != nil {
						return err
					}
					item, err := getItem(ctx, tx, scope, task)
					if err != nil {
						return err
					}
					item.Notes = "虚构采纳文本"
					sanitized, refs, err := sanitizeItemTx(ctx, tx, scope, "model", item)
					if err != nil {
						return err
					}
					if got := sanitized.Notes != ""; got != tc.allowed {
						t.Errorf("adopted field allowed=%v want=%v", got, tc.allowed)
					}
					b1HasRef(t, refs, ref, tc.allowed)
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}
