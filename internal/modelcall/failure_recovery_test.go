package modelcall

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/soaringjerry/PCAS/internal/ai"
)

func TestPaidFailureRetainsRetryClassificationAfterAccountingRecovery(t *testing.T) {
	for _, category := range []string{"serverOverloaded", "responseStreamDisconnected", "refresh_token_expired"} {
		t.Run(category, func(t *testing.T) {
			fixture := &generationFixture{provider: ai.Provider{ID: "default", Model: "fictitious-model"}, failAt: "record", generationError: &ai.CodexError{Operation: "turn/completed", Category: category, HTTPStatus: 503, Cause: errors.New("fictitious private provider text")}}
			request := generationRequest()
			if _, err := New(fixture, fixture, fixture, fixture).Call(context.Background(), request); err == nil {
				t.Fatal("accounting failure was hidden")
			}
			encoded, err := json.Marshal(fixture.paid)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(encoded), "private provider") {
				t.Fatal("diagnostic text reached durable receipt")
			}
			fixture.paid = &PaidResult{}
			if err := json.Unmarshal(encoded, fixture.paid); err != nil {
				t.Fatal(err)
			}
			_, err = New(nil, fixture, fixture, fixture).Call(context.Background(), request)
			var failure *Failure
			var detail *ai.CodexError
			if !errors.As(err, &failure) || !errors.As(err, &detail) || detail.Category != category || detail.HTTPStatus != 503 || detail.Operation != "turn/completed" || fixture.calls != 1 || failure.InvocationID != "invocation" {
				t.Fatal("saved failure lost identity or retry classification", err, fixture.calls)
			}
			if errors.Is(err, ErrOutcomeUnknown) != (category == "responseStreamDisconnected") {
				t.Fatal("unknown outcome sentinel changed", err)
			}
		})
	}
}

func TestPaidFailurePreservesCancellationWithoutRetainingProviderCause(t *testing.T) {
	fixture := &generationFixture{provider: ai.Provider{ID: "default"}, generationError: &ai.CodexError{Category: "serverOverloaded", Cause: context.DeadlineExceeded}}
	_, err := New(fixture, fixture, fixture, fixture).Call(context.Background(), generationRequest())
	if !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, ErrOutcomeUnknown) {
		t.Fatal("cancellation classification disappeared", err)
	}
	var detail *ai.CodexError
	if !errors.As(err, &detail) || detail.Category != "serverOverloaded" {
		t.Fatal(err)
	}
}

func TestInapplicablePaidResultFinishesAccountingBeforeRejection(t *testing.T) {
	fixture := &generationFixture{paid: &PaidResult{InvocationID: "previous", NotApplicable: true}}
	if paid, err := New(nil, fixture, fixture, fixture).Call(context.Background(), generationRequest()); paid != nil || !errors.Is(err, ErrNotApplicable) || fixture.records != 1 || fixture.settlements != 1 || fixture.calls != 0 {
		t.Fatal(paid, err, fixture)
	}
}
