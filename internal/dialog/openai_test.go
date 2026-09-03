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

func TestOpenAICompatRetry(t *testing.T) {
	old := chatRetryBackoff
	chatRetryBackoff = time.Millisecond
	t.Cleanup(func() { chatRetryBackoff = old })

	ok := `{"choices":[{"message":{"content":"ok"}}]}`

	t.Run("retries once past a 500", func(t *testing.T) {
		var n int
		g := oaiStub(t, func(w http.ResponseWriter, r *http.Request) {
			n++
			if n == 1 {
				w.WriteHeader(http.StatusBadGateway)
				return
			}
			w.Write([]byte(ok))
		})
		out, err := g.Generate(context.Background(), "s", nil)
		if err != nil || out != "ok" {
			t.Fatalf("out=%q err=%v", out, err)
		}
		if n != 2 {
			t.Fatalf("requests = %d, want 2", n)
		}
	})

	t.Run("retries once past a 429", func(t *testing.T) {
		var n int
		g := oaiStub(t, func(w http.ResponseWriter, r *http.Request) {
			n++
			if n == 1 {
				w.WriteHeader(http.StatusTooManyRequests)
				return
			}
			w.Write([]byte(ok))
		})
		if _, err := g.Generate(context.Background(), "s", nil); err != nil {
			t.Fatal(err)
		}
		if n != 2 {
			t.Fatalf("requests = %d, want 2", n)
		}
	})

	t.Run("gives up after two transient failures", func(t *testing.T) {
		var n int
		g := oaiStub(t, func(w http.ResponseWriter, r *http.Request) {
			n++
			w.WriteHeader(http.StatusServiceUnavailable)
		})
		if _, err := g.Generate(context.Background(), "s", nil); err == nil {
			t.Fatal("want error")
		}
		if n != 2 {
			t.Fatalf("requests = %d, want 2 (one retry)", n)
		}
	})

	t.Run("does not retry a 400", func(t *testing.T) {
		var n int
		g := oaiStub(t, func(w http.ResponseWriter, r *http.Request) {
			n++
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":{"message":"bad model"}}`))
		})
		if _, err := g.Generate(context.Background(), "s", nil); err == nil {
			t.Fatal("want error")
		}
		if n != 1 {
			t.Fatalf("requests = %d, want 1 (no retry on 4xx)", n)
		}
	})

	t.Run("does not retry empty choices", func(t *testing.T) {
		var n int
		g := oaiStub(t, func(w http.ResponseWriter, r *http.Request) {
			n++
			w.Write([]byte(`{"choices":[]}`))
		})
		if _, err := g.Generate(context.Background(), "s", nil); err == nil {
			t.Fatal("want error")
		}
		if n != 1 {
			t.Fatalf("requests = %d, want 1", n)
		}
	})

	t.Run("aborts the backoff on a cancelled context", func(t *testing.T) {
		chatRetryBackoff = time.Hour // only a ctx cancel can end the wait
		t.Cleanup(func() { chatRetryBackoff = time.Millisecond })
		g := oaiStub(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
		})
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		done := make(chan error, 1)
		go func() { _, err := g.Generate(ctx, "s", nil); done <- err }()
		select {
		case err := <-done:
			if err == nil || !strings.Contains(err.Error(), "context") {
				t.Fatalf("want a context error, got %v", err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("Generate did not abort the backoff on ctx cancel")
		}
	})
}

func TestNewOllamaOpenAIDefaults(t *testing.T) {
	if g := NewOllama("http://x/", "").(*openAICompatGen); g.model != "gemma4:cloud" || g.apiKey != "" || g.baseURL != "http://x" {
		t.Fatalf("ollama: %+v", g)
	}
	if g := NewOpenAI("k", "").(*openAICompatGen); g.model != "gpt-4.1-mini" || g.apiKey != "k" || !g.jsonMode {
		t.Fatalf("openai: %+v", g)
	}
	if g := NewOpenAI("k", "gpt-4o").(*openAICompatGen); g.model != "gpt-4o" {
		t.Fatalf("explicit model dropped: %+v", g)
	}
}
