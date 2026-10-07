package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func TestPhase26C4HeavyReaderPagesAll1200AndDisclosesSkippedGroups(t *testing.T) {
	f := phase26LoadFixture(t)
	m := phase26NewModel(t, f)
	phase26Exec(t, f, `INSERT INTO record_grants(owner_id,record_id,principal_id) SELECT owner_id,id,'phase26' FROM memory_records WHERE owner_id=$1 ON CONFLICT DO NOTHING`, f.Scope.OwnerID)
	if _, err := f.Store.Snapshot(f.Context, f.Scope); err != nil {
		t.Fatal(err)
	}
	ids := regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)
	m.mu.Lock()
	m.Reply = func(c phase26Call) string {
		b, _ := json.Marshal(map[string]any{"used": ids.FindAllString(c.Prompt, -1)})
		return string(b)
	}
	m.mu.Unlock()
	key := "entity:" + string(f.Entities[0])
	u := useContext{Location: time.UTC, Ready: true, Selected: true, RequiredGroups: []string{key}, Coverage: &useCoverage{}, Index: []workspace.StatusCardRef{{Key: key, Name: f.Corpus.Groups[0].Name, Kind: "person", Count: 1200}}}
	agent := workspace.Agent{ID: "phase26", MemoryKinds: []string{"fact", "preference", "decision", "intention", "plan"}}
	start := time.Now()
	picked, refs, keys := f.Store.heavyUse(f.Context, f.Context, f.Scope, agent, nil, "Inspect this fictitious archive completely", u, string(memory.NewID()), "")
	seen := map[memory.ID]bool{}
	for _, r := range refs {
		seen[r.ID] = true
	}
	for _, i := range f.Corpus.Groups[0].Members {
		if !seen[f.Claims[i]] {
			t.Errorf("heavy reader omitted member %d", i)
		}
	}
	calls := m.calls("secretary")
	if len(picked) != 1200 || len(refs) != 1200 || len(keys) != 1 || len(calls) != 12 || len(u.Coverage.Skipped) != 0 {
		t.Errorf("heavy complete read picked=%d refs=%d keys=%v calls=%d skipped=%v", len(picked), len(refs), keys, len(calls), u.Coverage.Skipped)
	}
	for _, c := range calls {
		if len(ids.FindAllString(c.Prompt, -1)) > 101 {
			t.Error("reader input not paged at 100 memories")
		}
	}
	t.Logf("largest heavy group pages=%d refs=%d elapsed=%s", len(calls), len(refs), time.Since(start))
	// A deliberately exhausted independent reader deadline exercises truthful
	// coverage/overflow reporting; it is not a user-latency benchmark.
	u.RequiredGroups = nil
	u.Index = nil
	u.Coverage = &useCoverage{}
	for g := 0; g < 13; g++ {
		k := "entity:" + string(f.Entities[g])
		u.RequiredGroups = append(u.RequiredGroups, k)
		u.Index = append(u.Index, workspace.StatusCardRef{Key: k, Name: f.Corpus.Groups[g].Name})
	}
	canceled, cancel := context.WithCancel(f.Context)
	cancel()
	_, _, _ = f.Store.heavyUse(canceled, f.Context, f.Scope, agent, nil, "Fictitious exhausted deadline", u, string(memory.NewID()), "")
	if len(u.Coverage.Skipped) != 13 {
		t.Errorf("skipped names=%d expected13", len(u.Coverage.Skipped))
	}
	var b strings.Builder
	writeUseContext(&b, u, time.UTC, func(workspace.Memory) {})
	for _, g := range u.Index {
		if !strings.Contains(b.String(), g.Name) {
			t.Errorf("user coverage disclosure omitted %s", g.Name)
		}
	}
	health, err := f.Store.BackgroundHealth(f.Context, f.Scope)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(health), "reader_group_limit") || !strings.Contains(string(health), "reader_timeout_or_failure") {
		t.Errorf("reader overflow hidden: %s", health)
	}
}

func TestPhase26C5OnlyChangedActionTargetInvalidatesAnswer(t *testing.T) {
	f := phase26LoadFixture(t)
	m := phase26NewModel(t, f)
	phase26Exec(t, f, `INSERT INTO record_grants(owner_id,record_id,principal_id) SELECT owner_id,id,'phase26' FROM memory_records WHERE owner_id=$1 ON CONFLICT DO NOTHING`, f.Scope.OwnerID)
	state, err := f.Store.Execute(f.Context, f.Scope, workspace.Command{Type: "addTask", Title: "Fictitious original task", RequestID: string(memory.NewID())})
	if err != nil {
		t.Fatal(err)
	}
	target := state.Tasks[0].ID
	for _, changeTarget := range []bool{false, true} {
		t.Run(fmt.Sprintf("target_changed_%t", changeTarget), func(t *testing.T) {
			entered, release := make(chan struct{}), make(chan struct{})
			m.mu.Lock()
			m.Before = func(ctx context.Context, c phase26Call) {
				close(entered)
				select {
				case <-release:
				case <-ctx.Done():
				}
			}
			m.Reply = func(phase26Call) string {
				if changeTarget {
					return `{"reply":"Fictitious proposed update.","used":[],"actions":[{"op":"update","ref":"T1","set":{"title":"Fictitious model overwrite"}}],"memoryPlan":{"depth":"light","groups":[]}}`
				}
				return `{"reply":"Fictitious acceptance reply.","used":[],"actions":[],"memoryPlan":{"depth":"light","groups":[]}}`
			}
			m.mu.Unlock()
			before := len(m.calls("secretary"))
			type result struct {
				out workspace.DeskTurnResponse
				err error
			}
			done := make(chan result, 1)
			go func() {
				out, err := f.Store.DeskTurn(WithMemoryTier(f.Context, "light"), f.Scope, workspace.DeskTurnRequest{AgentID: "phase26", RequestID: string(memory.NewID()), Text: "Answer this fictitious question and update the fictitious task if asked."})
				done <- result{out, err}
			}()
			select {
			case <-entered:
			case r := <-done:
				t.Fatalf("no in-flight provider: %v", r.err)
			case <-time.After(15 * time.Second):
				t.Fatal("provider barrier timeout")
			}
			cmd := workspace.Command{Type: "addTask", Title: "Fictitious unrelated task", RequestID: string(memory.NewID())}
			if changeTarget {
				cmd = workspace.Command{Type: "updateTask", ID: target, Patch: json.RawMessage(`{"title":"Fictitious user change"}`), RequestID: string(memory.NewID())}
			}
			if _, err := f.Store.Execute(f.Context, f.Scope, cmd); err != nil {
				close(release)
				<-done
				t.Fatal(err)
			}
			close(release)
			r := <-done
			if r.err != nil {
				t.Fatal(r.err)
			}
			if !changeTarget && r.out.Turn.Reply != "Fictitious acceptance reply." {
				t.Error("unrelated edit discarded completed answer")
			}
			if changeTarget {
				current, err := f.Store.Snapshot(f.Context, f.Scope)
				if err != nil {
					t.Fatal(err)
				}
				for _, task := range current.Tasks {
					if task.ID == target && task.Title != "Fictitious user change" {
						t.Error("model overwrote changed action target")
					}
				}
				receipt, _ := json.Marshal(r.out.Turn.Receipts)
				if !strings.Contains(string(receipt), "重新") && !strings.Contains(string(receipt), "重试") {
					t.Errorf("changed target gave no retry guidance: %s", receipt)
				}
				if strings.Contains(string(receipt), "自动整理") {
					t.Error("contradictory automatic retry receipt")
				}
			}
			if len(m.calls("secretary"))-before != 1 {
				t.Error("single change duplicated primary model call")
			}
			t.Logf("changed_target=%t reply=%q receipts=%+v", changeTarget, r.out.Turn.Reply, r.out.Turn.Receipts)
		})
	}
}
