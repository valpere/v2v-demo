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

func oaiStub(t *testing.T, handler http.HandlerFunc) *openAICompatGen {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &openAICompatGen{name: "openai", baseURL: srv.URL, apiKey: "sk-test", model: "gpt-4o-mini", hc: &http.Client{Timeout: 5 * time.Second}}
}

func TestOpenAICompatGenerate(t *testing.T) {
	var gotAuth, gotPath string
	var gotReq oaiRequest
	g := oaiStub(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotReq)
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"hi there"}}]}`))
	})

	out, err := g.Generate(context.Background(), "SYS", []Msg{{Role: "user", Text: "hello"}})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if out != "hi there" {
		t.Fatalf("out = %q", out)
	}
	if gotAuth != "Bearer sk-test" {
		t.Errorf("auth = %q", gotAuth)
	}
	if gotPath != "/v1/chat/completions" {
		t.Errorf("path = %q", gotPath)
	}
	if len(gotReq.Messages) != 2 || gotReq.Messages[0].Role != "system" || gotReq.Messages[1].Content != "hello" {
		t.Errorf("messages = %+v", gotReq.Messages)
	}
	if gotReq.Model != "gpt-4o-mini" || gotReq.Stream {
		t.Errorf("req = %+v", gotReq)
	}
}

func TestOpenAICompatErrors(t *testing.T) {
	t.Run("non-200 carries body", func(t *testing.T) {
		g := oaiStub(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error":{"message":"rate limited"}}`))
		})
		_, err := g.Generate(context.Background(), "s", []Msg{{Role: "user", Text: "x"}})
		if err == nil || !strings.Contains(err.Error(), "rate limited") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("empty choices", func(t *testing.T) {
		g := oaiStub(t, func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"choices":[]}`))
		})
		_, err := g.Generate(context.Background(), "s", nil)
		if err == nil || !strings.Contains(err.Error(), "empty choices") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("200 with error body", func(t *testing.T) {
		g := oaiStub(t, func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"error":{"message":"model not found"}}`))
		})
		_, err := g.Generate(context.Background(), "s", nil)
		if err == nil || !strings.Contains(err.Error(), "model not found") {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestNewOllamaOpenAIDefaults(t *testing.T) {
	if g := NewOllama("http://x/", "").(*openAICompatGen); g.model != "gemma4:cloud" || g.apiKey != "" || g.baseURL != "http://x" {
		t.Fatalf("ollama: %+v", g)
	}
	if g := NewOpenAI("k", "").(*openAICompatGen); g.model != "gpt-4o-mini" || g.apiKey != "k" {
		t.Fatalf("openai: %+v", g)
	}
	if g := NewOpenAI("k", "gpt-4o").(*openAICompatGen); g.model != "gpt-4o" {
		t.Fatalf("explicit model dropped: %+v", g)
	}
}
