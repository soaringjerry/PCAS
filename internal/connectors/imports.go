package connectors

import (
	"context"
	"errors"
	"net/url"
	"strings"

	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/worker"
)

// conversationStage starts the stage of a conversation's extraction job.
const conversationStage = "source.extract:conversation:"

// Describe sets what an import is doing now and its error in the owner's
// words. stage is the stage of a running job of the import, or empty;
// budgetWait is true when its organizing waits for the next budget day.
func Describe(b *ImportBatch, stage string, budgetWait bool) {
	if b.ErrorCode != "" {
		b.Error = ImportError(b.ErrorCode)
	}
	switch {
	case b.State == "paused":
		b.Activity = "paused"
	case b.State == "failed":
		b.Activity = "failed"
	case b.State == "importing":
		b.Activity = "storing"
	case strings.HasPrefix(stage, conversationStage):
		b.Activity = "organizing"
	case stage == "source.chunk":
		b.Activity = "preparing"
	case stage == "source.embed":
		b.Activity = "vectorizing"
	case stage == "source.tokenize" || stage == "memory.summary":
		b.Activity = "indexing"
	case b.Prepared < b.Stored:
		b.Activity = "preparing"
	case b.OrganizeLater:
		b.Activity = "held"
	case b.OrganizingFailed > 0:
		b.Activity = "organizing_failed"
	case budgetWait && b.Organized < b.Total:
		b.Activity = "budget_wait"
	case b.Organized < b.Total:
		b.Activity = "queued"
	case b.Indexed < b.Total:
		b.Activity = "indexing"
	case b.Vectorized < b.Total:
		b.Activity = "vectorizing"
	default:
		b.Activity = "complete"
	}
}

// ImportError is the error of an import in the owner's words.
func ImportError(code string) string {
	switch code {
	case "import_storage_failed":
		return "保存中断了，已存好的记录仍在，请点继续重试。"
	case "import_source_missing":
		return "原始文件已不可用，请重新上传导出文件。"
	}
	return (&ArchiveError{Code: code}).Message()
}

// ErrImportNotActive is a pause of an import that is not storing, or a
// resume of one that is neither paused nor failed.
var ErrImportNotActive = &ArchiveError{Code: "import_not_active"}

// ChangeImport checks a pause or a resume of an import in its state and
// returns the state it goes to.
func ChangeImport(state string, pause bool) (string, error) {
	if pause {
		if state != "importing" {
			return "", ErrImportNotActive
		}
		return "paused", nil
	}
	if state != "paused" && state != "failed" {
		return "", ErrImportNotActive
	}
	return "importing", nil
}

// FailureCode names the failure of an archive import. failed is false when
// the import did not fail: it lost its lease, or its own context ended. A
// stopped worker ends the context, and the database driver can then return
// an error that does not wrap the context's error. Such an import is tried
// again; it is not a storage failure.
func FailureCode(contextErr, err error) (code string, failed bool) {
	if err == nil || contextErr != nil || errors.Is(err, worker.ErrLeaseLost) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return "", false
	}
	code = "import_storage_failed"
	var failure *ArchiveError
	if errors.As(err, &failure) {
		code = failure.Code
	}
	if errors.Is(err, memory.ErrNotFound) {
		code = "import_source_missing"
	}
	return code, true
}

// CheckConfiguration checks a connection: a name, a known kind, a sync
// interval from 15 seconds to a day, a plain web address for a poll and
// none otherwise, and a token named in an environment variable of PCAS.
func CheckConfiguration(in ConfigureRequest) error {
	if strings.TrimSpace(in.Name) == "" || len(in.Name) > 200 || in.Kind != "webhook" && in.Kind != "poll" && in.Kind != "folder" || in.IntervalSeconds < 15 || in.IntervalSeconds > 86400 {
		return memory.ErrInvalid
	}
	if in.Kind == "poll" {
		u, err := url.Parse(in.URL)
		if err != nil || u.Scheme != "https" && u.Scheme != "http" || u.Host == "" || u.User != nil || u.Fragment != "" || len(in.URL) > 4096 {
			return memory.ErrInvalid
		}
	} else if in.URL != "" || in.TokenEnv != "" {
		return memory.ErrInvalid
	}
	if in.TokenEnv != "" && (!strings.HasPrefix(in.TokenEnv, "PCAS_CONNECTOR_") || strings.ContainsAny(in.TokenEnv, " \n\r=\x00")) {
		return memory.ErrInvalid
	}
	return nil
}
