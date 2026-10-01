package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/httpapi"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func secretaryModel(t *testing.T, s *Store, handler http.HandlerFunc) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	s.SetModels(&ai.Registry{HTTP: server.Client(), Config: ai.Configuration{Extraction: "model", Providers: []ai.Provider{{ID: "model", Name: "测试秘书", Protocol: "openai", BaseURL: server.URL, Model: "test", MaxOutput: 100, CostMode: "free"}}}})
}
func secretaryModelReply(w http.ResponseWriter, value any) {
	content := string(asJSON(value))
	if text, ok := value.(string); ok {
		content = text
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": content}}}, "usage": map[string]int{"prompt_tokens": 100, "completion_tokens": 10}})
}
func turnRequest(text string) workspace.DeskTurnRequest {
	return workspace.DeskTurnRequest{RequestID: string(memory.NewID()), AgentID: "model", Text: text}
}
func mustTurn(t *testing.T, s *Store, scope memory.Scope, req workspace.DeskTurnRequest) workspace.DeskTurnResponse {
	t.Helper()
	out, err := s.DeskTurn(context.Background(), scope, req)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func TestSecretarySchedulesAndUpdatesRecentTask(t *testing.T) {
	s := testStore(t)
	scope := owner()
	var calls atomic.Int32
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			secretaryModelReply(w, `{"reply":"安排好了。","actions":[{"op":"create_task","title":"给张三回邮件","due":"2026-10-02T15:00","project":"P1","remind":null},{"op":"create_task","title":"买牛奶"}]}`)
		} else {
			secretaryModelReply(w, `{"reply":"改好了。","actions":[{"op":"update","ref":"R1","set":{"due":"2026-10-05T10:00"}}]}`)
		}
	})
	workspaceCommand(t, s, scope, workspace.Command{Type: "updateSettings", Patch: asJSON(map[string]string{"timezone": "Asia/Shanghai"})})
	st := workspaceCommand(t, s, scope, workspace.Command{Type: "addProject", Name: "A"})
	project := st.Projects[0].ID
	req := turnRequest("周五下午三点给张三回邮件，算在 A 项目里；还有买牛奶")
	out := mustTurn(t, s, scope, req)
	if out.Turn.Reply != "安排好了。" || len(out.State.Tasks) != 2 || len(out.Turn.Receipts) != 2 {
		t.Fatal(out)
	}
	receipt := out.Turn.Receipts[0]
	other := out.Turn.Receipts[1]
	if !receipt.Undoable || receipt.ActionID == nil || other.ActionID == nil || *receipt.ActionID == *other.ActionID {
		t.Fatal(out.Turn.Receipts)
	}
	var task workspace.Item
	for _, item := range out.State.Tasks {
		if item.Title == "给张三回邮件" {
			task = item
		}
	}
	if task.Due != "2026-10-02T07:00:00Z" || task.ProjectID != project || len(task.Triggers) != 1 || task.Triggers[0].NextAt != "2026-10-02T06:30:00Z" || task.Triggers[0].Offset != "-30m" {
		t.Fatal(task)
	}
	if !strings.Contains(receipt.Text, "15:00 给张三回邮件 · A 项目 · 14:30 提醒") {
		t.Fatal(receipt.Text)
	}
	again := mustTurn(t, s, scope, req)
	if calls.Load() != 1 || len(again.State.Tasks) != 2 || again.Turn.ID != out.Turn.ID {
		t.Fatal("idempotency", again, calls.Load())
	}
	req.Text = "换了内容"
	if _, err := s.DeskTurn(context.Background(), scope, req); !errors.Is(err, memory.ErrConflict) {
		t.Fatal(err)
	}
	second := turnRequest("改到周一十点")
	second.ConversationID = &out.ConversationID
	updated := mustTurn(t, s, scope, second)
	if updated.Turn.Receipts[0].ThingID == nil || *updated.Turn.Receipts[0].ThingID != task.ID {
		t.Fatal("R1 targeted another item", updated.Turn.Receipts)
	}
	for _, item := range updated.State.Tasks {
		if item.ID == task.ID {
			if item.Due != "2026-10-05T02:00:00Z" || item.Triggers[0].NextAt != "2026-10-05T01:30:00Z" {
				t.Fatal(item)
			}
		}
	}
	restored, err := s.Undo(context.Background(), scope, *updated.Turn.Receipts[0].ActionID)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range restored.Tasks {
		if item.ID == task.ID && item.Due != "2026-10-02T07:00:00Z" {
			t.Fatal(item)
		}
	}
	turns, err := s.DeskTurns(context.Background(), scope, out.ConversationID)
	if err != nil || len(turns.Turns) != 2 || turns.Turns[0].ID != out.Turn.ID || turns.Turns[1].ID != updated.Turn.ID {
		t.Fatal(err, turns)
	}
	var candidates int
	if err = s.pool.QueryRow(context.Background(), "SELECT count(*) FROM capture_candidates WHERE owner_id=$1", string(scope.OwnerID)).Scan(&candidates); err != nil || candidates != 0 {
		t.Fatal("desk created candidates", err, candidates)
	}
}
func TestSecretaryRejectsUntrustedRefsAndLimitsActions(t *testing.T) {
	s := testStore(t)
	scope := owner()
	var payload secretaryOutput
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) { secretaryModelReply(w, payload) })
	st := workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "原事项"})
	id := st.Tasks[0].ID
	payload = secretaryOutput{Actions: []secretaryAction{{Op: "update", Ref: "T999", Set: map[string]json.RawMessage{"title": asJSON("伪造")}}, {Op: "update", Ref: id, Set: map[string]json.RawMessage{"title": asJSON("真实 UUID")}}, {Op: "update", Ref: "T1", Set: map[string]json.RawMessage{"status": asJSON("evil"), "notesAppend": asJSON("保留正常字段")}}, {Op: "create_task", Title: "新任务", Due: stringPointer("bad-date")}}}
	out := mustTurn(t, s, scope, turnRequest("改事项"))
	if len(out.State.Tasks) != 2 || out.Turn.Receipts[0].Status != "skipped" || out.Turn.Receipts[1].Status != "skipped" {
		t.Fatal(out)
	}
	for _, item := range out.State.Tasks {
		if item.ID == id && (item.Status != "todo" || item.Notes != "保留正常字段" || item.Title != "原事项") {
			t.Fatal(item)
		}
	}
	if !strings.Contains(out.Turn.Receipts[3].Text, "时间没看懂") {
		t.Fatal(out.Turn.Receipts)
	}
	payload = secretaryOutput{}
	for i := 0; i < 11; i++ {
		payload.Actions = append(payload.Actions, secretaryAction{Op: "create_task", Title: "待办"})
	}
	out = mustTurn(t, s, scope, turnRequest("建十一件事"))
	if len(out.State.Tasks) != 12 || len(out.Turn.Receipts) != 11 || out.Turn.Receipts[10].Reason != "一次太多了，只做了前 10 件" {
		t.Fatal(out)
	}
}
func TestSecretaryFallbackPersistsOriginal(t *testing.T) {
	for _, mode := range []string{"500", "manual", "missing", "budget"} {
		t.Run(mode, func(t *testing.T) {
			s := testStore(t)
			logs := secretaryLogs(t)
			scope := owner()
			var calls atomic.Int32
			secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if mode == "500" {
					http.Error(w, "offline", 500)
				} else {
					secretaryModelReply(w, `{"actions":[{"op":"create_task","title":"不该创建"}]}`)
				}
			})
			req := turnRequest("原话不能丢")
			if mode == "manual" {
				req.AgentID = "manual"
			}
			if mode == "missing" {
				s.SetModels(nil)
			}
			if mode == "budget" {
				s.models.Config.Providers[0].InputPerMillion = 1
				s.models.Config.Providers[0].CostMode = ""
				workspaceCommand(t, s, scope, workspace.Command{Type: "updateSettings", Patch: asJSON(map[string]any{"dailyBudget": 0})})
			}
			out := mustTurn(t, s, scope, req)
			if out.Turn.Reply != "" || len(out.Turn.Cards) != 0 || len(out.Turn.Receipts) != 1 || out.Turn.Receipts[0].Op != "capture" || out.Turn.Receipts[0].ActionID != nil || out.Turn.Receipts[0].Undoable || len(out.State.Tasks) != 0 {
				t.Fatal(out)
			}
			stage, errorType := "context", "unavailable"
			reason := "没有可用的模型"
			if mode == "500" {
				stage, errorType, reason = "model", "model_error", "模型没有响应"
			} else if mode == "budget" {
				stage, errorType, reason = "budget", "budget_exceeded", "超过今天的额度"
			}
			if out.Turn.Receipts[0].Text != "已记下原话；"+reason+"，稍后会自动整理" {
				t.Fatal(out.Turn.Receipts)
			}
			assertSecretaryLog(t, logs, "WARN", stage, errorType)
			var text string
			if err := s.pool.QueryRow(context.Background(), "SELECT body FROM source_versions v JOIN sources s ON (s.owner_id,s.id)=(v.owner_id,v.source_id) WHERE s.owner_id=$1 AND s.connector='capture' AND s.external_id=$2", string(scope.OwnerID), req.RequestID).Scan(&text); err != nil || text != req.Text {
				t.Fatal(err, text)
			}
			replay := mustTurn(t, s, scope, req)
			if replay.Turn.ID != out.Turn.ID {
				t.Fatal("fallback not idempotent")
			}
			if oneOf(mode, "manual", "missing", "budget") && calls.Load() != 0 {
				t.Fatal("called unavailable/over-budget model")
			}
		})
	}
}
func TestSecretaryStablePrefixAndVisibility(t *testing.T) {
	s := testStore(t)
	scope := owner()
	var prompts, systems []string
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		for _, m := range body.Messages {
			if m.Role == "system" {
				systems = append(systems, m.Content)
			}
			if m.Role == "user" {
				prompts = append(prompts, m.Content)
			}
		}
		secretaryModelReply(w, `{"reply":"好的","used":[],"actions":[]}`)
	})
	st := workspaceCommand(t, s, scope, workspace.Command{Type: "capture", Text: "暗号 hunter2"})
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "acceptCandidate", ID: st.Candidates[0].ID, Kind: "memory", MemoryKind: "fact", Text: "暗号 hunter2"})
	workspaceCommand(t, s, scope, workspace.Command{Type: "setMemoryVisibility", ID: st.Memories[0].ID, AgentIDs: []string{}})
	workspaceCommand(t, s, scope, workspace.Command{Type: "addProject", Name: "A"})
	workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "整理发票"})
	workspaceCommand(t, s, scope, workspace.Command{Type: "addIdea", Title: "旅行"})
	out := mustTurn(t, s, scope, turnRequest("暗号是什么"))
	req := turnRequest("那今天有什么事")
	req.ConversationID = &out.ConversationID
	mustTurn(t, s, scope, req)
	if len(systems) != 2 || systems[0] != systems[1] || systems[0] != secretaryInstructions {
		t.Fatal("system changed")
	}
	prefix := func(p string) string { return strings.Split(p, "本对话历史：")[0] }
	if len(prompts) != 2 || prefix(prompts[0]) != prefix(prompts[1]) {
		t.Fatal("unstable cache prefix", prompts)
	}
	for _, p := range prompts {
		if strings.Contains(p, "hunter2") || deskUUID.MatchString(p) || !strings.Contains(p, "整理发票") {
			t.Fatal("prompt leaked or missing context", p)
		}
	}
}
func TestSecretaryMemoryCardsAndRevokedHistory(t *testing.T) {
	s := testStore(t)
	scope := owner()
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct{ Role, Content string }
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		found := false
		for _, message := range body.Messages {
			if message.Role == "system" {
				found = strings.Contains(message.Content, "timeline、tasks") && strings.Contains(message.Content, "reply 只写一句结论（40 字以内）") && strings.Contains(message.Content, "不要重复列举卡片内容")
			}
		}
		if !found {
			t.Error("model did not receive the concise card-reply instruction")
		}
		secretaryModelReply(w, `{"reply":"成都的两条记录。","used":["M1","M2","M999"],"show":["T1","nope"],"links":["https://example.com/a","javascript:bad","https://u:p@example.com"],"actions":[]}`)
	})
	var ids []string
	for _, text := range []string{"去年关于成都的计划", "今年关于成都的安排"} {
		st := workspaceCommand(t, s, scope, workspace.Command{Type: "capture", Text: text})
		st = workspaceCommand(t, s, scope, workspace.Command{Type: "acceptCandidate", ID: st.Candidates[0].ID, Kind: "memory", MemoryKind: "fact", Text: text})
		for _, m := range st.Memories {
			if m.Text == text {
				ids = append(ids, m.ID)
			}
		}
	}
	workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "去成都"})
	out := mustTurn(t, s, scope, turnRequest("关于成都的计划和安排"))
	kinds := map[string]bool{}
	for _, card := range out.Turn.Cards {
		kinds[card.Kind] = true
	}
	if !kinds["sources"] || !kinds["links"] || !kinds["timeline"] || !kinds["tasks"] {
		t.Fatal(out.Turn.Cards)
	}
	if out.Turn.Reply != "成都的两条记录。" || utf8.RuneCountInString(out.Turn.Reply) > 40 {
		t.Fatal("card reply is not a concise conclusion", out.Turn.Reply)
	}
	for _, card := range out.Turn.Cards {
		if card.Kind == "sources" {
			items := card.Items.([]workspace.DeskSourceItem)
			if len(items) != 2 || items[0].SourceID == "" || items[0].SourceVersion < 1 {
				t.Fatal(items)
			}
		}
		if card.Kind == "links" && len(card.Items.([]workspace.DeskLinkItem)) != 1 {
			t.Fatal(card)
		}
	}
	workspaceCommand(t, s, scope, workspace.Command{Type: "deleteMemory", ID: ids[0]})
	turns, err := s.DeskTurns(context.Background(), scope, out.ConversationID)
	if err != nil || len(turns.Turns) != 1 || turns.Turns[0].Reply != "（这条回答依据的记忆已变更）" || len(turns.Turns[0].Cards) != 0 {
		t.Fatal(err, turns)
	}
	other, err := s.DeskTurns(context.Background(), owner(), out.ConversationID)
	if err != nil || len(other.Turns) != 0 {
		t.Fatal("cross owner history", err, other)
	}
}
func TestDeskExtractionSkipsTaskAndIdea(t *testing.T) {
	s := testStore(t)
	scope := owner()
	ctx := context.Background()
	text := "明天寄信，也想做个工具，偏好清晨工作"
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		secretaryModelReply(w, extracted{Items: []extractedItem{{Kind: "task", Text: "明天寄信", Quote: "明天寄信", Confidence: 1}, {Kind: "idea", Text: "做个工具", Quote: "做个工具", Confidence: 1}, {Kind: "memory", Nature: "preference", Text: "偏好清晨工作", Quote: "偏好清晨工作", Subject: "用户", Predicate: "工作时间", Acquisition: "direct", Confidence: 1}}})
	})
	source := mustIngest(t, s, scope, memory.IngestRequest{Connector: "desk", ExternalID: string(memory.NewID()), ExternalVersion: "1", Title: "秘书原话", Text: text, MediaType: "text/plain"})
	if err := s.ProcessExtraction(ctx, leaseStage(t, s, scope, source.Ref, "source.extract")); err != nil {
		t.Fatal(err)
	}
	st, err := s.Snapshot(ctx, scope)
	if err != nil || len(st.Memories) != 1 {
		t.Fatal(err, st.Memories)
	}
	for _, c := range st.Candidates {
		if oneOf(c.Kind, "task", "idea") {
			t.Fatal("duplicate desk candidate", c)
		}
	}
}
func TestSecretaryConcurrentRetryAndHTTPContract(t *testing.T) {
	s := testStore(t)
	scope := owner()
	var calls atomic.Int32
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		time.Sleep(20 * time.Millisecond)
		secretaryModelReply(w, `{"actions":[{"op":"create_task","title":"一件事"}]}`)
	})
	req := turnRequest("一件事")
	req.AgentID = "" // default agent also works for internal callers
	var wg sync.WaitGroup
	responses := make(chan workspace.DeskTurnResponse, 2)
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, err := s.DeskTurn(context.Background(), scope, req)
			responses <- out
			errs <- err
		}()
	}
	wg.Wait()
	close(responses)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var out workspace.DeskTurnResponse
	for response := range responses {
		if out.Turn.ID != "" && out.Turn.ID != response.Turn.ID {
			t.Fatal("duplicate turn")
		}
		out = response
	}
	if calls.Load() != 1 || len(out.State.Tasks) != 1 {
		t.Fatal(calls.Load(), out)
	}
	token := strings.Repeat("b", 64)
	api := httpapi.New(s, s, httpapi.NewOwnerToken(token, scope.OwnerID), func(context.Context) error { return nil }, slog.New(slog.NewTextHandler(io.Discard, nil)), httpapi.Options{Workspace: s})
	send := func(method, path string, body any) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, path, strings.NewReader(string(asJSON(body))))
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		api.ServeHTTP(w, request)
		return w
	}
	w := send("POST", "/v1/desk/turn", req)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var wire map[string]any
	if json.Unmarshal(w.Body.Bytes(), &wire) != nil {
		t.Fatal(w.Body.String())
	}
	turn := wire["turn"].(map[string]any)
	if turn["ask"] != nil || turn["cards"] == nil || turn["receipts"] == nil || turn["agent"] != "测试秘书" {
		t.Fatal(turn)
	}
	w = send("GET", "/v1/desk/turns?conversationId="+out.ConversationID, nil)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	req.Text = "different"
	w = send("POST", "/v1/desk/turn", req)
	if w.Code != 409 || !strings.Contains(w.Body.String(), `"error":"version_conflict"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	req = turnRequest(strings.Repeat("中", 4001))
	w = send("POST", "/v1/desk/turn", req)
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
	// All undo errors keep their exact HTTP status/code.
	action := *out.Turn.Receipts[0].ActionID
	undo := workspace.Command{Type: "undoAction", ID: action, RequestID: string(memory.NewID()), ExpectedRevision: out.State.Revision}
	w = send("POST", "/v1/workspace/commands", undo)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var state workspace.State
	if err := json.Unmarshal(w.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	undo.RequestID = string(memory.NewID())
	undo.ExpectedRevision = state.Revision
	w = send("POST", "/v1/workspace/commands", undo)
	if w.Code != 409 || !strings.Contains(w.Body.String(), `"error":"already_undone"`) {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestSecretaryActionMappingAndDateDefaults(t *testing.T) {
	s := testStore(t)
	scope := owner()
	workspaceCommand(t, s, scope, workspace.Command{Type: "updateSettings", Patch: asJSON(map[string]string{"timezone": "Asia/Shanghai"})})
	var payload string
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) { secretaryModelReply(w, payload) })
	st := workspaceCommand(t, s, scope, workspace.Command{Type: "addProject", Name: "A"})
	project := st.Projects[0].ID
	payload = `{"reply":"办好了","remember":true,"ask":{"question":"还有补充吗？","options":["没有"]},"actions":[{"op":"create_task","title":"寄信","due":"2026-10-04","project":"new: a ","notes":"信封","owedTo":"朋友","waitingFor":"地址"},{"op":"create_idea","title":"小工具","condition":"等成本降下来","conditionDue":"2026-10-10"},{"op":"create_project","name":"B"}]}`
	out := mustTurn(t, s, scope, turnRequest("寄信，还想做个小工具，新建 B 项目，记住我的偏好"))
	if len(out.State.Projects) != 2 || len(out.State.Ideas) != 1 || len(out.State.Tasks) != 1 || out.Turn.Ask == nil || len(out.Turn.Receipts) != 4 {
		t.Fatal(out)
	}
	item := out.State.Tasks[0]
	if item.ProjectID != project || item.Due != "2026-10-04T15:59:00Z" || item.Triggers[0].NextAt != "2026-10-04T01:00:00Z" || item.Triggers[0].Offset != "09:00" || item.Notes != "信封" || item.OwedTo == nil || item.OwedTo.Who != "朋友" || item.WaitingFor != "地址" {
		t.Fatal(item)
	}
	if len(out.State.Ideas[0].Conditions) != 1 || out.State.Ideas[0].Conditions[0].DueAt != "2026-10-10T15:59:00Z" || out.Turn.Receipts[3].Op != "remember" || out.Turn.Receipts[3].Undoable {
		t.Fatal(out.State.Ideas, out.Turn.Receipts)
	}
	payload = `{"actions":[{"op":"add_steps","ref":"THIS","steps":["写信","装信封"]},{"op":"update","ref":"THIS","set":{"title":"寄两封信","notesAppend":"带邮票","project":"none","remind":"none"}},{"op":"delegate","ref":"THIS","kind":"plan","prompt":"写寄信方案"}]}`
	req := turnRequest("改成寄两封信，加两步，让副手写方案")
	req.ThingID = &item.ID
	second := mustTurn(t, s, scope, req)
	item = second.State.Tasks[0]
	if item.Title != "寄两封信" || len(item.Checklist) != 2 || item.Notes != "信封\n带邮票" || item.ProjectID != "" || len(item.Triggers) != 0 || len(second.State.Runs) != 1 || second.State.Runs[0].Kind != "plan" {
		t.Fatal(item, second.State.Runs)
	}
	undone, err := s.Undo(context.Background(), scope, *second.Turn.Receipts[2].ActionID)
	if err != nil || len(undone.Runs) != 0 || len(undone.Tasks) != 1 {
		t.Fatal(err, undone.Runs)
	}
	payload = `{"actions":[{"op":"delegate","ref":"new","title":"写材料","kind":"summary","prompt":"写总结材料"}]}`
	third := mustTurn(t, s, scope, turnRequest("写总结材料"))
	if len(third.State.Runs) != 1 || third.State.Runs[0].Kind != "summary" || !third.Turn.Receipts[0].Undoable {
		t.Fatal(third)
	}
	if _, err = s.pool.Exec(context.Background(), "UPDATE agent_runs SET status='running' WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), third.State.Runs[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Undo(context.Background(), scope, *third.Turn.Receipts[0].ActionID); !errors.Is(err, workspace.ErrWorkStarted) {
		t.Fatal(err)
	}
	var samples int
	if err = s.pool.QueryRow(context.Background(), "SELECT count(*) FROM training_samples WHERE owner_id=$1", string(scope.OwnerID)).Scan(&samples); err != nil || samples != 0 {
		t.Fatal("secretary wrote samples", err, samples)
	}
}
func TestSecretaryRejectsStaleRowsAndKeepsOriginalOnCancellation(t *testing.T) {
	for _, mode := range []string{"stale", "cancel", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			s := testStore(t)
			logs := secretaryLogs(t)
			scope := owner()
			var id string
			requestCtx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "timeout" {
				requestCtx, cancel = context.WithTimeout(context.Background(), 100*time.Millisecond)
				defer cancel()
			}
			secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
				if mode == "stale" {
					workspaceCommand(t, s, scope, workspace.Command{Type: "renameThing", ID: id, Title: "后来的名称"})
				} else if mode == "timeout" {
					select {
					case <-r.Context().Done():
					case <-time.After(time.Second):
					}
				} else {
					cancel()
				}
				secretaryModelReply(w, `{"actions":[{"op":"update","ref":"T1","set":{"title":"过时的名称"}}]}`)
			})
			st := workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "原名称"})
			id = st.Tasks[0].ID
			req := turnRequest("  帮我改名称  ")
			out, err := s.DeskTurn(requestCtx, scope, req)
			if err != nil || len(out.Turn.Receipts) != 1 || out.Turn.Receipts[0].Op != "capture" || out.Turn.Text != req.Text {
				t.Fatal(err, out)
			}
			stage, errorType, reason := "model", "canceled", "模型没有响应"
			if mode == "stale" {
				stage, errorType, reason = "verify", "conflict", "上下文已变更，请重试"
			} else if mode == "timeout" {
				errorType = "timeout"
			}
			assertSecretaryLog(t, logs, "WARN", stage, errorType)
			if out.Turn.Receipts[0].Text != "已记下原话；"+reason+"，稍后会自动整理" {
				t.Fatal(out.Turn.Receipts)
			}
			if mode == "stale" && out.State.Tasks[0].Title != "后来的名称" {
				t.Fatal("overwrote concurrent edit", out.State.Tasks)
			}
		})
	}
}

func TestSecretaryReplayUsesCurrentStateAndScrubbedTurn(t *testing.T) {
	s := testStore(t)
	scope := owner()
	ctx := context.Background()
	var calls atomic.Int32
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		secretaryModelReply(w, `{"reply":"私密暗号是蓝色灯塔。","used":["M1"],"actions":[{"op":"create_task","title":"核对蓝色灯塔"}],"ask":{"question":"要核对蓝色灯塔吗？","options":["要"]}}`)
	})
	st := workspaceCommand(t, s, scope, workspace.Command{Type: "capture", Text: "私密暗号是蓝色灯塔"})
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "acceptCandidate", ID: st.Candidates[0].ID, Kind: "memory", MemoryKind: "fact", Text: "私密暗号是蓝色灯塔"})
	mem := st.Memories[0]
	req := turnRequest("核对私密暗号蓝色灯塔")
	first := mustTurn(t, s, scope, req)
	if len(first.Turn.Cards) == 0 || len(first.Turn.Receipts) != 1 {
		t.Fatal("fixture lacks used memory or receipt", first.Turn)
	}
	var response []byte
	if err := s.pool.QueryRow(ctx, "SELECT response FROM desk_turns WHERE owner_id=$1 AND request_id=$2", string(scope.OwnerID), req.RequestID).Scan(&response); err != nil {
		t.Fatal(err)
	}
	var stored map[string]json.RawMessage
	if err := json.Unmarshal(response, &stored); err != nil || len(stored) != 2 || stored["conversationId"] == nil || stored["turn"] == nil || stored["state"] != nil {
		t.Fatal("persisted full workspace state", err, string(response))
	}
	current := workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "后来添加的工作"})
	replay := mustTurn(t, s, scope, req)
	if replay.State.Revision != current.Revision || len(replay.State.Tasks) != 2 || replay.Turn.ID != first.Turn.ID || replay.Turn.Reply != first.Turn.Reply || calls.Load() != 1 {
		t.Fatal("replay is stale or reran actions", replay, calls.Load())
	}
	current = workspaceCommand(t, s, scope, workspace.Command{Type: "deleteMemory", ID: mem.ID, IncludeSources: true})
	replay = mustTurn(t, s, scope, req)
	if replay.ConversationID != first.ConversationID || replay.Turn.ID != first.Turn.ID || replay.Turn.Text != "" || replay.Turn.Reply != "" || len(replay.Turn.Cards) != 0 || replay.Turn.Ask != nil || len(replay.Turn.Receipts) != 1 || calls.Load() != 1 {
		t.Fatal("deletion did not scrub replay", replay.Turn, calls.Load())
	}
	receipt := replay.Turn.Receipts[0]
	original := first.Turn.Receipts[0]
	if receipt.Text != "（内容已删除）" || receipt.ActionID == nil || original.ActionID == nil || *receipt.ActionID != *original.ActionID || receipt.Op != original.Op || receipt.Status != original.Status {
		t.Fatal("lost audit metadata", receipt, original)
	}
	if replay.State.Revision != current.Revision || len(replay.State.Memories) != 0 {
		t.Fatal("replay resurrected deleted state", replay.State)
	}
	if err := s.pool.QueryRow(ctx, "SELECT response FROM desk_turns WHERE owner_id=$1 AND request_id=$2", string(scope.OwnerID), req.RequestID).Scan(&response); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(response), "蓝色灯塔") || strings.Contains(string(response), `"state"`) {
		t.Fatal("deleted exchange remained in storage", string(response))
	}
}

func TestSecretaryClientConversationIDAndOwnerIsolation(t *testing.T) {
	s := testStore(t)
	scope := owner()
	other := owner()
	ctx := context.Background()
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		secretaryModelReply(w, `{"reply":"安排了","actions":[{"op":"create_task","title":"收到的安排"}]}`)
	})
	conversationID := string(memory.NewID())
	empty, err := s.DeskTurns(ctx, scope, conversationID)
	if err != nil || empty.ConversationID != conversationID || len(empty.Turns) != 0 {
		t.Fatal("unknown conversation must be empty", err, empty)
	}
	first, second := turnRequest("第一件事"), turnRequest("第二件事")
	first.ConversationID = &conversationID
	second.ConversationID = &conversationID
	type result struct {
		out workspace.DeskTurnResponse
		err error
	}
	results := make(chan result, 2)
	var wg sync.WaitGroup
	for _, req := range []workspace.DeskTurnRequest{first, second} {
		wg.Add(1)
		go func(req workspace.DeskTurnRequest) {
			defer wg.Done()
			out, err := s.DeskTurn(ctx, scope, req)
			results <- result{out, err}
		}(req)
	}
	wg.Wait()
	close(results)
	ownIDs := map[string]bool{}
	for res := range results {
		if res.err != nil || res.out.ConversationID != conversationID || len(res.out.Turn.Receipts) != 1 {
			t.Fatal("client conversation rejected", res.err, res.out)
		}
		ownIDs[res.out.Turn.ID] = true
	}
	turns, err := s.DeskTurns(ctx, scope, conversationID)
	if err != nil || len(turns.Turns) != 2 {
		t.Fatal(err, turns)
	}
	otherEmpty, err := s.DeskTurns(ctx, other, conversationID)
	if err != nil || len(otherEmpty.Turns) != 0 {
		t.Fatal("leaked another owner's conversation", err, otherEmpty)
	}
	// Even identical conversation and request IDs belong to a different owner.
	foreign := mustTurn(t, s, other, first)
	if foreign.ConversationID != conversationID || ownIDs[foreign.Turn.ID] || len(foreign.State.Tasks) != 1 {
		t.Fatal("cross-owner retry or state", foreign)
	}
	own, err := s.DeskTurns(ctx, scope, conversationID)
	if err != nil || len(own.Turns) != 2 {
		t.Fatal(err, own)
	}
	theirs, err := s.DeskTurns(ctx, other, conversationID)
	if err != nil || len(theirs.Turns) != 1 || theirs.Turns[0].ID != foreign.Turn.ID {
		t.Fatal(err, theirs)
	}
	legacy := mustTurn(t, s, scope, turnRequest("null 仍开启新对话"))
	if !memory.ID(legacy.ConversationID).Valid() || legacy.ConversationID == conversationID {
		t.Fatal("null conversation compatibility", legacy.ConversationID)
	}
}

func TestSecretaryReceiptUndoneIsLiveOnHistoryAndReplay(t *testing.T) {
	s := testStore(t)
	scope := owner()
	ctx := context.Background()
	var calls atomic.Int32
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		secretaryModelReply(w, `{"reply":"两件事都安排好了","remember":true,"actions":[{"op":"create_task","title":"第一件"},{"op":"create_task","title":"第二件"},{"op":"update","ref":"T999","set":{"title":"找不到"}}]}`)
	})
	req := turnRequest("请安排两件事")
	initial := mustTurn(t, s, scope, req)
	if len(initial.Turn.Receipts) != 4 {
		t.Fatal(initial.Turn)
	}
	for _, receipt := range initial.Turn.Receipts {
		if receipt.Undone {
			t.Fatal("fresh receipt marked undone", receipt)
		}
	}
	before, err := s.DeskTurns(ctx, scope, initial.ConversationID)
	if err != nil || len(before.Turns) != 1 {
		t.Fatal(err, before)
	}
	for _, receipt := range before.Turns[0].Receipts {
		if receipt.Undone {
			t.Fatal(receipt)
		}
	}
	action := *initial.Turn.Receipts[0].ActionID
	current, err := s.Undo(ctx, scope, action)
	if err != nil {
		t.Fatal(err)
	}
	check := func(turn workspace.SecretaryTurn) {
		t.Helper()
		if turn.ID != initial.Turn.ID || len(turn.Receipts) != 4 || !turn.Receipts[0].Undone || *turn.Receipts[0].ActionID != action {
			t.Fatal("undo not reflected", turn)
		}
		for _, receipt := range turn.Receipts[1:] {
			if receipt.Undone {
				t.Fatal("changed another receipt", receipt)
			}
		}
	}
	history, err := s.DeskTurns(ctx, scope, initial.ConversationID)
	if err != nil || len(history.Turns) != 1 {
		t.Fatal(err, history)
	}
	check(history.Turns[0])
	replay := mustTurn(t, s, scope, req)
	check(replay.Turn)
	if replay.State.Revision != current.Revision || len(replay.State.Tasks) != 1 || calls.Load() != 1 {
		t.Fatal("replay repeated action or returned old State", replay, calls.Load())
	}
	// The initial false flag remains in the stored turn. Reads derive true from
	// action_log without mutating the idempotency record.
	var stored storedSecretaryResponse
	if err = s.pool.QueryRow(ctx, "SELECT response FROM desk_turns WHERE owner_id=$1 AND request_id=$2", string(scope.OwnerID), req.RequestID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored.Turn.Receipts[0].Undone {
		t.Fatal("stored live workspace state", stored)
	}
	token := strings.Repeat("u", 64)
	api := httpapi.New(s, s, httpapi.NewOwnerToken(token, scope.OwnerID), func(context.Context) error { return nil }, slog.New(slog.NewTextHandler(io.Discard, nil)), httpapi.Options{Workspace: s})
	for _, endpoint := range []struct {
		method, path string
		body         any
	}{{"GET", "/v1/desk/turns?conversationId=" + initial.ConversationID, nil}, {"POST", "/v1/desk/turn", req}} {
		request := httptest.NewRequest(endpoint.method, endpoint.path, strings.NewReader(string(asJSON(endpoint.body))))
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		api.ServeHTTP(w, request)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		var response struct {
			Turn  workspace.SecretaryTurn   `json:"turn"`
			Turns []workspace.SecretaryTurn `json:"turns"`
		}
		if err = json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if endpoint.method == "GET" {
			if len(response.Turns) != 1 {
				t.Fatal(response)
			}
			check(response.Turns[0])
		} else {
			check(response.Turn)
		}
		if !strings.Contains(w.Body.String(), `"undone":true`) || !strings.Contains(w.Body.String(), `"undone":false`) {
			t.Fatal("missing explicit wire flag", w.Body.String())
		}
	}
}
