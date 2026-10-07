package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"net/http"

	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/worker"
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

// Physical deletion invalidates before cascading membership and selected items.

// Exhausted attempts must not write an empty card and call it built.

func errorsAsJob(err error, out **worker.JobError) bool {
	v, ok := err.(*worker.JobError)
	if ok {
		*out = v
	}
	return ok
}

// Explicit opt-in; this test uses only invented records in testStore's isolated
// schema and a dedicated authenticated Codex home. No production DB is opened.

// Nonselected changes still invalidate the card and postpone its job.
