package modelcall

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/prompts"
)

type generationFixture struct {
	provider        ai.Provider
	called          ai.Provider
	prepared        Request
	mode            ai.GenerationMode
	paid            *PaidResult
	failAt          string
	capabilityErr   error
	ordinaryOnly    bool
	failure         error
	generationError error
	afterReserve    func()
	calls           int
	reservations    int
	records         int
	settlements     int
	estimate        float64
}

func (f *generationFixture) Get(id string) (ai.Provider, bool) {
	return f.provider, id == f.provider.ID
}
func (f *generationFixture) ExtractionID() string  { return "default" }
func (f *generationFixture) Available(string) bool { return true }
func (f *generationFixture) CheckGeneration(_ ai.Provider, mode ai.GenerationMode) error {
	f.mode = mode
	if f.ordinaryOnly {
		if len(mode.Schema) > 0 {
			return &ai.CapabilityError{Capability: "output_schema"}
		}
		if mode.Search {
			return &ai.CapabilityError{Capability: "web_search"}
		}
	}
	return f.capabilityErr
}
func (f *generationFixture) GenerateProvider(_ context.Context, p ai.Provider, _, _ string, modes ...ai.GenerationMode) (ai.Result, error) {
	f.calls++
	f.called = p
	f.mode = modes[0]
	result := ai.Result{Text: "Fictitious reply.", InputTokens: 4, OutputTokens: 3, Cost: 0.00001}
	if f.mode.Search {
		result.Searches = []string{"Fictitious public query."}
	}
	return result, f.generationError
}
func (f *generationFixture) Reserve(_ context.Context, _ Request, estimate float64) (Reservation, error) {
	f.reservations++
	f.estimate = estimate
	if f.afterReserve != nil {
		f.afterReserve()
	}
	return Reservation{ID: "reservation", Cost: estimate}, nil
}
func (f *generationFixture) Record(context.Context, Request, *PaidResult) error {
	f.records++
	return f.fail("record")
}
func (f *generationFixture) Settle(context.Context, Request, *PaidResult) error {
	f.settlements++
	return f.fail("settle")
}
func (f *generationFixture) Load(context.Context, Request) (*PaidResult, error) { return f.paid, nil }
func (f *generationFixture) Save(_ context.Context, _ Request, paid *PaidResult) error {
	f.paid = paid
	return f.fail("save")
}
func (f *generationFixture) Forget(context.Context, Request, *PaidResult) error {
	f.paid = nil
	return nil
}
func (f *generationFixture) Prepare(_ context.Context, request Request, _ ai.Provider) (memory.ID, error) {
	f.prepared = request
	return "invocation", nil
}
func (f *generationFixture) Start(context.Context, Request, *PaidResult) error { return nil }
func (f *generationFixture) PreparationFailed(_ context.Context, _ Request, _ memory.ID, err error) error {
	f.failure = err
	return f.fail("journal")
}
func (f *generationFixture) AccountingState(context.Context, Request, *PaidResult, string) error {
	return nil
}
func (f *generationFixture) fail(stage string) error {
	if f.failAt == stage {
		f.failAt = ""
		return errors.New("fictitious persistence failure")
	}
	return nil
}
func generationRequest() Request {
	return Request{OwnerID: "owner", ExecutionID: "execution", RootExecutionID: "execution", Function: "fictitious", Stage: "answer", Instructions: prompts.Must("organize"), Prompt: []byte(`{"rawPrompt":"Fictitious input."}`)}
}

func TestGatewayPinsExplicitProviderBeforeReservation(t *testing.T) {
	fixture := &generationFixture{provider: ai.Provider{ID: "selected", Model: "selected-model", MaxOutput: 100, InputPerMillion: 1, OutputPerMillion: 2}}
	original := fixture.provider
	fixture.afterReserve = func() { fixture.provider.Model = "replacement-model"; fixture.provider.InputPerMillion = 100 }
	request := generationRequest()
	request.ProviderID, request.Search, request.ContextBuilderVersion = "selected", true, "fictitious-context-v1"
	request.Schema = prompts.MustSchema("use-groups")
	paid, err := New(fixture, fixture, fixture, fixture).Call(context.Background(), request)
	if err != nil || paid.Provider != "selected" || paid.Model != original.Model || fixture.called != original || fixture.reservations != 1 || fixture.calls != 1 {
		t.Fatalf("paid=%+v fixture=%+v error=%v", paid, fixture, err)
	}
	if fixture.estimate != original.Reserve(request.Instructions.Text()+"Fictitious input.") || !fixture.mode.Search || !reflect.DeepEqual(fixture.mode.Schema, request.Schema.Bytes()) || !reflect.DeepEqual(paid.Searches, []string{"Fictitious public query."}) {
		t.Fatal("reservation or invocation did not retain the selected input contract")
	}
	if fixture.prepared.ContextBuilderVersion != request.ContextBuilderVersion || !reflect.DeepEqual(request.RequiredCapabilities(), []string{"text_generation", "output_schema", "web_search"}) {
		t.Fatal("journal request lost required capabilities or context version")
	}
}

func TestUnsupportedGatewayModeIsRecordedWithoutReservation(t *testing.T) {
	for _, failJournal := range []bool{false, true} {
		fixture := &generationFixture{provider: ai.Provider{ID: "default"}, capabilityErr: &ai.CapabilityError{Capability: "output_schema"}}
		if failJournal {
			fixture.failAt = "journal"
		}
		request := generationRequest()
		request.Schema = prompts.MustSchema("use-groups")
		paid, err := New(fixture, fixture, fixture, fixture).Call(context.Background(), request)
		if paid != nil || !errors.Is(err, ai.ErrUnsupportedCapability) || !errors.Is(fixture.failure, ai.ErrUnsupportedCapability) || fixture.reservations != 0 || fixture.calls != 0 || fixture.records != 0 || fixture.settlements != 0 {
			t.Fatalf("paid=%+v fixture=%+v error=%v", paid, fixture, err)
		}
	}
}

func TestPersistenceRecoveryUsesPaidResultWithoutAProvider(t *testing.T) {
	for _, stage := range []string{"save", "record", "settle"} {
		t.Run(stage, func(t *testing.T) {
			fixture := &generationFixture{provider: ai.Provider{ID: "default", Model: "fictitious-model"}, failAt: stage}
			request := generationRequest()
			if _, err := New(fixture, fixture, fixture, fixture).Call(context.Background(), request); err == nil {
				t.Fatal("persistence failure was hidden")
			}
			paid, err := New(nil, fixture, fixture, fixture).Call(context.Background(), request)
			if err != nil || paid == nil || paid.Output != "Fictitious reply." || paid.InvocationID != "invocation" || paid.Reservation != "reservation" || fixture.calls != 1 || fixture.reservations != 1 {
				t.Fatalf("paid=%+v fixture=%+v error=%v", paid, fixture, err)
			}
		})
	}
}

func TestBackgroundDefaultKeepsOrdinaryGenerationMode(t *testing.T) {
	fixture := &generationFixture{provider: ai.Provider{ID: "default", Model: "fictitious-model"}}
	request := generationRequest()
	paid, err := New(fixture, fixture, fixture, fixture).Call(context.Background(), request)
	if err != nil || paid.Provider != "default" || fixture.mode.Search || fixture.mode.Schema != nil || !reflect.DeepEqual(request.RequiredCapabilities(), []string{"text_generation"}) {
		t.Fatalf("paid=%+v mode=%+v error=%v", paid, fixture.mode, err)
	}
}

func TestModeFallbackSubmitsOrdinaryGenerationAndNamesMissingCapabilities(t *testing.T) {
	fixture := &generationFixture{provider: ai.Provider{ID: "default", Model: "fictitious-model"}, ordinaryOnly: true}
	request := generationRequest()
	request.Schema, request.Search, request.ModeFallback = prompts.MustSchema("use-groups"), true, true
	paid, err := New(fixture, fixture, fixture, fixture).Call(context.Background(), request)
	if err != nil || paid == nil || fixture.calls != 1 || fixture.mode.Search || len(fixture.mode.Schema) != 0 {
		t.Fatalf("paid=%+v mode=%+v error=%v", paid, fixture.mode, err)
	}
	if len(fixture.prepared.Unsupported) != 2 || fixture.prepared.Unsupported[0] != "output_schema" || fixture.prepared.Unsupported[1] != "web_search" || fixture.prepared.Schema.Name() != "use-groups" {
		t.Fatal("the journal request did not name the missing capabilities", fixture.prepared.Unsupported)
	}
	// The same provider without the caller's declaration fails before submission.
	strict := &generationFixture{provider: ai.Provider{ID: "default", Model: "fictitious-model"}, ordinaryOnly: true}
	request.ModeFallback = false
	if _, err := New(strict, strict, strict, strict).Call(context.Background(), request); !errors.Is(err, ai.ErrUnsupportedCapability) || strict.calls != 0 {
		t.Fatal(err, strict.calls)
	}
}
