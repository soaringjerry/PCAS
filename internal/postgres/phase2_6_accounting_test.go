package postgres

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/soaringjerry/PCAS/internal/ai"
)

// A fictitious app-server uses the REAL subscription adapter and never emits
// token usage. Its text decisions come from the independent HTTP content oracle.
func phase26Subscription(t *testing.T, f *phase26LoadedFixture, m *phase26Model) {
	t.Helper()
	dir := t.TempDir()
	binary := filepath.Join(dir, "phase2_6_fake_codex")
	script := `#!/usr/bin/env python3
import json,sys,urllib.request
url=` + strconv.Quote(m.URL) + `
threads={};sequence=0
opener=urllib.request.build_opener(urllib.request.ProxyHandler({}))
def emit(x): print(json.dumps(x),flush=True)
for line in sys.stdin:
 msg=json.loads(line)
 if 'id' not in msg: continue
 method=msg.get('method');p=msg.get('params',{});out={}
 if method=='account/read': out={'account':{'type':'chatgpt'}}
 if method=='thread/start':
  sequence+=1;thread='fictitious-thread-'+str(sequence);threads[thread]=p;out={'thread':{'id':thread}}
 if method=='turn/start':
  thread=p['threadId'];turn='fictitious-turn-'+str(sequence);out={'turn':{'id':turn}}
 emit({'id':msg['id'],'result':out})
 if method=='turn/start':
  data=json.dumps({'system':threads[thread].get('baseInstructions',''),'prompt':p['input'][0]['text']}).encode()
  result=json.loads(opener.open(urllib.request.Request(url,data=data,headers={'Content-Type':'application/json'})).read())
  emit({'method':'item/completed','params':{'threadId':thread,'item':{'type':'agentMessage','text':result['text']}}})
  emit({'method':'turn/completed','params':{'threadId':thread,'turn':{'id':turn,'status':'completed'}}})
`
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	codex, err := ai.NewCodex(binary, filepath.Join(dir, "private-fictitious-home"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(codex.Close)
	// Explicit free on a subscription MUST still use the documented 3/12 internal
	// accounting weights. No inherited settings/authentication files are loaded.
	f.Store.SetModels(&ai.Registry{Codex: codex, Config: ai.Configuration{Extraction: "phase26", Providers: []ai.Provider{{ID: "phase26", Name: "Fictitious subscription", Protocol: "codex", Model: "fictitious", MaxOutput: 8192, CostMode: "free"}}}})
}
func TestPhase26A9EstimatedSubscriptionDailyBudgetAndHealth(t *testing.T) {
	f := phase26LoadFixture(t)
	m := phase26NewModel(t, f)
	phase26Subscription(t, f, m)
	s, ctx, owner := f.Store, f.Context, f.Scope.OwnerID
	phase26Exec(t, f, `UPDATE claims SET organized=2 WHERE owner_id=$1`, owner)
	// Leave one real classification batch and a modeled alias proposal pending.
	phase26Exec(t, f, `UPDATE claims SET organized=0 WHERE owner_id=$1 AND id=ANY($2::uuid[])`, owner, []string{string(f.Claims[4998]), string(f.Claims[4999])})
	phase26Proposal(t, f, 0, 4)
	stages := []string{OrganizeStage, CompareStage, EntityCompareStage, EntityCandidatesStage, HandoverStage}
	total := 0.0
	for _, stage := range stages {
		phase26Isolate(t, f, stage)
		if _, err := phase26Schedule(ctx, s, stage); err != nil {
			t.Fatal(err)
		}
		phase26Isolate(t, f, stage)
		j := phase26ClaimStage(t, f, stage)
		if err := phase26Process(ctx, s, j); err != nil {
			t.Fatalf("positive accounting control %s: %v", stage, err)
		}
		calls := m.calls(stage)
		if len(calls) != 1 {
			t.Fatalf("stage %s actual calls=%d", stage, len(calls))
		}
		call := calls[0]
		var in, out int
		var inputEstimated, outputEstimated, costEstimated bool
		var cost, settled float64
		if err := s.pool.QueryRow(ctx, `SELECT input_tokens,output_tokens,input_estimated,output_estimated,cost_estimated,cost FROM model_usage WHERE owner_id=$1 AND job_id=$2`, owner, j.ID).Scan(&in, &out, &inputEstimated, &outputEstimated, &costEstimated, &cost); err != nil {
			t.Fatal(err)
		}
		if err := s.pool.QueryRow(ctx, `SELECT reserved_cost FROM background_usage WHERE owner_id=$1 AND job_id=$2`, owner, j.ID).Scan(&settled); err != nil {
			t.Fatal(err)
		}
		wantIn, wantOut := utf8.RuneCountInString(call.System+call.Prompt), utf8.RuneCountInString(call.Output)
		want := (float64(wantIn)*3 + float64(wantOut)*12) / 1e6
		if in != wantIn || out != wantOut || !inputEstimated || !outputEstimated || !costEstimated || cost <= 0 || math.Abs(cost-want) > 0.000001 || math.Abs(cost-settled) > 0.000001 {
			t.Errorf("%s estimated usage=%d/%d flags=%t/%t/%t cost=%f settled=%f independent_expected=%d/%d/%f", stage, in, out, inputEstimated, outputEstimated, costEstimated, cost, settled, wantIn, wantOut, want)
		}
		total += settled
	}
	phase26Exec(t, f, `UPDATE claims SET organized=0 WHERE owner_id=$1 AND id=$2`, owner, f.Claims[4999])
	phase26Proposal(t, f, 8, 12)
	// Advance only the handover precondition between joined calls, forcing new due
	// work so the daily cost guard, rather than its one-hour debounce, is exercised.
	phase26Exec(t, f, `UPDATE handovers SET built_at=now()-interval '61 minutes',input_hash='' WHERE owner_id=$1`, owner)
	phase26Exec(t, f, `UPDATE workspace_owners SET settings=jsonb_set(settings,'{dailyBudget}',to_jsonb($2::numeric)) WHERE owner_id=$1`, owner, total)
	for _, stage := range stages {
		phase26Isolate(t, f, stage)
		if _, err := phase26Schedule(ctx, s, stage); err != nil {
			t.Fatal(err)
		}
		phase26Isolate(t, f, stage)
		j := phase26ClaimStage(t, f, stage)
		e := phase26FinishError(t, f, j, phase26Process(ctx, s, j))
		if e.Code != "budget_deferred" || !e.NoAttempt {
			t.Errorf("daily limit did not defer %s without attempt: %+v", stage, e)
		}
		if len(m.calls(stage)) != 1 {
			t.Errorf("daily budget guard made a second provider call for %s", stage)
		}
	}
	health, err := s.BackgroundHealth(ctx, f.Scope)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Stages []struct {
			Stage        string
			Deferred     int
			HourlyBudget int
		}
	}
	if err := json.Unmarshal(health, &decoded); err != nil {
		t.Fatal(err)
	}
	for _, h := range decoded.Stages {
		// Only the stages this test drove had due work; the phase 3 stages
		// (project handover, effort) are idle here and so show no deferral.
		if oneOf(h.Stage, stages...) && h.Deferred < 1 {
			t.Errorf("missing visible daily deferral for %s", h.Stage)
		}
	}
	t.Logf("actual subscription attempts=5 independent_estimated_daily_total=%f health=%s", total, health)
	t.Run("deferral_reason_visibility", func(t *testing.T) {
		phase26Finding(t, "S-P26-001")
		if !strings.Contains(string(health), "budget_deferred") {
			t.Error("daily budget blocks every stage, but stage health omits the deferral reason")
		}
	})
}
