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

// openAICompatGen calls an OpenAI-compatible POST {baseURL}/v1/chat/completions
// endpoint. It backs both NewOllama (baseURL = OLLAMA_BASE_URL, no key) and
// NewOpenAI (api.openai.com, bearer key). name is used in error messages.
//
// jsonMode is this backend's "model connection context" for structured output:
// gpt-4o-mini drops the JSON reply object on trivial turns unless forced, so
// NewOpenAI sets it (→ response_format:{"type":"json_object"}). NewOllama
// leaves it off — gemma4:cloud follows the prompt's format instruction
// reliably, and the dev path is not re-verified here. Another backend/model
// gets whatever coaxing its own constructor decides.
type openAICompatGen struct {
	name     string
	baseURL  string
	apiKey   string // "" → no Authorization header
	model    string
	jsonMode bool
	hc       *http.Client
}

// chatRetryBackoff is the wait before the single retry. A var so tests can
// shrink it.
var chatRetryBackoff = 1500 * time.Millisecond

type oaiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type oaiRequest struct {
	Model          string         `json:"model"`
	Messages       []oaiMessage   `json:"messages"`
	Stream         bool           `json:"stream"`
	Temperature    float64        `json:"temperature"`
	ResponseFormat *oaiRespFormat `json:"response_format,omitempty"`
}

type oaiRespFormat struct {
	Type string `json:"type"` // "json_object"
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

	req := oaiRequest{Model: g.model, Messages: msgs, Stream: false, Temperature: 0.2}
	if g.jsonMode {
		req.ResponseFormat = &oaiRespFormat{Type: "json_object"}
	}
	body, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("%s: marshal: %w", g.name, err)
	}

	// One retry on a transient failure — the Ollama cloud free tier and the
	// OpenAI API both throw the occasional transport error / 5xx / 429.
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return "", fmt.Errorf("%s: %w", g.name, ctx.Err())
			case <-time.After(chatRetryBackoff):
			}
		}
		text, transient, err := g.attempt(ctx, body)
		if err == nil {
			return text, nil
		}
		lastErr = err
		if !transient {
			return "", err
		}
	}
	return "", lastErr
}

// attempt makes one HTTP call. transient=true means the error may clear on a
// retry (transport error with a live context, 5xx, or 429).
func (g *openAICompatGen) attempt(ctx context.Context, body []byte) (text string, transient bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", false, fmt.Errorf("%s: request: %w", g.name, err)
	}
	req.Header.Set("Content-Type", "application/json")
	if g.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+g.apiKey)
	}

	resp, err := g.hc.Do(req)
	if err != nil {
		return "", ctx.Err() == nil, fmt.Errorf("%s: %w", g.name, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	if resp.StatusCode != http.StatusOK {
		transient := resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
		return "", transient, fmt.Errorf("%s: %s: %s", g.name, resp.Status, strings.TrimSpace(string(raw)))
	}

	var out oaiResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", false, fmt.Errorf("%s: decode: %w", g.name, err)
	}
	if out.Error != nil && out.Error.Message != "" {
		return "", false, fmt.Errorf("%s: %s", g.name, out.Error.Message)
	}
	if len(out.Choices) == 0 {
		return "", false, fmt.Errorf("%s: empty choices", g.name)
	}
	return out.Choices[0].Message.Content, false, nil
}
