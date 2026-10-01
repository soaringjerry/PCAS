package postgres

import (
	"context"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/notify"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// Expectations come from stabilization R1–R11 and phase1 contracts §3/§4.
// Model outputs are synthetic wire JSON; no product date helpers compute wants.
func stabilizationTimeTurn(t *testing.T, s *Store, scope memory.Scope, action map[string]any) workspace.DeskTurnResponse {
	t.Helper()
	secretaryModel(t, s, func(w http.ResponseWriter, _ *http.Request) {
		secretaryModelReply(w, map[string]any{"actions": []any{action}})
	})
	return mustTurn(t, s, scope, turnRequest("合成时间序列"))
}

func stabilizationTimeTask(t *testing.T, state workspace.State, id string) workspace.Item {
	t.Helper()
	for _, task := range state.Tasks {
		if id == "" || task.ID == id {
			return task
		}
	}
	t.Fatalf("task %q absent", id)
	return workspace.Item{}
}

func stabilizationTimeReminder(t *testing.T, task workspace.Item, due, next, offset string) {
	t.Helper()
	if task.Due != due || len(task.Triggers) != 1 {
		t.Fatalf("due/reminder: got due=%q triggers=%+v; want due=%q next=%q offset=%q", task.Due, task.Triggers, due, next, offset)
	}
	tr := task.Triggers[0]
	if tr.ID != "due-reminder" || tr.Kind != "time" || !tr.Active || tr.NextAt != next || tr.Offset != offset {
		t.Errorf("reminder: got %+v; want active due-reminder next=%q offset=%q", tr, next, offset)
	}
}

func stabilizationTimeSnapshot(t *testing.T, s *Store, scope memory.Scope) workspace.State {
	t.Helper()
	state, err := s.Snapshot(context.Background(), scope)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

type stabilizationTimeChannel struct {
	calls []notify.Message
	err   error
}

type stabilizationTimeTransport struct {
	target   *url.URL
	delegate http.RoundTripper
}

func (tr stabilizationTimeTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	local := request.Clone(request.Context())
	address := *request.URL
	address.Scheme, address.Host = tr.target.Scheme, tr.target.Host
	local.URL = &address
	return tr.delegate.RoundTrip(local)
}

func (*stabilizationTimeChannel) Name() string { return "synthetic-mobile" }
func (c *stabilizationTimeChannel) Send(_ context.Context, m notify.Message) error {
	c.calls = append(c.calls, m)
	return c.err
}

func stabilizationTimeCheck(t *testing.T, s *Store, at time.Time, channels ...notify.Channel) {
	t.Helper()
	var existing []string
	rows, err := s.pool.Query(context.Background(), "SELECT id::text FROM workspace_notices")
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		existing = append(existing, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if existing == nil {
		existing = []string{}
	}
	if err := s.CheckReminders(context.Background(), at); err != nil {
		t.Fatal(err)
	}
	// The database DEFAULT now() uses wall time, whereas the checker accepts a
	// synthetic clock. Align only newly inserted notice bookkeeping with that
	// clock so dispatch observes the same time; no business fields are changed.
	if _, err := s.pool.Exec(context.Background(), "UPDATE workspace_notices SET created_at=$1 WHERE NOT (id=ANY($2::uuid[]))", at, existing); err != nil {
		t.Fatal(err)
	}
	if err := s.DispatchNotices(context.Background(), at, channels); err != nil {
		t.Fatal(err)
	}
}

// SQL changes are fixture setup against schema/public Item JSON only. This lets
// historical completion scenarios run without changing the process clock.
func stabilizationTimeSeedSchedule(t *testing.T, s *Store, scope memory.Scope, task workspace.Item, due, next time.Time) {
	t.Helper()
	task.Due = due.UTC().Format(time.RFC3339)
	task.Triggers = []workspace.Trigger{{ID: "due-reminder", Kind: "time", Description: task.Title, Active: true, Offset: "at", NextAt: next.UTC().Format(time.RFC3339)}}
	_, err := s.pool.Exec(context.Background(), "UPDATE work_items SET due_at=$3, document=$4 WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), task.ID, due, asJSON(task))
	if err != nil {
		t.Fatal(err)
	}
}

func TestR1_ElapsedLeadFallsBackAndElapsedDueHasNoReminder(t *testing.T) {
	for _, past := range []bool{false, true} {
		name := "future_due"
		if past {
			name = "elapsed_due"
		}
		t.Run(name, func(t *testing.T) {
			s := testStore(t)
			scope := owner()
			workspaceCommand(t, s, scope, workspace.Command{Type: "updateSettings", Patch: asJSON(map[string]string{"timezone": "UTC"})})
			due := time.Now().UTC().Truncate(time.Second).Add(5 * time.Minute)
			if past {
				due = due.Add(-10 * time.Minute)
			}
			out := stabilizationTimeTurn(t, s, scope, map[string]any{"op": "create_task", "title": "喝水", "due": due.Format(time.RFC3339)})
			task := stabilizationTimeTask(t, out.State, "")
			if past {
				if task.Due != due.Format(time.RFC3339) || len(task.Triggers) != 0 || len(out.Turn.Receipts) != 1 || !strings.Contains(out.Turn.Receipts[0].Text, "时间已过，没有设提醒") {
					t.Fatalf("elapsed due: due=%q triggers=%+v receipts=%+v", task.Due, task.Triggers, out.Turn.Receipts)
				}
			} else {
				stabilizationTimeReminder(t, task, due.Format(time.RFC3339), due.Format(time.RFC3339), "-30m")
			}
			channel := &stabilizationTimeChannel{}
			stabilizationTimeCheck(t, s, due.Add(-time.Second), channel)
			if len(channel.calls) != 0 || len(stabilizationTimeSnapshot(t, s, scope).Notices) != 0 {
				t.Fatal("sent before fallback due or for elapsed due")
			}
			stabilizationTimeCheck(t, s, due, channel)
			want := 1
			if past {
				want = 0
			}
			if len(channel.calls) != want || len(stabilizationTimeSnapshot(t, s, scope).Notices) != want {
				t.Fatalf("calls=%d want=%d", len(channel.calls), want)
			}
		})
	}
}

func TestR2_DateOnlyCreatedAfterNineFallsBackTo2359(t *testing.T) {
	s := testStore(t)
	scope := owner()
	var loc *time.Location
	for _, zone := range []string{"UTC", "Asia/Shanghai", "America/New_York", "Pacific/Honolulu"} {
		candidate, err := time.LoadLocation(zone)
		if err != nil {
			t.Fatal(err)
		}
		if h := time.Now().In(candidate).Hour(); h >= 10 && h <= 20 {
			loc = candidate
			break
		}
	}
	if loc == nil {
		t.Fatal("no daytime fixture timezone")
	}
	workspaceCommand(t, s, scope, workspace.Command{Type: "updateSettings", Patch: asJSON(map[string]string{"timezone": loc.String()})})
	localNow := time.Now().In(loc)
	due := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 23, 59, 0, 0, loc)
	out := stabilizationTimeTurn(t, s, scope, map[string]any{"op": "create_task", "title": "日期提醒", "due": localNow.Format("2006-01-02")})
	stabilizationTimeReminder(t, stabilizationTimeTask(t, out.State, ""), due.UTC().Format(time.RFC3339), due.UTC().Format(time.RFC3339), "09:00")
	channel := &stabilizationTimeChannel{}
	stabilizationTimeCheck(t, s, due.Add(-time.Second), channel)
	if len(channel.calls) != 0 {
		t.Fatal("date-only reminder sent before 23:59")
	}
	stabilizationTimeCheck(t, s, due, channel)
	if len(channel.calls) != 1 || len(stabilizationTimeSnapshot(t, s, scope).Notices) != 1 {
		t.Fatal("date-only fallback reminder absent")
	}
	nextDate := localNow.AddDate(0, 0, 1)
	nextDue := time.Date(nextDate.Year(), nextDate.Month(), nextDate.Day(), 23, 59, 0, 0, loc)
	nextReminder := time.Date(nextDate.Year(), nextDate.Month(), nextDate.Day(), 9, 0, 0, 0, loc)
	out = stabilizationTimeTurn(t, s, scope, map[string]any{"op": "update", "ref": "T1", "set": map[string]any{"due": nextDate.Format("2006-01-02")}})
	stabilizationTimeReminder(t, stabilizationTimeTask(t, out.State, ""), nextDue.UTC().Format(time.RFC3339), nextReminder.UTC().Format(time.RFC3339), "09:00")
}

func TestR3_RescheduleRetainsLeadAndRemovingDueDeletesReminder(t *testing.T) {
	s := testStore(t)
	scope := owner()
	workspaceCommand(t, s, scope, workspace.Command{Type: "updateSettings", Patch: asJSON(map[string]string{"timezone": "Australia/Melbourne"})})
	out := stabilizationTimeTurn(t, s, scope, map[string]any{"op": "create_task", "title": "改期", "due": "2099-01-05T15:00", "remind": "-2h"})
	task := stabilizationTimeTask(t, out.State, "")
	stabilizationTimeReminder(t, task, "2099-01-05T04:00:00Z", "2099-01-05T02:00:00Z", "-2h")
	out = stabilizationTimeTurn(t, s, scope, map[string]any{"op": "update", "ref": "T1", "set": map[string]any{"due": "2099-01-06T16:00"}})
	stabilizationTimeReminder(t, stabilizationTimeTask(t, out.State, task.ID), "2099-01-06T05:00:00Z", "2099-01-06T03:00:00Z", "-2h")
	channel := &stabilizationTimeChannel{}
	stabilizationTimeCheck(t, s, time.Date(2099, 1, 5, 2, 0, 0, 0, time.UTC), channel)
	if len(channel.calls) != 0 || len(stabilizationTimeSnapshot(t, s, scope).Notices) != 0 {
		t.Fatal("old reminder survived rescheduling")
	}
	out = stabilizationTimeTurn(t, s, scope, map[string]any{"op": "update", "ref": "T1", "set": map[string]any{"due": ""}})
	task = stabilizationTimeTask(t, out.State, task.ID)
	if task.Due != "" || len(task.Triggers) != 0 {
		t.Fatalf("removed due retained reminder: due=%q triggers=%+v", task.Due, task.Triggers)
	}
	stabilizationTimeCheck(t, s, time.Date(2099, 1, 6, 5, 0, 0, 0, time.UTC), channel)
	if len(channel.calls) != 0 || len(stabilizationTimeSnapshot(t, s, scope).Notices) != 0 {
		t.Fatal("removed reminder generated notification")
	}
}

func TestR4_CompletedBeforeReminderNeverSends(t *testing.T) {
	s := testStore(t)
	scope := owner()
	state := workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "已完成事项"})
	task := stabilizationTimeTask(t, state, "")
	due := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	stabilizationTimeSeedSchedule(t, s, scope, task, due, due)
	workspaceCommand(t, s, scope, workspace.Command{Type: "setTaskStatus", ID: task.ID, Status: "done"})
	channel := &stabilizationTimeChannel{}
	stabilizationTimeCheck(t, s, due, channel)
	stabilizationTimeCheck(t, s, due.Add(time.Minute), channel)
	state = stabilizationTimeSnapshot(t, s, scope)
	if len(channel.calls) != 0 || len(state.Notices) != 0 || stabilizationTimeTask(t, state, task.ID).Status != "done" {
		t.Fatalf("completed task delivered: calls=%d notices=%+v", len(channel.calls), state.Notices)
	}
}

func TestR5_UndoCompletionAfterOneHourNeverCatchesUp(t *testing.T) {
	s := testStore(t)
	scope := owner()
	now := time.Now().UTC().Truncate(time.Second)
	state := workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "历史完成事项"})
	task := stabilizationTimeTask(t, state, "")
	stabilizationTimeSeedSchedule(t, s, scope, task, now.Add(-2*time.Hour), now.Add(-2*time.Hour))
	actionID := string(memory.NewID())
	state = stabilizationTimeSnapshot(t, s, scope)
	if _, err := s.Execute(context.Background(), scope, workspace.Command{Type: "setTaskStatus", ID: task.ID, Status: "done", RequestID: actionID, ExpectedRevision: state.Revision}); err != nil {
		t.Fatal(err)
	}
	// The action is a synthetic completion three hours ago; Undo remains public.
	if _, err := s.pool.Exec(context.Background(), "UPDATE action_log SET created_at=$3 WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), actionID, now.Add(-3*time.Hour)); err != nil {
		t.Fatal(err)
	}
	channel := &stabilizationTimeChannel{}
	stabilizationTimeCheck(t, s, now.Add(-time.Hour), channel)
	if len(channel.calls) != 0 {
		t.Fatal("completed historical task sent")
	}
	state, err := s.Undo(context.Background(), scope, actionID)
	if err != nil || stabilizationTimeTask(t, state, task.ID).Status != "todo" {
		t.Fatal("completion undo failed", err)
	}
	stabilizationTimeCheck(t, s, now, channel)
	state = stabilizationTimeSnapshot(t, s, scope)
	if len(channel.calls) != 0 || len(state.Notices) != 0 {
		t.Fatalf("stale reminder caught up after undo: calls=%d notices=%+v", len(channel.calls), state.Notices)
	}
	if stabilizationTimeTask(t, state, task.ID).Due != now.Add(-2*time.Hour).Format(time.RFC3339) {
		t.Fatal("overdue todo lost its due time; homepage waiting eligibility unavailable")
	}
}

func TestR6_NonexistentMelbourne0230Becomes0330(t *testing.T) {
	s := testStore(t)
	scope := owner()
	workspaceCommand(t, s, scope, workspace.Command{Type: "updateSettings", Patch: asJSON(map[string]string{"timezone": "Australia/Melbourne"})})
	out := stabilizationTimeTurn(t, s, scope, map[string]any{"op": "create_task", "title": "夏令时安排", "due": "2099-10-04T02:30", "remind": "at"})
	task := stabilizationTimeTask(t, out.State, "")
	// Independent UTC fixture: 03:30 AEDT is 16:30Z the previous day.
	if task.Due != "2099-10-03T16:30:00Z" || len(out.Turn.Receipts) != 1 || !strings.Contains(out.Turn.Receipts[0].Text, "03:30") || strings.Contains(out.Turn.Receipts[0].Text, "02:30") {
		t.Fatalf("DST gap: due=%q receipts=%+v; want 03:30 AEDT / 16:30Z", task.Due, out.Turn.Receipts)
	}
}

func TestR7_ReminderAcrossDSTUsesMondayLocalTime(t *testing.T) {
	s := testStore(t)
	scope := owner()
	workspaceCommand(t, s, scope, workspace.Command{Type: "updateSettings", Patch: asJSON(map[string]string{"timezone": "Australia/Melbourne"})})
	out := stabilizationTimeTurn(t, s, scope, map[string]any{"op": "create_task", "title": "跨夏令时", "due": "2099-10-05T09:00", "remind": "at"})
	task := stabilizationTimeTask(t, out.State, "")
	stabilizationTimeReminder(t, task, "2099-10-04T22:00:00Z", "2099-10-04T22:00:00Z", "at")
	channel := &stabilizationTimeChannel{}
	for _, at := range []string{"2099-10-03T00:00:00Z", "2099-10-04T21:59:59Z"} {
		instant, err := time.Parse(time.RFC3339, at)
		if err != nil {
			t.Fatal(err)
		}
		stabilizationTimeCheck(t, s, instant, channel)
	}
	if len(channel.calls) != 0 || len(stabilizationTimeSnapshot(t, s, scope).Notices) != 0 {
		t.Fatal("DST reminder sent early")
	}
	instant := time.Date(2099, 10, 4, 22, 0, 0, 0, time.UTC)
	stabilizationTimeCheck(t, s, instant, channel)
	state := stabilizationTimeSnapshot(t, s, scope)
	if len(channel.calls) != 1 || len(state.Notices) != 1 || !strings.Contains(channel.calls[0].Body, "2099-10-05 09:00 AEDT") {
		t.Fatalf("DST delivery: calls=%+v notices=%+v", channel.calls, state.Notices)
	}
	actualDue, err := time.Parse(time.RFC3339, state.Notices[0].DueAt)
	if err != nil || !actualDue.Equal(instant) {
		t.Fatalf("DST notice due=%q want=%s err=%v", state.Notices[0].DueAt, instant, err)
	}
}

func TestR8_ChangingTimezonePreservesUTCAndChangesFutureInterpretation(t *testing.T) {
	s := testStore(t)
	scope := owner()
	workspaceCommand(t, s, scope, workspace.Command{Type: "updateSettings", Patch: asJSON(map[string]string{"timezone": "Australia/Melbourne"})})
	out := stabilizationTimeTurn(t, s, scope, map[string]any{"op": "create_task", "title": "原事项", "due": "2099-01-05T09:00"})
	before := stabilizationTimeTask(t, out.State, "")
	stabilizationTimeReminder(t, before, "2099-01-04T22:00:00Z", "2099-01-04T21:30:00Z", "-30m")
	workspaceCommand(t, s, scope, workspace.Command{Type: "updateSettings", Patch: asJSON(map[string]string{"timezone": "Asia/Shanghai"})})
	after := stabilizationTimeTask(t, stabilizationTimeSnapshot(t, s, scope), before.ID)
	if after.Due != before.Due || string(asJSON(after.Triggers)) != string(asJSON(before.Triggers)) {
		t.Fatalf("timezone translated persisted UTC: before=%+v after=%+v", before, after)
	}
	out = stabilizationTimeTurn(t, s, scope, map[string]any{"op": "create_task", "title": "新事项", "due": "2099-01-05T09:00"})
	var next workspace.Item
	for _, task := range out.State.Tasks {
		if task.Title == "新事项" {
			next = task
		}
	}
	stabilizationTimeReminder(t, next, "2099-01-05T01:00:00Z", "2099-01-05T00:30:00Z", "-30m")
	if len(out.Turn.Receipts) != 1 || !strings.Contains(out.Turn.Receipts[0].Text, "09:00 新事项") || !strings.Contains(out.Turn.Receipts[0].Text, "08:30 提醒") {
		t.Fatalf("new-timezone receipt: %+v", out.Turn.Receipts)
	}
	channel := &stabilizationTimeChannel{}
	stabilizationTimeCheck(t, s, time.Date(2099, 1, 4, 21, 30, 0, 0, time.UTC), channel)
	if len(channel.calls) != 1 || !strings.Contains(channel.calls[0].Body, "2099-01-05 05:30 CST") {
		t.Fatalf("existing reminder display did not use new timezone: %+v", channel.calls)
	}
}

func TestR9_RestartAndReentryNeverDuplicateNoticeOrSend(t *testing.T) {
	s := testStore(t)
	scope := owner()
	state := workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "重启提醒"})
	task := stabilizationTimeTask(t, state, "")
	now := time.Now().UTC().Truncate(time.Second)
	stabilizationTimeSeedSchedule(t, s, scope, task, now, now)
	channel := &stabilizationTimeChannel{}
	stabilizationTimeCheck(t, s, now, channel)
	first := stabilizationTimeSnapshot(t, s, scope)
	if len(first.Notices) != 1 || len(channel.calls) != 1 {
		t.Fatalf("initial notice/send missing: notices=%+v sends=%d", first.Notices, len(channel.calls))
	}
	// Close the pool and reopen the same test schema, retaining only DB state.
	dsn := s.pool.Config().ConnString()
	s.Close()
	restarted, err := Open(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restarted.Close)
	for range 2 {
		stabilizationTimeCheck(t, restarted, now.Add(time.Minute), channel)
	}
	state = stabilizationTimeSnapshot(t, restarted, scope)
	var count int
	if err := restarted.pool.QueryRow(context.Background(), "SELECT count(*) FROM workspace_notices WHERE owner_id=$1 AND thing_id=$2", string(scope.OwnerID), task.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 || len(state.Notices) != 1 || state.Notices[0].ID != first.Notices[0].ID || len(channel.calls) != 1 {
		t.Fatalf("restart duplicated: rows=%d notices=%+v sends=%d", count, state.Notices, len(channel.calls))
	}
}

func TestR10_TelegramStopsAfterFiveFailuresAndRecordsCause(t *testing.T) {
	s := testStore(t)
	scope := owner()
	logs := secretaryLogs(t)
	now := time.Now().UTC().Truncate(time.Second)
	state := workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "发送失败仍到点"})
	task := stabilizationTimeTask(t, state, "")
	stabilizationTimeSeedSchedule(t, s, scope, task, now, now)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		http.Error(w, "synthetic Telegram outage", http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)
	settings := notify.Settings{Path: filepath.Join(t.TempDir(), "notify.json")}
	if err := settings.SaveTelegram("synthetic-T2-token", "123"); err != nil {
		t.Fatal(err)
	}
	telegram := &notify.Telegram{Settings: settings, Client: server.Client(), BaseURL: server.URL}
	healthy := &stabilizationTimeChannel{}
	for attempt := range 8 {
		stabilizationTimeCheck(t, s, now.Add(time.Duration(attempt)*time.Hour), telegram, healthy)
	}
	state = stabilizationTimeSnapshot(t, s, scope)
	t.Run("retry_limit_and_notice", func(t *testing.T) {
		if calls.Load() != 5 || len(healthy.calls) != 1 || len(state.Notices) != 1 || state.Notices[0].DismissedAt != "" || stabilizationTimeTask(t, state, task.ID).Status != "todo" {
			t.Fatalf("exhaustion/home data: sends=%d notices=%+v", calls.Load(), state.Notices)
		}
	})
	t.Run("failure_log", func(t *testing.T) {
		found := false
		var retries, stopped int
		for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
			var record map[string]any
			if json.Unmarshal([]byte(line), &record) == nil && record["channel"] == "telegram" {
				if category, ok := record["error_type"].(string); ok && category != "" && category != "none" {
					found = true
					if record["status"] == "retry" {
						retries++
					}
					if record["status"] == "stopped" && record["attempt"] == float64(5) {
						stopped++
					}
				}
			}
		}
		if !found || retries != 4 || stopped != 1 {
			t.Fatalf("missing log with Telegram channel and error type; sends=%d logs=%s", calls.Load(), logs.String())
		}
		for _, sensitive := range []string{"synthetic-T2-token", server.URL, "synthetic Telegram outage", task.Title, task.ID} {
			if strings.Contains(logs.String(), sensitive) {
				t.Errorf("sensitive notification value in logs: %q", sensitive)
			}
		}
	})
}

func TestR11_ExpiredPushDeletedAndTelegramStillDelivers(t *testing.T) {
	s := testStore(t)
	scope := owner()
	var pushCalls, telegramCalls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/expired" {
			pushCalls.Add(1)
			w.WriteHeader(http.StatusGone)
			return
		}
		telegramCalls.Add(1)
		_, _ = w.Write([]byte(`{"ok":true,"result":{}}`))
	}))
	t.Cleanup(server.Close)
	settings := notify.Settings{Path: filepath.Join(t.TempDir(), "notify.json")}
	if err := settings.SaveTelegram("synthetic-T2-token", "123"); err != nil {
		t.Fatal(err)
	}
	_, x, y, err := elliptic.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	auth := make([]byte, 16)
	if _, err := rand.Read(auth); err != nil {
		t.Fatal(err)
	}
	n := NewNotifier(s, settings, "https://example.invalid")
	// Use a valid public-shaped URL at the API boundary; route its HTTP request
	// exclusively to the local TLS fixture. No external DNS/network is used.
	sub := webpush.Subscription{Endpoint: "https://push.example.invalid/expired", Keys: webpush.Keys{P256dh: base64.RawURLEncoding.EncodeToString(elliptic.Marshal(elliptic.P256(), x, y)), Auth: base64.RawURLEncoding.EncodeToString(auth)}}
	if err := n.SavePushSubscription(context.Background(), scope, sub); err != nil {
		t.Fatal(err)
	}
	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	pushClient := &http.Client{Transport: stabilizationTimeTransport{target: target, delegate: server.Client().Transport}}
	push := &notify.WebPush{Settings: settings, Store: s, PublicURL: "https://example.invalid", Client: pushClient}
	telegram := &notify.Telegram{Settings: settings, BaseURL: server.URL, Client: server.Client()}
	now := time.Now().UTC().Truncate(time.Second)
	state := workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "失效订阅"})
	stabilizationTimeSeedSchedule(t, s, scope, stabilizationTimeTask(t, state, ""), now, now)
	stabilizationTimeCheck(t, s, now, push, telegram)
	stabilizationTimeCheck(t, s, now.Add(time.Hour), push, telegram)
	subs, err := s.Subscriptions(context.Background(), string(scope.OwnerID))
	state = stabilizationTimeSnapshot(t, s, scope)
	if err != nil || len(subs) != 0 || pushCalls.Load() != 1 || telegramCalls.Load() != 1 || len(state.Notices) != 1 {
		t.Fatalf("expired push interfered with channels: subs=%+v err=%v push=%d telegram=%d notices=%+v", subs, err, pushCalls.Load(), telegramCalls.Load(), state.Notices)
	}
}

func TestR1_FallbackRescheduleRestoresOriginalLead(t *testing.T) {
	s := testStore(t)
	scope := owner()
	now := time.Now().UTC().Truncate(time.Second)
	out := stabilizationTimeTurn(t, s, scope, map[string]any{"op": "create_task", "title": "恢复提前量", "due": now.Add(5 * time.Minute).Format(time.RFC3339)})
	task := stabilizationTimeTask(t, out.State, "")
	stabilizationTimeReminder(t, task, now.Add(5*time.Minute).Format(time.RFC3339), now.Add(5*time.Minute).Format(time.RFC3339), "-30m")
	out = stabilizationTimeTurn(t, s, scope, map[string]any{"op": "update", "ref": "T1", "set": map[string]any{"due": now.Add(2 * time.Hour).Format(time.RFC3339)}})
	stabilizationTimeReminder(t, stabilizationTimeTask(t, out.State, task.ID), now.Add(2*time.Hour).Format(time.RFC3339), now.Add(90*time.Minute).Format(time.RFC3339), "-30m")
	out = stabilizationTimeTurn(t, s, scope, map[string]any{"op": "update", "ref": "T1", "set": map[string]any{"due": now.Add(-time.Minute).Format(time.RFC3339)}})
	if len(stabilizationTimeTask(t, out.State, task.ID).Triggers) != 0 || !strings.Contains(out.Turn.Receipts[0].Text, "时间已过，没有设提醒") {
		t.Fatalf("elapsed reschedule: task=%+v receipt=%+v", out.State.Tasks, out.Turn.Receipts)
	}
}

func TestR2_MorningAfternoonAndRescheduleKeepClockPreference(t *testing.T) {
	// Explicit expected local/UTC fixtures keep both sides of 09:00 stable.
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	for _, fixture := range []struct{ now, next string }{
		{"2099-01-05T08:00:00+08:00", "2099-01-05T01:00:00Z"},
		{"2099-01-05T09:00:00+08:00", "2099-01-05T15:59:00Z"},
		{"2099-01-05T15:00:00+08:00", "2099-01-05T15:59:00Z"},
	} {
		t.Run(fixture.now, func(t *testing.T) {
			now, err := time.Parse(time.RFC3339, fixture.now)
			if err != nil {
				t.Fatal(err)
			}
			task := workspace.Item{Title: "日期偏好", Due: "2099-01-05T15:59:00Z"}
			applyDueReminderAt(&task, "09:00", loc, now)
			stabilizationTimeReminder(t, task, "2099-01-05T15:59:00Z", fixture.next, "09:00")
			task.Due = "2099-01-06T15:59:00Z"
			applyDueReminderAt(&task, "", loc, now)
			stabilizationTimeReminder(t, task, "2099-01-06T15:59:00Z", "2099-01-06T01:00:00Z", "09:00")
		})
	}
}

func stabilizationTimeComplete(t *testing.T, s *Store, scope memory.Scope, taskID string) string {
	t.Helper()
	state := stabilizationTimeSnapshot(t, s, scope)
	id := string(memory.NewID())
	if _, err := s.Execute(context.Background(), scope, workspace.Command{Type: "setTaskStatus", ID: taskID, Status: "done", RequestID: id, ExpectedRevision: state.Revision}); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestR5_SuppressionSurvivesRestartAndContinuousUndo(t *testing.T) {
	s := testStore(t)
	scope := owner()
	now := time.Now().UTC().Truncate(time.Second)
	task := stabilizationTimeTask(t, workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "原标题"}), "")
	stabilizationTimeSeedSchedule(t, s, scope, task, now.Add(-2*time.Hour), now.Add(-2*time.Hour))
	state := stabilizationTimeSnapshot(t, s, scope)
	renameID := string(memory.NewID())
	if _, err := s.Execute(context.Background(), scope, workspace.Command{Type: "renameThing", ID: task.ID, Title: "改名", RequestID: renameID, ExpectedRevision: state.Revision}); err != nil {
		t.Fatal(err)
	}
	completeID := stabilizationTimeComplete(t, s, scope, task.ID)
	if _, err := s.Undo(context.Background(), scope, completeID); err != nil {
		t.Fatal(err)
	}
	state, err := s.Undo(context.Background(), scope, renameID)
	if err != nil || stabilizationTimeTask(t, state, task.ID).Title != "原标题" {
		t.Fatal("earlier content fingerprint invalidated by reminder suppression", err)
	}
	dsn := s.pool.Config().ConnString()
	s.Close()
	restarted, err := Open(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restarted.Close)
	channel := &stabilizationTimeChannel{}
	stabilizationTimeCheck(t, restarted, now.Add(time.Minute), channel)
	state = stabilizationTimeSnapshot(t, restarted, scope)
	if len(channel.calls) != 0 || len(state.Notices) != 0 || stabilizationTimeTask(t, state, task.ID).Due != now.Add(-2*time.Hour).Format(time.RFC3339) {
		t.Fatal("suppression did not survive restart or original due was lost")
	}
	for _, job := range state.Jobs {
		if strings.HasPrefix(job.ID, "notice:") {
			t.Fatal("suppression appeared as reminder job")
		}
	}
	for _, activity := range state.Activity {
		if strings.HasPrefix(activity.ID, "notice:") {
			t.Fatal("suppression appeared as reminder activity")
		}
	}
	out := stabilizationTimeTurn(t, restarted, scope, map[string]any{"op": "update", "ref": "T1", "set": map[string]any{"due": now.Add(2 * time.Hour).Format(time.RFC3339)}})
	stabilizationTimeReminder(t, stabilizationTimeTask(t, out.State, task.ID), now.Add(2*time.Hour).Format(time.RFC3339), now.Add(2*time.Hour).Format(time.RFC3339), "at")
	stabilizationTimeCheck(t, restarted, now.Add(2*time.Hour), channel)
	if len(channel.calls) != 1 || len(stabilizationTimeSnapshot(t, restarted, scope).Notices) != 1 {
		t.Fatal("future occurrence inherited old suppression")
	}
}

func TestR5_FutureAndRecentRemindersRestoreButOrdinaryLateTaskCatchesUp(t *testing.T) {
	for _, fixture := range []struct {
		name    string
		elapsed time.Duration
		undo    bool
	}{
		{"future_undo", -time.Hour, true},
		{"recent_undo", 30 * time.Minute, true},
		{"ordinary_late_restart", 2 * time.Hour, false},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			s := testStore(t)
			scope := owner()
			now := time.Now().UTC().Truncate(time.Second)
			at := now.Add(-fixture.elapsed)
			task := stabilizationTimeTask(t, workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: fixture.name}), "")
			stabilizationTimeSeedSchedule(t, s, scope, task, at, at)
			if fixture.undo {
				if _, err := s.Undo(context.Background(), scope, stabilizationTimeComplete(t, s, scope, task.ID)); err != nil {
					t.Fatal(err)
				}
			}
			dsn := s.pool.Config().ConnString()
			s.Close()
			restarted, err := Open(context.Background(), dsn)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(restarted.Close)
			channel := &stabilizationTimeChannel{}
			if at.After(now) {
				stabilizationTimeCheck(t, restarted, at.Add(-time.Second), channel)
				if len(channel.calls) != 0 {
					t.Fatal("future reminder sent before due")
				}
				now = at
			}
			stabilizationTimeCheck(t, restarted, now, channel)
			stabilizationTimeCheck(t, restarted, now.Add(time.Minute), channel)
			if len(channel.calls) != 1 || len(stabilizationTimeSnapshot(t, restarted, scope).Notices) != 1 {
				t.Fatalf("eligible reminder not restored once: sends=%d", len(channel.calls))
			}
		})
	}
}

func TestR5_PreviousDeliveryAndDismissalRemainAuthoritative(t *testing.T) {
	for _, dismissed := range []bool{false, true} {
		t.Run(fmt.Sprint(dismissed), func(t *testing.T) {
			s := testStore(t)
			scope := owner()
			now := time.Now().UTC().Truncate(time.Second)
			at := now.Add(-2 * time.Hour)
			task := stabilizationTimeTask(t, workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "已真实发送"}), "")
			stabilizationTimeSeedSchedule(t, s, scope, task, at, at)
			channel := &stabilizationTimeChannel{}
			stabilizationTimeCheck(t, s, at, channel)
			original := stabilizationTimeSnapshot(t, s, scope).Notices[0]
			if dismissed {
				notifier := &Notifier{Store: s}
				if _, err := notifier.DismissNotice(context.Background(), scope, original.ID); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := s.Undo(context.Background(), scope, stabilizationTimeComplete(t, s, scope, task.ID)); err != nil {
				t.Fatal(err)
			}
			stabilizationTimeCheck(t, s, now, channel)
			state := stabilizationTimeSnapshot(t, s, scope)
			if len(channel.calls) != 1 || len(state.Notices) != 1 || state.Notices[0].ID != original.ID || (state.Notices[0].DismissedAt != "") != dismissed {
				t.Fatal("existing notice or dismissal overwritten")
			}
			var delivered string
			var hidden bool
			if err := s.pool.QueryRow(context.Background(), `SELECT delivered->>'synthetic-mobile',delivered @> '{"_suppressionOnly":true}'::jsonb FROM workspace_notices WHERE id=$1`, original.ID).Scan(&delivered, &hidden); err != nil || delivered != at.Format(time.RFC3339) || hidden {
				t.Fatalf("original delivery changed: timestamp=%q hidden=%v err=%v", delivered, hidden, err)
			}
		})
	}
}

func TestR5_StrictHourBoundaryAndOnlyCompletionRestoration(t *testing.T) {
	for _, fixture := range []struct {
		name               string
		elapsed            time.Duration
		from, to           string
		active, suppressed bool
	}{
		{"exact_hour", time.Hour, "done", "todo", true, false},
		{"over_hour", time.Hour + time.Second, "done", "todo", true, true},
		{"cancelled_restore", 2 * time.Hour, "cancelled", "todo", true, false},
		{"edit_open", 2 * time.Hour, "todo", "todo", true, false},
		{"restore_done", 2 * time.Hour, "done", "done", true, false},
		{"inactive", 2 * time.Hour, "done", "todo", false, false},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			s := testStore(t)
			scope := owner()
			now := time.Date(2099, 1, 5, 12, 0, 0, 0, time.UTC)
			task := stabilizationTimeTask(t, workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: fixture.name}), "")
			current, restored := task, task
			current.Status, restored.Status = fixture.from, fixture.to
			restored.Triggers = []workspace.Trigger{{ID: "due-reminder", NextAt: now.Add(-fixture.elapsed).Format(time.RFC3339), Active: fixture.active}}
			if err := pgx.BeginFunc(context.Background(), s.pool, func(tx pgx.Tx) error {
				return suppressRestoredRemindersTx(context.Background(), tx, scope, current, restored, now)
			}); err != nil {
				t.Fatal(err)
			}
			var count int
			if err := s.pool.QueryRow(context.Background(), "SELECT count(*) FROM workspace_notices WHERE owner_id=$1", string(scope.OwnerID)).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if (count == 1) != fixture.suppressed {
				t.Fatalf("suppression rows=%d want suppressed=%v", count, fixture.suppressed)
			}
			if fixture.suppressed {
				// Existing item deletion cascades internal occurrences too.
				if _, err := s.pool.Exec(context.Background(), "DELETE FROM work_items WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), task.ID); err != nil {
					t.Fatal(err)
				}
				if err := s.pool.QueryRow(context.Background(), "SELECT count(*) FROM workspace_notices WHERE owner_id=$1", string(scope.OwnerID)).Scan(&count); err != nil || count != 0 {
					t.Fatal("suppression record did not follow item deletion", err)
				}
			}
		})
	}
}

func TestR10_UnknownChannelErrorsNeverExposeRawSensitiveText(t *testing.T) {
	s := testStore(t)
	scope := owner()
	logs := secretaryLogs(t)
	now := time.Now().UTC().Truncate(time.Second)
	task := stabilizationTimeTask(t, workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "sensitive-body-F9"}), "")
	stabilizationTimeSeedSchedule(t, s, scope, task, now, now)
	channel := &stabilizationTimeChannel{err: errors.New("sensitive-body-F9 https://user:secret-F9@invalid/bot-token-F9")}
	stabilizationTimeCheck(t, s, now, channel)
	var record map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(logs.String())), &record); err != nil || record["error_type"] != "channel_failed" || record["channel"] != "synthetic-mobile" || record["status"] != "retry" {
		t.Fatalf("missing safe failure classification: %s err=%v", logs.String(), err)
	}
	for _, sensitive := range []string{"sensitive-body-F9", "secret-F9", "bot-token-F9", "https://", task.ID} {
		if strings.Contains(logs.String(), sensitive) {
			t.Errorf("raw notification data exposed: %q", sensitive)
		}
	}
}

func TestR5_PendingFailedOccurrenceStopsWithoutLosingRetryData(t *testing.T) {
	s := testStore(t)
	scope := owner()
	now := time.Now().UTC().Truncate(time.Second)
	at := now.Add(-2 * time.Hour)
	task := stabilizationTimeTask(t, workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "历史失败提醒"}), "")
	stabilizationTimeSeedSchedule(t, s, scope, task, at, at)
	channel := &stabilizationTimeChannel{err: notify.ErrDelivery}
	stabilizationTimeCheck(t, s, at, channel)
	original := stabilizationTimeSnapshot(t, s, scope).Notices[0]
	if _, err := s.Undo(context.Background(), scope, stabilizationTimeComplete(t, s, scope, task.ID)); err != nil {
		t.Fatal(err)
	}
	stabilizationTimeCheck(t, s, now, channel)
	state := stabilizationTimeSnapshot(t, s, scope)
	if len(channel.calls) != 1 || len(state.Notices) != 1 || state.Notices[0].ID != original.ID {
		t.Fatal("old failed occurrence retried or existing notice lost")
	}
	var raw []byte
	if err := s.pool.QueryRow(context.Background(), "SELECT delivered FROM workspace_notices WHERE id=$1", original.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	var delivery deliveryState
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &delivery); err != nil {
		t.Fatal(err)
	}
	if delivery.Attempts["synthetic-mobile"] != 1 || !delivery.RetryAt["synthetic-mobile"].Equal(at.Add(time.Minute)) || string(fields["_suppressed"]) != "true" || fields["_suppressionOnly"] != nil || fields["synthetic-mobile"] != nil {
		t.Fatalf("retry metadata overwritten or success fabricated: %s", raw)
	}
}
