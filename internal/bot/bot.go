// Package bot implements the Telegram side: receiving commands from the
// family chat and translating them into Aqara API calls.
package bot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/srgkharkov/domovoy_alert_bot/internal/aqara"
)

// connectAttempts is how many times New retries getMe on network errors.
const connectAttempts = 6

// SmartHome is what the bot needs from the Aqara client (handy for tests).
type SmartHome interface {
	ListDevices(ctx context.Context) ([]aqara.Device, error)
	ListPositions(ctx context.Context, parentID string) ([]aqara.Position, error)
	ListScenes(ctx context.Context, positionID string) ([]aqara.Scene, error)
	RunScene(ctx context.Context, sceneID string) error
	ReadResources(ctx context.Context, queries []aqara.ResourceQuery) ([]aqara.ResourceValue, error)
	ResourceNames(ctx context.Context, dids []string) ([]aqara.ResourceName, error)
	WriteResource(ctx context.Context, did, resourceID, value string) error
}

// Bot is the Telegram bot.
type Bot struct {
	api     *tgbotapi.BotAPI
	home    SmartHome
	allowed map[int64]bool
	log     *slog.Logger
	redact  *strings.Replacer

	// Access requests (optional, see EnableAccessRequests). Updates are
	// handled sequentially, so pending maps need no locking.
	adminChatID  int64
	access       *AccessStore
	pending      map[int64]bool   // chats with a request awaiting the admin
	pendingTitle map[int64]string // their display titles
}

// New connects to Telegram and returns a bot. allowedChatIDs restricts who can
// control the home; if empty, the bot only answers /chatid (setup mode).
// Transient network errors on startup are retried with exponential backoff.
func New(ctx context.Context, token string, home SmartHome, allowedChatIDs []int64, log *slog.Logger) (*Bot, error) {
	// The Bot API token is part of every request URL, so it leaks into
	// net/http errors. Scrub it from everything we and the library log.
	redact := strings.NewReplacer(token, "<TOKEN>")
	tgbotapi.SetLogger(libLogger{log: log, redact: redact})

	var api *tgbotapi.BotAPI
	var err error
	delay := 2 * time.Second
	for attempt := 1; ; attempt++ {
		api, err = tgbotapi.NewBotAPI(token)
		if err == nil {
			break
		}
		var tgErr *tgbotapi.Error
		if errors.As(err, &tgErr) || attempt == connectAttempts {
			// API-level error (e.g. 401 bad token) or out of attempts.
			return nil, errors.New(redact.Replace(err.Error()))
		}
		log.Warn("telegram connect failed, retrying", "attempt", attempt, "in", delay, "err", redact.Replace(err.Error()))
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
		delay *= 2
	}

	allowed := make(map[int64]bool, len(allowedChatIDs))
	for _, id := range allowedChatIDs {
		allowed[id] = true
	}
	log.Info("telegram authorized", "bot", api.Self.UserName, "allowed_chats", len(allowed))
	return &Bot{
		api: api, home: home, allowed: allowed, log: log, redact: redact,
		pending: map[int64]bool{}, pendingTitle: map[int64]string{},
	}, nil
}

// errText returns err as a string with the bot token removed.
func (b *Bot) errText(err error) string {
	if err == nil {
		return ""
	}
	return b.redact.Replace(err.Error())
}

// libLogger routes telegram-bot-api's internal logging (e.g. getUpdates
// retries) to slog, with the token scrubbed.
type libLogger struct {
	log    *slog.Logger
	redact *strings.Replacer
}

func (l libLogger) Println(v ...any) {
	l.log.Warn("telegram", "msg", l.redact.Replace(strings.TrimSpace(fmt.Sprintln(v...))))
}

func (l libLogger) Printf(format string, v ...any) {
	l.log.Warn("telegram", "msg", l.redact.Replace(fmt.Sprintf(format, v...)))
}

// Run processes updates until ctx is cancelled.
func (b *Bot) Run(ctx context.Context) error {
	if _, err := b.api.Request(tgbotapi.NewSetMyCommands(commandList()...)); err != nil {
		b.log.Warn("set commands menu", "err", b.errText(err))
	}

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := b.api.GetUpdatesChan(u)

	for {
		select {
		case <-ctx.Done():
			b.api.StopReceivingUpdates()
			return nil
		case upd := <-updates:
			b.handleUpdate(ctx, upd)
		}
	}
}

func (b *Bot) handleUpdate(ctx context.Context, upd tgbotapi.Update) {
	defer func() {
		if r := recover(); r != nil {
			b.log.Error("panic in handler", "panic", r)
		}
	}()

	switch {
	case upd.Message != nil && upd.Message.IsCommand():
		b.handleCommand(ctx, upd.Message)
	case upd.Message != nil && upd.Message.Chat.IsPrivate() && !b.isAllowed(upd.Message.Chat.ID):
		// A stranger writing to the bot directly, without a command.
		b.handleUnknownChat(upd.Message)
	case upd.CallbackQuery != nil:
		b.handleCallback(ctx, upd.CallbackQuery)
	}
}

// isAllowed reports whether a chat may control the home: listed in the
// config, the admin's own chat, or approved by the admin.
func (b *Bot) isAllowed(chatID int64) bool {
	if b.allowed[chatID] {
		return true
	}
	if b.access == nil {
		return false
	}
	return chatID == b.adminChatID || b.access.IsApproved(chatID)
}

func (b *Bot) reply(chatID int64, text string) {
	if _, err := b.api.Send(tgbotapi.NewMessage(chatID, text)); err != nil {
		b.log.Error("send message", "chat", chatID, "err", b.errText(err))
	}
}
