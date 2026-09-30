package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/soaringjerry/PCAS/internal/workspace"
)

func secretaryLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var logs bytes.Buffer
	prior := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prior) })
	return &logs
}

func assertSecretaryLog(t *testing.T, logs *bytes.Buffer, level, stage, errorType string) {
	t.Helper()
	found := false
	for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatal(err, logs.String())
		}
		// No request IDs, questions, prompts, model text or raw errors may be
		// attached: only standard logging metadata and the two categories.
		if len(record) != 5 {
			t.Fatal("unexpected logging fields", record)
		}
		for _, key := range []string{"time", "level", "msg", "stage", "error_type"} {
			if _, ok := record[key]; !ok {
				t.Fatal("missing logging field", key, record)
			}
		}
		if record["level"] == level && record["stage"] == stage && record["error_type"] == errorType {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing %s %s %s: %s", level, stage, errorType, logs.String())
	}
}

func TestSecretaryModelOutputRecovery(t *testing.T) {
	const valid = `{"reply":"安排好了","actions":[{"op":"create_task","title":"买牛奶","extra":"action-private"}],"extra":"model-private"}`
	for _, tt := range []struct {
		name, text, level, stage, errorType, reply string
		tasks, receipts                            int
	}{
		{"extra fields", valid, "INFO", "parse", "none", "安排好了", 1, 1},
		{"preface and suffix", "先说一句 model-private。\n" + valid + "\n好了。", "INFO", "parse", "none", "安排好了", 1, 1},
		{"markdown", "```json\n" + valid + "\n```", "INFO", "parse", "none", "安排好了", 1, 1},
		{"plain text", "我没有实时天气记录。model-private", "WARN", "parse", "no_json_object", "我没有实时天气记录。model-private", 0, 0},
		{"500", "model-private", "WARN", "model", "model_error", "", 0, 1},
		{"invalid action fields", `{"reply":"安排好了","actions":[{"op":"create_task","title":42,"extra":"model-private"},null,7,{"op":"create_task","title":""},{"op":"create_task","title":"买牛奶"}]}`, "WARN", "parse", "invalid_field_type", "安排好了", 1, 5},
		{"invalid reply type", `{"reply":42,"actions":[{"op":"create_task","title":"不该执行"}]}`, "WARN", "parse", "invalid_field_type", `{"reply":42,"actions":[{"op":"create_task","title":"不该执行"}]}`, 0, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := testStore(t)
			logs := secretaryLogs(t)
			scope := owner()
			secretaryModel(t, s, func(w http.ResponseWriter, r *http.Request) {
				if tt.name == "500" {
					http.Error(w, tt.text, http.StatusInternalServerError)
					return
				}
				secretaryModelReply(w, tt.text)
			})
			req := turnRequest("原话 user-private")
			out := mustTurn(t, s, scope, req)
			if out.Turn.Reply != tt.reply || len(out.State.Tasks) != tt.tasks || len(out.Turn.Receipts) != tt.receipts || len(out.Turn.Cards) != 0 {
				t.Fatal(out)
			}
			connector := "desk"
			if tt.name == "500" {
				connector = "capture"
				if r := out.Turn.Receipts[0]; r.Op != "capture" || r.Status != "done" || r.Undoable || r.Text != "已记下原话；模型没有响应，稍后会自动整理" {
					t.Fatal(r)
				}
			} else if tt.tasks > 0 {
				last := out.Turn.Receipts[len(out.Turn.Receipts)-1]
				if last.Op != "create_task" || last.Status != "done" || !last.Undoable || last.ActionID == nil {
					t.Fatal(last)
				}
				for _, receipt := range out.Turn.Receipts[:len(out.Turn.Receipts)-1] {
					if receipt.Status != "skipped" || receipt.ActionID != nil || receipt.Undoable {
						t.Fatal(receipt)
					}
				}
			}
			var saved string
			if err := s.pool.QueryRow(context.Background(), `SELECT v.body FROM source_versions v JOIN sources s ON (s.owner_id,s.id)=(v.owner_id,v.source_id) WHERE s.owner_id=$1 AND s.connector=$2 AND s.external_id=$3`, string(scope.OwnerID), connector, req.RequestID).Scan(&saved); err != nil || saved != req.Text {
				t.Fatal("original not saved", err, saved)
			}
			assertSecretaryLog(t, logs, tt.level, tt.stage, tt.errorType)
			if strings.Contains(logs.String(), "private") || strings.Contains(logs.String(), "不该执行") {
				t.Fatal("private contents in logs", logs.String())
			}
			before := logs.String()
			replay := mustTurn(t, s, scope, req)
			if !bytes.Equal(asJSON(out.Turn), asJSON(replay.Turn)) || logs.String() != before || len(replay.State.Tasks) != tt.tasks {
				t.Fatal("replayed model/action or changed saved reply", replay)
			}
		})
	}
}

func TestParseSecretaryFirstObjectAndQuotedBraces(t *testing.T) {
	for _, text := range []string{
		`前言 {不是 JSON} {"reply":"括号 } { 和引号 \"","actions":[{"op":"create_task","title":"第一个"}]} 后文 {"actions":[{"op":"create_task","title":"不执行第二个"}]}`,
		"说明\n```JSON\n" + `{"reply":"括号 } { 和引号 \"","actions":[{"op":"create_task","title":"第一个"}]}` + "\n```\n后文",
	} {
		out, err := parseSecretaryOutput(text)
		if err != nil || out.Reply != "括号 } { 和引号 \"" || len(out.Actions) != 1 || out.Actions[0].Title != "第一个" {
			t.Fatal(err, out)
		}
	}
	// Invalid fields in the first object cannot be replaced by a later object.
	out, err := parseSecretaryOutput(`{"reply":42} {"actions":[{"op":"create_task","title":"不能执行"}]}`)
	if err == nil || len(out.Actions) != 0 {
		t.Fatal(err, out)
	}
}

func TestSecretaryCaptureReasonForBudgetInfrastructure(t *testing.T) {
	if secretaryCaptureText("budget", context.DeadlineExceeded) != "已记下原话；暂时无法检查额度，稍后会自动整理" {
		t.Fatal("budget infrastructure timeout mislabeled as quota exhaustion")
	}
	if secretaryErrorType("budget", workspace.ErrBudget) != "budget_exceeded" {
		t.Fatal("budget error not categorized")
	}
}
