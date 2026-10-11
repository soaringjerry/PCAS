package attachment

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/worker"
)

// fake is the model calls and the installed processors of one file.
type fake struct {
	transcript, reading string
	noVision            bool
	visionErr           error
	pages               map[string]string // page number to its text layer
	duration            string
	log                 []string
}

func (f *fake) SavedTranscript(context.Context) (string, bool, error) {
	return f.transcript, f.transcript != "", nil
}
func (f *fake) Transcribe(_ context.Context, audio io.Reader, title string, seconds float64) (string, error) {
	data, _ := io.ReadAll(audio)
	f.log = append(f.log, "transcribe "+title+" "+string(data))
	return "虚构的录音文字", nil
}
func (f *fake) SavedReading(context.Context, string) (string, bool, error) {
	return f.reading, f.reading != "", nil
}
func (f *fake) CanReadImages() bool { return !f.noVision }
func (f *fake) ReadImage(_ context.Context, component string, image ai.Image) (string, error) {
	f.log = append(f.log, "vision "+image.MediaType)
	if f.visionErr != nil {
		return "", f.visionErr
	}
	return "虚构的图片文字", nil
}
func (f *fake) run(_ context.Context, name string, args ...string) (string, error) {
	f.log = append(f.log, name)
	switch name {
	case "pdfinfo":
		return "Title: fictitious\nPages:          " + f.pages["count"] + "\n", nil
	case "pdftotext":
		return f.pages[args[1]], nil
	case "pdftoppm":
		return "", os.WriteFile(args[len(args)-1]+".png", []byte("fictitious page "+args[1]), 0600)
	case "tesseract":
		return "虚构的识别文字", nil
	case "ffprobe":
		return f.duration + "\n", nil
	case "ffmpeg":
		return "", os.WriteFile(args[len(args)-1], []byte("fictitious png"), 0600)
	}
	return "", memory.ErrUnavailable
}

func parse(f *fake, body, media string) (Parsed, error) {
	return Parser{Calls: f, Run: f.run}.Parse(context.Background(), strings.NewReader(body), "虚构文件", media)
}

func code(err error) string {
	var job *worker.JobError
	if errors.As(err, &job) {
		return job.Code
	}
	return ""
}

func TestTextFileIsKeptAsItIsWithinItsLimits(t *testing.T) {
	got, err := parse(&fake{}, "虚构的笔记。", "text/markdown")
	if err != nil || got.Text != "虚构的笔记。" || got.Representation != "extracted" || got.Title != "虚构文件" {
		t.Fatal(got, err)
	}
	for body, want := range map[string]string{"   ": "file_text_invalid", "\xff\xfe": "file_text_invalid", strings.Repeat("a", 1<<20+1): "file_text_too_large"} {
		if _, err := parse(&fake{}, body, "text/plain"); code(err) != want {
			t.Fatal(want, err)
		}
	}
	if _, err := parse(&fake{}, "x", "application/zip"); code(err) != "file_parser_unsupported" {
		t.Fatal(err)
	}
}

func TestImageUsesThePaidReadingThenTheModelThenRecognition(t *testing.T) {
	f := &fake{reading: "虚构的已付文字"}
	if got, err := parse(f, "img", "image/png"); err != nil || got.Text != "虚构的已付文字" || got.Representation != "vision" || len(f.log) != 0 {
		t.Fatal(got, err, f.log)
	}
	f = &fake{}
	if got, err := parse(f, "img", "image/tiff"); err != nil || got.Representation != "vision" || strings.Join(f.log, ",") != "ffmpeg,vision image/png" {
		t.Fatal(got, err, f.log)
	}
	f = &fake{visionErr: &Fallback{Stage: "model", Kind: "timeout"}}
	if got, err := parse(f, "img", "image/jpeg"); err != nil || got.Text != "虚构的识别文字" || got.Representation != "ocr" {
		t.Fatal(got, err)
	}
	f = &fake{noVision: true}
	if got, err := parse(f, "img", "image/jpeg"); err != nil || got.Representation != "ocr" || strings.Join(f.log, ",") != "tesseract" {
		t.Fatal(got, err, f.log)
	}
	// An error that is not a fallback needs repair. It does not become a recognition.
	broken := errors.New("fictitious accounting failure")
	f = &fake{visionErr: broken}
	if _, err := parse(f, "img", "image/jpeg"); !errors.Is(err, broken) {
		t.Fatal(err)
	}
}

func TestPDFKeepsTextPagesAndReadsEmptyPagesAsImages(t *testing.T) {
	f := &fake{pages: map[string]string{"count": "2", "1": "第一页的文字", "2": " "}}
	got, err := parse(f, "pdf", "application/pdf")
	if err != nil || got.Text != "\n[第 1 页]\n第一页的文字\n[第 2 页]\n虚构的图片文字" || got.Representation != "vision" {
		t.Fatalf("%q %v", got.Text, err)
	}
	f = &fake{noVision: true, pages: map[string]string{"count": "1", "1": ""}}
	if got, err := parse(f, "pdf", "application/pdf"); err != nil || got.Representation != "ocr" {
		t.Fatal(got, err)
	}
	for count, want := range map[string]string{"0": "file_pdf_invalid", "101": "file_pdf_page_limit"} {
		if _, err := parse(&fake{pages: map[string]string{"count": count}}, "pdf", "application/pdf"); code(err) != want || !errors.Is(err, memory.ErrUnavailable) {
			t.Fatal(want, err)
		}
	}
}

func TestAudioUsesThePaidTranscriptBeforeItMeasuresTheFile(t *testing.T) {
	f := &fake{transcript: "虚构的已付转写"}
	if got, err := parse(f, "audio", "audio/mpeg"); err != nil || got.Text != "虚构的已付转写" || got.Representation != "transcript" || len(f.log) != 0 {
		t.Fatal(got, err, f.log)
	}
	f = &fake{duration: "61.5"}
	if got, err := parse(f, "audio", "audio/mpeg"); err != nil || got.Text != "虚构的录音文字" || strings.Join(f.log, ",") != "ffprobe,transcribe 虚构文件 audio" {
		t.Fatal(got, err, f.log)
	}
	for duration, want := range map[string]string{"0": "file_audio_probe_failed", "N/A": "file_audio_probe_failed", "14401": "file_audio_duration_limit"} {
		f = &fake{duration: duration}
		if _, err := parse(f, "audio", "audio/wav"); code(err) != want || len(f.log) != 1 {
			t.Fatal(want, err, f.log)
		}
	}
}
