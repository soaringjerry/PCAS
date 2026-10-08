package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func currentStudioProjectTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, thing string) (string, error) {
	if thing == "" {
		return "", nil
	}
	item, err := getItem(ctx, tx, scope, thing)
	if err != nil {
		return "", err
	}
	if item.Kind == "project" {
		return item.ID, nil
	}
	return item.ProjectID, nil
}

// Supply current evidence behind the handover, including completed items that
// are absent from the ordinary unfinished task list. Aliases remain server-owned;
// the model decides which evidence the user's correction concerns.
func (s *Store) secretaryHandoverEvidenceTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, c *secretaryContext, b *strings.Builder, sent map[string]workspace.Memory) error {
	h := c.Use.ProjectHandover
	if h == nil || h.WrittenAt == nil {
		return nil
	}
	memoryIDs := []string{}
	items := map[string]bool{}
	labels := map[string]string{}
	lines := append(append(append([]workspace.ProjectSentence{}, h.Conclusion...), h.Blockers...), h.NextSteps...)
	for _, line := range lines {
		for _, e := range line.Evidence {
			switch e.Kind {
			case "memory":
				memoryIDs = append(memoryIDs, e.ID)
			case "item":
				if items[e.ID] {
					continue
				}
				items[e.ID] = true
				item, err := getItem(ctx, tx, scope, e.ID)
				if errors.Is(err, memory.ErrNotFound) {
					continue
				}
				if err != nil {
					return err
				}
				alias := ""
				for k, v := range c.Aliases {
					if v.ID == item.ID {
						alias = k
						break
					}
				}
				if alias == "" {
					alias = fmt.Sprintf("H%d", len(items))
					c.TargetItems[item.ID] = item
					clean, refs, err := sanitizeItemTx(ctx, tx, scope, c.Agent.ID, item)
					if err != nil {
						return err
					}
					c.Aliases[alias] = clean
					c.Dependencies = append(c.Dependencies, refs...)
				}
				labels[e.ID] = alias
				fmt.Fprintf(b, "\n交接说明依据事项 %s：%s（当前状态 %s；截止 %s）；说明：%s\n", alias, item.Title, item.Status, item.Due, itemNotes(item))
			}
		}
	}
	var thing *string
	if item, ok := c.Aliases["THIS"]; ok {
		thing = &item.ID
	}
	if len(memoryIDs) > 0 {
		ms, err := s.useMemoriesTx(ctx, tx, scope, c.Agent, thing, memoryIDs)
		if err != nil {
			return err
		}
		for _, m := range ms {
			alias := ""
			for k, v := range sent {
				if v.ID == m.ID {
					alias = k
					break
				}
			}
			if alias == "" {
				alias = fmt.Sprintf("M%d", len(sent)+1)
				sent[alias] = m
				fmt.Fprintf(b, "\n交接说明依据当前记忆 [%s / trust=%s] %s\n", alias, m.Trust, m.Text+memoryPromptSuffix(m, deskLocation(c.Settings)))
			}
			labels[m.ID] = alias
			c.Dependencies = append(c.Dependencies, memory.Ref{ID: memory.ID(m.ID), Version: m.Version, Kind: memory.ClaimKind})
		}
	}
	b.WriteString("\n交接说明句子的可用依据别名（当前原始依据优先于过期交接文字；纠错修改对应事项或记住新事实，不修改交接说明）：\n")
	for _, line := range lines {
		refs := []string{}
		for _, e := range line.Evidence {
			if label := labels[e.ID]; label != "" {
				refs = append(refs, label)
			}
		}
		fmt.Fprintf(b, "%s → %v\n", line.Text, refs)
	}
	return nil
}
