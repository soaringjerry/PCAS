package ai

import (
	"context"
	"encoding/base64"
	"errors"

	"github.com/soaringjerry/PCAS/internal/memory"
)

var ErrVisionUnsupported = errors.New("这个通道不能看图")

// Image carries bytes, never an arbitrary remote URL or local filesystem path.
type Image struct {
	MediaType string
	Data      []byte
}

func (i Image) DataURL() string {
	return "data:" + i.MediaType + ";base64," + base64.StdEncoding.EncodeToString(i.Data)
}
func (p Provider) SupportsVision() bool {
	if p.Embedding || p.Transcription {
		return false
	}
	switch p.Protocol {
	case "openai", "responses", "anthropic", "siwc", "codex":
		return true
	}
	return false
}

// VisionProvider follows the existing text default, then configured order.
func (r *Registry) VisionProvider() (Provider, bool) {
	if r == nil {
		return Provider{}, false
	}
	if p, ok := r.Get(r.ExtractionID()); ok && p.SupportsVision() && r.providerAvailable(p) {
		return p, true
	}
	for _, p := range r.Providers() {
		if p.SupportsVision() && r.providerAvailable(p) {
			return p, true
		}
	}
	return Provider{}, false
}

// ReserveVision conservatively counts the encoded payload along with framing.
func (p Provider) ReserveVision(prompt string, image Image) float64 {
	return p.Reserve(prompt + image.DataURL())
}
func (r *Registry) Vision(ctx context.Context, id, instruction string, image Image) (Result, error) {
	p, ok := r.Get(id)
	if !ok || !p.SupportsVision() {
		return Result{}, ErrVisionUnsupported
	}
	if !r.providerAvailable(p) {
		return Result{}, memory.ErrUnavailable
	}
	if len(image.Data) == 0 || len(image.Data) > 20<<20 {
		return Result{}, memory.ErrInvalid
	}
	switch image.MediaType {
	case "image/png", "image/jpeg", "image/webp", "image/gif":
	default:
		return Result{}, ErrVisionUnsupported
	}
	return r.generate(ctx, id, "", instruction, &image)
}
func visionContent(prompt string, image Image, responses bool) []any {
	textType, imageType := "text", "image_url"
	if responses {
		textType, imageType = "input_text", "input_image"
	}
	im := map[string]any{"type": imageType, "image_url": map[string]string{"url": image.DataURL()}}
	if responses {
		im["image_url"] = image.DataURL()
	}
	return []any{map[string]string{"type": textType, "text": prompt}, im}
}
