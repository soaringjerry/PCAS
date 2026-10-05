package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// Model calls are outside each reader's database transaction. A buffered channel
// lets late readers finish accounting without blocking the answer at its cutoff.
func (s *Store) heavyUse(ctx, persist context.Context, scope memory.Scope, agent workspace.Agent, thing *string, text string, u useContext, turn, run string) ([]workspace.Memory, []memory.Ref, []string) {
	readCtx, cancel := context.WithTimeout(ctx, heavyReaderBudget)
	defer cancel()
	var catalog strings.Builder
	fmt.Fprintf(&catalog, "这件事：%s\n交接说明：%s\n分组目录（只选这些 key，上限12）：\n", text, u.Handover.Body)
	for _, g := range u.Index {
		fmt.Fprintf(&catalog, "%s：%s（%s，%d条；%s的当前情况）\n", g.Key, g.Name, g.Kind, g.Count, g.Name)
	}
	catalog.WriteString(`只输出 JSON：{"groups":["目录里的key"]}。` + "\n")
	selectionID := memory.NewID()
	choice, err := s.useModelCall(readCtx, persist, scope, agent.ID, useReaderInstructions, catalog.String(), useGroupsSchema, modelUsage{ID: selectionID, Purpose: "reader", Tier: "heavy", TurnID: turn, RunID: run, MemoryRefs: u.Dependencies, Plan: asJSON(usePlan{Groups: u.Groups})})
	keys := append([]string{}, u.RequiredGroups...)
	if len(keys) > 12 {
		keys = keys[:12]
	}
	if err == nil {
		var p usePlan
		if json.Unmarshal([]byte(choice.Text), &p) == nil {
			for _, key := range p.Groups {
				for _, g := range u.Index {
					if key == g.Key && !oneOf(key, keys...) && len(keys) < 12 {
						keys = append(keys, key)
					}
				}
			}
		}
	}
	if err != nil || len(keys) == 0 {
		keys = append(keys, u.Groups...)
		if len(keys) > 12 {
			keys = keys[:12]
		}
	}
	if err == nil {
		if _, e := s.pool.Exec(readCtx, "UPDATE model_usage SET plan=$3 WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), string(selectionID), safeUsePlan(asJSON(usePlan{Groups: keys}))); e != nil {
			slog.WarnContext(persist, "memory selector accounting update failed", "stage", "reader", "error_type", secretaryErrorType("reader", e))
		}
	}
	type readerResult struct {
		memories []workspace.Memory
		refs     []memory.Ref
	}
	results := make(chan readerResult, len(keys))
	for _, key := range keys {
		go func(key string) {
			ms := []workspace.Memory{}
			err := pgx.BeginFunc(readCtx, s.pool, func(tx pgx.Tx) error {
				opts := memoryReadOptions{useCurrent: true}
				if strings.HasPrefix(key, "entity:") {
					opts.useEntity = strings.TrimPrefix(key, "entity:")
				} else {
					opts.query.Category = strings.TrimPrefix(key, "self:")
				}
				var err error
				ms, err = s.readMemoriesTx(readCtx, tx, memory.Scope{OwnerID: scope.OwnerID, PrincipalID: agent.ID}, true, opts)
				if err != nil {
					return err
				}
				allowed := []workspace.Memory{}
				for _, m := range ms {
					if useMemoryAllowed(m, agent) {
						ref := memory.Ref{ID: memory.ID(m.ID), Version: m.Version, Kind: memory.ClaimKind}
						if thing == nil || verifyRunTx(readCtx, tx, scope, workspace.Run{ThingID: *thing, AgentID: agent.ID, ContextVersions: []memory.Ref{ref}}) == nil {
							allowed = append(allowed, m)
						}
					}
				}
				ms = allowed
				return nil
			})
			refs := memoryRefs(ms)
			selected := []workspace.Memory{}
			if err == nil && len(ms) > 0 {
				raw, e := s.useModelCall(readCtx, persist, scope, agent.ID, useReaderInstructions, "这件事："+text+"\n分组："+key+"\n"+writeReaderMemories(ms, u.Location)+`只输出 JSON：{"used":["上面的记忆ID"]}。`, useReaderSchema, modelUsage{Purpose: "reader", Tier: "heavy", TurnID: turn, RunID: run, MemoryRefs: refs, Plan: asJSON(usePlan{Groups: []string{key}})})
				err = e
				if err == nil {
					var out struct {
						Used []string `json:"used"`
					}
					if e = json.Unmarshal([]byte(raw.Text), &out); e == nil {
						for _, id := range out.Used {
							for _, m := range ms {
								if m.ID == id {
									selected = append(selected, m)
									break
								}
							}
						}
					}
				}
			}
			if err != nil {
				slog.WarnContext(persist, "memory reader skipped", "stage", "reader", "error_type", secretaryErrorType("reader", err))
				refs = nil
			}
			results <- readerResult{selected, refs}
		}(key)
	}
	picked := []workspace.Memory{}
	refs := []memory.Ref{}
	seen := map[string]bool{}
	for range keys {
		select {
		case result := <-results:
			refs = append(refs, result.refs...)
			for _, m := range result.memories {
				if !seen[m.ID] {
					picked = append(picked, m)
					seen[m.ID] = true
				}
			}
		case <-readCtx.Done():
			return picked, uniqueRefs(refs), keys
		}
	}
	return picked, uniqueRefs(refs), keys
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
		if err != nil || !u.Ready {
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
		return s.finishUseContextTx(ctx, tx, scope, agent, &run.ThingID, run.Prompt, ms, &u)
	})
	return u, agent, err
}
