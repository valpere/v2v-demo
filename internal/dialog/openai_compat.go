package dialog

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// openAICompatGen calls an OpenAI-compatible POST {baseURL}/v1/chat/completions
// endpoint. It backs both NewOllama (baseURL = OLLAMA_BASE_URL, no key) and
// NewOpenAI (api.openai.com, bearer key). name is used in error messages.
type openAICompatGen struct {
	name    string
	baseURL string
	apiKey  string // "" → no Authorization header
	model   string
	hc      *http.Client
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

func (g *openAICompatGen) Generate(ctx context.Context, systemPrompt string, history []Msg) (string, error) {
	msgs := make([]oaiMessage, 0, len(history)+1)
	msgs = append(msgs, oaiMessage{Role: "system", Content: systemPrompt})
	for _, m := range history {
		msgs = append(msgs, oaiMessage{Role: m.Role, Content: m.Text})
	}

	body, err := json.Marshal(oaiRequest{Model: g.model, Messages: msgs, Stream: false})
	if err != nil {
		return "", fmt.Errorf("%s: marshal: %w", g.name, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("%s: request: %w", g.name, err)
	}
	req.Header.Set("Content-Type", "application/json")
	if g.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+g.apiKey)
	}

	resp, err := g.hc.Do(req)
	if err != nil {
		return "", fmt.Errorf("%s: %w", g.name, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s: %s: %s", g.name, resp.Status, strings.TrimSpace(string(raw)))
	}

	var out oaiResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("%s: decode: %w", g.name, err)
	}
	if out.Error != nil && out.Error.Message != "" {
		return "", fmt.Errorf("%s: %s", g.name, out.Error.Message)
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("%s: empty choices", g.name)
	}
	return out.Choices[0].Message.Content, nil
}
