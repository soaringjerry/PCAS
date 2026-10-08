//go:build phase35_browser

package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/worker"
)

// Only controlled model responses and explicit user input injection are test
// boundaries. All /v1 routes are the real full Store HTTP service.
func TestPhase35BrowserOwnedServer(t *testing.T) {
	path := os.Getenv("PCAS_PHASE35_BROWSER_MANIFEST")
	if path == "" {
		t.Skip("run web/tests/phase3_5_browser_run.mjs")
	}
	if !strings.HasPrefix(filepath.Clean(path), "/tmp/phase3_5-browser-") {
		t.Fatal("manifest must belong to owned runner temp directory")
	}
	f := phase35Load(t)
	s := f.Store
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	f.Context = ctx
	phase26Exec(t, f.phase26LoadedFixture, `UPDATE claims SET organized=$2 WHERE owner_id=$1`, f.Scope.OwnerID, OrganizeVersion)
	phase26Exec(t, f.phase26LoadedFixture, `UPDATE memory_jobs SET available_at=now()+interval '1 day' WHERE owner_id=$1`, f.Scope.OwnerID)
	now := time.Now().UTC()
	// Chinese civil week starts Monday; 下周三 is Wednesday in the next week.
	delta := 9 - (int(now.Weekday())+6)%7
	day := time.Date(now.Year(), now.Month(), now.Day()+delta, 15, 0, 0, 0, time.UTC)
	appointmentText := "下周三下午和导师过虚构青岚方案"
	taskText := "我要给虚构青岚书店寄陶瓷标本"
	phase35Model(t, s, func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []struct{ Role, Content string }
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		system, prompt := "", ""
		for _, m := range req.Messages {
			if m.Role == "system" {
				system = m.Content
			}
			if m.Role == "user" {
				prompt = m.Content
			}
		}
		switch {
		case strings.Contains(system, "每条记忆标类型"):
			var in struct {
				Memories []struct {
					N    int
					Text string
				}
			}
			_ = json.Unmarshal([]byte(prompt), &in)
			items := []any{}
			for _, m := range in.Memories {
				ds := []any{}
				if strings.Contains(m.Text, appointmentText) {
					ds = append(ds, map[string]any{"kind": "appointment", "at": day.Format(time.RFC3339), "recurrence": "", "title": "和导师过虚构青岚方案", "timeNote": "下午15点"})
				}
				items = append(items, map[string]any{"n": m.N, "category": "event", "durable": true, "project": "", "topics": []string{}, "area": "", "unrestricted": false, "scope": "", "deadlines": ds})
			}
			secretaryModelReply(w, map[string]any{"items": items, "new": []any{}})
		case strings.Contains(system, "提取独立线索"):
			var in struct{ Source string }
			_ = json.Unmarshal([]byte(prompt), &in)
			it := phase35Direct(in.Source, "memory")
			it.Nature = "plan"
			if strings.Contains(in.Source, taskText) {
				it = phase35Direct(taskText, "task")
			}
			secretaryModelReply(w, extracted{Items: []extractedItem{it}})
		default:
			actions := []any{}
			reply := "记下了虚构预约，会出现在当天。"
			remember := true
			if strings.Contains(prompt, "虚构青岚多步目标") {
				remember = false
				reply = "建了项目「虚构青岚展览」，步骤归入其中。"
				actions = []any{map[string]any{"op": "create_task", "title": "虚构征集陶瓷标本", "project": "new:虚构青岚展览"}, map[string]any{"op": "create_task", "title": "虚构排出展览方案", "project": "new:虚构青岚展览"}}
			}
			secretaryModelReply(w, map[string]any{"reply": reply, "actions": actions, "used": []any{}, "links": []any{}, "show": []any{}, "remember": remember, "missingKeyInfo": false, "ask": nil, "memoryPlan": map[string]any{"depth": "light", "groups": []any{}}})
		}
	})
	// Ensure the browser's default agent selects this controlled provider.
	phase26Exec(t, f.phase26LoadedFixture, `UPDATE workspace_agents SET document=document||jsonb_build_object('default',false) WHERE owner_id=$1`, f.Scope.OwnerID)
	phase26Exec(t, f.phase26LoadedFixture, `INSERT INTO workspace_agents(owner_id,id,document) VALUES($1,'model',$2) ON CONFLICT(owner_id,id) DO UPDATE SET document=excluded.document`, f.Scope.OwnerID, asJSON(map[string]any{"id": "model", "name": "虚构青岚验收", "channel": "api", "enabled": true, "available": true, "default": true, "memoryKinds": []string{"fact", "plan", "intention"}, "includeInferred": false, "maxOutput": 8192}))
	h := phase35HTTP(t, s, f.Scope)
	finish := make(chan struct{})
	var once sync.Once
	key := string(memory.NewID())
	completionState := func() (map[string]string, error) {
		result := map[string]string{}
		var target string
		err := s.pool.QueryRow(ctx, `SELECT md5(to_jsonb(r)::text||to_jsonb(cl)::text||to_jsonb(cr)::text) FROM memory_records r JOIN claims cl ON(cl.owner_id,cl.id)=(r.owner_id,r.id) JOIN claim_revisions cr ON(cr.owner_id,cr.claim_id,cr.version)=(r.owner_id,r.id,r.version) WHERE r.owner_id=$1 AND r.id=$2`, f.Scope.OwnerID, f.Deadlines[1].Claim).Scan(&target)
		if err != nil {
			return result, err
		}
		result["memoryDigest"] = target
		err = s.pool.QueryRow(ctx, `SELECT md5(string_agg(to_jsonb(v)::text,'|' ORDER BY v.source_id,v.version)) FROM source_versions v WHERE owner_id=$1`, f.Scope.OwnerID).Scan(&target)
		result["originalSourcesDigest"] = target
		return result, err
	}
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/phase35-finish" {
			if r.URL.Query().Get("key") != key {
				w.WriteHeader(403)
				return
			}
			once.Do(func() { close(finish) })
			w.WriteHeader(204)
			return
		}
		if r.URL.Path == "/phase35-completion-state" && r.Method == "GET" {
			state, err := completionState()
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			_ = json.NewEncoder(w).Encode(state)
			return
		}

		if r.URL.Path == "/phase35-current-chat" && r.Method == "POST" {
			src, err := s.Ingest(ctx, f.Scope, memory.IngestRequest{Connector: "capture", ExternalID: string(memory.NewID()), ExternalVersion: "1", Title: "虚构当前聊天接入", Text: taskText})
			if err == nil {
				err = s.ProcessExtraction(ctx, phase35BrowserLease(ctx, s, f.Scope, src.Ref, "source.extract"))
			}
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			w.WriteHeader(204)
			return
		}
		if r.URL.Path == "/phase35-drain" && r.Method == "POST" {
			err := phase35BrowserDrain(ctx, s, f.Scope)
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			w.WriteHeader(204)
			return
		}
		// Forward locally to the complete real service. No fabricated /v1 response.
		req, err := http.NewRequestWithContext(r.Context(), r.Method, h.Base+r.URL.RequestURI(), r.Body)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		req.Header = r.Header.Clone()
		req.Header.Set("Authorization", "Bearer phase35-browser-fiction")
		response, err := h.Client.Do(req)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer response.Body.Close()
		for k, v := range response.Header {
			w.Header()[k] = v
		}
		w.WriteHeader(response.StatusCode)
		_, _ = io.Copy(w, response.Body)
	}))
	defer control.Close()
	manifest := map[string]any{"ownedDisposable": true, "backendURL": control.URL, "finishKey": key, "appointmentText": appointmentText, "appointmentAt": day.Format(time.RFC3339), "appointmentDay": day.Format("2006-01-02"), "taskText": taskText, "overdueTitle": "虚构期限 " + f.Deadlines[1].ID, "overdueId": f.Deadlines[1].ID}
	raw, _ := json.Marshal(manifest)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	select {
	case <-finish:
	case <-ctx.Done():
		t.Error("owned browser controller timed out")
	}
}
func phase35BrowserLease(ctx context.Context, s *Store, scope memory.Scope, ref memory.Ref, stage string) worker.Job {
	j := worker.Job{OwnerID: scope.OwnerID, Record: ref, Stage: stage, LeaseToken: memory.NewID(), Attempts: 1}
	// An invalid lease produces a normal processor error; never calls Fatal from
	// an HTTP handler goroutine. The runner asserts every control response.
	_ = s.pool.QueryRow(ctx, `INSERT INTO memory_jobs(id,owner_id,record_id,record_version,stage,state,attempts,lease_token,lease_until) VALUES(gen_random_uuid(),$1,$2,$3,$4,'leased',1,$5,now()+interval '5 minutes') ON CONFLICT(owner_id,record_id,record_version,stage) DO UPDATE SET state='leased',attempts=1,lease_token=$5,lease_until=now()+interval '5 minutes' RETURNING id::text`, scope.OwnerID, ref.ID, ref.Version, stage, j.LeaseToken).Scan(&j.ID)
	return j
}
func phase35BrowserDrain(ctx context.Context, s *Store, scope memory.Scope) error {
	rows, err := s.pool.Query(ctx, `SELECT j.record_id::text,j.record_version,j.stage FROM memory_jobs j JOIN sources so ON(so.owner_id,so.id)=(j.owner_id,j.record_id) WHERE j.owner_id=$1 AND j.stage='source.extract' AND j.state='queued' AND so.connector='desk'`, scope.OwnerID)
	if err != nil {
		return err
	}
	jobs := []worker.Job{}
	for rows.Next() {
		j := worker.Job{Record: memory.Ref{Kind: memory.SourceKind}}
		if err := rows.Scan(&j.Record.ID, &j.Record.Version, &j.Stage); err != nil {
			rows.Close()
			return err
		}
		jobs = append(jobs, j)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, j := range jobs {
		if err := s.ProcessExtraction(ctx, phase35BrowserLease(ctx, s, scope, j.Record, j.Stage)); err != nil {
			return err
		}
	}
	if _, err := s.ScheduleOrganize(ctx, time.Now()); err != nil {
		return err
	}
	rows, err = s.pool.Query(ctx, `SELECT record_id::text,record_version,stage FROM memory_jobs WHERE owner_id=$1 AND stage LIKE 'memory.organize:%' AND state='queued' AND available_at<=now()`, scope.OwnerID)
	if err != nil {
		return err
	}
	jobs = nil
	for rows.Next() {
		j := worker.Job{Record: memory.Ref{Kind: memory.ClaimKind}}
		if err := rows.Scan(&j.Record.ID, &j.Record.Version, &j.Stage); err != nil {
			rows.Close()
			return err
		}
		jobs = append(jobs, j)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, j := range jobs {
		if err := s.ProcessOrganize(ctx, phase35BrowserLease(ctx, s, scope, j.Record, j.Stage)); err != nil {
			return fmt.Errorf("organize: %w", err)
		}
	}
	return nil
}
