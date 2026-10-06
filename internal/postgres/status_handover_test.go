package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/worker"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func handoverTestJob(t *testing.T, s *Store, scope memory.Scope) worker.Job {
	t.Helper()
	if _, err := s.ScheduleStatus(context.Background(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(context.Background(), "DELETE FROM memory_jobs WHERE owner_id=$1 AND state='queued' AND stage NOT LIKE 'memory.handover:%'", scope.OwnerID); err != nil {
		t.Fatal(err)
	}
	j, err := s.Claim(context.Background(), 5*time.Minute)
	if err != nil || j == nil {
		t.Fatal(j, err)
	}
	return *j
}
func handoverFakeReply(t *testing.T, w http.ResponseWriter, r *http.Request) {
	t.Helper()
	var req struct {
		Messages []struct{ Role, Content string }
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	var prompt handoverInput
	for _, m := range req.Messages {
		if m.Role == "user" {
			if err := json.Unmarshal([]byte(m.Content), &prompt); err != nil {
				t.Error(err)
			}
		}
	}
	for _, m := range prompt.Memories {
		if m.Trust == "inferred" || strings.Contains(m.Text, "推断哨兵") || strings.Contains(m.Text, "AI来源哨兵") {
			t.Error("inferred supplied to handover")
		}
	}
	output := handoverOutput{}
	for _, title := range handoverTitles {
		section := handoverSection{Title: title, Body: "（暂无依据）", Refs: []int{}}
		if len(prompt.Memories) > 0 {
			section.Body = prompt.Memories[0].Text
			section.Refs = []int{1}
		}
		output.Sections = append(output.Sections, section)
	}
	secretaryModelReply(w, output)
}
func TestStatusHandoverGroundingLimitsAndDeletion(t *testing.T) {
	s, scope := testStore(t), owner()
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) { statusFakeReply(t, w, r) })
	refs := statusTestMemories(t, s, scope, 12)
	if _, err := s.pool.Exec(context.Background(), "UPDATE claim_revisions SET value=to_jsonb('推断哨兵：云杉有未说明的经历'::text),acquisition='inferred' WHERE owner_id=$1 AND claim_id=$2", scope.OwnerID, refs[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(context.Background(), "UPDATE record_versions SET actor='ai' WHERE owner_id=$1 AND record_id=ANY($2::uuid[])", scope.OwnerID, []string{string(refs[2].ID), string(refs[3].ID)}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(context.Background(), "UPDATE claim_revisions SET value=to_jsonb('AI来源哨兵：这是助手说的'::text) WHERE owner_id=$1 AND claim_id=$2", scope.OwnerID, refs[1].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(context.Background(), "INSERT INTO source_contexts(owner_id,source_id,source_version,role) SELECT owner_id,source_id,source_version,'assistant' FROM evidence WHERE owner_id=$1 AND target_id=$2 ON CONFLICT(owner_id,source_id,source_version) DO UPDATE SET role='assistant'", scope.OwnerID, refs[1].ID); err != nil {
		t.Fatal(err)
	}

	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) { handoverFakeReply(t, w, r) })
	j := handoverTestJob(t, s, scope)
	if err := s.ProcessHandover(context.Background(), j); err != nil {
		t.Fatal(err)
	}
	about, err := s.About(context.Background(), scope, "")
	if err != nil || about.Handover.Body == "" || about.Handover.Stale || strings.Count(about.Handover.Body, "## ") != 9 || strings.Contains(about.Handover.Body, "推断哨兵") || !strings.Contains(about.Handover.Body, "虚构用户云杉") {
		t.Fatal(about, err)
	}
	// Changing an eligible source stales the note, but keeps its old text/time.
	if _, err := s.pool.Exec(context.Background(), "UPDATE memory_records SET state='withdrawn' WHERE owner_id=$1 AND id=$2", scope.OwnerID, refs[2].ID); err != nil {
		t.Fatal(err)
	}
	stale, err := s.ReadHandover(context.Background(), scope)
	if err != nil || !stale.Stale || stale.Body != about.Handover.Body || stale.BuiltAt != about.Handover.BuiltAt {
		t.Fatal("stale text/time must remain available", stale, err)
	}
	// The changed input waits a full hour even when callers schedule early.
	if _, err := s.ScheduleStatus(context.Background(), time.Now()); err != nil {
		t.Fatal(err)
	}
	var due, built time.Time
	if err := s.pool.QueryRow(context.Background(), "SELECT available_at,(SELECT built_at FROM handovers WHERE owner_id=$1) FROM memory_jobs WHERE owner_id=$1 AND stage LIKE 'memory.handover:%' AND state='queued'", scope.OwnerID).Scan(&due, &built); err != nil || due.Before(built.Add(time.Hour)) {
		t.Fatal(due, built, err)
	}

}
func TestStatusHandoverParser(t *testing.T) {
	ms := []cardMemory{{N: 1, Text: "虚构规则", Category: "rule", Trust: "stated"}, {N: 2, Text: "可能参加一次活动", Category: "event", Trust: "tentative"}, {N: 3, Text: "推断", Trust: "inferred"}}
	output := handoverOutput{}
	for i, title := range handoverTitles {
		ref := 2
		if i == 2 {
			ref = 3
		}
		output.Sections = append(output.Sections, handoverSection{Title: title, Body: "可能参加一次活动", Refs: []int{ref}})
	}
	body, valid := parseHandover(string(asJSON(output)), ms)
	if valid || body != "" {
		t.Fatal(body, valid)
	}
	output.Sections[0].Title = "不合法标题"
	if _, valid := parseHandover(string(asJSON(output)), ms); valid {
		t.Fatal("accepted unknown section")
	}
	output.Sections[0].Title = handoverTitles[0]
	output.Sections[0].Refs = []int{1}
	output.Sections[0].Body = strings.Repeat("字", 1800)
	if _, valid := parseHandover(string(asJSON(output)), ms); valid {
		t.Fatal("accepted overlong handover")
	}
}

func statusRichSynthetic80(t *testing.T, s *Store, scope memory.Scope) []memory.Ref {
	t.Helper()
	refs := []memory.Ref{}
	now := time.Now()
	loc, _ := time.LoadLocation("Asia/Shanghai")
	for i := range 80 {
		category := "progress"
		text := fmt.Sprintf("虚构流萤项目第%d项材料已准备，下一步核对样稿。", i)
		switch {
		case i < 10:
			category = "identity"
			text = fmt.Sprintf("虚构用户云杉是材料研究员，当前在流萤项目负责第%d类实验的记录。", i)
		case i < 20:
			category = "taste"
			text = fmt.Sprintf("虚构用户云杉偏好第%d类图表保持清晰，报告用简洁中文。", i)
		case i < 40:
			category = "rule"
			text = fmt.Sprintf("为虚构流萤项目起草报告时，第%d项表格必须写明样品编号与单位。", i)
		case i < 50:
			category = "goal"
			text = fmt.Sprintf("虚构流萤项目的第%d阶段目标是形成可复验的实验记录。", i)
		case i < 56:
			day := now.In(loc).AddDate(0, 0, i-49)
			text = fmt.Sprintf("虚构流萤项目第%d份报告在%s上午十点前提交。", i, day.Format("2006年1月2日"))
		case i == 56:
			text = "虚构流萤项目每周二晚上有课。"
		case i == 57:
			text = "虚构流萤项目每周四7点有课。"
		}
		ref := organizeTestMemory(t, s, scope, text)
		refs = append(refs, ref)
		if _, err := s.pool.Exec(context.Background(), "UPDATE claim_revisions SET category=$3,durable=$4 WHERE owner_id=$1 AND claim_id=$2", scope.OwnerID, ref.ID, category, category != "progress"); err != nil {
			t.Fatal(err)
		}
		if _, err := s.pool.Exec(context.Background(), "UPDATE record_versions SET expressed_at=$3 WHERE owner_id=$1 AND record_id=$2", scope.OwnerID, ref.ID, now); err != nil {
			t.Fatal(err)
		}
	}
	if err := pgx.BeginFunc(context.Background(), s.pool, func(tx pgx.Tx) error {
		id, err := entityTx(context.Background(), tx, scope.OwnerID, "project", "流萤项目")
		if err != nil {
			return err
		}
		for _, ref := range refs {
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
func TestStatusCorrectionAndSourceWithdrawal(t *testing.T) {
	s, scope, ctx := testStore(t), owner(), context.Background()
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) { handoverFakeReply(t, w, r) })
	refs := statusTestMemories(t, s, scope, 12)
	if err := s.ProcessHandover(ctx, handoverTestJob(t, s, scope)); err != nil {
		t.Fatal(err)
	}
	previous, err := s.ReadHandover(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	workspaceCommand(t, s, scope, workspace.Command{Type: "editMemory", ID: string(refs[0].ID), Text: "虚构流萤项目的原要求改为附上样品日期。"})
	rules, err := s.ListAssistantRequirements(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range rules.Items {
		if r.MemoryID == string(refs[0].ID) {
			found = strings.Contains(r.Text, "样品日期")
		}
	}
	if !found {
		t.Fatal("requirements must read current corrected memory", rules)
	}
	note, err := s.ReadHandover(ctx, scope)
	if err != nil || !note.Stale || note.Body != previous.Body {
		t.Fatal("correction must retain old stale handover", note, err)
	}
	if _, err := s.pool.Exec(ctx, "UPDATE record_versions SET actor='ai' WHERE owner_id=$1 AND record_id=$2", scope.OwnerID, refs[1].ID); err != nil {
		t.Fatal(err)
	}
	var source string
	if err := s.pool.QueryRow(ctx, "SELECT source_id::text FROM evidence WHERE owner_id=$1 AND target_id=$2 LIMIT 1", scope.OwnerID, refs[1].ID).Scan(&source); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, "INSERT INTO claim_source_keys(owner_id,source_id,claim_key,claim_id) VALUES($1,$2,'synthetic-extracted',$3)", scope.OwnerID, source, refs[1].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, "UPDATE memory_records SET state='withdrawn' WHERE owner_id=$1 AND id=$2", scope.OwnerID, source); err != nil {
		t.Fatal(err)
	}
	rules, err = s.ListAssistantRequirements(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rules.Items {
		if r.MemoryID == string(refs[1].ID) {
			t.Fatal("withdrawn evidence still supplies requirement", r)
		}
	}
}
