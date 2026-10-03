package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func requireOwner(scope memory.Scope) error {
	if !scope.Valid() || !scope.IsOwner {
		return memory.ErrForbidden
	}
	return nil
}
func stamp() string       { return time.Now().UTC().Format(time.RFC3339Nano) }
func asJSON(v any) []byte { b, _ := json.Marshal(v); return b }
func requireText(v string) error {
	if strings.TrimSpace(v) == "" || len(v) > 1<<20 {
		return memory.ErrInvalid
	}
	return nil
}
func nullString(v string) any {
	if v == "" {
		return nil
	}
	return v
}
func queryDocument[T any](ctx context.Context, tx pgx.Tx, sql string, args ...any) (T, error) {
	var result T
	var data []byte
	err := tx.QueryRow(ctx, sql, args...).Scan(&data)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, memory.ErrNotFound
	}
	if err != nil {
		return result, err
	}
	err = json.Unmarshal(data, &result)
	return result, err
}
func queryDocuments[T any](ctx context.Context, tx pgx.Tx, sql string, args ...any) ([]T, error) {
	out := []T{}
	rows, err := tx.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var data []byte
		var v T
		if err = rows.Scan(&data); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(data, &v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func patchAllowed(dst any, patch json.RawMessage, keys ...string) error {
	var fields map[string]json.RawMessage
	if len(patch) == 0 || json.Unmarshal(patch, &fields) != nil || len(fields) == 0 {
		return memory.ErrInvalid
	}
	for k := range fields {
		if !oneOf(k, keys...) {
			return memory.ErrInvalid
		}
	}
	var merged map[string]json.RawMessage
	if json.Unmarshal(asJSON(dst), &merged) != nil {
		return memory.ErrInvalid
	}
	for k, v := range fields {
		if string(v) == "null" {
			delete(merged, k)
		} else {
			merged[k] = v
		}
	}
	replacement := reflect.New(reflect.TypeOf(dst).Elem())
	decoder := json.NewDecoder(bytes.NewReader(asJSON(merged)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(replacement.Interface()) != nil {
		return memory.ErrInvalid
	}
	reflect.ValueOf(dst).Elem().Set(replacement.Elem())
	return nil
}
func (s *Store) ensureOwner(ctx context.Context, tx pgx.Tx, scope memory.Scope) error {
	settings := workspace.Settings{WakeIdeas: true, FollowUps: true, DailyReviewAt: "09:00", DailyBudget: 10, Timezone: workspace.InitialTimezone(ctx)}
	if _, err := tx.Exec(ctx, "INSERT INTO workspace_owners(owner_id,settings) VALUES($1,$2) ON CONFLICT DO NOTHING", string(scope.OwnerID), asJSON(settings)); err != nil {
		return err
	}
	agents := []workspace.Agent{{ID: "manual", Name: "手动交接", Channel: "manual", Enabled: true, MemoryKinds: []string{"fact", "preference", "decision", "intention", "plan"}, Note: "复制上下文到任意 AI，再粘贴结果"}}
	if s.models != nil {
		for _, p := range s.models.Providers() {
			if p.Embedding || p.Transcription {
				continue
			}
			note := "通用 API · " + p.Model
			if p.Protocol == "codex" {
				note = "ChatGPT 订阅 · 在设置中登录"
			}
			if p.Protocol == "siwc" {
				note = "消耗你的 ChatGPT 套餐 · 官方直接授权"
			}
			agents = append(agents, workspace.Agent{ID: p.ID, Name: p.Name, Channel: "api", Note: note, Enabled: true, MemoryKinds: []string{"fact", "preference", "decision", "intention", "plan"}})
		}
	}
	for _, a := range agents {
		if a.ID == "chatgpt-direct" {
			legacy, err := queryDocument[workspace.Agent](ctx, tx, "SELECT document FROM workspace_agents WHERE owner_id=$1 AND id='chatgpt'", string(scope.OwnerID))
			if err == nil {
				a.Enabled = legacy.Enabled
				a.MemoryKinds = legacy.MemoryKinds
				a.IncludeInferred = legacy.IncludeInferred
			} else if !errors.Is(err, memory.ErrNotFound) {
				return err
			}
		}
		tag, err := tx.Exec(ctx, "INSERT INTO workspace_agents(owner_id,id,document) VALUES($1,$2,$3) ON CONFLICT DO NOTHING", string(scope.OwnerID), a.ID, asJSON(a))
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 1 && a.Enabled {
			if err := initializeAgentMemoriesTx(ctx, tx, scope.OwnerID, a.ID); err != nil {
				return err
			}
		}
		if a.ID == "chatgpt-direct" && tag.RowsAffected() == 1 {
			// The new model channel inherits precisely the existing subscription
			// visibility once. Subsequent explicit grant changes remain authoritative.
			if _, err := tx.Exec(ctx, "INSERT INTO record_grants(owner_id,record_id,principal_id) SELECT owner_id,record_id,'chatgpt-direct' FROM record_grants WHERE owner_id=$1 AND principal_id='chatgpt' ON CONFLICT DO NOTHING", string(scope.OwnerID)); err != nil {
				return err
			}
		}
	}
	// Existing enabled agents are initialized without reopening any grants.
	if _, err := tx.Exec(ctx, `UPDATE workspace_agents SET document=jsonb_set(document,'{memoryInitialized}','true')
        WHERE owner_id=$1 AND coalesce((document->>'enabled')::boolean,false)
        AND NOT coalesce((document->>'memoryInitialized')::boolean,false)`, string(scope.OwnerID)); err != nil {
		return err
	}
	return nil
}
func (s *Store) Snapshot(ctx context.Context, scope memory.Scope) (workspace.State, error) {
	var out workspace.State
	if err := requireOwner(scope); err != nil {
		return out, err
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.ensureOwner(ctx, tx, scope); err != nil {
			return err
		}
		var err error
		out, err = s.snapshotTx(ctx, tx, scope)
		return err
	})
	return out, err
}
func (s *Store) snapshotTx(ctx context.Context, tx pgx.Tx, scope memory.Scope) (workspace.State, error) {
	out := workspace.State{Version: 1, Tasks: []workspace.Item{}, Ideas: []workspace.Item{}, Projects: []workspace.Item{}, Sources: []workspace.Source{}, Jobs: []workspace.Job{}, ExcludedMemories: map[string][]string{}}
	var data []byte
	err := tx.QueryRow(ctx, "SELECT revision,settings FROM workspace_owners WHERE owner_id=$1 FOR SHARE", string(scope.OwnerID)).Scan(&out.Revision, &data)
	if err != nil {
		return out, err
	}
	if err = json.Unmarshal(data, &out.Settings); err != nil {
		return out, err
	}
	items, err := queryDocuments[workspace.Item](ctx, tx, "SELECT document FROM work_items WHERE owner_id=$1 ORDER BY updated_at DESC,id", string(scope.OwnerID))
	if err != nil {
		return out, err
	}
	for _, item := range items {
		switch item.Kind {
		case "task":
			out.Tasks = append(out.Tasks, item)
		case "idea":
			out.Ideas = append(out.Ideas, item)
		case "project":
			out.Projects = append(out.Projects, item)
		}
	}
	if out.Notices, err = queryDocuments[workspace.Notice](ctx, tx, `SELECT jsonb_build_object(
        'id',n.id,'thingId',n.thing_id,'title',w.title,'reason',n.reason,
        'dueAt',n.due_at,'createdAt',n.created_at) ||
        CASE WHEN n.trigger_id LIKE 'run:%' THEN '{"result":true}'::jsonb ELSE '{}'::jsonb END ||
        CASE WHEN n.dismissed_at IS NULL THEN '{}'::jsonb ELSE jsonb_build_object('dismissedAt',n.dismissed_at) END
        FROM workspace_notices n JOIN work_items w ON (w.owner_id,w.id)=(n.owner_id,n.thing_id)
        WHERE n.owner_id=$1 AND NOT (n.delivered @> '{"_suppressionOnly":true}'::jsonb) ORDER BY (n.dismissed_at IS NOT NULL),n.created_at DESC,n.id LIMIT 100`, string(scope.OwnerID)); err != nil {
		return out, err
	}
	if out.Memories, err = s.readMemoriesTx(ctx, tx, scope, false, memoryReadOptions{limit: 200}); err != nil {
		return out, err
	}
	where, args := memoryWhere(scope, false, memoryReadOptions{})
	if err := tx.QueryRow(ctx, "SELECT count(*)"+memoryJoins+where, args...).Scan(&out.MemoryTotal); err != nil {
		return out, err
	}
	if out.Agents, err = queryDocuments[workspace.Agent](ctx, tx, "SELECT document FROM workspace_agents WHERE owner_id=$1 ORDER BY id", string(scope.OwnerID)); err != nil {
		return out, err
	}
	for i := range out.Agents {
		a := &out.Agents[i]
		if a.Channel == "manual" {
			a.Available = true
		}
		if p, ok := s.models.Get(a.ID); ok {
			a.Default = a.ID == s.models.ExtractionID()
			a.Protocol = p.Protocol
			a.Available = s.models.Available(a.ID)
			a.InputPrice = p.InputPerMillion
			a.OutputPrice = p.OutputPerMillion
			a.MaxOutput = p.MaxOutput
			if p.Protocol == "codex" {
				a.Note = "ChatGPT 订阅 · " + p.Model
			} else if p.Protocol != "siwc" {
				a.Note = "通用 API · " + p.Model
			}
		}
	}
	if s.models != nil && s.models.ChatGPT != nil && s.models.ChatGPT.DefaultReady() {
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
	if out.Candidates, err = queryDocuments[workspace.Candidate](ctx, tx, "SELECT document FROM capture_candidates WHERE owner_id=$1 ORDER BY document->>'createdAt' DESC,id", string(scope.OwnerID)); err != nil {
		return out, err
	}
	if out.Docs, err = queryDocuments[workspace.Doc](ctx, tx, "SELECT document FROM work_documents WHERE owner_id=$1 ORDER BY document->>'updatedAt' DESC,id", string(scope.OwnerID)); err != nil {
		return out, err
	}
	if out.Runs, err = queryDocuments[workspace.Run](ctx, tx, "SELECT document FROM agent_runs WHERE owner_id=$1 ORDER BY created_at DESC,id", string(scope.OwnerID)); err != nil {
		return out, err
	}
	if out.Samples, err = queryDocuments[workspace.Sample](ctx, tx, "SELECT document || jsonb_build_object('stale',stale,'state',state) FROM training_samples WHERE owner_id=$1 ORDER BY document->>'createdAt' DESC,id", string(scope.OwnerID)); err != nil {
		return out, err
	}
	if out.Sources, err = sourceGroupsTx(ctx, tx, scope); err != nil {
		return out, err
	}
	rows, err := tx.Query(ctx, `SELECT j.id::text,j.stage,j.state,j.error_code,j.created_at,j.available_at,
		coalesce((SELECT v.title FROM source_versions v WHERE (v.owner_id,v.source_id,v.version)=(j.owner_id,j.record_id,j.record_version)),'')
		FROM memory_jobs j WHERE j.owner_id=$1 ORDER BY j.created_at DESC LIMIT 500`, string(scope.OwnerID))
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var j workspace.Job
		var at, next time.Time
		var stage, state, code, source string
		if err = rows.Scan(&j.ID, &stage, &state, &code, &at, &next, &source); err != nil {
			rows.Close()
			return out, err
		}
		j.Title = jobTitle(stage)
		j.Trigger = "资料处理"
		j.Status = map[string]string{"done": "done", "queued": "queued", "leased": "running", "blocked": "failed", "failed": "failed"}[state]
		var detail []string
		if source != "" {
			detail = append(detail, "「"+source+"」")
		}
		if code != "" {
			detail = append(detail, jobProblem(code))
		}
		j.Detail = strings.Join(detail, " · ")
		j.CreatedAt = at.Format(time.RFC3339Nano)
		if state == "queued" {
			j.NextRunAt = next.Format(time.RFC3339Nano)
		}
		if state == "blocked" {
			j.Recovery = "配置对应模型或处理器后重试"
			if oneOf(code, "model_call_failed", "model_output_invalid") {
				j.Recovery = "可以直接重试"
			}
		}
		out.Jobs = append(out.Jobs, j)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	notices, err := queryDocuments[workspace.Job](ctx, tx, `SELECT jsonb_build_object('id','notice:'||thing_id::text||':'||trigger_id||':'||due_at::text,'title','事项提醒','trigger','条件与时间','status','done','detail',reason,'createdAt',created_at) FROM workspace_notices WHERE owner_id=$1 AND NOT (delivered @> '{"_suppressionOnly":true}'::jsonb) ORDER BY created_at DESC LIMIT 100`, string(scope.OwnerID))
	if err != nil {
		return out, err
	}
	out.Jobs = append(out.Jobs, notices...)
	reviews, err := queryDocuments[workspace.Job](ctx, tx, `SELECT jsonb_build_object('id','review:'||due_at::text,'title','每日整理','trigger','定时检查','status','done','detail','有 '||pending_count::text||' 条拿不准的等你确认','createdAt',created_at) FROM workspace_reviews WHERE owner_id=$1 ORDER BY created_at DESC LIMIT 30`, string(scope.OwnerID))
	if err != nil {
		return out, err
	}
	out.Jobs = append(out.Jobs, reviews...)
	if out.Activity, err = activityTx(ctx, tx, scope, out.Settings.Timezone); err != nil {
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

func (s *Store) Execute(ctx context.Context, scope memory.Scope, in workspace.Command) (workspace.State, error) {
	var out workspace.State
	if err := requireOwner(scope); err != nil {
		return out, err
	}
	if !memory.ID(in.RequestID).Valid() || in.ExpectedRevision < 0 {
		return out, memory.ErrInvalid
	}
	hash := sha256.Sum256(asJSON(in))
	if oneOf(in.Type, "requestRun", "delegateTask") {
		if requireText(in.Prompt) != nil || in.Type == "delegateTask" && !memory.ID(in.ID).Valid() || in.Type == "requestRun" && !memory.ID(in.ThingID).Valid() {
			return out, memory.ErrInvalid
		}
		// Avoid a second embedding/budget reservation for a completed retry.
		var prior []byte
		err := s.pool.QueryRow(ctx, "SELECT request_hash FROM workspace_commands WHERE owner_id=$1 AND request_id=$2", string(scope.OwnerID), in.RequestID).Scan(&prior)
		if err == nil {
			if !bytes.Equal(prior, hash[:]) {
				return out, memory.ErrConflict
			}
			return s.Snapshot(ctx, scope)
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return out, err
		}
		if in.Type == "delegateTask" {
			var exists bool
			if err := s.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM work_items WHERE owner_id=$1 AND id=$2)", string(scope.OwnerID), in.ID).Scan(&exists); err != nil {
				return out, err
			}
			if exists {
				return out, memory.ErrConflict
			}
		}
		var errPrepare error
		ctx, errPrepare = s.prepareRunContext(ctx, scope, in)
		if errPrepare != nil {
			return out, errPrepare
		}
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.ensureOwner(ctx, tx, scope); err != nil {
			return err
		}
		var revision int64
		if err := tx.QueryRow(ctx, "SELECT revision FROM workspace_owners WHERE owner_id=$1 FOR UPDATE", string(scope.OwnerID)).Scan(&revision); err != nil {
			return err
		}
		var prior []byte
		err := tx.QueryRow(ctx, "SELECT request_hash FROM workspace_commands WHERE owner_id=$1 AND request_id=$2", string(scope.OwnerID), in.RequestID).Scan(&prior)
		if err == nil {
			if !bytes.Equal(prior, hash[:]) {
				return memory.ErrConflict
			}
			out, err = s.snapshotTx(ctx, tx, scope)
			return err
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		// Only toggles and bulk operations depend on the observed workspace
		// state. Unrelated worker commits must not block other commands;
		// their request IDs and command-specific guards still apply.
		if (oneOf(in.Type, "toggleCheck", "toggleTrigger", "toggleContextMemory") || strings.HasPrefix(in.Type, "bulk")) && revision != in.ExpectedRevision {
			return memory.ErrConflict
		}
		if undoableCommand(in.Type) {
			ctx = withActionLog(ctx, in.RequestID, "command", "", commandSummary(ctx, tx, scope, in))
			if err := beginActionLogTx(ctx, tx); err != nil {
				return err
			}
		}
		if err := s.commandTx(ctx, tx, scope, in); err != nil {
			return err
		}
		if err := flushActionLog(ctx, tx, scope); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, "UPDATE workspace_owners SET revision=revision+1 WHERE owner_id=$1", string(scope.OwnerID)); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, "INSERT INTO workspace_commands(owner_id,request_id,request_hash,revision) VALUES($1,$2,$3,$4)", string(scope.OwnerID), in.RequestID, hash[:], revision+1); err != nil {
			return err
		}
		out, err = s.snapshotTx(ctx, tx, scope)
		return err
	})
	return out, err
}
func (s *Store) Export(ctx context.Context, scope memory.Scope, training, confirmedOnly bool) ([]byte, error) {
	if err := requireOwner(scope); err != nil {
		return nil, err
	}
	var data []byte
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.ensureOwner(ctx, tx, scope); err != nil {
			return err
		}
		state, err := s.snapshotTx(ctx, tx, scope)
		if err != nil {
			return err
		}
		if !training {
			sources, err := queryDocuments[json.RawMessage](ctx, tx, `SELECT jsonb_build_object('id',s.id,'connector',s.connector,'external_id',s.external_id,'version',v.version,'external_version',v.external_version,'title',v.title,'text',v.body,'media_type',v.media_type,'expressed_at',rv.expressed_at,'recorded_at',rv.recorded_at)
			FROM sources s JOIN source_versions v ON (v.owner_id,v.source_id)=(s.owner_id,s.id) JOIN record_versions rv ON (rv.owner_id,rv.record_id,rv.version)=(v.owner_id,v.source_id,v.version) WHERE s.owner_id=$1 ORDER BY s.id,v.version`, string(scope.OwnerID))
			if err != nil {
				return err
			}
			canonical := map[string][]json.RawMessage{}
			// Names are a fixed server allowlist, never caller-supplied SQL.
			for _, table := range []string{"memory_records", "record_versions", "sources", "source_versions", "entities", "entity_versions", "aliases", "episodes", "episode_members", "claims", "claim_revisions", "evidence", "relations", "activity", "record_grants", "claim_source_keys", "claim_key_redirects", "claim_reimport_blocks", "reimport_blocks", "record_reimport_blocks", "source_contexts", "episode_keys", "archive_entries", "claim_mentions", "source_extractions", "import_batches", "model_usage"} {
				rows, err := queryDocuments[json.RawMessage](ctx, tx, "SELECT to_jsonb(t) FROM "+table+" t WHERE owner_id=$1", string(scope.OwnerID))
				if err != nil {
					return err
				}
				canonical[table] = rows
			}
			data = asJSON(map[string]any{"format": "pcas-export-1", "createdAt": stamp(), "state": state, "sourceVersions": sources, "canonical": canonical, "attachments": "Binary originals must be backed up from the memory-files volume."})
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
		_, err = tx.Exec(ctx, "INSERT INTO memory_export_manifests(owner_id,id,sample_versions) VALUES($1,$2,$3)", string(scope.OwnerID), string(memory.NewID()), asJSON(manifest))
		data = buf.Bytes()
		return err
	})
	return data, err
}

func newItem(kind, title string) workspace.Item {
	at := stamp()
	status := "active"
	if kind == "task" {
		status = "todo"
	}
	return workspace.Item{ID: string(memory.NewID()), Kind: kind, Version: 1, Title: title, Name: title, Status: status, DependsOn: []string{}, Checklist: []workspace.Check{}, Triggers: []workspace.Trigger{}, Sources: []workspace.SourceRef{}, History: []workspace.Revision{{At: at, By: "user", Summary: "创建"}}, Evolution: []workspace.Revision{{At: at, By: "user", Summary: "提出"}}, Conditions: []workspace.Condition{}, RemindersOn: true, NextSteps: []string{}, CreatedAt: at, UpdatedAt: at}
}
func getItem(ctx context.Context, tx pgx.Tx, scope memory.Scope, id string) (workspace.Item, error) {
	if !memory.ID(id).Valid() {
		return workspace.Item{}, memory.ErrInvalid
	}
	return queryDocument[workspace.Item](ctx, tx, "SELECT document FROM work_items WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), id)
}
func saveItem(ctx context.Context, tx pgx.Tx, scope memory.Scope, item workspace.Item) error {
	if err := syncArtifactEditsTx(ctx, tx, scope, item); err != nil {
		return err
	}
	if !memory.ID(item.ID).Valid() || requireText(item.Title) != nil || len(item.Title) > 2000 {
		return memory.ErrInvalid
	}
	if !oneOf(item.Kind, "task", "idea", "project") {
		return memory.ErrInvalid
	}
	if item.Kind == "task" && !oneOf(item.Status, "todo", "doing", "waiting", "done", "cancelled") || item.Kind == "idea" && !oneOf(item.Status, "active", "shelved", "awakened", "promoted", "dropped") || item.Kind == "project" && !oneOf(item.Status, "active", "paused", "done") {
		return memory.ErrInvalid
	}
	if item.ProjectID != "" {
		p, err := getItem(ctx, tx, scope, item.ProjectID)
		if err != nil {
			return err
		}
		if p.Kind != "project" || item.Kind == "project" {
			return memory.ErrInvalid
		}
	}
	for _, d := range []string{item.Due, item.Scheduled} {
		if d != "" {
			if _, err := time.Parse(time.RFC3339, d); err != nil {
				return memory.ErrInvalid
			}
		}
	}
	for _, id := range item.DependsOn {
		dependency, err := getItem(ctx, tx, scope, id)
		if err != nil {
			return err
		}
		if dependency.Kind != "task" || id == item.ID {
			return memory.ErrInvalid
		}
	}
	_, err := tx.Exec(ctx, `INSERT INTO work_items(owner_id,id,kind,title,status,project_id,due_at,scheduled_at,version,document,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		ON CONFLICT(owner_id,id) DO UPDATE SET title=excluded.title,status=excluded.status,project_id=excluded.project_id,due_at=excluded.due_at,scheduled_at=excluded.scheduled_at,version=excluded.version,document=excluded.document,updated_at=excluded.updated_at`, string(scope.OwnerID), item.ID, item.Kind, item.Title, item.Status, nullString(item.ProjectID), nullString(item.Due), nullString(item.Scheduled), item.Version, asJSON(item), item.CreatedAt, item.UpdatedAt)
	return err
}
func strictJSON(data []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(v) != nil {
		return memory.ErrInvalid
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return memory.ErrInvalid
	}
	return nil
}
func uuidOrNew(id string) (string, error) {
	if id == "" {
		return string(memory.NewID()), nil
	}
	if !memory.ID(id).Valid() {
		return "", memory.ErrInvalid
	}
	return id, nil
}
func wrapInvalid(field string) error { return fmt.Errorf("%s: %w", field, memory.ErrInvalid) }

// jobTitle and jobProblem put processing stages and error codes into the
// user's words; the raw names stay in the database and logs.
func jobTitle(stage string) string {
	switch strings.SplitN(stage, ":", 2)[0] {
	case "source.parse":
		return "读取附件"
	case "source.chunk":
		return "切分原文"
	case "source.extract":
		return "从资料里读出要点"
	case "source.embed", "memory.embed":
		return "建立语义索引"
	case "source.tokenize", "memory.index":
		return "建立检索索引"
	case "memory.summary":
		return "更新摘要"
	}
	return "后台整理"
}

func jobProblem(code string) string {
	switch code {
	case "provider_not_configured", "handler_not_configured":
		return "还没配置处理它的模型"
	case "provider_unavailable":
		return "模型通道暂时不可用；稍后会自动再试，若已停住请检查连接后点重试"
	case "budget_deferred":
		return "今天的预算已用完，明天会继续；需要现在处理可提高每日预算"
	case "archive_redacted", "source_unavailable":
		return "原文已经删除"
	case "attempts_exhausted", "processing_failed":
		return "试了几次没成功"
	case "model_call_failed":
		return "模型调用没成功"
	case "model_output_invalid":
		return "模型的输出没看懂"
	}
	return "没做完"
}

// activityTx is the home screen's "while you were away" log. Processing jobs
// are folded per day into sources finished and problems by cause, so one
// import reads as one line rather than a dozen stage names. Idea wakes and
// finished runs are already on their own records and are added by the client.
func activityTx(ctx context.Context, tx pgx.Tx, scope memory.Scope, timezone string) ([]workspace.Activity, error) {
	out := []workspace.Activity{}
	owner := string(scope.OwnerID)
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
		text := "整理好了「" + title + "」"
		if n > 1 {
			text = fmt.Sprintf("整理好了「%s」等 %d 份资料", title, n)
		}
		out = append(out, workspace.Activity{ID: "sources:" + day, At: at.UTC().Format(time.RFC3339Nano), Text: text, To: "/library?tab=sources"})
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
	// Several codes share one reason, so fold again after translating.
	latest := map[string]time.Time{}
	counts := map[string]int{}
	for rows.Next() {
		var code string
		var n int
		var at time.Time
		if err = rows.Scan(&code, &n, &at); err != nil {
			rows.Close()
			return nil, err
		}
		reason := jobProblem(code)
		counts[reason] += n
		if at.After(latest[reason]) {
			latest[reason] = at
		}
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	for reason, at := range latest {
		out = append(out, workspace.Activity{ID: "problem:" + reason, At: at.UTC().Format(time.RFC3339Nano), Text: fmt.Sprintf("有 %d 项整理没做完：%s", counts[reason], reason), To: "/library?tab=sources", Failed: true})
	}
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
		out = append(out, workspace.Activity{ID: "notice:" + thing + ":" + trigger + ":" + due.UTC().Format(time.RFC3339), At: at.UTC().Format(time.RFC3339Nano), Text: "提醒了你「" + title + "」：" + reason, To: "/t/" + thing})
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
		out = append(out, workspace.Activity{ID: "review:" + due.UTC().Format(time.RFC3339), At: at.UTC().Format(time.RFC3339Nano), Text: fmt.Sprintf("每日整理：有 %d 条拿不准的等你确认", n)})
	}
	rows.Close()
	return out, rows.Err()
}

// New enabled agents inherit only memories already shared with a deputy. Old
// enabled documents are marked initialized by ensureOwner without this call.
func initializeAgentMemoriesTx(ctx context.Context, tx pgx.Tx, owner memory.ID, agent string) error {
	_, err := tx.Exec(ctx, `INSERT INTO record_grants(owner_id,record_id,principal_id)
        SELECT r.owner_id,r.id,$2 FROM memory_records r JOIN claim_revisions c ON(c.owner_id,c.claim_id,c.version)=(r.owner_id,r.id,r.version)
        WHERE r.owner_id=$1 AND r.state='active' AND claim_source_is_current($1,c.claim_id,c.version,now())
        AND EXISTS(SELECT 1 FROM record_grants g JOIN workspace_agents ag ON(ag.owner_id,ag.id)=(g.owner_id,g.principal_id)
            WHERE g.owner_id=r.owner_id AND g.record_id=r.id)
        ON CONFLICT DO NOTHING`, string(owner), agent)
	return err
}
