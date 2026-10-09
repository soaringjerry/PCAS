package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/prompts"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// Model calls are outside each reader's database transaction. A buffered channel
// lets late readers finish accounting without blocking the answer at its cutoff.
func (s *Store) heavyUse(ctx, persist context.Context, scope memory.Scope, agent workspace.Agent, thing *string, text string, u useContext, turn, run, phase string) ([]workspace.Memory, []memory.Ref, []string) {
	readCtx, cancel := context.WithTimeout(ctx, heavyReaderBudget)
	defer cancel()
	var catalog strings.Builder
	fmt.Fprintf(&catalog, "这件事：%s\n交接说明：%s\n分组目录（只选这些 key，上限12）：\n", text, u.Handover.Body)
	for _, g := range u.Index {
		fmt.Fprintf(&catalog, "%s：%s（%s，%d条；%s的当前情况）\n", g.Key, g.Name, g.Kind, g.Count, g.Name)
	}
	writeProjectHandover(&catalog, u.ProjectHandover)
	catalog.WriteString(`只输出 JSON：{"groups":["目录里的key"]}。` + "\n")
	selectionID := memory.NewID()
	keys := []string{}
	requested := append([]string{}, u.RequiredGroups...)
	if u.ProjectGroup != "" {
		requested = append([]string{u.ProjectGroup}, requested...)
	}
	var err error
	if !u.Selected {
		choice, e := s.useModelCall(readCtx, persist, scope, agent.ID, phase+":catalog", prompts.Must("memory-reader"), catalog.String(), prompts.MustSchema("use-groups"), modelUsage{ID: selectionID, Purpose: "reader", Tier: "heavy", TurnID: turn, RunID: run, MemoryRefs: u.Dependencies, Plan: asJSON(usePlan{Groups: u.Groups})})
		err = e
		if err == nil {
			var p usePlan
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
			if !oneOf(note, u.Coverage.Notices...) {
				u.Coverage.Notices = append(u.Coverage.Notices, note)
			}
		}
	}
	omitted := []string{}
	for _, key := range requested {
		for _, g := range u.Index {
			if g.Key != key || oneOf(key, keys...) || oneOf(g.Name, omitted...) {
				continue
			}
			if len(keys) < 12 {
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
		s.recordUseOverflow(persist, scope, "reader_group_limit", len(omitted))
	}

	type readerResult struct {
		key      string
		memories []workspace.Memory
		refs     []memory.Ref
		complete bool
	}
	results := make(chan readerResult, len(keys))
	for _, key := range keys {
		go func(key string) {
			selected := []workspace.Memory{}
			refs := []memory.Ref{}
			ids := []string{}
			err := pgx.BeginTxFunc(readCtx, s.pool, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
				var e error
				ids, e = queryDocuments[string](readCtx, tx, "SELECT to_jsonb(claim_id::text) FROM status_current_members WHERE owner_id=$1 AND key=$2 ORDER BY claim_id", string(scope.OwnerID), key)
				return e
			})
			// 100 memories per model input bounds both hydration and prompt
			// size. Every page is read, subject only to the shared wall clock.
			for offset := 0; err == nil && offset < len(ids); offset += 100 {
				var ms []workspace.Memory
				end := min(offset+100, len(ids))
				err = pgx.BeginTxFunc(readCtx, s.pool, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
					var e error
					ms, e = s.useMemoriesTx(readCtx, tx, scope, agent, thing, ids[offset:end])
					return e
				})
				if err != nil {
					break
				}
				if len(ms) == 0 {
					continue
				}
				batchRefs := memoryRefs(ms)
				raw, e := s.useModelCall(readCtx, persist, scope, agent.ID, fmt.Sprintf("%s:group:%s:%d", phase, key, offset), prompts.Must("memory-reader"), "这件事："+text+"\n分组："+key+"\n"+writeReaderMemories(ms, u.Location)+`只输出 JSON：{"used":["上面的记忆ID"]}。`, prompts.MustSchema("use-reader"), modelUsage{Purpose: "reader", Tier: "heavy", TurnID: turn, RunID: run, MemoryRefs: batchRefs, Plan: asJSON(usePlan{Groups: []string{key}})})
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
				slog.WarnContext(persist, "memory reader skipped", "stage", "reader", "error_type", secretaryErrorType("reader", err))
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
				s.recordUseOverflow(persist, scope, "reader_timeout_or_failure", 1)
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

func (s *Store) deputyUseContext(ctx context.Context, scope memory.Scope, run *workspace.Run) (useContext, workspace.Agent, error) {
	var u useContext
	var agent workspace.Agent
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		agent, err = queryDocument[workspace.Agent](ctx, tx, "SELECT document FROM workspace_agents WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), run.AgentID)
		if err != nil {
			return err
		}
		u, err = s.startUseContextTx(ctx, tx, scope)
		if err != nil {
			return err
		}
		ms, err := s.readMemoriesTx(ctx, tx, memory.Scope{OwnerID: scope.OwnerID, PrincipalID: agent.ID}, true, memoryReadOptions{useCurrent: true, ids: run.ContextMemoryIDs})
		if err != nil {
			return err
		}
		settings, err := queryDocument[workspace.Settings](ctx, tx, "SELECT settings FROM workspace_owners WHERE owner_id=$1", string(scope.OwnerID))
		if err != nil {
			return err
		}
		u.Location = deskLocation(settings)
		u.Tier = run.MemoryTier
		return s.finishUseContextTx(ctx, tx, scope, agent, &run.ThingID, run.Prompt, ms, &u)
	})
	return u, agent, err
}

func (s *Store) recordUseOverflow(ctx context.Context, scope memory.Scope, reason string, count int) {
	persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if _, err := s.pool.Exec(persist, "INSERT INTO background_stage_events(owner_id,stage,outcome,reason,count) VALUES($1,'reader','overflow',$2,$3)", string(scope.OwnerID), reason, count); err != nil {
		slog.WarnContext(persist, "reader overflow accounting failed", "reason", reason, "count", count)
	}
}
