package postgres

import (
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/notify"
	"github.com/soaringjerry/PCAS/internal/workspace"
	"testing"
	"time"
)

func TestRescheduledNoticeIsInvalidated(t *testing.T) {
	for _, change := range []string{"reschedule", "due-command", "remove", "inactive", "guard", "between-channels"} {
		t.Run(change, func(t *testing.T) {
			s, scope := testStore(t), owner()
			now := time.Now().Add(time.Second)
			notice := dueNotice(t, s, scope, now)
			changeItem := func() {
				if change == "due-command" {
					err := pgx.BeginFunc(context.Background(), s.pool, func(tx pgx.Tx) error {
						item, err := getItem(context.Background(), tx, scope, notice.ThingID)
						if err != nil {
							return err
						}
						item.Triggers[0].Offset = "at"
						return saveItem(context.Background(), tx, scope, item)
					})
					if err != nil {
						t.Fatal(err)
					}
					workspaceCommand(t, s, scope, workspace.Command{Type: "updateTask", ID: notice.ThingID, Patch: asJSON(map[string]string{"due": now.Add(24 * time.Hour).UTC().Format(time.RFC3339)})})
					return
				}
				err := pgx.BeginFunc(context.Background(), s.pool, func(tx pgx.Tx) error {
					item, err := getItem(context.Background(), tx, scope, notice.ThingID)
					if err != nil {
						return err
					}
					switch change {
					case "remove":
						item.Triggers = nil
					case "inactive":
						item.Triggers[0].Active = false
					case "guard":
						item.Triggers[0].Guard = "waiting"
					default:
						item.Triggers[0].NextAt = now.Add(24 * time.Hour).UTC().Format(time.RFC3339)
					}
					return saveItem(context.Background(), tx, scope, item)
				})
				if err != nil {
					t.Fatal(err)
				}
			}
			ch := &fakeNotifyChannel{name: "test"}
			channels := []notify.Channel{ch}
			if change == "between-channels" {
				channels = append([]notify.Channel{&fakeNotifyChannel{name: "first", before: changeItem}}, channels...)
			} else {
				changeItem()
				state, err := s.Snapshot(context.Background(), scope)
				if err != nil {
					t.Fatal(err)
				}
				for _, n := range state.Notices {
					if n.ID == notice.ID && n.DismissedAt == "" {
						t.Error("obsolete notice pinned before dispatch")
					}
				}
			}
			if err := s.DispatchNotices(context.Background(), now, channels); err != nil {
				t.Fatal(err)
			}
			if len(ch.calls) != 0 {
				t.Error("obsolete occurrence sent")
			}
			st, err := s.Snapshot(context.Background(), scope)
			if err != nil {
				t.Fatal(err)
			}
			for _, n := range st.Notices {
				if n.ID == notice.ID && n.DismissedAt == "" {
					t.Error("obsolete reminder still pinned")
				}
			}
			var dismissed bool
			if err := s.pool.QueryRow(context.Background(), "SELECT dismissed_at IS NOT NULL FROM workspace_notices WHERE id=$1", notice.ID).Scan(&dismissed); err != nil || !dismissed {
				t.Error("obsolete notice not durably invalidated", err)
			}
			if change == "reschedule" {
				if err := s.CheckReminders(context.Background(), now.Add(25*time.Hour)); err != nil {
					t.Fatal(err)
				}
				st, err = s.Snapshot(context.Background(), scope)
				if err != nil {
					t.Fatal(err)
				}
				active := 0
				for _, n := range st.Notices {
					if n.DismissedAt == "" {
						active++
					}
				}
				if active != 1 {
					t.Error("new reminder occurrence missing", active)
				}
			}
		})
	}
}

func TestRunNoticeDoesNotRequireReminderTrigger(t *testing.T) {
	s, scope := testStore(t), owner()
	now := time.Now().Add(time.Second)
	st := workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "Result"})
	_, err := s.pool.Exec(context.Background(), "INSERT INTO workspace_notices(owner_id,thing_id,trigger_id,due_at,reason) VALUES($1,$2,$3,$4,'Done')", string(scope.OwnerID), st.Tasks[0].ID, runNoticePrefix+string(memory.NewID()), now)
	if err != nil {
		t.Fatal(err)
	}
	ch := &fakeNotifyChannel{name: "test"}
	if err := s.DispatchNotices(context.Background(), now, []notify.Channel{ch}); err != nil {
		t.Fatal(err)
	}
	if len(ch.calls) != 1 {
		t.Error("run notice suppressed")
	}
}

func TestEquivalentInstantKeepsCurrentNotice(t *testing.T) {
	s, scope := testStore(t), owner()
	now := time.Now().Add(time.Second)
	notice := dueNotice(t, s, scope, now)
	err := pgx.BeginFunc(context.Background(), s.pool, func(tx pgx.Tx) error {
		item, err := getItem(context.Background(), tx, scope, notice.ThingID)
		if err != nil {
			return err
		}
		at, err := time.Parse(time.RFC3339, item.Triggers[0].NextAt)
		if err != nil {
			return err
		}
		item.Triggers[0].NextAt = at.In(time.FixedZone("Test", 2*3600)).Format(time.RFC3339)
		return saveItem(context.Background(), tx, scope, item)
	})
	if err != nil {
		t.Fatal(err)
	}
	ch := &fakeNotifyChannel{name: "test"}
	if err := s.DispatchNotices(context.Background(), now, []notify.Channel{ch}); err != nil {
		t.Fatal(err)
	}
	if len(ch.calls) != 1 {
		t.Error("same instant suppressed because its timezone spelling changed")
	}
}

func TestNotificationInvalidationDoesNotLockOwnerDuringDelivery(t *testing.T) {
	s, scope := testStore(t), owner()
	now := time.Now().Add(time.Second)
	notice := dueNotice(t, s, scope, now)
	var ownerTx pgx.Tx
	defer func() {
		if ownerTx != nil {
			_ = ownerTx.Rollback(context.Background())
		}
	}()
	first := &fakeNotifyChannel{name: "first", before: func() {
		workspaceCommand(t, s, scope, workspace.Command{Type: "toggleTrigger", ID: notice.ThingID, TriggerID: "due-reminder"})
		var err error
		ownerTx, err = s.pool.Begin(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ownerTx.Exec(context.Background(), "SELECT 1 FROM workspace_owners WHERE owner_id=$1 FOR UPDATE", string(scope.OwnerID)); err != nil {
			t.Fatal(err)
		}
	}}
	second := &fakeNotifyChannel{name: "second"}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.DispatchNotices(ctx, now, []notify.Channel{first, second}); err != nil {
		t.Fatal("notice invalidation waited on owner lock during delivery", err)
	}
	if len(second.calls) != 0 {
		t.Error("disabled reminder sent to second channel")
	}
}
