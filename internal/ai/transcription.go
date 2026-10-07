package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/soaringjerry/PCAS/internal/memory"
)

func (r *Registry) Transcribe(ctx context.Context, file io.Reader, name string) (string, error) {
	text, _, err := r.TranscribeUsage(ctx, file, name)
	return text, err
}

func (r *Registry) TranscribeUsage(ctx context.Context, file io.Reader, name string) (text string, durationMS *int64, callErr error) {
	p, ok := r.Get(r.Config.Transcription)
	if !ok || !r.Available(p.ID) || !p.Transcription {
		return "", nil, memory.ErrUnavailable
	}
	started := time.Now()
	defer func() { ms := time.Since(started).Milliseconds(); durationMS = &ms; observeInvocation(ctx, ms) }()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	name = strings.NewReplacer("\r", "", "\n", "").Replace(name)
	part, err := form.CreateFormFile("file", name)
	if err != nil {
		return "", durationMS, err
	}
	if _, err := io.Copy(part, io.LimitReader(file, 20<<20)); err != nil {
		return "", durationMS, err
	}
	if err := form.WriteField("model", p.Model); err != nil {
		return "", durationMS, err
	}
	if err := form.Close(); err != nil {
		return "", durationMS, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(p.BaseURL, "/")+"/audio/transcriptions", &body)
	if err != nil {
		return "", durationMS, err
	}
	req.Header.Set("Content-Type", form.FormDataContentType())
	if key := os.Getenv(p.KeyEnv); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	response, err := r.HTTP.Do(req)
	if err != nil {
		return "", durationMS, fmt.Errorf("transcription provider unreachable")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", durationMS, fmt.Errorf("transcription provider HTTP %d", response.StatusCode)
	}
	var result struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(&result); err != nil {
		return "", durationMS, fmt.Errorf("invalid transcription response")
	}
	if strings.TrimSpace(result.Text) == "" {
		return "", durationMS, fmt.Errorf("empty transcription")
	}
	return result.Text, durationMS, nil
}
