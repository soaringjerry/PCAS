package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/worker"
)

type phase26Call struct {
	Stage, System, Prompt, Output string
	Elapsed                       time.Duration
}
type phase26Model struct {
	mu     sync.Mutex
	Calls  []phase26Call
	Before func(context.Context, phase26Call)
	Reply  func(phase26Call) string
	f      *phase26LoadedFixture
	URL    string
}

func phase26NewModel(t *testing.T, f *phase26LoadedFixture) *phase26Model {
	t.Helper()
	m := &phase26Model{f: f}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages       []struct{ Role, Content string }
			System, Prompt string
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "fictitious invalid request", 400)
			return
		}
		system, prompt := req.System, req.Prompt
		for _, msg := range req.Messages {
			if msg.Role == "system" {
				system = msg.Content
			}
			if msg.Role == "user" {
				prompt = msg.Content
			}
		}
		stage := phase26ModelStage(system)
		call := phase26Call{Stage: stage, System: system, Prompt: prompt}
		start := time.Now()
		m.mu.Lock()
		before, reply := m.Before, m.Reply
		m.mu.Unlock()
		if before != nil {
			before(r.Context(), call)
		}
		out := m.goldReply(call)
		if reply != nil {
			out = reply(call)
		}
		call.Output = out
		call.Elapsed = time.Since(start)
		m.mu.Lock()
		m.Calls = append(m.Calls, call)
		m.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"text": out, "choices": []any{map[string]any{"message": map[string]string{"content": out}}}})
	}))
	t.Cleanup(server.Close)
	m.URL = server.URL
	f.Store.SetModels(&ai.Registry{HTTP: server.Client(), Config: ai.Configuration{Extraction: "phase26", Providers: []ai.Provider{{ID: "phase26", Name: "Fictitious acceptance model", Protocol: "openai", BaseURL: server.URL, Model: "fictitious", MaxOutput: 8192, CostMode: "free"}}}})
	return m
}
func phase26ModelStage(system string) string {
	switch {
	case strings.Contains(system, "每条记忆标类型"):
		return OrganizeStage
	case strings.Contains(system, "写交接说明"):
		return HandoverStage
	case strings.Contains(system, "只提出候选"):
		return EntityCandidatesStage
	case strings.Contains(system, "同一个人") || strings.Contains(system, "同一实体") || strings.Contains(system, "same"):
		return EntityCompareStage
	case strings.Contains(system, "duplicates"):
		return CompareStage
	default:
		return "secretary"
	}
}
func (m *phase26Model) calls(stage string) []phase26Call {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []phase26Call{}
	for _, c := range m.Calls {
		if c.Stage == stage {
			out = append(out, c)
		}
	}
	return out
}
func (m *phase26Model) goldReply(call phase26Call) string {
	switch call.Stage {
	case OrganizeStage:
		var prompt struct {
			Memories []struct {
				N    int
				Text string
			}
			Refs []memory.Ref
		}
		_ = json.Unmarshal([]byte(call.Prompt), &prompt)
		index := map[string]int{}
		for i, id := range m.f.Claims {
			index[string(id)] = i
		}
		items := []any{}
		for j, input := range prompt.Memories {
			if j >= len(prompt.Refs) {
				continue
			}
			i, ok := index[string(prompt.Refs[j].ID)]
			if !ok {
				continue
			}
			seed := m.f.Corpus.Memories[i]
			deadlines := []any{}
			for _, d := range m.f.Corpus.Deadlines {
				if d.Memory != i {
					continue
				}
				var at *string
				if d.At != nil {
					v := d.At.Format(time.RFC3339)
					at = &v
				}
				kind := d.Kind
				if d.State == "unclear" {
					kind = "unclear"
				}
				deadlines = append(deadlines, map[string]any{"kind": kind, "at": at, "recurrence": d.Recurrence, "title": fmt.Sprintf("Fictitious deadline %d", i), "timeNote": "Fictitious model clarification"})
			}
			scope := ""
			unrestricted := false
			for _, rule := range m.f.Corpus.Rules {
				if rule.Memory == i {
					scope = rule.Scope
					unrestricted = scope == ""
				}
			}
			project, area := "", ""
			topics := []string{}
			for _, g := range seed.Groups {
				group := m.f.Corpus.Groups[g]
				switch group.Kind {
				case "project":
					if project == "" {
						project = group.Name
					}
				case "area":
					if area == "" {
						area = group.Name
					}
				case "topic":
					topics = append(topics, group.Name)
				}
			}
			items = append(items, map[string]any{"n": input.N, "category": seed.Category, "durable": true, "unrestricted": unrestricted, "scope": scope, "deadlines": deadlines, "project": project, "area": area, "topics": topics})
		}
		b, _ := json.Marshal(map[string]any{"items": items, "new": []any{}})
		return string(b)
	case CompareStage:
		var prompt struct {
			Memories []struct{ N int }
			Refs     []memory.Ref
		}
		_ = json.Unmarshal([]byte(call.Prompt), &prompt)
		positions := map[memory.ID]int{}
		for i, r := range prompt.Refs {
			if i < len(prompt.Memories) {
				positions[r.ID] = prompt.Memories[i].N
			}
		}
		duplicates, superseded := []any{}, []any{}
		for _, p := range m.f.Corpus.Pairs {
			early, recent := positions[m.f.Claims[p.Early]], positions[m.f.Claims[p.Recent]]
			if early == 0 || recent == 0 {
				continue
			}
			if p.Relation == "duplicate" {
				duplicates = append(duplicates, map[string]any{"keep": recent, "members": []int{early, recent}})
			} else {
				superseded = append(superseded, map[string]int{"old": early, "new": recent})
			}
		}
		b, _ := json.Marshal(map[string]any{"duplicates": duplicates, "superseded": superseded})
		return string(b)
	case EntityCompareStage:
		return `{"same":false,"keep":null}`
	case EntityCandidatesStage:
		return `{"groups":[]}`
	case HandoverStage:
		var prompt struct {
			Memories []struct {
				N        int
				Category string
			}
			Refs      []memory.Ref
			Deadlines []struct{ MemoryID string }
		}
		_ = json.Unmarshal([]byte(call.Prompt), &prompt)
		byCategory := map[string][]int{}
		numbered := map[string]int{}
		for i, input := range prompt.Memories {
			byCategory[input.Category] = append(byCategory[input.Category], input.N)
			if i < len(prompt.Refs) {
				numbered[string(prompt.Refs[i].ID)] = input.N
			}
		}
		deadlineRefs := []int{}
		for _, d := range prompt.Deadlines {
			if n := numbered[d.MemoryID]; n != 0 {
				deadlineRefs = append(deadlineRefs, n)
			}
		}
		titles := []string{"他是谁和现在的处境", "怎么跟他配合", "现在手上的事", "时间和节奏", "资源和限制", "口味和标准", "重要的人", "他的叫法", "他看重什么"}
		refs := [][]int{byCategory["identity"], byCategory["rule"], byCategory["goal"], deadlineRefs, nil, byCategory["taste"], nil, nil, nil}
		bodies := []string{
			"The fictitious owner curates the Azure Bay ceramics archive.",
			"Provide specimen IDs for the defined ceramic types. Apply each requirement within its declared scope, including all unrestricted requirements on every turn. The latest exhibition-budget correction is 200 units, replacing the older 100-unit statement.",
			"The current fictitious goal is to finish the Azure Bay ceramics exhibition.",
			"Preserve the fictitious future deadlines, past deadlines with completion unknown, recurring Tuesday arrangements, and dates awaiting clarification, with their original wording and uncertainty.",
			"", "The fictitious owner prefers blue ceramics.", "", "", "",
		}
		sections := []any{}
		for i, title := range titles {
			body := bodies[i]
			r := refs[i]
			if len(r) == 0 {
				body = "（暂无依据）"
				r = []int{}
			}
			sections = append(sections, map[string]any{"title": title, "body": body, "refs": r})
		}
		b, _ := json.Marshal(map[string]any{"sections": sections})
		return string(b)
	default:
		return `{"reply":"Fictitious acceptance reply.","used":[],"links":[],"show":[],"remember":false,"missingKeyInfo":false,"actions":[],"ask":null,"memoryPlan":{"depth":"light","groups":[]}}`
	}
}
func phase26Exec(t *testing.T, f *phase26LoadedFixture, sql string, args ...any) {
	t.Helper()
	if _, err := f.Store.pool.Exec(f.Context, sql, args...); err != nil {
		t.Fatal(err)
	}
}
func phase26Isolate(t *testing.T, f *phase26LoadedFixture, stage string) {
	t.Helper()
	// Test precondition: switch the isolated due pipeline without deleting receipts.
	phase26Exec(t, f, `UPDATE memory_jobs SET available_at=CASE WHEN stage LIKE $1 THEN now() ELSE now()+interval '1 day' END WHERE state='queued'`, stage+":%")
}
func phase26Schedule(ctx context.Context, s *Store, stage string) (int, error) {
	switch stage {
	case OrganizeStage:
		return s.ScheduleOrganize(ctx, time.Now())
	case CompareStage, EntityCompareStage:
		return s.ScheduleCompare(ctx, time.Now())
	case EntityCandidatesStage:
		return s.ScheduleEntityCandidates(ctx, time.Now())
	case HandoverStage:
		return s.ScheduleStatus(ctx, time.Now())
	}
	return 0, fmt.Errorf("unknown test stage %s", stage)
}
func phase26Process(ctx context.Context, s *Store, j worker.Job) error {
	switch strings.SplitN(j.Stage, ":", 2)[0] {
	case OrganizeStage:
		return s.ProcessOrganize(ctx, j)
	case CompareStage:
		return s.ProcessCompare(ctx, j)
	case EntityCompareStage:
		return s.ProcessEntityCompare(ctx, j)
	case EntityCandidatesStage:
		return s.ProcessEntityCandidates(ctx, j)
	case HandoverStage:
		return s.ProcessHandover(ctx, j)
	}
	return fmt.Errorf("unknown test stage %s", j.Stage)
}
func phase26ClaimStage(t *testing.T, f *phase26LoadedFixture, stage string) worker.Job {
	t.Helper()
	for i := 0; i < 200; i++ {
		j, err := f.Store.Claim(f.Context, 5*time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if j == nil {
			t.Fatalf("no due job for %s", stage)
		}
		if strings.HasPrefix(j.Stage, stage+":") {
			return *j
		}
		if err := f.Store.Defer(f.Context, *j, "phase26_nonfocus_pipeline", time.Now().Add(24*time.Hour), true); err != nil {
			t.Fatal(err)
		}
	}
	t.Fatal("too many nonfocus jobs")
	return worker.Job{}
}
func phase26FinishError(t *testing.T, f *phase26LoadedFixture, j worker.Job, err error) *worker.JobError {
	t.Helper()
	var e *worker.JobError
	if !errors.As(err, &e) || e.Until.IsZero() {
		t.Fatalf("expected typed deferral for %s, got %v", j.Stage, err)
	}
	if err := f.Store.Defer(f.Context, j, e.Code, e.Until, e.NoAttempt); err != nil {
		t.Fatal(err)
	}
	return e
}
