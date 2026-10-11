package handover

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/prompts"
	"github.com/soaringjerry/PCAS/internal/worker"
)

func sections(edit func(i int, s *Section)) string {
	out := Output{}
	for i, title := range Titles {
		s := Section{Title: title, Body: NoBasis, Refs: []int{}}
		if edit != nil {
			edit(i, &s)
		}
		out.Sections = append(out.Sections, s)
	}
	data, _ := json.Marshal(out)
	return string(data)
}

func TestParseAcceptsOnlySectionsGroundedInMemoriesThatAreNotInferred(t *testing.T) {
	trusts := []string{"stated", "inferred"}
	body, valid := Parse(sections(func(i int, s *Section) {
		if i == 0 {
			s.Body, s.Refs = "Fictitious owner writes reports.", []int{1}
		}
	}), trusts)
	if !valid || !strings.HasPrefix(body, "## "+Titles[0]+"\nFictitious owner writes reports.") || strings.Count(body, NoBasis) != len(Titles)-1 {
		t.Fatal(valid, body)
	}
	if empty, valid := Parse(sections(nil), trusts); !valid || empty != Empty() {
		t.Fatal(valid, empty)
	}
	for name, edit := range map[string]func(i int, s *Section){
		"inferred memory": func(i int, s *Section) {
			if i == 0 {
				s.Body, s.Refs = "Fictitious guess.", []int{2}
			}
		},
		"unknown reference": func(i int, s *Section) {
			if i == 0 {
				s.Body, s.Refs = "Fictitious text.", []int{3}
			}
		},
		"text without reference": func(i int, s *Section) {
			if i == 0 {
				s.Body = "Fictitious text."
			}
		},
		"reference without text": func(i int, s *Section) {
			if i == 0 {
				s.Body, s.Refs = " ", []int{1}
			}
		},
		"no basis with reference": func(i int, s *Section) {
			if i == 0 {
				s.Refs = []int{1}
				s.Body = ""
			}
		},
		"changed title": func(i int, s *Section) {
			if i == 3 {
				s.Title = "Fictitious title"
			}
		},
		"too long": func(i int, s *Section) {
			if i == 0 {
				s.Body, s.Refs = strings.Repeat("长", BodyCharacters), []int{1}
			}
		},
	} {
		if _, valid := Parse(sections(edit), trusts); valid {
			t.Error("accepted:", name)
		}
	}
	if _, valid := Parse(`{"sections":[]}`, trusts); valid {
		t.Error("accepted an output without the nine sections")
	}
}

// fake is the storage and the model of one global handover job.
type fake struct {
	written   *time.Time
	saved     json.RawMessage
	output    string
	calls     int
	body      string
	hash      string
	end       string
	discarded int
}

func (f *fake) Exclusive(context.Context) (func(), error)                         { return func() {}, nil }
func (f *fake) Read(_ context.Context, _ worker.Job, fn func(Reader) error) error { return fn(f) }
func (f *fake) DiscardResult(context.Context, worker.Job) error                   { f.discarded++; return nil }
func (f *fake) RetryLater(_ worker.Job, code string) error {
	return &worker.JobError{Code: code, Until: time.Now().Add(time.Minute)}
}
func (f *fake) Apply(_ context.Context, _ worker.Job, fn func(Unit) error) error { return fn(unit{f}) }
func (f *fake) WrittenAt() (*time.Time, error)                                   { return f.written, nil }
func (f *fake) Input() (json.RawMessage, []memory.Ref, error) {
	return json.RawMessage(`{"input":{"memories":[{"trust":"stated"}],"inputHash":"h1"}}`), nil, nil
}
func (f *fake) HasSavedResult(context.Context, worker.Job) (bool, error) { return f.saved != nil, nil }
func (f *fake) GenerateForJob(_ context.Context, _ worker.Job, _ string, _ prompts.Definition, _ string, record json.RawMessage, _ []memory.Ref) (json.RawMessage, string, error) {
	if f.saved != nil {
		return f.saved, f.output, nil
	}
	f.calls++
	return record, f.output, nil
}

type unit struct{ *fake }

func (u unit) SaveHandover(body, hash string) error { u.body, u.hash = body, hash; return nil }
func (u unit) DiscardResult() error                 { u.discarded++; return nil }
func (u unit) Acknowledge() error                   { u.end = "acknowledged"; return nil }

func grounded() string {
	return sections(func(i int, s *Section) {
		if i == 0 {
			s.Body, s.Refs = "Fictitious owner writes reports.", []int{1}
		}
	})
}

func TestHandoverIsWrittenWithTheHashOfItsInput(t *testing.T) {
	f := &fake{output: grounded()}
	if err := (Service{Store: f, Generator: f}).Process(context.Background(), worker.Job{}); err != nil {
		t.Fatal(err)
	}
	if f.calls != 1 || f.hash != "h1" || !strings.Contains(f.body, "Fictitious owner writes reports.") || f.end != "acknowledged" || f.discarded != 1 {
		t.Fatalf("%+v", f)
	}
}

func TestHandoverIsRewrittenAtMostOnceAnHour(t *testing.T) {
	recent := time.Now().Add(-20 * time.Minute)
	f := &fake{output: grounded(), written: &recent}
	err := (Service{Store: f, Generator: f}).Process(context.Background(), worker.Job{})
	var wait *worker.JobError
	if !errors.As(err, &wait) || wait.Code != "handover_interval" || !wait.NoAttempt || f.calls != 0 {
		t.Fatal(err, f.calls)
	}
	old := time.Now().Add(-2 * time.Hour)
	f.written = &old
	if err := (Service{Store: f, Generator: f}).Process(context.Background(), worker.Job{}); err != nil || f.calls != 1 {
		t.Fatal(err, f.calls)
	}
}

func TestInvalidOutputKeepsTheEarlierHandoverAndIsTriedAgain(t *testing.T) {
	f := &fake{output: sections(func(i int, s *Section) {
		if i == 0 {
			s.Body = "Fictitious text without a reference."
		}
	})}
	err := (Service{Store: f, Generator: f}).Process(context.Background(), worker.Job{})
	var retry *worker.JobError
	if !errors.As(err, &retry) || retry.Code != "handover_invalid_output" || f.body != "" || f.discarded != 1 || f.end != "" {
		t.Fatal(err, f.body, f.end)
	}
}

func TestSavedResultIsCheckedAgainstItsOwnMemoriesWithoutAnotherCall(t *testing.T) {
	// The earlier attempt's input had one inferred memory only.
	f := &fake{output: grounded(), saved: json.RawMessage(`{"input":{"memories":[{"trust":"inferred"}],"inputHash":"h0"}}`)}
	recent := time.Now().Add(-time.Minute)
	f.written = &recent
	err := (Service{Store: f, Generator: f}).Process(context.Background(), worker.Job{})
	var retry *worker.JobError
	if !errors.As(err, &retry) || retry.Code != "handover_invalid_output" || f.calls != 0 {
		t.Fatal(err, f.calls)
	}
}
