package postgres_test

import (
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/postgres"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func TestPhase25B1_X22_TwoWorkersTwelveRounds(t *testing.T) {
	f := phase25B1NewFixture(t)
	replyItems := phase25B1Items(40, "progress", false)
	for i := range replyItems {
		replyItems[i].Topics = []string{"虚构并发"}
	}
	reply := phase25B1ModelJSON(t, replyItems, phase25B1NewGroup{Type: "topic", Name: "虚构并发", Description: "虚构并发整理测试"})
	// The first worker is paused inside its model call. The second really calls
	// ScheduleOrganize and Claim during that interval, then releases the first.
	var entered, release chan struct{}
	fake := phase25B1NewFakeModel(t, func(call int, _ phase25B1ModelRequest) (phase25B1ModelResponse, error) {
		if call%2 == 1 {
			close(entered)
			select {
			case <-release:
			case <-f.ctx.Done():
				return phase25B1ModelResponse{}, f.ctx.Err()
			}
		}
		return phase25B1ModelResponse{Text: reply}, nil
	})
	f.store.SetModels(fake.registry)
	for round := 0; round < 12; round++ {
		for i := 0; i < 41; i++ {
			f.claim(t, fmt.Sprintf("虚构并发第%d轮第%d条。", round, i))
		}
		entered, release = make(chan struct{}), make(chan struct{})
		if f.schedule(t) != 1 {
			t.Fatal("initial job not queued")
		}
		var workers sync.WaitGroup
		workers.Add(2)
		errors := make(chan error, 2)
		go func() {
			defer workers.Done()
			j, err := f.store.Claim(f.ctx, 30*time.Second)
			if err != nil {
				errors <- err
				close(entered)
				return
			}
			if j == nil {
				errors <- fmt.Errorf("first worker claimed no job")
				close(entered)
				return
			}
			errors <- f.store.ProcessOrganize(f.ctx, *j)
		}()
		go func() {
			defer workers.Done()
			defer close(release)
			select {
			case <-entered:
			case <-f.ctx.Done():
				errors <- f.ctx.Err()
				return
			}
			count, err := f.store.ScheduleOrganize(f.ctx, time.Now())
			if err != nil {
				errors <- err
				return
			}
			if count != 0 {
				errors <- fmt.Errorf("second worker queued %d duplicate jobs", count)
				return
			}
			j, err := f.store.Claim(f.ctx, 30*time.Second)
			if err != nil {
				errors <- err
				return
			}
			if j != nil {
				errors <- fmt.Errorf("second worker claimed duplicate task %s", j.Stage)
				return
			}
			errors <- nil
		}()
		workers.Wait()
		close(errors)
		for err := range errors {
			if err != nil {
				t.Fatal(err)
			}
		}
		// Both workers race for the continuation. Only its actual claimant runs it.
		workers.Add(2)
		errors = make(chan error, 2)
		start := make(chan struct{})
		for i := 0; i < 2; i++ {
			go func() {
				defer workers.Done()
				<-start
				j, err := f.store.Claim(f.ctx, 30*time.Second)
				if err == nil && j != nil {
					err = f.store.ProcessOrganize(f.ctx, *j)
				}
				errors <- err
			}()
		}
		close(start)
		workers.Wait()
		close(errors)
		for err := range errors {
			if err != nil {
				t.Fatal(err)
			}
		}
		if fake.calls() != (round+1)*2 {
			t.Fatalf("round %d calls = %d", round, fake.calls())
		}
	}
	seen := make(map[memory.ID]int)
	for _, batch := range f.usages(t) {
		for _, ref := range batch {
			seen[ref.ID]++
		}
	}
	if len(seen) != 12*41 {
		t.Errorf("unique organized references = %d", len(seen))
	}
	for id, count := range seen {
		if count != 1 {
			t.Errorf("memory %s billed in %d batches", id, count)
		}
	}
	var groups int
	if err := f.db.QueryRow(f.ctx, `SELECT count(*) FROM entity_versions WHERE owner_id=$1 AND entity_type='topic' AND name='虚构并发'`, f.scope.OwnerID).Scan(&groups); err != nil {
		t.Fatal(err)
	}
	if groups != 1 {
		t.Errorf("concurrent group entities = %d", groups)
	}
	var state workspace.State
	f.get(t, "/v1/workspace", &state)
	if state.Organize.Done != 492 || state.Organize.Total != 492 {
		t.Errorf("final concurrent progress = %+v", state.Organize)
	}
	t.Log("12 rounds, 492 memories, 24 model calls; two real queue claimants")
}

func TestPhase25B1_RandomSequenceInvariants(t *testing.T) {
	oldVersion := postgres.OrganizeVersion
	defer func() { postgres.OrganizeVersion = oldVersion }()
	f := phase25B1NewFixture(t)
	const seed int64 = 252501
	rng := rand.New(rand.NewSource(seed))
	ordinary := map[string]memory.ID{"person": f.entity(t, "person", "虚构人物许澄"), "place": f.entity(t, "place", "虚构松湾"), "organization": f.entity(t, "organization", "虚构蓝鹭公司")}
	refs := make([]memory.Ref, 0)
	add := func() {
		ref := f.claim(t, fmt.Sprintf("虚构随机记忆编号%d，标题使用青色。", rng.Int63()))
		for role, id := range ordinary {
			f.exec(t, `INSERT INTO claim_mentions(owner_id,claim_id,claim_version,entity_id,role) VALUES($1,$2,$3,$4,$5)`, f.scope.OwnerID, ref.ID, ref.Version, id, role)
		}
		refs = append(refs, ref)
	}
	for i := 0; i < 3; i++ {
		add()
	}
	items := phase25B1Items(40, "rule", true)
	for i := range items {
		items[i].Topics = []string{"虚构随机"}
		items[i].Area = "工作"
	}
	fake := f.model(t, phase25B1ModelJSON(t, items, phase25B1NewGroup{Type: "topic", Name: "虚构随机", Description: "虚构随机序列测试"}))
	check := func(step int) {
		for _, ref := range refs {
			var version, revisions int
			if err := f.db.QueryRow(f.ctx, `SELECT r.version,(SELECT count(*) FROM claim_revisions cr WHERE cr.owner_id=r.owner_id AND cr.claim_id=r.id) FROM memory_records r WHERE r.owner_id=$1 AND r.id=$2`, f.scope.OwnerID, ref.ID).Scan(&version, &revisions); err != nil {
				t.Fatal(err)
			}
			if version != ref.Version || revisions != ref.Version {
				t.Fatalf("seed %d step %d revision mismatch for %s: %d/%d, want %d", seed, step, ref.ID, version, revisions, ref.Version)
			}
			rows, err := f.db.Query(f.ctx, `SELECT role,entity_id::text FROM claim_mentions WHERE owner_id=$1 AND claim_id=$2 AND claim_version=$3 AND role IN ('person','place','organization')`, f.scope.OwnerID, ref.ID, ref.Version)
			if err != nil {
				t.Fatal(err)
			}
			got := make(map[string]memory.ID)
			for rows.Next() {
				var role, id string
				if err := rows.Scan(&role, &id); err != nil {
					rows.Close()
					t.Fatal(err)
				}
				got[role] = memory.ID(id)
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 3 {
				t.Fatalf("step %d ordinary mentions = %+v", step, got)
			}
			for role, id := range ordinary {
				if got[role] != id {
					t.Fatalf("step %d ordinary mention %s changed", step, role)
				}
			}
		}
		var invalid, duplicates, done int
		if err := f.db.QueryRow(f.ctx, `SELECT count(*) FROM claims c JOIN memory_records r ON r.owner_id=c.owner_id AND r.id=c.id JOIN claim_revisions cr ON cr.owner_id=c.owner_id AND cr.claim_id=c.id AND cr.version=r.version WHERE c.owner_id=$1 AND c.organized=$2 AND (cr.category IS NULL OR cr.category='')`, f.scope.OwnerID, postgres.OrganizeVersion).Scan(&invalid); err != nil {
			t.Fatal(err)
		}
		if invalid != 0 {
			t.Fatalf("step %d empty organized categories = %d", step, invalid)
		}
		if err := f.db.QueryRow(f.ctx, `SELECT count(*) FROM claim_mentions m LEFT JOIN entities e ON e.owner_id=m.owner_id AND e.id=m.entity_id WHERE m.owner_id=$1 AND m.role IN ('project','topic','area') AND e.id IS NULL`, f.scope.OwnerID).Scan(&invalid); err != nil {
			t.Fatal(err)
		}
		if invalid != 0 {
			t.Fatalf("step %d dangling group refs = %d", step, invalid)
		}
		if err := f.db.QueryRow(f.ctx, `SELECT count(*) FROM (SELECT ev.entity_type,lower(btrim(ev.name)) FROM entity_versions ev JOIN memory_records r ON r.owner_id=ev.owner_id AND r.id=ev.entity_id AND r.version=ev.version WHERE ev.owner_id=$1 AND ev.entity_type IN ('project','topic','area') GROUP BY ev.entity_type,lower(btrim(ev.name)) HAVING count(*)>1) duplicates`, f.scope.OwnerID).Scan(&duplicates); err != nil {
			t.Fatal(err)
		}
		if duplicates != 0 {
			t.Fatalf("step %d duplicate group entities = %d", step, duplicates)
		}
		if err := f.db.QueryRow(f.ctx, `SELECT count(*) FROM claims c JOIN memory_records r ON r.owner_id=c.owner_id AND r.id=c.id WHERE c.owner_id=$1 AND r.state='active' AND c.organized=$2`, f.scope.OwnerID, postgres.OrganizeVersion).Scan(&done); err != nil {
			t.Fatal(err)
		}
		state, err := f.store.Snapshot(f.ctx, f.scope)
		if err != nil {
			t.Fatal(err)
		}
		if state.Organize.Done != done || state.Organize.Total != len(refs) || state.Organize.Version != postgres.OrganizeVersion {
			t.Fatalf("step %d snapshot = %+v, actual done=%d,total=%d", step, state.Organize, done, len(refs))
		}
	}
	check(-1)
	counts := make([]int, 5)
	for block := 0; block < 16; block++ {
		for offset, action := range rng.Perm(5) {
			step := block*5 + offset
			counts[action]++
			switch action {
			case 0:
				add()
			case 1:
				f.schedule(t)
				if j := f.claimOrganize(t); j != nil {
					if err := f.store.ProcessOrganize(f.ctx, *j); err != nil {
						t.Fatal(err)
					}
				}
			case 2:
				i := rng.Intn(len(refs))
				ref, err := f.correction(refs[i], "虚构随机纠正：改用紫色标题。")
				if err != nil {
					t.Fatal(err)
				}
				refs[i] = ref
			case 3:
				i := rng.Intn(len(refs))
				if err := f.store.Delete(f.ctx, f.scope, memory.DeleteRequest{Targets: []memory.Ref{refs[i]}}); err != nil {
					t.Fatal(err)
				}
				refs = append(refs[:i], refs[i+1:]...)
			case 4:
				postgres.OrganizeVersion++
			}
			check(step)
		}
	}
	if fake.calls() > 16 {
		t.Errorf("random sequence unexpectedly exceeded 16 model batches: %d", fake.calls())
	}
	t.Logf("seed=%d, steps=80, action counts=%v, model calls=%d", seed, counts, fake.calls())
}

func TestPhase25B1_R01_OnlyCurrentActiveRevisions(t *testing.T) {
	f := phase25B1NewFixture(t)
	ref := f.claim(t, "虚构旧版本便签。")
	current, err := f.correction(ref, "虚构当前版本便签。")
	if err != nil {
		t.Fatal(err)
	}
	withdrawn := f.claim(t, "虚构撤回便签。")
	f.exec(t, `UPDATE memory_records SET state='withdrawn' WHERE owner_id=$1 AND id=$2`, f.scope.OwnerID, withdrawn.ID)
	f.model(t, phase25B1ModelJSON(t, phase25B1Items(40, "progress", false)))
	f.batch(t)
	f.checkClaim(t, current, "progress", 1, 0)
	var category string
	if err := f.db.QueryRow(f.ctx, `SELECT category FROM claim_revisions WHERE owner_id=$1 AND claim_id=$2 AND version=$3`, f.scope.OwnerID, ref.ID, ref.Version).Scan(&category); err != nil {
		t.Fatal(err)
	}
	if category != "unknown" {
		t.Errorf("old revision category = %s", category)
	}
	f.checkClaim(t, withdrawn, "unknown", 0, 0)
	batches := f.usages(t)
	if len(batches) != 1 || len(batches[0]) != 1 || batches[0][0] != current {
		t.Errorf("selected revisions = %+v", batches)
	}
	var state workspace.State
	f.get(t, "/v1/workspace", &state)
	if state.Organize.Total != 1 || state.Organize.Done != 1 {
		t.Errorf("active progress = %+v", state.Organize)
	}
}

func TestPhase25B1_R03_GroupAliasAndCaseReuse(t *testing.T) {
	f := phase25B1NewFixture(t)
	topic := f.entity(t, "topic", "虚构月报")
	// An alias is a supplied fixture, not a model-invented fuzzy merge.
	ref := f.claim(t, "虚构月报名称的别名用于分组复用。")
	f.exec(t, `INSERT INTO aliases(owner_id,entity_id,entity_version,alias) VALUES($1,$2,1,'Monthly')`, f.scope.OwnerID, topic)
	f.model(t, phase25B1ModelJSON(t, []phase25B1ModelItem{{Number: 1, Category: "rule", Durable: true, Topics: []string{" monthly "}}}))
	f.batch(t)
	m, err := f.store.GetMemory(f.ctx, f.scope, string(ref.ID))
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Groups) != 1 || m.Groups[0].EntityID != string(topic) || strings.TrimSpace(m.Groups[0].Name) != "虚构月报" {
		t.Errorf("alias grouping = %+v", m.Groups)
	}
}
