package postgres

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/worker"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// Fixtures contain invented utterances only. Comparisons use the real scheduler,
// lease, provider adapter, writer and workspace command/undo entry points.
func compareFixture(t *testing.T, s *Store, scope memory.Scope, texts ...string) []memory.Ref {
	t.Helper()
	refs := []memory.Ref{}
	for i, text := range texts {
		src := b1Source(t, s, scope, "虚构比较资料", text, "manual")
		ref := b1Claim(t, s, scope, text, "fact", "candidate", src)
		err := pgx.BeginFunc(context.Background(), s.pool, func(tx pgx.Tx) error {
			self, err := selfEntityTx(context.Background(), tx, scope.OwnerID)
			if err != nil {
				return err
			}
			group, err := entityTx(context.Background(), tx, scope.OwnerID, "topic", "虚构汇报")
			if err != nil {
				return err
			}
			if _, err = tx.Exec(context.Background(), "UPDATE claim_revisions SET subject_id=$3,category='progress',durable=true WHERE owner_id=$1 AND claim_id=$2", string(scope.OwnerID), string(ref.ID), string(self)); err != nil {
				return err
			}
			if _, err = tx.Exec(context.Background(), "INSERT INTO claim_mentions(owner_id,claim_id,claim_version,entity_id,role) VALUES($1,$2,1,$3,'topic') ON CONFLICT DO NOTHING", string(scope.OwnerID), string(ref.ID), string(group)); err != nil {
				return err
			}
			if _, err = tx.Exec(context.Background(), "UPDATE claims SET organized=$3 WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), string(ref.ID), OrganizeVersion); err != nil {
				return err
			}
			_, err = tx.Exec(context.Background(), "UPDATE record_versions SET expressed_at=$3 WHERE owner_id=$1 AND record_id=$2", string(scope.OwnerID), string(ref.ID), time.Date(2026, 9, 12, 9, 0, i, 0, time.UTC))
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		refs = append(refs, ref)
	}
	return refs
}
func compareJob(t *testing.T, s *Store, scope memory.Scope, entity bool, version int) worker.Job {
	t.Helper()
	ctx := context.Background()
	if _, err := s.scheduleCompareVersion(ctx, time.Now(), version); err != nil {
		t.Fatal(err)
	}
	stage := CompareStage
	if entity {
		stage = EntityCompareStage
	}
	if _, err := s.pool.Exec(ctx, "DELETE FROM memory_jobs WHERE owner_id=$1 AND state='queued' AND stage NOT LIKE $2", string(scope.OwnerID), stage+":%"); err != nil {
		t.Fatal(err)
	}
	j, err := s.Claim(ctx, 5*time.Minute)
	if err != nil || j == nil || !strings.HasPrefix(j.Stage, stage+":") {
		t.Fatal("lease comparison", j, err)
	}
	return *j
}
func compareProcess(t *testing.T, s *Store, scope memory.Scope) {
	t.Helper()
	j := compareJob(t, s, scope, false, CompareVersion)
	if err := s.ProcessCompare(context.Background(), j); err != nil {
		t.Fatal(err)
	}
}
func compareDetail(t *testing.T, s *Store, scope memory.Scope, ref memory.Ref) workspace.Memory {
	t.Helper()
	m, err := s.GetMemory(context.Background(), scope, string(ref.ID))
	if err != nil {
		t.Fatal(err)
	}
	return m
}
func compareState(t *testing.T, s *Store, scope memory.Scope, refs []memory.Ref, retired []string, compared int) {
	t.Helper()
	for i, ref := range refs {
		var r string
		var c, v int
		if err := s.pool.QueryRow(context.Background(), "SELECT cl.retired,cl.compared,r.version FROM claims cl JOIN memory_records r ON(r.owner_id,r.id)=(cl.owner_id,cl.id) WHERE cl.owner_id=$1 AND cl.id=$2", string(scope.OwnerID), string(ref.ID)).Scan(&r, &c, &v); err != nil {
			t.Fatal(err)
		}
		if r != retired[i] || c != compared || v != ref.Version {
			t.Errorf("memory %d retired=%q compared=%d version=%d", i, r, c, v)
		}
	}
}
func compareList(t *testing.T, s *Store, scope memory.Scope, retired bool) []workspace.Memory {
	t.Helper()
	p, err := s.ListMemories(context.Background(), scope, workspace.MemoryQuery{Retired: retired, Limit: 500})
	if err != nil {
		t.Fatal(err)
	}
	return p.Items
}
func TestCompareDuplicateEvidenceAndHistoricalDeletion(t *testing.T) {
	s := testStore(t)
	scope := owner()
	f := b1Model(t, s, `{"duplicates":[{"keep":3,"members":[1,2,3]}],"superseded":[]}`)
	refs := compareFixture(t, s, scope, "虚构汇报周五提交", "周五提交虚构汇报", "虚构汇报周五提交给主管")
	compareProcess(t, s, scope)
	compareState(t, s, scope, refs, []string{"duplicate", "duplicate", ""}, CompareVersion)
	m := compareDetail(t, s, scope, refs[2])
	if m.MergedFrom != 2 || len(m.Sources) != 3 || m.Trust != "repeated" {
		t.Fatal(m)
	}
	for _, ref := range refs[:2] {
		m := compareDetail(t, s, scope, ref)
		if m.RetiredBy != string(refs[2].ID) {
			t.Fatal(m)
		}
	}
	if len(compareList(t, s, scope, false)) != 1 || len(compareList(t, s, scope, true)) != 2 {
		t.Fatal("current/history split")
	}
	var prompt struct{ Memories []compareMemory }
	if err := json.Unmarshal([]byte(strings.TrimSpace(f.last(t).Prompt)), &prompt); err != nil || len(prompt.Memories) != 3 {
		t.Fatal(err, f.last(t).Prompt)
	}
	b1Delete(t, s, scope, refs[2])
	if len(compareList(t, s, scope, false)) != 0 || len(compareList(t, s, scope, true)) != 2 {
		t.Fatal("historical duplicates resurrected after keeper deletion")
	}
}
func TestCompareSupersessionAndDependencyValidity(t *testing.T) {
	for _, kind := range []string{"duplicate", "superseded"} {
		t.Run(kind, func(t *testing.T) {
			s := testStore(t)
			scope := owner()
			f := b1Model(t, s, `{"reply":"期限是周三。","used":["M1"],"actions":[]}`)
			refs := compareFixture(t, s, scope, "虚构汇报期限是周三")
			req := turnRequest("虚构汇报期限是什么")
			out := mustTurn(t, s, scope, req)
			b1HasRef(t, b1Refs(t, s, scope, req.RequestID), refs[0], true)
			newer := compareFixture(t, s, scope, "虚构汇报期限改到周五")[0]
			if _, err := s.pool.Exec(context.Background(), "UPDATE record_versions SET expressed_at=expressed_at+interval '1 day' WHERE owner_id=$1 AND record_id=$2", string(scope.OwnerID), string(newer.ID)); err != nil {
				t.Fatal(err)
			}
			if kind == "duplicate" {
				f.set(`{"duplicates":[{"keep":2,"members":[1,2]}],"superseded":[]}`)
			} else {
				f.set(`{"duplicates":[],"superseded":[{"old":1,"new":2}]}`)
			}
			compareProcess(t, s, scope)
			history := b1History(t, s, scope, out.ConversationID)
			b1Outdated(t, history, kind == "superseded")
			if history.Text != out.Turn.Text || history.Reply != out.Turn.Reply {
				t.Fatal("history changed")
			}
			if len(compareList(t, s, scope, false)) != 1 {
				t.Fatal("retired memory still current")
			}
			for _, mode := range []memory.RecallMode{memory.Remember, memory.History} {
				recalled, err := s.Recall(context.Background(), scope, memory.RecallRequest{Query: "虚构汇报", Mode: mode})
				if err != nil {
					t.Fatal(err)
				}
				for _, ref := range recalled.Memories {
					if ref.ID == refs[0].ID {
						t.Fatal("recall returned retired memory", mode)
					}
				}
			}

		})
	}
}
func TestCompareProtectConfirmedEditedAndCycles(t *testing.T) {
	s := testStore(t)
	scope := owner()
	b1Model(t, s, `{"duplicates":[],"superseded":[{"old":1,"new":2},{"old":2,"new":1},{"old":3,"new":5},{"old":4,"new":5}]}`)
	refs := compareFixture(t, s, scope, "甲旧说法", "乙旧说法", "本人确认的说法", "本人改过的说法", "新说法")
	if _, err := s.pool.Exec(context.Background(), "UPDATE claim_revisions SET confirmation='confirmed' WHERE owner_id=$1 AND claim_id=$2", string(scope.OwnerID), string(refs[2].ID)); err != nil {
		t.Fatal(err)
	}
	b1Correct(t, s, scope, refs[3], "本人改过的说法")
	refs[3].Version++
	if _, err := s.pool.Exec(context.Background(), "UPDATE claims SET organized=$3 WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), string(refs[3].ID), OrganizeVersion); err != nil {
		t.Fatal(err)
	}
	// Preserve local fixture ordering across the new user revision.
	if _, err := s.pool.Exec(context.Background(), "UPDATE record_versions SET expressed_at=$3 WHERE owner_id=$1 AND record_id=$2 AND version=2", string(scope.OwnerID), string(refs[3].ID), time.Date(2026, 9, 12, 9, 0, 3, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	compareProcess(t, s, scope)
	compareState(t, s, scope, refs, []string{"", "", "", "", ""}, CompareVersion)
}
func TestCompareRestoreUndoAndVersionBump(t *testing.T) {
	s := testStore(t)
	scope := owner()
	f := b1Model(t, s, `{"duplicates":[],"superseded":[{"old":1,"new":2}]}`)
	refs := compareFixture(t, s, scope, "虚构期限周三", "虚构期限周五")
	compareProcess(t, s, scope)
	state, err := s.Snapshot(context.Background(), scope)
	if err != nil {
		t.Fatal(err)
	}
	cmd := workspace.Command{Type: "restoreMemory", ID: string(refs[0].ID), RequestID: string(memory.NewID()), ExpectedRevision: state.Revision}
	if _, err = s.Execute(context.Background(), scope, cmd); err != nil {
		t.Fatal(err)
	}
	if _, err = s.pool.Exec(context.Background(), "UPDATE claims SET compared=0 WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), string(refs[1].ID)); err != nil {
		t.Fatal(err)
	}
	compareProcess(t, s, scope)
	compareState(t, s, scope, refs, []string{"", ""}, CompareVersion)
	b1Undo(t, s, scope, cmd.RequestID)
	compareState(t, s, scope, refs, []string{"superseded", ""}, CompareVersion)
	f.set(`{"duplicates":[],"superseded":[]}`)
	j := compareJob(t, s, scope, false, CompareVersion+1)
	if err = s.processCompareVersion(context.Background(), j, CompareVersion+1); err != nil {
		t.Fatal(err)
	}
	var compared int
	if err = s.pool.QueryRow(context.Background(), "SELECT compared FROM claims WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), string(refs[1].ID)).Scan(&compared); err != nil || compared != CompareVersion+1 {
		t.Fatal(compared, err)
	}
	if m := compareDetail(t, s, scope, refs[0]); m.Retired != "superseded" {
		t.Fatal("rule bump resurrected old record", m)
	}
}
func TestCompareConcurrentChangesSkipOnlyAffectedEdges(t *testing.T) {
	for _, change := range []string{"delete", "correct"} {
		t.Run(change, func(t *testing.T) {
			s := testStore(t)
			scope := owner()
			b1Model(t, s, `{}`)
			refs := compareFixture(t, s, scope, "旧甲", "新甲", "旧乙", "新乙")
			entered, release := make(chan struct{}), make(chan struct{})
			secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
				close(entered)
				<-release
				secretaryModelReply(w, `{"duplicates":[],"superseded":[{"old":1,"new":2},{"old":3,"new":4}]}`)
			})
			j := compareJob(t, s, scope, false, CompareVersion)
			done := make(chan error, 1)
			go func() { done <- s.ProcessCompare(context.Background(), j) }()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				close(release)
				t.Fatal("no model call")
			}
			if change == "delete" {
				b1Delete(t, s, scope, refs[1])
			} else {
				b1Correct(t, s, scope, refs[1], "新甲再次纠正")
			}
			close(release)
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			if m := compareDetail(t, s, scope, refs[0]); m.Retired != "" {
				t.Fatal("changed edge applied", m)
			}
			if m := compareDetail(t, s, scope, refs[2]); m.Retired != "superseded" || m.RetiredBy != string(refs[3].ID) {
				t.Fatal("unaffected edge discarded", m)
			}
		})
	}
}
func TestCompareInvalidOutputAttemptsAndUsage(t *testing.T) {
	s := testStore(t)
	scope := owner()
	f := b1Model(t, s, "not JSON")
	refs := compareFixture(t, s, scope, "合成比较甲", "合成比较乙")
	for i := 1; i <= 3; i++ {
		compareProcess(t, s, scope)
		want := 0
		if i == 3 {
			want = CompareVersion
		}
		compareState(t, s, scope, refs, []string{"", ""}, want)
	}
	var count int
	if err := s.pool.QueryRow(context.Background(), "SELECT count(*) FROM model_usage WHERE owner_id=$1 AND purpose='compare'", string(scope.OwnerID)).Scan(&count); err != nil || count != 3 || len(f.all()) != 3 {
		t.Fatal(count, err)
	}
}
func TestCompareWindowAndNoModel(t *testing.T) {
	s := testStore(t)
	scope := owner()
	b1Model(t, s, `{}`)
	refs := compareFixture(t, s, scope, "初始虚构记忆")
	s.models = nil
	if n, err := s.ScheduleCompare(context.Background(), time.Now()); err != nil || n != 0 {
		t.Fatal("unconfigured model queued", n, err)
	}
	// Bulk rows use the same synthetic source/subject/group as the first fixture.
	if _, err := s.pool.Exec(context.Background(), `DO $$ DECLARE i int; identifier uuid; o uuid; subject uuid; grp uuid; BEGIN
 SELECT owner_id,subject_id INTO o,subject FROM claim_revisions LIMIT 1;
 SELECT entity_id INTO grp FROM claim_mentions WHERE role='topic' LIMIT 1;
 FOR i IN 1..204 LOOP
 identifier:=gen_random_uuid();
 INSERT INTO memory_records(owner_id,id,kind,version) VALUES(o,identifier,'claim',1);
 INSERT INTO record_versions(owner_id,record_id,version,actor,expressed_at) VALUES(o,identifier,1,'ai','2026-09-12 10:00:00+00'::timestamptz+i*interval '1 second');
 INSERT INTO claims(owner_id,id,organized) VALUES(o,identifier,1);
 INSERT INTO claim_revisions(owner_id,claim_id,version,subject_id,predicate,value,nature,acquisition,confirmation,change_type,category,durable) VALUES(o,identifier,1,subject,'合成批量',to_jsonb('合成记忆'||i),'fact','direct','candidate','initial','progress',true);
 INSERT INTO claim_mentions(owner_id,claim_id,claim_version,entity_id,role) VALUES(o,identifier,1,grp,'topic');
 END LOOP; END $$`); err != nil {
		t.Fatal(err)
	}
	f := b1Model(t, s, `{"duplicates":[],"superseded":[]}`)
	for i, want := range []int{200, 205} {
		compareProcess(t, s, scope)
		var n int
		if err := s.pool.QueryRow(context.Background(), "SELECT count(*) FROM claims WHERE owner_id=$1 AND compared=$2", string(scope.OwnerID), CompareVersion).Scan(&n); err != nil || n != want {
			t.Fatal(i, n, err)
		}
		var p struct{ Memories []compareMemory }
		if err := json.Unmarshal([]byte(strings.TrimSpace(f.last(t).Prompt)), &p); err != nil || len(p.Memories) > 200 {
			t.Fatal(err, len(p.Memories))
		}
	}
	if m := compareDetail(t, s, scope, refs[0]); m.Retired != "" {
		t.Fatal(m)
	}
	if n, err := s.ScheduleCompare(context.Background(), time.Now()); err != nil || n != 0 {
		t.Fatal("completed group repeated", n, err)
	}
}

func TestComparePotentialEntityNames(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want bool
	}{{"小陈", "陈亮", false}, {"陈老师", "陈", true}, {"Alice", "ALICE", true}, {"这位陈老师", "陈", false}, {"那个王先生", "王", false}, {"陈亮", "刘山", false}} {
		if got := possibleSameEntity(tc.a, tc.b); got != tc.want {
			t.Errorf("%q %q=%v", tc.a, tc.b, got)
		}
	}
}
