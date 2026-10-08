package postgres

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

const scheduleRangeDays = 62
const inProgressLimit = 12

var _ workspace.ScheduleReader = (*Store)(nil)

func (s *Store) ListSchedule(ctx context.Context, scope memory.Scope, q workspace.ScheduleQuery) (workspace.Schedule, error) {
	out := workspace.Schedule{From: q.From, To: q.To, Days: []workspace.ScheduleDay{}, Unclear: []workspace.ScheduleEntry{}, Overdue: []workspace.ScheduleEntry{}}
	if err := requireOwner(scope); err != nil {
		return out, err
	}
	err := pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{AccessMode: pgx.ReadOnly, IsoLevel: pgx.RepeatableRead}, func(tx pgx.Tx) error {
		settings, err := queryDocument[workspace.Settings](ctx, tx, "SELECT settings FROM workspace_owners WHERE owner_id=$1", string(scope.OwnerID))
		if err != nil {
			return err
		}
		out.Timezone = settings.Timezone
		loc, err := time.LoadLocation(settings.Timezone)
		if err != nil {
			return err
		}
		from, err := time.Parse("2006-01-02", q.From)
		if err != nil {
			return memory.ErrInvalid
		}
		to, err := time.Parse("2006-01-02", q.To)
		if err != nil || to.Before(from) || !to.Before(from.AddDate(0, 0, scheduleRangeDays)) {
			return memory.ErrInvalid
		}
		rangeStart := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, loc)
		endCivil := to.AddDate(0, 0, 1)
		rangeEnd := time.Date(endCivil.Year(), endCivil.Month(), endCivil.Day(), 0, 0, 0, 0, loc)
		days := map[string]int{}
		for day := from; !day.After(to); day = day.AddDate(0, 0, 1) {
			date := day.Format("2006-01-02")
			days[date] = len(out.Days)
			out.Days = append(out.Days, workspace.ScheduleDay{Date: date, Items: []workspace.ScheduleEntry{}})
		}
		add := func(entry workspace.ScheduleEntry) {
			if i, ok := days[entry.Date]; ok {
				out.Days[i].Items = append(out.Days[i].Items, entry)
			}
		}
		rows, err := tx.Query(ctx, `SELECT d.id::text,d.kind,d.at,d.recurrence,d.title,d.time_note,d.claim_id::text,coalesce(nullif(d.original_text,''),c.value #>> '{}'),d.date_only,d.schedule_rule,coalesce(rv.expressed_at,rv.recorded_at)
 FROM deadlines d JOIN memory_records r ON(r.owner_id,r.id,r.version)=(d.owner_id,d.claim_id,d.claim_version)
 JOIN record_versions rv ON(rv.owner_id,rv.record_id,rv.version)=(r.owner_id,r.id,r.version)
 JOIN claims cl ON(cl.owner_id,cl.id)=(r.owner_id,r.id)
 JOIN claim_revisions c ON(c.owner_id,c.claim_id,c.version)=(r.owner_id,r.id,r.version)
 WHERE d.owner_id=$1 AND r.state='active' AND rv.state='active' AND cl.retired='' AND NOT(coalesce(c.scope->>'deadline_completed','false')='true' AND c.scope->>'deadline_completed_version'=c.version::text) AND claim_source_is_current(r.owner_id,r.id,r.version,now())
 AND (d.kind IN('recurring','unclear') OR d.at<$2) ORDER BY d.id`, string(scope.OwnerID), rangeEnd)
		if err != nil {
			return err
		}
		for rows.Next() {
			var e workspace.ScheduleEntry
			var recurrence string
			var at *time.Time
			var ruleJSON []byte
			var expressed time.Time
			if err = rows.Scan(&e.Source.DeadlineID, &e.Kind, &at, &recurrence, &e.Title, &e.TimeNote, &e.Source.MemoryID, &e.OriginalText, &e.DateOnly, &ruleJSON, &expressed); err != nil {
				rows.Close()
				return err
			}
			e.ID = e.Source.DeadlineID
			e.Source.Kind = "deadline"
			if e.Kind == "recurring" {
				var rule scheduleRule
				valid := len(ruleJSON) > 0 && json.Unmarshal(ruleJSON, &rule) == nil && rule.valid()
				if !valid {
					rule, valid = legacyScheduleRule(recurrence, expressed, loc)
				}
				if !valid {
					e.At = nil
					e.DateOnly = true
					e.TimeNote = "周期尚未明确，正在整理；" + e.TimeNote
					out.Unclear = append(out.Unclear, e)
					continue
				}
				for day := from; !day.After(to); day = day.AddDate(0, 0, 1) {
					if !rule.occurs(day) {
						continue
					}
					instance := e
					instance.Date = day.Format("2006-01-02")
					instance.ID = e.ID + ":" + instance.Date
					instance.At = nil
					instance.DateOnly = rule.Clock == ""
					if rule.Clock != "" {
						when, ok := scheduleClock(day, rule.Clock, loc)
						if !ok {
							instance.TimeNote = "当地夏令时跳过了这个钟点；" + e.TimeNote
							out.Unclear = append(out.Unclear, instance)
							continue
						}
						v := when.Format(time.RFC3339)
						instance.At = &v
					} else if instance.TimeNote == "" {
						instance.TimeNote = "未说明具体时间"
					}
					add(instance)
				}
				continue
			}
			if at == nil || e.Kind == "unclear" {
				e.At = nil
				e.DateOnly = true
				out.Unclear = append(out.Unclear, e)
				continue
			}
			local := at.In(loc)
			e.Date = local.Format("2006-01-02")
			if e.DateOnly {
				e.At = nil
			} else {
				v := local.Format(time.RFC3339)
				e.At = &v
			}
			if local.Before(rangeStart) {
				if e.Kind == "deadline" {
					out.Overdue = append(out.Overdue, e)
				}
				continue
			}
			add(e)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		items, err := queryDocuments[workspace.Item](ctx, tx, `SELECT document FROM work_items WHERE owner_id=$1 AND kind='task' AND status NOT IN('done','cancelled') AND (due_at IS NOT NULL OR scheduled_at IS NOT NULL) ORDER BY id`, string(scope.OwnerID))
		if err != nil {
			return err
		}
		for _, item := range items {
			for _, v := range []struct{ kind, value string }{{"due", item.Due}, {"scheduled", item.Scheduled}} {
				if v.value == "" || v.kind == "scheduled" && v.value == item.Due {
					continue
				}
				at, err := time.Parse(time.RFC3339Nano, v.value)
				dateOnly := false
				if err != nil {
					at, err = time.ParseInLocation("2006-01-02", v.value, loc)
					dateOnly = err == nil
				}
				if err != nil {
					continue
				}
				local := at.In(loc)
				if v.kind == "due" {
					if item.DueDateOnly != nil {
						dateOnly = *item.DueDateOnly
					} else {
						dateOnly = dateOnly || local.Hour() == 23 && local.Minute() == 59 && local.Second() == 0
					}
				}
				e := workspace.ScheduleEntry{ID: item.ID + ":" + v.kind, Title: item.Title, Kind: "task", Date: local.Format("2006-01-02"), DateOnly: dateOnly, Source: workspace.ScheduleSource{Kind: "task", ItemID: item.ID}}
				if item.Creation != nil && item.Creation.Source != nil {
					e.OriginalText = item.Creation.Source.Excerpt
				}
				if dateOnly {
					e.TimeNote = "只有日期"
				} else {
					timeValue := local.Format(time.RFC3339)
					e.At = &timeValue
				}
				if local.Before(rangeStart) {
					if v.kind == "due" {
						out.Overdue = append(out.Overdue, e)
					}
					continue
				}
				add(e)
			}
		}
		less := func(a, b workspace.ScheduleEntry) bool {
			if a.At == nil || b.At == nil {
				if a.At == nil && b.At != nil {
					return false
				}
				if a.At != nil && b.At == nil {
					return true
				}
			} else {
				ta, _ := time.Parse(time.RFC3339, *a.At)
				tb, _ := time.Parse(time.RFC3339, *b.At)
				if !ta.Equal(tb) {
					return ta.Before(tb)
				}
			}
			if a.Date != b.Date {
				return a.Date < b.Date
			}
			return a.ID < b.ID
		}
		for i := range out.Days {
			sort.Slice(out.Days[i].Items, func(a, b int) bool { return less(out.Days[i].Items[a], out.Days[i].Items[b]) })
		}
		sort.Slice(out.Overdue, func(a, b int) bool { return less(out.Overdue[a], out.Overdue[b]) })
		return nil
	})
	return out, err
}
func (s *Store) ListInProgress(ctx context.Context, scope memory.Scope) (workspace.InProgress, error) {
	out := workspace.InProgress{Items: []workspace.Item{}}
	if err := requireOwner(scope); err != nil {
		return out, err
	}
	err := pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{AccessMode: pgx.ReadOnly, IsoLevel: pgx.RepeatableRead}, func(tx pgx.Tx) error {
		items, err := queryDocuments[workspace.Item](ctx, tx, `SELECT document FROM work_items WHERE owner_id=$1 AND kind='task' AND status NOT IN('done','cancelled') AND due_at IS NULL AND scheduled_at IS NULL AND NOT coalesce((document->>'urgent')::boolean,false) ORDER BY coalesce(nullif(document->>'updatedAt','')::timestamptz,updated_at) DESC,id`, string(scope.OwnerID))
		if err != nil {
			return err
		}
		// Validate the document too, for old imported rows with absent SQL columns.
		for _, item := range items {
			if strings.TrimSpace(item.Due) != "" || strings.TrimSpace(item.Scheduled) != "" || item.Urgent {
				continue
			}
			out.Total++
			if len(out.Items) < inProgressLimit {
				out.Items = append(out.Items, item)
			}
		}
		out.Remaining = out.Total - len(out.Items)
		return nil
	})
	return out, err
}
