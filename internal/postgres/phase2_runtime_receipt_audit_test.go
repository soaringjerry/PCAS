package postgres

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func TestPhase2RuntimeOwnerUndoReceiptAuditNeverRestoresSourceText(t *testing.T) {
	for _, name := range []string{"RECEIPT-EMPTY", "RECEIPT-SOURCE"} {
		t.Run(name, func(t *testing.T) {
			s, scope, capture := phase2RTSetup(t)
			ctx := context.Background()
			title := "RECEIPT-AUDIT-EMPTY-866"
			var source memory.Ref
			var sourcePolicy phase2RTAuthResult
			if name == "RECEIPT-SOURCE" {
				source = phase2RTSource(t, s, scope).Ref
				sourcePolicy, _ = phase2RTAuthorize(t, s, scope, source, "phase2-model", "secretary", phase2RTUnscoped())
				title = "RECEIPT-AUDIT-SOURCE " + phase2RTGoldRead(t).Records[0].Atoms[0]
			}
			setReply := func(actions []any) {
				capture.mu.Lock()
				defer capture.mu.Unlock()
				capture.Reply = string(asJSON(map[string]any{"reply": "RECEIPT-AUDIT-RESULT-868", "used": []any{}, "links": []any{}, "show": []any{}, "remember": false, "ask": nil, "actions": actions}))
			}
			setReply([]any{map[string]any{"op": "create_task", "title": title}})
			question := "请创建一个安排"
			if name == "RECEIPT-SOURCE" {
				question = phase2RTGoldRead(t).Cases[0].Query + question
			}
			firstRequest := workspace.DeskTurnRequest{RequestID: string(memory.NewID()), AgentID: "phase2-model", Text: question}
			first, err := s.DeskTurn(ctx, scope, firstRequest)
			if err != nil || len(first.Turn.Receipts) != 1 || capture.count() != 1 {
				t.Fatal("real first create turn missing", err, first)
			}
			initial := first.Turn.Receipts[0]
			if initial.Op != "create_task" || initial.Status != "done" || initial.ActionID == nil || initial.ThingID == nil || !strings.Contains(initial.Text, title) {
				t.Fatal("real initial title/action receipt positive control missing", initial)
			}
			thingID := *initial.ThingID
			setReply([]any{map[string]any{"op": "update", "ref": "T1", "set": map[string]any{"due": "2026-10-05T10:00"}}})
			// Hall conversation preserves the original title in both real receipt texts.
			// Thing-page short receipts would remove it before the read gate is tested.
			secondRequest := workspace.DeskTurnRequest{RequestID: string(memory.NewID()), ConversationID: &first.ConversationID, AgentID: "phase2-model", Text: "把刚才的安排改到下周一十点"}
			if name == "RECEIPT-SOURCE" {
				secondRequest.Text = phase2RTGoldRead(t).Cases[0].Query + secondRequest.Text
			}
			second, err := s.DeskTurn(ctx, scope, secondRequest)
			if err != nil || len(second.Turn.Receipts) != 1 || capture.count() != 2 || second.ConversationID != first.ConversationID {
				t.Fatal("separate real second turn missing", err, second)
			}
			updated := second.Turn.Receipts[0]
			if updated.Op != "update" || updated.Status != "done" || updated.ActionID == nil || *updated.ActionID == *initial.ActionID || updated.ThingID == nil || *updated.ThingID != thingID || !strings.Contains(updated.Text, title) {
				t.Fatal("second real same-title/action positive control missing", updated)
			}
			var counts []int
			for i, turn := range []workspace.DeskTurnResponse{first, second} {
				receipt := turn.Turn.Receipts[0]
				var deps int
				if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM context_artifact_dependencies WHERE owner_id=$1 AND parent_kind='artifact' AND parent_id=$2 AND parent_version=1", string(scope.OwnerID), *receipt.ActionID).Scan(&deps); err != nil {
					t.Fatal(err)
				}
				counts = append(counts, deps)
				if name == "RECEIPT-EMPTY" && deps != 0 {
					t.Fatal("source-free audit control has actual typed deps", deps)
				}
				if name == "RECEIPT-SOURCE" {
					var exact int
					if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM context_artifact_dependencies WHERE owner_id=$1 AND parent_kind='artifact' AND parent_id=$2 AND dependency_kind='source' AND dependency_id=$3 AND dependency_version=$4", string(scope.OwnerID), *receipt.ActionID, string(source.ID), source.Version).Scan(&exact); err != nil || exact < 1 {
						t.Fatal("actual source origin typed dependency positive control missing", err, exact)
					}
					phase2RTSnapshotForSource(t, s, scope, source, "secretary", []string{firstRequest.RequestID, secondRequest.RequestID}[i], capture.request(t, i))
				}
			}
			if name == "RECEIPT-EMPTY" {
				var all int
				if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM context_artifact_dependencies WHERE owner_id=$1", string(scope.OwnerID)).Scan(&all); err != nil || all != 0 {
					t.Fatal("whole source-free DAG is not actually empty", err, all)
				}
			}
			var originRows []byte
			if err := s.pool.QueryRow(ctx, `SELECT coalesce(jsonb_agg(jsonb_build_object('id',l.id,'turn_id',l.turn_id,'context_task',l.context_task,'context_stale',l.context_stale,'typed_dependencies',coalesce((SELECT jsonb_agg(to_jsonb(d)) FROM context_artifact_dependencies d WHERE d.owner_id=l.owner_id AND d.parent_kind='artifact' AND d.parent_id=l.id::text),'[]'::jsonb))),'[]'::jsonb) FROM action_log l WHERE l.owner_id=$1 AND l.id=ANY($2::uuid[])`, string(scope.OwnerID), []string{*initial.ActionID, *updated.ActionID}).Scan(&originRows); err != nil {
				t.Fatal(err)
			}
			phase2RTEvidence(t, "receipt-real-origin-before-undo", map[string]any{"first": first, "second": second, "typed_dependency_counts": counts, "actual_action_rows": json.RawMessage(originRows)})
			if _, err := s.Undo(ctx, scope, *updated.ActionID); err != nil {
				t.Fatal("strict reverse Undo update failed", err)
			}
			state, err := s.Undo(ctx, scope, *initial.ActionID)
			if err != nil {
				t.Fatal("strict reverse Undo create failed", err)
			}
			for _, item := range state.Tasks {
				if item.ID == thingID {
					t.Fatal("second real Undo failed to remove task", item)
				}
			}
			response := phase2RTHTTP(t, s, scope, http.MethodGet, "/v1/desk/turns?conversationId="+first.ConversationID, nil)
			if response.Code != http.StatusOK {
				t.Fatal("real HTTP history failed", response.Code, response.Body.String())
			}
			var history workspace.DeskTurnsResponse
			if err := json.Unmarshal(response.Body.Bytes(), &history); err != nil {
				t.Fatal(err)
			}
			if len(history.Turns) != 2 || capture.count() != 2 {
				t.Fatal("history lost/re-executed one of two turns", history, capture.count())
			}
			expected := map[string]string{first.Turn.ID: *initial.ActionID, second.Turn.ID: *updated.ActionID}
			sameTitle := 0
			for _, turn := range history.Turns {
				id, ok := expected[turn.ID]
				if !ok || len(turn.Receipts) != 1 {
					t.Fatal("history changed actual turn identities or receipt counts", turn)
				}
				receipt := turn.Receipts[0]
				if receipt.ActionID == nil || *receipt.ActionID != id || !receipt.Undone {
					t.Fatal("history lost stable actual undone identity", receipt)
				}
				if name == "RECEIPT-EMPTY" {
					if receipt.ThingID == nil || *receipt.ThingID != thingID || !strings.Contains(receipt.Text, title) {
						t.Error("empty-typed owner audit lost original task/title identity", receipt)
					} else {
						sameTitle++
					}
				}
			}
			if name == "RECEIPT-EMPTY" && sameTitle != 2 {
				t.Error("both independent turns must retain the same original title after task deletion", sameTitle)
			}
			if name == "RECEIPT-SOURCE" {
				for _, turn := range history.Turns {
					receipt := turn.Receipts[0]
					if turn.ID == first.Turn.ID {
						if !strings.Contains(receipt.Text, title) || turn.Reply != first.Turn.Reply {
							t.Error("still-lawful first source history was erased solely by unrelated action Undo", turn)
						}
					} else if turn.ID == second.Turn.ID {
						if receipt.Op != "action" || receipt.ThingID != nil || receipt.Text != "这项操作已完成；相关回答内容已隐藏" || strings.Contains(string(asJSON(turn)), phase2RTGoldRead(t).Records[0].Atoms[0]) || turn.Reply == second.Turn.Reply || len(turn.Cards) != 0 {
							t.Error("second history illegally revived its undone action ancestor", turn)
						}
					}
				}
				// Action Undo leaves the raw-source policy live. Explicitly revoke it to
				// test that the local audit exception cannot recover any typed source.
				phase2RTUpdatePolicy(t, s, scope, source, sourcePolicy, true)
				revokedResponse := phase2RTHTTP(t, s, scope, http.MethodGet, "/v1/desk/turns?conversationId="+first.ConversationID, nil)
				if revokedResponse.Code != http.StatusOK || strings.Contains(revokedResponse.Body.String(), title) || strings.Contains(revokedResponse.Body.String(), phase2RTGoldRead(t).Records[0].Atoms[0]) {
					t.Fatal("formal source revoke left generated source title/atom in actual history", revokedResponse.Code, revokedResponse.Body.String())
				}
				var revokedHistory workspace.DeskTurnsResponse
				if err := json.Unmarshal(revokedResponse.Body.Bytes(), &revokedHistory); err != nil || len(revokedHistory.Turns) != 2 {
					t.Fatal("source revoke lost actual audit turn identities", err, revokedHistory)
				}
				for _, turn := range revokedHistory.Turns {
					id, ok := expected[turn.ID]
					if !ok || len(turn.Receipts) != 1 || turn.Receipts[0].ActionID == nil || *turn.Receipts[0].ActionID != id || !turn.Receipts[0].Undone || turn.Receipts[0].ThingID != nil || turn.Receipts[0].Op != "action" || turn.Receipts[0].Text != "这项操作已完成；相关回答内容已隐藏" || len(turn.Cards) != 0 || turn.Reply == first.Turn.Reply {
						t.Error("typed-source audit exception revived content or erased identity", turn)
					}
				}
				phase2RTEvidence(t, "receipt-formal-source-revoke-history", map[string]any{"source": source, "original_policy": sourcePolicy, "actual_http": json.RawMessage(revokedResponse.Body.Bytes()), "before_revoke_history": json.RawMessage(response.Body.Bytes())})
			}
			for _, receipt := range []workspace.DeskReceipt{initial, updated} {
				var undone bool
				if err := s.pool.QueryRow(ctx, "SELECT undone_at IS NOT NULL FROM action_log WHERE owner_id=$1 AND id=$2", string(scope.OwnerID), *receipt.ActionID).Scan(&undone); err != nil || !undone {
					t.Fatal("real audit identity was removed or not undone", err, undone)
				}
			}
			phase2RTEvidence(t, "receipt-two-real-turns-undone", map[string]any{"first": first, "second": second, "typed_dependency_counts": counts, "source": source, "actual_http_history": json.RawMessage(response.Body.Bytes()), "original_title": title, "removed_thing": thingID})
		})
	}
}
