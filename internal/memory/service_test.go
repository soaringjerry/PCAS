package memory

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type recordingRepo struct {
	called bool
	input  IngestRequest
}

func (r *recordingRepo) Ingest(_ context.Context, _ Scope, in IngestRequest) (IngestResult, error) {
	r.called = true
	r.input = in
	return IngestResult{}, nil
}
func (r *recordingRepo) GetSource(context.Context, Scope, ID, int) (SourceResult, error) {
	r.called = true
	return SourceResult{}, nil
}

func TestIngestValidatesBeforePersistence(t *testing.T) {
	scope := Scope{OwnerID: NewID(), PrincipalID: "owner", IsOwner: true}
	valid := IngestRequest{Connector: "manual", ExternalID: "message-1", ExternalVersion: "1", Text: "下次路过时想去那家旧书店"}
	for _, tc := range []struct {
		name   string
		change func(*IngestRequest, *Scope)
		want   error
	}{
		{"missing identity", func(_ *IngestRequest, s *Scope) { s.OwnerID = "" }, ErrForbidden},
		{"read-only principal", func(_ *IngestRequest, s *Scope) { s.IsOwner = false }, ErrForbidden},
		{"missing source version", func(i *IngestRequest, _ *Scope) { i.ExternalVersion = "" }, ErrInvalid},
		{"whitespace", func(i *IngestRequest, _ *Scope) { i.Text = "  " }, ErrInvalid},
		{"NUL", func(i *IngestRequest, _ *Scope) { i.Text = "bad\x00text" }, ErrInvalid},
		{"binary attachment", func(i *IngestRequest, _ *Scope) { i.MediaType = "image/png" }, ErrInvalid},
		{"oversized text", func(i *IngestRequest, _ *Scope) { i.Text = strings.Repeat("a", (1<<20)+1) }, ErrInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &recordingRepo{}
			in, principal := valid, scope
			tc.change(&in, &principal)
			_, err := NewService(r).Ingest(context.Background(), principal, in)
			if !errors.Is(err, tc.want) || r.called {
				t.Fatalf("got %v, repository called=%v", err, r.called)
			}
		})
	}
	r := &recordingRepo{}
	if _, err := NewService(r).Ingest(context.Background(), scope, valid); err != nil {
		t.Fatal(err)
	}
	if !r.called || r.input.MediaType != "text/plain" {
		t.Fatal("plain text default missing")
	}
}

func TestChunksPreserveUnicodeEvidenceOffsets(t *testing.T) {
	text := strings.Repeat("明天去旧书店📚，先确认地址。", 250)
	runes := []rune(text)
	chunks := SplitText(text, 1200, 160)
	if len(chunks) < 3 {
		t.Fatal("expected overlapping chunks")
	}
	var reconstructed []rune
	for i, c := range chunks {
		if c.Text != string(runes[c.StartRune:c.EndRune]) {
			t.Fatal("evidence offsets do not locate original text")
		}
		start := 0
		if i > 0 {
			start = 160
			if c.StartRune != chunks[i-1].EndRune-160 {
				t.Fatal("wrong overlap")
			}
		}
		reconstructed = append(reconstructed, []rune(c.Text)[start:]...)
	}
	if string(reconstructed) != text {
		t.Fatal("text was lost or duplicated")
	}
}
