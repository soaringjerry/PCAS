package postgres

// Section 11 oracles were frozen in 631ab49 before these tests were written.
import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/worker"
)

type b4DeferredGolden struct {
	Fixtures struct {
		Messages, PauseMessages   int
		Prefix, OrdinaryUtterance string
	}
}

func b4DeferredGold(t *testing.T) b4DeferredGolden {
	t.Helper()
	raw, err := os.ReadFile("../../testdata/phase2/b4-gold.json")
	if err != nil {
		t.Fatal(err)
	}
	var g struct {
		Deferred b4DeferredGolden `json:"coordinator_amendment_b526328"`
	}
	b4JSON(t, raw, &g)
	return g.Deferred
}

// Drain through real Claim and handlers. No leaseStage override can bypass
// the organizing hold; exhaustion proves extraction was not claimable.
func b4DrainQueue(t *testing.T, s *Store) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	w := worker.New(s, map[string]worker.Handler{"source.parse": s.ProcessAttachment, "source.chunk": s.ProcessChunks, "source.tokenize": s.ProcessIndex, "source.extract": s.ProcessExtraction, "source.embed": s.ProcessEmbedding, "claim.embed": s.ProcessEmbedding}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	for range 1000 {
		worked, err := w.RunOnce(ctx)
		if err != nil {
			t.Fatal("actual queue drain", err)
		}
		if !worked {
			return
		}
	}
	t.Fatal("owned synthetic queue did not become idle")
}

func b4OrganizingHeld(t *testing.T, s *Store, scope memory.Scope, id string, expected bool) b4ImportItem {
	t.Helper()
	item := b4ImportItemFor(t, s, scope, id)
	if item.OrganizeLater == nil || *item.OrganizeLater != expected {
		t.Fatalf("organizeLater must be present and equal %t: %+v", expected, item)
	}
	var held bool
	if err := s.pool.QueryRow(context.Background(), `SELECT hold_organizing FROM import_batches WHERE owner_id=$1 AND id=$2`, string(scope.OwnerID), id).Scan(&held); err != nil {
		t.Fatal(err)
	}
	if held != expected {
		t.Errorf("persistent organizing hold=%t; want %t", held, expected)
	}
	return item
}

func b4UploadOrganize(t *testing.T, s *Store, scope memory.Scope, name string, data []byte, mode string) (string, memory.Ref) {
	t.Helper()
	fields := map[string]string{}
	if mode != "" {
		fields["organize"] = mode
	}
	w := b4Upload(t, b4API(s, scope, true), "/v1/connectors/archive", name, data, fields)
	b4OK(t, w)
	var result struct{ BatchID string }
	b4JSON(t, w.Body.Bytes(), &result)
	if result.BatchID == "" {
		t.Fatal("deferred upload omitted batchId")
	}
	item := b4ImportItemFor(t, s, scope, result.BatchID)
	return result.BatchID, memory.Ref{ID: memory.ID(item.ArchiveID), Version: item.ArchiveVersion, Kind: memory.SourceKind}
}

type b4HeldFixture struct {
	s             *Store
	scope         memory.Scope
	f             *b4Fake
	conversations []b4Conversation
	id            string
	archive       memory.Ref
}

func b4HeldImport(t *testing.T, mode string) b4HeldFixture {
	t.Helper()
	x := b4HeldFixture{s: b4Store(t), scope: owner()}
	x.f = b4Model(t, x.s, true)
	g := b4DeferredGold(t)
	x.conversations = b4Conversations(t, g.Fixtures.Prefix, g.Fixtures.Messages)
	x.conversations[len(x.conversations)-1].Messages[0].Text = b4FixtureFor(t, "recall").Text
	x.id, x.archive = b4UploadOrganize(t, x.s, x.scope, "held.zip", b4Zip(t, b4Export(x.conversations), false), mode)
	b4Complete(t, x.s, x.scope, x.id, x.archive)
	b4DrainQueue(t, x.s)
	return x
}

func b4HasExtractionUsage(t *testing.T, s *Store, scope memory.Scope) bool {
	t.Helper()
	// A9 now bills indexing calls too; these were already executed while held.
	for _, row := range b4Usage(t, s, scope) {
		if row.Purpose != "embedding" && row.Purpose != "query_embedding" {
			return true
		}
	}
	return false
}

func b4AssertHeldAndIndexed(t *testing.T, x b4HeldFixture) {
	t.Helper()
	item := b4OrganizingHeld(t, x.s, x.scope, x.id, true)
	if item.Stored != len(x.conversations) || item.Total != item.Stored || item.Organized != 0 {
		t.Errorf("held originals not completely stored without organizing: %+v", item)
	}
	if len(x.f.all()) != 0 || b4HasExtractionUsage(t, x.s, x.scope) {
		t.Error("held import called extraction model or recorded model usage")
	}
	b4MessagesExactlyOnce(t, x.s, x.scope, x.conversations)
	for _, conv := range x.conversations {
		source := b4SourceByText(t, x.s, x.scope, conv.Messages[0].Text)
		var indexed bool
		if err := x.s.pool.QueryRow(context.Background(), `SELECT EXISTS(SELECT 1 FROM chunks c JOIN embeddings e ON e.owner_id=c.owner_id AND e.record_id=c.id AND e.record_version=c.version WHERE c.owner_id=$1 AND c.source_id=$2 AND c.search_vector <> ''::tsvector)`, string(x.scope.OwnerID), string(source.ID)).Scan(&indexed); err != nil {
			t.Fatal(err)
		}
		if !indexed {
			t.Errorf("held original %s lacks chunk/token/vector index", source.ID)
		}
		var pending int
		if err := x.s.pool.QueryRow(context.Background(), `SELECT count(*) FROM memory_jobs WHERE owner_id=$1 AND record_id=$2 AND stage='source.extract' AND state='queued' AND attempts=0`, string(x.scope.OwnerID), string(source.ID)).Scan(&pending); err != nil {
			t.Fatal(err)
		}
		if pending != 1 {
			t.Errorf("held extraction should exist but never be claimed: %s pending=%d", source.ID, pending)
		}
	}
	x.f.mu.Lock()
	vectors := append([]string(nil), x.f.vectors...)
	x.f.mu.Unlock()
	if len(vectors) == 0 {
		t.Error("vacuous vector-index check: fake embedding provider never called")
	}
}

func TestPhase2B4_I15_LaterStoresIndexedOriginalsWithoutExtracting(t *testing.T) {
	x := b4HeldImport(t, "later")
	b4AssertHeldAndIndexed(t, x)
}

func TestPhase2B4_I15_NowPositiveControlCallsExtraction(t *testing.T) {
	x := b4HeldImport(t, "now")
	item := b4OrganizingHeld(t, x.s, x.scope, x.id, false)
	if item.Organized == 0 || item.Stored != item.Total || len(x.f.all()) == 0 {
		t.Errorf("explicit now did not organize: %+v calls=%d", item, len(x.f.all()))
	}
}

func TestPhase2B4_I16_HeldOriginalReachesSecretaryInAnotherConversation(t *testing.T) {
	x := b4HeldImport(t, "later")
	b4AssertHeldAndIndexed(t, x)
	first := mustTurn(t, x.s, x.scope, turnRequest("开启一段合成新聊天"))
	g := b4FixtureFor(t, "recall")
	x.f.set(`{"reply":"密码8624。","used":["S1"],"actions":[]}`, 200)
	req := turnRequest(g.Question)
	out := mustTurn(t, x.s, x.scope, req)
	if out.ConversationID == first.ConversationID {
		t.Error("secretary recall did not switch conversations")
	}
	b1Contains(t, x.f.last(t).Prompt, g.Text)
	b1HasRef(t, b1Refs(t, x.s, x.scope, req.RequestID), b4SourceByText(t, x.s, x.scope, g.Text), true)
	item := b4OrganizingHeld(t, x.s, x.scope, x.id, true)
	if item.Organized != 0 {
		t.Error("secretary recall released held extraction")
	}
	for _, row := range b4Usage(t, x.s, x.scope) {
		if row.Purpose != "secretary" && row.Purpose != "embedding" && row.Purpose != "query_embedding" {
			t.Errorf("unexpected held extraction usage: %+v", row)
		}
	}
}

func TestPhase2B4_I17_StartOrganizingReleasesRealPriorityTenExtraction(t *testing.T) {
	x := b4HeldImport(t, "later")
	b4AssertHeldAndIndexed(t, x)
	b4OK(t, b4HTTP(t, x.s, x.scope, "POST", "/v1/connectors/imports/"+x.id+"/organize", nil))
	b4OrganizingHeld(t, x.s, x.scope, x.id, false)
	for _, conv := range x.conversations {
		source := b4SourceByText(t, x.s, x.scope, conv.Messages[0].Text)
		var priority int
		if err := x.s.pool.QueryRow(context.Background(), `SELECT priority FROM memory_jobs WHERE owner_id=$1 AND record_id=$2 AND stage='source.extract'`, string(x.scope.OwnerID), string(source.ID)).Scan(&priority); err != nil {
			t.Fatal(err)
		}
		if priority != 10 {
			t.Errorf("released import priority=%d", priority)
		}
	}
	b4DrainQueue(t, x.s)
	item := b4OrganizingHeld(t, x.s, x.scope, x.id, false)
	if item.Organized == 0 || len(x.f.all()) == 0 {
		t.Errorf("released extraction did not run: %+v calls=%d", item, len(x.f.all()))
	}
	for _, conv := range x.conversations {
		found := false
		for _, request := range x.f.all() {
			if strings.Contains(request.Prompt, conv.Messages[0].Text) {
				found = true
			}
		}
		if !found {
			t.Errorf("released original never reached actual extraction HTTP: %s", conv.ID)
		}
	}
}

func TestPhase2B4_I18_NewSecretaryUtteranceExtractsWhileImportHeld(t *testing.T) {
	x := b4HeldImport(t, "later")
	b4AssertHeldAndIndexed(t, x)
	text := b4DeferredGold(t).Fixtures.OrdinaryUtterance
	req := turnRequest(text)
	mustTurn(t, x.s, x.scope, req)
	source := b1TurnSource(t, x.s, x.scope, req.RequestID)
	x.f.set(`{"items":[]}`, 200)
	b4DrainQueue(t, x.s)
	var extracted bool
	if err := x.s.pool.QueryRow(context.Background(), `SELECT EXISTS(SELECT 1 FROM source_extractions WHERE owner_id=$1 AND source_id=$2 AND state IN ('done','empty'))`, string(x.scope.OwnerID), string(source.ID)).Scan(&extracted); err != nil {
		t.Fatal(err)
	}
	if !extracted {
		t.Error("ordinary new secretary utterance was held with import")
	}
	requests := x.f.all()
	if len(requests) != 2 {
		t.Fatalf("expected secretary call and one new-utterance extraction, got %d", len(requests))
	}
	b1Contains(t, requests[1].Prompt, text)
	for _, conv := range x.conversations {
		b1Absent(t, requests[1].Prompt, conv.Messages[0].Text)
	}
	item := b4OrganizingHeld(t, x.s, x.scope, x.id, true)
	if item.Organized != 0 {
		t.Error("ordinary extraction organized held import")
	}
}

func TestPhase2B4_I19_HeldImportPausesResumesAndDeletesItsClosure(t *testing.T) {
	s, scope := b4Store(t), owner()
	f := b4Model(t, s)
	b4SmallChunks(t, 1)
	conv := b4Conversations(t, "held-pause-delete", b4DeferredGold(t).Fixtures.PauseMessages)
	id, archive := b4UploadOrganize(t, s, scope, "held-pause.zip", b4Zip(t, b4Export(conv), false), "later")
	p := b4ParseAsync(t, s, scope, archive)
	b4Partial(t, s, scope, id, 10)
	paused := b4ImportAction(t, s, scope, id, "pause")
	p.awaitPaused(t)
	stable := b4OrganizingHeld(t, s, scope, id, true)
	if stable.State != "paused" || stable.Stored > paused.Stored+1 || stable.Organized != 0 {
		t.Errorf("held import did not pause after current chunk: %+v", stable)
	}
	for range 10 {
		now := b4OrganizingHeld(t, s, scope, id, true)
		if now.State != "paused" || now.Stored != stable.Stored || now.Organized != stable.Organized {
			t.Fatalf("paused held import continued storing or organizing: %+v -> %+v", stable, now)
		}
		time.Sleep(10 * time.Millisecond)
	}
	b4ImportAction(t, s, scope, id, "resume")
	b4OrganizingHeld(t, s, scope, id, true)
	done := b4Complete(t, s, scope, id, archive)
	if done.Stored != len(conv) || done.Organized != 0 {
		t.Errorf("resuming changed hold or lost originals: %+v", done)
	}
	b4OrganizingHeld(t, s, scope, id, true)
	b4MessagesExactlyOnce(t, s, scope, conv)
	if len(f.all()) != 0 {
		t.Error("pause/resume caused model extraction")
	}
	b4OK(t, b4HTTP(t, s, scope, "POST", "/v1/memory/delete", memory.DeleteRequest{Targets: []memory.Ref{archive}, IncludeSources: true}))
	for _, item := range b4Imports(t, s, scope) {
		if item.ID == id {
			t.Error("deleted held batch remains listed")
		}
	}
	var messages int
	if err := s.pool.QueryRow(context.Background(), `SELECT count(*) FROM source_versions WHERE owner_id=$1 AND body LIKE $2`, string(scope.OwnerID), "%held-pause-delete%").Scan(&messages); err != nil {
		t.Fatal(err)
	}
	if messages != 0 {
		t.Errorf("deleted held originals remain: %d", messages)
	}
	b1Active(t, s, scope, archive, false)
}

func TestPhase2B4_I20_OmittedOrganizeDefaultsToHeldIndexedOriginals(t *testing.T) {
	x := b4HeldImport(t, "")
	b4AssertHeldAndIndexed(t, x)
}
