package telegram

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/go-telegram/bot"
)

func TestSplitText(t *testing.T) {
	if got := splitText("short", 4096); len(got) != 1 || got[0] != "short" {
		t.Fatalf("short text must pass through, got %q", got)
	}
	para := strings.Repeat("слово ", 300) // 1800 runes
	text := para + "\n\n" + para + "\n\n" + para + "\n\n" + para
	parts := splitText(text, 4096)
	if len(parts) < 2 {
		t.Fatalf("want a split, got %d part(s)", len(parts))
	}
	var rebuilt []string
	for _, p := range parts {
		if n := utf8.RuneCountInString(p); n == 0 || n > 4096 {
			t.Fatalf("part of %d runes", n)
		}
		rebuilt = append(rebuilt, p)
	}
	if strings.Join(strings.Fields(strings.Join(rebuilt, " ")), " ") != strings.Join(strings.Fields(text), " ") {
		t.Fatal("split lost or reordered text")
	}
	// no whitespace at all: hard cut at the limit
	for _, p := range splitText(strings.Repeat("я", 9000), 4096) {
		if utf8.RuneCountInString(p) > 4096 {
			t.Fatal("hard cut exceeded the limit")
		}
	}
}

func testClient(t *testing.T, h http.HandlerFunc) *client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	b, err := bot.New("1:T", bot.WithServerURL(srv.URL), bot.WithSkipGetMe())
	if err != nil {
		t.Fatal(err)
	}
	return &client{b: b, httpc: &http.Client{Timeout: 5 * time.Second}}
}

// A 429 is retried once after the server's retry_after; the message goes out.
func TestSendTextRetriesAfter429(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if n == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"ok":false,"error_code":429,"description":"Too Many Requests: retry after 2","parameters":{"retry_after":2}}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1,"chat":{"id":5}}}`))
	})
	var waited time.Duration
	old := sleepFn
	sleepFn = func(_ context.Context, d time.Duration) { waited = d }
	defer func() { sleepFn = old }()

	if err := c.SendText(context.Background(), 5, "hi"); err != nil {
		t.Fatalf("SendText: %v", err)
	}
	if calls != 2 || waited != 2*time.Second {
		t.Fatalf("calls=%d waited=%v, want 2 calls after a 2s wait", calls, waited)
	}
}

// A long reply goes out as several messages, each within Telegram's limit.
func TestSendTextSplitsLongReplies(t *testing.T) {
	var mu sync.Mutex
	var sent []string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseMultipartForm(1 << 20)
		_ = r.ParseForm()
		mu.Lock()
		sent = append(sent, r.FormValue("text"))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1,"chat":{"id":5}}}`))
	})
	long := strings.Repeat("речення. ", 1200) // ~10 800 runes
	if err := c.SendText(context.Background(), 5, long); err != nil {
		t.Fatal(err)
	}
	if len(sent) < 3 {
		t.Fatalf("want >=3 messages, got %d", len(sent))
	}
	for _, s := range sent {
		if n := utf8.RuneCountInString(s); n == 0 || n > maxMessageRunes {
			t.Fatalf("message of %d runes", n)
		}
	}
}
