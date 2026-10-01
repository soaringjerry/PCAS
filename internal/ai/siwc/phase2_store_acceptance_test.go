package siwc_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/ai/siwc"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/postgres"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

const phase2StoreProvider = "chatgpt-direct"
const phase2StoreRawMarker = "SYNTHETIC_RAW_SIWC_NEVER_SUPPLY_826"
const phase2StoreClaim = "独立确认的海棠邀请偏好：措辞亲切且简短"

func phase2StoreDatabase(t *testing.T) (*postgres.Store, memory.Scope) {
	t.Helper()
	dsn := os.Getenv("PCAS_TEST_DATABASE_URL")
	u, err := url.Parse(dsn)
	if dsn == "" || err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		t.Fatal("requires an explicitly configured synthetic PCAS_TEST_DATABASE_URL; no skip")
	}
	host, database := u.Hostname(), strings.TrimPrefix(u.Path, "/")
	ip := net.ParseIP(host)
	if (host != "localhost" && (ip == nil || !ip.IsLoopback())) || strings.Contains(database, "/") || !(strings.HasSuffix(database, "_test") || strings.HasPrefix(database, "phase2_")) {
		t.Fatal("test database must use loopback and an explicit _test suffix or phase2_ prefix")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := admin.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	var encoding string
	if err := admin.QueryRow(ctx, "SHOW server_encoding").Scan(&encoding); err != nil || encoding != "UTF8" {
		t.Fatalf("synthetic database must be UTF8: %q %v", encoding, err)
	}
	var vector bool
	if err := admin.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_extension WHERE extname='vector')").Scan(&vector); err != nil || !vector {
		t.Fatalf("dedicated database requires existing vector extension: %t %v", vector, err)
	}
	schema := "phase2_siwc_store_" + strings.ReplaceAll(string(memory.NewID()), "-", "")
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec(context.Background(), "DROP SCHEMA "+quoted+" CASCADE"); err != nil {
			t.Error(err)
		}
	})
	q := u.Query()
	q.Set("search_path", schema+",public")
	u.RawQuery = q.Encode()
	store, err := postgres.Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	t.Logf("synthetic_database=%s schema=%s", database, schema)
	return store, memory.Scope{OwnerID: memory.NewID(), PrincipalID: "owner", IsOwner: true}
}

func phase2StoreExecute(t *testing.T, store *postgres.Store, scope memory.Scope, command workspace.Command) workspace.State {
	t.Helper()
	state, err := store.Snapshot(context.Background(), scope)
	if err != nil {
		t.Fatal(err)
	}
	command.RequestID, command.ExpectedRevision = string(memory.NewID()), state.Revision
	state, err = store.Execute(context.Background(), scope, command)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func phase2StoreIndependentClaim(t *testing.T, store *postgres.Store, scope memory.Scope) (memory.Ref, memory.Ref) {
	t.Helper()
	state := phase2StoreExecute(t, store, scope, workspace.Command{Type: "capture", Text: "海棠合成资料原文：" + phase2StoreRawMarker})
	if len(state.Candidates) != 1 {
		t.Fatalf("capture positive control missing: %+v", state.Candidates)
	}
	candidate := state.Candidates[0]
	source := memory.Ref{ID: memory.ID(candidate.Source.SourceID), Version: candidate.Source.Version, Kind: memory.SourceKind}
	state = phase2StoreExecute(t, store, scope, workspace.Command{Type: "acceptCandidate", ID: candidate.ID, Kind: "memory", MemoryKind: "preference", Text: phase2StoreClaim})
	for _, m := range state.Memories {
		if m.Text == phase2StoreClaim {
			return source, memory.Ref{ID: memory.ID(m.ID), Version: m.Version, Kind: memory.ClaimKind}
		}
	}
	t.Fatal("accepted independent claim positive control missing")
	return memory.Ref{}, memory.Ref{}
}

func phase2StoreEvidence(t *testing.T, name string, value any) {
	t.Helper()
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("phase2-siwc-store-%s=%s", name, body)
	if dir := os.Getenv("PCAS_PHASE2_SIWC_EVIDENCE_DIR"); dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name+".json"), append(body, '\n'), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPhase2SIWCUnknownModelRealStore(t *testing.T) {
	for _, scenario := range []string{"ordinary_question", "independent_claim", "raw_source_refused"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			store, scope := phase2StoreDatabase(t)
			used := []string{}
			if scenario == "independent_claim" {
				used = []string{"M1"}
			}
			wantReply := "合成SIWC Store回答：" + scenario
			reply, err := json.Marshal(map[string]any{"reply": wantReply, "used": used, "links": []string{}, "show": []string{}, "remember": false, "actions": []any{}, "ask": nil})
			if err != nil {
				t.Fatal(err)
			}
			manager, fixture := siwc.NewPhase2StoreFixture(t, string(reply))
			var mu sync.Mutex
			var events []memory.ContextRequestEvent
			registry := &ai.Registry{ChatGPT: manager, Config: ai.Configuration{Providers: []ai.Provider{{ID: phase2StoreProvider, Name: "合成SIWC接收者", Protocol: "siwc", BaseURL: fixture.Endpoint(), Model: "", MaxOutput: 200, CostMode: "free"}}}}
			registry.ContextObserver = memory.ContextRequestObserverFunc(func(_ context.Context, event memory.ContextRequestEvent) error {
				event.Payload = append([]byte(nil), event.Payload...)
				mu.Lock()
				events = append(events, event)
				mu.Unlock()
				return nil
			})
			store.SetModels(registry)
			status, err := manager.Status(ctx)
			if err != nil || len(status.Accounts) != 1 || status.Accounts[0].Model != "" {
				t.Fatalf("unknown account-model precondition missing: %+v %v", status, err)
			}
			if provider, ok := registry.Get(phase2StoreProvider); !ok || provider.Model != "" {
				t.Fatalf("unknown configured-model precondition missing: %+v", provider)
			}
			if !manager.Available() {
				t.Fatal("synthetic SIWC login is not available")
			}
			if _, err := store.Snapshot(ctx, scope); err != nil {
				t.Fatal(err)
			}
			var source, claim memory.Ref
			question := "用一句中文回应这次合成问答"
			if scenario == "independent_claim" {
				source, claim = phase2StoreIndependentClaim(t, store, scope)
				question = "海棠邀请的措辞偏好是什么？"
			}
			if scenario == "raw_source_refused" {
				ingested, err := memory.NewService(store).Ingest(ctx, scope, memory.IngestRequest{Connector: "phase2-siwc-synthetic", ExternalID: "ha tang source", ExternalVersion: "v1", Title: "海棠合成预约资料", Text: "海棠合成预约的原始安排：" + phase2StoreRawMarker, MediaType: "text/plain"})
				if err != nil {
					t.Fatal(err)
				}
				source = ingested.Ref
				if _, err := store.ContextRecipient(ctx, scope, phase2StoreProvider, "secretary", nil); !errors.Is(err, memory.ErrUnavailable) {
					t.Fatalf("unknown model unexpectedly gave an authorizable recipient: %v", err)
				}
				for _, guessedModel := range []string{"", "first"} {
					selection := memory.Recipient{PrincipalID: phase2StoreProvider, Role: "secretary", Provider: phase2StoreProvider, Model: guessedModel}
					_, err := store.SetSourceAuthorization(ctx, scope, memory.SourceAuthorizationRequest{RequestID: string(memory.NewID()), Source: source, Recipient: selection, Purpose: memory.KnowledgePurpose, Scope: memory.HardScope{Kind: memory.UnscopedContextScope}})
					if !errors.Is(err, memory.ErrUnavailable) {
						t.Fatalf("client model=%q changed unknown route authorization: %v", guessedModel, err)
					}
				}
				if len(fixture.Bodies()) != 0 {
					t.Fatal("authorization unexpectedly generated a model request")
				}
				question = "海棠合成预约资料中有哪些原始安排？"
			}
			if source.ID.Valid() {
				policies, err := store.SourceAuthorizations(ctx, scope, source.ID)
				if err != nil || len(policies) != 0 {
					t.Fatalf("raw source positive boundary missing: policies=%+v error=%v", policies, err)
				}
			}
			request := workspace.DeskTurnRequest{RequestID: string(memory.NewID()), AgentID: phase2StoreProvider, Text: question}
			out, err := store.DeskTurn(ctx, scope, request)
			if err != nil || out.Turn.Reply != wantReply || out.Turn.Text != question {
				t.Fatalf("real Store DeskTurn did not complete ordinary/claim-only inference: %+v %v", out.Turn, err)
			}
			bodies := fixture.Bodies()
			if len(bodies) != 1 {
				t.Fatalf("real fakeSIWC inference count=%d want=1", len(bodies))
			}
			payload := bodies[0]
			var wire struct {
				Model string `json:"model"`
				Input []struct {
					Content string `json:"content"`
				} `json:"input"`
			}
			if err := json.Unmarshal(payload, &wire); err != nil || wire.Model != "first" {
				t.Fatalf("actual adapter did not choose catalog model first: %s %v", payload, err)
			}
			if len(wire.Input) != 1 || !strings.Contains(wire.Input[0].Content, question) {
				t.Fatal("actual provider body lacks the original question")
			}
			if bytes.Contains(payload, []byte(phase2StoreRawMarker)) {
				t.Fatal("unknown configured model supplied raw source text")
			}
			if scenario == "independent_claim" && !strings.Contains(wire.Input[0].Content, phase2StoreClaim) {
				t.Fatal("independently accepted claim did not reach actual fakeSIWC")
			}
			mu.Lock()
			observed := append([]memory.ContextRequestEvent(nil), events...)
			mu.Unlock()
			var stages []string
			for _, event := range observed {
				stages = append(stages, event.Stage)
				if event.Model != "first" || event.Protocol != "siwc" || event.ProviderID != phase2StoreProvider || event.Endpoint != fixture.Endpoint() || event.ObservationLayer != "serialized_request" {
					t.Fatalf("observed final transport binding differs: %+v", event)
				}
				if !bytes.Equal(event.Payload, payload) {
					t.Fatal("observed serialized bytes differ from real fakeSIWC HTTP request")
				}
			}
			if !reflect.DeepEqual(stages, []string{"prepared", "before_dispatch", "dispatched"}) {
				t.Fatalf("actual chained observer stages=%v", stages)
			}
			attempts, err := store.ContextAttempts(ctx, scope, request.RequestID)
			if err != nil || len(attempts) != 1 {
				t.Fatalf("real Store attempt absent: %+v %v", attempts, err)
			}
			attempt := attempts[0]
			binding := attempt.Manifest.Recipient
			if attempt.State != memory.AttemptCompleted || attempt.SnapshotState != memory.SnapshotRetained || attempt.DispatchedAt == nil || attempt.CompletedAt == nil || attempt.ExternalReceipt != "unknown" {
				t.Fatalf("Store completion trace incorrect: %+v", attempt)
			}
			if !binding.Valid() || binding.Model != "first" || binding.Provider != phase2StoreProvider || binding.Protocol != "siwc" || binding.Role != "secretary" || binding.PrincipalID != phase2StoreProvider {
				t.Fatalf("Store retained unresolved model or incorrect recipient: %+v", binding)
			}
			snapshot, err := store.ContextAttemptSnapshot(ctx, scope, attempt.ID)
			if err != nil || !bytes.Equal(snapshot, payload) {
				t.Fatalf("real retained Store snapshot differs from actual HTTP request: bytes=%d error=%v", len(snapshot), err)
			}
			if attempt.Manifest.ObservationLayer != "serialized_request" || attempt.Manifest.InputBytes != len(payload) {
				t.Fatal("Store manifest does not describe actual serialized request")
			}
			for _, input := range attempt.Manifest.Input {
				if input.Ref.Kind == memory.SourceKind {
					t.Fatalf("unknown route has raw source in actual manifest input: %+v", input)
				}
			}
			if scenario == "independent_claim" {
				if len(attempt.Manifest.Input) != 1 || attempt.Manifest.Input[0].Ref != claim {
					t.Fatalf("independent exact claim input absent: %+v want=%+v", attempt.Manifest.Input, claim)
				}
				if len(attempt.Manifest.Used) != 1 || attempt.Manifest.Used[0].Ref != claim || attempt.Manifest.Used[0].Validation != "supported" {
					t.Fatalf("fake exact claim Used is not product-validated: %+v", attempt.Manifest.Used)
				}
			} else if len(attempt.Manifest.Input) != 0 || len(attempt.Manifest.Used) != 0 {
				t.Fatalf("ordinary/raw-refused inference falsely lists content input/used: %+v %+v", attempt.Manifest.Input, attempt.Manifest.Used)
			}
			hash := sha256.Sum256(payload)
			phase2StoreEvidence(t, scenario, map[string]any{"configured_model": "", "account_model": status.Accounts[0].Model, "adapter_model": wire.Model, "source": source, "claim": claim, "request": request, "reply": out.Turn.Reply, "actual_http_body": json.RawMessage(payload), "actual_http_bytes": len(payload), "actual_http_sha256": hex.EncodeToString(hash[:]), "events": observed, "attempt": attempt, "exact_snapshot_equal": bytes.Equal(snapshot, payload), "raw_source_absent": !bytes.Contains(payload, []byte(phase2StoreRawMarker)), "third_party_internal_context": "unknown"})
		})
	}
}
