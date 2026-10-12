// Package reader reads groups of memory for one task with the model: it lets
// the model choose the groups, and then lets one reader for each group choose
// the memories that the task needs.
package reader

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/contextbuilder"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/modelcall"
	"github.com/soaringjerry/PCAS/internal/prompts"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// Budget is the time that the readers of one task have together.
const Budget = 90 * time.Second

// GroupLimit is the number of groups that one task reads at most.
const GroupLimit = 12

// PageSize is the number of memories in one reader input. It bounds the read
// and the size of the input. Every page of a group is read, subject only to
// the shared time.
const PageSize = 100

// Store gives the readers the memories of a group.
type Store interface {
	// GroupMembers returns the identifiers of the memories of a group.
	GroupMembers(ctx context.Context, scope memory.Scope, key string) ([]string, error)
	// Memories returns those of the memories that the agent may use for the
	// item.
	Memories(ctx context.Context, scope memory.Scope, agent workspace.Agent, thing *string, ids []string) ([]workspace.Memory, error)
	// Overflow records that count groups were not read for reason.
	Overflow(ctx context.Context, scope memory.Scope, reason string, count int)
}

// Calls makes one stage call of the execution that ctx carries.
type Calls interface {
	Call(ctx, persist context.Context, scope memory.Scope, agent, stage string, instructions prompts.Definition, prompt string, schema prompts.Schema, usage modelcall.Usage) (ai.Result, error)
}

// Service reads memory for the secretary and the deputy.
type Service struct {
	Store Store
	Calls Calls
	// ErrorType gives the fixed category of a failure for the log.
	ErrorType func(stage string, err error) string
}

// groups is the plan of a reader call and the answer of the group choice.
type groups struct {
	Groups []string `json:"groups"`
}

// Task is one reading task.
type Task struct {
	Scope memory.Scope
	Agent workspace.Agent
	// Thing is the open item, or nil.
	Thing *string
	Text  string
	// Turn or Run names the execution in the usage records.
	Turn, Run string
	// Phase is the prefix of the stage of each call.
	Phase string
}

// Read returns the memories that the readers chose, the records that they
// were shown, and the groups that were read. Model calls are outside each
// reader's storage read. A reader that does not finish in time or fails is
// left out and named in the coverage; the finished readers still count.
// persist is the context of the records that outlive ctx.
func (s Service) Read(ctx, persist context.Context, task Task, u contextbuilder.UseContext) ([]workspace.Memory, []memory.Ref, []string) {
	scope, agent, text := task.Scope, task.Agent, task.Text
	readCtx, cancel := context.WithTimeout(ctx, Budget)
	defer cancel()
	var catalog strings.Builder
	fmt.Fprintf(&catalog, "这件事：%s\n交接说明：%s\n分组目录（只选这些 key，上限12）：\n", text, u.Handover.Body)
	for _, g := range u.Index {
		fmt.Fprintf(&catalog, "%s：%s（%s，%d条；%s的当前情况）\n", g.Key, g.Name, g.Kind, g.Count, g.Name)
	}
	contextbuilder.WriteProjectHandover(&catalog, u.ProjectHandover)
	catalog.WriteString(`只输出 JSON：{"groups":["目录里的key"]}。` + "\n")
	selectionID := memory.NewID()
	keys := []string{}
	requested := append([]string{}, u.RequiredGroups...)
	if u.ProjectGroup != "" {
		requested = append([]string{u.ProjectGroup}, requested...)
	}
	var err error
	if !u.Selected {
		choice, e := s.Calls.Call(readCtx, persist, scope, agent.ID, task.Phase+":catalog", prompts.Must("memory-reader"), catalog.String(), prompts.MustSchema("use-groups"), modelcall.Usage{ID: selectionID, Purpose: "reader", Tier: "heavy", TurnID: task.Turn, RunID: task.Run, MemoryRefs: u.Dependencies, Plan: plan(u.Groups)})
		err = e
		if err == nil {
			var p groups
			err = json.Unmarshal([]byte(choice.Text), &p)
			requested = append(requested, p.Groups...)
		}
	}
	if err != nil || len(requested) == 0 {
		requested = append(requested, u.Groups...)
		if !u.Selected && u.Coverage != nil {
			note := "记忆分组选择未完成，本次沿用已有分组。"
			if err == nil {
				note = "记忆分组选择没有返回分组，本次沿用已有分组。"
			}
			if !slices.Contains(u.Coverage.Notices, note) {
				u.Coverage.Notices = append(u.Coverage.Notices, note)
			}
		}
	}
	omitted := []string{}
	for _, key := range requested {
		for _, g := range u.Index {
			if g.Key != key || slices.Contains(keys, key) || slices.Contains(omitted, g.Name) {
				continue
			}
			if len(keys) < GroupLimit {
				keys = append(keys, key)
			} else {
				omitted = append(omitted, g.Name)
			}
		}
	}
	if u.Coverage != nil {
		u.Coverage.Skipped = append(u.Coverage.Skipped, omitted...)
	}
	if len(omitted) > 0 {
		s.Store.Overflow(persist, scope, "reader_group_limit", len(omitted))
	}

	type readerResult struct {
		key      string
		memories []workspace.Memory
		refs     []memory.Ref
		complete bool
	}
	// The buffered channel lets a late reader finish its accounting without
	// blocking the answer at its cutoff.
	results := make(chan readerResult, len(keys))
	for _, key := range keys {
		go func(key string) {
			selected := []workspace.Memory{}
			refs := []memory.Ref{}
			ids, err := s.Store.GroupMembers(readCtx, scope, key)
			for offset := 0; err == nil && offset < len(ids); offset += PageSize {
				var ms []workspace.Memory
				end := min(offset+PageSize, len(ids))
				ms, err = s.Store.Memories(readCtx, scope, agent, task.Thing, ids[offset:end])
				if err != nil {
					break
				}
				if len(ms) == 0 {
					continue
				}
				batchRefs := Refs(ms)
				raw, e := s.Calls.Call(readCtx, persist, scope, agent.ID, fmt.Sprintf("%s:group:%s:%d", task.Phase, key, offset), prompts.Must("memory-reader"), "这件事："+text+"\n分组："+key+"\n"+WriteMemories(ms, u.Location)+`只输出 JSON：{"used":["上面的记忆ID"]}。`, prompts.MustSchema("use-reader"), modelcall.Usage{Purpose: "reader", Tier: "heavy", TurnID: task.Turn, RunID: task.Run, MemoryRefs: batchRefs, Plan: plan([]string{key})})
				err = e
				if err != nil {
					break
				}
				var out struct {
					Used []string `json:"used"`
				}
				err = json.Unmarshal([]byte(raw.Text), &out)
				if err != nil {
					break
				}
				refs = append(refs, batchRefs...)
				for _, id := range out.Used {
					for _, m := range ms {
						if m.ID == id {
							selected = append(selected, m)
							break
						}
					}
				}
			}
			if err != nil {
				slog.WarnContext(persist, "memory reader skipped", "stage", "reader", "error_type", s.ErrorType("reader", err))
			}
			results <- readerResult{key: key, memories: selected, refs: refs, complete: err == nil}

		}(key)
	}
	completed := map[string]readerResult{}
	finish := func() ([]workspace.Memory, []memory.Ref, []string) {
		picked := []workspace.Memory{}
		refs := []memory.Ref{}
		seen := map[string]bool{}
		for _, key := range keys {
			result := completed[key]
			if !result.complete && u.Coverage != nil {
				for _, g := range u.Index {
					if g.Key == key {
						u.Coverage.Skipped = append(u.Coverage.Skipped, g.Name)
						break
					}
				}
				s.Store.Overflow(persist, scope, "reader_timeout_or_failure", 1)
			}
			refs = append(refs, result.refs...)
			for _, m := range result.memories {
				if !seen[m.ID] {
					picked = append(picked, m)
					seen[m.ID] = true
				}
			}
		}
		return picked, uniqueRefs(refs), keys
	}
	for range keys {
		select {
		case result := <-results:
			completed[result.key] = result
		case <-readCtx.Done():
			// The deadline must not discard completed results still buffered.
			for {
				select {
				case result := <-results:
					completed[result.key] = result
				default:
					return finish()
				}
			}
		}
	}
	return finish()
}

func plan(keys []string) json.RawMessage {
	raw, _ := json.Marshal(groups{Groups: keys})
	return raw
}

// Refs are the records of the memories, each once.
func Refs(ms []workspace.Memory) []memory.Ref {
	refs := []memory.Ref{}
	for _, m := range ms {
		refs = append(refs, memory.Ref{ID: memory.ID(m.ID), Version: m.Version, Kind: memory.ClaimKind})
	}
	return uniqueRefs(refs)
}

// WriteMemories writes one line for each memory of a reader input.
func WriteMemories(ms []workspace.Memory, loc *time.Location) string {
	if loc == nil {
		loc = time.UTC
	}
	var b strings.Builder
	for _, m := range ms {
		fmt.Fprintf(&b, "[%s@%d / trust=%s] %s\n", m.ID, m.Version, m.Trust, m.Text+contextbuilder.MemorySuffix(m, loc))
	}
	return b.String()
}

func uniqueRefs(refs []memory.Ref) []memory.Ref {
	seen := make(map[memory.Ref]bool, len(refs))
	out := make([]memory.Ref, 0, len(refs))
	for _, ref := range refs {
		if !seen[ref] {
			seen[ref] = true
			out = append(out, ref)
		}
	}
	return out
}
