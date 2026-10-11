package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/prompts"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func (s *Store) recordReturnedUsage(ctx context.Context, text string, usage modelUsage) error {
	if strings.TrimSpace(text) == "" && usage.InputTokens+usage.OutputTokens == 0 && usage.DurationMS == nil {
		return nil
	}
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	return pgx.BeginFunc(persistCtx, s.pool, func(tx pgx.Tx) error {
		return recordUsageTx(persistCtx, tx, usage)
	})
}

// tail keeps the last n bytes of s without splitting a character.
func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	i := len(s) - n
	for i < len(s) && !utf8.RuneStart(s[i]) {
		i++
	}
	return s[i:]
}

// deskContextTx keeps the legacy answer and secretary on the same visibility
// and derived-artifact rules. Stable ordering is used by the secretary cache.
func (s *Store) deskContextTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, agent workspace.Agent, stable bool) ([]workspace.Memory, []workspace.Item, workspace.Settings, []memory.Ref, error) {
	memories, err := s.memoriesTx(ctx, tx, memory.Scope{OwnerID: scope.OwnerID, PrincipalID: agent.ID}, true)
	if err != nil {
		return nil, nil, workspace.Settings{}, nil, err
	}
	tasks, settings, refs, err := s.deskItemsContextTx(ctx, tx, scope, agent, stable)
	return memories, tasks, settings, refs, err
}

// Secretary recall supplies the memory identities later; the legacy answer
// still needs its complete memory context. Share only the item/settings reads.
func (s *Store) deskItemsContextTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, agent workspace.Agent, stable bool) ([]workspace.Item, workspace.Settings, []memory.Ref, error) {
	order := "due_at NULLS LAST, updated_at DESC"
	if stable {
		order = "created_at,id"
	}
	tasks, err := queryDocuments[workspace.Item](ctx, tx, "SELECT document FROM work_items WHERE owner_id=$1 AND kind='task' AND status IN ('todo','doing','waiting') ORDER BY "+order+" LIMIT 40", string(scope.OwnerID))
	if err != nil {
		return nil, workspace.Settings{}, nil, err
	}
	dependencies := []memory.Ref{}
	for i := range tasks {
		var refs []memory.Ref
		tasks[i], refs, err = sanitizeItemTx(ctx, tx, scope, agent.ID, tasks[i])
		if err != nil {
			return nil, workspace.Settings{}, nil, err
		}
		dependencies = append(dependencies, refs...)
	}
	settings, err := queryDocument[workspace.Settings](ctx, tx, "SELECT settings FROM workspace_owners WHERE owner_id=$1", string(scope.OwnerID))
	return tasks, settings, dependencies, err
}
func deskLocation(settings workspace.Settings) *time.Location {
	loc, err := time.LoadLocation(settings.Timezone)
	if err != nil {
		return time.UTC
	}
	return loc
}
func (s *Store) deskNow(loc *time.Location) string {
	now := s.businessNow().In(loc)
	return fmt.Sprintf("现在：%s 星期%s（%s）\n", now.Format("2006-01-02 15:04"), []string{"日", "一", "二", "三", "四", "五", "六"}[now.Weekday()], loc)
}

const recallTimeRelaxed = "按这个时间没有找到，下面是其他时间说的，回答时说明实际日期"

var recallDateInstructions = prompts.Must("recall-date").Text()

// Append only supplied metadata; old memory lines stay byte-for-byte intact.
func memoryPromptSuffix(m workspace.Memory, loc *time.Location) string {
	if loc == nil {
		loc = time.UTC
	}
	var suffix strings.Builder
	if m.Completed {
		suffix.WriteString(" / 已标完成（依据记忆的期限事项已办完）")
	}
	if at, err := time.Parse(time.RFC3339Nano, m.ExpressedAt); err == nil {
		fmt.Fprintf(&suffix, " / 说于 %s", at.In(loc).Format("2006-01-02"))
	}
	if from, err := time.Parse(time.RFC3339Nano, m.EventFrom); err == nil {
		from = from.In(loc)
		date := ""
		switch m.EventPrecision {
		case "day":
			date = from.Format("2006-01-02")
		case "month":
			date = from.Format("2006-01")
		case "year":
			date = from.Format("2006")
		case "range":
			if to, err := time.Parse(time.RFC3339Nano, m.EventTo); err == nil && to.After(from) {
				date = from.Format("2006-01-02") + " 至 " + to.Add(-time.Nanosecond).In(loc).Format("2006-01-02")
			}
		}
		if date != "" {
			fmt.Fprintf(&suffix, " / 事件 %s", date)
		}
	}
	for _, mention := range m.Mentions {
		if oneOf(mention.Role, "place", "person") && strings.TrimSpace(mention.Name) != "" {
			fmt.Fprintf(&suffix, " / %s", mention.Name)
		}
	}
	return suffix.String()
}
