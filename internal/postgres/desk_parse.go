package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/workspace"
)

var errNoSecretaryObject = errors.New("no JSON object")

// Decode the first complete JSON object, allowing surrounding prose and fences.
// The decoder handles nested objects and braces/escapes inside JSON strings.
// Stop at that object even if its fields are invalid: later objects must not
// supply actions that were absent from the first response.
func parseSecretaryOutput(text string) (secretaryOutput, error) {
	for i := 0; i < len(text); i++ {
		if text[i] != '{' {
			continue
		}
		var object json.RawMessage
		if json.NewDecoder(strings.NewReader(text[i:])).Decode(&object) != nil {
			continue
		}
		var out secretaryOutput
		// Decode each action independently so a wrong field type cannot reject
		// the reply or other actions. Unknown fields are intentionally ignored.
		var raw struct {
			*secretaryOutput
			Actions []json.RawMessage `json:"actions"`
		}
		raw.secretaryOutput = &out
		if err := json.Unmarshal(object, &raw); err != nil {
			return secretaryOutput{}, err
		}
		for _, data := range raw.Actions {
			var action secretaryAction
			action.parseErr = json.Unmarshal(data, &action)
			if strings.TrimSpace(string(data)) == "null" {
				action.parseErr = memory.ErrInvalid
			}
			if action.parseErr != nil {
				// Retain only the op for the skipped receipt, never partially
				// decoded executable fields.
				var header struct {
					Op string `json:"op"`
				}
				_ = json.Unmarshal(data, &header)
				action = secretaryAction{Op: header.Op, parseErr: action.parseErr}
			}
			out.Actions = append(out.Actions, action)
		}
		return out, nil
	}
	return secretaryOutput{}, errNoSecretaryObject
}

// Only fixed categories reach logs. Error messages can contain private text
// (including JSON field values), provider bodies or database details.
func secretaryErrorType(stage string, err error) string {
	var fieldErr *json.UnmarshalTypeError
	var syntaxErr *json.SyntaxError
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, workspace.ErrBudget), stage == "budget" && errors.Is(err, memory.ErrUnavailable):
		return "budget_exceeded"
	case errors.Is(err, memory.ErrRecordCapacity):
		return "record_capacity"
	case errors.Is(err, memory.ErrUnavailable):
		return "unavailable"
	case errors.Is(err, memory.ErrConflict):
		return "conflict"
	case errors.Is(err, errNoSecretaryObject):
		return "no_json_object"
	case errors.As(err, &fieldErr):
		return "invalid_field_type"
	case errors.As(err, &syntaxErr):
		return "invalid_json"
	case errors.Is(err, memory.ErrInvalid):
		return "invalid_input"
	default:
		return stage + "_error"
	}
}

func secretaryCaptureText(stage string, err error) string {
	if errors.Is(err, memory.ErrRecordCapacity) {
		return "已记下原话；这轮暂时无法发送，请缩短问题或稍后再试"
	}
	if errors.Is(err, memory.ErrConflict) {
		return "已记下原话；上下文已变更，请重试"
	}
	reason := "模型没有响应"
	switch stage {
	case "context":
		if errors.Is(err, memory.ErrUnavailable) {
			reason = "没有可用的模型"
		} else {
			reason = "暂时无法读取上下文"
		}
	case "budget":
		if secretaryErrorType(stage, err) == "budget_exceeded" {
			reason = "超过今天的额度"
		} else {
			reason = "暂时无法检查额度"
		}
	case "model":
		if errors.Is(err, context.DeadlineExceeded) {
			reason = "模型响应超时"
		}
	case "verify":
		reason = "上下文已变更，请重试"
	}
	return "已记下原话；" + reason + "，稍后会自动整理"
}
