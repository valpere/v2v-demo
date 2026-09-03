package dialog

import (
	"net/http"
	"time"
)

// NewOpenAI builds the client-facing Generator (DIALOG_BACKEND=openai) —
// api.openai.com's chat completions, bearer key. Default model gpt-4.1-mini
// (gpt-4o-mini followed the certification/grounding rules poorly on the
// client stack — 2026-09-03, see .engage/conversation-style.md). Shares the
// OpenAI key with the I-10 whisper-1 STT flip.
func NewOpenAI(apiKey, model string) Generator {
	if model == "" {
		model = "gpt-4.1-mini"
	}
	return &openAICompatGen{
		name:     "openai",
		baseURL:  "https://api.openai.com",
		apiKey:   apiKey,
		model:    model,
		jsonMode: true, // gpt-4o-mini won't reliably emit the JSON reply object otherwise
		hc:       &http.Client{Timeout: 60 * time.Second},
	}
}
