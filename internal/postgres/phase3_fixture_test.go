package postgres

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/blob"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

// Only preconditions are seeded. No handover, effort, diff, reminder, model usage,
// or expected output is written by the fixture. All assertions use this oracle.
type phase3VersionSeed struct {
	Number       int
	BudgetUnits  int
	Body, Author string
	At           time.Time
	BasedOn      int
	RunID        string
}
type phase3DocumentSeed struct {
	ID, Title string
	Versions  []phase3VersionSeed
}
type phase3FileSeed struct {
	Title, MediaType string
	Bytes            []byte
}
type phase3ProjectSeed struct {
	ID, Name                                       string
	Entity, ScopedMemory, OldMemory, CurrentMemory int
	Items                                          []workspace.Item
	Documents                                      []phase3DocumentSeed
	Runs                                           []workspace.Run
	Files                                          []phase3FileSeed
}
type phase3Corpus struct {
	Base     phase26Corpus
	Projects []phase3ProjectSeed
}

func phase3ID(kind, project, index int) string {
	return fmt.Sprintf("33000000-%04x-4000-8000-%012x", kind, project*10000+index)
}
func phase3CorpusSeed() phase3Corpus {
	c := phase3Corpus{Base: phase26CorpusSeed()}
	png, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jRZkAAAAASUVORK5CYII=")
	for i := 0; i < 30; i++ {
		p := phase3ProjectSeed{ID: phase3ID(1, i, 0), Name: fmt.Sprintf("虚构青湾项目%02d", i), Entity: 1 + 4*i, ScopedMemory: 4800 + i, OldMemory: 4700 + 2*i, CurrentMemory: 4701 + 2*i}
		if i%2 == 1 {
			p.Name = fmt.Sprintf("Fictitious Azure Bay Project %02d", i)
		}
		n := 5 + (i*7)%36
		if i == 29 {
			n = 40
		}
		for j := 0; j < n; j++ {
			it := workspace.Item{ID: phase3ID(2, i, j), Kind: "task", Version: 1, Title: fmt.Sprintf("虚构核验事项 %02d-%02d", i, j), ProjectID: p.ID, Status: []string{"todo", "doing", "waiting", "done", "cancelled"}[j%5], CreatedAt: c.Base.Now.AddDate(0, 0, -14).Format(time.RFC3339), UpdatedAt: c.Base.Now.Format(time.RFC3339), RemindersOn: true, Checklist: []workspace.Check{}, Triggers: []workspace.Trigger{}, Sources: []workspace.SourceRef{}, History: []workspace.Revision{}, NextSteps: []string{}}
			if j%4 != 0 {
				it.Due = time.Date(2026, 11, 9, 17, 0, 0, 0, time.UTC).AddDate(0, 0, j%8).Format(time.RFC3339)
			}
			if i%2 == 1 {
				it.Title = fmt.Sprintf("Fictitious verification item %02d-%02d", i, j)
			}
			p.Items = append(p.Items, it)
		}
		// This task's latest state deliberately differs from the old note's claim.
		p.Items[0].Status = "done"
		p.Items[0].Title = fmt.Sprintf("虚构卡点已解决 %02d", i)
		for d := 0; d < 1+i%10; d++ {
			doc := phase3DocumentSeed{ID: phase3ID(3, i, d), Title: fmt.Sprintf("虚构方案 %02d-%02d", i, d)}
			versions := 1 + (i*3+d)%20
			if i == 0 && d == 0 {
				versions = 2
			}
			for v := 1; v <= versions; v++ {
				body := fmt.Sprintf("虚构青湾项目%02d固定目标。\n\n预算为%d虚构单位。\n\n仅第%d版的新增依据。", i, 100+v*17, v)
				if i%2 == 1 {
					body = fmt.Sprintf("Fictitious goal %02d.\n\nBudget: %d fictitious units.\n\nEvidence unique to version %d.", i, 100+v*17, v)
				}
				if i == 0 && d == 0 {
					if v == 1 {
						body = "虚构目标：青湾展览。\n\n删除段：旧运输安排。\n\n固定段：样品编号。\n\n预算：100虚构单位。\n\n固定段：验收清单。"
					} else {
						body = "虚构目标：青湾展览。\n\n固定段：样品编号。\n\n预算：200虚构单位。\n\n固定段：验收清单。\n\n新增段：改用青湾仓库。"
					}
				}
				budget := 100 + v*17
				if i == 0 && d == 0 {
					budget = v * 100
				}
				doc.Versions = append(doc.Versions, phase3VersionSeed{BudgetUnits: budget, Number: v, Body: body, Author: []string{"user", "user", "deputy", "secretary"}[(v-1)%4], At: c.Base.Now.Add(-time.Duration(versions-v) * 5 * time.Minute), BasedOn: v - 1})
			}
			p.Documents = append(p.Documents, doc)
		}
		for r := 0; r < i%6; r++ {
			run := workspace.Run{ID: phase3ID(4, i, r), ThingID: p.ID, AgentID: "manual", Kind: "draft", Prompt: "虚构项目的取材工作", Status: "done", Output: fmt.Sprintf("虚构副手结果 %02d-%02d：第%d次复核完成。", i, r, r), CreatedAt: c.Base.Now.Add(-time.Duration(10-r) * time.Hour).Format(time.RFC3339), FinishedAt: c.Base.Now.Add(-time.Duration(9-r) * time.Hour).Format(time.RFC3339)}
			if r%2 == 0 {
				run.Adopted = &workspace.Adoption{As: "memory", At: run.FinishedAt}
			}
			p.Runs = append(p.Runs, run)
		}
		for k := 0; k < i%9; k++ {
			payload := append(append([]byte{}, png...), []byte(fmt.Sprintf("fictitious-%02d-%02d", i, k))...)
			p.Files = append(p.Files, phase3FileSeed{fmt.Sprintf("虚构样本%02d-%02d.png", i, k), "image/png", payload})
		}
		c.Base.Memories[p.OldMemory].Text = "虚构卡点仍未解决，沿用旧预算100单位。"
		c.Base.Memories[p.ScopedMemory].Text = fmt.Sprintf("虚构项目%02d在2026年11月5日下午5点前交方案。", i)
		if i%2 == 1 {
			c.Base.Memories[p.ScopedMemory].Text = fmt.Sprintf("Fictitious project %02d must submit its proposal by November 5, 2026 at 5 pm UTC.", i)
		}
		c.Base.Memories[p.CurrentMemory].Text = "Fictitious blocker resolved; use the latest document budget. A fictitious correction replaces the old blocker claim."
		c.Projects = append(c.Projects, p)
	}
	return c
}

type phase3LoadedFixture struct {
	*phase26LoadedFixture
	Gold     phase3Corpus
	FileRefs map[string][]memory.Ref
}

func phase3LoadFixture(t *testing.T) *phase3LoadedFixture {
	t.Helper()
	f := &phase3LoadedFixture{phase26LoadedFixture: phase26LoadFixture(t), Gold: phase3CorpusSeed(), FileRefs: map[string][]memory.Ref{}}
	f.Corpus = f.Gold.Base
	s, ctx, scope := f.Store, f.Context, f.Scope
	if _, err := s.Snapshot(ctx, scope); err != nil {
		t.Fatal(err)
	} // Explicit owner/agent setup, outside read acceptance.
	files, err := blob.NewFiles(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.SetBlobs(files)
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		for _, p := range f.Gold.Projects {
			project := workspace.Item{ID: p.ID, Kind: "project", Version: 1, Title: p.Name, Name: p.Name, Status: "active", Goal: "完成虚构青湾展览", CreatedAt: f.Gold.Base.Now.AddDate(0, 0, -14).Format(time.RFC3339), UpdatedAt: f.Gold.Base.Now.Format(time.RFC3339)}
			if err := saveItem(ctx, tx, scope, project); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE entity_versions SET disambiguation=disambiguation || jsonb_build_object('work_item_id',$3::text) WHERE owner_id=$1 AND entity_id=$2`, scope.OwnerID, f.Entities[p.Entity], p.ID); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE claim_revisions SET scope=scope || jsonb_build_object('project_id',$3::text) WHERE owner_id=$1 AND claim_id=$2`, scope.OwnerID, f.Claims[p.ScopedMemory], p.ID); err != nil {
				return err
			}
			// Add a correction pair with independent provenance, leaving exactly 5,000 memories.
			for _, x := range []struct {
				index int
				text  string
			}{{p.ScopedMemory, f.Gold.Base.Memories[p.ScopedMemory].Text}, {p.OldMemory, f.Gold.Base.Memories[p.OldMemory].Text}, {p.CurrentMemory, f.Gold.Base.Memories[p.CurrentMemory].Text}} {
				if _, err := tx.Exec(ctx, `UPDATE claim_revisions SET value=to_jsonb($3::text),scope=scope || jsonb_build_object('project_id',$4::text) WHERE owner_id=$1 AND claim_id=$2`, scope.OwnerID, f.Claims[x.index], x.text, p.ID); err != nil {
					return err
				}
			}
			if _, err := tx.Exec(ctx, `UPDATE claims SET retired='superseded',retired_by=$3,retired_at=now() WHERE owner_id=$1 AND id=$2`, scope.OwnerID, f.Claims[p.OldMemory], f.Claims[p.CurrentMemory]); err != nil {
				return err
			}
			// Existing extracted deadline is H1 INPUT, not extraction output under test.
			if _, err := tx.Exec(ctx, `INSERT INTO deadlines(owner_id,id,claim_id,claim_version,kind,at,title,original_text) VALUES($1,$2,$3,1,'deadline','2026-11-05T17:00:00Z',$4,$5)`, scope.OwnerID, phase3ID(5, int(p.ScopedMemory-4800), 0), f.Claims[p.ScopedMemory], "Fictitious project proposal deadline", f.Gold.Base.Memories[p.ScopedMemory].Text); err != nil {
				return err
			}
			for _, it := range p.Items {
				if err := saveItem(ctx, tx, scope, it); err != nil {
					return err
				}
			}
			for _, doc := range p.Documents {
				for _, v := range doc.Versions {
					d := workspace.Doc{ID: doc.ID, ThingID: p.ID, Title: doc.Title, Body: v.Body, By: v.Author, CreatedAt: doc.Versions[0].At.Format(time.RFC3339), UpdatedAt: v.At.Format(time.RFC3339)}
					// Production writer, not a hand-built version table. A missing D1 implementation
					// is detected independently by the version-list acceptance.
					if err := saveDoc(withActor(ctx, v.Author), tx, scope, d); err != nil {
						return err
					}
				}
			}
			for _, run := range p.Runs {
				if _, err := tx.Exec(ctx, `INSERT INTO agent_runs(owner_id,id,thing_id,agent_id,status,reserved_cost,created_at,document) VALUES($1,$2,$3,$4,'done',0,$5,$6)`, scope.OwnerID, run.ID, p.ID, run.AgentID, run.CreatedAt, asJSON(run)); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range f.Gold.Projects {
		for k, file := range p.Files {
			var result memory.IngestResult
			// Use the published project upload API as soon as implemented. The
			// initial integration lacks it: retain raw inputs with oracle project
			// associations; TestPhase3T1ProjectFileScope stays skipped S-P3-004.
			scoped := false
			if reflect.ValueOf(s).MethodByName("UploadProjectFile").IsValid() {
				values, uploadErr := phase3ReflectCall(s, "UploadProjectFile", ctx, scope, p.ID, file.Title, file.MediaType, bytes.NewReader(file.Bytes))
				if uploadErr == nil {
					var uploaded phase3FileUpload
					decodePhase3(t, asJSON(values[0].Interface()), &uploaded)
					if uploaded.File.ProjectID != p.ID {
						t.Fatal("fixture upload lost project scope")
					}
					result.Ref = memory.Ref{ID: memory.ID(uploaded.File.SourceID), Version: 1, Kind: memory.SourceKind}
					scoped = true
				} else if !errors.Is(uploadErr, memory.ErrUnavailable) {
					t.Fatal(uploadErr)
				}
			}
			if !scoped {
				result, err = s.IngestAttachment(ctx, scope, memory.IngestRequest{Connector: "phase3-fictitious", ExternalID: fmt.Sprintf("%s-%d", p.ID, k), ExternalVersion: "1", Title: file.Title, MediaType: file.MediaType}, bytes.NewReader(file.Bytes))
			}
			if err != nil {
				t.Fatal(err)
			}
			f.FileRefs[p.ID] = append(f.FileRefs[p.ID], result.Ref)
		}
	}
	for _, batch := range []int{47, 48} {
		var original strings.Builder
		for _, m := range f.Gold.Base.Memories[batch*100 : (batch+1)*100] {
			original.WriteString(m.Text)
			original.WriteByte('\n')
		}
		if _, err := s.pool.Exec(ctx, `UPDATE source_versions SET body=$3,content_hash=sha256(convert_to($3,'UTF8')) WHERE owner_id=$1 AND source_id=$2 AND version=1`, scope.OwnerID, f.Sources[batch].ID, original.String()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.pool.Exec(ctx, "ANALYZE"); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestPhase3T1FrozenCorpus(t *testing.T) {
	TestPhase26T1CorpusOracle(t)
	c := phase3CorpusSeed()
	if !reflect.DeepEqual(c, phase3CorpusSeed()) {
		t.Fatal("nondeterministic corpus")
	}
	if len(c.Base.Memories) != 5000 || len(c.Base.Groups) != 300 || len(c.Base.Entities) != 1000 || len(c.Projects) != 30 {
		t.Fatal("base/extension scale")
	}
	bounds := map[string][2]int{"items": {999, 0}, "docs": {999, 0}, "versions": {999, 0}, "runs": {999, 0}, "files": {999, 0}}
	measure := func(k string, n int) {
		b := bounds[k]
		if n < b[0] {
			b[0] = n
		}
		if n > b[1] {
			b[1] = n
		}
		bounds[k] = b
	}
	for _, p := range c.Projects {
		measure("items", len(p.Items))
		measure("docs", len(p.Documents))
		measure("runs", len(p.Runs))
		measure("files", len(p.Files))
		for _, d := range p.Documents {
			measure("versions", len(d.Versions))
		}
		if p.Items[0].Status != "done" || p.OldMemory == p.CurrentMemory {
			t.Fatal("missing content oracle")
		}
	}
	want := map[string][2]int{"items": {5, 40}, "docs": {1, 10}, "versions": {1, 20}, "runs": {0, 5}, "files": {0, 8}}
	if !reflect.DeepEqual(bounds, want) {
		t.Fatalf("range=%v want=%v", bounds, want)
	}
	t.Logf("T1 fictional deterministic extension: projects=30 bounds=%v", bounds)
}
func TestPhase3T1LoadedScale(t *testing.T) {
	start := time.Now()
	f := phase3LoadFixture(t)
	expected := map[string]int{"claims": 5000, "entities": 1000, "work_items": 30, "work_documents": 0, "agent_runs": 0}
	for _, p := range f.Gold.Projects {
		expected["work_items"] += len(p.Items)
		expected["work_documents"] += len(p.Documents)
		expected["agent_runs"] += len(p.Runs)
	}
	for table, want := range expected {
		var n int
		if err := f.Store.pool.QueryRow(f.Context, "SELECT count(*) FROM "+table+" WHERE owner_id=$1", f.Scope.OwnerID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != want {
			t.Fatalf("%s=%d want=%d", table, n, want)
		}
	}
	for _, p := range f.Gold.Projects {
		for _, d := range p.Documents {
			var got workspace.Doc
			raw := []byte{}
			if err := f.Store.pool.QueryRow(f.Context, `SELECT document FROM work_documents WHERE owner_id=$1 AND id=$2`, f.Scope.OwnerID, d.ID).Scan(&raw); err != nil {
				t.Fatal(err)
			}
			decodePhase3(t, raw, &got)
			if got.Body != d.Versions[len(d.Versions)-1].Body {
				t.Fatal("latest body differs from gold")
			}
		}
		for i, ref := range f.FileRefs[p.ID] {
			r, _, _, err := f.Store.OpenAttachment(f.Context, f.Scope, ref.ID, ref.Version)
			if err != nil {
				t.Fatal(err)
			}
			data := new(bytes.Buffer)
			_, err = data.ReadFrom(r)
			r.Close()
			if err != nil || !bytes.Equal(data.Bytes(), p.Files[i].Bytes) {
				t.Fatal("fictional file bytes changed", err)
			}
		}
	}
	t.Logf("T1 database preconditions=%v load_ms=%.3f; historical versions are certified by D1, not fixture loading", expected, float64(time.Since(start).Microseconds())/1000)
}

func TestPhase3T1ProjectFileScope(t *testing.T) {
	phase3Finding(t, "S-P3-004")
	f := phase3LoadFixture(t)
	h := phase3NewHTTP(t, f)
	for _, p := range f.Gold.Projects {
		var list struct{ Items []phase3File }
		h.get(t, f.Context, "/v1/workspace/items/"+p.ID+"/files", &list)
		if len(list.Items) != len(p.Files) {
			t.Fatalf("project %s files=%d want=%d", p.ID, len(list.Items), len(p.Files))
		}
		for _, file := range list.Items {
			if file.ProjectID != p.ID {
				t.Fatal("file assigned to another/global project", file)
			}
		}
	}
}
