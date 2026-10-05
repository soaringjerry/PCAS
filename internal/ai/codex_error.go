package ai

import (
	"encoding/json"
	"fmt"
	"strings"
)

// CodexError contains only bounded protocol metadata. Provider messages and
// additionalDetails can contain prompts or credentials and are never retained.
type CodexError struct {
	Operation  string
	Category   string
	RPCCode    int
	HTTPStatus int
	Cause      error
}

func (e *CodexError) Error() string {
	return fmt.Sprintf("Codex %s: %s (rpc=%d http=%d)", e.Operation, e.Category, e.RPCCode, e.HTTPStatus)
}

func (e *CodexError) Unwrap() error { return e.Cause }

type codexTurnError struct {
	Info    json.RawMessage `json:"codexErrorInfo"`
	Message string          `json:"message"`
}

func (e *codexTurnError) safeError(operation string) *CodexError {
	out := &CodexError{Operation: operation, Category: "unknown"}
	if e == nil {
		return out
	}
	var name string
	if json.Unmarshal(e.Info, &name) == nil {
		out.Category = codexErrorCategory(name)
	} else {
		var info map[string]struct {
			Status int `json:"httpStatusCode"`
		}
		if json.Unmarshal(e.Info, &info) == nil && len(info) == 1 {
			for name, detail := range info {
				out.Category = codexErrorCategory(name)
				if out.Category != "unknown" && detail.Status >= 100 && detail.Status <= 599 {
					out.HTTPStatus = detail.Status
				}
			}
		}
	}
	// Some CLI versions report authentication failures as "other". Classify
	// known machine codes/phrases without copying any part of their message.
	if out.Category != "other" && out.Category != "unknown" && out.Category != "unauthorized" {
		return out
	}
	message := strings.ToLower(e.Message)
	for _, auth := range []struct{ code, phrase string }{
		{"refresh_token_reused", "refresh token has already been used"},
		{"refresh_token_expired", "refresh token has expired"},
		{"refresh_token_invalidated", "refresh token has been invalidated"},
	} {
		if strings.Contains(message, auth.code) || strings.Contains(message, auth.phrase) {
			out.Category = auth.code
			break
		}
	}
	return out
}

func codexErrorCategory(name string) string {
	// This allowlist follows the installed app-server's CodexErrorInfo schema.
	// Future variants stay unknown rather than allowing arbitrary text in logs.
	switch name {
	case "contextWindowExceeded", "sessionBudgetExceeded", "usageLimitExceeded",
		"rateLimitExceeded", "flexUnavailable", "serverOverloaded", "cyberPolicy",
		"misalignmentPolicyViolation", "tooManyDenials", "internalServerError",
		"unauthorized", "badRequest", "threadRollbackFailed", "sandboxError", "other",
		"httpConnectionFailed", "responseStreamConnectionFailed", "responseStreamDisconnected",
		"responseTooManyFailedAttempts", "activeTurnNotSteerable":
		return name
	default:
		return "unknown"
	}
}
