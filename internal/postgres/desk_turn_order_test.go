package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

type orderedTurnResult struct {
	out workspace.DeskTurnResponse
	err error
}

func orderedTurnStart(s *Store, ctx context.Context, scope memory.Scope, req workspace.DeskTurnRequest) <-chan orderedTurnResult {
	result := make(chan orderedTurnResult, 1)
	go func() { out, err := s.DeskTurn(ctx, scope, req); result <- orderedTurnResult{out, err} }()
	return result
}
func orderedTurnGet(t *testing.T, result <-chan orderedTurnResult) orderedTurnResult {
	t.Helper()
	select {
	case r := <-result:
		return r
	case <-time.After(10 * time.Second):
		t.Fatal("DeskTurn did not finish")
		return orderedTurnResult{}
	}
}
func orderedTicketWait(t *testing.T, s *Store, scope memory.Scope, request string) int64 {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		var order int64
		err := s.pool.QueryRow(ctx, "SELECT admission_order FROM desk_turn_order WHERE owner_id=$1 AND request_id=$2", string(scope.OwnerID), request).Scan(&order)
		if err == nil {
			return order
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatalf("committed admission missing: %v", err)
			return 0
		}
	}
}
func orderedCurrentInput(r *http.Request) string {
	var body struct {
		Messages []struct {
			Content string `json:"content"`
		} `json:"messages"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	for _, m := range body.Messages {
		if i := strings.LastIndex(m.Content, "这句话："); i >= 0 {
			return strings.TrimSpace(strings.SplitN(m.Content[i+len("这句话："):], "\n", 2)[0])
		}
	}
	return ""
}
func orderedPeer(t *testing.T, s *Store) *Store {
	t.Helper()
	peer, err := Open(context.Background(), s.pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(peer.Close)
	return peer
}

func TestSecretaryOrderCommittedFIFOAcrossStores(t *testing.T) {
	s, scope := testStore(t), owner()
	peer := orderedPeer(t, s)
	workspaceCommand(t, s, scope, workspace.Command{Type: "updateSettings", Patch: asJSON(map[string]string{"timezone": "UTC"})})
	conversation := string(memory.NewID())
	held, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	var mu sync.Mutex
	calls := []string{}
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		text := orderedCurrentInput(r)
		mu.Lock()
		calls = append(calls, text)
		mu.Unlock()
		switch text {
		case "三点开会":
			close(held)
			<-release
			secretaryModelReply(w, `{"actions":[{"op":"create_task","title":"F15会议","due":"2026-10-02T15:00"}]}`)
		case "改四点":
			secretaryModelReply(w, `{"actions":[{"op":"update","ref":"R1","set":{"due":"2026-10-02T16:00"}}]}`)
		case "最后改五点":
			secretaryModelReply(w, `{"actions":[{"op":"update","ref":"R1","set":{"due":"2026-10-02T17:00"}}]}`)
		default:
			http.Error(w, "unexpected fixture", 500)
		}
	})
	peer.SetModels(s.models)
	requests := []workspace.DeskTurnRequest{turnRequest("三点开会"), turnRequest("改四点"), turnRequest("最后改五点")}
	for i := range requests {
		requests[i].ConversationID = &conversation
	}
	first := orderedTurnStart(s, context.Background(), scope, requests[0])
	select {
	case <-held:
	case <-time.After(5 * time.Second):
		t.Fatal("first generation did not enter")
	}
	firstOrder := orderedTicketWait(t, s, scope, requests[0].RequestID)
	upper := strings.ToUpper(conversation)
	requests[1].ConversationID = &upper
	second := orderedTurnStart(peer, context.Background(), scope, requests[1])
	secondOrder := orderedTicketWait(t, s, scope, requests[1].RequestID)
	third := orderedTurnStart(s, context.Background(), scope, requests[2])
	thirdOrder := orderedTicketWait(t, s, scope, requests[2].RequestID)
	if !(firstOrder < secondOrder && secondOrder < thirdOrder) {
		t.Fatalf("committed order: %d %d %d", firstOrder, secondOrder, thirdOrder)
	}
	unblock()
	results := []orderedTurnResult{orderedTurnGet(t, first), orderedTurnGet(t, second), orderedTurnGet(t, third)}
	for _, r := range results {
		if r.err != nil || len(r.out.Turn.Receipts) != 1 || r.out.Turn.Receipts[0].Status != "done" {
			t.Fatalf("turn did not perform action: %v %+v", r.err, r.out.Turn)
		}
	}
	mu.Lock()
	got := strings.Join(calls, "→")
	mu.Unlock()
	if got != "三点开会→改四点→最后改五点" {
		t.Fatal(got)
	}
	state, err := s.Snapshot(context.Background(), scope)
	if err != nil || len(state.Tasks) != 1 || state.Tasks[0].Due != "2026-10-02T17:00:00Z" {
		t.Fatalf("final business state: %v %+v", err, state.Tasks)
	}
	for _, r := range results {
		if r.out.Turn.Receipts[0].ThingID == nil || *r.out.Turn.Receipts[0].ThingID != state.Tasks[0].ID {
			t.Fatal("turns did not update same item")
		}
	}
	history, err := s.DeskTurns(context.Background(), scope, conversation)
	if err != nil || len(history.Turns) != 3 {
		t.Fatal(err, history)
	}
	for i, turn := range history.Turns {
		if turn.Text != requests[i].Text {
			t.Fatal("saved history did not match admission order", history)
		}
	}
	replay := mustTurn(t, peer, scope, requests[1])
	if replay.Turn.ID != results[1].out.Turn.ID || replay.State.Tasks[0].Due != state.Tasks[0].Due {
		t.Fatal("replay failed")
	}
	requests[1].Text = "body conflict"
	if _, err := s.DeskTurn(context.Background(), scope, requests[1]); !errors.Is(err, memory.ErrConflict) {
		t.Fatal("changed body accepted", err)
	}
	t.Logf("committed admission %d→%d→%d; model/history 1→2→3; same task final due=%s", firstOrder, secondOrder, thirdOrder, state.Tasks[0].Due)
}

func TestSecretaryOrderWaitingUsesNoSlots(t *testing.T) {
	s, scope := testStore(t), owner()
	peer := orderedPeer(t, s)
	conversation := string(memory.NewID())
	held, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		if orderedCurrentInput(r) == "held" {
			close(held)
			<-release
		}
		secretaryModelReply(w, `{"actions":[{"op":"create_task","title":"ordered task"}]}`)
	})
	peer.SetModels(s.models)
	req := turnRequest("held")
	req.ConversationID = &conversation
	results := []<-chan orderedTurnResult{orderedTurnStart(s, context.Background(), scope, req)}
	select {
	case <-held:
	case <-time.After(5 * time.Second):
		t.Fatal("no held first")
	}
	for i := 0; i < 14; i++ {
		q := turnRequest("waiter")
		q.ConversationID = &conversation
		target := s
		if i%2 == 0 {
			target = peer
		}
		results = append(results, orderedTurnStart(target, context.Background(), scope, q))
		orderedTicketWait(t, s, scope, q.RequestID)
	}
	if len(s.secretarySlots) != 1 || len(peer.secretarySlots) != 0 {
		t.Fatalf("waiters consumed generation slots: %d/%d", len(s.secretarySlots), len(peer.secretarySlots))
	}
	for _, isolated := range []memory.Scope{scope, owner()} {
		q := turnRequest("independent")
		if isolated.OwnerID != scope.OwnerID {
			q.ConversationID = &conversation
		}
		r := orderedTurnGet(t, orderedTurnStart(peer, context.Background(), isolated, q))
		if r.err != nil || len(r.out.Turn.Receipts) != 1 || r.out.Turn.Receipts[0].Op != "create_task" {
			t.Fatalf("independent turn blocked: %v %+v", r.err, r.out.Turn)
		}
	}
	unblock()
	for _, result := range results {
		if r := orderedTurnGet(t, result); r.err != nil {
			t.Fatal(r.err)
		}
	}
	stabilizationSecretaryTasks(t, s, scope, 16)
}

func TestSecretaryOrderDuplicateCancellationDoesNotCancelCreator(t *testing.T) {
	s, scope := testStore(t), owner()
	peer := orderedPeer(t, s)
	held, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	var calls atomic.Int32
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		close(held)
		<-release
		secretaryModelReply(w, `{"actions":[{"op":"create_task","title":"only once"}]}`)
	})
	peer.SetModels(s.models)
	req := turnRequest("same request with null conversation")
	original := orderedTurnStart(s, context.Background(), scope, req)
	select {
	case <-held:
	case <-time.After(5 * time.Second):
		t.Fatal("model did not enter")
	}
	order := orderedTicketWait(t, s, scope, req.RequestID)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	duplicate := orderedTurnGet(t, orderedTurnStart(peer, ctx, scope, req))
	if !errors.Is(duplicate.err, context.DeadlineExceeded) {
		t.Fatal(duplicate.err)
	}
	conflict := req
	conflict.Text = "changed while pending"
	if _, err := peer.DeskTurn(context.Background(), scope, conflict); !errors.Is(err, memory.ErrConflict) {
		t.Fatal(err)
	}
	unblock()
	first := orderedTurnGet(t, original)
	if first.err != nil {
		t.Fatal(first.err)
	}
	replay := mustTurn(t, peer, scope, req)
	if first.out.ConversationID != replay.ConversationID || first.out.Turn.ID != replay.Turn.ID || calls.Load() != 1 || orderedTicketWait(t, s, scope, req.RequestID) != order {
		t.Fatal("duplicate affected creator/queue identity")
	}
	var count int
	if err := s.pool.QueryRow(context.Background(), "SELECT count(*) FROM desk_turn_order WHERE owner_id=$1 AND request_id=$2", string(scope.OwnerID), req.RequestID).Scan(&count); err != nil || count != 1 {
		t.Fatal(err, count)
	}
}

func orderedTicketStatus(t *testing.T, s *Store, scope memory.Scope, request, want string) {
	t.Helper()
	var status string
	if err := s.pool.QueryRow(context.Background(), "SELECT status FROM desk_turn_order WHERE owner_id=$1 AND request_id=$2", string(scope.OwnerID), request).Scan(&status); err != nil || status != want {
		t.Fatalf("ticket status=%s want=%s err=%v", status, want, err)
	}
}
func orderedAssertCapture(t *testing.T, r orderedTurnResult, text string) {
	t.Helper()
	if r.err != nil || r.out.Turn.Text != text || r.out.Turn.Reply != "" || len(r.out.Turn.Receipts) != 1 || r.out.Turn.Receipts[0].Op != "capture" || r.out.Turn.Receipts[0].Status != "done" || !strings.Contains(r.out.Turn.Receipts[0].Text, "操作未完成") {
		t.Fatalf("incomplete original was not truthfully captured: %v %+v", r.err, r.out.Turn)
	}
}

func TestSecretaryOrderQueuedCancellationAndCaptureRecovery(t *testing.T) {
	for _, position := range []string{"head", "middle"} {
		t.Run(position, func(t *testing.T) {
			s, scope := testStore(t), owner()
			conversation := string(memory.NewID())
			held, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			t.Cleanup(unblock)
			var calls atomic.Int32
			secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
				text := orderedCurrentInput(r)
				calls.Add(1)
				if text == "first" {
					close(held)
					<-release
				}
				secretaryModelReply(w, `{"actions":[{"op":"create_task","title":"new intent"}]}`)
			})
			workspaceCommand(t, s, scope, workspace.Command{Type: "updateSettings", Patch: asJSON(map[string]bool{"autoAccept": true})})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			canceled := turnRequest("请建明天三点旧会议-private-f15")
			canceled.ConversationID = &conversation
			var first <-chan orderedTurnResult
			if position == "middle" {
				req := turnRequest("first")
				req.ConversationID = &conversation
				first = orderedTurnStart(s, context.Background(), scope, req)
				select {
				case <-held:
				case <-time.After(5 * time.Second):
					t.Fatal("no first model")
				}
			} else {
				// A real PostgreSQL execution lock holds the queue head before generation.
				conn, err := s.pool.Acquire(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				defer conn.Release()
				tx, err := conn.Begin(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				defer tx.Rollback(context.Background())
				if err := secretaryTryLock(context.Background(), tx, secretaryConversationKey(string(scope.OwnerID), conversation)); err != nil {
					t.Fatal(err)
				}
				defer tx.Rollback(context.Background())
				// Register cleanup after the creator finishes below, then release this lock.
				unblock = func() { _ = tx.Rollback(context.Background()) }
			}
			failed := orderedTurnStart(s, ctx, scope, canceled)
			order := orderedTicketWait(t, s, scope, canceled.RequestID)
			cancel()
			r := orderedTurnGet(t, failed)
			if !errors.Is(r.err, context.Canceled) {
				t.Fatal("queued cancellation did not return cancellation", r.err)
			}
			orderedTicketStatus(t, s, scope, canceled.RequestID, "canceled")
			next := turnRequest("newer request")
			next.ConversationID = &conversation
			after := orderedTurnStart(s, context.Background(), scope, next)
			orderedTicketWait(t, s, scope, next.RequestID)
			unblock()
			if first != nil {
				if r := orderedTurnGet(t, first); r.err != nil {
					t.Fatal(r.err)
				}
			}
			if r := orderedTurnGet(t, after); r.err != nil {
				t.Fatal(r.err)
			}
			beforeCalls := calls.Load()
			recovered := orderedTurnResult{out: mustTurn(t, s, scope, canceled)}
			orderedAssertCapture(t, recovered, canceled.Text)
			if calls.Load() != beforeCalls || orderedTicketWait(t, s, scope, canceled.RequestID) != order {
				t.Fatal("retired request generated again or obtained a new position")
			}
			replay := mustTurn(t, s, scope, canceled)
			if replay.Turn.ID != recovered.out.Turn.ID || calls.Load() != beforeCalls {
				t.Fatal("capture replay repeated work")
			}
			var sourceID string
			var version int
			if err := s.pool.QueryRow(context.Background(), `SELECT s.id::text,v.version FROM sources s JOIN source_versions v ON(v.owner_id,v.source_id)=(s.owner_id,s.id) WHERE s.owner_id=$1 AND s.connector='desk-incomplete' AND s.external_id=$2`, string(scope.OwnerID), canceled.RequestID).Scan(&sourceID, &version); err != nil {
				t.Fatal(err)
			}
			source, err := s.GetSource(context.Background(), scope, memory.ID(sourceID), version)
			if err != nil || source.Source.Text != canceled.Text || len(source.Processing) != 0 {
				t.Fatalf("source original/processing: %v %+v", err, source)
			}
			var jobs, candidates int
			if err := s.pool.QueryRow(context.Background(), "SELECT count(*) FROM memory_jobs WHERE owner_id=$1 AND record_id=$2", string(scope.OwnerID), sourceID).Scan(&jobs); err != nil || jobs != 0 {
				t.Fatal("retired input has an automatic processing/retry job", err, jobs)
			}
			if err := s.pool.QueryRow(context.Background(), "SELECT count(*) FROM capture_candidates WHERE owner_id=$1 AND source_id=$2 AND state='pending' AND document->>'kind'='unknown' AND document->>'text'=$3", string(scope.OwnerID), sourceID, canceled.Text).Scan(&candidates); err != nil || candidates != 1 {
				t.Fatal("unknown original missing", err, candidates)
			}
			if err := s.Delete(context.Background(), scope, memory.DeleteRequest{Targets: []memory.Ref{{ID: memory.ID(sourceID), Version: version}}, BlockReimport: true}); err != nil {
				t.Fatal(err)
			}
			scrubbed := mustTurn(t, s, scope, canceled)
			if scrubbed.Turn.ID != recovered.out.Turn.ID || scrubbed.Turn.Text != "" || scrubbed.Turn.Reply != "" || len(scrubbed.Turn.Cards) != 0 || len(scrubbed.Turn.Receipts) != 1 || scrubbed.Turn.Receipts[0].Text != "（内容已删除）" || scrubbed.Turn.Receipts[0].Op != "capture" || scrubbed.Turn.Receipts[0].Status != "done" {
				t.Fatal("incomplete source deletion failed to scrub replay", scrubbed.Turn)
			}
			var question, answer string
			var response []byte
			if err := s.pool.QueryRow(context.Background(), "SELECT question,answer,response FROM desk_turns WHERE owner_id=$1 AND request_id=$2", string(scope.OwnerID), canceled.RequestID).Scan(&question, &answer, &response); err != nil || question != "" || answer != "" || strings.Contains(string(response), "private-f15") {
				t.Fatal("deleted original retained in persisted exchange", err, question, answer, string(response))
			}
			var sources int
			if err := s.pool.QueryRow(context.Background(), "SELECT count(*) FROM sources WHERE owner_id=$1 AND connector='desk-incomplete' AND external_id=$2", string(scope.OwnerID), canceled.RequestID).Scan(&sources); err != nil || sources != 0 {
				t.Fatal("scrubbed replay recreated source", err, sources)
			}
		})
	}
}

func TestSecretaryOrderCanceledBeforeAdmission(t *testing.T) {
	s, scope := testStore(t), owner()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := turnRequest("not accepted")
	if _, err := s.DeskTurn(ctx, scope, req); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	var count int
	if err := s.pool.QueryRow(context.Background(), "SELECT count(*) FROM desk_turn_order WHERE owner_id=$1 AND request_id=$2", string(scope.OwnerID), req.RequestID).Scan(&count); err != nil || count != 0 {
		t.Fatal("canceled input was admitted", err, count)
	}
}

func TestSecretaryOrderRecoveryDoesNotDeletePublishedCaptureJob(t *testing.T) {
	s, scope := testStore(t), owner()
	workspaceCommand(t, s, scope, workspace.Command{Type: "updateSettings", Patch: asJSON(map[string]bool{"autoAccept": true})})
	req := turnRequest("old intent")
	conversation := string(memory.NewID())
	req.ConversationID = &conversation
	normal, err := s.Ingest(context.Background(), scope, memory.IngestRequest{Connector: "capture", ExternalID: req.RequestID, ExternalVersion: "1", Title: "快速记录", Text: req.Text, MediaType: "text/plain"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	hash := sha256.Sum256(asJSON(req))
	if _, err := s.admitSecretaryTurn(ctx, ctx, string(scope.OwnerID), req.RequestID, conversation, hash[:]); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(context.Background(), "UPDATE desk_turn_order SET status='failed' WHERE owner_id=$1 AND request_id=$2", string(scope.OwnerID), req.RequestID); err != nil {
		t.Fatal(err)
	}
	orderedAssertCapture(t, orderedTurnResult{out: mustTurn(t, s, scope, req)}, req.Text)
	var count int
	if err := s.pool.QueryRow(context.Background(), "SELECT count(*) FROM memory_jobs WHERE owner_id=$1 AND record_id=$2 AND record_version=$3 AND stage='source.chunk' AND state='queued'", string(scope.OwnerID), string(normal.ID), normal.Version).Scan(&count); err != nil || count != 1 {
		t.Fatal("published ordinary capture job was removed", err, count)
	}
}

func TestSecretaryOrderRecoveryDuplicateDoesNotSuppressPublishedJobs(t *testing.T) {
	s, scope := testStore(t), owner()
	if _, err := s.Snapshot(context.Background(), scope); err != nil {
		t.Fatal(err)
	}
	req := turnRequest("conflicting recovery identity")
	conversation := string(memory.NewID())
	req.ConversationID = &conversation
	source, err := s.Ingest(context.Background(), scope, memory.IngestRequest{Connector: "desk-incomplete", ExternalID: req.RequestID, ExternalVersion: "1", Title: "未完成的秘书原话", Text: req.Text, MediaType: "text/plain"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	hash := sha256.Sum256(asJSON(req))
	if _, err := s.admitSecretaryTurn(ctx, ctx, string(scope.OwnerID), req.RequestID, conversation, hash[:]); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(context.Background(), "UPDATE desk_turn_order SET status='failed' WHERE owner_id=$1 AND request_id=$2", string(scope.OwnerID), req.RequestID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DeskTurn(context.Background(), scope, req); !errors.Is(err, memory.ErrConflict) {
		t.Fatal("source identity collision was falsely acknowledged", err)
	}
	var count int
	if err := s.pool.QueryRow(context.Background(), "SELECT count(*) FROM memory_jobs WHERE owner_id=$1 AND record_id=$2 AND stage='source.chunk' AND state='queued'", string(scope.OwnerID), string(source.ID)).Scan(&count); err != nil || count != 1 {
		t.Fatal("preexisting published job suppressed", err, count)
	}
	orderedTicketStatus(t, s, scope, req.RequestID, "failed")
}

func TestSecretaryOrderConnectionLossThenNewStoreContinues(t *testing.T) {
	s, scope := testStore(t), owner()
	peer := orderedPeer(t, s)
	conversation := string(memory.NewID())
	workspaceCommand(t, s, scope, workspace.Command{Type: "updateSettings", Patch: asJSON(map[string]string{"timezone": "UTC"})})
	base := workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "surviving meeting", Due: "2026-10-02T15:00:00Z"})
	id := base.Tasks[0].ID
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	var calls atomic.Int32
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		text := orderedCurrentInput(r)
		calls.Add(1)
		if text == "old failed update" {
			close(entered)
			<-release
			secretaryModelReply(w, `{"actions":[{"op":"update","ref":"THIS","set":{"due":"2026-10-02T16:00"}}]}`)
			return
		}
		secretaryModelReply(w, `{"actions":[{"op":"update","ref":"THIS","set":{"due":"2026-10-02T17:00"}}]}`)
	})
	peer.SetModels(s.models)
	req := turnRequest("old failed update")
	req.ConversationID = &conversation
	req.ThingID = &id
	first := orderedTurnStart(s, context.Background(), scope, req)
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("first model never entered")
	}
	var pid int
	if err := peer.pool.QueryRow(context.Background(), `SELECT pid FROM pg_locks WHERE locktype='advisory' AND granted AND ((classid::bigint<<32)|objid::bigint)=hashtextextended($1,0)`, secretaryConversationKey(string(scope.OwnerID), conversation)).Scan(&pid); err != nil {
		t.Fatal(err)
	}
	var terminated bool
	if err := peer.pool.QueryRow(context.Background(), "SELECT pg_terminate_backend($1)", pid).Scan(&terminated); err != nil || !terminated {
		t.Fatal("failed to terminate own generation connection", err, terminated)
	}
	unblock()
	failed := orderedTurnGet(t, first)
	if failed.err == nil {
		t.Fatal("lost transaction falsely returned success", failed.out.Turn)
	}
	orderedTicketStatus(t, s, scope, req.RequestID, "failed")
	restarted := orderedPeer(t, s)
	restarted.SetModels(s.models)
	next := turnRequest("newest update")
	next.ConversationID = &conversation
	next.ThingID = &id
	out := mustTurn(t, restarted, scope, next)
	if len(out.State.Tasks) != 1 || out.State.Tasks[0].Due != "2026-10-02T17:00:00Z" || out.Turn.Receipts[0].Status != "done" {
		t.Fatal("restart failed to advance business state", out.Turn, out.State.Tasks)
	}
	recovered := mustTurn(t, restarted, scope, req)
	orderedAssertCapture(t, orderedTurnResult{out: recovered}, req.Text)
	if calls.Load() != 2 || recovered.State.Tasks[0].Due != "2026-10-02T17:00:00Z" {
		t.Fatal("failed old request overwrote newer update", calls.Load(), recovered.State.Tasks)
	}
	history, err := restarted.DeskTurns(context.Background(), scope, conversation)
	if err != nil || len(history.Turns) != 2 || history.Turns[0].Text != next.Text || history.Turns[1].Text != req.Text || history.Turns[1].Receipts[0].Op != "capture" {
		t.Fatal("saved history falsely claimed original action completed", err, history)
	}
	t.Logf("terminated own generation PG pid %d; lost transaction failed; new Store committed 17:00; old request retry only captured", pid)
}

func TestSecretaryOrderExpiredAdmissionSurvivesStoreRestart(t *testing.T) {
	s, scope := testStore(t), owner()
	if _, err := s.Snapshot(context.Background(), scope); err != nil {
		t.Fatal(err)
	}
	req := turnRequest("abandoned old intent")
	conversation := string(memory.NewID())
	req.ConversationID = &conversation
	hash := sha256.Sum256(asJSON(req))
	// Commit a genuine admission without an executor, representing death between
	// admission and execution. Accelerate only its deadline, without fabricating a
	// completed turn or relying on an actual two-minute wall-clock pause.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	ticket, err := s.admitSecretaryTurn(ctx, ctx, string(scope.OwnerID), req.RequestID, conversation, hash[:])
	if err != nil || !ticket.own {
		t.Fatal(err, ticket)
	}
	var expires time.Time
	if err := s.pool.QueryRow(context.Background(), "SELECT expires_at FROM desk_turn_order WHERE owner_id=$1 AND request_id=$2", string(scope.OwnerID), req.RequestID).Scan(&expires); err != nil {
		t.Fatal(err)
	}
	deadline, _ := ctx.Deadline()
	if !expires.Equal(deadline.Truncate(time.Microsecond)) {
		t.Fatalf("expiry did not retain initial deadline: %s %s", expires, deadline)
	}
	if _, err := s.pool.Exec(context.Background(), "UPDATE desk_turn_order SET expires_at=clock_timestamp()-interval '1 second' WHERE owner_id=$1 AND request_id=$2", string(scope.OwnerID), req.RequestID); err != nil {
		t.Fatal(err)
	}
	restarted := orderedPeer(t, s)
	var calls atomic.Int32
	secretaryModel(t, restarted, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		secretaryModelReply(w, `{"actions":[{"op":"create_task","title":"new intent after restart"}]}`)
	})
	// Prove expiry cannot bypass a still-held transaction fence.
	conn, err := s.pool.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	tx, err := conn.Begin(context.Background())
	if err != nil {
		conn.Release()
		t.Fatal(err)
	}
	if err := secretaryTryLock(context.Background(), tx, secretaryConversationKey(string(scope.OwnerID), conversation)); err != nil {
		t.Fatal(err)
	}
	if err := restarted.expireSecretaryTickets(context.Background(), string(scope.OwnerID), conversation); err != nil {
		t.Fatal(err)
	}
	orderedTicketStatus(t, s, scope, req.RequestID, "pending")
	_ = tx.Rollback(context.Background())
	conn.Release()
	next := turnRequest("latest independent accepted intent")
	next.ConversationID = &conversation
	out := mustTurn(t, restarted, scope, next)
	if len(out.State.Tasks) != 1 || out.Turn.Receipts[0].Status != "done" {
		t.Fatal("expired head blocked restarted Store", out)
	}
	orderedTicketStatus(t, restarted, scope, req.RequestID, "expired")
	before := orderedTicketWait(t, restarted, scope, req.RequestID)
	old := mustTurn(t, restarted, scope, req)
	orderedAssertCapture(t, orderedTurnResult{out: old}, req.Text)
	if calls.Load() != 1 || orderedTicketWait(t, restarted, scope, req.RequestID) != before || len(old.State.Tasks) != 1 {
		t.Fatal("expired retry revived or requeued old intent")
	}
	orderedTicketStatus(t, restarted, scope, req.RequestID, "done")
}
