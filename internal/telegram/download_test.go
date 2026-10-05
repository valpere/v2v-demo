package telegram

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-telegram/bot"
)

// A failed voice download must not put the bot token into the error: the file
// URL is https://api.telegram.org/file/bot<TOKEN>/<path>, and net/http's
// *url.Error prints the whole URL. The error is logged by the caller.
func TestDownloadVoiceErrorDoesNotLeakToken(t *testing.T) {
	const token = "123456:FAKE-SECRET-TOKEN"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/getFile") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"result":{"file_id":"x","file_path":"voice/file_1.oga"}}`))
			return
		}
		// the file download: drop the connection mid-request
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			conn.Close()
		}
	}))
	defer srv.Close()

	b, err := bot.New(token, bot.WithServerURL(srv.URL), bot.WithSkipGetMe())
	if err != nil {
		t.Fatal(err)
	}
	c := &client{b: b, httpc: &http.Client{Timeout: 5 * time.Second}}

	_, err = c.DownloadVoice(context.Background(), "x")
	if err == nil {
		t.Fatal("expected a download error")
	}
	if strings.Contains(err.Error(), token) || strings.Contains(err.Error(), "FAKE-SECRET") {
		t.Fatalf("download error leaks the bot token: %v", err)
	}
	if !strings.Contains(err.Error(), "download") {
		t.Fatalf("error lost its context: %v", err)
	}
}
