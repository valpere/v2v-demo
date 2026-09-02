package tts

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func azureStub(t *testing.T, h http.HandlerFunc) *azureTTS {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &azureTTS{key: "az-key", region: "westeurope", baseURL: srv.URL, hc: &http.Client{Timeout: 5 * time.Second}}
}

func TestAzureSpeak(t *testing.T) {
	var gotKey, gotFmt, gotCT, gotUA, gotBody string
	a := azureStub(t, func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("Ocp-Apim-Subscription-Key")
		gotFmt = r.Header.Get("X-Microsoft-OutputFormat")
		gotCT = r.Header.Get("Content-Type")
		gotUA = r.Header.Get("User-Agent")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Write([]byte("OggS\x00azure-opus"))
	})

	audio, err := a.Speak(context.Background(), "Ціна — 45 & більше", "uk-UA-PolinaNeural", "uk")
	if err != nil {
		t.Fatalf("Speak: %v", err)
	}
	if !strings.HasPrefix(string(audio), "OggS") {
		t.Fatalf("audio = %q", audio)
	}
	if gotKey != "az-key" || gotFmt != "ogg-48khz-16bit-mono-opus" || gotCT != "application/ssml+xml" || gotUA == "" {
		t.Errorf("headers: key=%q fmt=%q ct=%q ua=%q", gotKey, gotFmt, gotCT, gotUA)
	}
	if !strings.Contains(gotBody, "xml:lang='uk-UA'") ||
		!strings.Contains(gotBody, "name='uk-UA-PolinaNeural'") ||
		!strings.Contains(gotBody, "45 &amp; більше") { // XML-escaped
		t.Errorf("ssml = %q", gotBody)
	}
}

func TestAzureErrors(t *testing.T) {
	t.Run("empty voice", func(t *testing.T) {
		_, err := NewAzure("k", "r").Speak(context.Background(), "hi", "", "uk")
		if err == nil {
			t.Fatal("want error")
		}
	})
	t.Run("non-200", func(t *testing.T) {
		a := azureStub(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("Unsupported voice"))
		})
		_, err := a.Speak(context.Background(), "hi", "v", "uk")
		if err == nil || !strings.Contains(err.Error(), "Unsupported voice") {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestAzureRetry(t *testing.T) {
	t.Run("retries past a 500", func(t *testing.T) {
		var n int
		a := azureStub(t, func(w http.ResponseWriter, r *http.Request) {
			if n++; n == 1 {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			w.Write([]byte("OggS\x00ok"))
		})
		audio, err := a.Speak(context.Background(), "hi", "uk-UA-PolinaNeural", "uk")
		if err != nil || !strings.HasPrefix(string(audio), "OggS") {
			t.Fatalf("audio=%q err=%v", audio, err)
		}
		if n != 2 {
			t.Fatalf("calls = %d, want 2", n)
		}
	})

	t.Run("no retry on 4xx", func(t *testing.T) {
		var n int
		a := azureStub(t, func(w http.ResponseWriter, r *http.Request) {
			n++
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte("bad key"))
		})
		_, err := a.Speak(context.Background(), "hi", "v", "uk")
		if err == nil || !strings.Contains(err.Error(), "bad key") {
			t.Fatalf("err = %v", err)
		}
		if n != 1 {
			t.Fatalf("calls = %d, want 1 (4xx must not retry)", n)
		}
	})

	t.Run("gives up after two 429s", func(t *testing.T) {
		var n int
		a := azureStub(t, func(w http.ResponseWriter, r *http.Request) {
			n++
			w.WriteHeader(http.StatusTooManyRequests)
		})
		_, err := a.Speak(context.Background(), "hi", "v", "uk")
		if err == nil {
			t.Fatal("want error after two 429s")
		}
		if n != 2 {
			t.Fatalf("calls = %d, want 2", n)
		}
	})
}

func TestSSMLLang(t *testing.T) {
	if ssmlLang("uk-UA-OstapNeural", "en") != "uk-UA" {
		t.Error("voice locale should win over lang")
	}
	if ssmlLang("Sarah", "uk") != "uk-UA" || ssmlLang("Sarah", "en") != "en-US" {
		t.Error("non-locale voice id should fall back to lang")
	}
}

func TestEscapeXML(t *testing.T) {
	got := escapeXML(`a & b < c > d " e ' f`)
	want := `a &amp; b &lt; c &gt; d &quot; e &apos; f`
	if got != want {
		t.Fatalf("got %q", got)
	}
}
