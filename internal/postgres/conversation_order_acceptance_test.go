package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/soaringjerry/PCAS/internal/httpapi"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/testsupport"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// Independent Q2 oracle: phase1 contracts §2.1.1, fixed 2026-10-01. These
// assertions were written before integrating F15, without reading its tests.
// Only committed admission metadata supplies ordering evidence; goroutine
// launch order and raw HTTP arrival are never treated as a global order.
type q2Result struct {
	out workspace.DeskTurnResponse
	err error
}
type q2Ticket struct {
	order        int64
	conversation string
	status       string
	expires      time.Time
}
type q2Model struct {
	mu      sync.Mutex
	calls   []string
	prompts []string
	entered chan struct{}
	release func()
}

// Observation only: this marks a tagged duplicate's completed server-side
// readiness query while the creator is held. It neither pauses a product
// query nor supplies its result. Cancellation is gated by this real event.
type q2DuplicateTraceKey struct{}
type q2DuplicateQueryKey struct{}
type q2DuplicateWitness struct {
	entered chan struct{}
	once    sync.Once
}

func (w *q2DuplicateWitness) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	marked, _ := ctx.Value(q2DuplicateTraceKey{}).(bool)
	return context.WithValue(ctx, q2DuplicateQueryKey{}, marked && strings.Contains(data.SQL, "FROM desk_turn_order t") && strings.Contains(data.SQL, "status"))
}
func (w *q2DuplicateWitness) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	matched, _ := ctx.Value(q2DuplicateQueryKey{}).(bool)
	if matched && data.Err == nil {
		w.once.Do(func() { close(w.entered) })
	}
}
func q2WitnessDuplicate(t *testing.T, s *Store) *q2DuplicateWitness {
	t.Helper()
	w := &q2DuplicateWitness{entered: make(chan struct{})}
	config := s.pool.Config()
	config.ConnConfig.Tracer = w
	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	s.pool.Close()
	s.pool = pool
	return w
}

func q2Script(t *testing.T, s *Store, held string, answers map[string]string) *q2Model {
	t.Helper()
	m := &q2Model{entered: make(chan struct{})}
	gate := make(chan struct{})
	var releaseOnce, enteredOnce sync.Once
	m.release = func() { releaseOnce.Do(func() { close(gate) }) }
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Messages) == 0 {
			http.Error(w, "invalid synthetic model request", http.StatusBadRequest)
			return
		}
		prompt := body.Messages[len(body.Messages)-1].Content
		input := ""
		for text := range answers {
			if strings.Contains(prompt, "这句话："+text+"\n") {
				input = text
			}
		}
		m.mu.Lock()
		m.calls = append(m.calls, input)
		m.prompts = append(m.prompts, prompt)
		m.mu.Unlock()
		if input == held {
			enteredOnce.Do(func() { close(m.entered) })
			select {
			case <-gate:
			case <-r.Context().Done():
				return
			}
		}
		answer, ok := answers[input]
		if !ok {
			http.Error(w, "unexpected synthetic input", http.StatusInternalServerError)
			return
		}
		secretaryModelReply(w, answer)
	})
	t.Cleanup(m.release) // Release before the helper closes its HTTP server.
	return m
}
func (m *q2Model) observed() ([]string, []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.calls...), append([]string(nil), m.prompts...)
}
func q2Context(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	return ctx
}
func q2Peer(t *testing.T, s *Store) *Store {
	t.Helper()
	peer, err := Open(context.Background(), s.pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	peer.SetModels(s.models)
	t.Cleanup(peer.Close)
	return peer
}
func q2Start(ctx context.Context, s *Store, scope memory.Scope, req workspace.DeskTurnRequest) <-chan q2Result {
	done := make(chan q2Result, 1)
	go func() { out, err := s.DeskTurn(ctx, scope, req); done <- q2Result{out, err} }()
	return done
}
func q2Receive(t *testing.T, done <-chan q2Result) q2Result {
	t.Helper()
	select {
	case got := <-done:
		return got
	case <-time.After(8 * time.Second):
		t.Fatal("bounded Q2 turn did not finish")
	}
	return q2Result{}
}
func q2Entered(t *testing.T, m *q2Model) {
	t.Helper()
	select {
	case <-m.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("first real provider call never reached its barrier")
	}
}
func q2Admission(t *testing.T, s *Store, scope memory.Scope, req workspace.DeskTurnRequest, status string) q2Ticket {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		var ticket q2Ticket
		err := s.pool.QueryRow(ctx, "SELECT admission_order,conversation_id::text,status,expires_at FROM desk_turn_order WHERE owner_id=$1 AND request_id=$2", string(scope.OwnerID), req.RequestID).Scan(&ticket.order, &ticket.conversation, &ticket.status, &ticket.expires)
		if err == nil && (status == "" || ticket.status == status) {
			t.Logf("committed admission request=%s order=%d conversation=%s status=%s expires=%s", req.RequestID, ticket.order, ticket.conversation, ticket.status, ticket.expires.Format(time.RFC3339Nano))
			return ticket
		}
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			t.Fatal(err)
		}
		select {
		case <-ctx.Done():
			t.Fatalf("missing committed admission/status for %s: wanted=%s last=%s", req.Text, status, ticket.status)
		case <-time.After(5 * time.Millisecond):
		}
	}
}
func q2Successful(t *testing.T, got q2Result, op string) workspace.DeskTurnResponse {
	t.Helper()
	if got.err != nil {
		t.Fatal(got.err)
	}
	if len(got.out.Turn.Receipts) != 1 || got.out.Turn.Receipts[0].Op != op || got.out.Turn.Receipts[0].Status != "done" {
		t.Fatalf("expected real %s success, got %+v", op, got.out.Turn)
	}
	return got.out
}
func q2Meeting(t *testing.T, s *Store, scope memory.Scope, due string) workspace.Item {
	t.Helper()
	state, err := s.Snapshot(context.Background(), scope)
	if err != nil || len(state.Tasks) != 1 || state.Tasks[0].Due != due {
		t.Fatalf("visible meeting must remain a single task due=%s: err=%v tasks=%+v", due, err, state.Tasks)
	}
	return state.Tasks[0]
}
func q2Request(text, conversation string) workspace.DeskTurnRequest {
	req := turnRequest(text)
	if conversation != "" {
		req.ConversationID = &conversation
	}
	return req
}
func q2Settings(t *testing.T, s *Store, scope memory.Scope) {
	workspaceCommand(t, s, scope, workspace.Command{Type: "updateSettings", Patch: asJSON(map[string]any{"timezone": "UTC", "autoAccept": true})})
}
func q2HTTP(t *testing.T, s *Store, scope memory.Scope, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	token := strings.Repeat("q", 64)
	api := httpapi.New(s, s, httpapi.NewOwnerToken(token, scope.OwnerID), func(context.Context) error { return nil }, slog.New(slog.NewTextHandler(io.Discard, nil)), httpapi.Options{Workspace: s})
	req := httptest.NewRequest(method, path, strings.NewReader(string(asJSON(body))))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	api.ServeHTTP(w, req)
	return w
}

func TestConversationOrderAcceptance_CommittedOrderAcrossStores(t *testing.T) {
	s, scope, ctx := testStore(t), owner(), q2Context(t)
	q2Settings(t, s, scope)
	m := q2Script(t, s, "三点开会", map[string]string{
		"三点开会":  fmt.Sprintf(`{"actions":[{"op":"create_task","title":"Q2明确顺序会议","due":"%s"}]}`, testsupport.DateFromToday(t, "UTC", 1, 15, 0).Format("2006-01-02T15:04")),
		"改四点":   fmt.Sprintf(`{"actions":[{"op":"update","ref":"R1","set":{"due":"%s"}}]}`, testsupport.DateFromToday(t, "UTC", 1, 16, 0).Format("2006-01-02T15:04")),
		"最后改五点": fmt.Sprintf(`{"actions":[{"op":"update","ref":"R1","set":{"due":"%s"}}]}`, testsupport.DateFromToday(t, "UTC", 1, 17, 0).Format("2006-01-02T15:04")),
	})
	peer := q2Peer(t, s)
	conversation := string(memory.NewID())
	a, b, c := q2Request("三点开会", conversation), q2Request("改四点", strings.ToUpper(conversation)), q2Request("最后改五点", conversation)
	aDone := q2Start(ctx, s, scope, a)
	q2Entered(t, m)
	at := q2Admission(t, peer, scope, a, "pending")
	bDone := q2Start(ctx, peer, scope, b)
	bt := q2Admission(t, s, scope, b, "pending")
	cDone := q2Start(ctx, s, scope, c)
	ct := q2Admission(t, peer, scope, c, "pending")
	if !(at.order < bt.order && bt.order < ct.order) || at.conversation != conversation || bt.conversation != conversation || ct.conversation != conversation {
		t.Fatal("no common committed order or normalized conversation")
	}
	if order, _ := m.observed(); !reflect.DeepEqual(order, []string{a.Text}) {
		t.Fatalf("later model passed first barrier: %v", order)
	}
	m.release()
	outs := []workspace.DeskTurnResponse{q2Successful(t, q2Receive(t, aDone), "create_task"), q2Successful(t, q2Receive(t, bDone), "update"), q2Successful(t, q2Receive(t, cDone), "update")}
	meeting := q2Meeting(t, peer, scope, testsupport.DateFromToday(t, "UTC", 1, 17, 0).UTC().Format(time.RFC3339))
	for i, out := range outs {
		if out.ConversationID != conversation || out.Turn.Receipts[0].ThingID == nil || *out.Turn.Receipts[0].ThingID != meeting.ID {
			t.Fatalf("turn %d did not operate on the shared meeting", i+1)
		}
	}
	order, prompts := m.observed()
	if !reflect.DeepEqual(order, []string{a.Text, b.Text, c.Text}) || !strings.Contains(prompts[1], "15:00:00Z") || !strings.Contains(prompts[2], "16:00:00Z") {
		t.Fatalf("model sequence/context violates committed order: %v", order)
	}
	history, err := peer.DeskTurns(ctx, scope, strings.ToUpper(conversation))
	if err != nil || len(history.Turns) != 3 {
		t.Fatalf("history %v %+v", err, history)
	}
	for i, turn := range history.Turns {
		if turn.ID != outs[i].Turn.ID {
			t.Fatalf("saved history position %d did not match committed/model order", i)
		}
	}
	t.Logf("accepted_orders=[%d,%d,%d] model=%v history=[%s,%s,%s] final_due=%s", at.order, bt.order, ct.order, order, history.Turns[0].Text, history.Turns[1].Text, history.Turns[2].Text, meeting.Due)
}

func TestConversationOrderAcceptance_SameKeyNullConversationAndDuplicateCancellation(t *testing.T) {
	s, scope, ctx := testStore(t), owner(), q2Context(t)
	q2Settings(t, s, scope)
	req := q2Request("只建一次空conversation会议", "")
	m := q2Script(t, s, req.Text, map[string]string{req.Text: fmt.Sprintf(`{"actions":[{"op":"create_task","title":"Q2只执行一次","due":"%s"}]}`, testsupport.DateFromToday(t, "UTC", 1, 15, 0).Format("2006-01-02T15:04"))})
	peer := q2Peer(t, s)
	witness := q2WitnessDuplicate(t, peer)
	creator := q2Start(ctx, s, scope, req)
	q2Entered(t, m)
	accepted := q2Admission(t, peer, scope, req, "pending")
	duplicateCtx, cancel := context.WithCancel(context.WithValue(ctx, q2DuplicateTraceKey{}, true))
	duplicateWaiter := q2Start(duplicateCtx, peer, scope, req)
	select {
	case <-witness.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("duplicate never completed a real server-side readiness query")
	}
	cancel()
	if canceled := q2Receive(t, duplicateWaiter); !errors.Is(canceled.err, context.Canceled) {
		t.Fatalf("duplicate cancellation should terminate only its waiter: %v", canceled.err)
	}
	after := q2Admission(t, s, scope, req, "pending")
	if after.order != accepted.order || after.conversation != accepted.conversation || !after.expires.Equal(accepted.expires) {
		t.Fatal("retry re-admitted or extended deadline")
	}
	var count int
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM desk_turn_order WHERE owner_id=$1 AND request_id=$2", string(scope.OwnerID), req.RequestID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("ticket multiplicity %d: %v", count, err)
	}
	changed := req
	changed.Text = "冲突正文不能产生第二事项"
	if _, err := peer.DeskTurn(ctx, scope, changed); !errors.Is(err, memory.ErrConflict) {
		t.Fatalf("changed body must conflict during active original: %v", err)
	}
	successfulWitness := q2WitnessDuplicate(t, peer)
	duplicate := q2Start(context.WithValue(ctx, q2DuplicateTraceKey{}, true), peer, scope, req)
	select {
	case <-successfulWitness.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("successful duplicate never entered server-side waiting before first model release")
	}
	m.release()
	one, two := q2Successful(t, q2Receive(t, creator), "create_task"), q2Successful(t, q2Receive(t, duplicate), "create_task")
	if one.Turn.ID != two.Turn.ID || one.ConversationID != accepted.conversation || two.ConversationID != accepted.conversation {
		t.Fatal("duplicate did not replay the original turn/conversation")
	}
	q2Meeting(t, s, scope, testsupport.DateFromToday(t, "UTC", 1, 15, 0).UTC().Format(time.RFC3339))
	if order, _ := m.observed(); !reflect.DeepEqual(order, []string{req.Text}) {
		t.Fatalf("same key executed more than once: %v", order)
	}
	if _, err := peer.DeskTurn(ctx, scope, changed); !errors.Is(err, memory.ErrConflict) {
		t.Fatalf("changed body must also conflict after completion: %v", err)
	}
	workspaceCommand(t, s, scope, workspace.Command{Type: "addIdea", Title: "Q2当前State变化"})
	replay := q2Successful(t, q2Receive(t, q2Start(ctx, peer, scope, req)), "create_task")
	if replay.Turn.ID != one.Turn.ID || len(replay.State.Ideas) != 1 {
		t.Fatal("completed replay did not include current workspace state")
	}
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM desk_turns WHERE owner_id=$1 AND request_id=$2", string(scope.OwnerID), req.RequestID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("saved turn multiplicity %d: %v", count, err)
	}
}

// Canceled while queued, then a later accepted turn commits before recovery.
// Return fixtures, so the recovery and deletion tests assert different public
// outcomes without depending on an author test or private queue helper.
func q2CanceledInput(t *testing.T) (*Store, *Store, memory.Scope, workspace.DeskTurnRequest, *q2Model) {
	t.Helper()
	s, scope, ctx := testStore(t), owner(), q2Context(t)
	q2Settings(t, s, scope)
	conversation := string(memory.NewID())
	first, old, last := q2Request("Q2阻塞三点", conversation), q2Request("Q2故障暗号橙色山脉，创建旧四点事项", conversation), q2Request("Q2新意图五点", conversation)
	old.RequestID = strings.ToUpper(old.RequestID)
	m := q2Script(t, s, first.Text, map[string]string{
		first.Text: fmt.Sprintf(`{"actions":[{"op":"create_task","title":"Q2取消后的同一会议","due":"%s"}]}`, testsupport.DateFromToday(t, "UTC", 1, 15, 0).Format("2006-01-02T15:04")),
		old.Text:   fmt.Sprintf(`{"actions":[{"op":"create_task","title":"Q2禁止迟到旧事项","due":"%s"}]}`, testsupport.DateFromToday(t, "UTC", 1, 16, 0).Format("2006-01-02T15:04")),
		last.Text:  fmt.Sprintf(`{"actions":[{"op":"update","ref":"R1","set":{"due":"%s"}}]}`, testsupport.DateFromToday(t, "UTC", 1, 17, 0).Format("2006-01-02T15:04")),
	})
	peer := q2Peer(t, s)
	firstDone := q2Start(ctx, s, scope, first)
	q2Entered(t, m)
	firstTicket := q2Admission(t, peer, scope, first, "pending")
	oldCtx, cancelOld := context.WithCancel(ctx)
	t.Cleanup(cancelOld)
	oldDone := q2Start(oldCtx, peer, scope, old)
	oldTicket := q2Admission(t, s, scope, old, "pending")
	if oldTicket.order <= firstTicket.order {
		t.Fatal("queued old request lacks committed position")
	}
	cancelOld()
	if got := q2Receive(t, oldDone); !errors.Is(got.err, context.Canceled) {
		t.Fatalf("queued creator cancellation falsely succeeded: %+v", got)
	}
	q2Admission(t, s, scope, old, "canceled")
	lastDone := q2Start(ctx, s, scope, last)
	lastTicket := q2Admission(t, peer, scope, last, "pending")
	if lastTicket.order <= oldTicket.order {
		t.Fatal("later input lacks common order")
	}
	m.release()
	q2Successful(t, q2Receive(t, firstDone), "create_task")
	q2Successful(t, q2Receive(t, lastDone), "update")
	q2Meeting(t, s, scope, testsupport.DateFromToday(t, "UTC", 1, 17, 0).UTC().Format(time.RFC3339))
	if order, _ := m.observed(); !reflect.DeepEqual(order, []string{first.Text, last.Text}) {
		t.Fatalf("canceled request called model: %v", order)
	}
	var turns int
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM desk_turns WHERE owner_id=$1 AND request_id=$2", string(scope.OwnerID), old.RequestID).Scan(&turns); err != nil || turns != 0 {
		t.Fatalf("canceled queued input saved a fake successful turn: %d %v", turns, err)
	}
	return s, peer, scope, old, m
}
func q2Recover(t *testing.T, s *Store, scope memory.Scope, req workspace.DeskTurnRequest) workspace.DeskTurnResponse {
	t.Helper()
	w := q2HTTP(t, s, scope, http.MethodPost, "/v1/desk/turn", req)
	if w.Code != http.StatusOK {
		t.Fatalf("same-key terminal HTTP recovery must close retry loop: status=%d body=%s", w.Code, w.Body.String())
	}
	var out workspace.DeskTurnResponse
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Turn.Text != req.Text || len(out.Turn.Receipts) != 1 {
		t.Fatalf("recovery did not preserve original input: %+v", out.Turn)
	}
	r := out.Turn.Receipts[0]
	if r.Op != "capture" || r.Status != "done" || r.ActionID != nil || r.ThingID != nil || r.Undoable || !strings.Contains(r.Text, "未完成") || strings.Contains(r.Text, "自动整理") {
		t.Fatalf("recovery pretended to execute or auto-organize old input: %+v", r)
	}
	return out
}
func q2RecoveredSource(t *testing.T, s *Store, scope memory.Scope, req workspace.DeskTurnRequest) memory.Ref {
	t.Helper()
	var source memory.Ref
	var id string
	if err := s.pool.QueryRow(context.Background(), "SELECT s.id::text,r.version FROM sources s JOIN memory_records r ON (r.owner_id,r.id)=(s.owner_id,s.id) WHERE s.owner_id=$1 AND s.connector='desk-incomplete' AND s.external_id=$2", string(scope.OwnerID), req.RequestID).Scan(&id, &source.Version); err != nil {
		t.Fatal(err)
	}
	source.ID, source.Kind = memory.ID(id), memory.SourceKind
	return source
}

func TestConversationOrderAcceptance_TerminalRecoveryPreservesRawWithoutAutomaticActions(t *testing.T) {
	s, peer, scope, old, m := q2CanceledInput(t)
	ctx := q2Context(t)
	// Same external request identity in ordinary capture must retain its work.
	state, err := s.Snapshot(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	normal, err := s.Execute(ctx, scope, workspace.Command{Type: "capture", Text: old.Text, RequestID: old.RequestID, ExpectedRevision: state.Revision})
	if err != nil {
		t.Fatal(err)
	}
	var normalSource string
	if err := s.pool.QueryRow(ctx, "SELECT id::text FROM sources WHERE owner_id=$1 AND connector='capture' AND external_id=$2", string(scope.OwnerID), old.RequestID).Scan(&normalSource); err != nil {
		t.Fatal(err)
	}
	if len(normal.Candidates) != 1 {
		t.Fatal("ordinary capture fixture lacks its unknown candidate")
	}
	before, _ := m.observed()
	recovered := q2Recover(t, peer, scope, old)
	source := q2RecoveredSource(t, s, scope, old)
	if string(source.ID) == normalSource {
		t.Fatal("incomplete recovery reused ordinary capture source identity")
	}
	raw, err := peer.GetSource(ctx, scope, source.ID, source.Version)
	if err != nil || raw.Source.Text != old.Text || len(raw.Processing) != 0 {
		t.Fatalf("raw incomplete source unavailable or has a re-executable job: err=%v source=%+v", err, raw)
	}
	var incompleteJobs, normalJobs int
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM memory_jobs WHERE owner_id=$1 AND record_id=$2", string(scope.OwnerID), string(source.ID)).Scan(&incompleteJobs); err != nil {
		t.Fatal(err)
	}
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM memory_jobs WHERE owner_id=$1 AND record_id=$2 AND stage='source.chunk' AND state='queued' AND attempts=0", string(scope.OwnerID), normalSource).Scan(&normalJobs); err != nil {
		t.Fatal(err)
	}
	if incompleteJobs != 0 || normalJobs != 1 {
		t.Fatalf("automatic isolation changed wrong source: incomplete=%d ordinary=%d", incompleteJobs, normalJobs)
	}
	unknown := 0
	for _, candidate := range recovered.State.Candidates {
		if candidate.Source.SourceID == string(source.ID) {
			if candidate.Kind != "unknown" || candidate.State != "pending" || candidate.Text != old.Text {
				t.Fatalf("recovery candidate must wait for explicit user handling: %+v", candidate)
			}
			unknown++
		}
	}
	if unknown != 1 {
		t.Fatalf("recovery unknown candidate count=%d", unknown)
	}
	sourceVisible := false
	for _, entry := range recovered.State.Sources {
		// The library lists everything said to the secretary as one entry.
		if entry.ID == "said" && entry.ItemCount >= 1 {
			sourceVisible = true
			if entry.Note != "" || entry.Status != "connected" {
				t.Fatalf("untouched raw source falsely presented as extracted/failure: %+v", entry)
			}
		}
	}
	if !sourceVisible {
		t.Fatal("raw recovered source absent from visible workspace sources")
	}
	exported, err := peer.Export(ctx, scope, false, false)
	if err != nil {
		t.Fatal(err)
	}
	var export struct {
		Sources []struct {
			ID   string `json:"id"`
			Text string `json:"text"`
		} `json:"sourceVersions"`
	}
	if err := json.Unmarshal(exported, &export); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range export.Sources {
		if entry.ID == string(source.ID) && entry.Text == old.Text {
			found = true
		}
	}
	if !found {
		t.Fatal("full raw source missing from public export")
	}
	replayed := q2Recover(t, s, scope, old)
	if replayed.Turn.ID != recovered.Turn.ID || replayed.ConversationID != recovered.ConversationID {
		t.Fatal("recovered input re-admitted on same-key repeat")
	}
	if after, _ := m.observed(); !reflect.DeepEqual(after, before) {
		t.Fatalf("terminal retry called model after later input: %v", after)
	}
	q2Meeting(t, s, scope, testsupport.DateFromToday(t, "UTC", 1, 17, 0).UTC().Format(time.RFC3339))
	q2Admission(t, s, scope, old, "done")
	t.Logf("terminal recovery HTTP=200 source=%s raw_read/export=true jobs=%d ordinary_capture_jobs=%d model=%v final_due=17:00", source.ID, incompleteJobs, normalJobs, before)
}

func TestConversationOrderAcceptance_DeletedRecoveryDoesNotResurrectThroughHTTP(t *testing.T) {
	s, peer, scope, old, m := q2CanceledInput(t)
	ctx := q2Context(t)
	recovered := q2Recover(t, s, scope, old)
	source := q2RecoveredSource(t, s, scope, old)
	before, _ := m.observed()
	if err := peer.Delete(ctx, scope, memory.DeleteRequest{Targets: []memory.Ref{source}, BlockReimport: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetSource(ctx, scope, source.ID, source.Version); !errors.Is(err, memory.ErrNotFound) {
		t.Fatalf("deleted recovery source is still readable: %v", err)
	}
	for _, endpoint := range []struct{ method, path string }{{http.MethodGet, "/v1/desk/turns?conversationId=" + recovered.ConversationID}, {http.MethodPost, "/v1/desk/turn"}} {
		w := q2HTTP(t, peer, scope, endpoint.method, endpoint.path, old)
		if w.Code != http.StatusOK || strings.Contains(w.Body.String(), old.Text) || strings.Contains(w.Body.String(), "橙色山脉") {
			t.Fatalf("deleted incomplete raw text revived: %s status=%d body=%s", endpoint.path, w.Code, w.Body.String())
		}
		if endpoint.method == http.MethodPost {
			var replay workspace.DeskTurnResponse
			if err := json.Unmarshal(w.Body.Bytes(), &replay); err != nil {
				t.Fatal(err)
			}
			if replay.Turn.ID != recovered.Turn.ID || replay.Turn.Text != "" || replay.Turn.Reply != "" || len(replay.Turn.Cards) != 0 || len(replay.Turn.Receipts) != 1 || replay.Turn.Receipts[0].Text != "（内容已删除）" || replay.Turn.Receipts[0].Op != "capture" || replay.Turn.Receipts[0].Status != "done" {
				t.Fatalf("deleted replay lost scrubbed audit skeleton: %+v", replay.Turn)
			}
		}
	}
	byRequest, err := peer.DeskTurnByRequest(ctx, scope, old.RequestID)
	if err != nil || strings.Contains(string(asJSON(byRequest)), "橙色山脉") {
		t.Fatalf("Telegram request replay leaks deleted raw: %v %+v", err, byRequest)
	}
	exported, err := s.Export(ctx, scope, false, false)
	if err != nil || strings.Contains(string(exported), "橙色山脉") {
		t.Fatal("deleted raw remains in export", err)
	}
	var question, answer string
	var stored []byte
	if err := s.pool.QueryRow(ctx, "SELECT question,answer,response FROM desk_turns WHERE owner_id=$1 AND request_id=$2", string(scope.OwnerID), old.RequestID).Scan(&question, &answer, &stored); err != nil || question != "" || answer != "" || strings.Contains(string(stored), "橙色山脉") {
		t.Fatal("durable recovery response was not scrubbed", err)
	}
	if after, _ := m.observed(); !reflect.DeepEqual(after, before) {
		t.Fatalf("deleted same-key replay called model: %v", after)
	}
	q2Meeting(t, s, scope, testsupport.DateFromToday(t, "UTC", 1, 17, 0).UTC().Format(time.RFC3339))
}

func TestConversationOrderAcceptance_ConnectionLossAndExpiredHeadFence(t *testing.T) {
	s, scope, ctx := testStore(t), owner(), q2Context(t)
	q2Settings(t, s, scope)
	conversation := string(memory.NewID())
	old, next := q2Request("Q2旧连接故障不得迟到建四点", conversation), q2Request("Q2连接故障后五点", conversation)
	m := q2Script(t, s, old.Text, map[string]string{
		old.Text:  fmt.Sprintf(`{"actions":[{"op":"create_task","title":"Q2旧故障四点","due":"%s"}]}`, testsupport.DateFromToday(t, "UTC", 1, 16, 0).Format("2006-01-02T15:04")),
		next.Text: fmt.Sprintf(`{"actions":[{"op":"create_task","title":"Q2故障后五点","due":"%s"}]}`, testsupport.DateFromToday(t, "UTC", 1, 17, 0).Format("2006-01-02T15:04")),
	})
	// Hold the execution lock just for fixture setup. Admission can commit;
	// its creator cannot hold the ticket row for generation yet.
	fixture, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fixture.Rollback(context.Background()) })
	key := strings.ToLower("secretary-conversation:" + string(scope.OwnerID) + ":" + conversation)
	if _, err := fixture.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", key); err != nil {
		t.Fatal(err)
	}
	oldDone := q2Start(ctx, s, scope, old)
	q2Admission(t, s, scope, old, "pending")
	// Inject a historical deadline, not a real two-minute wait or process kill.
	if _, err := s.pool.Exec(ctx, "UPDATE desk_turn_order SET expires_at=clock_timestamp()+interval '1 second' WHERE owner_id=$1 AND request_id=$2 AND status='pending'", string(scope.OwnerID), old.RequestID); err != nil {
		t.Fatal(err)
	}
	if err := fixture.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	q2Entered(t, m)
	for {
		var expired bool
		if err := s.pool.QueryRow(ctx, "SELECT expires_at<=clock_timestamp() FROM desk_turn_order WHERE owner_id=$1 AND request_id=$2", string(scope.OwnerID), old.RequestID).Scan(&expired); err != nil {
			t.Fatal(err)
		}
		if expired {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("injected deadline never elapsed")
		case <-time.After(10 * time.Millisecond):
		}
	}
	peer := q2Peer(t, s) // Fresh Store shares only committed database state.
	nextDone := q2Start(ctx, peer, scope, next)
	q2Admission(t, s, scope, next, "pending")
	time.Sleep(100 * time.Millisecond) // Give the observed follower finite cleanup attempts.
	if ticket := q2Admission(t, peer, scope, old, ""); ticket.status != "pending" {
		t.Fatalf("expired timestamp jumped over still-running old transaction: %+v", ticket)
	}
	if order, _ := m.observed(); !reflect.DeepEqual(order, []string{old.Text}) {
		t.Fatalf("follower modeled while old execution lock live: %v", order)
	}
	// Terminate only this synthetic request's lock-holding PostgreSQL backend.
	// Matching the exact bigint advisory key avoids touching an unrelated PID.
	var pid int32
	requestKey := strings.ToLower(string(scope.OwnerID) + ":" + old.RequestID)
	if err := peer.pool.QueryRow(ctx, `SELECT pid FROM pg_locks WHERE locktype='advisory' AND granted
 AND classid=((hashtextextended($1,0)>>32)&4294967295)::oid
 AND objid=(hashtextextended($1,0)&4294967295)::oid AND objsubid=1`, requestKey).Scan(&pid); err != nil {
		t.Fatal(err)
	}
	var terminated bool
	if err := peer.pool.QueryRow(ctx, "SELECT pg_terminate_backend($1)", pid).Scan(&terminated); err != nil || !terminated {
		t.Fatalf("own synthetic backend termination failed: pid=%d %v", pid, err)
	}
	newResult := q2Successful(t, q2Receive(t, nextDone), "create_task")
	q2Meeting(t, peer, scope, testsupport.DateFromToday(t, "UTC", 1, 17, 0).UTC().Format(time.RFC3339))
	q2Admission(t, peer, scope, old, "expired")
	if order, _ := m.observed(); !reflect.DeepEqual(order, []string{old.Text, next.Text}) {
		t.Fatalf("new Store failed to advance after connection loss: %v", order)
	}
	// Late completion of the actual old model must fail to commit business.
	m.release()
	if got := q2Receive(t, oldDone); got.err == nil {
		t.Fatalf("lost old execution transaction falsely committed: %+v", got.out)
	}
	before, _ := m.observed()
	recovered := q2Recover(t, peer, scope, old)
	if recovered.ConversationID != newResult.ConversationID {
		t.Fatal("expired same-key recovery changed conversation")
	}
	q2Meeting(t, peer, scope, testsupport.DateFromToday(t, "UTC", 1, 17, 0).UTC().Format(time.RFC3339))
	if after, _ := m.observed(); !reflect.DeepEqual(after, before) {
		t.Fatalf("expired recovery reran old model: %v", after)
	}
	raw, err := peer.GetSource(ctx, scope, q2RecoveredSource(t, peer, scope, old).ID, 0)
	if err != nil || raw.Source.Text != old.Text || len(raw.Processing) != 0 {
		t.Fatalf("expired recovery did not preserve isolated raw input: %v %+v", err, raw)
	}
	t.Logf("injected deadline + actual own backend loss pid=%d; fresh Store advanced while old model was held; late old commit failed; final_due=17:00", pid)
}
