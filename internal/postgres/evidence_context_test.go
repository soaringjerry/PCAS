package postgres

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// All names, conversations and messages in these tests are fictional.
func e4Conversation(t *testing.T, s *Store, scope memory.Scope, conversation string, n int) []memory.Ref {
	t.Helper()
	refs := []memory.Ref{}
	at := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)
	for i := 0; i < n; i++ {
		body := fmt.Sprintf("虚构消息 %02d：演示项目上下文", i)
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		switch i {
		case 4:
			body = "我们讨论的是测试人物甲的演示项目。"
		case 5:
			body = "AI 建议交流技术方案。"
		case 6:
			body = "我想给那位同事发个邮件。"
		case 8:
			body = "仅讨论技术方案，尚未决定合作。"
		}
		ref := mustIngest(t, s, scope, memory.IngestRequest{Connector: "archive-records", ExternalID: fmt.Sprintf("synthetic/%s/%03d", conversation, i), ExternalVersion: "1", Title: "虚构对话", Text: body, MediaType: "text/plain", ExpressedAt: &at}).Ref
		parent := ""
		if i > 0 {
			parent = fmt.Sprintf("%03d", i-1)
		}
		b3Exec(t, s, `INSERT INTO source_contexts(owner_id,source_id,source_version,conversation_key,role,parent_key,branch) VALUES($1,$2,1,$3,$4,$5,'active')`, scope.OwnerID, ref.ID, conversation, role, parent)
		refs = append(refs, ref)
	}
	return refs
}

func TestEvidenceConversationWindowPaginationAndIsolation(t *testing.T) {
	s, scope := b1Store(t), owner()
	refs := e4Conversation(t, s, scope, "fiction-one", 13)
	ctx := context.Background()
	in := memory.ConversationRequest{ID: refs[6].ID, Version: 1}
	out, err := s.SourceConversation(ctx, scope, in)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Messages) != 7 || out.Messages[0].ID != refs[3].ID || out.Messages[3].ID != refs[6].ID || !out.Messages[3].Anchor || out.Messages[6].ID != refs[9].ID || out.Before != refs[3].ID || out.After != refs[9].ID {
		t.Fatalf("wrong window: %+v", out)
	}
	for _, side := range []string{"before", "after"} {
		page := in
		page.Limit = 3
		if side == "before" {
			page.Before = out.Before
		} else {
			page.After = out.After
		}
		a, err := s.SourceConversation(ctx, scope, page)
		if err != nil {
			t.Fatal(err)
		}
		b, err := s.SourceConversation(ctx, scope, page)
		if err != nil || !reflect.DeepEqual(a, b) {
			t.Fatal("page replay differs", err)
		}
		offset := 0
		if side == "after" {
			offset = 10
		}
		if len(a.Messages) != 3 {
			t.Fatal("bad page size")
		}
		for i, m := range a.Messages {
			if m.ID != refs[offset+i].ID {
				t.Fatal("page overlap or ordering error")
			}
		}
	}
	other := e4Conversation(t, s, scope, "fiction-two", 1)
	in.Before = other[0].ID
	if _, err = s.SourceConversation(ctx, scope, in); !errors.Is(err, memory.ErrInvalid) {
		t.Fatal("cross-conversation cursor accepted", err)
	}
	in.Before = ""
	if _, err = s.SourceConversation(ctx, owner(), in); !errors.Is(err, memory.ErrNotFound) {
		t.Fatal("cross-owner read accepted", err)
	}
	agent := scope
	agent.IsOwner = false
	agent.PrincipalID = "model"
	if _, err = s.SourceConversation(ctx, agent, in); !errors.Is(err, memory.ErrForbidden) {
		t.Fatal("owner endpoint leaked", err)
	}
	b3Exec(t, s, `UPDATE source_contexts SET branch='historical' WHERE owner_id=$1 AND source_id=$2`, scope.OwnerID, refs[6].ID)
	out, err = s.SourceConversation(ctx, scope, in)
	if err != nil || len(out.Messages) != 1 || len(out.Gaps) == 0 {
		t.Fatal("mixed historical branches", err)
	}
	if err := s.Delete(t.Context(), scope, memory.DeleteRequest{Targets: []memory.Ref{refs[6]}}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SourceConversation(ctx, scope, in); !errors.Is(err, memory.ErrNotFound) {
		t.Fatal("deleted anchor returned", err)
	}
}

func TestEvidenceContextsKeepRestrictionsAndDependencies(t *testing.T) {
	s, scope := b1Store(t), owner()
	b1Model(t, s, map[string]any{"reply": "虚构验收", "actions": []any{}})
	refs := e4Conversation(t, s, scope, "fiction-three", 13)
	self := b3Entity(t, s, scope, "self", "我")
	claim := b3Claim(t, s, scope, b3ClaimSpec{Text: "那位同事的演示项目值得联系。", Subject: self, Source: refs[6]})
	claims := []evidenceContextClaim{{Label: "M1", Ref: claim, Text: "那位同事的演示项目值得联系。"}}
	load := func() ([]evidenceContextGroup, []string) {
		t.Helper()
		var groups []evidenceContextGroup
		var gaps []string
		err := pgx.BeginFunc(t.Context(), s.pool, func(tx pgx.Tx) error {
			var err error
			groups, gaps, err = evidenceContextsTx(t.Context(), tx, scope, "model", nil, claims)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		return groups, gaps
	}
	groups, gaps := load()
	if len(groups) != 1 || len(groups[0].Window.Messages) != 7 || len(gaps) != 0 {
		t.Fatal("missing old-memory context")
	}
	deps := append([]memory.Ref{}, groups[0].Window.ProofRefs...)
	for _, m := range groups[0].Window.Messages {
		deps = append(deps, m.Ref)
	}
	verify := func() error {
		return pgx.BeginFunc(t.Context(), s.pool, func(tx pgx.Tx) error {
			return verifyRunTx(t.Context(), tx, scope, workspace.Run{AgentID: "model", ContextVersions: deps})
		})
	}
	if err := verify(); err != nil {
		t.Fatal("authorized context cannot be fenced", err)
	}
	if !slices.Contains(deps, refs[2]) {
		t.Fatal("assistant parent outside window missing from permission dependencies")
	}
	b3Claim(t, s, scope, b3ClaimSpec{Text: "虚构受限信息", Subject: self, Source: refs[2], Agents: []string{"manual"}})
	groups, _ = load()
	if len(groups) != 1 || len(groups[0].Window.Messages) != 1 || len(groups[0].Window.Gaps) == 0 {
		t.Fatal("restricted neighbor leaked into model")
	}
	if err := verify(); err == nil {
		t.Fatal("revoked context stayed valid")
	}
	if err := s.Delete(t.Context(), scope, memory.DeleteRequest{Targets: []memory.Ref{refs[8]}}); err != nil {
		t.Fatal(err)
	}
	public, err := s.SourceConversation(t.Context(), scope, memory.ConversationRequest{ID: refs[6].ID})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range public.Messages {
		if m.ID == refs[8].ID {
			t.Fatal("deleted neighboring text returned")
		}
	}
}

func TestEvidenceContextReachesSecretaryAndManualRun(t *testing.T) {
	s, scope := b1Store(t), owner()
	f := b1Model(t, s, map[string]any{"reply": "已核对虚构材料。", "actions": []any{}})
	refs := e4Conversation(t, s, scope, "fiction-four", 13)
	self := b3Entity(t, s, scope, "self", "我")
	b3Claim(t, s, scope, b3ClaimSpec{Text: "那位同事的演示项目值得联系。", Subject: self, Source: refs[6]})
	req := turnRequest("那位同事的演示项目如何联系？")
	mustTurn(t, s, scope, req)
	prompt := f.last(t).Prompt
	for _, text := range []string{"记忆的来源上下文", "测试人物甲", "尚未决定合作", "AI回复不能当成用户事实", "引用原话"} {
		if !strings.Contains(prompt, text) {
			t.Fatalf("missing %s", text)
		}
	}
	if len(f.all()) != 1 {
		t.Fatal("context caused extra model call")
	}
	deps := b1Refs(t, s, scope, req.RequestID)
	for _, ref := range []memory.Ref{refs[4], refs[6], refs[8]} {
		b1HasRef(t, deps, ref, true)
	}
	st := workspaceCommand(t, s, scope, workspace.Command{Type: "addTask", Title: "核对虚构演示项目"})
	st = workspaceCommand(t, s, scope, workspace.Command{Type: "requestRun", ThingID: st.Tasks[0].ID, AgentID: "manual", Kind: "ask", Prompt: "那位同事的演示项目如何联系？"})
	if !strings.Contains(st.Runs[0].Brief, "尚未决定合作") || !strings.Contains(st.Runs[0].Brief, "引用原话") {
		t.Fatal("manual run lost evidence context")
	}
}

func TestEvidenceContextBudgetAndUnresolvedNames(t *testing.T) {
	s, scope := b1Store(t), owner()
	b1Model(t, s, map[string]any{"reply": "虚构验收", "actions": []any{}})
	refs := e4Conversation(t, s, scope, "fiction-five", 13)
	self := b3Entity(t, s, scope, "self", "我")
	b3Exec(t, s, `UPDATE source_versions SET body=$3 WHERE owner_id=$1 AND source_id=$2`, scope.OwnerID, refs[5].ID, strings.Repeat("虚构长材料。", 1000))
	claim := b3Claim(t, s, scope, b3ClaimSpec{Text: "那位同事的演示项目值得联系。", Subject: self, Source: refs[6]})
	err := pgx.BeginFunc(t.Context(), s.pool, func(tx pgx.Tx) error {
		groups, _, err := evidenceContextsTx(t.Context(), tx, scope, "model", nil, []evidenceContextClaim{{Label: "M1", Ref: claim, Text: "那位同事"}})
		if err != nil {
			return err
		}
		if len(groups) != 1 {
			t.Fatal("context unavailable")
		}
		for _, m := range groups[0].Window.Messages {
			if len([]rune(m.Text)) > 1000 {
				t.Fatal("message exceeded bounded budget")
			}
			if m.ID == refs[5].ID && !strings.Contains(m.Text, "未读部分") {
				t.Fatal("silent truncation")
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	got := groundedMentions([]string{"那位同事", "对方", "测试人物甲"}, "person", "那位同事是对方，测试人物甲在上文出现。")
	if len(got) != 1 || got[0].Name != "测试人物甲" {
		t.Fatal("ambiguous label became a person entity")
	}
}
