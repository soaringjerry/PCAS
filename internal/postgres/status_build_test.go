package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/worker"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func statusTestMemories(t *testing.T, s *Store, scope memory.Scope, n int) []memory.Ref {
	t.Helper()
	refs := []memory.Ref{}
	for i := range n {
		ref := organizeTestMemory(t, s, scope, fmt.Sprintf("虚构用户云杉在流萤项目中第%d项工作已准备。", i))
		refs = append(refs, ref)
	}
	if err := pgx.BeginFunc(context.Background(), s.pool, func(tx pgx.Tx) error {
		id, err := entityTx(context.Background(), tx, scope.OwnerID, "project", "流萤项目")
		if err != nil {
			return err
		}
		for i, ref := range refs {
			cat := "progress"
			if i < 3 {
				cat = "rule"
			}
			if _, err := tx.Exec(context.Background(), "UPDATE claim_revisions SET category=$3,durable=true WHERE owner_id=$1 AND claim_id=$2", scope.OwnerID, ref.ID, cat); err != nil {
				return err
			}
			if _, err := tx.Exec(context.Background(), "INSERT INTO claim_mentions(owner_id,claim_id,claim_version,entity_id,role) VALUES($1,$2,1,$3,'project')", scope.OwnerID, ref.ID, id); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return refs
}
func statusTestJob(t *testing.T, s *Store, scope memory.Scope) worker.Job {
	t.Helper()
	if _, err := s.ScheduleStatus(context.Background(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(context.Background(), "DELETE FROM memory_jobs WHERE owner_id=$1 AND stage NOT LIKE 'memory.card:%'", scope.OwnerID); err != nil {
		t.Fatal(err)
	}
	j, err := s.Claim(context.Background(), 5*time.Minute)
	if err != nil || j == nil {
		t.Fatal("status lease", j, err)
	}
	return *j
}
func statusFakeReply(t *testing.T, w http.ResponseWriter, r *http.Request) {
	t.Helper()
	var req struct {
		Messages []struct{ Role, Content string }
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		t.Error(err)
		return
	}
	var prompt struct {
		Key      string
		Memories []cardMemory
	}
	for _, m := range req.Messages {
		if m.Role == "user" {
			if err := json.Unmarshal([]byte(m.Content), &prompt); err != nil {
				t.Error(err)
			}
		}
	}
	nums := []int{999}
	rules := []cardRule{}
	for _, m := range prompt.Memories {
		nums = append(nums, m.N)
		rules = append(rules, cardRule{N: m.N, AppliesTo: "起草邮件"})
	}
	secretaryModelReply(w, cardOutput{Fields: map[string][]int{"status": nums, "next": nums}, Rules: rules})
}
func TestStatusCardsBuildReadInvalidation(t *testing.T) {
	s, scope := testStore(t), owner()
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) { statusFakeReply(t, w, r) })
	refs := statusTestMemories(t, s, scope, 12)
	for range 2 {
		j := statusTestJob(t, s, scope)
		if err := s.ProcessCard(context.Background(), j); err != nil {
			t.Fatal(err)
		}
	}
	about, err := s.About(context.Background(), scope, "")
	if err != nil || len(about.Cards) != 2 || about.Building.Done != 2 || about.Building.Total != 2 {
		t.Fatal(about, err)
	}
	if len(about.Cards[0].Fields) != 0 {
		t.Fatal("directory included body")
	}
	about, err = s.About(context.Background(), scope, "self:rule")
	if err != nil || len(about.Cards) != 1 || about.Cards[0].Count != 3 {
		t.Fatal(about, err)
	}
	m := about.Cards[0].Fields[0].Items[0]
	if m.AppliesTo != "起草邮件" || !strings.Contains(m.Text, "虚构") {
		t.Fatal(m)
	}
	var before int
	if err := s.pool.QueryRow(context.Background(), "SELECT count(*) FROM claim_revisions WHERE owner_id=$1", scope.OwnerID).Scan(&before); err != nil || before != 12 {
		t.Fatal(before, err)
	}
	// Physical deletion invalidates before cascading membership and selected items.
	if _, err := s.pool.Exec(context.Background(), "DELETE FROM memory_records WHERE owner_id=$1 AND id=$2", scope.OwnerID, refs[0].ID); err != nil {
		t.Fatal(err)
	}
	about, err = s.About(context.Background(), scope, "self:rule")
	if err != nil || len(about.Cards) != 0 {
		t.Fatal("two-member group returned", about, err)
	}
	about, err = s.About(context.Background(), scope, "")
	if err != nil || len(about.Cards) != 1 || !about.Cards[0].Stale || about.Cards[0].Count != 11 {
		t.Fatal(about, err)
	}
	j, err := s.Claim(context.Background(), 5*time.Minute)
	if err != nil || j != nil {
		t.Fatal("debounce did not wait", j, err)
	}
}
func TestStatusCardsBadOutputAndVersion(t *testing.T) {
	s, scope := testStore(t), owner()
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) { secretaryModelReply(w, "not-json") })
	statusTestMemories(t, s, scope, 4)
	j := statusTestJob(t, s, scope)
	for attempt := 1; attempt <= 3; attempt++ {
		j.Attempts = attempt
		err := s.ProcessCard(context.Background(), j)
		if attempt < 3 {
			var failure *worker.JobError
			if !errorsAsJob(err, &failure) || failure.Code != "card_invalid_output" {
				t.Fatal(err)
			}
		} else if err != nil {
			t.Fatal(err)
		}
	}
	// Exhausted attempts must not write an empty card and call it built.
	about, err := s.About(context.Background(), scope, "self:rule")
	if err != nil || len(about.Cards) != 0 || about.Building.Done != 0 {
		t.Fatal(about, err)
	}
	var built, done int
	if err := s.pool.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM status_cards WHERE owner_id=$1 AND built_at IS NOT NULL),
 (SELECT count(*) FROM memory_jobs WHERE id=$2 AND state='done')`, scope.OwnerID, j.ID).Scan(&built, &done); err != nil || built != 0 || done != 1 {
		t.Fatal(built, done, err)
	}
}
func errorsAsJob(err error, out **worker.JobError) bool {
	v, ok := err.(*worker.JobError)
	if ok {
		*out = v
	}
	return ok
}

// Explicit opt-in; this test uses only invented records in testStore's isolated
// schema and a dedicated authenticated Codex home. No production DB is opened.
func TestLiveStatusCardsSynthetic80(t *testing.T) {
	home := os.Getenv("PCAS_LIVE_CODEX_HOME")
	if home == "" {
		t.Skip("requires dedicated PCAS_LIVE_CODEX_HOME")
	}
	s, scope := testStore(t), owner()
	c, err := ai.NewCodex(os.Getenv("PCAS_LIVE_CODEX_BINARY"), home)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	models, err := ai.Load(os.Getenv("PCAS_LIVE_MODELS_FILE"), c)
	if err != nil {
		t.Fatal(err)
	}
	b1Model(t, s, `{}`)
	refs := statusTestMemories(t, s, scope, 80)
	s.SetModels(models)
	workspaceCommand(t, s, scope, workspace.Command{Type: "updateSettings", Patch: asJSON(map[string]string{"timezone": "Asia/Shanghai"})})
	for range 2 {
		j := statusTestJob(t, s, scope)
		if err := s.ProcessCard(context.Background(), j); err != nil {
			t.Fatal(err)
		}
	}
	about, err := s.About(context.Background(), scope, "")
	if err != nil || about.Building.Done != about.Building.Total || len(about.Cards) != 2 {
		t.Fatal(about, err)
	}
	t.Logf("synthetic=%d default_provider=%s cards=%d progress=%d/%d", len(refs), models.ExtractionID(), len(about.Cards), about.Building.Done, about.Building.Total)
}

func TestStatusCardsLimitsAndDuringCallChange(t *testing.T) {
	s, scope := testStore(t), owner()
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) { statusFakeReply(t, w, r) })
	statusTestMemories(t, s, scope, 65)
	if _, err := s.pool.Exec(context.Background(), "UPDATE claim_revisions SET category='rule' WHERE owner_id=$1", scope.OwnerID); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		j := statusTestJob(t, s, scope)
		if err := s.ProcessCard(context.Background(), j); err != nil {
			t.Fatal(err)
		}
	}
	about, err := s.About(context.Background(), scope, "")
	if err != nil || len(about.Cards) != 2 {
		t.Fatal(about, err)
	}
	for _, c := range about.Cards {
		want := 25
		if c.Key == "self:rule" {
			want = 60
		}
		if c.Count != want {
			t.Fatal(c, want)
		}
	}
	// Nonselected changes still invalidate the card and postpone its job.
	var id string
	if err := s.pool.QueryRow(context.Background(), "SELECT id::text FROM claims WHERE owner_id=$1 ORDER BY id LIMIT 1", scope.OwnerID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(context.Background(), "UPDATE claims SET compared=compared+1 WHERE owner_id=$1 AND id=$2", scope.OwnerID, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(context.Background(), "UPDATE memory_jobs SET available_at=now()-interval '1 second' WHERE owner_id=$1 AND stage LIKE 'memory.card:%'", scope.OwnerID); err != nil {
		t.Fatal(err)
	}
	j := statusTestJob(t, s, scope)
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
		if _, err := s.pool.Exec(context.Background(), "UPDATE claims SET compared=compared+1 WHERE owner_id=$1 AND id=$2", scope.OwnerID, id); err != nil {
			t.Error(err)
		}
		statusFakeReply(t, w, r)
	})
	if err := s.ProcessCard(context.Background(), j); err == nil {
		t.Fatal("committed output after input changed")
	}
}

func TestStatusCardsHourly120Of200(t *testing.T) {
	s, scope := testStore(t), owner()
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) { statusFakeReply(t, w, r) })
	refs := statusTestMemories(t, s, scope, 3)
	if _, err := s.pool.Exec(context.Background(), "DELETE FROM claim_mentions WHERE owner_id=$1", scope.OwnerID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(context.Background(), "UPDATE claim_revisions SET category='progress' WHERE owner_id=$1", scope.OwnerID); err != nil {
		t.Fatal(err)
	}
	if err := pgx.BeginFunc(context.Background(), s.pool, func(tx pgx.Tx) error {
		for i := range 200 {
			id, err := entityTx(context.Background(), tx, scope.OwnerID, "topic", fmt.Sprintf("虚构主题%03d", i))
			if err != nil {
				return err
			}
			for _, ref := range refs {
				if _, err := tx.Exec(context.Background(), "INSERT INTO claim_mentions(owner_id,claim_id,claim_version,entity_id,role) VALUES($1,$2,1,$3,'topic')", scope.OwnerID, ref.ID, id); err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for range 120 {
		j := statusTestJob(t, s, scope)
		if err := s.ProcessCard(context.Background(), j); err != nil {
			t.Fatal(err)
		}
	}
	j := statusTestJob(t, s, scope)
	failure, ok := s.ProcessCard(context.Background(), j).(*worker.JobError)
	if !ok || failure.Code != "card_hourly_limit" || !failure.NoAttempt {
		t.Fatal(failure)
	}
	about, err := s.About(context.Background(), scope, "")
	if err != nil || about.Building.Done != 120 || about.Building.Total != 200 {
		t.Fatal(about.Building, err)
	}
	if _, err := s.pool.Exec(context.Background(), "UPDATE background_usage SET created_at=now()-interval '2 hours' WHERE owner_id=$1", scope.OwnerID); err != nil {
		t.Fatal(err)
	}
	if err := s.ProcessCard(context.Background(), j); err != nil {
		t.Fatal(err)
	}
}
