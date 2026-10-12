package retrieval

import (
	"context"
	"errors"
	"testing"

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/worker"
)

type fakeProviders struct {
	id        string
	known     bool
	available bool
}

func (f fakeProviders) EmbeddingID() string { return f.id }
func (f fakeProviders) Get(string) (ai.Provider, bool) {
	return ai.Provider{ID: f.id, Model: "m"}, f.known
}
func (f fakeProviders) Available(string) bool { return f.available }

type fakeIndex struct{ model string }

func (f *fakeIndex) Missing(_ context.Context, _ worker.Job, model string) ([]memory.Ref, []string, bool, error) {
	f.model = model
	return nil, nil, false, nil
}
func (f *fakeIndex) Save(context.Context, worker.Job, []memory.Ref, []memory.Embedding) error {
	panic("nothing was embedded")
}

func TestEmbeddingJobWithoutAUsableProviderReadsNothing(t *testing.T) {
	for code, providers := range map[string]Providers{
		"provider_not_configured": nil,
		"provider_unavailable":    fakeProviders{id: "e", known: true},
	} {
		store := &fakeIndex{}
		err := Indexer{Store: store, Providers: providers}.Process(context.Background(), worker.Job{})
		var failure *worker.JobError
		if !errors.As(err, &failure) || failure.Code != code || !errors.Is(err, memory.ErrUnavailable) || failure.Retry != (code == "provider_unavailable") || store.model != "" {
			t.Fatal(code, err, store.model)
		}
	}
	for _, providers := range []Providers{fakeProviders{}, fakeProviders{id: "e"}} {
		err := Indexer{Store: &fakeIndex{}, Providers: providers}.Process(context.Background(), worker.Job{})
		var failure *worker.JobError
		if !errors.As(err, &failure) || failure.Code != "provider_not_configured" {
			t.Fatal(err)
		}
	}
}

func TestEmbeddingJobWithEveryVectorPresentMakesNoCall(t *testing.T) {
	store := &fakeIndex{}
	// The gateway is absent: a call would fail the test.
	if err := (Indexer{Store: store, Providers: fakeProviders{id: "e", known: true, available: true}}).Process(context.Background(), worker.Job{}); err != nil || store.model != "e:m" {
		t.Fatal(err, store.model)
	}
}
