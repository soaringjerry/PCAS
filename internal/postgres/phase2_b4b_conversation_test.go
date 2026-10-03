package postgres

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func TestPhase2B4b_C1_WholeConversationHasOrderedSpeakersTimesAndOneCall(t *testing.T) {
	s, scope := testStore(t), owner()
	spec := b4bSpec[struct {
		Messages  int      `json:"messages"`
		Calls     int      `json:"calls"`
		Organized int      `json:"organized"`
		Roles     []string `json:"roles"`
	}](t, "C1")
	roles, texts := []string{}, []string{}
	for n := 0; n < spec.Messages; n++ {
		roles = append(roles, spec.Roles[n%len(spec.Roles)])
		texts = append(texts, fmt.Sprintf("第%02d句合成成都讨论", n+1))
	}
	f := b4bModel(t, s, func(b4bInput) any { return map[string]any{"items": []any{}} })
	a := b4bImport(t, s, scope, b4bMessages(t, roles, texts))
	b4bOperation(t, s, scope, a.Batch, "organize")
	b4bDrain(t, s)
	b2Equal(t, len(f.all()), spec.Calls)
	in := b4bInputFrom(t, f.last(t))
	b2Equal(t, in.Messages, a.Messages)
	b2Equal(t, len(in.Context), 0)
	b2Equal(t, len(in.Earlier), 0)
	for _, src := range a.Sources {
		b4bRecord(t, s, scope, src, "empty", 0)
	}
	p := b4bProgress(t, s, scope, a.Batch)
	b2Equal(t, p.Total, spec.Messages)
	b2Equal(t, p.Stored, p.Total)
	b2Equal(t, p.Organized, spec.Organized)
	b2Equal(t, b2Count(t, s, `SELECT count(*) FROM model_usage WHERE owner_id=$1 AND purpose='extraction'`, string(scope.OwnerID)), spec.Calls)
}

func TestPhase2B4b_C2_FinalChoiceAndAgreementKeepUserEvidence(t *testing.T) {
	spec := b4bSpec[struct {
		FinalIndex      int      `json:"final_index"`
		Abandoned       string   `json:"abandoned"`
		Quote           string   `json:"final_quote"`
		Memory          string   `json:"final_memory"`
		Places          []string `json:"places"`
		Proposal        string   `json:"long_proposal"`
		Agreement       string   `json:"agreement"`
		AgreementMemory string   `json:"agreement_memory"`
		AgreementIndex  int      `json:"agreement_index"`
		People          []string `json:"people"`
	}](t, "C2")
	t.Run("later_choice", func(t *testing.T) {
		s, scope := testStore(t), owner()
		roles, texts := []string{}, []string{}
		for n := 0; n < spec.FinalIndex; n++ {
			roles = append(roles, "user")
			texts = append(texts, fmt.Sprintf("中间无关的旅行讨论%02d", n+1))
		}
		texts[0] = spec.Abandoned
		texts[len(texts)-1] = spec.Quote
		i := b4bItem(spec.FinalIndex, spec.Memory, spec.Quote, "plan")
		i["places"] = spec.Places
		f := b4bModel(t, s, func(b4bInput) any { return map[string]any{"items": []any{i}} })
		a := b4bImport(t, s, scope, b4bMessages(t, roles, texts))
		b4bOperation(t, s, scope, a.Batch, "organize")
		b4bDrain(t, s)
		b2Equal(t, b4bInputFrom(t, f.last(t)).Messages, a.Messages)
		m := b2One(t, b2Snapshot(t, s, scope).Memories)
		b2Equal(t, m.Text, spec.Memory)
		b2Names(t, m, "place", spec.Places)
		b4bEvidence(t, s, scope, m, a.Sources[spec.FinalIndex-1], spec.Quote, a.Messages[spec.FinalIndex-1].At)
	})
	t.Run("agreement_with_long_AI_proposal", func(t *testing.T) {
		s, scope := testStore(t), owner()
		limit := b4bSpec[b4bLimits](t, "limits")
		texts := []string{"给我一个成都安排。", spec.Proposal + strings.Repeat("方案填充", 500), spec.Agreement}
		i := b4bItem(spec.AgreementIndex, spec.AgreementMemory, spec.Agreement, "plan")
		i["people"] = spec.People
		i["places"] = spec.Places
		f := b4bModel(t, s, func(b4bInput) any { return map[string]any{"items": []any{i}} })
		a := b4bImport(t, s, scope, b4bMessages(t, []string{"user", "assistant", "user"}, texts))
		b4bOperation(t, s, scope, a.Batch, "organize")
		b4bDrain(t, s)
		in := b4bInputFrom(t, f.last(t))
		b2Equal(t, in.Messages[1].Text, string([]rune(texts[1])[:limit.AI]))
		m := b2One(t, b2Snapshot(t, s, scope).Memories)
		b2Equal(t, m.Text, spec.AgreementMemory)
		b2Names(t, m, "person", spec.People)
		b2Names(t, m, "place", spec.Places)
		b4bEvidence(t, s, scope, m, a.Sources[2], spec.Agreement, a.Messages[2].At)
	})
}

func TestPhase2B4b_C3_AIMessageNeverBecomesEvidence(t *testing.T) {
	spec := b4bSpec[struct {
		AIQuote    string `json:"ai_quote"`
		AIMemory   string `json:"ai_memory"`
		UserQuote  string `json:"user_quote"`
		UserMemory string `json:"user_memory"`
		Invalid    int    `json:"only_invalid_memories"`
		Mixed      int    `json:"mixed_memories"`
	}](t, "C3")
	for _, mixed := range []bool{false, true} {
		t.Run(fmt.Sprintf("valid_user_item_%t", mixed), func(t *testing.T) {
			s, scope := testStore(t), owner()
			items := []any{b4bItem(2, spec.AIMemory, spec.AIQuote, "plan")}
			want := spec.Invalid
			if mixed {
				items = append(items, b4bItem(1, spec.UserMemory, spec.UserQuote, "decision"))
				want = spec.Mixed
			}
			f := b4bModel(t, s, func(b4bInput) any { return map[string]any{"items": items} })
			a := b4bImport(t, s, scope, b4bMessages(t, []string{"user", "assistant"}, []string{spec.UserQuote, spec.AIQuote}))
			b4bOperation(t, s, scope, a.Batch, "organize")
			b4bDrain(t, s)
			b2Equal(t, b4bInputFrom(t, f.last(t)).Messages, a.Messages)
			memories := b2Snapshot(t, s, scope).Memories
			b2Equal(t, len(memories), want)
			b4bRecord(t, s, scope, a.Sources[1], "empty", 0)
			if mixed {
				m := b2One(t, memories)
				b2Equal(t, m.Text, spec.UserMemory)
				b4bEvidence(t, s, scope, m, a.Sources[0], spec.UserQuote, a.Messages[0].At)
			}
		})
	}
}

func TestPhase2B4b_C4_QuoteMustBelongToItsNumberedMessage(t *testing.T) {
	spec := b4bSpec[struct {
		First        string `json:"first"`
		Second       string `json:"second"`
		Invalid      string `json:"invalid"`
		Accepted     string `json:"accepted"`
		InvalidCount int    `json:"invalid_memories"`
		Mixed        int    `json:"mixed_memories"`
	}](t, "C4")
	for _, mixed := range []bool{false, true} {
		t.Run(fmt.Sprintf("valid_item_%t", mixed), func(t *testing.T) {
			s, scope := testStore(t), owner()
			items := []any{b4bItem(1, spec.Invalid, spec.Second, "preference"), b4bItem(99, "非法编号", spec.First, "preference")}
			want := spec.InvalidCount
			if mixed {
				items = append(items, b4bItem(2, spec.Accepted, spec.Second, "preference"))
				want = spec.Mixed
			}
			f := b4bModel(t, s, func(b4bInput) any { return map[string]any{"items": items} })
			a := b4bImport(t, s, scope, b4bMessages(t, []string{"user", "user"}, []string{spec.First, spec.Second}))
			b4bOperation(t, s, scope, a.Batch, "organize")
			b4bDrain(t, s)
			b2Equal(t, b4bInputFrom(t, f.last(t)).Messages, a.Messages)
			memories := b2Snapshot(t, s, scope).Memories
			b2Equal(t, len(memories), want)
			if mixed {
				m := b2One(t, memories)
				b2Equal(t, m.Text, spec.Accepted)
				b4bEvidence(t, s, scope, m, a.Sources[1], spec.Second, a.Messages[1].At)
			}
		})
	}
}

func b4bLongFixture(t *testing.T, s *Store, scope memory.Scope, prefixes []string) b4bArchive {
	t.Helper()
	spec := b4bSpec[struct {
		Characters []int `json:"message_characters"`
	}](t, "C5")
	texts, roles := []string{}, []string{}
	for n, size := range spec.Characters {
		prefix := fmt.Sprintf("第%d条长用户消息。", n+1)
		if n < len(prefixes) && prefixes[n] != "" {
			prefix = prefixes[n]
		}
		texts = append(texts, b4bSized(prefix, size))
		roles = append(roles, "user")
	}
	return b4bImport(t, s, scope, b4bMessages(t, roles, texts))
}
func b4bBoundaries(t *testing.T, requests []b1Request, a b4bArchive) {
	t.Helper()
	spec := b4bSpec[struct {
		Segments [][]int `json:"segments"`
		Contexts [][]int `json:"contexts"`
		Calls    int     `json:"calls"`
	}](t, "C5")
	limits := b4bSpec[b4bLimits](t, "limits")
	b2Equal(t, len(requests), spec.Calls)
	if len(requests) != len(spec.Segments) {
		t.Fatal("wrong segment count")
	}
	for n, req := range requests {
		in := b4bInputFrom(t, req)
		want := []b4bMessage{}
		overlap := []b4bMessage{}
		for _, index := range spec.Segments[n] {
			want = append(want, a.Messages[index-1])
		}
		for _, index := range spec.Contexts[n] {
			m := a.Messages[index-1]
			m.Text = string([]rune(m.Text)[:limits.OverlapCharacters])
			overlap = append(overlap, m)
		}
		b2Equal(t, in.Messages, want)
		b2Equal(t, append([]b4bMessage{}, in.Context...), overlap)
		bodySize := 0
		for _, m := range in.Messages {
			bodySize += utf8.RuneCountInString(m.Text)
		}
		if bodySize > limits.Body {
			t.Error("body character budget exceeded", bodySize)
		}
	}
}
func TestPhase2B4b_C5_MessageBoundarySegmentsGlobalNumbersAndOverlap(t *testing.T) {
	s, scope := testStore(t), owner()
	spec := b4bSpec[struct {
		First    string `json:"first_memory"`
		Last     string `json:"last_memory"`
		Memories int    `json:"memories"`
	}](t, "C5")
	first, last := "第一段去成都。", "最后一段去杭州。"
	f := b4bModel(t, s, func(in b4bInput) any {
		if in.Messages[0].Index == 1 {
			return map[string]any{"items": []any{b4bItem(1, spec.First, first, "plan")}}
		}
		return map[string]any{"items": []any{b4bItem(1, spec.First, first, "plan"), b4bItem(4, spec.Last, last, "plan")}}
	})
	a := b4bLongFixture(t, s, scope, []string{first, "", "", last})
	b4bOperation(t, s, scope, a.Batch, "organize")
	b4bDrain(t, s)
	b4bBoundaries(t, f.all(), a)
	memories := b2Snapshot(t, s, scope).Memories
	b2Equal(t, len(memories), spec.Memories)
	for _, m := range memories {
		switch m.Text {
		case spec.First:
			b4bEvidence(t, s, scope, m, a.Sources[0], first, a.Messages[0].At)
		case spec.Last:
			b4bEvidence(t, s, scope, m, a.Sources[3], last, a.Messages[3].At)
		default:
			t.Error("unexpected segment memory", m.Text)
		}
	}
}

func b4bOldClaim(t *testing.T, s *Store, scope memory.Scope, text string, source memory.Ref) memory.Ref {
	t.Helper()
	b2Snapshot(t, s, scope)
	ref := b1Claim(t, s, scope, text, "plan", "candidate", source)
	// Represent a pre-E4 system extraction through the existing commit fixture.
	b2Exec(t, s, `UPDATE record_versions SET actor='ai' WHERE owner_id=$1 AND record_id=$2`, string(scope.OwnerID), string(ref.ID))
	return ref
}
func TestPhase2B4b_C6_ReplacesOnlyUntouchedSystemMemoriesAndMarksHistory(t *testing.T) {
	s, scope := testStore(t), owner()
	spec := b4bSpec[struct {
		Memories int    `json:"memories"`
		New      string `json:"new_memory"`
		Outdated bool   `json:"outdated"`
	}](t, "C6")
	a := b4bImport(t, s, scope, b4bMessages(t, []string{"user", "user", "user"}, []string{"成都旧计划青灯。", "成都确认过的旧计划。", "成都修改过的旧计划。"}))
	old := b4bOldClaim(t, s, scope, "成都旧计划青灯", a.Sources[0])
	b1Model(t, s, `{"reply":"旧回答青灯完整保留","used":["M1"],"actions":[]}`)
	req := turnRequest("成都旧计划青灯是什么？")
	out := mustTurn(t, s, scope, req)
	b1HasRef(t, b1Refs(t, s, scope, req.RequestID), old, true)
	confirmed := b4bOldClaim(t, s, scope, "成都确认过的旧计划", a.Sources[1])
	workspaceCommand(t, s, scope, workspace.Command{Type: "confirmMemory", ID: string(confirmed.ID)})
	edited := b4bOldClaim(t, s, scope, "成都修改过的旧计划", a.Sources[2])
	b1Correct(t, s, scope, edited, "用户亲自改过的成都计划")
	b4bModel(t, s, func(b4bInput) any {
		return map[string]any{"items": []any{b4bItem(1, spec.New, a.Messages[0].Text, "plan")}}
	})
	b4bOnlyArchiveJobs(t, s, scope, a.Root)
	b4bOperation(t, s, scope, a.Batch, "organize")
	b4bDrain(t, s)
	b1Active(t, s, scope, old, false)
	b1Active(t, s, scope, confirmed, true)
	b1Active(t, s, scope, edited, true)
	visible := b1History(t, s, scope, out.ConversationID)
	b1Outdated(t, visible, spec.Outdated)
	b2Equal(t, visible.Text, out.Turn.Text)
	b2Equal(t, visible.Reply, out.Turn.Reply)
	memories := b2Snapshot(t, s, scope).Memories
	b2Equal(t, len(memories), spec.Memories)
	newCount := 0
	for _, m := range memories {
		if m.Text == spec.New {
			newCount++
			b4bEvidence(t, s, scope, m, a.Sources[0], a.Messages[0].Text, a.Messages[0].At)
		}
	}
	b2Equal(t, newCount, 1)
	// A completed replay must not duplicate memories or replace protected records.
	if err := s.ProcessExtraction(context.Background(), leaseStage(t, s, scope, a.Sources[0], "source.extract")); err != nil {
		t.Fatal(err)
	}
	b4bDrain(t, s)
	b2Equal(t, len(b2Snapshot(t, s, scope).Memories), spec.Memories)
	b1Active(t, s, scope, confirmed, true)
	b1Active(t, s, scope, edited, true)
}

func TestPhase2B4b_C8_HoldPauseIncompleteBatchAndFreshInputPriority(t *testing.T) {
	s, scope := testStore(t), owner()
	f := b4bModel(t, s, func(b4bInput) any { return map[string]any{"items": []any{}} })
	a := b4bImport(t, s, scope, b4bMessages(t, []string{"user", "assistant"}, []string{"合成历史成都问题。", "合成历史成都回答。"}))
	claimNone := func() {
		t.Helper()
		job, err := s.Claim(context.Background(), time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if job != nil {
			t.Fatal("blocked conversation job claimed", job.Stage)
		}
	}
	claimNone()
	b4bOperation(t, s, scope, a.Batch, "organize")
	// An incomplete legacy batch is a stored-data fixture; actual pause and
	// resume still use the HTTP endpoints, with no manufactured queue job.
	b2Exec(t, s, `UPDATE import_batches SET stored=total-1,state='importing' WHERE owner_id=$1 AND id=$2`, string(scope.OwnerID), string(a.Batch))
	claimNone()
	b4bOperation(t, s, scope, a.Batch, "pause")
	b4bOperation(t, s, scope, a.Batch, "resume")
	claimNone()
	b2Exec(t, s, `UPDATE import_batches SET stored=total WHERE owner_id=$1 AND id=$2`, string(scope.OwnerID), string(a.Batch))
	b4bOperation(t, s, scope, a.Batch, "pause")
	claimNone()
	b4bOperation(t, s, scope, a.Batch, "resume")
	fresh := b2Source(t, s, scope, "capture", "user", "新说的昆明计划。", nil)
	if err := s.ProcessChunks(context.Background(), leaseStage(t, s, scope, fresh, "source.chunk")); err != nil {
		t.Fatal(err)
	}
	b2Exec(t, s, `DELETE FROM memory_jobs WHERE owner_id=$1 AND stage NOT LIKE 'source.extract%'`, string(scope.OwnerID))
	job, err := s.Claim(context.Background(), time.Minute)
	if err != nil || job == nil {
		t.Fatal("fresh claim", err)
	}
	b2Equal(t, job.Record.ID, fresh.ID)
	if err = s.Block(context.Background(), *job, "synthetic_finished"); err != nil {
		t.Fatal(err)
	}
	job, err = s.Claim(context.Background(), time.Minute)
	if err != nil || job == nil {
		t.Fatal("resumed archive claim", err)
	}
	b2Equal(t, b2Count(t, s, `SELECT priority FROM memory_jobs WHERE id=$1`, string(job.ID)), b4bSpec[b4bLimits](t, "limits").Priority)
	if err = s.ProcessExtraction(context.Background(), *job); err != nil {
		t.Fatal(err)
	}
	b4bDrain(t, s)
	b2Equal(t, len(f.all()), 1)
	b2Equal(t, b4bProgress(t, s, scope, a.Batch).Organized, len(a.Messages))
}

func TestPhase2B4b_C9_ConversationCallsAtMostOneFifthOfMessageCalls(t *testing.T) {
	spec := b4bSpec[struct {
		Messages int `json:"messages"`
		OldCalls int `json:"per_message_calls"`
		MaxCalls int `json:"conversation_calls_max"`
	}](t, "C9")
	s, scope := testStore(t), owner()
	oldModel := b1Model(t, s, map[string]any{"items": []any{}})
	roles, texts := []string{}, []string{}
	for n := 0; n < spec.Messages; n++ {
		text := fmt.Sprintf("合成短历史第%03d句。", n+1)
		roles = append(roles, "user")
		texts = append(texts, text)
		src := b2Source(t, s, scope, "archive", "user", text, nil)
		b2Extract(t, s, scope, oldModel, src)
	}
	b2Equal(t, len(oldModel.all()), spec.OldCalls)
	// The baseline above uses standalone historical messages with no conversation
	// membership, making real per-message HTTP calls without changing production.
	s2, scope2 := testStore(t), owner()
	whole := b4bModel(t, s2, func(b4bInput) any { return map[string]any{"items": []any{}} })
	a := b4bImport(t, s2, scope2, b4bMessages(t, roles, texts))
	b4bOperation(t, s2, scope2, a.Batch, "organize")
	b4bDrain(t, s2)
	if len(whole.all()) == 0 || len(whole.all()) > spec.MaxCalls {
		t.Errorf("whole-conversation calls=%d, want 1..%d", len(whole.all()), spec.MaxCalls)
	}
	delivered := 0
	for _, req := range whole.all() {
		delivered += len(b4bInputFrom(t, req).Messages)
	}
	b2Equal(t, delivered, spec.Messages)
	b2Equal(t, b4bProgress(t, s2, scope2, a.Batch).Organized, spec.Messages)
}

func TestPhase2B4b_C10_CrossSegmentWithdrawalCarriesOnlyThisRunsMemories(t *testing.T) {
	s, scope := testStore(t), owner()
	spec := b4bSpec[struct {
		First       string `json:"first_memory"`
		Second      string `json:"second_memory"`
		FirstQuote  string `json:"first_quote"`
		SecondQuote string `json:"second_quote"`
		Ref         int    `json:"withdraw_ref"`
		Unknown     int    `json:"unknown_ref"`
	}](t, "C10")
	f := b4bModel(t, s, func(in b4bInput) any {
		if in.Messages[0].Index == 1 {
			return map[string]any{"items": []any{b4bItem(1, spec.First, spec.FirstQuote, "plan")}}
		}
		return map[string]any{"items": []any{b4bItem(4, spec.Second, spec.SecondQuote, "plan")}, "withdraw": []int{spec.Ref, spec.Unknown}}
	})
	a := b4bLongFixture(t, s, scope, []string{spec.FirstQuote, "", "", spec.SecondQuote})
	protected := b4bOldClaim(t, s, scope, "用户已确认的旧计划", a.Sources[0])
	workspaceCommand(t, s, scope, workspace.Command{Type: "confirmMemory", ID: string(protected.ID)})
	edited := b4bOldClaim(t, s, scope, "用户会改过的旧计划", a.Sources[1])
	b1Correct(t, s, scope, edited, "用户已改过的旧计划")
	b4bOperation(t, s, scope, a.Batch, "organize")
	b4bDrain(t, s)
	b4bBoundaries(t, f.all(), a)
	second := b4bInputFrom(t, f.all()[1])
	b2Equal(t, second.Earlier, []b4bEarlier{{Ref: spec.Ref, Text: spec.First}})
	b1Active(t, s, scope, protected, true)
	b1Active(t, s, scope, edited, true)
	count := 0
	for _, m := range b2Snapshot(t, s, scope).Memories {
		if m.Text == spec.First {
			t.Error("withdrawn plan survived")
		}
		if m.Text == spec.Second {
			count++
			b4bEvidence(t, s, scope, m, a.Sources[3], spec.SecondQuote, a.Messages[3].At)
		}
	}
	b2Equal(t, count, 1)
}

func TestPhase2B4b_C11_VisibleLimitsRejectHiddenQuoteAndPreserveOriginals(t *testing.T) {
	s, scope := testStore(t), owner()
	spec := b4bSpec[struct {
		AIOriginal   int    `json:"assistant_original_characters"`
		AIVisible    int    `json:"assistant_visible_characters"`
		UserOriginal int    `json:"user_original_characters"`
		UserVisible  int    `json:"user_visible_characters"`
		Calls        int    `json:"calls"`
		Memories     int    `json:"memories"`
		Visible      string `json:"visible_quote"`
		Hidden       string `json:"hidden_quote"`
	}](t, "C11")
	assistant := b4bSized("AI 长方案开头。", spec.AIOriginal)
	user := []rune(b4bSized("用户超长资料。", spec.UserOriginal))
	copy(user[20:], []rune(spec.Visible))
	copy(user[spec.UserVisible+20:], []rune(spec.Hidden))
	f := b4bModel(t, s, func(in b4bInput) any {
		if in.Messages[0].Index == 1 {
			return map[string]any{"items": []any{}}
		}
		return map[string]any{"items": []any{b4bItem(2, "有效成都计划", spec.Visible, "plan"), b4bItem(2, "不可见杭州计划", spec.Hidden, "plan")}}
	})
	a := b4bImport(t, s, scope, b4bMessages(t, []string{"assistant", "user"}, []string{assistant, string(user)}))
	b4bOperation(t, s, scope, a.Batch, "organize")
	b4bDrain(t, s)
	b2Equal(t, len(f.all()), spec.Calls)
	if len(f.all()) != spec.Calls {
		t.Fatal("cannot inspect incomplete long-message requests")
	}
	first := b4bInputFrom(t, f.all()[0])
	second := b4bInputFrom(t, f.all()[1])
	b2Equal(t, len(first.Messages), 1)
	b2Equal(t, first.Messages[0].Text, string([]rune(assistant)[:spec.AIVisible]))
	b2Equal(t, len(second.Messages), 1)
	b2Equal(t, second.Messages[0].Index, 2)
	b2Equal(t, second.Messages[0].Text, string(user[:spec.UserVisible]))
	memories := b2Snapshot(t, s, scope).Memories
	b2Equal(t, len(memories), spec.Memories)
	m := b2One(t, memories)
	b2Equal(t, m.Text, "有效成都计划")
	b4bEvidence(t, s, scope, m, a.Sources[1], spec.Visible, a.Messages[1].At)
	for n, source := range a.Sources {
		raw, err := s.GetSource(context.Background(), scope, source.ID, source.Version)
		if err != nil {
			t.Fatal(err)
		}
		b2Equal(t, raw.Source.Text, a.Messages[n].Text)
	}
	b4bRecord(t, s, scope, a.Sources[0], "empty", 0)
	b4bRecord(t, s, scope, a.Sources[1], "done", spec.Memories)
}

func TestPhase2B4b_C12_OverlapCannotBeEvidenceForNewSegment(t *testing.T) {
	spec := b4bSpec[struct {
		OverlapQuote string `json:"overlap_quote"`
		ValidQuote   string `json:"valid_quote"`
		Memory       string `json:"accepted_memory"`
		Invalid      int    `json:"only_context_memories"`
		Mixed        int    `json:"mixed_memories"`
	}](t, "C12")
	for _, mixed := range []bool{false, true} {
		t.Run(fmt.Sprintf("valid_body_item_%t", mixed), func(t *testing.T) {
			s, scope := testStore(t), owner()
			f := b4bModel(t, s, func(in b4bInput) any {
				items := []any{}
				if in.Messages[0].Index != 1 {
					items = append(items, b4bItem(1, "错误的上文依据", spec.OverlapQuote, "plan"))
					if mixed {
						items = append(items, b4bItem(4, spec.Memory, spec.ValidQuote, "plan"))
					}
				}
				return map[string]any{"items": items}
			})
			a := b4bLongFixture(t, s, scope, []string{spec.OverlapQuote, "", "", spec.ValidQuote})
			b4bOperation(t, s, scope, a.Batch, "organize")
			b4bDrain(t, s)
			b4bBoundaries(t, f.all(), a)
			want := spec.Invalid
			if mixed {
				want = spec.Mixed
			}
			memories := b2Snapshot(t, s, scope).Memories
			b2Equal(t, len(memories), want)
			if mixed {
				m := b2One(t, memories)
				b2Equal(t, m.Text, spec.Memory)
				b4bEvidence(t, s, scope, m, a.Sources[3], spec.ValidQuote, a.Messages[3].At)
			}
		})
	}
}
