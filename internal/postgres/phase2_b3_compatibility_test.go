package postgres

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/soaringjerry/PCAS/internal/memory"
)

type b3BaselineGold struct {
	Commit string
	S6     struct{ Plain, Structured, MetadataSuffix string }
	S10    []struct {
		Request  memory.RecallRequest
		External bool
		Status   int
		Body     string
	}
}

func b3BaselineOracle(t *testing.T) b3BaselineGold {
	t.Helper()
	var g b3BaselineGold
	if err := json.Unmarshal(b3Gold(t)["preBatch3Baseline"], &g); err != nil {
		t.Fatal(err)
	}
	if g.Commit == "" || len(g.S10) != 15 || g.S6.Plain == "" || g.S6.Structured == "" {
		t.Fatal("missing pre-implementation frozen observations")
	}
	return g
}
func b3ModelMemoryContent(t *testing.T, prompt string) string {
	t.Helper()
	start := strings.Index(prompt, "召回的记忆（引用短别名）：")
	if start < 0 {
		t.Fatal("actual model request lacks the baseline memory heading")
	}
	end := strings.Index(prompt[start:], "\nTHIS：")
	if end < 0 {
		t.Fatal("actual model request lacks the baseline section boundary")
	}
	return prompt[start : start+end]
}

func TestPhase2B3_S6_NoConditionsPreserveBaselineBytesAndOnlyAppendMetadata(t *testing.T) {
	gold := b3BaselineOracle(t)
	for _, structured := range []bool{false, true} {
		t.Run(fmt.Sprintf("structured_%v", structured), func(t *testing.T) {
			s := b1Store(t)
			scope := memory.Scope{OwnerID: b3FixedID(999), PrincipalID: "owner", IsOwner: true}
			f := b1Model(t, s, b3Used())
			b3BaselineData(t, s, scope)
			if structured {
				b3BaselineStructured(t, s, scope)
			}
			mustTurn(t, s, scope, turnRequest("neutralneedle"))
			content := b3ModelMemoryContent(t, f.last(t).Prompt)
			want := gold.S6.Plain
			if structured {
				want = gold.S6.Structured
				lines := strings.Split(content, "\n")
				seen := 0
				for i, line := range lines {
					if strings.HasPrefix(line, "[M") {
						if !strings.HasSuffix(line, gold.S6.MetadataSuffix) {
							t.Errorf("memory line lacks exactly the R12 suffix: %q", line)
						} else {
							lines[i] = strings.TrimSuffix(line, gold.S6.MetadataSuffix)
						}
						seen++
					}
				}
				if seen != 3 {
					t.Errorf("structured baseline memory lines=%d want 3", seen)
				}
				content = strings.Join(lines, "\n")
			}
			want = compareBaselineExpectation(t, want, false)
			if content != want {
				t.Errorf("actual memory/original bytes differ from baseline %s:\nwant:\n%s\ngot:\n%s", gold.Commit, want, content)
			}
		})
	}
}

func TestPhase2B3_S10_PublicRecallFifteenRequestsByteIdentical(t *testing.T) {
	s := b1Store(t)
	scope := memory.Scope{OwnerID: b3FixedID(999), PrincipalID: "owner", IsOwner: true}
	b1Model(t, s, b3Used())
	b3BaselineData(t, s, scope)
	gold := b3BaselineOracle(t)
	for i, c := range gold.S10 {
		t.Run(fmt.Sprintf("request_%02d", i+1), func(t *testing.T) {
			// The frozen continuation cases included an activity prior. C7
			// requires actual use evidence for that prior and enables it in
			// every mode. Give each case its original activity/no-activity
			// condition without relaxing any ranking or pagination oracle.
			b3Exec(t, s, `DELETE FROM use_events WHERE owner_id=$1 AND event_key LIKE 'b3-baseline-confirmation-%'`, scope.OwnerID)
			if c.Request.Mode == memory.Continue {
				for _, n := range []int{20, 21, 22} {
					b3Exec(t, s, `INSERT INTO use_events(owner_id,event_key,record_id,record_version,kind,occurred_at) VALUES($1,$2,$3,1,'confirmation','2025-08-01T00:00:00Z')`, scope.OwnerID, fmt.Sprintf("b3-baseline-confirmation-%d", n), b3FixedID(n))
				}
			}
			who := scope
			if c.External {
				who.IsOwner = false
				who.PrincipalID = "baseline-external"
			}
			w := b1HTTP(t, s, who, "POST", "/v1/memory/recall", c.Request)
			body := w.Body.Bytes()
			if w.Code == 200 {
				var got memory.RecallResult
				if err := json.Unmarshal(body, &got); err != nil {
					t.Fatal(err)
				}
				// C6 adds overflow metadata. Preserve the historical business bytes.
				got.Coverage.Omitted, got.Coverage.OmittedSources = 0, 0
				body = append(asJSON(got), '\n')
			}
			if w.Code != c.Status || !bytes.Equal(body, []byte(c.Body)) {
				t.Errorf("public recall changed from %s: status=%d want %d\nwant bytes=%s\ngot bytes=%s", gold.Commit, w.Code, c.Status, c.Body, w.Body.String())
			}
		})
	}
}
