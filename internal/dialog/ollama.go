package dialog

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ollamaGen talks to Ollama's OpenAI-compatible endpoint
// (POST {baseURL}/v1/chat/completions). Needs `ollama` logged in to a
// Pro/Max account for :cloud models; those cold-start slow (20–70s on the
// first call), hence the long client timeout.
type ollamaGen struct {
	baseURL string
	model   string
	hc      *http.Client
}

// NewOllama builds the default Generator. DIALOG_MODEL default is gemma4:cloud.
func NewOllama(baseURL, model string) Generator {
	return &ollamaGen{
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
		hc:      &http.Client{Timeout: 120 * time.Second},
	}
}

type oaiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type oaiRequest struct {
	Model    string       `json:"model"`
	Messages []oaiMessage `json:"messages"`
	Stream   bool         `json:"stream"`
}

type oaiResponse struct {
	Choices []struct {
		Message oaiMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (g *ollamaGen) Generate(ctx context.Context, systemPrompt string, history []Msg) (string, error) {
	msgs := make([]oaiMessage, 0, len(history)+1)
	msgs = append(msgs, oaiMessage{Role: "system", Content: systemPrompt})
	for _, m := range history {
		msgs = append(msgs, oaiMessage{Role: m.Role, Content: m.Text})
	}

	body, err := json.Marshal(oaiRequest{Model: g.model, Messages: msgs, Stream: false})
	if err != nil {
		return "", fmt.Errorf("ollama: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("ollama: request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.hc.Do(req)
	if err != nil {
		return "", fmt.Errorf("ollama: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ollama: %s: %s", resp.Status, strings.TrimSpace(string(raw)))
	}

	var out oaiResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("ollama: decode: %w", err)
	}
	if out.Error != nil && out.Error.Message != "" {
		return "", fmt.Errorf("ollama: %s", out.Error.Message)
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("ollama: empty choices")
	}
	return out.Choices[0].Message.Content, nil
}
