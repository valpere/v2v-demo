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

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.endpoint(), strings.NewReader(ssml))
	if err != nil {
		return nil, fmt.Errorf("tts: azure: request: %w", err)
	}
	req.Header.Set("Ocp-Apim-Subscription-Key", a.key)
	req.Header.Set("Content-Type", "application/ssml+xml")
	req.Header.Set("X-Microsoft-OutputFormat", "ogg-48khz-16bit-mono-opus")
	req.Header.Set("User-Agent", "v2v-demo") // Azure TTS rejects requests with no User-Agent

	resp, err := a.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tts: azure: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tts: azure: %s: %s", resp.Status, tail(string(data), 400))
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("tts: azure: empty audio")
	}
	return data, nil
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
