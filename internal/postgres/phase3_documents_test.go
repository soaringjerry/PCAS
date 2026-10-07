package postgres

import (
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"testing"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func phase3CurrentDoc(t *testing.T, h *phase3HTTP, f *phase3LoadedFixture, id string) (phase3VersionList, phase3Version) {
	t.Helper()
	var list phase3VersionList
	h.get(t, f.Context, phase3DocumentPath(id, "versions"), &list)
	var current phase3Version
	h.get(t, f.Context, phase3DocumentPath(id, fmt.Sprintf("versions/%d", list.CurrentVersion)), &current)
	return list, current
}
func phase3Revision(t *testing.T, h *phase3HTTP, f *phase3LoadedFixture, docID string, base int, body string) (string, phase3Version) {
	t.Helper()
	p := f.Gold.Projects[0]
	state := h.command(t, f.Context, map[string]any{"type": "requestRun", "thingId": p.ID, "kind": "revise", "agentId": "manual", "prompt": "只改虚构预算，保留全文其他段落。", "documentId": docID, "baseVersion": base})
	runID := ""
	for _, r := range state.Runs {
		if r.Kind == "revise" && r.Status == "waiting" {
			runID = r.ID
			break
		}
	}
	if runID == "" {
		t.Fatal("revise not queued with its original base")
	}
	h.command(t, f.Context, map[string]any{"type": "pasteRunResult", "id": runID, "output": body})
	list, current := phase3CurrentDoc(t, h, f, docID)
	if current.Body != body || current.BasedOn == nil || *current.BasedOn != base || current.RunID == nil || *current.RunID != runID {
		t.Fatalf("revision metadata/current output wrong: %+v list=%+v", current, list)
	}
	var action string
	if err := f.Store.pool.QueryRow(f.Context, `SELECT id::text FROM action_log WHERE owner_id=$1 AND undone_at IS NULL ORDER BY action_order DESC LIMIT 1`, f.Scope.OwnerID).Scan(&action); err != nil {
		t.Fatal(err)
	}
	return action, current
}
func phase3Undo(t *testing.T, h *phase3HTTP, f *phase3LoadedFixture, action string) {
	t.Helper()
	h.command(t, f.Context, map[string]any{"type": "undoAction", "id": action})
}
func TestPhase3D4RevisionUsesSpecifiedOldBaseAndProvenance(t *testing.T) {
	phase3Finding(t, "S-P3-002")
	f := phase3LoadFixture(t)
	h := phase3NewHTTP(t, f)
	d := f.Gold.Projects[0].Documents[0]
	_, v := phase3Revision(t, h, f, d.ID, 1, strings.ReplaceAll(d.Versions[0].Body, "100虚构单位", "80虚构单位"))
	if v.Version != 3 {
		t.Fatalf("not v3: %d", v.Version)
	}
	var original phase3Version
	h.get(t, f.Context, phase3DocumentPath(d.ID, "versions/2"), &original)
	if original.Body != d.Versions[1].Body {
		t.Fatal("rewrote existing v2")
	}
}
func TestPhase3D4BadRevisionPreservesGoodVersionAndReason(t *testing.T) {
	for _, kind := range []string{"empty", "truncated", "unchanged"} {
		t.Run(kind, func(t *testing.T) {
			phase3Finding(t, "S-P3-002")
			f := phase3LoadFixture(t)
			h := phase3NewHTTP(t, f)
			d := f.Gold.Projects[0].Documents[0]
			before, current := phase3CurrentDoc(t, h, f, d.ID)
			body := map[string]string{"empty": "", "truncated": "虚构目标：青湾", "unchanged": current.Body}[kind]
			state := h.command(t, f.Context, map[string]any{"type": "requestRun", "thingId": f.Gold.Projects[0].ID, "kind": "revise", "agentId": "manual", "prompt": "把预算改保守一点。", "documentId": d.ID, "baseVersion": 2})
			id := ""
			for _, r := range state.Runs {
				if r.Kind == "revise" && r.Status == "waiting" {
					id = r.ID
					break
				}
			}
			if id == "" {
				t.Fatal("no revision job")
			}
			code, _, err := h.call(f.Context, "POST", "/v1/workspace/commands", map[string]any{"type": "pasteRunResult", "id": id, "output": body, "requestId": string(memory.NewID())})
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("bad revise kind=%s completion_http=%d", kind, code)
			after, got := phase3CurrentDoc(t, h, f, d.ID)
			if len(after.Items) != len(before.Items) || got.Body != current.Body || after.CurrentVersion != before.CurrentVersion {
				t.Fatal("bad revision replaced good version")
			}
			var raw []byte
			if err := f.Store.pool.QueryRow(f.Context, `SELECT document FROM agent_runs WHERE owner_id=$1 AND id=$2`, f.Scope.OwnerID, id).Scan(&raw); err != nil {
				t.Fatal(err)
			}
			var r workspace.Run
			decodePhase3(t, raw, &r)
			if r.Error == "" {
				t.Fatal("invalid revision has no stored failure reason")
			}
		})
	}
}
func TestPhase3D5ContinuousSkipAndInsertedUndoSequences(t *testing.T) {
	for _, sequence := range []string{"continuous", "skip", "user_inserted", "replay_and_restore", "concurrent_same_request"} {
		t.Run(sequence, func(t *testing.T) {
			phase3Finding(t, "S-P3-002")
			f := phase3LoadFixture(t)
			h := phase3NewHTTP(t, f)
			d := f.Gold.Projects[0].Documents[0]
			start := d.Versions[1].Body
			a, av := phase3Revision(t, h, f, d.ID, 2, strings.ReplaceAll(start, "200虚构单位", "180虚构单位"))
			b, bv := phase3Revision(t, h, f, d.ID, av.Version, strings.ReplaceAll(av.Body, "180虚构单位", "170虚构单位"))
			before, _ := phase3CurrentDoc(t, h, f, d.ID)
			switch sequence {
			case "continuous":
				phase3Undo(t, h, f, b)
				_, cur := phase3CurrentDoc(t, h, f, d.ID)
				if cur.Body != av.Body {
					t.Fatal("undo B did not restore A")
				}
				phase3Undo(t, h, f, a)
				_, cur = phase3CurrentDoc(t, h, f, d.ID)
				if cur.Body != start {
					t.Fatal("undo A did not restore start")
				}
			case "skip", "user_inserted":
				want := bv.Body
				if sequence == "user_inserted" {
					want = bv.Body + "\n\n用户插队保存的虚构说明。"
					h.command(t, f.Context, map[string]any{"type": "updateDoc", "id": d.ID, "patch": map[string]string{"body": want}})
				}
				code, _, err := h.call(f.Context, "POST", "/v1/workspace/commands", map[string]any{"type": "undoAction", "id": a, "requestId": string(memory.NewID())})
				if err != nil || code >= 500 {
					t.Fatal("targeted undo failed internally", err, code)
				}
				_, cur := phase3CurrentDoc(t, h, f, d.ID)
				if cur.Body != want {
					t.Fatal("undo older adoption overwrote inserted/newer result")
				}
			case "replay_and_restore", "concurrent_same_request":
				request := map[string]any{"type": "undoAction", "id": b, "requestId": string(memory.NewID())}
				if sequence == "replay_and_restore" {
					h.command(t, f.Context, request)
					h.command(t, f.Context, request)
				} else {
					var wg sync.WaitGroup
					errs := make(chan error, 2)
					for i := 0; i < 2; i++ {
						wg.Add(1)
						go func() {
							defer wg.Done()
							code, raw, err := h.call(f.Context, "POST", "/v1/workspace/commands", request)
							if err != nil || code != 200 {
								errs <- fmt.Errorf("replay status=%d body=%s err=%v", code, raw, err)
							}
						}()
					}
					wg.Wait()
					close(errs)
					for err := range errs {
						t.Error(err)
					}
				}
				_, cur := phase3CurrentDoc(t, h, f, d.ID)
				if cur.Body != av.Body {
					t.Fatal("same request toggled more than once")
				}
				phase3Undo(t, h, f, b)
				_, cur = phase3CurrentDoc(t, h, f, d.ID)
				if cur.Body != bv.Body {
					t.Fatal("second explicit undo did not restore B")
				}
			}
			after, _ := phase3CurrentDoc(t, h, f, d.ID)
			if len(after.Items) < len(before.Items) {
				t.Fatal("undo physically deleted historical version")
			}
		})
	}
}
func TestPhase3D5RandomReversibleSequence(t *testing.T) {
	phase3Finding(t, "S-P3-002")
	f := phase3LoadFixture(t)
	h := phase3NewHTTP(t, f)
	d := f.Gold.Projects[0].Documents[0]
	initial, current := phase3CurrentDoc(t, h, f, d.ID)
	const seed int64 = 330507
	rng := rand.New(rand.NewSource(seed))
	actions := []string{}
	t.Logf("random undo seed=%d", seed)
	for i := 0; i < 24; i++ {
		if rng.Intn(2) == 0 {
			id := string(memory.NewID())
			h.command(t, f.Context, map[string]any{"type": "updateDoc", "id": d.ID, "patch": map[string]string{"body": current.Body + fmt.Sprintf("\n\n虚构用户第%d次追加。", i)}, "requestId": id})
			actions = append(actions, id)
		} else {
			a, _ := phase3Revision(t, h, f, d.ID, current.Version, current.Body+fmt.Sprintf("\n\n虚构副手第%d次完整修订。", i))
			actions = append(actions, a)
		}
		_, current = phase3CurrentDoc(t, h, f, d.ID)
	}
	for i := len(actions) - 1; i >= 0; i-- {
		phase3Undo(t, h, f, actions[i])
	}
	after, body := phase3CurrentDoc(t, h, f, d.ID)
	var original phase3Version
	h.get(t, f.Context, phase3DocumentPath(d.ID, fmt.Sprintf("versions/%d", initial.CurrentVersion)), &original)
	if body.Body != original.Body || after.CurrentVersion != initial.CurrentVersion {
		t.Fatalf("random inverse does not restore start: %+v %+v", after, body)
	}
	if len(after.Items) < len(initial.Items)+24 {
		t.Fatal("random reversal discarded history")
	}
}
func TestPhase3D7DeleteAllVersionsAndRestoreAndLateOperation(t *testing.T) {
	phase3Finding(t, "S-P3-002")
	f := phase3LoadFixture(t)
	h := phase3NewHTTP(t, f)
	d := f.Gold.Projects[0].Documents[0]
	before, current := phase3CurrentDoc(t, h, f, d.ID)
	action := string(memory.NewID())
	h.command(t, f.Context, map[string]any{"type": "deleteDoc", "id": d.ID, "requestId": action})
	for _, part := range []string{"versions", "versions/1", "diff"} {
		code, _, err := h.call(f.Context, "GET", phase3DocumentPath(d.ID, part), nil)
		if err != nil || code != 404 {
			t.Errorf("deleted document %s accessible status=%d err=%v", part, code, err)
		}
	}
	code, _, err := h.call(f.Context, "POST", "/v1/workspace/commands", map[string]any{"type": "updateDoc", "id": d.ID, "patch": map[string]string{"body": "虚构迟到写入"}, "requestId": string(memory.NewID())})
	if err != nil || code != 404 {
		t.Error("deleted doc resurrected", code, err)
	}
	// The connector undo entry point, shared with Telegram, uses the same action ID.
	if _, err := f.Store.Undo(f.Context, f.Scope, action); err != nil {
		t.Fatal(err)
	}
	after, got := phase3CurrentDoc(t, h, f, d.ID)
	if len(after.Items) != len(before.Items) || got.Body != current.Body {
		t.Fatal("document restoration lost versions/content")
	}
}

func TestPhase3D6SecretaryDelegatesExactVersionOrOnlyAsksAmbiguousField(t *testing.T) {
	for _, ambiguous := range []bool{false, true} {
		t.Run(fmt.Sprint(ambiguous), func(t *testing.T) {
			phase3Finding(t, "S-P3-002")
			f := phase3LoadFixture(t)
			h := phase3NewHTTP(t, f)
			p := f.Gold.Projects[0]
			d := p.Documents[0]
			m := phase3NewModel(t, f)
			if ambiguous {
				h.command(t, f.Context, map[string]any{"type": "createDoc", "doc": workspace.Doc{ID: string(memory.NewID()), ThingID: p.ID, Title: d.Title, Body: "另一份同名的虚构方案。", By: "user"}})
			}
			m.mu.Lock()
			m.Override = func(stage, out string) string {
				if stage != "secretary" {
					return out
				}
				if ambiguous {
					return `{"reply":"需要明确哪一份方案","actions":[],"used":[],"links":[],"show":[],"remember":false,"missingKeyInfo":true,"ask":{"question":"哪一份虚构方案？","options":[]},"memoryPlan":{"depth":"light","groups":[]}}`
				}
				return string(asJSON(map[string]any{"reply": "已派副手从第二版改虚构预算。", "actions": []any{map[string]any{"op": "delegate", "ref": "THIS", "kind": "revise", "documentId": d.ID, "baseVersion": 2, "prompt": "只改虚构第二版预算。"}}, "used": []any{}, "links": []any{}, "show": []any{}, "remember": false, "missingKeyInfo": false, "ask": nil, "memoryPlan": map[string]any{"depth": "light", "groups": []any{}}}))
			}
			m.mu.Unlock()
			code, raw, err := h.call(f.Context, "POST", "/v1/desk/turn", workspace.DeskTurnRequest{RequestID: string(memory.NewID()), ThingID: &p.ID, AgentID: "phase3", Text: "把第二版方案里的预算部分改保守一点"})
			if err != nil || code != 200 {
				t.Fatal(code, string(raw), err)
			}
			var result workspace.DeskTurnResponse
			decodePhase3(t, raw, &result)
			if ambiguous {
				if result.Turn.Ask == nil || !strings.Contains(result.Turn.Ask.Question, "哪一份") || strings.Contains(result.Turn.Ask.Question, "版本") {
					t.Fatal("asked a supplied version instead of only ambiguous document", result.Turn.Ask)
				}
				for _, r := range result.State.Runs {
					if r.Kind == "revise" {
						t.Fatal("ambiguity silently started revision")
					}
				}
			} else {
				var wire struct {
					State struct {
						Runs []struct {
							Kind, DocumentID string
							BaseVersion      int
						}
					}
				}
				decodePhase3(t, raw, &wire)
				found := false
				for _, r := range wire.State.Runs {
					if r.Kind == "revise" {
						found = true
						if r.DocumentID != d.ID || r.BaseVersion != 2 {
							t.Fatal("delegate silently chose another document/base", r)
						}
					}
				}
				if !found {
					t.Fatal("unique second version did not delegate")
				}
			}
		})
	}
}
