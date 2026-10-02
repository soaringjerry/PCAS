package postgres

import (
	"context"
	"net/url"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func (s *Store) secretaryCardsTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, answer secretaryOutput, sent map[string]workspace.Memory, sentSources map[string]workspace.DeskSourceItem, aliases map[string]workspace.Item, loc *time.Location, recall bool) ([]workspace.DeskCard, error) {
	cards := []workspace.DeskCard{}
	sources := []workspace.DeskSourceItem{}
	timeline := []workspace.DeskTimelineItem{}
	refs := []memory.Ref{}
	for _, alias := range answer.Used {
		if _, ok := sentSources[alias]; ok {
			continue
		}
		if m, ok := sent[alias]; ok {
			refs = append(refs, memory.Ref{ID: memory.ID(m.ID), Version: m.Version})
		}
	}
	metadata, err := secretaryTimelineMetadataTx(ctx, tx, scope, uniqueRefs(refs))
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, alias := range answer.Used {
		if source, ok := sentSources[alias]; ok {
			if !seen[source.MemoryID] {
				seen[source.MemoryID] = true
				sources = append(sources, source)
			}
			// Source citations do not participate in this batch's timeline.
			continue
		}
		m, ok := sent[alias]
		if !ok || seen[m.ID] {
			continue
		}
		seen[m.ID] = true
		// Claim grants do not necessarily grant the model the source itself.
		// Provenance is resolved for the owner response, never added to the prompt.
		meta := metadata[m.ID]
		if len(m.Sources) == 0 && meta.Source.SourceID != "" {
			m.Sources = []workspace.SourceRef{meta.Source.SourceRef}
		}
		source := workspace.DeskSourceItem{Kind: "claim", MemoryID: m.ID, Version: m.Version, Text: m.Text}
		if len(m.Sources) > 0 {
			source.SourceID = m.Sources[0].SourceID
			source.SourceVersion = m.Sources[0].Version
			source.At = stringPointer(m.Sources[0].At)
		}
		sources = append(sources, source)
		entry := workspace.DeskTimelineItem{
			At: secretaryTimelineAt(meta.Memory.ExpressedAt, meta.Source), Text: m.Text,
			Status: secretaryTimelineStatus(meta.Tasks, meta.Changed), MemoryID: stringPointer(m.ID),
			EventFrom: meta.Memory.EventFrom, EventTo: meta.Memory.EventTo,
			EventPrecision: meta.Memory.EventPrecision, Mentions: meta.Memory.Mentions,
		}
		if len(meta.Tasks) > 0 {
			entry.ThingID = stringPointer(meta.Tasks[0].ID)
		}
		timeline = append(timeline, entry)
	}
	if len(sources) > 0 {
		cards = append(cards, workspace.DeskCard{Kind: "sources", Items: sources})
	}
	links := []workspace.DeskLinkItem{}
	for _, link := range answer.Links {
		u, err := url.Parse(link)
		if err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && u.User == nil && len(link) <= 500 && len(links) < 3 {
			links = append(links, workspace.DeskLinkItem{URL: u.String(), Host: u.Hostname()})
		}
	}
	if len(links) > 0 {
		cards = append(cards, workspace.DeskCard{Kind: "links", Items: links})
	}
	if secretaryTimelineVisible(timeline, recall) {
		sort.SliceStable(timeline, func(i, j int) bool { return secretaryTimelineBefore(timeline[i], timeline[j]) })
		cards = append(cards, workspace.DeskCard{Kind: "timeline", Title: "相关记录", Items: timeline})
	}
	tasks := []workspace.DeskTaskItem{}
	seen = map[string]bool{}
	for _, alias := range answer.Show {
		item, ok := aliases[alias]
		if !ok || item.Kind != "task" || seen[item.ID] {
			continue
		}
		seen[item.ID] = true
		current, err := getItem(ctx, tx, scope, item.ID)
		if err != nil {
			return nil, err
		}
		// The title was sanitized before generation; use the fresh due/status only.
		task := workspace.DeskTaskItem{ThingID: item.ID, Title: item.Title, Due: stringPointer(current.Due), Status: current.Status}
		if current.ProjectID != "" {
			for _, p := range aliases {
				if p.ID == current.ProjectID {
					task.Project = stringPointer(p.Title)
					break
				}
			}
		}
		tasks = append(tasks, task)
	}
	if len(tasks) > 0 {
		cards = append(cards, workspace.DeskCard{Kind: "tasks", Items: tasks})
	}
	return cards, nil
}

type secretaryTimelineTask struct {
	ID     string
	Status string
}

type secretaryTimelineMetadata struct {
	Memory  workspace.Memory
	Source  secretaryTimelineSource
	Tasks   []secretaryTimelineTask
	Changed bool
}

type secretaryTimelineSource struct {
	workspace.SourceRef
	ExpressedAt string
	Connector   string
}

// Resolve metadata and the creating turn's surviving items in one batch. Source
// provenance belongs only to the owner's cards, never to the model's prompt.
func secretaryTimelineMetadataTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, refs []memory.Ref) (map[string]secretaryTimelineMetadata, error) {
	result := map[string]secretaryTimelineMetadata{}
	if len(refs) == 0 {
		return result, nil
	}
	rows, err := queryDocuments[secretaryTimelineMetadata](ctx, tx, `WITH cited AS (
 SELECT c.*,rv.expressed_at FROM jsonb_to_recordset($2::jsonb) AS i(id uuid,version integer)
 JOIN claim_revisions c ON c.owner_id=$1 AND c.claim_id=i.id AND c.version=i.version
 JOIN record_versions rv ON (rv.owner_id,rv.record_id,rv.version)=(c.owner_id,c.claim_id,c.version)
), turns AS (
 SELECT DISTINCT c.claim_id,c.version,t.id,t.response FROM cited c
 JOIN evidence e ON (e.owner_id,e.target_id,e.target_version)=(c.owner_id,c.claim_id,c.version)
 JOIN sources s ON (s.owner_id,s.id)=(e.owner_id,e.source_id)
 JOIN desk_turns t ON t.owner_id=s.owner_id AND t.request_id::text=lower(s.external_id)
 WHERE s.connector IN ('desk','capture','desk-incomplete')
), created_items AS (
 SELECT DISTINCT t.claim_id,t.version,w.id,w.status FROM turns t
 CROSS JOIN LATERAL jsonb_array_elements(CASE WHEN jsonb_typeof(t.response#>'{turn,receipts}')='array'
   THEN t.response#>'{turn,receipts}' ELSE '[]'::jsonb END) receipt
 JOIN work_items w ON w.owner_id=$1 AND w.id::text=receipt->>'thingId'
 WHERE receipt->>'op' IN ('create_task','create_idea','create_project') AND receipt->>'status'='done'
)
SELECT jsonb_build_object(
 'memory',jsonb_build_object('id',c.claim_id,'expressedAt',c.expressed_at,
   'eventFrom',c.event_from,'eventTo',c.event_to,
   'eventPrecision',CASE WHEN c.event_from IS NULL AND c.event_to IS NULL THEN '' ELSE c.event_precision END,
   'mentions',coalesce((SELECT jsonb_agg(jsonb_build_object('entityId',m.entity_id,'name',v.name,'role',m.role) ORDER BY m.role,v.name,m.entity_id)
     FROM claim_mentions m JOIN memory_records r ON r.owner_id=m.owner_id AND r.id=m.entity_id AND r.state='active'
     JOIN entity_versions v ON (v.owner_id,v.entity_id,v.version)=(r.owner_id,r.id,r.version)
     WHERE (m.owner_id,m.claim_id,m.claim_version)=(c.owner_id,c.claim_id,c.version)),'[]'::jsonb)),
 'source',coalesce((SELECT jsonb_build_object('sourceId',v.source_id,'version',v.version,'label',v.title,
   'at',rv.recorded_at,'expressedAt',rv.expressed_at,'connector',s.connector)
   FROM evidence e JOIN source_versions v ON (v.owner_id,v.source_id,v.version)=(e.owner_id,e.source_id,e.source_version)
   JOIN sources s ON (s.owner_id,s.id)=(v.owner_id,v.source_id)
   JOIN record_versions rv ON (rv.owner_id,rv.record_id,rv.version)=(v.owner_id,v.source_id,v.version)
   WHERE (e.owner_id,e.target_id,e.target_version)=(c.owner_id,c.claim_id,c.version)
   ORDER BY rv.recorded_at,v.source_id,v.version LIMIT 1),'{}'::jsonb),
 'tasks',coalesce((SELECT jsonb_agg(jsonb_build_object('id',w.id,'status',w.status) ORDER BY w.id)
   FROM created_items w WHERE (w.claim_id,w.version)=(c.claim_id,c.version)),'[]'::jsonb),
 'changed',EXISTS(SELECT 1 FROM claim_revisions v WHERE v.owner_id=c.owner_id AND v.claim_id=c.claim_id
   AND v.version>1 AND v.version<=c.version AND v.change_type IN ('change','correction')))
FROM cited c`, string(scope.OwnerID), asJSON(refs))
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		result[row.Memory.ID] = row
	}
	return result, nil
}

// Follow batch 2 R5 for old memories; an import's recorded_at is not its date.
func secretaryTimelineAt(expressed string, source secretaryTimelineSource) *string {
	if expressed != "" {
		return stringPointer(expressed)
	}
	if source.ExpressedAt != "" {
		return stringPointer(source.ExpressedAt)
	}
	if oneOf(source.Connector, "desk", "capture", "telegram", "desk-incomplete") {
		return stringPointer(source.At)
	}
	return nil
}

func secretaryTimelineStatus(tasks []secretaryTimelineTask, changed bool) string {
	if len(tasks) == 0 {
		if changed {
			return "changed"
		}
		return "open"
	}
	done, dropped := true, true
	for _, task := range tasks {
		done = done && task.Status == "done"
		dropped = dropped && oneOf(task.Status, "cancelled", "dropped")
	}
	if done {
		return "done"
	}
	if dropped {
		return "dropped"
	}
	return "open"
}

func secretaryTimelineVisible(items []workspace.DeskTimelineItem, recall bool) bool {
	if len(items) >= 2 {
		return true
	}
	return recall && len(items) == 1 && items[0].At != nil
}

func secretaryTimelineBefore(a, b workspace.DeskTimelineItem) bool {
	if a.At == nil {
		return false
	}
	if b.At == nil {
		return true
	}
	left, _ := time.Parse(time.RFC3339Nano, *a.At)
	right, _ := time.Parse(time.RFC3339Nano, *b.At)
	return left.Before(right)
}
