package tts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const (
	elevenModel  = "eleven_multilingual_v2"
	elevenFormat = "opus_48000_128" // Ogg-encapsulated Opus — Telegram sendVoice ready
)

// speakRetryBackoff is the wait before the single retry. A var so tests can
// shrink it. Mirrors internal/dialog/openai_compat.go — voice is the client's
// headline criterion (NFR-1), so a one-off ElevenLabs blip should not silently
// drop the whole turn to text.
var speakRetryBackoff = 1500 * time.Millisecond

type elevenLabs struct {
	apiKey  string
	baseURL string
	hc      *http.Client
}

// NewElevenLabs builds the default Synthesizer.
func NewElevenLabs(apiKey string) Synthesizer {
	return &elevenLabs{
		apiKey:  apiKey,
		baseURL: "https://api.elevenlabs.io",
		hc:      &http.Client{Timeout: 60 * time.Second},
	}
}

type elevenRequest struct {
	Text    string `json:"text"`
	ModelID string `json:"model_id"`
}

// Speak — lang is ignored: eleven_multilingual_v2 does not accept a language
// code and auto-detects uk/en from the text.
func (e *elevenLabs) Speak(ctx context.Context, text, voiceID, _ string) ([]byte, error) {
	if voiceID == "" {
		return nil, fmt.Errorf("tts: elevenlabs: empty voice id")
	}

	body, err := json.Marshal(elevenRequest{Text: text, ModelID: elevenModel})
	if err != nil {
		return nil, fmt.Errorf("tts: elevenlabs: marshal: %w", err)
	}

	// One retry on a transient failure (transport error with a live context,
	// 5xx, or 429). A 4xx (bad key, quota, bad voice) is not retried.
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("tts: elevenlabs: %w", ctx.Err())
			case <-time.After(speakRetryBackoff):
			}
		}
		data, transient, err := e.attempt(ctx, voiceID, body)
		if err == nil {
			return data, nil
		}
		lastErr = err
		if !transient {
			return nil, err
		}
	}
	return nil, lastErr
}

// attempt makes one HTTP call. transient=true means the error may clear on a
// retry.
func (e *elevenLabs) attempt(ctx context.Context, voiceID string, body []byte) (data []byte, transient bool, err error) {
	endpoint := fmt.Sprintf("%s/v1/text-to-speech/%s?output_format=%s",
		e.baseURL, url.PathEscape(voiceID), elevenFormat)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, false, fmt.Errorf("tts: elevenlabs: request: %w", err)
	}
	req.Header.Set("xi-api-key", e.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "audio/ogg")

	resp, err := e.hc.Do(req)
	if err != nil {
		return nil, ctx.Err() == nil, fmt.Errorf("tts: elevenlabs: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))

	if resp.StatusCode != http.StatusOK {
		transient := resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
		return nil, transient, fmt.Errorf("tts: elevenlabs: %s: %s", resp.Status, tail(string(raw), 400))
	}
	if len(raw) == 0 {
		return nil, true, fmt.Errorf("tts: elevenlabs: empty audio")
	}
	return raw, false, nil
}
