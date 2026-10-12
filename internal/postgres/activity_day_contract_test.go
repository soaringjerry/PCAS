package postgres

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/modelcall"
	"github.com/soaringjerry/PCAS/internal/postgres/callstore"
	"github.com/soaringjerry/PCAS/internal/postgres/kernel"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

type dayActivity struct {
	Date, Timezone string
	EventCount     int
	OmittedEvents  int
	Kinds          map[string]int
	Events         []struct {
		Kind, EventID, Function, Outcome, Summary, Reason string
		ExecutionID                                       *string
		Cost                                              *float64
		CostKnown                                         bool
		Evidence                                          map[string]any
	}
	Cost struct {
		Settled                float64
		Invocations            int
		UnknownCostInvocations int
		HeldReservations       float64
		ByFunction             []struct {
			Function    string
			Invocations int
			Settled     float64
		}
	}
	Gaps []string
}

func readDayActivity(t *testing.T, s *Store, scope memory.Scope, date string, limit, offset int) dayActivity {
	t.Helper()
	raw, err := s.DayActivity(context.Background(), scope, date, limit, offset)
	if err != nil {
		t.Fatal(err)
	}
	var out dayActivity
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err, string(raw))
	}
	return out
}

func TestDayActivityAnswersWhatHappenedItsCostAndItsReasons(t *testing.T) {
	s, scope, ctx := testStore(t), owner(), context.Background()
	secretaryModel(t, s, func(w http.ResponseWriter, _ *http.Request) {
		secretaryModelReply(w, `{"reply":"Fictitious reply.","actions":[{"op":"create_task","title":"Fictitious task one"},{"op":"create_task","title":"Fictitious task two"}]}`)
	})
	s.Models.Config.Providers[0].CostMode = ""
	s.Models.Config.Providers[0].InputPerMillion, s.Models.Config.Providers[0].OutputPerMillion = 2, 4
	workspaceCommand(t, s, scope, workspace.Command{Type: "updateSettings", Patch: kernel.JSON(map[string]string{"timezone": "Asia/Shanghai"})})

	// One paid call leads to two actions. One of them is reversed.
	request := turnRequest("Fictitious request with two tasks.")
	turn := mustTurn(t, s, scope, request).Turn
	if len(turn.Receipts) != 2 || turn.Receipts[0].ActionID == nil {
		t.Fatal(turn.Receipts)
	}
	if _, err := s.Undo(ctx, scope, *turn.Receipts[0].ActionID); err != nil {
		t.Fatal(err)
	}
	// A deferral, usage from before the call journal, and a call whose cost is not known.
	if _, err := s.Pool.Exec(ctx, `INSERT INTO background_stage_events(owner_id,stage,outcome,reason,count) VALUES($1,'memory.topic_project','deferred','topic_project_hourly_limit',3)`, string(scope.OwnerID)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Pool.Exec(ctx, `INSERT INTO model_usage(owner_id,id,purpose,agent_id,model,input_tokens,output_tokens,cost) VALUES($1,gen_random_uuid(),'extraction','model','fictitious',100,10,0.0005)`, string(scope.OwnerID)); err != nil {
		t.Fatal(err)
	}
	provider, _ := s.Models.Get("model")
	pending := modelcall.DirectRequest{OwnerID: scope.OwnerID, ExecutionID: memory.NewID(), Function: "vision", Stage: "image", Provider: provider, Estimate: 0.02, Operation: "vision", Inputs: 1}
	pending.RootExecutionID = pending.ExecutionID
	if _, err := (callstore.DirectCalls{Store: s.Calls}).Begin(ctx, pending); err != nil {
		t.Fatal(err)
	}
	// Work on the earlier local day stays out: 23:59 in Shanghai is 15:59 UTC.
	now := time.Now().In(time.FixedZone("CST", 8*3600))
	today := now.Format(time.DateOnly)
	earlier := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).Add(-time.Minute)
	if _, err := s.Pool.Exec(ctx, `INSERT INTO model_usage(owner_id,id,at,purpose,agent_id,model,input_tokens,output_tokens,cost) VALUES($1,gen_random_uuid(),$2,'extraction','model','fictitious',100,10,9)`, string(scope.OwnerID), earlier); err != nil {
		t.Fatal(err)
	}

	var secretaryCost float64
	if err := s.Pool.QueryRow(ctx, `SELECT cost::float8 FROM model_usage WHERE owner_id=$1 AND purpose='secretary'`, string(scope.OwnerID)).Scan(&secretaryCost); err != nil || secretaryCost <= 0 {
		t.Fatal(secretaryCost, err)
	}
	day := readDayActivity(t, s, scope, today, 0, 0)
	if day.Date != today || day.Timezone != "Asia/Shanghai" || day.OmittedEvents != 0 || len(day.Events) != day.EventCount || len(day.Gaps) == 0 {
		t.Fatalf("%+v", day)
	}
	if day.Kinds["call"] != 2 || day.Kinds["usage"] != 1 || day.Kinds["action"] < 2 || day.Kinds["undo"] != 1 || day.Kinds["stage"] != 1 {
		t.Fatal(day.Kinds)
	}
	// Two actions share one call. Its cost is counted once, and the earlier day's usage is absent.
	if math.Abs(day.Cost.Settled-(secretaryCost+0.0005)) > 1e-9 || day.Cost.Invocations != 3 || day.Cost.UnknownCostInvocations != 1 || day.Cost.HeldReservations != 0.02 {
		t.Fatalf("%+v secretary=%g", day.Cost, secretaryCost)
	}
	var undone, undo, deferral, unknown, linked bool
	for _, e := range day.Events {
		switch {
		case e.Kind == "action" && e.EventID == *turn.Receipts[0].ActionID:
			undone = e.Outcome == "undone" && e.ExecutionID != nil && *e.ExecutionID == request.RequestID
		case e.Kind == "undo":
			undo = e.Evidence["undoOf"] == *turn.Receipts[0].ActionID
		case e.Kind == "stage":
			deferral = e.Outcome == "deferred" && e.Reason == "topic_project_hourly_limit" && e.Evidence["count"] == float64(3)
		case e.Kind == "call" && e.Function == "vision":
			unknown = e.Cost == nil && !e.CostKnown && e.Outcome == "started" && e.Evidence["heldReservation"] == 0.02
		case e.Kind == "call" && e.Function == "secretary":
			linked = e.ExecutionID != nil && *e.ExecutionID == request.RequestID && e.CostKnown && e.Outcome == "returned"
		}
	}
	if !undone || !undo || !deferral || !unknown || !linked {
		t.Fatal("an event lost its outcome, reason, link, or cost state", undone, undo, deferral, unknown, linked)
	}
	// A short page reports what it leaves out, and the totals still cover the day.
	page := readDayActivity(t, s, scope, today, 2, 1)
	if len(page.Events) != 2 || page.OmittedEvents != day.EventCount-3 || page.Cost.Settled != day.Cost.Settled {
		t.Fatalf("events=%d omitted=%d", len(page.Events), page.OmittedEvents)
	}
	// Another owner and a non-owner read nothing.
	if other := readDayActivity(t, s, owner(), today, 0, 0); other.EventCount != 0 {
		t.Fatal(other.EventCount)
	}
	if _, err := s.DayActivity(ctx, memory.Scope{OwnerID: scope.OwnerID, PrincipalID: "agent"}, today, 0, 0); err == nil {
		t.Fatal("a non-owner read the owner's activity")
	}
}
