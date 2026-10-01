package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/blob"
	"github.com/soaringjerry/PCAS/internal/connectors"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestConnectorReplayAndSummary(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	scope := owner()
	c, err := s.ConfigureConnection(ctx, scope, connectors.ConfigureRequest{Name: "聊天记录", Kind: "webhook", IntervalSeconds: 60, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(c.WebhookToken) != 64 {
		t.Fatal("no scoped token")
	}
	auth, err := s.AuthenticateConnection(ctx, c.Connection.ID, c.WebhookToken)
	if err != nil || auth.OwnerID != scope.OwnerID {
		t.Fatal("auth", err)
	}
	if _, err = s.AuthenticateConnection(ctx, c.Connection.ID, strings.Repeat("0", 64)); err == nil {
		t.Fatal("wrong credential accepted")
	}
	batch := connectors.Batch{Records: []connectors.Record{{ID: "message-1", Text: "以后想去河边的旧书店", Title: "沉睡愿望", Role: "user", ConversationID: "visit", MissingAttachments: []string{"原图未接入"}}}}
	out, err := s.ImportBatch(ctx, scope, c.Connection.ID, batch)
	if err != nil || out.Imported != 1 {
		t.Fatal(out, err)
	}
	again, err := s.ImportBatch(ctx, scope, c.Connection.ID, batch)
	if err != nil || again.Duplicates != 1 {
		t.Fatal(again, err)
	}
	source, err := s.GetSource(ctx, scope, out.Refs[0].ID, 0)
	if err != nil || source.Context == nil || source.Context.Role != "user" {
		t.Fatal(source, err)
	}
	summary, err := s.Summarize(ctx, scope, memory.SummaryRequest{ID: out.Refs[0].ID})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(summary.Text, "旧书店") || summary.Coverage.Complete {
		t.Fatal(summary)
	}
	cached, err := s.Summarize(ctx, scope, memory.SummaryRequest{ID: out.Refs[0].ID})
	if err != nil || !cached.Cached {
		t.Fatal(cached, err)
	}
	other := memory.Scope{OwnerID: scope.OwnerID, PrincipalID: "other"}
	if _, err = s.Summarize(ctx, other, memory.SummaryRequest{ID: out.Refs[0].ID}); err != memory.ErrNotFound {
		t.Fatal("summary leaked", err)
	}
	if err = s.Delete(ctx, scope, memory.DeleteRequest{Targets: out.Refs, BlockReimport: true}); err != nil {
		t.Fatal(err)
	}
	var orphanEpisodes int
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM episodes WHERE owner_id=$1", string(scope.OwnerID)).Scan(&orphanEpisodes); err != nil || orphanEpisodes != 0 {
		t.Fatal("deleted conversation title survived", orphanEpisodes, err)
	}
	blocked, err := s.ImportBatch(ctx, scope, c.Connection.ID, batch)
	if err != nil || blocked.Blocked != 1 {
		t.Fatal(blocked, err)
	}
	if _, err = s.Summarize(ctx, scope, memory.SummaryRequest{ID: out.Refs[0].ID}); err != memory.ErrNotFound {
		t.Fatal("deleted summary", err)
	}
}
func TestConnectorPollingCursorAtomicity(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	scope := owner()
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		b := connectors.Batch{Records: []connectors.Record{{ID: "one", Version: "1", Text: "计划周末去书店"}}, NextCursor: "page2", HasMore: true}
		if r.URL.Query().Get("cursor") == "page2" {
			b = connectors.Batch{Records: []connectors.Record{{ID: "two", Version: "1", Text: "但还没有决定日期"}}}
		}
		_ = json.NewEncoder(w).Encode(b)
	}))
	defer upstream.Close()
	c, err := s.ConfigureConnection(ctx, scope, connectors.ConfigureRequest{Name: "文档", Kind: "poll", URL: upstream.URL, IntervalSeconds: 60, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err = s.syncNextConnection(ctx); err != nil {
			t.Fatal(err)
		}
	}
	all, err := s.ListConnections(ctx, scope)
	if err != nil || len(all) != 1 || all[0].Imported != 2 || calls != 2 {
		t.Fatal(all, calls, err)
	}
	stale := connectors.ConfigureRequest{ID: c.Connection.ID, ExpectedVersion: 9, Name: "x", Kind: "poll", URL: upstream.URL, IntervalSeconds: 60, Enabled: true}
	if _, err = s.ConfigureConnection(ctx, scope, stale); err != memory.ErrConflict {
		t.Fatal("stale configuration", err)
	}
}
func TestConnectorFolderAndSymlink(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	scope := owner()
	root := t.TempDir()
	t.Setenv("PCAS_INBOX_DIR", root)
	c, err := s.ConfigureConnection(ctx, scope, connectors.ConfigureRequest{Name: "收件夹", Kind: "folder", IntervalSeconds: 15, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, string(c.Connection.ID))
	if err = os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(path, "note.md"), []byte("当前作业版本是 v3"), 0600); err != nil {
		t.Fatal(err)
	}
	external := filepath.Join(t.TempDir(), "private.txt")
	_ = os.WriteFile(external, []byte("must not be imported"), 0600)
	if err = os.Symlink(external, filepath.Join(path, "escape.txt")); err != nil {
		t.Fatal(err)
	}
	if err = s.syncNextConnection(ctx); err != nil {
		t.Fatal(err)
	}
	if err = s.SyncConnection(ctx, scope, c.Connection.ID, 1); err != nil {
		t.Fatal(err)
	}
	if err = s.syncNextConnection(ctx); err != nil {
		t.Fatal(err)
	}
	all, err := s.ListConnections(ctx, scope)
	if err != nil || all[0].Imported != 1 {
		t.Fatal(all, err)
	}
}
func TestSummaryCorrectionPropagation(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	scope := owner()
	source := mustIngest(t, s, scope, input())
	var claim memory.Ref
	err := s.pool.QueryRow(ctx, "SELECT 1").Scan(new(int))
	if err != nil {
		t.Fatal(err)
	}
	// Use the same canonical write path as extraction and user capture.
	claim = rememberForContinuity(t, s, scope, source.Ref, "以后想去那家河边的旧书店")
	before, err := s.Summarize(ctx, scope, memory.SummaryRequest{ID: source.ID})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(before.Text, "intention") {
		t.Fatal(before)
	}
	replacement := memory.Claim{}
	replacement.Value = json.RawMessage(`"已不打算去了"`)
	replacement.Nature = "intention"
	replacement.Confirmation = "confirmed"
	expanded, err := s.Expand(ctx, scope, memory.ExpandRequest{Refs: []memory.Ref{claim}})
	if err != nil {
		t.Fatal(err)
	}
	replacement = expanded.Claims[0]
	replacement.Value = json.RawMessage(`"已不打算去了"`)
	fixed, err := s.Correct(ctx, scope, memory.CorrectRequest{Target: claim, Replacement: replacement, Reason: "原理解有误"})
	if err != nil {
		t.Fatal(err)
	}
	after, err := s.Summarize(ctx, scope, memory.SummaryRequest{ID: source.ID})
	if err != nil {
		t.Fatal(err)
	}
	if after.Cached || after.Ref.Version <= before.Ref.Version || !strings.Contains(after.Text, "已不打算去了") {
		t.Fatal(after)
	}
	if err = s.ConfigureActivity(ctx, scope, memory.ActivitySettings{Ref: fixed, HalfLifeDays: 7, ReinforcementLimit: 3, Pinned: true}); err != nil {
		t.Fatal(err)
	}
	var days float64
	var pinned bool
	if err = s.pool.QueryRow(ctx, "SELECT half_life_seconds/86400,pinned FROM activity WHERE owner_id=$1 AND record_id=$2", string(scope.OwnerID), string(fixed.ID)).Scan(&days, &pinned); err != nil || days != 7 || !pinned {
		t.Fatal(days, pinned, err)
	}
}

func rememberForContinuity(t *testing.T, s *Store, scope memory.Scope, source memory.Ref, text string) memory.Ref {
	t.Helper()
	var ref memory.Ref
	err := pgx.BeginFunc(context.Background(), s.pool, func(tx pgx.Tx) error {
		if err := s.ensureOwner(context.Background(), tx, scope); err != nil {
			return err
		}
		var err error
		ref, err = s.rememberTx(context.Background(), tx, scope, statement{Text: text, Subject: "用户", Predicate: "愿望", Nature: "intention", Confirmation: "adopted", Actor: "ai", Quote: text, Source: source})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return ref
}

func TestArchiveDeletionErasesOriginalCopies(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	scope := owner()
	files, err := blob.NewFiles(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.SetBlobs(files)
	data := []byte(`{"records":[{"id":"first","text":"隐私记忆","conversation_id":"c"},{"id":"second","text":"保留的其他记录","conversation_id":"c"}]}`)
	archived, err := s.ImportArchive(ctx, scope, "export.json", data)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ProcessAttachment(ctx, leaseStage(t, s, scope, archived.Refs[0], "source.parse")); err != nil {
		t.Fatal(err)
	}
	var child memory.Ref
	child.Kind = memory.SourceKind
	if err = s.pool.QueryRow(ctx, "SELECT s.id::text,r.version FROM sources s JOIN memory_records r ON(r.owner_id,r.id)=(s.owner_id,s.id) WHERE s.owner_id=$1 AND external_id='first'", string(scope.OwnerID)).Scan(&child.ID, &child.Version); err != nil {
		t.Fatal(err)
	}
	if err = s.Delete(ctx, scope, memory.DeleteRequest{Targets: []memory.Ref{child}, BlockReimport: true}); err != nil {
		t.Fatal(err)
	}
	original, err := s.GetSource(ctx, scope, archived.Refs[0].ID, 0)
	if err != nil || !original.Source.AttachmentMissing {
		t.Fatal("archive copy retained", original, err)
	}
	var kept int
	if err = s.pool.QueryRow(ctx, "SELECT count(*) FROM sources WHERE owner_id=$1 AND external_id='second'", string(scope.OwnerID)).Scan(&kept); err != nil || kept != 1 {
		t.Fatal("unrelated source erased", err)
	}
	data = append(data, ' ')
	retry, err := s.ImportArchive(ctx, scope, "new-export.json", data)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ProcessAttachment(ctx, leaseStage(t, s, scope, retry.Refs[0], "source.parse")); err != nil {
		t.Fatal(err)
	}
	original, err = s.GetSource(ctx, scope, retry.Refs[0].ID, 0)
	if err != nil || !original.Source.AttachmentMissing {
		t.Fatal("blocked text survived in new archive", err)
	}
}

func TestDecayRecallAndSnoozeStayIndependent(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	scope := owner()
	in := input()
	in.Text = "以后想去河边旧书店"
	src := mustIngest(t, s, scope, in)
	claim := rememberForContinuity(t, s, scope, src.Ref, in.Text)
	if _, err := s.pool.Exec(ctx, "UPDATE activity SET last_effective_use_at=now()-interval '5 years',half_life_seconds=86400,stability=2.9,reinforcement_limit=3 WHERE owner_id=$1 AND record_id=$2", string(scope.OwnerID), string(claim.ID)); err != nil {
		t.Fatal(err)
	}
	var before time.Time
	if err := s.pool.QueryRow(ctx, "SELECT last_effective_use_at FROM activity WHERE owner_id=$1 AND record_id=$2", string(scope.OwnerID), string(claim.ID)).Scan(&before); err != nil {
		t.Fatal(err)
	}
	out, err := s.Recall(ctx, scope, memory.RecallRequest{Query: "河边旧书店", Mode: memory.Remember})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, ref := range out.Memories {
		found = found || ref.ID == claim.ID
	}
	if !found {
		t.Fatal("dormant memory disappeared")
	}
	var after time.Time
	if err = s.pool.QueryRow(ctx, "SELECT last_effective_use_at FROM activity WHERE owner_id=$1 AND record_id=$2", string(scope.OwnerID), string(claim.ID)).Scan(&after); err != nil || !after.Equal(before) {
		t.Fatal("retrieval reinforced memory", err)
	}
	event := memory.UseEvent{Ref: claim, EventID: "actually-adopted", Kind: "adoption", At: time.Now()}
	for range 2 {
		if err = s.RecordUse(ctx, scope, event); err != nil {
			t.Fatal(err)
		}
	}
	var stability float64
	if err = s.pool.QueryRow(ctx, "SELECT stability FROM activity WHERE owner_id=$1 AND record_id=$2", string(scope.OwnerID), string(claim.ID)).Scan(&stability); err != nil || stability != 3 {
		t.Fatal("reinforcement limit ignored", stability, err)
	}
	st := workspaceCommand(t, s, scope, workspace.Command{Type: "addIdea", Title: "等书店开门"})
	id := st.Ideas[0].ID
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "ideaShelve", ID: id, Condition: "书店开门"})
	item := st.Ideas[0]
	item.Status = "awakened"
	item.Wake = &workspace.Wake{At: stamp(), Reason: "测试到期提醒"}
	if err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error { return saveItem(ctx, tx, scope, item) }); err != nil {
		t.Fatal(err)
	}
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "ideaSnooze", ID: id, Days: 3})
	if st.Ideas[0].Status != "awakened" || st.Ideas[0].Wake.SnoozedUntil == "" {
		t.Fatal("snooze changed action state")
	}
	if err = s.CheckReminders(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "ideaStopReminders", ID: id})
	if st.Ideas[0].RemindersOn || st.Ideas[0].Status != "awakened" {
		t.Fatal("stop changed action state")
	}
	expanded, err := s.Expand(ctx, scope, memory.ExpandRequest{Refs: []memory.Ref{claim}})
	if err != nil || !strings.Contains(string(expanded.Claims[0].Value), "旧书店") {
		t.Fatal("reminder altered fact", err)
	}
}

func TestConcurrentSummaryAndGrantRevocation(t *testing.T) {
	s, scope, _ := phase2RTSetup(t)
	ctx := context.Background()
	in := input()
	in.Text = "小林说以后想去旧书店"
	src := mustIngest(t, s, scope, in)
	claim := rememberForContinuity(t, s, scope, src.Ref, in.Text)
	var wg sync.WaitGroup
	failures := make(chan error, 6)
	for range 6 {
		wg.Go(func() { _, err := s.Summarize(ctx, scope, memory.SummaryRequest{ID: src.ID}); failures <- err })
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal("concurrent summary", err)
		}
	}
	agent := phase2RTTask(t, s, scope, "phase2-model", "secretary", phase2RTUnscoped())
	for _, id := range []memory.ID{src.ID, claim.ID} {
		if _, err := s.pool.Exec(ctx, "INSERT INTO record_grants(owner_id,record_id,principal_id) VALUES($1,$2,$3) ON CONFLICT DO NOTHING", string(scope.OwnerID), string(id), agent.PrincipalID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Summarize(ctx, agent, memory.SummaryRequest{ID: src.ID}); !errors.Is(err, memory.ErrForbidden) {
		t.Fatal("coarse grant alone permitted raw-root summary", err)
	}
	policy, _ := phase2RTAuthorize(t, s, scope, src.Ref, "phase2-model", "secretary", phase2RTUnscoped())
	before, err := s.Summarize(ctx, agent, memory.SummaryRequest{ID: src.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.pool.Exec(ctx, "DELETE FROM record_grants WHERE owner_id=$1 AND record_id=$2 AND principal_id=$3", string(scope.OwnerID), string(claim.ID), agent.PrincipalID); err != nil {
		t.Fatal(err)
	}
	after, err := s.Summarize(ctx, agent, memory.SummaryRequest{ID: src.ID})
	if err != nil || after.Cached || after.Ref.Version <= before.Ref.Version {
		t.Fatal("grant cache remained", err)
	}
	for _, dep := range after.Dependencies {
		if dep.ID == claim.ID {
			t.Fatal("revoked claim retained")
		}
	}
	phase2RTUpdatePolicy(t, s, scope, src.Ref, policy, true)
	if _, err := s.Summarize(ctx, agent, memory.SummaryRequest{ID: src.ID}); !errors.Is(err, memory.ErrForbidden) {
		t.Fatal("explicit source revoke permitted cached raw-root summary", err)
	}
}
