package postgres

import (
	"context"
	"errors"
	"net/url"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func (s *Store) secretaryCardsTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, answer secretaryOutput, sent map[string]workspace.Memory, sentSources map[string]workspace.DeskSourceItem, aliases map[string]workspace.Item, loc *time.Location) ([]workspace.DeskCard, error) {
	cards := []workspace.DeskCard{}
	sources := []workspace.DeskSourceItem{}
	timeline := []workspace.DeskTimelineItem{}
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
		if len(m.Sources) == 0 {
			provenance, err := queryDocuments[workspace.SourceRef](ctx, tx, `SELECT jsonb_build_object('sourceId',v.source_id,'version',v.version,'label',v.title,'at',rv.recorded_at)
                FROM evidence e JOIN source_versions v ON (v.owner_id,v.source_id,v.version)=(e.owner_id,e.source_id,e.source_version)
                JOIN record_versions rv ON (rv.owner_id,rv.record_id,rv.version)=(v.owner_id,v.source_id,v.version)
                WHERE e.owner_id=$1 AND e.target_id=$2 AND e.target_version=$3 ORDER BY rv.recorded_at,v.source_id,v.version LIMIT 1`, string(scope.OwnerID), m.ID, m.Version)
			if err != nil {
				return nil, err
			}
			m.Sources = provenance
		}
		source := workspace.DeskSourceItem{Kind: "claim", MemoryID: m.ID, Version: m.Version, Text: m.Text}
		if len(m.Sources) > 0 {
			source.SourceID = m.Sources[0].SourceID
			source.SourceVersion = m.Sources[0].Version
			source.At = stringPointer(m.Sources[0].At)
		}
		sources = append(sources, source)
		if source.At != nil {
			if _, err := time.Parse(time.RFC3339, *source.At); err == nil {
				entry := workspace.DeskTimelineItem{At: source.At, Text: m.Text, Status: "open", MemoryID: stringPointer(m.ID)}
				var thingID, status string
				err := tx.QueryRow(ctx, `SELECT id::text,status FROM work_items w WHERE owner_id=$1 AND EXISTS(SELECT 1 FROM jsonb_array_elements(w.document->'sources') src WHERE src->>'sourceId'=$2) ORDER BY created_at,id LIMIT 1`, string(scope.OwnerID), source.SourceID).Scan(&thingID, &status)
				if err == nil {
					entry.ThingID = &thingID
					if oneOf(status, "done") {
						entry.Status = "done"
					}
					if oneOf(status, "cancelled", "dropped") {
						entry.Status = "dropped"
					}
				} else if !errors.Is(err, pgx.ErrNoRows) {
					return nil, err
				}
				timeline = append(timeline, entry)
			}
		}
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
	if len(timeline) >= 2 {
		sort.SliceStable(timeline, func(i, j int) bool {
			a, _ := time.Parse(time.RFC3339, *timeline[i].At)
			b, _ := time.Parse(time.RFC3339, *timeline[j].At)
			return a.Before(b)
		})
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
