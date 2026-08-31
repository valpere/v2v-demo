package dialog

import (
	"net/http"
	"time"
)

// NewOpenAI builds the first alternate Generator (DIALOG_BACKEND=openai) —
// api.openai.com's chat completions, bearer key. Default model gpt-4o-mini.
// Shares the OpenAI key with the I-10 whisper-1 STT flip.
func NewOpenAI(apiKey, model string) Generator {
	if model == "" {
		model = "gpt-4o-mini"
	}
	return &openAICompatGen{
		name:    "openai",
		baseURL: "https://api.openai.com",
		apiKey:  apiKey,
		model:   model,
		hc:      &http.Client{Timeout: 60 * time.Second},
	}
}
