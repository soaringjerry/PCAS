package claim

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
)

// rememberStore is an in-memory RememberUnit.
type rememberStore struct {
	body       string
	blocked    bool
	redirected *memory.Ref
	quoted     *Quoted
	prior      *memory.Ref
	sourceSeen bool
	log        []string
	subject    memory.ID
	confirmed  string
}

var (
	quotedRef = memory.Ref{ID: memory.NewID(), Version: 2, Kind: memory.ClaimKind}
	newRef    = memory.Ref{ID: memory.NewID(), Version: 1, Kind: memory.ClaimKind}
)

func (s *rememberStore) Original(context.Context, memory.Ref) (Original, error) {
	return Original{Body: s.body, Connector: "desk", RecordedAt: time.Now()}, nil
}
func (s *rememberStore) Blocked(context.Context, [32]byte) (bool, error) { return s.blocked, nil }
func (s *rememberStore) Redirected(context.Context, [32]byte) (memory.Ref, bool, error) {
	if s.redirected == nil {
		return memory.Ref{}, false, nil
	}
	return *s.redirected, true, nil
}
func (s *rememberStore) AtQuote(context.Context, memory.Ref, string) (Quoted, bool, error) {
	if s.quoted == nil {
		return Quoted{}, false, nil
	}
	return *s.quoted, true, nil
}
func (s *rememberStore) Supplement(context.Context, memory.Ref, Statement) error {
	s.log = append(s.log, "supplement")
	return nil
}
func (s *rememberStore) Prior(context.Context, memory.ID, [32]byte) (memory.Ref, bool, error) {
	if s.prior == nil {
		return memory.Ref{}, false, nil
	}
	return *s.prior, true, nil
}
func (s *rememberStore) Reassert(context.Context, memory.Ref, Statement, int, int) error {
	s.log = append(s.log, "reassert")
	return nil
}
func (s *rememberStore) Self(context.Context) (memory.ID, error) { return "self", nil }
func (s *rememberStore) Entity(_ context.Context, kind, name string) (memory.ID, error) {
	return memory.ID(kind + ":" + name), nil
}
func (s *rememberStore) SourceEntity(_ context.Context, name string, _ memory.ID) (memory.ID, bool, error) {
	return memory.ID("seen:" + name), s.sourceSeen, nil
}
func (s *rememberStore) NewSourceEntity(_ context.Context, name string, _ memory.ID, _ string) (memory.ID, error) {
	return memory.ID("new:" + name), nil
}
func (s *rememberStore) Create(_ context.Context, _ Statement, subject memory.ID, _, _ int, _ [32]byte) (memory.Ref, error) {
	s.subject = subject
	s.log = append(s.log, "create")
	return newRef, nil
}
func (s *rememberStore) Confirm(_ context.Context, _ memory.Ref, title, _ string) error {
	s.confirmed = title
	return nil
}

func statement() Statement {
	return Statement{Text: "我住在杭州", Nature: "fact", Subject: "我", Predicate: "住在", Quote: "我住在杭州", Acquisition: "direct", Actor: "ai", Source: memory.Ref{ID: memory.NewID(), Version: 1, Kind: memory.SourceKind}}
}

func remember(t *testing.T, s *rememberStore, in Statement) memory.Ref {
	t.Helper()
	if s.body == "" {
		s.body = "今天说：我住在杭州。"
	}
	ref, err := Remember(context.Background(), s, in)
	if err != nil {
		t.Fatal(err)
	}
	return ref
}

func TestAStatementTheOwnerErasedIsNotWrittenAgain(t *testing.T) {
	s := &rememberStore{body: "我住在杭州", blocked: true}
	if _, err := Remember(context.Background(), s, statement()); !errors.Is(err, memory.ErrBlocked) || s.log != nil {
		t.Fatalf("err = %v, log = %v", err, s.log)
	}
}

func TestAQuotationTheSourceDoesNotHoldIsInvalid(t *testing.T) {
	s := &rememberStore{body: "别的话"}
	if _, err := Remember(context.Background(), s, statement()); !errors.Is(err, memory.ErrInvalid) {
		t.Fatalf("err = %v", err)
	}
}

func TestAMergedStatementGivesTheMemoryItWasMergedInto(t *testing.T) {
	s := &rememberStore{redirected: &quotedRef, prior: &newRef}
	if ref := remember(t, s, statement()); ref != quotedRef || s.log != nil {
		t.Fatalf("ref = %v, log = %v", ref, s.log)
	}
}

func TestAStructuredStatementAtAQuotationGivesItsMemoryAndFillsOnlyTheAssistantsDraft(t *testing.T) {
	in := statement()
	in.Structured, in.SubjectType = true, "self"
	s := &rememberStore{quoted: &Quoted{Ref: quotedRef}}
	if ref := remember(t, s, in); ref != quotedRef || s.log != nil {
		t.Fatalf("the owner's memory was changed: ref = %v, log = %v", ref, s.log)
	}
	s = &rememberStore{quoted: &Quoted{Ref: quotedRef, Editable: true}}
	if ref := remember(t, s, in); ref != quotedRef || len(s.log) != 1 || s.log[0] != "supplement" {
		t.Fatalf("ref = %v, log = %v", ref, s.log)
	}
	in.AllowNewAtQuote = true
	s = &rememberStore{quoted: &Quoted{Ref: quotedRef}}
	if ref := remember(t, s, in); ref != newRef {
		t.Fatalf("a second memory at the quotation was not made: %v", ref)
	}
}

func TestALaterSourceVersionSayingTheSameIsNewEvidence(t *testing.T) {
	s := &rememberStore{prior: &quotedRef}
	if ref := remember(t, s, statement()); ref != quotedRef || len(s.log) != 1 || s.log[0] != "reassert" {
		t.Fatalf("ref = %v, log = %v", ref, s.log)
	}
}

func TestANewMemoryIsAboutItsSubject(t *testing.T) {
	in := statement()
	in.Structured, in.SubjectType = true, "self"
	s := &rememberStore{}
	remember(t, s, in)
	if s.subject != "self" {
		t.Fatalf("subject = %s", s.subject)
	}
	in.SubjectType, in.Subject = "person", "小王"
	remember(t, s, in)
	if s.subject != "person:小王" {
		t.Fatalf("subject = %s", s.subject)
	}
	in = statement()
	in.Subject = "小王"
	remember(t, s, in)
	if s.subject != "new:小王" {
		t.Fatalf("subject = %s", s.subject)
	}
	s.sourceSeen = true
	remember(t, s, in)
	if s.subject != "seen:小王" {
		t.Fatalf("subject = %s", s.subject)
	}
}

func TestTheOwnersOwnMemoryKeepsTheirWords(t *testing.T) {
	in := statement()
	s := &rememberStore{}
	remember(t, s, in)
	if s.confirmed != "" {
		t.Fatal("the assistant's memory was kept as the owner's words")
	}
	in.Actor = "user"
	remember(t, s, in)
	if s.confirmed != ConfirmedTitle {
		t.Fatalf("confirmed = %q", s.confirmed)
	}
}
