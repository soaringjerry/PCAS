package postgres

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/worker"
)

// Only the explicitly launched child opens the parent's temporary schema.
func TestPhase2B4bWorkerChild(t *testing.T) {
	if os.Getenv("PCAS_B4B_WORKER_CHILD") != "1" {
		return
	}
	s, err := Open(context.Background(), os.Getenv("PCAS_B4B_WORKER_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.SetModels(&ai.Registry{HTTP: &http.Client{Timeout: 30 * time.Second}, Config: ai.Configuration{Extraction: "model", Providers: []ai.Provider{{ID: "model", Name: "合成对话抽取", Protocol: "openai", BaseURL: os.Getenv("PCAS_B4B_WORKER_URL"), Model: "test", MaxOutput: 100, CostMode: "free"}}}})
	for n := 0; n < 20; n++ {
		job, err := s.Claim(context.Background(), time.Minute)
		if err != nil || job == nil {
			t.Fatal("child expected another conversation job", err)
		}
		data, err := json.Marshal(job)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(os.Getenv("PCAS_B4B_WORKER_JOB"), data, 0600); err != nil {
			t.Fatal(err)
		}
		if err = s.ProcessExtraction(context.Background(), *job); err != nil {
			t.Fatal(err)
		}
	}
	t.Fatal("child did not reach the held generation")
}

func TestPhase2B4b_C7_KilledWorkerResumesWithoutReplayingCompletedSegment(t *testing.T) {
	s, scope := testStore(t), owner()
	spec := b4bSpec[struct {
		Memories  int `json:"memories"`
		Organized int `json:"organized"`
	}](t, "C7")
	first, last := "第一段要去成都。", "第二段要去杭州。"
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	var calls atomic.Int32
	f := b4bModel(t, s, func(in b4bInput) any {
		call := calls.Add(1)
		if call == 2 {
			close(entered)
			<-release
		}
		if in.Messages[0].Index == 1 {
			return map[string]any{"items": []any{b4bItem(1, "第一段成都记忆", first, "plan")}}
		}
		return map[string]any{"items": []any{b4bItem(4, "第二段杭州记忆", last, "plan")}}
	})
	t.Cleanup(unblock)
	a := b4bLongFixture(t, s, scope, []string{first, "", "", last})
	b4bOperation(t, s, scope, a.Batch, "organize")
	dir := t.TempDir()
	jobFile := filepath.Join(dir, "leased-job.json")
	log, err := os.Create(filepath.Join(dir, "child.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	cmd := exec.Command(os.Args[0], "-test.run=^TestPhase2B4bWorkerChild$", "-test.v")
	cmd.Stdout = log
	cmd.Stderr = log
	cmd.Env = append(os.Environ(), "PCAS_B4B_WORKER_CHILD=1", "PCAS_B4B_WORKER_DSN="+s.pool.Config().ConnConfig.ConnString(), "PCAS_B4B_WORKER_URL="+s.models.Config.Providers[0].BaseURL, "PCAS_B4B_WORKER_JOB="+jobFile)
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "owned-child.pid"), []byte(strconv.Itoa(cmd.Process.Pid)), 0600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		unblock()
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})
	select {
	case <-entered:
	case <-time.After(45 * time.Second):
		t.Fatal("child did not enter second segment generation")
	}
	// The first segment really committed while the second HTTP call is held.
	b2Equal(t, len(b2Snapshot(t, s, scope).Memories), 1)
	data, err := os.ReadFile(jobFile)
	if err != nil {
		t.Fatal(err)
	}
	var leased worker.Job
	if err = json.Unmarshal(data, &leased); err != nil {
		t.Fatal(err)
	}
	if err = cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err = cmd.Wait(); err == nil {
		t.Fatal("killed worker unexpectedly succeeded")
	}
	unblock()
	b2Exec(t, s, `UPDATE memory_jobs SET lease_until=clock_timestamp()-interval '1 second' WHERE id=$1 AND lease_token=$2 AND state='leased'`, string(leased.ID), string(leased.LeaseToken))
	b4bDrain(t, s)
	memories := b2Snapshot(t, s, scope).Memories
	b2Equal(t, len(memories), spec.Memories)
	seen := map[string]int{}
	for _, m := range memories {
		seen[m.Text]++
		switch m.Text {
		case "第一段成都记忆":
			b4bEvidence(t, s, scope, m, a.Sources[0], first, a.Messages[0].At)
		case "第二段杭州记忆":
			b4bEvidence(t, s, scope, m, a.Sources[3], last, a.Messages[3].At)
		default:
			t.Error("unexpected recovery memory", m.Text)
		}
	}
	b2Equal(t, seen["第一段成都记忆"], 1)
	b2Equal(t, seen["第二段杭州记忆"], 1)
	firstCalls, secondCalls := 0, 0
	for _, req := range f.all() {
		in := b4bInputFrom(t, req)
		switch in.Messages[0].Index {
		case 1:
			firstCalls++
		case 3:
			secondCalls++
		}
	}
	b2Equal(t, firstCalls, 1)
	b2Equal(t, secondCalls, 2)
	b2Equal(t, b4bProgress(t, s, scope, a.Batch).Organized, spec.Organized)
	for n, src := range a.Sources {
		state, count := "empty", 0
		if n == 0 || n == 3 {
			state, count = "done", 1
		}
		b4bRecord(t, s, scope, src, state, count)
	}
	// Any remaining family work must be exhausted after the resumed commit.
	b2Equal(t, b2Count(t, s, `SELECT count(*) FROM memory_jobs WHERE owner_id=$1 AND stage LIKE 'source.extract%' AND state IN('queued','leased')`, string(memory.ID(scope.OwnerID))), 0)
}
