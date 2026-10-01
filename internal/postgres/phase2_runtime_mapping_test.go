package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

func TestPhase2RuntimeSourceMappingExcludesIdenticalQuestionBytes(t *testing.T) {
	for _, authorized := range []bool{false, true} {
		label := "absence"
		if authorized {
			label = "authorized"
		}
		t.Run(label, func(t *testing.T) {
			s, scope, capture := phase2RTSetup(t)
			var sequence struct {
				Mapping struct {
					Text     string `json:"source_text"`
					Question string `json:"question_with_duplicate"`
				} `json:"request_mapping_controls"`
			}
			phase2RTReadJSON(t, "runtime-sequences.json", &sequence)
			source := mustIngest(t, s, scope, memory.IngestRequest{Connector: "phase2-mapping-synthetic", ExternalID: "mapping", ExternalVersion: "v1", Title: "来源映射资料", Text: sequence.Mapping.Text, MediaType: "text/plain"})
			if authorized {
				phase2RTAuthorize(t, s, scope, source.Ref, "phase2-model", "secretary", phase2RTUnscoped())
			}
			requestID := string(memory.NewID())
			question := "《来源映射资料》：" + sequence.Mapping.Question
			_, err := s.DeskTurn(context.Background(), scope, workspace.DeskTurnRequest{RequestID: requestID, AgentID: "phase2-model", Text: question})
			if err != nil {
				t.Fatal(err)
			}
			if capture.count() != 1 {
				t.Fatal("mapping control requires exactly one actual request")
			}
			payload := capture.request(t, 0)
			encodedQuestion, err := json.Marshal(question)
			if err != nil {
				t.Fatal(err)
			}
			needle := encodedQuestion[1 : len(encodedQuestion)-1]
			var questionSpans []memory.PayloadSpan
			for offset := 0; offset < len(payload); {
				i := bytes.Index(payload[offset:], needle)
				if i < 0 {
					break
				}
				i += offset
				questionSpans = append(questionSpans, memory.PayloadSpan{StartByte: i, EndByte: i + len(needle)})
				offset = i + len(needle)
			}
			if len(questionSpans) == 0 {
				t.Fatal("actual question literal absent; identical user-text control not established")
			}
			diagnostics, ok := any(s).(phase2RTDiagnostics)
			if !ok {
				t.Fatal("actual diagnostic API missing")
			}
			attempts, err := diagnostics.ContextAttempts(context.Background(), scope, requestID)
			if err != nil || len(attempts) != 1 {
				t.Fatalf("real mapping attempt rows=%d error=%v", len(attempts), err)
			}
			attempt := attempts[0]
			snapshot, err := diagnostics.ContextAttemptSnapshot(context.Background(), scope, attempt.ID)
			if err != nil || !bytes.Equal(payload, snapshot) {
				t.Error("mapping assertion was not backed by exact actual request snapshot")
			}
			inputs := 0
			for _, input := range attempt.Manifest.Input {
				if input.Ref != source.Ref {
					continue
				}
				inputs++
				if input.SourceSpan == nil || input.SourceSpan.Source != source.Ref || !input.SourceSpan.Valid() || input.SourceSpan.EndRune > len([]rune(sequence.Mapping.Text)) {
					t.Error("mapping source identity/rune interval invalid")
					continue
				}
				var mapped strings.Builder
				for _, span := range input.PayloadSpans {
					if span.StartByte < 0 || span.EndByte <= span.StartByte || span.EndByte > len(payload) {
						t.Error("final escaped byte interval invalid")
						continue
					}
					for _, user := range questionSpans {
						if span.StartByte < user.EndByte && user.StartByte < span.EndByte {
							t.Error("source supply map points into identical literal in user question")
						}
					}
					mapped.Write(payload[span.StartByte:span.EndByte])
				}
				literal := string([]rune(sequence.Mapping.Text)[input.SourceSpan.StartRune:input.SourceSpan.EndRune])
				encodedLiteral, err := json.Marshal(literal)
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(mapped.String(), string(encodedLiteral[1:len(encodedLiteral)-1])) {
					t.Error("source rune evidence and mapped Chinese/quote/backslash/newline bytes differ")
				}
			}
			if authorized && inputs == 0 {
				t.Error("lawfully authorized source evidence not mapped in actual input")
			}
			if !authorized && inputs != 0 {
				t.Error("matching user phrase falsely labeled source supply without source policy")
			}
			phase2RTEvidence(t, "mapping-origin", map[string]any{"authorized": authorized, "source": source.Ref, "question_spans": questionSpans, "source_input_records": inputs, "attempt": attempt, "input_observation": "exact synthetic HTTP receive bytes"})
		})
	}
}
