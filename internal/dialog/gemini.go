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

// geminiGen calls the native Gemini API
// (POST generativelanguage.googleapis.com/v1beta/models/{model}:generateContent).
// Last resort (DIALOG_BACKEND=gemini): the AI Studio project needs a $25
// prepay before the API responds at all (429 "prepayment credits depleted").
type geminiGen struct {
	apiKey  string
	model   string
	baseURL string
	hc      *http.Client
}

// NewGemini builds the last-resort Generator. Default model gemini-flash-latest.
func NewGemini(apiKey, model string) Generator {
	if model == "" {
		model = "gemini-flash-latest"
	}
	return &geminiGen{
		apiKey:  apiKey,
		model:   model,
		baseURL: "https://generativelanguage.googleapis.com",
		hc:      &http.Client{Timeout: 90 * time.Second},
	}
}

type geminiPart struct {
	Text string `json:"text"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"` // "user" | "model"
	Parts []geminiPart `json:"parts"`
}

type geminiRequest struct {
	SystemInstruction *geminiContent  `json:"systemInstruction,omitempty"`
	Contents          []geminiContent `json:"contents"`
	GenerationConfig  map[string]any  `json:"generationConfig,omitempty"`
}

type geminiResponse struct {
	Candidates []struct {
		Content geminiContent `json:"content"`
	} `json:"candidates"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (g *geminiGen) Generate(ctx context.Context, systemPrompt string, history []Msg) (string, error) {
	contents := make([]geminiContent, 0, len(history))
	for _, m := range history {
		role := "user"
		if m.Role == "assistant" {
			role = "model"
		}
		contents = append(contents, geminiContent{Role: role, Parts: []geminiPart{{Text: m.Text}}})
	}

	// The shared contract is a single JSON object {reply,slots,signal}. gemini's
	// own "model connection context" for forcing that would be
	// GenerationConfig.responseMimeType = "application/json" (optionally
	// responseSchema) — add it here if this last-resort backend is ever funded
	// and used; today it relies on the prompt instruction alone.
	body, err := json.Marshal(geminiRequest{
		SystemInstruction: &geminiContent{Parts: []geminiPart{{Text: systemPrompt}}},
		Contents:          contents,
		GenerationConfig:  map[string]any{"temperature": 0.2},
	})
	if err != nil {
		return "", fmt.Errorf("gemini: marshal: %w", err)
	}

	endpoint := fmt.Sprintf("%s/v1beta/models/%s:generateContent", g.baseURL, g.model)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("gemini: request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", g.apiKey)

	resp, err := g.hc.Do(req)
	if err != nil {
		return "", fmt.Errorf("gemini: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("gemini: %s: %s", resp.Status, strings.TrimSpace(string(raw)))
	}

	var out geminiResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("gemini: decode: %w", err)
	}
	if out.Error != nil && out.Error.Message != "" {
		return "", fmt.Errorf("gemini: %s", out.Error.Message)
	}
	if len(out.Candidates) == 0 || len(out.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("gemini: empty response")
	}

	var sb strings.Builder
	for _, p := range out.Candidates[0].Content.Parts {
		sb.WriteString(p.Text)
	}
	return sb.String(), nil
}
