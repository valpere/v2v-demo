package tts

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

func testEleven(srv *httptest.Server) *elevenLabs {
	return &elevenLabs{apiKey: "k-test", baseURL: srv.URL, hc: &http.Client{Timeout: 5 * time.Second}}
}

func TestElevenLabsSpeak(t *testing.T) {
	var gotPath, gotQuery, gotKey, gotAccept string
	var gotBody elevenRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		gotKey = r.Header.Get("xi-api-key")
		gotAccept = r.Header.Get("Accept")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.Header().Set("Content-Type", "audio/ogg")
		w.Write([]byte("OggS\x00fake-opus"))
	}))
	defer srv.Close()

	audio, err := testEleven(srv).Speak(context.Background(), "Доброго дня", "VOICE123", "uk")
	if err != nil {
		t.Fatalf("Speak: %v", err)
	}
	if !strings.HasPrefix(string(audio), "OggS") {
		t.Fatalf("audio = %q", audio)
	}
	if gotPath != "/v1/text-to-speech/VOICE123" {
		t.Errorf("path = %q", gotPath)
	}
	if !strings.Contains(gotQuery, "output_format=opus_48000_128") {
		t.Errorf("query = %q", gotQuery)
	}
	if gotKey != "k-test" || gotAccept != "audio/ogg" {
		t.Errorf("headers: key=%q accept=%q", gotKey, gotAccept)
	}
	if gotBody.Text != "Доброго дня" || gotBody.ModelID != "eleven_multilingual_v2" {
		t.Errorf("body = %+v", gotBody)
	}
}

func TestElevenLabsErrors(t *testing.T) {
	t.Run("empty voice id", func(t *testing.T) {
		_, err := NewElevenLabs("k").Speak(context.Background(), "hi", "", "en")
		if err == nil {
			t.Fatal("want error for empty voice id")
		}
	})

	t.Run("non-200 carries body", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnprocessableEntity)
			w.Write([]byte(`{"detail":"voice not found"}`))
		}))
		defer srv.Close()
		_, err := testEleven(srv).Speak(context.Background(), "hi", "v", "en")
		if err == nil || !strings.Contains(err.Error(), "voice not found") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("empty audio", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		defer srv.Close()
		_, err := testEleven(srv).Speak(context.Background(), "hi", "v", "en")
		if err == nil || !strings.Contains(err.Error(), "empty audio") {
			t.Fatalf("err = %v", err)
		}
	})
}
