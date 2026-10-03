package postgres

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/testsupport"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

type b3ReceiptOracle struct {
	ContractCommit string
	K9             struct {
		AgeDays                                                         int
		SourceText, TaskTitle, MemoryText, Query, Status, KeptReceiptOp string
		PurgedChanges                                                   []any
	}
	K10 struct {
		SourceText, TaskTitle, MemoryText, ChangedText, Query, UnchangedStatus, CorrectedStatus string
		ExistingItems                                                                           int
		ReceiptUndone                                                                           bool
	}
}

func b3ReceiptGold(t *testing.T) b3ReceiptOracle {
	t.Helper()
	var gold b3ReceiptOracle
	if err := json.Unmarshal(b3Gold(t)["timelineReceiptRetention"], &gold); err != nil {
		t.Fatal(err)
	}
	if gold.ContractCommit == "" || gold.K9.AgeDays != 40 || gold.K9.Status == "" || gold.K10.UnchangedStatus == "" || gold.K10.CorrectedStatus == "" {
		t.Fatal("missing frozen K9/K10 receipt oracle")
	}
	return gold
}

// Establish the association through the actual secretary and saved creation
// receipt. Synthetic dates and claims do not invent the receipt or item id.
func b3ReceiptTurn(t *testing.T, s *Store, scope memory.Scope, f *b1Fake, text, title string) (workspace.DeskTurnRequest, workspace.DeskTurnResponse, memory.Ref, workspace.DeskReceipt) {
	t.Helper()
	f.set(string(asJSON(map[string]any{"reply": "安排好了。", "actions": []map[string]any{{"op": "create_task", "title": title}}})))
	req := turnRequest(text)
	out := mustTurn(t, s, scope, req)
	for _, receipt := range out.Turn.Receipts {
		if receipt.Op == "create_task" && receipt.Status == "done" && receipt.ActionID != nil && receipt.ThingID != nil {
			return req, out, b1TurnSource(t, s, scope, req.RequestID), receipt
		}
	}
	t.Fatalf("actual secretary turn has no successful creation receipt: %+v", out.Turn.Receipts)
	return req, out, memory.Ref{}, workspace.DeskReceipt{}
}
func b3KeptCreationReceipt(t *testing.T, s *Store, scope memory.Scope, original workspace.DeskTurnResponse, receipt workspace.DeskReceipt, undone bool) {
	t.Helper()
	history := b1History(t, s, scope, original.ConversationID)
	for _, saved := range history.Receipts {
		if saved.ActionID != nil && *saved.ActionID == *receipt.ActionID {
			if saved.Op != receipt.Op || saved.ThingID == nil || *saved.ThingID != *receipt.ThingID || saved.Undone != undone {
				t.Fatalf("saved creation receipt lost its item identity/undo state: %+v", saved)
			}
			return
		}
	}
	t.Fatal("saved turn lost its creation receipt")
}

func TestPhase2B3_K9_OldCreationReceiptSurvivesPurgedActionDetails(t *testing.T) {
	gold := b3ReceiptGold(t).K9
	s, scope := b1Store(t), owner()
	f := b1Model(t, s, b3Used())
	b3Zone(t, s, scope, "Asia/Shanghai")
	self := b3Entity(t, s, scope, "self", "本人")
	place := b3Entity(t, s, scope, "place", "成都", "成都")
	_, original, source, receipt := b3ReceiptTurn(t, s, scope, f, gold.SourceText, gold.TaskTitle)
	if receipt.Op != gold.KeptReceiptOp {
		t.Fatal("unexpected creation operation", receipt.Op)
	}
	old := testsupport.DateFromToday(t, "Asia/Shanghai", -gold.AgeDays, 12, 0)
	ref := b3Claim(t, s, scope, b3ClaimSpec{Text: gold.MemoryText, Subject: self, Said: &old, Source: source, Mentions: []b3Mention{{place, "place"}}})
	// Age the real turn, original, created item and its creation actions together.
	// Only dates are fixtures; the saved receipt remains the real response.
	b3Exec(t, s, `UPDATE desk_turns SET created_at=$3,response=jsonb_set(response,'{turn,createdAt}',to_jsonb($4::text)) WHERE owner_id=$1 AND id=$2`, scope.OwnerID, original.Turn.ID, old, old.Format(time.RFC3339))
	b3Exec(t, s, `UPDATE record_versions SET expressed_at=$3,recorded_at=$3 WHERE owner_id=$1 AND record_id=$2`, scope.OwnerID, source.ID, old)
	b3Exec(t, s, `UPDATE work_items SET created_at=$3,updated_at=$3,document=jsonb_set(jsonb_set(document,'{createdAt}',to_jsonb($4::text)),'{updatedAt}',to_jsonb($4::text)) WHERE owner_id=$1 AND id=$2`, scope.OwnerID, *receipt.ThingID, old, old.Format(time.RFC3339))
	b3Exec(t, s, `UPDATE action_log SET created_at=$3 WHERE owner_id=$1 AND turn_id=$2`, scope.OwnerID, original.Turn.ID, old)
	var details int
	if err := s.pool.QueryRow(t.Context(), `SELECT jsonb_array_length(changes) FROM action_log WHERE owner_id=$1 AND id=$2`, scope.OwnerID, *receipt.ActionID).Scan(&details); err != nil || details == 0 {
		t.Fatalf("creation action had no real details before retention: %d (%v)", details, err)
	}
	// An ordinary unrelated command invokes the existing retention path. The old
	// item is still open when its creation action's details are actually purged.
	workspaceCommand(t, s, scope, workspace.Command{Type: "addIdea", Title: "触发旧动作清理的独立想法"})
	var changes []byte
	var expired bool
	if err := s.pool.QueryRow(t.Context(), `SELECT changes,expired_at IS NOT NULL FROM action_log WHERE owner_id=$1 AND id=$2`, scope.OwnerID, *receipt.ActionID).Scan(&changes, &expired); err != nil {
		t.Fatal(err)
	}
	var decoded []any
	if err := json.Unmarshal(changes, &decoded); err != nil {
		t.Fatal(err)
	}
	if !expired || !b1SameJSON(t, decoded, gold.PurgedChanges) {
		t.Fatalf("old action details were not actually cleared: %s expired=%v", changes, expired)
	}
	b3KeptCreationReceipt(t, s, scope, original, receipt, false)
	workspaceCommand(t, s, scope, workspace.Command{Type: "setTaskStatus", ID: *receipt.ThingID, Status: "done"})
	var status string
	if err := s.pool.QueryRow(t.Context(), `SELECT status FROM work_items WHERE owner_id=$1 AND id=$2`, scope.OwnerID, *receipt.ThingID).Scan(&status); err != nil || status != "done" {
		t.Fatalf("old item did not complete after purge: %q (%v)", status, err)
	}
	b3KeptCreationReceipt(t, s, scope, original, receipt, false)
	f.set(b3Used(ref))
	req := turnRequest(gold.Query)
	out := mustTurn(t, s, scope, req)
	b1Contains(t, f.last(t).Prompt, gold.MemoryText)
	b1HasRef(t, b1Refs(t, s, scope, req.RequestID), ref, true)
	items := b3Timeline(t, out)
	if len(items) != 1 {
		t.Fatalf("timeline count=%d want 1", len(items))
	}
	item := b3TimelineText(t, out, gold.MemoryText)
	b3MustRef(t, item["memoryId"], ref)
	b3Date(t, item["at"], old)
	if item["status"] != gold.Status {
		t.Errorf("purged old creation status=%v want %s", item["status"], gold.Status)
	}
}

func TestPhase2B3_K10_UndoneCreationLeavesNoAssociatedItems(t *testing.T) {
	gold := b3ReceiptGold(t).K10
	for _, corrected := range []bool{false, true} {
		name := "unchanged_open"
		if corrected {
			name = "corrected_changed"
		}
		t.Run(name, func(t *testing.T) {
			s, scope := b1Store(t), owner()
			f := b1Model(t, s, b3Used())
			now := b3Zone(t, s, scope, "Asia/Shanghai")
			self := b3Entity(t, s, scope, "self", "本人")
			place := b3Entity(t, s, scope, "place", "成都", "成都")
			_, original, source, receipt := b3ReceiptTurn(t, s, scope, f, gold.SourceText, gold.TaskTitle)
			b1Undo(t, s, scope, *receipt.ActionID)
			var count int
			if err := s.pool.QueryRow(t.Context(), `SELECT count(*) FROM work_items WHERE owner_id=$1 AND id=$2`, scope.OwnerID, *receipt.ThingID).Scan(&count); err != nil || count != gold.ExistingItems {
				t.Fatalf("undone item still exists: %d (%v)", count, err)
			}
			b3KeptCreationReceipt(t, s, scope, original, receipt, gold.ReceiptUndone)
			// The retained original also contains background information. Construct its
			// independent fact after the immediate undo: memory withdrawal belongs to
			// batch1/batch2, while K10 tests how a surviving memory follows the receipt.
			said := now.AddDate(0, 0, -1)
			text, want := gold.MemoryText, gold.UnchangedStatus
			ref := b3Claim(t, s, scope, b3ClaimSpec{Text: text, Nature: "fact", Subject: self, Said: &said, Source: source, Mentions: []b3Mention{{place, "place"}}})
			if corrected {
				text, want = gold.ChangedText, gold.CorrectedStatus
				replacement := memory.Claim{SubjectID: self, Predicate: "验收事项", Value: asJSON(text), Nature: "fact", Acquisition: "direct", Confirmation: "confirmed"}
				updated, err := s.Correct(t.Context(), scope, memory.CorrectRequest{ChangeType: "correction", Target: ref, Replacement: replacement, Reason: "独立背景记忆的纠正"})
				if err != nil {
					t.Fatal(err)
				}
				ref = updated
				b3Exec(t, s, `UPDATE record_versions SET expressed_at=$3 WHERE owner_id=$1 AND record_id=$2 AND version=$4`, scope.OwnerID, ref.ID, said, ref.Version)
				b3Exec(t, s, `INSERT INTO claim_mentions(owner_id,claim_id,claim_version,entity_id,role) VALUES($1,$2,$3,$4,'place') ON CONFLICT DO NOTHING`, scope.OwnerID, ref.ID, ref.Version, place)
			}
			// Preserve the source association on the current version as fixture data;
			// otherwise a correction that dropped evidence could hide a stale-item bug.
			b3Exec(t, s, `INSERT INTO evidence(owner_id,id,source_id,source_version,target_id,target_version,acquisition,stance) SELECT $1,gen_random_uuid(),$2,$3,$4,$5,'direct','supports' WHERE NOT EXISTS(SELECT 1 FROM evidence WHERE owner_id=$1 AND source_id=$2 AND source_version=$3 AND target_id=$4 AND target_version=$5)`, scope.OwnerID, source.ID, source.Version, ref.ID, ref.Version)
			f.set(b3Used(ref))
			req := turnRequest(gold.Query)
			out := mustTurn(t, s, scope, req)
			b1Contains(t, f.last(t).Prompt, text)
			b1HasRef(t, b1Refs(t, s, scope, req.RequestID), ref, true)
			items := b3Timeline(t, out)
			if len(items) != 1 {
				t.Fatalf("timeline count=%d want 1", len(items))
			}
			item := b3TimelineText(t, out, text)
			b3MustRef(t, item["memoryId"], ref)
			if item["status"] != want {
				t.Errorf("no associated item: status=%v want %s", item["status"], want)
			}
			if item["thingId"] != nil && item["thingId"] != "" {
				t.Errorf("deleted item must not remain linked: %v", item["thingId"])
			}
		})
	}
}
