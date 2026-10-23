package projectfile

import (
	"strings"
	"testing"
)

func TestAFileGetsItsDeclaredTypeOrOneFromItsNameOrItsBytes(t *testing.T) {
	none := func() []byte { return nil }
	for want, got := range map[string]string{
		"text/plain":               Media("a.bin", "text/plain; charset=utf-8", none),
		"application/pdf":          Media("报告.PDF", "", none),
		"image/png":                Media("无扩展名", "application/octet-stream", func() []byte { return []byte("\x89PNG\r\n\x1a\n") }),
		"application/octet-stream": Media("x", "not a type", func() []byte { return []byte{0, 1, 2} }),
	} {
		if got != want {
			t.Errorf("%s: %s", want, got)
		}
	}
}

func TestAFileIsFailedWhenAJobStoppedAndExtractedWhenNothingIsLeft(t *testing.T) {
	status, job, reason := State([]Job{{ID: "c", Stage: "source.chunk", State: "done"}, {ID: "p", Stage: "source.parse", State: "failed", Code: "file_pdf_invalid"}})
	if status != "failed" || job != "p" || !strings.HasPrefix(reason, "PDF没有可读页") {
		t.Fatal(status, job, reason)
	}
	if status, job, _ := State([]Job{{ID: "x", Stage: "source.extract", State: "done"}, {ID: "p", Stage: "source.parse", State: "done"}}); status != "extracted" || job != "x" {
		t.Fatal(status, job)
	}
	if status, _, _ := State([]Job{{ID: "x", Stage: "source.extract", State: "done"}, {ID: "y", Stage: "source.extract:1", State: "queued"}}); status != "stored" {
		t.Fatal(status)
	}
	if Reason("other") != "处理未完成（other）；原件保留，可在此重试" {
		t.Fatal(Reason("other"))
	}
}
