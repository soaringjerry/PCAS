// Package attachment owns the input rules that turn an original file into
// text: which processor reads each media type, the limits of a file, and the
// fallback from a model reading to character recognition. It needs no storage.
package attachment

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/soaringjerry/PCAS/internal/ai"
	"github.com/soaringjerry/PCAS/internal/memory"
	"github.com/soaringjerry/PCAS/internal/worker"
)

const (
	// textLimit is the largest text of one file, in bytes.
	textLimit = 1 << 20
	// pageLimit is the largest page count of one PDF file.
	pageLimit = 100
	// audioLimit is the longest audio file, in seconds.
	audioLimit = 4 * 3600
)

// Parsed is the text of one original file. Representation is "extracted",
// "ocr", "vision", or "transcript".
type Parsed struct{ Text, Representation, Title, Media string }

// Fallback is a model reading that did not give text. The image is then read
// with character recognition. Stage and Kind go to the log only.
type Fallback struct{ Stage, Kind string }

func (f *Fallback) Error() string { return "attachment model reading failed: " + f.Stage }

// Calls makes the paid model calls of one original file and keeps each paid
// result, so that a repeated parse does not pay again.
type Calls interface {
	// SavedTranscript returns the paid transcript of this file, if one exists.
	// It returns memory.ErrUnavailable when no transcript exists and no
	// provider can transcribe.
	SavedTranscript(ctx context.Context) (text string, found bool, err error)
	// Transcribe submits the audio and saves the paid transcript.
	Transcribe(ctx context.Context, audio io.Reader, title string, seconds float64) (string, error)
	// SavedReading returns the paid model reading of one image, if one exists.
	SavedReading(ctx context.Context, component string) (text string, found bool, err error)
	// CanReadImages reports whether a provider can read images.
	CanReadImages() bool
	// ReadImage submits one image and saves the paid reading. It returns a
	// *Fallback when the model gave no text and the call needs no repair.
	ReadImage(ctx context.Context, component string, image ai.Image) (string, error)
}

// Parser reads original files.
type Parser struct {
	Calls Calls
	// Run runs one external processor. The zero value runs the installed tool.
	Run func(ctx context.Context, name string, args ...string) (string, error)
}

func (p Parser) run(ctx context.Context, name string, args ...string) (string, error) {
	if p.Run != nil {
		return p.Run(ctx, name, args...)
	}
	return RunTool(ctx, name, args...)
}

// Parse copies the file to a private directory and reads it by its media type.
func (p Parser) Parse(ctx context.Context, file io.Reader, title, media string) (Parsed, error) {
	parsed := Parsed{Title: title, Media: media}
	dir, err := os.MkdirTemp("", "pcas-parse-")
	if err != nil {
		return parsed, err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "original")
	input, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return parsed, err
	}
	_, err = io.Copy(input, file)
	closeErr := input.Close()
	if err != nil {
		return parsed, err
	}
	if closeErr != nil {
		return parsed, closeErr
	}
	var text, representation string
	if strings.HasPrefix(media, "text/") || media == "application/json" || media == "application/xml" {
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return parsed, readErr
		}
		if len(data) > textLimit {
			return parsed, &worker.JobError{Code: "file_text_too_large"}
		}
		if !utf8.Valid(data) || strings.TrimSpace(string(data)) == "" {
			return parsed, &worker.JobError{Code: "file_text_invalid"}
		}
		text, representation = string(data), "extracted"
	} else if strings.HasPrefix(media, "audio/") {
		return p.transcribe(ctx, parsed, path)
	} else if media == "application/pdf" {
		text, representation, err = p.readPDF(ctx, dir, path)
	} else if isImage(media) {
		text, representation, err = p.ReadImage(ctx, path, media)
	} else {
		return parsed, &worker.JobError{Code: "file_parser_unsupported"}
	}
	if err != nil {
		return parsed, err
	}
	if strings.TrimSpace(text) == "" || len(text) > textLimit {
		return parsed, &worker.JobError{Code: "file_text_too_large"}
	}
	parsed.Text, parsed.Representation = text, representation
	return parsed, nil
}

func isImage(media string) bool {
	switch media {
	case "image/png", "image/jpeg", "image/webp", "image/tiff", "image/gif", "image/bmp":
		return true
	}
	return false
}

// A saved transcript is used before the audio is measured again.
func (p Parser) transcribe(ctx context.Context, parsed Parsed, path string) (Parsed, error) {
	saved, found, err := p.Calls.SavedTranscript(ctx)
	if err != nil {
		return parsed, err
	}
	if found {
		if len(saved) > textLimit {
			return parsed, &worker.JobError{Code: "file_text_too_large"}
		}
		if strings.TrimSpace(saved) == "" {
			return parsed, &worker.JobError{Code: "file_text_invalid"}
		}
		parsed.Text, parsed.Representation = saved, "transcript"
		return parsed, nil
	}
	durationText, err := p.run(ctx, "ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "default=noprint_wrappers=1:nokey=1", path)
	if err != nil {
		return parsed, memory.ErrUnavailable
	}
	seconds, err := strconv.ParseFloat(strings.TrimSpace(durationText), 64)
	if err != nil || seconds <= 0 {
		return parsed, &worker.JobError{Code: "file_audio_probe_failed"}
	}
	if seconds > audioLimit {
		return parsed, &worker.JobError{Code: "file_audio_duration_limit"}
	}
	audio, err := os.Open(path)
	if err != nil {
		return parsed, err
	}
	text, err := p.Calls.Transcribe(ctx, audio, parsed.Title, seconds)
	audio.Close()
	if err != nil {
		return parsed, err
	}
	parsed.Text, parsed.Representation = text, "transcript"
	if strings.TrimSpace(text) == "" || len(text) > textLimit {
		return parsed, &worker.JobError{Code: "file_text_too_large"}
	}
	return parsed, nil
}

// ReadImage reads one image with the model. A failed or unavailable model
// falls back to character recognition.
// Logs contain stages and error classes, never images, transcripts or keys.
func (p Parser) ReadImage(ctx context.Context, path, media string) (string, string, error) {
	originalData, err := os.ReadFile(path)
	if err != nil {
		return "", "", err
	}
	component := fmt.Sprintf("vision:%s:%x", filepath.Base(path), sha256.Sum256(originalData))
	saved, found, err := p.Calls.SavedReading(ctx, component)
	if err != nil {
		return "", "", err
	}
	if found {
		return saved, "vision", nil
	}
	fallback := &Fallback{Stage: "select", Kind: "unavailable"}
	if p.Calls.CanReadImages() {
		visionPath, visionMedia := path, media
		if media == "image/tiff" {
			visionPath = filepath.Join(filepath.Dir(path), "vision.png")
			if _, err := p.run(ctx, "ffmpeg", "-v", "error", "-i", path, "-frames:v", "1", "-y", visionPath); err != nil {
				slog.WarnContext(ctx, "attachment vision fallback", "stage", "convert", "error_type", "processor_unavailable")
				text, err := p.recognize(ctx, path)
				return text, "ocr", err
			}
			visionMedia = "image/png"
		}
		data, err := os.ReadFile(visionPath)
		if err != nil {
			return "", "", err
		}
		text, err := p.Calls.ReadImage(ctx, component, ai.Image{MediaType: visionMedia, Data: data})
		if err == nil {
			return text, "vision", nil
		}
		if !errors.As(err, &fallback) {
			return "", "", err
		}
	}
	slog.WarnContext(ctx, "attachment vision fallback", "stage", fallback.Stage, "error_type", fallback.Kind)
	text, err := p.recognize(ctx, path)
	return text, "ocr", err
}

func (p Parser) recognize(ctx context.Context, path string) (string, error) {
	return p.run(ctx, "tesseract", path, "stdout", "-l", "chi_sim+eng")
}

// A page that has text keeps it. A page without text is read as an image.
// The file is "ocr" when one page was recognized, else it has the
// representation of its last page that was read as an image.
func (p Parser) readPDF(ctx context.Context, dir, path string) (string, string, error) {
	info, err := p.run(ctx, "pdfinfo", path)
	if err != nil {
		return "", "", err
	}
	pages := 0
	for _, line := range strings.Split(info, "\n") {
		if strings.HasPrefix(line, "Pages:") {
			pages, _ = strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "Pages:")))
		}
	}
	if pages < 1 {
		return "", "", errors.Join(memory.ErrUnavailable, &worker.JobError{Code: "file_pdf_invalid"})
	}
	if pages > pageLimit {
		return "", "", errors.Join(memory.ErrUnavailable, &worker.JobError{Code: "file_pdf_page_limit"})
	}
	var out strings.Builder
	representation := "extracted"
	for page := 1; page <= pages; page++ {
		n := strconv.Itoa(page)
		text, err := p.run(ctx, "pdftotext", "-f", n, "-l", n, "-layout", path, "-")
		if err != nil {
			return "", "", err
		}
		if strings.TrimSpace(text) == "" {
			prefix := filepath.Join(dir, "page-"+n)
			if _, err := p.run(ctx, "pdftoppm", "-f", n, "-l", n, "-singlefile", "-scale-to", "1800", "-png", path, prefix); err != nil {
				return "", "", err
			}
			var pageRepresentation string
			text, pageRepresentation, err = p.ReadImage(ctx, prefix+".png", "image/png")
			if err != nil {
				return "", "", err
			}
			if pageRepresentation == "ocr" {
				representation = "ocr"
			} else if representation != "ocr" {
				representation = pageRepresentation
			}
		}
		fmt.Fprintf(&out, "\n[第 %d 页]\n%s", page, text)
		if out.Len() > textLimit {
			return "", "", &worker.JobError{Code: "file_text_too_large"}
		}
	}
	return out.String(), representation, nil
}

// RunTool runs an installed processor and returns its output. A processor
// that is not installed returns memory.ErrUnavailable. Output above the text
// limit stops the processor.
func RunTool(ctx context.Context, name string, args ...string) (string, error) {
	if _, err := exec.LookPath(name); err != nil {
		return "", memory.ErrUnavailable
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stderr = io.Discard
	var out boundedOutput
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("attachment parser failed")
	}
	return out.String(), nil
}

type boundedOutput struct{ strings.Builder }

func (b *boundedOutput) Write(data []byte) (int, error) {
	if b.Len()+len(data) > textLimit {
		return 0, memory.ErrInvalid
	}
	return b.Builder.Write(data)
}
