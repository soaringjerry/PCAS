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
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/worker"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func handoverTestJob(t *testing.T, s *Store, scope memory.Scope) worker.Job {
	t.Helper()
	if _, err := s.ScheduleStatus(context.Background(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(context.Background(), "DELETE FROM memory_jobs WHERE owner_id=$1 AND stage NOT LIKE 'memory.handover:%'", scope.OwnerID); err != nil {
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

	for range 2 {
		j := statusTestJob(t, s, scope)
		if err := s.ProcessCard(context.Background(), j); err != nil {
			t.Fatal(err)
		}
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
	for range 2 {
		if _, err := s.pool.Exec(context.Background(), "UPDATE handovers SET stale=true,built_at=now()-interval '7 hours' WHERE owner_id=$1", scope.OwnerID); err != nil {
			t.Fatal(err)
		}
		j = handoverTestJob(t, s, scope)
		if err := s.ProcessHandover(context.Background(), j); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.pool.Exec(context.Background(), "UPDATE handovers SET stale=true,built_at=now()-interval '7 hours' WHERE owner_id=$1", scope.OwnerID); err != nil {
		t.Fatal(err)
	}
	j = handoverTestJob(t, s, scope)
	err = s.ProcessHandover(context.Background(), j)
	failure, ok := err.(*worker.JobError)
	if !ok || failure.Code != "handover_daily_limit" || !failure.NoAttempt {
		t.Fatal("more than two rewrites", err)
	}
	if _, err := s.pool.Exec(context.Background(), "DELETE FROM memory_records WHERE owner_id=$1 AND id=$2", scope.OwnerID, refs[1].ID); err != nil {
		t.Fatal(err)
	}
	about, err = s.About(context.Background(), scope, "")
	if err != nil || !about.Handover.Stale || about.Handover.Body == "" {
		t.Fatal("stale text not retained", about, err)
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
	if !valid || strings.Count(body, "可能参加一次活动") != 2 {
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
func TestLiveStatusCompleteSynthetic80(t *testing.T) {
	home := os.Getenv("PCAS_LIVE_CODEX_HOME")
	if home == "" {
		t.Skip("requires dedicated PCAS_LIVE_CODEX_HOME")
	}
	s, scope := testStore(t), owner()
	b1Model(t, s, `{}`)
	refs := statusRichSynthetic80(t, s, scope)
	workspaceCommand(t, s, scope, workspace.Command{Type: "updateSettings", Patch: asJSON(map[string]string{"timezone": "Asia/Shanghai"})})
	c, err := ai.NewCodex(os.Getenv("PCAS_LIVE_CODEX_BINARY"), home)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	models, err := ai.Load(os.Getenv("PCAS_LIVE_MODELS_FILE"), c)
	if err != nil {
		t.Fatal(err)
	}
	s.SetModels(models)
	for range 5 {
		j := statusTestJob(t, s, scope)
		if err := s.ProcessCard(context.Background(), j); err != nil {
			t.Fatal(err)
		}
	}
	j := handoverTestJob(t, s, scope)
	if err := s.ProcessHandover(context.Background(), j); err != nil {
		t.Fatal(err)
	}
	about, err := s.About(context.Background(), scope, "")
	if err != nil || len(about.Cards) != 5 || about.Building.Done != about.Building.Total || len(about.Deadlines) < 8 || about.Handover.Body == "" {
		t.Fatal("incomplete real status build", about.Building, len(about.Cards), len(about.Deadlines), err)
	}
	t.Logf("synthetic=%d default_provider=%s model=gpt-6.1-sol cards=%d deadlines=%d handover_runes=%d progress=%d/%d", len(refs), models.ExtractionID(), len(about.Cards), len(about.Deadlines), utf8.RuneCountInString(about.Handover.Body), about.Building.Done, about.Building.Total)
	response := b1HTTP(t, s, scope, "GET", "/v1/workspace/about", nil)
	if response.Code != 200 {
		t.Fatal(response.Code)
	}
	for _, c := range about.Cards {
		t.Logf("card kind=%s items=%d", c.Kind, c.Count)
	}
}

func TestStatusCorrectionAndSourceWithdrawal(t *testing.T) {
	s, scope := testStore(t), owner()
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) { statusFakeReply(t, w, r) })
	refs := statusTestMemories(t, s, scope, 12)
	for range 2 {
		j := statusTestJob(t, s, scope)
		if err := s.ProcessCard(context.Background(), j); err != nil {
			t.Fatal(err)
		}
	}
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) { handoverFakeReply(t, w, r) })
	j := handoverTestJob(t, s, scope)
	if err := s.ProcessHandover(context.Background(), j); err != nil {
		t.Fatal(err)
	}
	workspaceCommand(t, s, scope, workspace.Command{Type: "editMemory", ID: string(refs[0].ID), Text: "虚构流萤项目的原要求改为附上样品日期。"})
	about, err := s.About(context.Background(), scope, "self:rule")
	if err != nil || len(about.Cards) != 1 || about.Cards[0].Count != 2 || !about.Cards[0].Stale || !about.Handover.Stale {
		t.Fatal(about, err)
	}
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) { statusFakeReply(t, w, r) })
	if _, err := s.pool.Exec(context.Background(), "UPDATE memory_jobs SET available_at=now()-interval '1 second' WHERE owner_id=$1 AND stage LIKE 'memory.card:%'", scope.OwnerID); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		j := statusTestJob(t, s, scope)
		if err := s.ProcessCard(context.Background(), j); err != nil {
			t.Fatal(err)
		}
	}
	about, err = s.About(context.Background(), scope, "self:rule")
	if err != nil || about.Cards[0].Count != 3 || about.Cards[0].Stale {
		t.Fatal(about, err)
	}
	found := false
	for _, f := range about.Cards[0].Fields {
		for _, m := range f.Items {
			if m.ID == string(refs[0].ID) {
				found = m.Version == 2 && strings.Contains(m.Text, "样品日期")
			}
		}
	}
	if !found {
		t.Fatal("new revision not rebuilt")
	}
	if _, err := s.pool.Exec(context.Background(), "UPDATE record_versions SET actor='ai' WHERE owner_id=$1 AND record_id=$2", scope.OwnerID, refs[1].ID); err != nil {
		t.Fatal(err)
	}
	var source string
	if err := s.pool.QueryRow(context.Background(), "SELECT source_id::text FROM evidence WHERE owner_id=$1 AND target_id=$2 LIMIT 1", scope.OwnerID, refs[1].ID).Scan(&source); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(context.Background(), "INSERT INTO claim_source_keys(owner_id,source_id,claim_key,claim_id) VALUES($1,$2,'synthetic-extracted',$3)", scope.OwnerID, source, refs[1].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(context.Background(), "UPDATE memory_records SET state='withdrawn' WHERE owner_id=$1 AND id=$2", scope.OwnerID, source); err != nil {
		t.Fatal(err)
	}
	about, err = s.About(context.Background(), scope, "self:rule")
	if err != nil || len(about.Cards) != 0 || !about.Handover.Stale {
		t.Fatal(about, err)
	}
}
