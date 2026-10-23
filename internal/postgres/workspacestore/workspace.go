package workspacestore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/soaringjerry/PCAS/internal/interpretation/organize"
	"github.com/soaringjerry/PCAS/internal/postgres/kernel"
	"github.com/soaringjerry/PCAS/internal/postgres/memoryread"
	"github.com/soaringjerry/PCAS/internal/postgres/memorystore"
	"github.com/soaringjerry/PCAS/internal/postgres/statestore"
	"github.com/soaringjerry/PCAS/internal/workspace/activity"
	"github.com/soaringjerry/PCAS/internal/workspace/itemcommand"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
	"github.com/soaringjerry/PCAS/internal/workspace/effort"
)

func PatchAllowed(dst any, patch json.RawMessage, keys ...string) error {
	return itemcommand.Patch(dst, patch, keys...)
}
func (s *Workspace) Snapshot(ctx context.Context, scope memory.Scope) (workspace.State, error) {
	var out workspace.State
	if err := kernel.RequireOwner(scope); err != nil {
		return out, err
	}
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		if err := s.EnsureOwner(ctx, tx, scope); err != nil {
			return err
		}
		var err error
		out, err = s.SnapshotTx(ctx, tx, scope)
		return err
	})
	return out, err
}
func (s *Workspace) SnapshotTx(ctx context.Context, tx pgx.Tx, scope memory.Scope) (workspace.State, error) {
	out := workspace.State{Version: 1, Tasks: []workspace.Item{}, Ideas: []workspace.Item{}, Projects: []workspace.Item{}, Sources: []workspace.Source{}, Jobs: []workspace.Job{}, ExcludedMemories: map[string][]string{}}
	var data []byte
	err := tx.QueryRow(ctx, "SELECT revision,settings FROM workspace_owners WHERE owner_id=$1 FOR SHARE", string(scope.OwnerID)).Scan(&out.Revision, &data)
	if err != nil {
		return out, err
	}
	if err := tx.QueryRow(ctx, "SELECT coalesce((to_jsonb(o)->>'library_revision')::bigint,1),coalesce((to_jsonb(o)->>'snapshot_revision')::bigint,1) FROM workspace_owners o WHERE owner_id=$1", string(scope.OwnerID)).Scan(&out.MemoryRevision, &out.SnapshotRevision); err != nil {
		return out, err
	}
	var journal bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema() AND c.relname='workspace_snapshot_events')").Scan(&journal); err != nil {
		return out, err
	}
	if journal {
		var events int64
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM workspace_snapshot_events WHERE owner_id=$1", string(scope.OwnerID)).Scan(&events); err != nil {
			return out, err
		}
		out.SnapshotRevision += events
	}
	changes, err := libraryEvents(ctx, tx, scope.OwnerID)
	if err != nil {
		return out, err
	}
	out.MemoryRevision += changes
	if err = json.Unmarshal(data, &out.Settings); err != nil {
		return out, err
	}
	out.Settings.AutoAccept = false
	items, err := kernel.QueryDocuments[workspace.Item](ctx, tx, `SELECT document || CASE WHEN coalesce(document->>'createdAt','')='' THEN jsonb_build_object('createdAt',created_at) ELSE '{}'::jsonb END FROM work_items WHERE owner_id=$1 ORDER BY updated_at DESC,id`, string(scope.OwnerID))
	if err != nil {
		return out, err
	}
	studioInstalled, err := StudioInstalledTx(ctx, tx)
	if err != nil {
		return out, err
	}
	for _, item := range items {
		if studioInstalled && item.Kind == "project" {
			h, e := ProjectHandoverTx(ctx, tx, scope, item.ID)
			if e != nil {
				return out, e
			}
			item.ProjectHandover = &h
		}
		switch item.Kind {
		case "task":
			item.StartDate, err = effort.StartDate(item.Due, item.EstimatedHours, out.Settings.Location())
			if err != nil {
				return out, err
			}
			out.Tasks = append(out.Tasks, item)
		case "idea":
			out.Ideas = append(out.Ideas, item)
		case "project":
			out.Projects = append(out.Projects, item)
		}
	}
	if out.Notices, err = kernel.QueryDocuments[workspace.Notice](ctx, tx, `SELECT jsonb_build_object(
        'id',n.id,'thingId',n.thing_id,'title',w.title,'reason',n.reason,
        'dueAt',n.due_at,'createdAt',n.created_at) ||
        CASE WHEN n.trigger_id LIKE 'run:%' THEN '{"result":true}'::jsonb ELSE '{}'::jsonb END ||
        CASE WHEN n.dismissed_at IS NULL THEN '{}'::jsonb ELSE jsonb_build_object('dismissedAt',n.dismissed_at) END
        FROM workspace_notices n JOIN work_items w ON (w.owner_id,w.id)=(n.owner_id,n.thing_id)
        WHERE n.owner_id=$1 AND (n.dismissed_at IS NOT NULL OR `+CurrentNoticeTriggerSQL+`) AND NOT (n.delivered @> '{"_suppressionOnly":true}'::jsonb) ORDER BY (n.dismissed_at IS NOT NULL),n.created_at DESC,n.id LIMIT 100`, string(scope.OwnerID)); err != nil {
		return out, err
	}
	if out.Memories, err = s.ReadMemoriesTx(ctx, tx, scope, false, memoryread.MemoryReadOptions{Limit: 200}); err != nil {
		return out, err
	}
	where, args := memoryread.MemoryWhere(scope, false, memoryread.MemoryReadOptions{})
	out.Organize.Version = organize.RuleVersion
	if err := tx.QueryRow(ctx, `SELECT count(*) FILTER (WHERE coalesce((to_jsonb(cl)->>'organized')::int,0) >= `+kernel.OrganizeRequiredSQL("coalesce(to_jsonb(c)->>'category','')", "$5")+`),count(*)`+memoryread.MemoryJoins+` JOIN claims cl ON(cl.owner_id,cl.id)=(r.owner_id,r.id)`+where,
		append(args, organize.RuleVersion)...).Scan(&out.Organize.Done, &out.Organize.Total); err != nil {
		return out, err
	}
	// Both counts use exactly the same visible current rows. Every revision has
	// a claims parent by foreign key, so the organizing count already supplies
	// the established memory total without evaluating source validity twice.
	out.MemoryTotal = out.Organize.Total
	if out.Agents, err = kernel.QueryDocuments[workspace.Agent](ctx, tx, "SELECT document FROM workspace_agents WHERE owner_id=$1 ORDER BY id", string(scope.OwnerID)); err != nil {
		return out, err
	}
	for i := range out.Agents {
		a := &out.Agents[i]
		if a.Channel == "manual" {
			a.Available = true
		}
		if p, ok := s.Model(a.ID); ok {
			a.Default = a.ID == s.Models.ExtractionID()
			a.Protocol = p.Protocol
			a.Available = s.ModelAvailable(a.ID)
			a.InputPrice = p.InputPerMillion
			a.OutputPrice = p.OutputPerMillion
			a.MaxOutput = p.MaxOutput
			if note := activity.AgentNote(p.Protocol, p.Model); note != "" {
				a.Note = note
			}
		}
	}
	if s.Models != nil && s.Models.DirectDefault() {
		// Preserve per-agent visibility/preferences; only reorder the default.
		for i, a := range out.Agents {
			if a.ID == "chatgpt-direct" {
				out.Agents = append([]workspace.Agent{a}, append(out.Agents[:i], out.Agents[i+1:]...)...)
				break
			}
		}
	}
	loc, err := time.LoadLocation(out.Settings.Timezone)
	if err != nil {
		return out, err
	}
	now := time.Now().In(loc)
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	if err := tx.QueryRow(ctx, "SELECT coalesce((SELECT sum(reserved_cost) FROM agent_runs WHERE owner_id=$1 AND created_at>=$2),0)+coalesce((SELECT sum(reserved_cost) FROM background_usage WHERE owner_id=$1 AND created_at>=$2),0)", string(scope.OwnerID), start).Scan(&out.BudgetUsage); err != nil {
		return out, err
	}
	if out.Candidates, err = kernel.QueryDocuments[workspace.Candidate](ctx, tx, "SELECT document FROM capture_candidates WHERE owner_id=$1 ORDER BY document->>'createdAt' DESC,id", string(scope.OwnerID)); err != nil {
		return out, err
	}
	if out.Docs, err = kernel.QueryDocuments[workspace.Doc](ctx, tx, "SELECT document FROM work_documents WHERE owner_id=$1 ORDER BY document->>'updatedAt' DESC,id", string(scope.OwnerID)); err != nil {
		return out, err
	}
	if out.Runs, err = kernel.QueryDocuments[workspace.Run](ctx, tx, "SELECT document FROM agent_runs WHERE owner_id=$1 ORDER BY created_at DESC,id", string(scope.OwnerID)); err != nil {
		return out, err
	}
	if out.Samples, err = kernel.QueryDocuments[workspace.Sample](ctx, tx, "SELECT document || jsonb_build_object('stale',stale,'state',state) FROM training_samples WHERE owner_id=$1 ORDER BY document->>'createdAt' DESC,id", string(scope.OwnerID)); err != nil {
		return out, err
	}
	if out.Sources, err = memorystore.SourceGroupsTx(ctx, tx, scope); err != nil {
		return out, err
	}
	rows, err := tx.Query(ctx, `SELECT j.id::text,j.stage,j.state,j.error_code,j.created_at,j.available_at,
		coalesce((SELECT v.title FROM source_versions v WHERE (v.owner_id,v.source_id,v.version)=(j.owner_id,j.record_id,j.record_version)),'')
		FROM memory_jobs j WHERE j.owner_id=$1 ORDER BY j.created_at DESC LIMIT 500`, string(scope.OwnerID))
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var id, stage, state, code, source string
		var at, next time.Time
		if err = rows.Scan(&id, &stage, &state, &code, &at, &next, &source); err != nil {
			rows.Close()
			return out, err
		}
		out.Jobs = append(out.Jobs, activity.Job(id, stage, state, code, source, at, next))
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	rows, err = tx.Query(ctx, `SELECT 'notice:'||thing_id::text||':'||trigger_id||':'||due_at::text,reason,created_at FROM workspace_notices WHERE owner_id=$1 AND NOT (delivered @> '{"_suppressionOnly":true}'::jsonb) ORDER BY created_at DESC LIMIT 100`, string(scope.OwnerID))
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var id, reason string
		var at time.Time
		if err = rows.Scan(&id, &reason, &at); err != nil {
			rows.Close()
			return out, err
		}
		out.Jobs = append(out.Jobs, activity.NoticeJob(id, reason, at))
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return out, err
	}
	rows, err = tx.Query(ctx, `SELECT 'review:'||due_at::text,pending_count,created_at FROM workspace_reviews WHERE owner_id=$1 ORDER BY created_at DESC LIMIT 30`, string(scope.OwnerID))
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var id string
		var pending int
		var at time.Time
		if err = rows.Scan(&id, &pending, &at); err != nil {
			rows.Close()
			return out, err
		}
		out.Jobs = append(out.Jobs, activity.ReviewJob(id, pending, at))
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return out, err
	}
	if out.Activity, err = ActivityTx(ctx, tx, scope, out.Settings.Timezone); err != nil {
		return out, err
	}
	rows, err = tx.Query(ctx, "SELECT thing_id::text,memory_id::text FROM context_exclusions WHERE owner_id=$1", string(scope.OwnerID))
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var thing, id string
		if err = rows.Scan(&thing, &id); err != nil {
			rows.Close()
			return out, err
		}
		out.ExcludedMemories[thing] = append(out.ExcludedMemories[thing], id)
	}
	err = rows.Err()
	rows.Close()
	return out, err
}

func (s *Workspace) Export(ctx context.Context, scope memory.Scope, training, confirmedOnly bool) ([]byte, error) {
	if err := kernel.RequireOwner(scope); err != nil {
		return nil, err
	}
	var data []byte
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		if err := s.EnsureOwner(ctx, tx, scope); err != nil {
			return err
		}
		state, err := s.SnapshotTx(ctx, tx, scope)
		if err != nil {
			return err
		}
		if !training {
			sources, err := kernel.QueryDocuments[json.RawMessage](ctx, tx, `SELECT jsonb_build_object('id',s.id,'connector',s.connector,'external_id',s.external_id,'version',v.version,'external_version',v.external_version,'title',v.title,'text',v.body,'media_type',v.media_type,'expressed_at',rv.expressed_at,'recorded_at',rv.recorded_at)
			FROM sources s JOIN source_versions v ON (v.owner_id,v.source_id)=(s.owner_id,s.id) JOIN record_versions rv ON (rv.owner_id,rv.record_id,rv.version)=(v.owner_id,v.source_id,v.version) WHERE s.owner_id=$1 ORDER BY s.id,v.version`, string(scope.OwnerID))
			if err != nil {
				return err
			}
			canonical := map[string][]json.RawMessage{}
			// Names are a fixed server allowlist, never caller-supplied SQL.
			for _, table := range []string{"memory_records", "record_versions", "sources", "source_versions", "entities", "entity_versions", "aliases", "episodes", "episode_members", "claims", "claim_revisions", "evidence", "relations", "activity", "record_grants", "claim_source_keys", "claim_key_redirects", "claim_reimport_blocks", "reimport_blocks", "record_reimport_blocks", "source_contexts", "episode_keys", "archive_entries", "claim_mentions", "source_extractions", "import_batches", "model_usage", "execution_timings"} {
				rows, err := kernel.QueryDocuments[json.RawMessage](ctx, tx, "SELECT to_jsonb(t) FROM "+table+" t WHERE owner_id=$1", string(scope.OwnerID))
				if err != nil {
					return err
				}
				canonical[table] = rows
			}
			data = kernel.JSON(map[string]any{"format": "pcas-export-1", "createdAt": kernel.Stamp(), "state": state, "sourceVersions": sources, "canonical": canonical, "attachments": "Binary originals must be backed up from the memory-files volume."})
			return nil
		}
		manifest := []map[string]any{}
		var buf bytes.Buffer
		for _, sample := range state.Samples {
			if sample.State != "included" || sample.Stale || confirmedOnly && sample.Epistemic != "confirmed" {
				continue
			}
			if err := json.NewEncoder(&buf).Encode(map[string]any{"messages": []map[string]string{{"role": "user", "content": sample.Prompt}, {"role": "assistant", "content": sample.Response}}, "metadata": sample}); err != nil {
				return err
			}
			manifest = append(manifest, map[string]any{"id": sample.ID, "version": sample.Version, "origin": sample.Origin})
		}
		_, err = tx.Exec(ctx, "INSERT INTO memory_export_manifests(owner_id,id,sample_versions) VALUES($1,$2,$3)", string(scope.OwnerID), string(memory.NewID()), kernel.JSON(manifest))
		data = buf.Bytes()
		return err
	})
	return data, err
}

func UuidOrNew(id string) (string, error) {
	if id == "" {
		return string(memory.NewID()), nil
	}
	if !memory.ID(id).Valid() {
		return "", memory.ErrInvalid
	}
	return id, nil
}
func wrapInvalid(field string) error { return fmt.Errorf("%s: %w", field, memory.ErrInvalid) }

// activityTx is the home screen's "while you were away" log. Processing jobs
// are folded per day into sources finished and problems by cause, so one
// import reads as one line rather than a dozen stage names. Idea wakes and
// finished runs are already on their own records and are added by the client.
func ActivityTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, timezone string) ([]workspace.Activity, error) {
	// Automatic creation is presented once through the item and its creation
	// actionId. A topic linked to an existing project has no new item receipt,
	// so retain its activity while excluding all automatic creation activities.
	out, err := kernel.QueryDocuments[workspace.Activity](ctx, tx, `SELECT jsonb_build_object('id',a.id::text,'at',a.created_at,'text',a.summary,'to','/t/'||l.project_id::text)
 FROM action_log a JOIN topic_project_links l ON(l.owner_id,l.action_id)=(a.owner_id,a.id)
 WHERE a.owner_id=$1 AND a.source='background_topic' AND NOT l.created_project
 AND a.undone_at IS NULL AND a.expired_at IS NULL AND a.created_at>=now()-interval '30 days'
 ORDER BY a.created_at DESC,a.id`, string(scope.OwnerID))
	if err != nil {
		return nil, err
	}
	owner := string(scope.OwnerID)
	tidied, err := statestore.TidiedDatesTx(ctx, tx, scope)
	if err != nil {
		return nil, err
	}
	if tidied != nil {
		out = append(out, *tidied)
	}
	rows, err := tx.Query(ctx, `WITH finished AS (
		SELECT j.record_id,j.record_version,max(j.updated_at) AS at FROM memory_jobs j
		JOIN memory_records r ON (r.owner_id,r.id,r.version)=(j.owner_id,j.record_id,j.record_version) AND r.state='active'
		WHERE j.owner_id=$1 AND j.stage LIKE 'source.%'
		GROUP BY j.record_id,j.record_version
		HAVING bool_and(j.state='done') AND max(j.updated_at)>=now()-interval '2 days')
		SELECT (f.at AT TIME ZONE $2)::date::text,max(f.at),count(*),(array_agg(v.title ORDER BY f.at DESC))[1]
		FROM finished f JOIN source_versions v ON (v.owner_id,v.source_id,v.version)=($1,f.record_id,f.record_version)
		GROUP BY 1`, owner, timezone)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var day, title string
		var at time.Time
		var n int
		if err = rows.Scan(&day, &at, &n, &title); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, activity.SourcesFinished(day, at, n, title))
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	rows, err = tx.Query(ctx, `SELECT error_code,count(*),max(updated_at) FROM memory_jobs
		WHERE owner_id=$1 AND state IN ('failed','blocked') AND updated_at>=now()-interval '2 days' GROUP BY error_code`, owner)
	if err != nil {
		return nil, err
	}
	failures := []activity.Failure{}
	for rows.Next() {
		var f activity.Failure
		if err = rows.Scan(&f.Code, &f.Count, &f.At); err != nil {
			rows.Close()
			return nil, err
		}
		failures = append(failures, f)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	out = append(out, activity.Problems(failures)...)
	rows, err = tx.Query(ctx, `SELECT n.thing_id::text,n.trigger_id,n.due_at,w.title,n.reason,n.created_at FROM workspace_notices n
		JOIN work_items w ON (w.owner_id,w.id)=(n.owner_id,n.thing_id)
		WHERE n.owner_id=$1 AND NOT (n.delivered @> '{"_suppressionOnly":true}'::jsonb) AND w.kind='task' AND n.trigger_id NOT LIKE 'run:%' AND n.created_at>=now()-interval '2 days' ORDER BY n.created_at DESC LIMIT 20`, owner)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var thing, trigger, title, reason string
		var due, at time.Time
		if err = rows.Scan(&thing, &trigger, &due, &title, &reason, &at); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, activity.Reminded(thing, trigger, due, title, reason, at))
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	rows, err = tx.Query(ctx, `SELECT due_at,pending_count,created_at FROM workspace_reviews
		WHERE owner_id=$1 AND pending_count>0 AND created_at>=now()-interval '2 days' ORDER BY created_at DESC LIMIT 2`, owner)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var due, at time.Time
		var n int
		if err = rows.Scan(&due, &n, &at); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, activity.Reviewed(due, n, at))
	}
	rows.Close()
	return out, rows.Err()
}

// New enabled agents inherit only memories already shared with a deputy. Old
// enabled documents are marked initialized by ensureOwner without this call.

// SnapshotETag reads only counters; matching polling requests never hydrate the
// memory list. The token includes job/notices freshness as well as commands.
func (s *Workspace) SnapshotETag(ctx context.Context, scope memory.Scope) (string, error) {
	if err := kernel.RequireOwner(scope); err != nil {
		return "", err
	}
	var revision, library, snapshot int64
	err := s.Pool.QueryRow(ctx, "SELECT revision,library_revision,snapshot_revision+(SELECT count(*) FROM workspace_snapshot_events WHERE owner_id=$1) FROM workspace_owners WHERE owner_id=$1", string(scope.OwnerID)).Scan(&revision, &library, &snapshot)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	changes, err := libraryEvents(ctx, s.Pool, scope.OwnerID)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("\"workspace-%s-%d-%d-%d\"", scope.OwnerID, revision, library+changes, snapshot), nil
}

// Memory changes counted since migration 045, which stopped writing them to
// the owner's row. Zero on a schema that predates it.
func libraryEvents(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, owner memory.ID) (int64, error) {
	var journal bool
	if err := q.QueryRow(ctx, "SELECT to_regclass('workspace_library_events') IS NOT NULL").Scan(&journal); err != nil || !journal {
		return 0, err
	}
	var n int64
	err := q.QueryRow(ctx, "SELECT count(*) FROM workspace_library_events WHERE owner_id=$1", string(owner)).Scan(&n)
	return n, err
}
