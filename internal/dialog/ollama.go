package dialog

import (
	"net/http"
	"strings"
	"time"
)

// NewOllama builds the default Generator — Ollama's OpenAI-compatible endpoint
// (POST {baseURL}/v1/chat/completions), no auth. Needs `ollama` logged in to a
// Pro/Max account for :cloud models; those cold-start slow (20–70s on the
// first call), hence the long client timeout.
func NewOllama(baseURL, model string) Generator {
	if model == "" {
		model = "gemma4:cloud"
	}
	return &openAICompatGen{
		name:    "ollama",
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
		hc:      &http.Client{Timeout: 120 * time.Second},
	}
}
