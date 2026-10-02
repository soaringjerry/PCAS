package postgres

import (
	"archive/zip"
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/connectors"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func TestPhase2B4_I1_ZipPreviewCountsMediaGapsAndNeverWrites(t *testing.T) {
	s, scope := b4Store(t), owner()
	conversations := b4Conversations(t, "preview", 3)
	for n := range conversations {
		msg := conversations[n].Messages[0]
		conversations[n].Messages = append(conversations[n].Messages, b4Message{ID: msg.ID + "-a", Role: "assistant", Text: "合成 AI 回答。", At: msg.At.Add(time.Minute)})
	}
	data := b4Zip(t, b4Export(conversations), true)
	before := b1DatabaseRows(t, s, false)
	preview := b4PreviewFile(t, s, scope, "chatgpt-export.zip", data)
	if preview.Name != "chatgpt-export.zip" || preview.Conversations != 3 || preview.Messages != 6 || preview.FromUser != 3 || preview.AlreadyImported != 0 || preview.LeftOut != 0 || preview.Blocked != 0 {
		t.Errorf("preview counts: %+v", preview)
	}
	first, last := conversations[0].Messages[0].At, conversations[2].Messages[1].At
	if !b4Instant(t, preview.Earliest).Equal(first) || !b4Instant(t, preview.Latest).Equal(last) {
		t.Errorf("preview range: %+v; expected %s..%s", preview, first, last)
	}
	gaps := strings.Join(preview.Gaps, "\n")
	if !strings.Contains(gaps, "图片") && !strings.Contains(gaps, "photo-1.png") {
		t.Error("preview does not explain skipped image")
	}
	if !strings.Contains(gaps, "音频") && !strings.Contains(gaps, "audio-1.mp3") {
		t.Error("preview does not explain skipped audio")
	}
	if !reflect.DeepEqual(before, b1DatabaseRows(t, s, false)) {
		t.Error("read-only preview mutated database")
	}
}

func TestPhase2B4_I4_BlockedMessageIsReportedAndSkippedWithoutFailingImport(t *testing.T) {
	s, scope := b4Store(t), owner()
	original := b4Conversations(t, "blocked-original", 2)
	id, archive := b4ImportFile(t, s, scope, "before-ban.zip", b4Zip(t, b4Export(original), false))
	b4Complete(t, s, scope, id, archive)
	bannedText := original[0].Messages[0].Text
	banned := b4SourceByText(t, s, scope, bannedText)
	w := b4HTTP(t, s, scope, "POST", "/v1/memory/delete", memory.DeleteRequest{Targets: []memory.Ref{banned}, BlockReimport: true})
	b4OK(t, w)
	var blocks int
	if err := s.pool.QueryRow(context.Background(), `SELECT count(*) FROM reimport_blocks WHERE owner_id=$1`, string(scope.OwnerID)).Scan(&blocks); err != nil || blocks == 0 {
		t.Fatalf("ban fixture did not create a reimport block: %d %v", blocks, err)
	}
	newConversations := b4Conversations(t, "blocked-new", 1)
	extended := append(append([]b4Conversation{}, original...), newConversations...)
	data := b4Zip(t, b4Export(extended), false)
	before := b1DatabaseRows(t, s, false)
	preview := b4PreviewFile(t, s, scope, "after-ban.zip", data)
	if preview.Messages != 3 || preview.AlreadyImported != 1 || preview.LeftOut != 0 || preview.Blocked != 1 {
		t.Errorf("blocked must be a separate preview classification: %+v", preview)
	}
	if preview.Messages-preview.AlreadyImported-preview.LeftOut-preview.Blocked != 1 {
		t.Error("wrong actual import count")
	}
	if strings.TrimSpace(strings.Join(preview.Gaps, " ")) == "" {
		t.Error("blocked preview lacks a human explanation in gaps")
	}
	if !reflect.DeepEqual(before, b1DatabaseRows(t, s, false)) {
		t.Error("blocked preview wrote data")
	}
	// Upload the new export through the real HTTP entry. Its original may be
	// made unavailable to honor the existing privacy/reimport-block contract.
	w = b4Upload(t, b4API(s, scope, true), "/v1/connectors/archive", "after-ban.zip", data)
	b4OK(t, w)
	var uploaded struct{ BatchID string }
	b4JSON(t, w.Body.Bytes(), &uploaded)
	if uploaded.BatchID == "" {
		t.Fatal("blocked-containing upload omitted batchId")
	}
	item := b4ImportItemFor(t, s, scope, uploaded.BatchID)
	ref := memory.Ref{ID: memory.ID(item.ArchiveID), Version: item.ArchiveVersion, Kind: memory.SourceKind}
	done := b4Complete(t, s, scope, uploaded.BatchID, ref)
	if done.Total != 1 || done.Stored != 1 || done.State != "done" || done.ErrorCode != "" || done.Error != "" {
		t.Errorf("blocked/existing messages must not count or fail the batch: %+v", done)
	}
	var restored bool
	if err := s.pool.QueryRow(context.Background(), `SELECT EXISTS(SELECT 1 FROM source_versions WHERE owner_id=$1 AND body=$2)`, string(scope.OwnerID), bannedText).Scan(&restored); err != nil {
		t.Fatal(err)
	}
	if restored {
		t.Error("message banned from reimport was restored")
	}
	b1Active(t, s, scope, banned, false)
	b4MessagesExactlyOnce(t, s, scope, append(original[1:], newConversations...))
	if err := s.pool.QueryRow(context.Background(), `SELECT count(*) FROM reimport_blocks WHERE owner_id=$1`, string(scope.OwnerID)).Scan(&blocks); err != nil || blocks == 0 {
		t.Errorf("successful import removed the user's ban: %d %v", blocks, err)
	}
}

func TestPhase2B4_I2_PartialImportOriginalReachesNewSecretaryConversation(t *testing.T) {
	s, scope := b4Store(t), owner()
	f := b4Model(t, s)
	b4SmallChunks(t, 1)
	conversations := b4Conversations(t, "partial", 2000)
	gold := b4FixtureFor(t, "recall")
	last := len(conversations) - 1
	conversations[last].Messages[0].Text = gold.Text
	for n := range conversations {
		conversations[n].Messages[0].At = b4Instant(t, gold.At).Add(-time.Duration(last-n) * time.Hour)
	}
	// Keep the message genuinely from 2024 and in the newest conversation.
	id, archive := b4ImportFile(t, s, scope, "partial.zip", b4Zip(t, b4Export(conversations), false))
	p := b4ParseAsync(t, s, scope, archive)
	b4Partial(t, s, scope, id, 1000)
	var extractions int
	if err := s.pool.QueryRow(context.Background(), `SELECT count(*) FROM source_extractions WHERE owner_id=$1 AND state IN ('done','empty')`, string(scope.OwnerID)).Scan(&extractions); err != nil {
		t.Fatal(err)
	}
	if extractions != 0 {
		t.Fatal("fixture has completed extraction before raw recall")
	}
	// A first fresh conversation proves there is another conversation to leave.
	first := mustTurn(t, s, scope, turnRequest("开启另一段合成聊天"))
	f.set(`{"reply":"密码8624。","used":["S1"],"actions":[]}`, 200)
	req := turnRequest(gold.Question)
	out := mustTurn(t, s, scope, req)
	if out.ConversationID == first.ConversationID {
		t.Error("recall did not switch conversation")
	}
	actual := f.last(t)
	b1Contains(t, actual.Prompt, gold.Text, "2024", "说于", "用户")
	source := b4SourceByText(t, s, scope, gold.Text)
	b1HasRef(t, b1Refs(t, s, scope, req.RequestID), source, true)
	p.cancel()
	p.await(t, true)
}

func TestPhase2B4_I3_PauseStopsStorageAndExtractionButNotOrdinarySource(t *testing.T) {
	s, scope := b4Store(t), owner()
	f := b4Model(t, s)
	b4SmallChunks(t, 1)
	conversations := b4Conversations(t, "pause", 2000)
	id, archive := b4ImportFile(t, s, scope, "pause.zip", b4Zip(t, b4Export(conversations), false))
	p := b4ParseAsync(t, s, scope, archive)
	partial := b4Partial(t, s, scope, id, 10)
	paused := b4ImportAction(t, s, scope, id, "pause")
	if paused.State != "paused" {
		t.Fatalf("pause state %+v", paused)
	}
	p.await(t, false)
	stable := b4ImportItemFor(t, s, scope, id)
	if stable.Stored < partial.Stored || stable.Stored > paused.Stored+1 {
		t.Errorf("pause stored beyond current small batch: pause=%d after=%d", paused.Stored, stable.Stored)
	}
	normalText := "普通新资料紫玉铃铛5508，今天喜欢步行。"
	source := b1Source(t, s, scope, "新资料", normalText, "manual")
	stop, _ := b4QueueWorker(t, s)
	b4Wait(t, "ordinary source extraction while archive paused", func() bool {
		var done bool
		if err := s.pool.QueryRow(context.Background(), `SELECT EXISTS(SELECT 1 FROM source_extractions WHERE owner_id=$1 AND source_id=$2 AND state IN ('done','empty'))`, string(scope.OwnerID), string(source.ID)).Scan(&done); err != nil {
			t.Fatal(err)
		}
		return done
	})
	for _, request := range f.all() {
		b1Absent(t, request.Prompt, "合成成都历史消息")
	}
	for range 10 {
		now := b4ImportItemFor(t, s, scope, id)
		if now.State != "paused" || now.Stored != stable.Stored {
			t.Fatalf("paused batch continued storing %+v", now)
		}
		time.Sleep(10 * time.Millisecond)
	}
	stop()
	resumed := b4ImportAction(t, s, scope, id, "resume")
	if resumed.State != "importing" {
		t.Fatalf("resume state %+v", resumed)
	}
	done := b4Complete(t, s, scope, id, archive)
	if done.Total != 2000 {
		t.Errorf("total=%d", done.Total)
	}
	b4MessagesExactlyOnce(t, s, scope, conversations)
}

func TestPhase2B4_I4_ReimportAndExtendedExportOnlyAddUnseenMessages(t *testing.T) {
	s, scope := b4Store(t), owner()
	conversations := b4Conversations(t, "repeat", 4)
	data := b4Zip(t, b4Export(conversations), false)
	id, archive := b4ImportFile(t, s, scope, "repeat.zip", data)
	b4Complete(t, s, scope, id, archive)
	before := b4SourceCount(t, s, scope)
	preview := b4PreviewFile(t, s, scope, "repeat.zip", data)
	if preview.Messages != 4 || preview.AlreadyImported != 4 {
		t.Errorf("duplicate preview %+v", preview)
	}
	id2, archive2 := b4ImportFile(t, s, scope, "repeat.zip", data)
	if b4ImportItemFor(t, s, scope, id2).State != "done" {
		b4Complete(t, s, scope, id2, archive2)
	}
	if after := b4SourceCount(t, s, scope); after != before {
		t.Errorf("identical file added sources: %d -> %d", before, after)
	}
	newConversations := b4Conversations(t, "extension", 3)
	all := append(append([]b4Conversation{}, conversations...), newConversations...)
	extended := b4Zip(t, b4Export(all), false)
	preview = b4PreviewFile(t, s, scope, "extended.zip", extended)
	if preview.Messages != 7 || preview.AlreadyImported != 4 {
		t.Errorf("extended preview %+v", preview)
	}
	id3, archive3 := b4ImportFile(t, s, scope, "extended.zip", extended)
	b4Complete(t, s, scope, id3, archive3)
	b4MessagesExactlyOnce(t, s, scope, all)
	if after := b4SourceCount(t, s, scope); after != before+4 {
		t.Errorf("extended export should add 3 messages plus its original archive: %d -> %d", before, after)
	}
}

func TestPhase2B4_I5_HistoricalPlanRemainsCandidateWithoutPresentActions(t *testing.T) {
	s, scope := b4Store(t), owner()
	f := b4Model(t, s)
	gold := b4FixtureFor(t, "oldPlan")
	workspaceCommand(t, s, scope, workspace.Command{Type: "updateSettings", Patch: asJSON(map[string]bool{"autoAccept": true, "wakeIdeas": true})})
	state := workspaceCommand(t, s, scope, workspace.Command{Type: "addIdea", Title: "去成都"})
	state = workspaceCommand(t, s, scope, workspace.Command{Type: "ideaShelve", ID: state.Ideas[0].ID, Condition: "旅行计划交上去了"})
	idea := state.Ideas[0]
	conv := b4Conversations(t, "old-plan", 1)
	conv[0].Messages[0].Text = gold.Text
	conv[0].Messages[0].At = b4Instant(t, gold.At)
	id, archive := b4ImportFile(t, s, scope, "old-plan.zip", b4Zip(t, b4Export(conv), false))
	b4Complete(t, s, scope, id, archive)
	item := b1ExtractItem(gold.Text, "plan")
	item["places"] = []string{"成都"}
	f.set(string(asJSON(map[string]any{"items": []any{item, map[string]any{"kind": "task", "text": gold.Text, "quote": gold.Text, "confidence": 1, "explicit": true, "acquisition": "direct"}}, "signals": []conditionSignal{{IdeaID: idea.ID, ConditionID: idea.Conditions[0].ID, Quote: gold.Text, Explanation: "合成触发信号", Confidence: 0.99}}})), 200)
	source := b4SourceByText(t, s, scope, gold.Text)
	if err := s.ProcessExtraction(context.Background(), leaseStage(t, s, scope, source, "source.extract")); err != nil {
		t.Fatal(err)
	}
	state, err := s.Snapshot(context.Background(), scope)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Tasks) != 0 {
		t.Errorf("old plan produced current tasks %+v", state.Tasks)
	}
	if len(state.Ideas) != 1 || state.Ideas[0].Status != "shelved" || state.Ideas[0].Wake != nil {
		t.Errorf("old plan awakened idea %+v", state.Ideas)
	}
	claim := b1MemoryRef(t, state.Memories, gold.Text)
	var confirmation string
	var expressed time.Time
	if err := s.pool.QueryRow(context.Background(), `SELECT c.confirmation,r.expressed_at FROM claim_revisions c JOIN record_versions r ON (r.owner_id,r.record_id,r.version)=(c.owner_id,c.claim_id,c.version) WHERE c.owner_id=$1 AND c.claim_id=$2 AND c.version=$3`, string(scope.OwnerID), string(claim.ID), claim.Version).Scan(&confirmation, &expressed); err != nil {
		t.Fatal(err)
	}
	if confirmation != gold.Confirmation || !expressed.Equal(b4Instant(t, gold.At)) {
		t.Errorf("historical claim confirmation=%s expressed=%s", confirmation, expressed)
	}
}

func TestPhase2B4_I6_SecretaryOriginalExtractionPrecedes500ImportedMessages(t *testing.T) {
	s, scope := b4Store(t), owner()
	f := b4Model(t, s)
	b4SmallChunks(t, 1)
	conversations := b4Conversations(t, "priority", 500)
	id, archive := b4ImportFile(t, s, scope, "priority.zip", b4Zip(t, b4Export(conversations), false))
	p := b4ParseAsync(t, s, scope, archive)
	b4Partial(t, s, scope, id, 10)
	current := "我现在喜欢清晨整理赤玉铃铛6173。"
	req := turnRequest(current)
	mustTurn(t, s, scope, req)
	p.await(t, false)
	source := b1TurnSource(t, s, scope, req.RequestID)
	offset := len(f.all())
	stop, _ := b4QueueWorker(t, s)
	b4Wait(t, "new secretary original extraction", func() bool {
		var done bool
		if err := s.pool.QueryRow(context.Background(), `SELECT EXISTS(SELECT 1 FROM source_extractions WHERE owner_id=$1 AND source_id=$2 AND state IN ('done','empty'))`, string(scope.OwnerID), string(source.ID)).Scan(&done); err != nil {
			t.Fatal(err)
		}
		return done
	})
	stop()
	requests := f.all()[offset:]
	if len(requests) == 0 {
		t.Fatal("no actual background model request")
	}
	b1Contains(t, requests[0].Prompt, current)
	b1Absent(t, requests[0].Prompt, "合成成都历史消息")
}

func TestPhase2B4_I9_CancelAtHalfCommitThenRecoverExpiredLease(t *testing.T) {
	s, scope := b4Store(t), owner()
	b4SmallChunks(t, 1)
	conversations := b4Conversations(t, "restart", 2000)
	id, archive := b4ImportFile(t, s, scope, "restart.zip", b4Zip(t, b4Export(conversations), false))
	p := b4ParseAsync(t, s, scope, archive)
	partial := b4Partial(t, s, scope, id, 1000)
	p.cancel()
	p.await(t, true)
	stopped := b4ImportItemFor(t, s, scope, id)
	if stopped.Stored < partial.Stored || stopped.Stored >= stopped.Total {
		t.Fatalf("not a real partial cancellation: %+v", stopped)
	}
	if _, err := s.pool.Exec(context.Background(), `UPDATE memory_jobs SET lease_until=now()-interval '1 second' WHERE owner_id=$1 AND id=$2`, string(scope.OwnerID), string(p.job.ID)); err != nil {
		t.Fatal(err)
	}
	reclaimed, err := s.Claim(context.Background(), time.Minute)
	if err != nil || reclaimed == nil || reclaimed.ID != p.job.ID || reclaimed.LeaseToken == p.job.LeaseToken {
		t.Fatalf("real expired lease did not recover owned parse: %+v, %v", reclaimed, err)
	}
	if err := s.ProcessAttachment(context.Background(), *reclaimed); err != nil {
		t.Fatal("recovered production parser", err)
	}
	done := b4ImportItemFor(t, s, scope, id)
	if done.State != "done" || done.Total != 2000 || done.Stored != 2000 {
		t.Errorf("recovered import %+v", done)
	}
	b4MessagesExactlyOnce(t, s, scope, conversations)
}

func TestPhase2B4_I10_DeleteArchiveChildrenClaimsBatchAndRecallControl(t *testing.T) {
	s, scope := b4Store(t), owner()
	f := b4Model(t, s)
	gold := b4FixtureFor(t, "recall")
	conversations := b4Conversations(t, "delete", 3)
	conversations[2].Messages[0].Text = gold.Text
	id, archive := b4ImportFile(t, s, scope, "delete.zip", b4Zip(t, b4Export(conversations), false))
	b4Complete(t, s, scope, id, archive)
	source := b4SourceByText(t, s, scope, gold.Text)
	f.set(string(asJSON(map[string]any{"items": []any{b1ExtractItem(gold.Text, "fact")}})), 200)
	if err := s.ProcessExtraction(context.Background(), leaseStage(t, s, scope, source, "source.extract")); err != nil {
		t.Fatal(err)
	}
	state, err := s.Snapshot(context.Background(), scope)
	if err != nil {
		t.Fatal(err)
	}
	claim := b1MemoryRef(t, state.Memories, gold.Text)
	f.set(`{"reply":"密码8624。","used":["S1"],"actions":[]}`, 200)
	mustTurn(t, s, scope, turnRequest(gold.Question))
	b1Contains(t, f.last(t).Prompt, gold.Text)
	deleted := b4HTTP(t, s, scope, "POST", "/v1/memory/delete", memory.DeleteRequest{Targets: []memory.Ref{archive}, IncludeSources: true})
	b4OK(t, deleted)
	for _, item := range b4Imports(t, s, scope) {
		if item.ID == id {
			t.Error("deleted import batch remains in progress API")
		}
	}
	var exists bool
	if err := s.pool.QueryRow(context.Background(), `SELECT EXISTS(SELECT 1 FROM import_batches WHERE owner_id=$1 AND id=$2)`, string(scope.OwnerID), id).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Error("deleted batch remains in table")
	}
	for _, ref := range []memory.Ref{archive, source, claim} {
		b1Active(t, s, scope, ref, false)
	}
	for _, conv := range conversations {
		if err := s.pool.QueryRow(context.Background(), `SELECT EXISTS(SELECT 1 FROM source_versions WHERE owner_id=$1 AND body=$2)`, string(scope.OwnerID), conv.Messages[0].Text).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if exists {
			t.Errorf("deleted import original persists: %s", conv.ID)
		}
	}
	f.set(`{"reply":"没有这份资料。","used":[],"actions":[]}`, 200)
	mustTurn(t, s, scope, turnRequest(gold.Question))
	b1Absent(t, f.last(t).Prompt, gold.Text)
}

func TestPhase2B4_I11_RegeneratedAnswerPreservesHistoricalBranch(t *testing.T) {
	s, scope := b4Store(t), owner()
	gold := b4FixtureFor(t, "branch")
	conv := b4Conversations(t, "branch", 1)
	at := conv[0].Messages[0].At
	conv[0].Messages = append(conv[0].Messages, b4Message{ID: "active-answer", Role: "assistant", Text: gold.Active, At: at.Add(2 * time.Minute)})
	conv[0].Branch = &b4Message{ID: "abandoned-answer", Role: "assistant", Text: gold.Abandoned, At: at.Add(time.Minute)}
	id, archive := b4ImportFile(t, s, scope, "branch.zip", b4Zip(t, b4Export(conv), false))
	done := b4Complete(t, s, scope, id, archive)
	if done.Total != 3 {
		t.Errorf("both branches must be preserved, total=%d", done.Total)
	}
	active := b4SourceByText(t, s, scope, gold.Active)
	abandoned := b4SourceByText(t, s, scope, gold.Abandoned)
	b4RoleAndTime(t, s, scope, abandoned, "assistant", at.Add(time.Minute), "historical")
	b4RoleAndTime(t, s, scope, active, "assistant", at.Add(2*time.Minute), "")
	var branch string
	if err := s.pool.QueryRow(context.Background(), `SELECT branch FROM source_contexts WHERE owner_id=$1 AND source_id=$2`, string(scope.OwnerID), string(active.ID)).Scan(&branch); err != nil {
		t.Fatal(err)
	}
	if branch == "historical" {
		t.Error("current regenerated answer marked as abandoned")
	}
}

func TestPhase2B4_I12_ConcurrentImportsPauseOnlyOneBatch(t *testing.T) {
	s, scope := b4Store(t), owner()
	b4SmallChunks(t, 1)
	first, second := b4Conversations(t, "concurrent-a", 2000), b4Conversations(t, "concurrent-b", 2000)
	idA, archiveA := b4ImportFile(t, s, scope, "first.zip", b4Zip(t, b4Export(first), false))
	idB, archiveB := b4ImportFile(t, s, scope, "second.zip", b4Zip(t, b4Export(second), false))
	a, b := b4ParseAsync(t, s, scope, archiveA), b4ParseAsync(t, s, scope, archiveB)
	b4Partial(t, s, scope, idA, 10)
	b4Partial(t, s, scope, idB, 10)
	paused := b4ImportAction(t, s, scope, idA, "pause")
	if paused.State != "paused" {
		t.Fatal(paused)
	}
	a.await(t, false)
	stable := b4ImportItemFor(t, s, scope, idA)
	b.await(t, false)
	done := b4ImportItemFor(t, s, scope, idB)
	if done.State != "done" || done.Total != 2000 || done.Stored != 2000 {
		t.Errorf("other concurrent import affected %+v", done)
	}
	if now := b4ImportItemFor(t, s, scope, idA); now.State != "paused" || now.Stored != stable.Stored {
		t.Errorf("paused batch changed while other finished: %+v -> %+v", stable, now)
	}
	b4MessagesExactlyOnce(t, s, scope, second)
	b4ImportAction(t, s, scope, idA, "resume")
	b4Complete(t, s, scope, idA, archiveA)
	b4MessagesExactlyOnce(t, s, scope, first)
}

func TestPhase2B4_I13_SpokenDateUsesSydneyAndShanghai(t *testing.T) {
	var cases []struct{ Zone, Instant, Date string }
	b4JSON(t, b4Gold(t).Fixtures["timezone"], &cases)
	for _, tc := range cases {
		t.Run(tc.Zone, func(t *testing.T) {
			s, scope := b4Store(t), owner()
			f := b4Model(t, s)
			workspaceCommand(t, s, scope, workspace.Command{Type: "updateSettings", Patch: asJSON(map[string]string{"timezone": tc.Zone})})
			gold := b4FixtureFor(t, "recall")
			conv := b4Conversations(t, "local-date", 1)
			conv[0].Messages[0].Text = gold.Text
			conv[0].Messages[0].At = b4Instant(t, tc.Instant)
			id, archive := b4ImportFile(t, s, scope, "date.zip", b4Zip(t, b4Export(conv), false))
			b4Complete(t, s, scope, id, archive)
			f.set(`{"reply":"密码8624。","used":["S1"],"actions":[]}`, 200)
			mustTurn(t, s, scope, turnRequest(gold.Question))
			actual := f.last(t)
			b1Contains(t, actual.Prompt, gold.Text, "说于", tc.Date, "用户")
			source := b4SourceByText(t, s, scope, gold.Text)
			b4RoleAndTime(t, s, scope, source, "user", b4Instant(t, tc.Instant), "")
		})
	}
}

func TestPhase2B4_I14_NonUserMessagesRemainOriginalWithoutExtractionCalls(t *testing.T) {
	s, scope := b4Store(t), owner()
	f := b4Model(t, s)
	gold := b4FixtureFor(t, "assistant")
	conv := b4Conversations(t, "roles", 1)
	base := b4Instant(t, gold.At)
	conv[0].Messages = []b4Message{{ID: "system-msg", Role: "system", Text: "合成系统消息金色木马1717。", At: base.Add(-time.Minute)}, {ID: "tool-msg", Role: "tool", Text: "合成工具结果橙色木马1818。", At: base}, {ID: "assistant-msg", Role: "assistant", Text: gold.Text, At: base.Add(time.Minute)}}
	id, archive := b4ImportFile(t, s, scope, "roles.zip", b4Zip(t, b4Export(conv), false))
	b4Complete(t, s, scope, id, archive)
	for _, message := range conv[0].Messages {
		source := b4SourceByText(t, s, scope, message.Text)
		b4RoleAndTime(t, s, scope, source, message.Role, message.At, "")
		if err := s.ProcessExtraction(context.Background(), leaseStage(t, s, scope, source, "source.extract")); err != nil {
			t.Fatal(err)
		}
		var state string
		var items, extractor int
		if err := s.pool.QueryRow(context.Background(), `SELECT state,items,extractor FROM source_extractions WHERE owner_id=$1 AND source_id=$2 AND source_version=$3`, string(scope.OwnerID), string(source.ID), source.Version).Scan(&state, &items, &extractor); err != nil {
			t.Fatal(err)
		}
		if state != "empty" || items != 0 || extractor != 2 {
			t.Errorf("nonuser extraction marker %s/%d/%d", state, items, extractor)
		}
	}
	if len(f.all()) != 0 {
		t.Fatal("nonuser original was sent to extraction model")
	}
	f.set(`{"reply":"备用号码3816。","used":["S1"],"actions":[]}`, 200)
	mustTurn(t, s, scope, turnRequest(gold.Question))
	b1Contains(t, f.last(t).Prompt, gold.Text, "AI", "说于")
	if len(f.all()) != 1 {
		t.Error("assistant recall performed unexpected model calls")
	}
}

func TestPhase2B4_I7_UnsupportedAndDecompressedLimitAreAtomic(t *testing.T) {
	s, scope := b4Store(t), owner()
	old := connectors.MaxArchiveBytes
	connectors.MaxArchiveBytes = 128
	t.Cleanup(func() { connectors.MaxArchiveBytes = old })
	data := b4Zip(t, b4Export(b4Conversations(t, "decompressed", 2)), false)
	for _, endpoint := range []string{"/v1/connectors/archive/preview", "/v1/connectors/archive"} {
		for _, tc := range []struct {
			name   string
			data   []byte
			code   string
			status int
		}{{"unrecognized.bin", []byte("unknown bytes"), "unsupported_archive", 400}, {"large.zip", data, "archive_too_large", 413}} {
			before := b1DatabaseRows(t, s, false)
			w := b4Upload(t, b4API(s, scope, true), endpoint, tc.name, tc.data)
			var result struct{ Error, Message string }
			b4JSON(t, w.Body.Bytes(), &result)
			if w.Code != tc.status || result.Error != tc.code {
				t.Errorf("%s: %d %s", tc.name, w.Code, w.Body.String())
			}
			b4NonemptyText(t, result.Message)
			if !reflect.DeepEqual(before, b1DatabaseRows(t, s, false)) {
				t.Errorf("rejected archive %s left data", tc.name)
			}
		}
	}
}

func TestPhase2B4_I3_ImportStateErrorsHaveFrozenCode(t *testing.T) {
	s, scope := b4Store(t), owner()
	id, archive := b4ImportFile(t, s, scope, "state.zip", b4Zip(t, b4Export(b4Conversations(t, "state", 1)), false))
	w := b4HTTP(t, s, scope, "POST", "/v1/connectors/imports/"+id+"/resume", nil)
	if w.Code != 409 || !strings.Contains(w.Body.String(), `"error":"import_not_active"`) {
		t.Errorf("resume active: %d %s", w.Code, w.Body.String())
	}
	b4Complete(t, s, scope, id, archive)
	w = b4HTTP(t, s, scope, "POST", "/v1/connectors/imports/"+id+"/pause", nil)
	if w.Code != 409 || !strings.Contains(w.Body.String(), `"error":"import_not_active"`) {
		t.Errorf("pause completed: %d %s", w.Code, w.Body.String())
	}
	w = b4HTTP(t, s, scope, "POST", "/v1/connectors/imports/"+string(memory.NewID())+"/pause", nil)
	if w.Code != 404 {
		t.Errorf("pause missing: %d %s", w.Code, w.Body.String())
	}
}

func TestPhase2B4_I7_FourUploadErrorsAreDistinctAndAtomic(t *testing.T) {
	s, scope := b4Store(t), owner()
	oldUpload := connectors.MaxUploadBytes
	connectors.MaxUploadBytes = 4096
	t.Cleanup(func() { connectors.MaxUploadBytes = oldUpload })
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	w, err := z.Create("photo.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("fake image")); err != nil {
		t.Fatal(err)
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		data   []byte
		code   string
		status int
	}{
		{"renamed.zip", []byte("a plain text file"), "invalid_zip", 400},
		{"empty.zip", buf.Bytes(), "no_supported_records", 400},
		{"conversations.json", []byte(`[{"id":"cut","mapping":`), "invalid_json", 400},
		{"oversize.json", bytes.Repeat([]byte(" "), 8192), "archive_too_large", 413},
	}
	for _, endpoint := range []string{"/v1/connectors/archive/preview", "/v1/connectors/archive"} {
		before := b1DatabaseRows(t, s, false)
		messages := map[string]bool{}
		for _, tc := range cases {
			response := b4Upload(t, b4API(s, scope, true), endpoint, tc.name, tc.data)
			var result struct{ Error, Message string }
			b4JSON(t, response.Body.Bytes(), &result)
			if response.Code != tc.status || result.Error != tc.code {
				t.Errorf("%s %s: HTTP %d %s", endpoint, tc.name, response.Code, response.Body.String())
			}
			b4NonemptyText(t, result.Message)
			if messages[result.Message] {
				t.Error("different failures share same human explanation")
			}
			messages[result.Message] = true
			if !reflect.DeepEqual(before, b1DatabaseRows(t, s, false)) {
				t.Errorf("invalid upload %s wrote data", tc.name)
			}
		}
	}
}

func TestPhase2B4_I8_RecordCapKeepsNewestAndReportsLeftOut(t *testing.T) {
	s, scope := b4Store(t), owner()
	old := connectors.MaxArchiveRecords
	connectors.MaxArchiveRecords = 3
	t.Cleanup(func() { connectors.MaxArchiveRecords = old })
	conversations := b4Conversations(t, "cap", 7)
	// Input order is deliberately neither ascending nor descending.
	input := []b4Conversation{conversations[3], conversations[6], conversations[0], conversations[5], conversations[1], conversations[4], conversations[2]}
	data := b4Zip(t, b4Export(input), false)
	preview := b4PreviewFile(t, s, scope, "cap.zip", data)
	if preview.Messages != 7 || preview.LeftOut != 4 {
		t.Errorf("cap preview %+v", preview)
	}
	id, archive := b4ImportFile(t, s, scope, "cap.zip", data)
	done := b4Complete(t, s, scope, id, archive)
	if done.Total != 3 || done.Stored != 3 || done.LeftOut != 4 {
		t.Errorf("capped batch %+v", done)
	}
	var leftOut int
	if err := s.pool.QueryRow(context.Background(), `SELECT left_out FROM import_batches WHERE owner_id=$1 AND id=$2`, string(scope.OwnerID), id).Scan(&leftOut); err != nil || leftOut != 4 {
		t.Errorf("persisted left_out=%d error=%v", leftOut, err)
	}
	b4MessagesExactlyOnce(t, s, scope, conversations[4:])
	for _, conv := range conversations[:4] {
		var exists bool
		if err := s.pool.QueryRow(context.Background(), `SELECT EXISTS(SELECT 1 FROM source_versions WHERE owner_id=$1 AND body=$2)`, string(scope.OwnerID), conv.Messages[0].Text).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if exists {
			t.Errorf("older excluded message stored: %s", conv.ID)
		}
	}
}
