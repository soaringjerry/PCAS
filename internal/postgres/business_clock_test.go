package postgres

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// Existing replay helpers use the production default clock through this adapter.
func deskNow(loc *time.Location) string { return (&Store{}).deskNow(loc) }

func TestBusinessClockDefaultsToActualTimeAndCanBeRestored(t *testing.T) {
	s := &Store{}
	before := time.Now()
	actual := s.businessNow()
	after := time.Now()
	if actual.Before(before) || actual.After(after) {
		t.Fatal("the default clock must use actual time")
	}
	fixed := time.Date(2024, 12, 31, 16, 30, 0, 0, time.UTC)
	s.SetBusinessClock(func() time.Time { return fixed })
	if !s.businessNow().Equal(fixed) {
		t.Fatal("the configured business time was not used")
	}
	s.SetBusinessClock(nil)
	before = time.Now()
	actual = s.businessNow()
	after = time.Now()
	if actual.Before(before) || actual.After(after) {
		t.Fatal("nil must restore actual time")
	}
}

func TestSecretaryUsesBusinessDateAndActualUsageTime(t *testing.T) {
	s := testStore(t)
	scope := owner()
	fixed := time.Date(2024, 12, 31, 16, 30, 0, 0, time.UTC)
	s.SetBusinessClock(func() time.Time { return fixed })
	secretaryModel(t, s, func(w http.ResponseWriter, _ *http.Request) {
		secretaryModelReply(w, `{"reply":"Fictional reply","actions":[]}`)
	})
	workspaceCommand(t, s, scope, workspace.Command{Type: "updateSettings", Patch: asJSON(map[string]string{"timezone": "Asia/Shanghai"})})
	req := turnRequest("昨天说过什么来着")
	conversationID := string(memory.NewID())
	req.ConversationID = &conversationID
	ctx := context.Background()
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		c, err := s.secretaryContextTx(ctx, tx, scope, req, conversationID)
		if err != nil {
			return err
		}
		if _, _, err := s.secretaryPrompt(ctx, tx, scope, req, &c); err != nil {
			return err
		}
		loc := deskLocation(c.Settings)
		want := time.Date(2024, 12, 31, 0, 0, 0, 0, loc)
		if c.Plan.Time == nil || !c.Plan.Time.From.Equal(want) || !c.Plan.Time.To.Equal(want.AddDate(0, 0, 1)) {
			t.Fatal("yesterday must use the fixed business date in the owner's timezone", c.Plan.Time)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	before := time.Now()
	out := mustTurn(t, s, scope, req)
	after := time.Now()
	var at time.Time
	if err := s.pool.QueryRow(ctx, "SELECT at FROM model_usage WHERE owner_id=$1 AND turn_id=$2 ORDER BY at LIMIT 1", string(scope.OwnerID), out.Turn.ID).Scan(&at); err != nil {
		t.Fatal(err)
	}
	if at.Before(before) || at.After(after) {
		t.Fatal("usage must record actual execution time", at)
	}
}
