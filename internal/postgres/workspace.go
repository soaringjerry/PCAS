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
	settings := workspace.Settings{WakeIdeas: true, FollowUps: true, DailyReviewAt: "09:00", DailyBudget: 10, Timezone: "Asia/Shanghai"}
	if _, err := tx.Exec(ctx, "INSERT INTO workspace_owners(owner_id,settings) VALUES($1,$2) ON CONFLICT DO NOTHING", string(scope.OwnerID), asJSON(settings)); err != nil {
		return err
	}
	agents := []workspace.Agent{{ID: "manual", Name: "手动交接", Channel: "manual", Enabled: true, MemoryKinds: []string{"fact", "preference", "decision", "intention", "plan"}, Note: "复制上下文到任意 AI，再粘贴结果"}}
	if s.models != nil {
		for _, p := range s.models.Config.Providers {
			if p.Embedding || p.Transcription {
				continue
			}
			note := "通用 API · " + p.Model
			if p.Protocol == "codex" {
				note = "ChatGPT 订阅 · 在设置中登录"
			}
			agents = append(agents, workspace.Agent{ID: p.ID, Name: p.Name, Channel: "api", Note: note, Enabled: true, MemoryKinds: []string{"fact", "preference", "decision", "intention", "plan"}})
		}
	}
	for _, a := range agents {
		if _, err := tx.Exec(ctx, "INSERT INTO workspace_agents(owner_id,id,document) VALUES($1,$2,$3) ON CONFLICT DO NOTHING", string(scope.OwnerID), a.ID, asJSON(a)); err != nil {
			return err
		}
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
	if out.Memories, err = s.memoriesTx(ctx, tx, scope); err != nil {
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
			a.Protocol = p.Protocol
			a.Available = s.models.Available(a.ID)
			a.InputPrice = p.InputPerMillion
			a.OutputPrice = p.OutputPerMillion
			a.MaxOutput = p.MaxOutput
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
	rows, err := tx.Query(ctx, `SELECT s.id::text,v.title,s.connector,r.updated_at,(SELECT count(*) FROM chunks c WHERE c.owner_id=s.owner_id AND c.source_id=s.id AND c.source_version=r.version),
		EXISTS(SELECT 1 FROM memory_jobs j WHERE j.owner_id=s.owner_id AND j.record_id=s.id AND j.record_version=r.version AND j.state IN ('failed','blocked')),
		EXISTS(SELECT 1 FROM memory_jobs j WHERE j.owner_id=s.owner_id AND j.record_id=s.id AND j.record_version=r.version AND j.state IN ('queued','leased'))
		FROM sources s JOIN memory_records r ON (r.owner_id,r.id)=(s.owner_id,s.id) JOIN source_versions v ON (v.owner_id,v.source_id,v.version)=(r.owner_id,r.id,r.version) WHERE s.owner_id=$1 AND r.state='active' ORDER BY r.updated_at DESC`, string(scope.OwnerID))
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var source workspace.Source
		var at time.Time
		var blocked, pending bool
		if err = rows.Scan(&source.ID, &source.Name, &source.Method, &at, &source.ItemCount, &blocked, &pending); err != nil {
			rows.Close()
			return out, err
		}
		source.Status = "connected"
		source.Note = "原文已保存"
		if pending {
			source.Status = "syncing"
			source.Note = "原文可读，索引处理中"
		}
		if blocked {
			source.Status = "failed"
			source.Note = "原文可读，部分处理未完成；展开后台任务查看缺口"
		}
		source.LastSyncAt = at.UTC().Format(time.RFC3339Nano)
		out.Sources = append(out.Sources, source)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	rows, err = tx.Query(ctx, `SELECT id::text,stage,state,error_code,created_at,available_at FROM memory_jobs WHERE owner_id=$1 ORDER BY created_at DESC LIMIT 500`, string(scope.OwnerID))
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var j workspace.Job
		var at, next time.Time
		var stage, state, code string
		if err = rows.Scan(&j.ID, &stage, &state, &code, &at, &next); err != nil {
			rows.Close()
			return out, err
		}
		j.Title = stage
		j.Trigger = "资料处理"
		j.Status = map[string]string{"done": "done", "queued": "queued", "leased": "running", "blocked": "failed", "failed": "failed"}[state]
		j.Detail = code
		j.CreatedAt = at.Format(time.RFC3339Nano)
		if state == "queued" {
			j.NextRunAt = next.Format(time.RFC3339Nano)
		}
		if state == "blocked" {
			j.Recovery = "配置对应模型或处理器后重试"
		}
		out.Jobs = append(out.Jobs, j)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	notices, err := queryDocuments[workspace.Job](ctx, tx, `SELECT jsonb_build_object('id','notice:'||thing_id::text||':'||trigger_id||':'||due_at::text,'title','事项提醒','trigger','条件与时间','status','done','detail',reason,'createdAt',created_at) FROM workspace_notices WHERE owner_id=$1 ORDER BY created_at DESC LIMIT 100`, string(scope.OwnerID))
	if err != nil {
		return out, err
	}
	out.Jobs = append(out.Jobs, notices...)
	reviews, err := queryDocuments[workspace.Job](ctx, tx, `SELECT jsonb_build_object('id','review:'||due_at::text,'title','每日整理','trigger','定时检查','status','done','detail','待确认线索：'||pending_count::text||' 条；在需要你确认中处理','createdAt',created_at) FROM workspace_reviews WHERE owner_id=$1 ORDER BY created_at DESC LIMIT 30`, string(scope.OwnerID))
	if err != nil {
		return out, err
	}
	out.Jobs = append(out.Jobs, reviews...)
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
		if revision != in.ExpectedRevision {
			return memory.ErrConflict
		}
		if err := s.commandTx(ctx, tx, scope, in); err != nil {
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
			for _, table := range []string{"memory_records", "record_versions", "sources", "source_versions", "entities", "entity_versions", "aliases", "episodes", "episode_members", "claims", "claim_revisions", "evidence", "relations", "activity", "record_grants", "claim_source_keys", "claim_key_redirects", "claim_reimport_blocks", "reimport_blocks", "record_reimport_blocks", "source_contexts", "episode_keys", "archive_entries"} {
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
