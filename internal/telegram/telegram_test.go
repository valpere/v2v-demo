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
		// 16h — non-text, non-voice attachments are dropped at the boundary
		{name: "document (pdf)", in: &models.Update{Message: &models.Message{Chat: models.Chat{ID: 5}, Document: &models.Document{FileID: "d"}}}, ok: false},
		{name: "sticker", in: &models.Update{Message: &models.Message{Chat: models.Chat{ID: 5}, Sticker: &models.Sticker{FileID: "s"}}}, ok: false},
		{name: "video note", in: &models.Update{Message: &models.Message{Chat: models.Chat{ID: 5}, VideoNote: &models.VideoNote{FileID: "v"}}}, ok: false},
		{name: "audio mp3", in: &models.Update{Message: &models.Message{Chat: models.Chat{ID: 5}, Audio: &models.Audio{FileID: "a"}}}, ok: false},
		{name: "photo with caption (caption is not Text)", in: &models.Update{Message: &models.Message{Chat: models.Chat{ID: 5}, Photo: []models.PhotoSize{{FileID: "p"}}, Caption: "переклад"}}, ok: false},
		{name: "edited message arrives on EditedMessage, not Message", in: &models.Update{EditedMessage: &models.Message{Chat: models.Chat{ID: 5}, Text: "fixed typo"}}, ok: false},
		{
			name: "callback query",
			in: &models.Update{CallbackQuery: &models.CallbackQuery{
				ID:      "cbid1",
				Data:    "topic:translations",
				Message: models.MaybeInaccessibleMessage{Message: &models.Message{Chat: models.Chat{ID: 9}}},
			}},
			want: Update{ChatID: 9, CallbackData: "topic:translations", CallbackID: "cbid1"},
			ok:   true,
		},
		{
			// still routed (and, in the real bot, still acked) even though
			// there's no live *Message to reply into — InaccessibleMessage
			// carries the chat id, and skipping AnswerCallback here would
			// leave the tapped button spinning for ~10s.
			name: "callback query on an inaccessible message",
			in: &models.Update{CallbackQuery: &models.CallbackQuery{
				ID:   "cbid2",
				Data: "topic:translations",
				Message: models.MaybeInaccessibleMessage{
					InaccessibleMessage: &models.InaccessibleMessage{Chat: models.Chat{ID: 9}},
				},
			}},
			want: Update{ChatID: 9, CallbackData: "topic:translations", CallbackID: "cbid2"},
			ok:   true,
		},
		{
			name: "callback query with neither Message nor InaccessibleMessage",
			in: &models.Update{CallbackQuery: &models.CallbackQuery{
				ID:   "cbid3",
				Data: "topic:translations",
			}},
			ok: false,
		},
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
