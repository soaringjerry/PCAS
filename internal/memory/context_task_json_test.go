package memory_test

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func phase2TaskWireGold(t *testing.T) (json.RawMessage, []string, memory.TrustedTaskContext) {
	t.Helper()
	body, err := os.ReadFile("../../testdata/phase2/task-json-gold.json")
	if err != nil {
		t.Fatal(err)
	}
	var gold struct {
		Legacy json.RawMessage `json:"legacy_task"`
		Keys   []string        `json:"new_task_keys"`
	}
	if err := json.Unmarshal(body, &gold); err != nil || len(gold.Legacy) == 0 || len(gold.Keys) != 10 {
		t.Fatal("independent fixed legacy Task and exact new field gold missing", err)
	}
	validAt := time.Date(2026, 9, 30, 9, 8, 7, 0, time.UTC)
	knownAt := time.Date(2026, 10, 1, 1, 0, 0, 0, time.UTC)
	// Independent literal expectation: never generate the legacy fixture by
	// marshaling the implementation whose tags are under test.
	expected := memory.TrustedTaskContext{
		OwnerID:          memory.ID("11111111-1111-4111-8111-111111111111"),
		Recipient:        memory.Recipient{PrincipalID: "synthetic-deputy", Role: "deputy", Model: "synthetic-model", Provider: "local-synthetic", Protocol: "openai", Channel: "api", RouteFingerprint: "abababababababababababababababababababababababababababababababab"},
		Purpose:          memory.KnowledgePurpose,
		Scope:            memory.HardScope{Kind: memory.StudioContextScope, StudioID: memory.ID("22222222-2222-4222-8222-222222222222"), IncludeGlobalConstraints: true},
		View:             memory.VersionView{Mode: memory.History, ValidAt: &validAt, KnownAt: &knownAt},
		Now:              time.Date(2026, 10, 1, 1, 2, 3, 123456789, time.UTC),
		Timezone:         "Australia/Melbourne",
		MemoryBudget:     memory.Budget{Candidates: 37, Edges: 9, Tokens: 777, Hops: 2},
		TotalInputTokens: 9999,
		DeskActions:      []memory.ID{memory.ID("33333333-3333-4333-8333-333333333333"), memory.ID("44444444-4444-4444-8444-444444444444")},
	}
	return gold.Legacy, gold.Keys, expected
}

func TestPhase2PublicTrustedTaskJSONAndLegacyCompatibility(t *testing.T) {
	t.Run("public_run_lowerCamel", func(t *testing.T) {
		legacy, keys, expected := phase2TaskWireGold(t)
		body, err := json.Marshal(workspace.Run{ID: "synthetic-public-run", ContextTask: &expected})
		if err != nil {
			t.Fatal(err)
		}
		var run map[string]json.RawMessage
		if err := json.Unmarshal(body, &run); err != nil {
			t.Fatal(err)
		}
		var task map[string]json.RawMessage
		if err := json.Unmarshal(run["contextTask"], &task); err != nil || len(task) != len(keys) {
			t.Fatalf("actual workspace.Run contextTask missing or wrong field topology: %s %v", body, err)
		}
		for _, key := range keys {
			if _, ok := task[key]; !ok {
				t.Errorf("public Task missing exact frozen wire key %s: %s", key, run["contextTask"])
			}
		}
		if _, ok := task["Recipient"]; ok {
			t.Error("public Task leaked uppercase Recipient instead of UI's recipient")
		}
		var old map[string]json.RawMessage
		if err := json.Unmarshal(legacy, &old); err != nil {
			t.Fatal(err)
		}
		for newKey, oldKey := range map[string]string{"ownerId": "OwnerID", "recipient": "Recipient", "purpose": "Purpose", "scope": "Scope", "view": "View", "now": "Now", "timezone": "Timezone", "memoryBudget": "MemoryBudget", "totalInputTokens": "TotalInputTokens", "desk_actions": "desk_actions"} {
			var got, want any
			if err := json.Unmarshal(task[newKey], &got); err != nil {
				t.Errorf("public %s not decodable: %v", newKey, err)
				continue
			}
			if err := json.Unmarshal(old[oldKey], &want); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("public %s changed full legacy semantic payload or nested wire: got=%+v want=%+v", newKey, got, want)
			}
		}
		var recipient memory.Recipient
		if err := json.Unmarshal(task["recipient"], &recipient); err != nil || recipient != expected.Recipient || !recipient.Valid() {
			t.Fatalf("public lower-case recipient lost complete canonical route: %+v %v", recipient, err)
		}
	})
	t.Run("fixed_legacy_uppercase_decode", func(t *testing.T) {
		legacy, _, expected := phase2TaskWireGold(t)
		var got memory.TrustedTaskContext
		if err := json.Unmarshal(legacy, &got); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, expected) {
			t.Fatalf("legacy uppercase Task lost identity/route/scope/version view/time/budget/origins: got=%+v want=%+v", got, expected)
		}
	})
	t.Run("new_output_roundtrip", func(t *testing.T) {
		legacy, _, expected := phase2TaskWireGold(t)
		var original memory.TrustedTaskContext
		if err := json.Unmarshal(legacy, &original); err != nil {
			t.Fatal(err)
		}
		body, err := json.Marshal(original)
		if err != nil {
			t.Fatal(err)
		}
		var restored memory.TrustedTaskContext
		if err := json.Unmarshal(body, &restored); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(original, expected) || !reflect.DeepEqual(restored, expected) {
			t.Fatalf("new output did not preserve full trusted Task: body=%s original=%+v restored=%+v want=%+v", body, original, restored, expected)
		}
	})
}
