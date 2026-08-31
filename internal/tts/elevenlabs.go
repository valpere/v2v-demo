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

	endpoint := fmt.Sprintf("%s/v1/text-to-speech/%s?output_format=%s",
		e.baseURL, url.PathEscape(voiceID), elevenFormat)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("tts: elevenlabs: request: %w", err)
	}
	req.Header.Set("xi-api-key", e.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "audio/ogg")

	resp, err := e.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tts: elevenlabs: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tts: elevenlabs: %s: %s", resp.Status, tail(string(data), 400))
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("tts: elevenlabs: empty audio")
	}
	return data, nil
}
