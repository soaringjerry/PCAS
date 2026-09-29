package memory

import (
	"context"
	"io"
)

type Attachments interface {
	IngestAttachment(context.Context, Scope, IngestRequest, io.Reader) (IngestResult, error)
	OpenAttachment(context.Context, Scope, ID, int) (io.ReadCloser, string, string, error)
}
