package tts

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// azureTTS is the ElevenLabs rollback (TTS_BACKEND=azure) — Azure Cognitive
// Services Speech, uk-UA-*Neural voices, free tier F0. Emits Ogg/Opus, same
// as the ElevenLabs impl.
type azureTTS struct {
	key     string
	region  string
	baseURL string // overrides the region-derived URL in tests
	hc      *http.Client
}

// NewAzure builds the rollback Synthesizer. Needs AZURE_SPEECH_KEY + REGION.
func NewAzure(key, region string) Synthesizer {
	return &azureTTS{key: key, region: region, hc: &http.Client{Timeout: 30 * time.Second}}
}

func (a *azureTTS) endpoint() string {
	if a.baseURL != "" {
		return a.baseURL + "/cognitiveservices/v1"
	}
	return fmt.Sprintf("https://%s.tts.speech.microsoft.com/cognitiveservices/v1", a.region)
}

func (a *azureTTS) Speak(ctx context.Context, text, voiceID, lang string) ([]byte, error) {
	if voiceID == "" {
		return nil, fmt.Errorf("tts: azure: empty voice id")
	}

	ssml := fmt.Sprintf(
		`<speak version='1.0' xml:lang='%s'><voice name='%s'>%s</voice></speak>`,
		ssmlLang(voiceID, lang), voiceID, escapeXML(text))

	// One retry on a transient failure (transport error with a live context,
	// 5xx, or 429 — the F0 tier caps TTS at ~20 requests/min). Same shape as
	// the ElevenLabs impl; shares speakRetryBackoff.
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("tts: azure: %w", ctx.Err())
			case <-time.After(speakRetryBackoff):
			}
		}
		data, transient, err := a.attempt(ctx, ssml)
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
func (a *azureTTS) attempt(ctx context.Context, ssml string) (data []byte, transient bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.endpoint(), strings.NewReader(ssml))
	if err != nil {
		return nil, false, fmt.Errorf("tts: azure: request: %w", err)
	}
	req.Header.Set("Ocp-Apim-Subscription-Key", a.key)
	req.Header.Set("Content-Type", "application/ssml+xml")
	req.Header.Set("X-Microsoft-OutputFormat", "ogg-48khz-16bit-mono-opus")
	req.Header.Set("User-Agent", "v2v-demo") // Azure TTS rejects requests with no User-Agent

	resp, err := a.hc.Do(req)
	if err != nil {
		return nil, ctx.Err() == nil, fmt.Errorf("tts: azure: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))

	if resp.StatusCode != http.StatusOK {
		t := resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
		return nil, t, fmt.Errorf("tts: azure: %s: %s", resp.Status, tail(string(raw), 400))
	}
	if len(raw) == 0 {
		return nil, true, fmt.Errorf("tts: azure: empty audio")
	}
	return raw, false, nil
}

// ssmlLang takes the locale from an "xx-YY-*" voice id (so the SSML matches
// the voice), falling back to the conversation language.
func ssmlLang(voiceID, lang string) string {
	if len(voiceID) >= 5 && voiceID[2] == '-' && voiceID[5] == '-' {
		return voiceID[:5]
	}
	if lang == "en" {
		return "en-US"
	}
	return "uk-UA"
}

func escapeXML(s string) string {
	return strings.NewReplacer(
		"&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;",
	).Replace(s)
}
