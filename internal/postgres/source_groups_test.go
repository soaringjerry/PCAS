package postgres

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func sourceEntry(t *testing.T, st workspace.State, id string) workspace.Source {
	t.Helper()
	for _, entry := range st.Sources {
		if entry.ID == id {
			return entry
		}
	}
	t.Fatalf("library entry %q missing: %+v", id, st.Sources)
	return workspace.Source{}
}

// What was said is one entry however many sentences there are; the system's
// own action records are not listed; a document stands alone.
func TestLibraryListsSourcesByOrigin(t *testing.T) {
	s, scope := testStore(t), owner()
	ctx := context.Background()
	var output any = `{"reply":"好。","actions":[{"op":"create_task","title":"整理发票"}]}`
	secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) { secretaryModelReply(w, output) })
	mustTurn(t, s, scope, turnRequest("帮我记一下要整理发票"))
	output = `{"reply":"好。","actions":[]}`
	mustTurn(t, s, scope, turnRequest("第二句：周末想去爬山"))
	last := mustTurn(t, s, scope, turnRequest("第三句：百分号 100% 和下划线 a_b"))
	doc, err := memory.NewService(s).Ingest(ctx, scope, memory.IngestRequest{Connector: "file-import", ExternalID: "doc-1", ExternalVersion: "1", Title: "租房合同", Text: "合同正文", MediaType: "text/plain"})
	if err != nil {
		t.Fatal(err)
	}
	st, err := s.Snapshot(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Sources) != 2 {
		t.Fatalf("want what was said and one document, got %+v", st.Sources)
	}
	said := sourceEntry(t, st, "said")
	if said.Name != "跟秘书说的话" || said.Kind != "said" || said.Single || said.ItemCount != 3 {
		t.Fatalf("said entry: %+v", said)
	}
	file := sourceEntry(t, st, string(doc.Ref.ID))
	if file.Name != "租房合同" || file.Kind != "file" || !file.Single {
		t.Fatalf("document entry: %+v", file)
	}
	for _, entry := range last.State.Sources {
		if strings.Contains(entry.Name, "整理发票") {
			t.Fatalf("an action record is listed as material: %+v", entry)
		}
	}

	// Newest first, a page at a time, and the cursor continues where it stopped.
	first, err := s.SourceGroupItems(ctx, scope, "said", "", "", 2)
	if err != nil || len(first.Items) != 2 || first.Next == "" || !strings.HasPrefix(first.Items[0].Excerpt, "第三句") {
		t.Fatalf("first page: %+v %v", first, err)
	}
	rest, err := s.SourceGroupItems(ctx, scope, "said", "", first.Next, 2)
	if err != nil || len(rest.Items) != 1 || rest.Next != "" || !strings.HasPrefix(rest.Items[0].Excerpt, "帮我记一下") {
		t.Fatalf("second page: %+v %v", rest, err)
	}
	// Searching is literal: % and _ are ordinary characters.
	for find, want := range map[string]int{"爬山": 1, "100%": 1, "a_b": 1, "a%b": 0, "axb": 0, "句": 2} {
		got, err := s.SourceGroupItems(ctx, scope, "said", find, "", 50)
		if err != nil || len(got.Items) != want {
			t.Errorf("find %q: %d items %v", find, len(got.Items), err)
		}
	}
	// Quick notes are one entry too.
	for _, text := range []string{"随手记一", "随手记二"} {
		if _, err := memory.NewService(s).Ingest(ctx, scope, memory.IngestRequest{Connector: "capture", ExternalID: text, ExternalVersion: "1", Title: "快速记录", Text: text, MediaType: "text/plain"}); err != nil {
			t.Fatal(err)
		}
	}
	if st, err = s.Snapshot(ctx, scope); err != nil || len(st.Sources) != 3 || sourceEntry(t, st, "capture").ItemCount != 2 || sourceEntry(t, st, "capture").Single {
		t.Fatalf("quick notes entry: %+v %v", st.Sources, err)
	}
	for _, key := range []string{"", "actions", "import:not-a-uuid", string(doc.Ref.ID)} {
		if _, err := s.SourceGroupItems(ctx, scope, key, "", "", 10); !errors.Is(err, memory.ErrInvalid) {
			t.Errorf("key %q: %v", key, err)
		}
	}
	if _, err := s.SourceGroupItems(ctx, memory.Scope{OwnerID: scope.OwnerID, PrincipalID: "agent"}, "said", "", "", 10); !errors.Is(err, memory.ErrForbidden) {
		t.Errorf("non-owner: %v", err)
	}
}

// An import is one entry named after its file, however many messages it holds,
// and waiting to be organized does not make it look busy or broken.
func TestLibraryListsAnImportOnce(t *testing.T) {
	s, scope := b4Store(t), owner()
	ctx := context.Background()
	conversations := b4Conversations(t, "library", 3)
	id, archive := b4ImportFile(t, s, scope, "old-chats.zip", b4Zip(t, b4Export(conversations), false))
	b4Complete(t, s, scope, id, archive)
	st, err := s.Snapshot(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	messages := 0
	for _, c := range conversations {
		messages += len(c.Messages)
	}
	if len(st.Sources) != 1 {
		t.Fatalf("want one entry for the import, got %+v", st.Sources)
	}
	entry := sourceEntry(t, st, "import:"+string(archive.ID))
	if entry.Kind != "import" || entry.Single || entry.ItemCount != messages || entry.Status != "connected" || entry.Note != "" || !strings.Contains(entry.Name, "old-chats") {
		t.Fatalf("import entry: %+v (messages %d)", entry, messages)
	}
	page, err := s.SourceGroupItems(ctx, scope, entry.ID, "", "", 100)
	if err != nil || len(page.Items) != messages {
		t.Fatalf("import items: %d %v", len(page.Items), err)
	}
	for _, item := range page.Items {
		if item.Role == "" || item.Excerpt == "" || item.At == "" {
			t.Fatalf("item lacks who said it, its words or its time: %+v", item)
		}
	}
}
