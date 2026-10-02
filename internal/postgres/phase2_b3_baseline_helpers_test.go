package postgres

import (
	"fmt"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// Deterministic IDs, times, evidence and chunks are identical on the pre-batch3
// baseline and candidate. No response bytes are normalized in S10.
func b3FixedID(n int) memory.ID { return memory.ID(fmt.Sprintf("b3000000-0000-4000-8000-%012d", n)) }
func b3BaselineData(t *testing.T, s *Store, scope memory.Scope) {
	t.Helper()
	workspaceCommand(t, s, scope, workspace.Command{Type: "updateSettings", Patch: asJSON(map[string]string{"timezone": "Asia/Shanghai"})})
	tx, err := s.pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(t.Context())
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := tx.Exec(t.Context(), q, args...); err != nil {
			t.Fatal(err)
		}
	}
	for n, kind := range map[int]string{1: "entity", 2: "entity", 10: "source", 11: "chunk", 20: "claim", 21: "claim", 22: "claim"} {
		exec(`INSERT INTO memory_records(owner_id,id,kind,version,created_at,updated_at) VALUES($1,$2,$3,1,'2025-08-01T00:00:00Z','2025-08-01T00:00:00Z')`, scope.OwnerID, b3FixedID(n), kind)
		exec(`INSERT INTO record_versions(owner_id,record_id,version,recorded_at) VALUES($1,$2,1,'2025-08-01T00:00:00Z')`, scope.OwnerID, b3FixedID(n))
		for _, principal := range []string{"model", "manual", "baseline-external"} {
			exec(`INSERT INTO record_grants(owner_id,record_id,principal_id) VALUES($1,$2,$3)`, scope.OwnerID, b3FixedID(n), principal)
		}
	}
	for _, n := range []int{1, 2} {
		exec(`INSERT INTO entities(owner_id,id) VALUES($1,$2)`, scope.OwnerID, b3FixedID(n))
		kind, name := "person", "匿名甲"
		if n == 2 {
			kind = "place"
			name = "成都"
		}
		exec(`INSERT INTO entity_versions(owner_id,entity_id,version,entity_type,name) VALUES($1,$2,1,$3,$4)`, scope.OwnerID, b3FixedID(n), kind, name)
		exec(`INSERT INTO aliases(owner_id,entity_id,entity_version,alias) VALUES($1,$2,1,$3)`, scope.OwnerID, b3FixedID(n), name)
	}
	exec(`INSERT INTO sources(owner_id,id,connector,external_id) VALUES($1,$2,'manual','b3-baseline-original')`, scope.OwnerID, b3FixedID(10))
	body := "neutralneedle 成都计划原话：准备去书店见老王。"
	exec(`INSERT INTO source_versions(owner_id,source_id,version,external_version,content_hash,title,body,media_type) VALUES($1,$2,1,'1',decode(repeat('11',32),'hex'),'秘书原话',$3,'text/plain')`, scope.OwnerID, b3FixedID(10), body)
	exec(`UPDATE record_versions SET expressed_at='2025-07-02T09:00:00Z' WHERE owner_id=$1 AND record_id=$2`, scope.OwnerID, b3FixedID(10))
	exec(`INSERT INTO chunks(owner_id,id,version,source_id,source_version,ordinal,start_rune,end_rune,body,search_text) VALUES($1,$2,1,$3,1,0,0,$4,$5,'neutralneedle 成都 计划 原话 书店 老王 去年')`, scope.OwnerID, b3FixedID(11), b3FixedID(10), len([]rune(body)), body)
	for i, n := range []int{20, 21, 22} {
		exec(`INSERT INTO claims(owner_id,id) VALUES($1,$2)`, scope.OwnerID, b3FixedID(n))
		nature, confirmation := "plan", "adopted"
		if i == 1 {
			nature = "fact"
		}
		if i == 2 {
			confirmation = "candidate"
		}
		text := fmt.Sprintf("neutralneedle 基准记忆%d：成都书店安排", i)
		exec(`INSERT INTO claim_revisions(owner_id,claim_id,version,subject_id,predicate,value,nature,acquisition,confirmation,change_type) VALUES($1,$2,1,$3,'基准安排',$4::jsonb,$5,'direct',$6,'initial')`, scope.OwnerID, b3FixedID(n), b3FixedID(1), string(asJSON(text)), nature, confirmation)
		exec(`INSERT INTO evidence(owner_id,id,source_id,source_version,target_id,target_version,locator,acquisition,stance) VALUES($1,$2,$3,1,$4,1,'{}','direct','supports')`, scope.OwnerID, b3FixedID(30+i), b3FixedID(10), b3FixedID(n))
		exec(`INSERT INTO activity(owner_id,record_id,last_effective_use_at,stability,half_life_seconds) VALUES($1,$2,'2025-08-01T00:00:00Z',1,2592000)`, scope.OwnerID, b3FixedID(n))
	}
	if err := tx.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, n := range []int{20, 21} {
		workspaceCommand(t, s, scope, workspace.Command{Type: "setMemoryVisibility", ID: string(b3FixedID(n)), AgentIDs: []string{"model", "manual"}})
	}
}

func b3BaselineStructured(t *testing.T, s *Store, scope memory.Scope) {
	t.Helper()
	for _, n := range []int{20, 21, 22} {
		b3Exec(t, s, `UPDATE record_versions SET expressed_at='2025-07-03T15:30:00Z' WHERE owner_id=$1 AND record_id=$2`, scope.OwnerID, b3FixedID(n))
		b3Exec(t, s, `UPDATE claim_revisions SET event_from='2025-11-01T00:00:00+08:00',event_to='2025-12-01T00:00:00+08:00',event_precision='month' WHERE owner_id=$1 AND claim_id=$2`, scope.OwnerID, b3FixedID(n))
		b3Exec(t, s, `INSERT INTO claim_mentions(owner_id,claim_id,claim_version,entity_id,role) VALUES($1,$2,1,$3,'place'),($1,$2,1,$4,'person')`, scope.OwnerID, b3FixedID(n), b3FixedID(2), b3FixedID(1))
	}
}

func b3RecallRequests() []memory.RecallRequest {
	at, _ := time.Parse(time.RFC3339, "2025-08-02T00:00:00Z")
	return []memory.RecallRequest{
		{Query: "neutralneedle", Mode: memory.Remember},
		{Query: "去年说去成都要干什么来着", Mode: memory.Remember},
		{Query: "成都", Mode: memory.Continue},
		{Query: "老王", Mode: memory.History},
		{Query: "无匹配暗号", Mode: memory.Remember},
		{Query: "neutralneedle", Mode: memory.Remember, Budget: memory.Budget{Candidates: 1}},
		{Query: "neutralneedle", Mode: memory.Remember, Budget: memory.Budget{Candidates: 2, Edges: 2, Tokens: 100, Hops: 1}},
		{Query: "neutralneedle", Mode: memory.Remember, Budget: memory.Budget{Candidates: 50, Edges: 100, Tokens: 2000, Hops: 2}},
		{Query: "2025 年 3 月计划", Mode: memory.History, Context: memory.WorkingContext{KnownAt: &at}},
		{Query: "neutralneedle", Mode: memory.Remember, Context: memory.WorkingContext{ValidAt: &at}},
		{Query: "neutralneedle", Mode: memory.Continue, Context: memory.WorkingContext{Objects: []memory.ID{b3FixedID(1)}}},
		{Query: "neutralneedle", Mode: memory.Remember, Context: memory.WorkingContext{Text: "成都旅行", Objects: []memory.ID{b3FixedID(2)}}},
		{Query: "neutralneedle", Mode: memory.History},
		{Query: "neutralneedle", Mode: memory.Remember, Context: memory.WorkingContext{KnownAt: &at, ValidAt: &at}},
		{Query: "neutralneedle", Mode: memory.Continue, Budget: memory.Budget{Candidates: 3, Edges: 1, Tokens: 40, Hops: 1}},
	}
}
