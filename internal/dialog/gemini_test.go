package dialog

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func geminiStub(t *testing.T, h http.HandlerFunc) *geminiGen {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &geminiGen{apiKey: "g-key", model: "gemini-flash-latest", baseURL: srv.URL, hc: &http.Client{Timeout: 5 * time.Second}}
}

func TestGeminiGenerate(t *testing.T) {
	var gotKey, gotPath string
	var gotReq geminiRequest
	g := geminiStub(t, func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("x-goog-api-key")
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotReq)
		w.Write([]byte(`{"candidates":[{"content":{"role":"model","parts":[{"text":"Yes, "},{"text":"we do."}]}}]}`))
	})

	out, err := g.Generate(context.Background(), "PERSONA", []Msg{
		{Role: "user", Text: "hi"},
		{Role: "assistant", Text: "hello"},
		{Role: "user", Text: "do you do X?"},
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if out != "Yes, we do." { // parts concatenated
		t.Fatalf("out = %q", out)
	}
	if gotKey != "g-key" {
		t.Errorf("key header = %q", gotKey)
	}
	if gotPath != "/v1beta/models/gemini-flash-latest:generateContent" {
		t.Errorf("path = %q", gotPath)
	}
	if gotReq.SystemInstruction == nil || gotReq.SystemInstruction.Parts[0].Text != "PERSONA" {
		t.Errorf("systemInstruction = %+v", gotReq.SystemInstruction)
	}
	if len(gotReq.Contents) != 3 ||
		gotReq.Contents[0].Role != "user" ||
		gotReq.Contents[1].Role != "model" || // assistant -> model
		gotReq.Contents[2].Parts[0].Text != "do you do X?" {
		t.Errorf("contents = %+v", gotReq.Contents)
	}
	if gotReq.GenerationConfig["temperature"] == nil {
		t.Errorf("generationConfig = %+v", gotReq.GenerationConfig)
	}
}

func TestGeminiErrors(t *testing.T) {
	t.Run("429 prepay", func(t *testing.T) {
		g := geminiStub(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error":{"message":"prepayment credits depleted"}}`))
		})
		_, err := g.Generate(context.Background(), "s", []Msg{{Role: "user", Text: "x"}})
		if err == nil || !strings.Contains(err.Error(), "prepayment credits depleted") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("no candidates", func(t *testing.T) {
		g := geminiStub(t, func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"candidates":[]}`))
		})
		_, err := g.Generate(context.Background(), "s", nil)
		if err == nil || !strings.Contains(err.Error(), "empty response") {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestNewGeminiDefault(t *testing.T) {
	if g := NewGemini("k", "").(*geminiGen); g.model != "gemini-flash-latest" {
		t.Fatalf("model = %q", g.model)
	}
}
