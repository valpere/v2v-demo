package telegram

import (
	"testing"

	"github.com/go-telegram/bot/models"
)

func TestToUpdate(t *testing.T) {
	tests := []struct {
		name string
		in   *models.Update
		want Update
		ok   bool
	}{
		{
			name: "text",
			in:   &models.Update{Message: &models.Message{Chat: models.Chat{ID: 5}, Text: "скільки коштує"}},
			want: Update{ChatID: 5, Text: "скільки коштує"},
			ok:   true,
		},
		{
			name: "start command",
			in:   &models.Update{Message: &models.Message{Chat: models.Chat{ID: 5}, Text: "/start"}},
			want: Update{ChatID: 5, Text: "/start", IsStart: true},
			ok:   true,
		},
		{
			name: "voice",
			in:   &models.Update{Message: &models.Message{Chat: models.Chat{ID: 7}, Voice: &models.Voice{FileID: "abc"}}},
			want: Update{ChatID: 7, VoiceFileID: "abc"},
			ok:   true,
		},
		{
			name: "voice wins over text caption",
			in:   &models.Update{Message: &models.Message{Chat: models.Chat{ID: 7}, Voice: &models.Voice{FileID: "abc"}, Text: "x"}},
			want: Update{ChatID: 7, VoiceFileID: "abc"},
			ok:   true,
		},
		{name: "nil message", in: &models.Update{}, ok: false},
		{name: "no chat", in: &models.Update{Message: &models.Message{Text: "hi"}}, ok: false},
		{name: "empty text", in: &models.Update{Message: &models.Message{Chat: models.Chat{ID: 5}, Text: "   "}}, ok: false},
		{name: "photo only", in: &models.Update{Message: &models.Message{Chat: models.Chat{ID: 5}}}, ok: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := toUpdate(tc.in)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v", ok, tc.ok)
			}
			if ok && got != tc.want {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestIsStartCommand(t *testing.T) {
	yes := []string{"/start", "/start@v2v_demo_bot", "/start deep-link-payload", "  /start  "}
	no := []string{"start", "/started", "/voice a", "hello /start", ""}
	for _, s := range yes {
		if !isStartCommand(s) {
			t.Errorf("isStartCommand(%q) = false, want true", s)
		}
	}
	for _, s := range no {
		if isStartCommand(s) {
			t.Errorf("isStartCommand(%q) = true, want false", s)
		}
	}
}

func TestSafeFilePath(t *testing.T) {
	ok := []string{"voice/file_1.oga", "music/file_12.ogg", "documents/x.bin"}
	bad := []string{"", "/etc/passwd", "../secret", "voice/../../x", "a//b", "./x"}
	for _, p := range ok {
		if !safeFilePath(p) {
			t.Errorf("safeFilePath(%q) = false, want true", p)
		}
	}
	for _, p := range bad {
		if safeFilePath(p) {
			t.Errorf("safeFilePath(%q) = true, want false", p)
		}
	}
}
