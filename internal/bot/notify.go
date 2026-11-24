package bot

import (
	"context"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// ChatNotifier sends alert messages to a fixed set of chats.
type ChatNotifier struct {
	b       *Bot
	chatIDs []int64
}

// Notifier returns an alert.Notifier that posts to chatIDs.
func (b *Bot) Notifier(chatIDs []int64) *ChatNotifier {
	return &ChatNotifier{b: b, chatIDs: chatIDs}
}

// Notify sends text to every alert chat. Errors are logged, not returned:
// one unreachable chat must not block alerts to the others.
func (n *ChatNotifier) Notify(_ context.Context, text string) {
	for _, id := range n.chatIDs {
		if _, err := n.b.api.Send(tgbotapi.NewMessage(id, text)); err != nil {
			n.b.log.Error("send alert", "chat", id, "err", n.b.errText(err))
		}
	}
}
