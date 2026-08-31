package stt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// openaiWhisper posts the OGG to the whisper-1 transcription API. Mandatory
// flip for the I-10 client recording (STT_BACKEND=openai): ~2–4s vs tens of
// seconds for local CPU Whisper. Needs OPENAI_API_KEY (with balance).
type openaiWhisper struct {
	apiKey  string
	model   string
	baseURL string
	hc      *http.Client
}

// NewOpenAI builds the whisper-1 Transcriber. Default model whisper-1.
func NewOpenAI(apiKey, model string) Transcriber {
	if model == "" {
		model = "whisper-1"
	}
	return &openaiWhisper{
		apiKey:  apiKey,
		model:   model,
		baseURL: "https://api.openai.com",
		hc:      &http.Client{Timeout: 60 * time.Second},
	}
}

func (w *openaiWhisper) Transcribe(ctx context.Context, oggPath, langHint string) (string, error) {
	f, err := os.Open(oggPath)
	if err != nil {
		return "", fmt.Errorf("stt: open %s: %w", oggPath, err)
	}
	defer f.Close()

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, err := mw.CreateFormFile("file", filepath.Base(oggPath))
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(part, f); err != nil {
		return "", err
	}
	_ = mw.WriteField("model", w.model)
	if langHint == "uk" || langHint == "en" {
		_ = mw.WriteField("language", langHint)
	}
	if err := mw.Close(); err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.baseURL+"/v1/audio/transcriptions", &buf)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+w.apiKey)
	req.Header.Set("Content-Type", mw.FormDataContentType())

	resp, err := w.hc.Do(req)
	if err != nil {
		return "", fmt.Errorf("stt: openai: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("stt: openai: %s: %s", resp.Status, strings.TrimSpace(string(raw)))
	}

	var out struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("stt: openai: decode: %w", err)
	}
	return strings.TrimSpace(out.Text), nil
}
