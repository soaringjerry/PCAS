package postgres_test

import (
	"fmt"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
	"net/http"
	"strings"
	"testing"
)

func TestPhase25B4_LiveMemoryContextUsesTrustAndAgentInferredSetting(t *testing.T) {
	for _, include := range []bool{false, true} {
		t.Run(fmt.Sprintf("include_inferred=%v", include), func(t *testing.T) {
			f := phase25B234NewFixture(t)
			g := workspace.MemoryGroup{EntityID: string(f.entity(t, "project", "虚构可信度项目")), Name: "虚构可信度项目", Type: "project"}
			var refs []memory.Ref
			texts := []string{"虚构直接表达：使用青色封面。", "虚构重复表达：使用白色图表。", "虚构可能考虑：换成浅色页面。", "虚构许澄说：汇报放在周五。", "虚构模型推断：喜欢红色页面。"}
			acquisitions := []string{"direct", "direct", "direct", "reported", "inferred"}
			for i, text := range texts {
				r := f.claimWith(t, text, acquisitions[i], "unknown", nil)
				f.labels(t, r, "progress", true, 1, g)
				refs = append(refs, r)
			}
			source, err := f.store.Ingest(f.ctx, f.scope, memory.IngestRequest{Connector: "acceptance", ExternalID: string(memory.NewID()), Text: "另一份虚构资料：使用白色图表。"})
			if err != nil {
				t.Fatal(err)
			}
			f.exec(t, `INSERT INTO evidence(owner_id,id,source_id,source_version,target_id,target_version,locator,acquisition,stance) VALUES($1,$2,$3,$4,$5,$6,'{}','direct','supports')`, f.scope.OwnerID, memory.NewID(), source.Ref.ID, source.Ref.Version, refs[1].ID, refs[1].Version)
			f.card(t, g, refs, false)
			f.model(t, func(_ *http.Request, _ int, r phase25B234ModelRequest) phase25B234ModelReply {
				body := phase25B4Prompt(r)
				for i, trust := range []string{"stated", "repeated", "tentative", "reported"} {
					phase25B4MustContain(t, body, texts[i], "trust="+trust)
				}
				if include {
					phase25B4MustContain(t, body, texts[4], "trust=inferred")
				} else if strings.Contains(body, texts[4]) {
					t.Error("agent excludes inferred but inferred memory reached live memory context")
				}
				return phase25B4ReplyJSON(phase25B4Reply{text: "虚构按可信度回答。"}, false)
			})
			f.secretaryTurn(t, "处理"+g.Name, "light", include)
			f.assertRevisions(t, refs...)
		})
	}
}
