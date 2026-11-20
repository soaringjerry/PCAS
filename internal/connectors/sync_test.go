package connectors

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/soaringjerry/PCAS/internal/memory"
)

// syncStore records what a sync finished with.
type syncStore struct {
	finished *Batch
	etag     string
}

func (s *syncStore) Finish(_ context.Context, _ Lease, b Batch, etag string, _ map[string]string) error {
	s.finished, s.etag = &b, etag
	return nil
}
func (s *syncStore) Fingerprint(context.Context, Lease, []byte) (string, error) { return "", nil }
func (s *syncStore) IngestArchive(context.Context, memory.Scope, string, []byte, string) error {
	return nil
}

func poll(t *testing.T, handler http.HandlerFunc, l Lease) (*syncStore, error) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	l.Connection.URL, l.Connection.Kind = server.URL, "poll"
	s := &syncStore{}
	return s, Sync(context.Background(), s, l)
}

func TestAnUnchangedPollKeepsItsCursorAndTag(t *testing.T) {
	s, err := poll(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") != `"v1"` || r.URL.Query().Get("cursor") != "c1" {
			t.Error(r.Header, r.URL)
		}
		w.WriteHeader(http.StatusNotModified)
	}, Lease{Cursor: "c1", ETag: `"v1"`})
	if err != nil || s.finished == nil || len(s.finished.Records) != 0 || s.etag != `"v1"` {
		t.Fatal(err, s)
	}
}

func TestAPollBeyondItsLimitsOrWithoutProgressIsInvalid(t *testing.T) {
	records := make([]string, pollRecords+1)
	for i := range records {
		records[i] = fmt.Sprintf(`{"id":"r%d","text":"x"}`, i)
	}
	for name, body := range map[string]string{
		"too many records":  `{"records":[` + strings.Join(records, ",") + `]}`,
		"a cursor in place": `{"records":[],"has_more":true,"next_cursor":"c1"}`,
		"no next cursor":    `{"records":[],"has_more":true}`,
	} {
		s, err := poll(t, func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, body) }, Lease{Cursor: "c1"})
		if !errors.Is(err, memory.ErrInvalid) || s.finished != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	if SyncError(memory.ErrInvalid) != "invalid_source_format" || SyncError(errors.New("x")) != "source_unavailable" {
		t.Fatal("sync error codes")
	}
}
