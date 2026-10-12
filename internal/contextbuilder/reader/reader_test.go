package reader

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/contextbuilder"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/modelcall"
	"github.com/soaringjerry/PCAS/internal/prompts"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// fake is the storage and the model of the readers of one task.
type fake struct {
	mu       sync.Mutex
	members  map[string][]string
	choice   string
	choiceEr error
	failing  string
	stages   []string
	overflow map[string]int
}

func (f *fake) GroupMembers(_ context.Context, _ memory.Scope, key string) ([]string, error) {
	return f.members[key], nil
}
func (f *fake) Memories(_ context.Context, _ memory.Scope, _ workspace.Agent, _ *string, ids []string) ([]workspace.Memory, error) {
	out := []workspace.Memory{}
	for _, id := range ids {
		out = append(out, workspace.Memory{ID: id, Version: 1, Text: "虚构记忆 " + id})
	}
	return out, nil
}
func (f *fake) Overflow(_ context.Context, _ memory.Scope, reason string, count int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.overflow[reason] += count
}
func (f *fake) Call(_, _ context.Context, _ memory.Scope, _, stage string, _ prompts.Definition, prompt string, _ prompts.Schema, usage modelcall.Usage) (ai.Result, error) {
	f.mu.Lock()
	f.stages = append(f.stages, stage)
	f.mu.Unlock()
	if strings.HasSuffix(stage, ":catalog") {
		return ai.Result{Text: f.choice}, f.choiceEr
	}
	if f.failing != "" && strings.Contains(stage, ":group:"+f.failing+":") {
		return ai.Result{}, errors.New("fictitious failure")
	}
	// Each reader uses the first memory of its page.
	return ai.Result{Text: fmt.Sprintf(`{"used":[%q]}`, usage.MemoryRefs[0].ID)}, nil
}

func service(f *fake) Service {
	f.overflow = map[string]int{}
	return Service{Store: f, Calls: f, ErrorType: func(stage string, _ error) string { return stage + "_error" }}
}

func index(keys ...string) []workspace.StatusCardRef {
	out := []workspace.StatusCardRef{}
	for _, key := range keys {
		out = append(out, workspace.StatusCardRef{Key: key, Name: "组" + key, Kind: "person", Count: 1})
	}
	return out
}

func task() Task { return Task{Phase: "reader:prepare", Text: "虚构的事"} }

func TestReadersUseTheGroupsThatTheModelChoseAndEveryPageOfEach(t *testing.T) {
	ids := []string{}
	for i := 0; i < PageSize+1; i++ {
		ids = append(ids, fmt.Sprintf("m%03d", i))
	}
	f := &fake{members: map[string][]string{"a": ids, "b": {"b1"}, "c": {"c1"}}, choice: `{"groups":["b","a","unknown"]}`}
	u := contextbuilder.UseContext{Index: index("a", "b", "c"), Coverage: &contextbuilder.Coverage{}}
	picked, refs, keys := service(f).Read(context.Background(), context.Background(), task(), u)
	if strings.Join(keys, ",") != "b,a" {
		t.Fatal("groups", keys)
	}
	got := []string{}
	for _, m := range picked {
		got = append(got, m.ID)
	}
	// Group a has two pages, and each page gave its first memory.
	if strings.Join(got, ",") != "b1,m000,m100" || len(refs) != PageSize+2 || len(f.stages) != 4 || len(u.Coverage.Skipped) != 0 {
		t.Fatal(got, len(refs), f.stages, u.Coverage.Skipped)
	}
}

func TestGroupChoiceThatFailsUsesTheGroupsOfTheTurnAndSaysSo(t *testing.T) {
	for note, f := range map[string]*fake{
		"记忆分组选择未完成，本次沿用已有分组。":    {choiceEr: errors.New("fictitious failure")},
		"记忆分组选择没有返回分组，本次沿用已有分组。": {choice: `{"groups":[]}`},
	} {
		f.members = map[string][]string{"a": {"a1"}}
		u := contextbuilder.UseContext{Index: index("a", "b"), Groups: []string{"a"}, Coverage: &contextbuilder.Coverage{}}
		_, _, keys := service(f).Read(context.Background(), context.Background(), task(), u)
		if strings.Join(keys, ",") != "a" || len(u.Coverage.Notices) != 1 || u.Coverage.Notices[0] != note {
			t.Fatal(keys, u.Coverage.Notices)
		}
	}
}

func TestSelectedGroupsAreReadWithoutAGroupChoice(t *testing.T) {
	f := &fake{members: map[string][]string{"p": {"p1"}, "r": {"r1"}}}
	u := contextbuilder.UseContext{Index: index("p", "r", "x"), Selected: true, ProjectGroup: "p", RequiredGroups: []string{"r", "p"}, Coverage: &contextbuilder.Coverage{}}
	picked, _, keys := service(f).Read(context.Background(), context.Background(), task(), u)
	for _, stage := range f.stages {
		if strings.HasSuffix(stage, ":catalog") {
			t.Fatal("the model chose groups again", f.stages)
		}
	}
	if strings.Join(keys, ",") != "p,r" || len(picked) != 2 {
		t.Fatal(keys, picked)
	}
}

func TestGroupsAboveTheLimitAndFailedReadersAreNamedAndCounted(t *testing.T) {
	keys, members := []string{}, map[string][]string{}
	for i := 0; i < GroupLimit+2; i++ {
		key := fmt.Sprintf("g%02d", i)
		keys = append(keys, key)
		members[key] = []string{key + "-1"}
	}
	f := &fake{members: members, failing: "g03"}
	u := contextbuilder.UseContext{Index: index(keys...), Selected: true, RequiredGroups: keys, Coverage: &contextbuilder.Coverage{}}
	picked, _, read := service(f).Read(context.Background(), context.Background(), task(), u)
	if len(read) != GroupLimit || len(picked) != GroupLimit-1 {
		t.Fatal(len(read), len(picked))
	}
	if f.overflow["reader_group_limit"] != 2 || f.overflow["reader_timeout_or_failure"] != 1 {
		t.Fatal(f.overflow)
	}
	if strings.Join(u.Coverage.Skipped, ",") != "组g12,组g13,组g03" {
		t.Fatal(u.Coverage.Skipped)
	}
}

func TestReadersOfAnEndedRequestGiveNothingAndNameEachGroup(t *testing.T) {
	f := &fake{members: map[string][]string{"a": {"a1"}, "b": {"b1"}}}
	u := contextbuilder.UseContext{Index: index("a", "b"), Selected: true, RequiredGroups: []string{"a", "b"}, Coverage: &contextbuilder.Coverage{}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	blocked := service(f)
	blocked.Store = blockedStore{f}
	picked, _, keys := blocked.Read(ctx, context.Background(), task(), u)
	if len(picked) != 0 || len(keys) != 2 || len(u.Coverage.Skipped) != 2 || f.overflow["reader_timeout_or_failure"] != 2 {
		t.Fatal(picked, keys, u.Coverage.Skipped, f.overflow)
	}
}

// blockedStore is a store whose reads end with the request.
type blockedStore struct{ *fake }

func (b blockedStore) GroupMembers(ctx context.Context, _ memory.Scope, _ string) ([]string, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}
