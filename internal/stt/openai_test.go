package stt

import (
	"context"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func oggFile(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "voice-9.ogg")
	if err := os.WriteFile(p, []byte("OggS-fake-audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func openaiStub(t *testing.T, h http.HandlerFunc) *openaiWhisper {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &openaiWhisper{apiKey: "sk-1", model: "whisper-1", baseURL: srv.URL, hc: &http.Client{Timeout: 5 * time.Second}}
}

func TestOpenAITranscribe(t *testing.T) {
	var gotAuth, gotModel, gotLang, gotFilename string
	w := openaiStub(t, func(rw http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		mr := multipart.NewReader(r.Body, params["boundary"])
		for {
			part, err := mr.NextPart()
			if err != nil {
				break
			}
			buf := make([]byte, 256)
			n, _ := part.Read(buf)
			switch part.FormName() {
			case "model":
				gotModel = string(buf[:n])
			case "language":
				gotLang = string(buf[:n])
			case "file":
				gotFilename = part.FileName()
			}
		}
		rw.Write([]byte(`{"text":"  привіт світ  "}`))
	})

	got, err := w.Transcribe(context.Background(), oggFile(t), "uk")
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if got != "привіт світ" {
		t.Fatalf("got %q, want trimmed", got)
	}
	if gotAuth != "Bearer sk-1" || gotModel != "whisper-1" || gotLang != "uk" || gotFilename != "voice-9.ogg" {
		t.Errorf("auth=%q model=%q lang=%q file=%q", gotAuth, gotModel, gotLang, gotFilename)
	}
}

func TestOpenAITranscribeNoLangHint(t *testing.T) {
	sawLang := false
	w := openaiStub(t, func(rw http.ResponseWriter, r *http.Request) {
		_, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		mr := multipart.NewReader(r.Body, params["boundary"])
		for {
			part, err := mr.NextPart()
			if err != nil {
				break
			}
			if part.FormName() == "language" {
				sawLang = true
			}
		}
		rw.Write([]byte(`{"text":"x"}`))
	})
	if _, err := w.Transcribe(context.Background(), oggFile(t), "auto"); err != nil {
		t.Fatal(err)
	}
	if sawLang {
		t.Error(`"auto" should not send a language field`)
	}
}

func TestOpenAITranscribeErrors(t *testing.T) {
	w := openaiStub(t, func(rw http.ResponseWriter, r *http.Request) {
		rw.WriteHeader(http.StatusUnauthorized)
		rw.Write([]byte(`{"error":{"message":"invalid api key"}}`))
	})
	_, err := w.Transcribe(context.Background(), oggFile(t), "")
	if err == nil || !strings.Contains(err.Error(), "invalid api key") {
		t.Fatalf("err = %v", err)
	}

	if _, err := w.Transcribe(context.Background(), filepath.Join(t.TempDir(), "missing.ogg"), ""); err == nil {
		t.Error("missing file: want error")
	}
}

func TestNewOpenAIDefault(t *testing.T) {
	if w := NewOpenAI("k", "").(*openaiWhisper); w.model != "whisper-1" {
		t.Fatalf("model = %q", w.model)
	}
}
