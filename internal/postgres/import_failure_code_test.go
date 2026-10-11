package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/soaringjerry/PCAS/internal/connectors"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/worker"
)

// An import whose own context ended is tried again. The driver's error for a
// closed connection does not wrap the context's error, so the context decides.
func TestAnImportThatWasStoppedIsNotAStorageFailure(t *testing.T) {
	closed := errors.New("fictitious driver error: conn closed")
	if code, failed := importFailureCode(context.Canceled, closed); failed || code != "" {
		t.Fatal("a stopped import was recorded as failed", code)
	}
	if _, failed := importFailureCode(context.DeadlineExceeded, closed); failed {
		t.Fatal("an import past its deadline was recorded as failed")
	}
	if _, failed := importFailureCode(nil, worker.ErrLeaseLost); failed {
		t.Fatal("a lost lease was recorded as failed")
	}
	if code, failed := importFailureCode(nil, closed); !failed || code != "import_storage_failed" {
		t.Fatal(code, failed)
	}
	if code, failed := importFailureCode(nil, memory.ErrNotFound); !failed || code != "import_source_missing" {
		t.Fatal(code, failed)
	}
	if code, failed := importFailureCode(nil, &connectors.ArchiveError{Code: "archive_format"}); !failed || code != "archive_format" {
		t.Fatal(code, failed)
	}
	if _, failed := importFailureCode(nil, nil); failed {
		t.Fatal("no error is no failure")
	}
}
