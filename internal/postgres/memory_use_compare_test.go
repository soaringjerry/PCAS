package postgres

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// Exercise the batch-2 writer and batch-4 ready path together, rather than
// fabricating retirement flags alone. Historical answers and new context have
// different contracts: duplicate references remain valid only in the former.
func TestB4ComparedRetirementAcrossSecretaryAndDeputy(t *testing.T) {
	for _, kind := range []string{"duplicate", "superseded"} {
		t.Run(kind, func(t *testing.T) {
			s, scope := testStore(t), owner()
			f := b1Model(t, s, `{"reply":"虚构旧回答","actions":[]}`)
			old := compareFixture(t, s, scope, "虚构汇报期限是周三")[0]
			m := compareDetail(t, s, scope, old)
			var entity string
			if err := s.pool.QueryRow(context.Background(), "SELECT entity_id::text FROM claim_mentions WHERE owner_id=$1 AND claim_id=$2 LIMIT 1", scope.OwnerID, old.ID).Scan(&entity); err != nil {
				t.Fatal(err)
			}
			key := "entity:" + entity
			b4Card(t, s, scope, key, "topic", "虚构汇报", m, b4Memory(t, s, scope, "虚构汇报有三页"), b4Memory(t, s, scope, "虚构汇报用蓝色"), b4Memory(t, s, scope, "虚构汇报给主管"))
			b4Exec(t, s, `INSERT INTO handovers(owner_id,body,depends,rule,built_at,stale) SELECT owner_id,'虚构已退出的交接摘要',jsonb_build_array(jsonb_build_object('key',key,'builtAt',built_at)),1,now(),false FROM status_cards WHERE owner_id=$1 AND key=$2`, scope.OwnerID, key)
			b4Exec(t, s, `INSERT INTO deadlines(owner_id,id,claim_id,claim_version,kind,at,title) VALUES($1,$2,$3,1,'deadline',$4,'虚构已退出的期限')`, scope.OwnerID, memory.NewID(), old.ID, time.Now().Add(24*time.Hour))
			req := turnRequest("虚构汇报期限是什么")
			out := mustTurn(t, s, scope, req)
			b1HasRef(t, b1Refs(t, s, scope, req.RequestID), old, true)
			newer := compareFixture(t, s, scope, "虚构汇报期限改到周五")[0]
			b4Exec(t, s, "UPDATE record_versions SET expressed_at=expressed_at+interval '1 day' WHERE owner_id=$1 AND record_id=$2", scope.OwnerID, newer.ID)
			if kind == "duplicate" {
				f.set(`{"duplicates":[{"keep":2,"members":[1,2]}],"superseded":[]}`)
			} else {
				f.set(`{"duplicates":[],"superseded":[{"old":1,"new":2}]}`)
			}
			compareProcess(t, s, scope)
			if got := compareDetail(t, s, scope, old); got.Retired != kind || got.RetiredBy != string(newer.ID) {
				t.Fatal(got)
			}
			b1Outdated(t, b1History(t, s, scope, out.ConversationID), kind == "superseded")
			keeper := compareDetail(t, s, scope, newer)
			if keeper.Trust != map[string]string{"duplicate": "repeated", "superseded": "stated"}[kind] {
				t.Fatal(keeper)
			}
			var readers atomic.Int32
			secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
				sys, prompt := b4RequestBody(t, r)
				for _, bad := range []string{string(old.ID), "虚构已退出的交接摘要", "虚构已退出的期限"} {
					if strings.Contains(prompt, bad) {
						t.Errorf("retired context reached model: %s", bad)
					}
				}
				if strings.Contains(prompt, "分组目录") {
					secretaryModelReply(w, map[string]any{"groups": []string{key}})
					return
				}
				if strings.Contains(sys, "只读的记忆读者") {
					readers.Add(1)
					if !strings.Contains(prompt, "trust="+keeper.Trust) || !strings.Contains(prompt, string(newer.ID)) {
						t.Error("reader lost keeper/trust")
					}
					secretaryModelReply(w, map[string]any{"used": []string{string(newer.ID)}})
					return
				}
				if strings.Contains(sys, "这是自查") {
					if strings.Contains(sys, "秘书") {
						secretaryModelReply(w, `{"reply":"虚构当前回答","actions":[]}`)
					} else {
						secretaryModelReply(w, "虚构当前方案")
					}
					return
				}
				if !strings.Contains(prompt, "trust="+keeper.Trust) {
					t.Error("answer lost batch-2 trust")
				}
				if strings.Contains(sys, "秘书") {
					secretaryModelReply(w, `{"reply":"虚构当前回答","actions":[]}`)
				} else {
					secretaryModelReply(w, "虚构当前方案")
				}
			})
			for _, tier := range []string{"light", "medium", "heavy"} {
				req := turnRequest("虚构汇报期限是什么")
				if _, err := s.DeskTurn(WithMemoryTier(context.Background(), tier), scope, req); err != nil {
					t.Fatal(tier, err)
				}
				b1HasRef(t, b1Refs(t, s, scope, req.RequestID), old, false)
			}
			state := workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "虚构汇报", Text: "请写当前方案"})
			workspaceCommand(t, s, scope, workspace.Command{Type: "requestRun", ThingID: state.Tasks[0].ID, AgentID: "model", Kind: "plan", Prompt: "虚构汇报当前方案"})
			if err := s.runAgentOnce(context.Background()); err != nil {
				t.Fatal(err)
			}
			state, err := s.Snapshot(context.Background(), scope)
			if err != nil || len(state.Runs) != 1 || state.Runs[0].Status != "done" || readers.Load() < 2 {
				t.Fatal(state.Runs, readers.Load(), err)
			}
			for _, ref := range state.Runs[0].ContextVersions {
				if ref.ID == old.ID {
					t.Fatal("retired deputy dependency", ref)
				}
			}
			if kind == "duplicate" {
				b1Delete(t, s, scope, newer)
				b1Outdated(t, b1History(t, s, scope, out.ConversationID), true)
			}
		})
	}
}

func TestB4ConsumesFiveBatch2TrustValues(t *testing.T) {
	s, scope := testStore(t), owner()
	b1Model(t, s, `{}`)
	ms := []workspace.Memory{}
	trusts := []string{"stated", "repeated", "tentative", "reported", "inferred"}
	for i, trust := range trusts {
		text := fmt.Sprintf("虚构可信度背景%d", i)
		acquisition, role := "direct", "user"
		if trust == "tentative" {
			text = "虚构可信度背景可能需要再核对"
		}
		if trust == "reported" {
			acquisition = "reported"
		}
		if trust == "inferred" {
			role = "assistant"
		}
		sources := []memory.Ref{}
		count := 1
		if trust == "repeated" {
			count = 2
		}
		for j := 0; j < count; j++ {
			src := b1Source(t, s, scope, "虚构聊天", text, "archive")
			b4Exec(t, s, `INSERT INTO source_contexts(owner_id,source_id,source_version,conversation_key,role,branch) VALUES($1,$2,1,$3,$4,'active')`, scope.OwnerID, src.ID, fmt.Sprintf("fictional-%d-%d", i, j), role)
			sources = append(sources, src)
		}
		ref := b1Claim(t, s, scope, text, "fact", "candidate", sources...)
		b4Exec(t, s, "UPDATE claim_revisions SET acquisition=$3 WHERE owner_id=$1 AND claim_id=$2", scope.OwnerID, ref.ID, acquisition)
		m := compareDetail(t, s, scope, ref)
		if m.Trust != trust {
			t.Fatal(m, trust)
		}
		ms = append(ms, m)
	}
	b4Card(t, s, scope, "self:goal", "self", "虚构可信度", ms...)
	for _, include := range []bool{false, true} {
		workspaceCommand(t, s, scope, workspace.Command{Type: "updateAgent", ID: "model", Patch: asJSON(map[string]any{"includeInferred": include})})
		secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
			_, prompt := b4RequestBody(t, r)
			for i, m := range ms {
				present := strings.Contains(prompt, "trust="+trusts[i])
				if present != (i < 4 || include) {
					t.Errorf("includeInferred=%v trust=%s present=%v", include, trusts[i], present)
				}
				if (i < 4 || include) && !strings.Contains(prompt, m.Text) {
					t.Error("human candidate excluded", trusts[i])
				}
			}
			secretaryModelReply(w, `{"reply":"虚构可信度回答","actions":[]}`)
		})
		req := turnRequest("虚构可信度背景是什么")
		mustTurn(t, s, scope, req)
		for i, m := range ms {
			b1HasRef(t, b1Refs(t, s, scope, req.RequestID), memory.Ref{ID: memory.ID(m.ID), Version: m.Version, Kind: memory.ClaimKind}, i < 4 || include)
		}
	}
}

// A prompt queued before comparison must not replay retired memory text. This
// check concerns new generation; the already answered duplicate case above
// deliberately remains valid. Cover current and pre-boundary queue layouts.
func TestB4QueuedRetiredPromptNeverReachesModel(t *testing.T) {
	for _, tier := range []string{"light", "medium", "heavy"} {
		for _, legacy := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/legacy=%v", tier, legacy), func(t *testing.T) {
				s, scope := testStore(t), owner()
				var calls atomic.Int32
				secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					secretaryModelReply(w, "虚构禁止调用")
				})
				old := compareFixture(t, s, scope, "虚构排队后退出的约束")[0]
				b4Card(t, s, scope, "self:goal", "self", "虚构排队目标", compareDetail(t, s, scope, old))
				state := workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "虚构排队目标"})
				_, err := s.Execute(WithMemoryTier(context.Background(), tier), scope, workspace.Command{RequestID: string(memory.NewID()), ExpectedRevision: state.Revision, Type: "requestRun", ThingID: state.Tasks[0].ID, AgentID: "model", Kind: "plan", Prompt: "虚构排队目标"})
				if err != nil {
					t.Fatal(err)
				}
				if legacy {
					b4Exec(t, s, "UPDATE agent_runs SET document=document-'memoryContextRange' WHERE owner_id=$1", scope.OwnerID)
				}
				keeper := compareFixture(t, s, scope, "虚构排队约束的当前保留条目")[0]
				b4Exec(t, s, "UPDATE claims SET retired='duplicate',retired_by=$3 WHERE owner_id=$1 AND id=$2", scope.OwnerID, old.ID, keeper.ID)
				if err := s.runAgentOnce(context.Background()); err != nil {
					t.Fatal(err)
				}
				state, err = s.Snapshot(context.Background(), scope)
				if err != nil || calls.Load() != 0 || len(state.Runs) != 1 || state.Runs[0].Status != "failed" {
					t.Fatal(state.Runs, calls.Load(), err)
				}
			})
		}
	}
}
