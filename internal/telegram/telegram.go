// Package telegram is the transport layer: a long-poll getUpdates loop that
// yields normalised Update values, plus file download and the three send
// calls the bot needs. It holds no command, session, or dialogue logic
// (that lives in cmd/bot and internal/dialog).
package telegram

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// Update is one inbound message, reduced to what the bot acts on. Exactly one
// of Text / VoiceFileID / CallbackData is set; the others are empty.
type Update struct {
	ChatID      int64
	Text        string // empty for a voice-only message
	VoiceFileID string // empty for a text message
	IsStart     bool   // the message is the /start command

	CallbackData string // an inline-keyboard tap's payload; empty for anything else
	CallbackID   string // that tap's callback query id — AnswerCallback needs it
}

// Button is one inline-keyboard button: Label is what the user sees, Data is
// the payload echoed back on Update.CallbackData when tapped.
type Button struct {
	Label string
	Data  string
}

// Client is the transport contract cmd/bot depends on.
type Client interface {
	// Updates starts long-polling and returns a channel of normalised
	// updates. The channel closes when ctx is cancelled.
	Updates(ctx context.Context) (<-chan Update, error)
	// DownloadVoice fetches a voice file to a temp .ogg path; the caller
	// deletes it after use.
	DownloadVoice(ctx context.Context, fileID string) (oggPath string, err error)
	// SendVoice sends an OGG/Opus voice message — never with a caption.
	SendVoice(ctx context.Context, chatID int64, ogg []byte) error
	// SendText sends a plain text message.
	SendText(ctx context.Context, chatID int64, text string) error
	// SendRecordingAction shows the "recording voice" chat action
	// (server-side it lasts ~5s, so cmd/bot re-sends it on a ticker).
	SendRecordingAction(ctx context.Context, chatID int64) error
	// SendButtons sends text with an inline keyboard, one button per row
	// (a handful of topics never needs a denser layout).
	SendButtons(ctx context.Context, chatID int64, text string, buttons []Button) error
	// AnswerCallback acks an inline-keyboard tap — Telegram requires this or
	// the tapped button shows a spinner until it times out client-side.
	AnswerCallback(ctx context.Context, callbackID string) error
}

type client struct {
	b       *bot.Bot
	httpc   *http.Client
	updates chan Update
}

var _ Client = (*client)(nil)

// New constructs a Client. It calls getMe to validate the token, so it needs
// network and a live token.
func New(token string) (Client, error) {
	c := &client{httpc: &http.Client{Timeout: 90 * time.Second}}

	b, err := bot.New(token,
		bot.WithDefaultHandler(c.dispatch),
		bot.WithAllowedUpdates(bot.AllowedUpdates{"message", "callback_query"}),
		// One synchronous worker with a cap-1 buffer: the getUpdates loop
		// stalls on our blocking dispatch until cmd/bot pulls the update,
		// so at most ~2 updates are acked-but-undelivered on a crash
		// (REQ-BOT-04 — a repeat is acceptable, no persistence is added).
		bot.WithWorkers(1),
		bot.WithNotAsyncHandlers(),
		bot.WithUpdatesChannelCap(1),
	)
	if err != nil {
		return nil, fmt.Errorf("telegram: %w", err)
	}
	c.b = b
	return c, nil
}

func (c *client) Updates(ctx context.Context) (<-chan Update, error) {
	c.updates = make(chan Update)
	go func() {
		c.b.Start(ctx) // blocks until ctx is cancelled
		close(c.updates)
	}()
	return c.updates, nil
}

// dispatch is the go-telegram default handler: normalise and hand off, or drop.
func (c *client) dispatch(ctx context.Context, _ *bot.Bot, u *models.Update) {
	up, ok := toUpdate(u)
	if !ok {
		return
	}
	select {
	case c.updates <- up:
	case <-ctx.Done():
	}
}

func toUpdate(u *models.Update) (Update, bool) {
	if cq := u.CallbackQuery; cq != nil {
		// an inaccessible message (too old, or the chat became inaccessible)
		// carries no *Message — nothing to reply into, so drop it. The tap
		// itself is still lost without an AnswerCallback, but that is no
		// worse than any other message this boundary already drops.
		if cq.Message.Message == nil || cq.Message.Message.Chat.ID == 0 {
			return Update{}, false
		}
		return Update{ChatID: cq.Message.Message.Chat.ID, CallbackData: cq.Data, CallbackID: cq.ID}, true
	}

	m := u.Message
	if m == nil || m.Chat.ID == 0 {
		return Update{}, false
	}
	switch {
	case m.Voice != nil && m.Voice.FileID != "":
		return Update{ChatID: m.Chat.ID, VoiceFileID: m.Voice.FileID}, true
	case strings.TrimSpace(m.Text) != "":
		return Update{ChatID: m.Chat.ID, Text: m.Text, IsStart: isStartCommand(m.Text)}, true
	default:
		return Update{}, false // photos, stickers, etc. — not part of the demo
	}
}

func isStartCommand(text string) bool {
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return false
	}
	cmd := fields[0]
	if i := strings.IndexByte(cmd, '@'); i >= 0 { // "/start@my_bot"
		cmd = cmd[:i]
	}
	return cmd == "/start"
}

func (c *client) DownloadVoice(ctx context.Context, fileID string) (string, error) {
	f, err := c.b.GetFile(ctx, &bot.GetFileParams{FileID: fileID})
	if err != nil {
		return "", fmt.Errorf("telegram: getFile: %w", err)
	}
	if !safeFilePath(f.FilePath) {
		return "", fmt.Errorf("telegram: rejected file_path %q", f.FilePath)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.b.FileDownloadLink(f), nil)
	if err != nil {
		return "", err
	}
	resp, err := c.httpc.Do(req)
	if err != nil {
		return "", fmt.Errorf("telegram: download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("telegram: download: %s", resp.Status)
	}

	tmp, err := os.CreateTemp("", "v2v-voice-*.ogg")
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(tmp, resp.Body); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", fmt.Errorf("telegram: save: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return "", err
	}
	return tmp.Name(), nil
}

// safeFilePath guards the Telegram-supplied relative path before it is used to
// build a download URL: relative, cleaned, no "..".
func safeFilePath(p string) bool {
	if p == "" || strings.HasPrefix(p, "/") || strings.Contains(p, "..") {
		return false
	}
	return path.Clean(p) == p
}

func (c *client) SendText(ctx context.Context, chatID int64, text string) error {
	if _, err := c.b.SendMessage(ctx, &bot.SendMessageParams{ChatID: chatID, Text: text}); err != nil {
		return fmt.Errorf("telegram: sendMessage: %w", err)
	}
	return nil
}

func (c *client) SendVoice(ctx context.Context, chatID int64, ogg []byte) error {
	_, err := c.b.SendVoice(ctx, &bot.SendVoiceParams{
		ChatID: chatID,
		Voice:  &models.InputFileUpload{Filename: "reply.ogg", Data: bytes.NewReader(ogg)},
	})
	if err != nil {
		return fmt.Errorf("telegram: sendVoice: %w", err)
	}
	return nil
}

func (c *client) SendRecordingAction(ctx context.Context, chatID int64) error {
	_, err := c.b.SendChatAction(ctx, &bot.SendChatActionParams{
		ChatID: chatID,
		Action: models.ChatActionRecordVoice,
	})
	if err != nil {
		return fmt.Errorf("telegram: sendChatAction: %w", err)
	}
	return nil
}

func (c *client) SendButtons(ctx context.Context, chatID int64, text string, buttons []Button) error {
	rows := make([][]models.InlineKeyboardButton, len(buttons))
	for i, b := range buttons {
		rows[i] = []models.InlineKeyboardButton{{Text: b.Label, CallbackData: b.Data}}
	}
	_, err := c.b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      chatID,
		Text:        text,
		ReplyMarkup: models.InlineKeyboardMarkup{InlineKeyboard: rows},
	})
	if err != nil {
		return fmt.Errorf("telegram: sendMessage (buttons): %w", err)
	}
	return nil
}

func (c *client) AnswerCallback(ctx context.Context, callbackID string) error {
	if _, err := c.b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{CallbackQueryID: callbackID}); err != nil {
		return fmt.Errorf("telegram: answerCallbackQuery: %w", err)
	}
	return nil
}
