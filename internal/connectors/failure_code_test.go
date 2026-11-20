package connectors

import (
	"context"
	"errors"
	"testing"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/worker"
)

// An import whose own context ended is tried again. The driver's error for a
// closed connection does not wrap the context's error, so the context decides.
func TestAnImportThatWasStoppedIsNotAStorageFailure(t *testing.T) {
	closed := errors.New("fictitious driver error: conn closed")
	if code, failed := FailureCode(context.Canceled, closed); failed || code != "" {
		t.Fatal("a stopped import was recorded as failed", code)
	}
	if _, failed := FailureCode(context.DeadlineExceeded, closed); failed {
		t.Fatal("an import past its deadline was recorded as failed")
	}
	if _, failed := FailureCode(nil, worker.ErrLeaseLost); failed {
		t.Fatal("a lost lease was recorded as failed")
	}
	if code, failed := FailureCode(nil, closed); !failed || code != "import_storage_failed" {
		t.Fatal(code, failed)
	}
	if code, failed := FailureCode(nil, memory.ErrNotFound); !failed || code != "import_source_missing" {
		t.Fatal(code, failed)
	}
	if code, failed := FailureCode(nil, &ArchiveError{Code: "archive_format"}); !failed || code != "archive_format" {
		t.Fatal(code, failed)
	}
	if _, failed := FailureCode(nil, nil); failed {
		t.Fatal("no error is no failure")
	}
}

func TestAnImportSaysWhatItIsDoingInOrder(t *testing.T) {
	b := ImportBatch{State: "done", Total: 3, Stored: 3, Prepared: 3, Organized: 1}
	Describe(&b, "", true)
	if b.Activity != "budget_wait" {
		t.Fatal(b.Activity)
	}
	Describe(&b, "source.extract:conversation:x", true)
	if b.Activity != "organizing" {
		t.Fatal(b.Activity)
	}
	b = ImportBatch{State: "failed", ErrorCode: "import_source_missing"}
	if Describe(&b, "", false); b.Activity != "failed" || b.Error != "原始文件已不可用，请重新上传导出文件。" {
		t.Fatal(b)
	}
}

func TestOnlyAStoringImportPausesAndOnlyAPausedOrFailedOneResumes(t *testing.T) {
	if state, err := ChangeImport("importing", true); err != nil || state != "paused" {
		t.Fatal(state, err)
	}
	if _, err := ChangeImport("done", true); err != ErrImportNotActive {
		t.Fatal(err)
	}
	if state, err := ChangeImport("failed", false); err != nil || state != "importing" {
		t.Fatal(state, err)
	}
}

func TestAConnectionNamesAPlainAddressAndAPCASToken(t *testing.T) {
	ok := ConfigureRequest{Name: "日记", Kind: "poll", URL: "https://example.com/feed", TokenEnv: "PCAS_CONNECTOR_DIARY", IntervalSeconds: 60}
	if CheckConfiguration(ok) != nil {
		t.Fatal("a valid poll was refused")
	}
	for _, change := range []func(*ConfigureRequest){
		func(c *ConfigureRequest) { c.URL = "https://user:pw@example.com" },
		func(c *ConfigureRequest) { c.TokenEnv = "HOME" },
		func(c *ConfigureRequest) { c.IntervalSeconds = 5 },
		func(c *ConfigureRequest) { c.Kind = "folder" },
	} {
		c := ok
		change(&c)
		if CheckConfiguration(c) == nil {
			t.Errorf("%+v was accepted", c)
		}
	}
}
