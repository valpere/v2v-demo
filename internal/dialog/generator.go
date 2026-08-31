package dialog

import "context"

// Generator is the single-call LLM abstraction (REQ-DLG-07). Three impls —
// ollama (default), openai, gemini — selected by DIALOG_BACKEND in cmd/bot.
type Generator interface {
	// Generate returns the raw model text (spoken reply + fenced trailer).
	Generate(ctx context.Context, systemPrompt string, history []Msg) (string, error)
}
