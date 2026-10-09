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

func TestDeputyUsesBusinessDateAndActualAdmissionTime(t *testing.T) {
	s, scope := testStore(t), owner()
	secretaryModel(t, s, func(w http.ResponseWriter, _ *http.Request) {
		t.Error("admission must not submit deputy generation")
	})
	fixed := time.Date(2024, 12, 31, 16, 30, 0, 0, time.UTC)
	s.SetBusinessClock(func() time.Time { return fixed })
	workspaceCommand(t, s, scope, workspace.Command{Type: "updateSettings", Patch: asJSON(map[string]string{"timezone": "Asia/Shanghai"})})
	state := workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "Fictitious dated request"})
	command := workspace.Command{Type: "requestRun", ThingID: state.Tasks[0].ID, AgentID: "model", Kind: "draft", Prompt: "昨天说过什么来着"}
	ctx, err := s.prepareRunContext(context.Background(), scope, command)
	if err != nil {
		t.Fatal(err)
	}
	prepared := ctx.Value(runContextKey{}).(preparedRunContext)
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2024, 12, 31, 0, 0, 0, 0, loc)
	if prepared.Plan.Time == nil || !prepared.Plan.Time.From.Equal(want) || !prepared.Plan.Time.To.Equal(want.AddDate(0, 0, 1)) {
		t.Fatal("deputy yesterday must use the business date in the owner's timezone", prepared.Plan.Time)
	}
	before := time.Now()
	state = workspaceCommand(t, s, scope, command)
	after := time.Now()
	created, err := time.Parse(time.RFC3339Nano, state.Runs[0].CreatedAt)
	if err != nil || created.Before(before) || created.After(after) {
		t.Fatal("deputy admission must keep actual time", created, err)
	}
}

func TestRetrievalBusinessDecayKeepsActualEligibilityAndAccess(t *testing.T) {
	for _, fusion := range []bool{false, true} {
		t.Run(map[bool]string{false: "lexical", true: "fusion"}[fusion], func(t *testing.T) {
			s, scope := testStore(t), owner()
			ctx := context.Background()
			secretaryModel(t, s, func(w http.ResponseWriter, _ *http.Request) {
				t.Error("lexical retrieval must not submit generation")
			})
			source := b1Source(t, s, scope, "Fictitious evidence", "Fictitious original evidence.", "manual")
			fast := b1Claim(t, s, scope, "Fictitious clock record", "fact", "adopted", source)
			slow := b1Claim(t, s, scope, "Fictitious clock record", "fact", "adopted", source)
			future := b1Claim(t, s, scope, "Fictitious clock record", "fact", "adopted", source)
			now := time.Now().UTC()
			if err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
				for _, ref := range []memory.Ref{fast, slow} {
					if err := recordUseTx(ctx, tx, scope, memory.UseEvent{Ref: ref, EventID: string(ref.ID), Kind: "confirmation", At: now}); err != nil {
						return err
					}
				}
				if _, err := tx.Exec(ctx, `UPDATE activity SET last_effective_use_at=$3,half_life_seconds=$4,stability=1,pinned=false WHERE owner_id=$1 AND record_id=$2`, scope.OwnerID, fast.ID, now, 86400); err != nil {
					return err
				}
				if _, err := tx.Exec(ctx, `UPDATE activity SET last_effective_use_at=$3,half_life_seconds=$4,stability=1,pinned=false WHERE owner_id=$1 AND record_id=$2`, scope.OwnerID, slow.ID, now.Add(-30*24*time.Hour), 30*86400); err != nil {
					return err
				}
				_, err := tx.Exec(ctx, `UPDATE record_versions SET valid_from=$3 WHERE owner_id=$1 AND record_id=$2`, scope.OwnerID, future.ID, now.Add(24*time.Hour))
				return err
			}); err != nil {
				t.Fatal(err)
			}
			recall := func(want memory.ID, count int, ranking bool) {
				t.Helper()
				out := memory.RecallResult{Coverage: coverage()}
				c := context.WithValue(ctx, activityRankingKey{}, ranking)
				err := pgx.BeginFunc(c, s.pool, func(tx pgx.Tx) error {
					return s.recallTx(c, tx, memory.Scope{OwnerID: scope.OwnerID, PrincipalID: "model", Team: true}, memory.RecallRequest{Mode: memory.Continue, Team: &memory.TeamRecall{RankFusion: fusion}}, memory.Budget{Candidates: 10, Tokens: 4000}, "Fictitious clock record", "", nil, "", 0, "", nil, &out)
				})
				if err != nil || len(out.Memories) != count || want != "" && out.Memories[0].ID != want {
					t.Fatal("decay ordering, eligibility, or access changed", out.Memories, err)
				}
			}
			recall(fast.ID, 2, true)
			fixed := now.Add(10 * 24 * time.Hour)
			s.SetBusinessClock(func() time.Time { return fixed })
			recall(slow.ID, 2, true)
			recall(slow.ID, 2, true)
			recall("", 2, false)
			workspaceCommand(t, s, scope, workspace.Command{Type: "setMemoryVisibility", ID: string(slow.ID), AgentIDs: []string{"manual"}})
			recall(fast.ID, 1, true)
			s.SetBusinessClock(nil)
			recall(fast.ID, 1, true)
		})
	}
}
