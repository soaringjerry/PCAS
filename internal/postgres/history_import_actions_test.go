package postgres

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/blob"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// An uploaded archive is a history migration. With automatic adoption and idea
// waking both on, a plan the user stated in a 2025 conversation must stay
// history: remembered and reviewable, but neither a task today nor the reason
// a shelved idea wakes. The same sentence arriving through a present-day
// entry keeps acting, so the rule is about the entry, not the wording.
func TestImportedHistoryNeverBecomesCurrentAction(t *testing.T) {
	const said = "明天交成都旅行计划"
	for _, tc := range []struct {
		name     string
		imported bool
	}{
		{name: "uploaded ChatGPT archive", imported: true},
		{name: "present-day entry", imported: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := testStore(t)
			ctx := context.Background()
			scope := owner()
			files, err := blob.NewFiles(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			s.SetBlobs(files)
			workspaceCommand(t, s, scope, workspace.Command{Type: "updateSettings", Patch: asJSON(map[string]bool{"autoAccept": true, "wakeIdeas": true})})
			st := workspaceCommand(t, s, scope, workspace.Command{Type: "addIdea", Title: "去成都"})
			st = workspaceCommand(t, s, scope, workspace.Command{Type: "ideaShelve", ID: st.Ideas[0].ID, Condition: "旅行计划交上去了"})
			idea := st.Ideas[0]

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.imported {
					var request struct {
						Messages []struct{ Role, Content string }
					}
					if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
						t.Error(err)
					}
					for _, message := range request.Messages {
						if message.Role != "system" {
							t.Logf("imported extraction input: %s", message.Content)
						}
					}
				}
				item := map[string]any{"kind": "task", "text": said, "quote": said, "confidence": 1, "explicit": true, "acquisition": "direct"}
				if tc.imported {
					item = b4bItem(1, said, said, "plan")
				}
				content := map[string]any{
					"items":   []map[string]any{item},
					"signals": []conditionSignal{{IdeaID: idea.ID, ConditionID: idea.Conditions[0].ID, Quote: said, Explanation: "写明要交旅行计划", Confidence: 0.99}},
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": string(asJSON(content))}}}, "usage": map[string]int{"prompt_tokens": 100, "completion_tokens": 30}})
			}))
			t.Cleanup(server.Close)
			s.SetModels(&ai.Registry{HTTP: server.Client(), Config: ai.Configuration{Extraction: "model", Providers: []ai.Provider{{ID: "model", Name: "模型", Protocol: "openai", BaseURL: server.URL, Model: "test", MaxOutput: 100, CostMode: "free"}}}})

			var source memory.Ref
			if tc.imported {
				// The official export shape: one user message on the active branch, sent 2025-03-01.
				export := []byte(`[{"id":"c1","title":"成都","current_node":"u1","mapping":{"u1":{"parent":null,"message":{"id":"u1","author":{"role":"user"},"create_time":1740787200,"content":{"parts":["` + said + `"]}}}}}]`)
				archived, err := s.ImportArchive(ctx, scope, "conversations.json", export)
				if err != nil {
					t.Fatal(err)
				}
				if err = s.ProcessAttachment(ctx, leaseStage(t, s, scope, archived.Refs[0], "source.parse")); err != nil {
					t.Fatal(err)
				}
				source.Kind = memory.SourceKind
				if err = s.pool.QueryRow(ctx, `SELECT v.source_id::text,v.version FROM source_versions v JOIN archive_entries e ON (e.owner_id,e.source_id,e.source_version)=(v.owner_id,v.source_id,v.version) WHERE v.owner_id=$1 AND v.body=$2`, string(scope.OwnerID), said).Scan(&source.ID, &source.Version); err != nil {
					t.Fatal("imported message not found", err)
				}
				var role, branch string
				var year int
				if err = s.pool.QueryRow(ctx, `SELECT c.role,c.branch,extract(year from rv.expressed_at)::int FROM source_contexts c JOIN record_versions rv ON (rv.owner_id,rv.record_id,rv.version)=(c.owner_id,c.source_id,c.source_version) WHERE c.owner_id=$1 AND c.source_id=$2`, string(scope.OwnerID), string(source.ID)).Scan(&role, &branch, &year); err != nil || role != "user" || branch == "historical" || year != 2025 {
					t.Fatal("fixture must be a 2025 user message on the active branch", role, branch, year, err)
				}
			} else {
				source = mustIngest(t, s, scope, memory.IngestRequest{Connector: "manual", ExternalID: string(memory.NewID()), ExternalVersion: "1", Title: "笔记", Text: said, MediaType: "text/plain"}).Ref
			}
			if tc.imported {
				var batch, archive string
				if err = s.pool.QueryRow(ctx, `SELECT b.id::text,b.archive_id::text FROM import_batches b JOIN archive_entries e ON(e.owner_id,e.archive_id)=(b.owner_id,b.archive_id) WHERE e.owner_id=$1 AND e.source_id=$2`, string(scope.OwnerID), string(source.ID)).Scan(&batch, &archive); err != nil {
					t.Fatal(err)
				}
				b4bOperation(t, s, scope, memory.ID(batch), "organize")
				b4bOnlyArchiveJobs(t, s, scope, memory.Ref{ID: memory.ID(archive), Kind: memory.SourceKind})
			}
			if err := s.ProcessExtraction(ctx, leaseStage(t, s, scope, source, "source.extract")); err != nil {
				t.Fatal(err)
			}
			if tc.imported {
				b4bDrain(t, s)
			}

			st, err = s.Snapshot(ctx, scope)
			if err != nil {
				t.Fatal(err)
			}
			var candidate *workspace.Candidate
			for i := range st.Candidates {
				if st.Candidates[i].Kind == "task" && st.Candidates[i].Text == said {
					candidate = &st.Candidates[i]
				}
			}
			if tc.imported {
				// Whole-conversation output is a reviewable memory, not a task
				// suggestion. Its pending status is still mandatory.
				var historical *workspace.Memory
				for i := range st.Memories {
					if st.Memories[i].Text == said {
						historical = &st.Memories[i]
					}
				}
				if historical == nil {
					rows, queryErr := s.pool.Query(ctx, `SELECT stage,state,error_code FROM memory_jobs WHERE owner_id=$1 AND stage LIKE 'source.extract%'`, string(scope.OwnerID))
					if queryErr != nil {
						t.Fatal(queryErr)
					}
					for rows.Next() {
						var stage, state, code string
						if scanErr := rows.Scan(&stage, &state, &code); scanErr != nil {
							t.Fatal(scanErr)
						}
						t.Log("extraction job", stage, state, code)
					}
					rows.Close()
					t.Fatal("the statement must remain reviewable as a candidate", st.Memories)
				}
				if len(st.Tasks) != 0 {
					t.Fatalf("2025 history became %d current task(s); first title=%q", len(st.Tasks), st.Tasks[0].Title)
				}
				if historical.Confirmation != b4bSpec[b4bLimits](t, "limits").Confirmation {
					t.Fatal("imported candidate was adopted without the owner", historical.Confirmation)
				}
				if st.Ideas[0].Status != "shelved" || st.Ideas[0].Wake != nil {
					t.Fatalf("2025 history woke an idea: %+v", st.Ideas[0])
				}
				return
			}
			if candidate == nil {
				t.Fatal("the statement must remain reviewable as a candidate", st.Candidates)
			}
			if len(st.Tasks) != 1 || st.Tasks[0].Title != said {
				t.Fatal("a present-day explicit task must still be adopted automatically", st.Tasks)
			}
			if st.Ideas[0].Status != "awakened" {
				t.Fatal("a present-day signal must still wake the idea", st.Ideas[0].Status)
			}
		})
	}
}
