package bot

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/srgkharkov/domovoy_alert_bot/internal/aqara"
)

const (
	callbackScenePrefix = "scene:"
	apiTimeout          = 20 * time.Second
)

func commandList() []tgbotapi.BotCommand {
	return []tgbotapi.BotCommand{
		{Command: "light", Description: "Пульт: свет и розетки"},
		{Command: "devices", Description: "Список устройств и их статус"},
		{Command: "scenes", Description: "Запустить сценарий"},
		{Command: "chatid", Description: "Показать ID этого чата"},
		{Command: "help", Description: "Справка"},
	}
}

const helpText = `Домовой на связи 🏠

/light — пульт: свет и розетки
/devices — список устройств Aqara и их статус
/scenes — список сценариев (нажмите кнопку, чтобы запустить)
/chatid — ID этого чата (для настройки)
/access — кто имеет доступ (только администратор)
/help — эта справка`

func (b *Bot) handleCommand(ctx context.Context, m *tgbotapi.Message) {
	chatID := m.Chat.ID
	cmd := m.Command()
	b.log.Info("command", "cmd", cmd, "chat", chatID, "user", m.From.UserName)

	// /chatid works everywhere: it is needed to fill TELEGRAM_ALLOWED_CHAT_IDS.
	if cmd == "chatid" {
		b.reply(chatID, fmt.Sprintf("ID этого чата: %d", chatID))
		return
	}
	if !b.isAllowed(chatID) {
		b.handleUnknownChat(m)
		return
	}

	ctx, cancel := context.WithTimeout(ctx, apiTimeout)
	defer cancel()

	switch cmd {
	case "start", "help":
		b.reply(chatID, helpText)
	case "light":
		b.cmdLight(ctx, chatID)
	case "devices":
		b.cmdDevices(ctx, chatID)
	case "scenes":
		b.cmdScenes(ctx, chatID)
	case "access":
		b.cmdAccess(chatID)
	default:
		b.reply(chatID, "Не знаю такой команды. /help")
	}
}

func (b *Bot) cmdDevices(ctx context.Context, chatID int64) {
	devices, err := b.home.ListDevices(ctx)
	if err != nil {
		b.replyError(chatID, err)
		return
	}
	if len(devices) == 0 {
		b.reply(chatID, "Устройств не найдено.")
		return
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Устройства (%d):\n\n", len(devices))
	for _, d := range devices {
		status := "🟢"
		if !d.Online() {
			status = "🔴"
		}
		fmt.Fprintf(&sb, "%s %s — %s\n", status, d.DeviceName, d.Model)
	}
	b.reply(chatID, sb.String())
}

func (b *Bot) cmdScenes(ctx context.Context, chatID int64) {
	positions, err := b.home.ListPositions(ctx, "")
	if err != nil {
		b.replyError(chatID, err)
		return
	}

	var rows [][]tgbotapi.InlineKeyboardButton
	for _, p := range positions {
		scenes, err := b.home.ListScenes(ctx, p.PositionID)
		if err != nil {
			b.replyError(chatID, err)
			return
		}
		for _, s := range scenes {
			rows = append(rows, tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("▶️ "+s.Name, callbackScenePrefix+s.SceneID),
			))
		}
	}
	if len(rows) == 0 {
		b.reply(chatID, "Сценариев не найдено. Создайте их в приложении Aqara Home.")
		return
	}

	msg := tgbotapi.NewMessage(chatID, "Выберите сценарий:")
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(rows...)
	if _, err := b.api.Send(msg); err != nil {
		b.log.Error("send scenes", "err", b.errText(err))
	}
}

func (b *Bot) handleCallback(ctx context.Context, q *tgbotapi.CallbackQuery) {
	if q.Message == nil {
		return
	}
	chatID := q.Message.Chat.ID
	if strings.HasPrefix(q.Data, callbackAccessPrefix) {
		b.handleAccessCallback(q)
		return
	}
	if !b.isAllowed(chatID) {
		b.answerCallback(q.ID, "⛔ Чат не авторизован")
		return
	}

	ctx, cancel := context.WithTimeout(ctx, apiTimeout)
	defer cancel()

	if strings.HasPrefix(q.Data, callbackSwitchPrefix) {
		b.handleSwitchCallback(ctx, q, q.Data)
		return
	}

	sceneID, ok := strings.CutPrefix(q.Data, callbackScenePrefix)
	if !ok {
		b.answerCallback(q.ID, "")
		return
	}

	b.log.Info("run scene", "scene", sceneID, "chat", chatID, "user", q.From.UserName)
	if err := b.home.RunScene(ctx, sceneID); err != nil {
		b.answerCallback(q.ID, "Ошибка запуска сценария")
		b.replyError(chatID, err)
		return
	}
	b.answerCallback(q.ID, "✅ Сценарий запущен")
	b.reply(chatID, fmt.Sprintf("✅ %s запустил(а) сценарий", displayName(q.From)))
}

func (b *Bot) answerCallback(id, text string) {
	if _, err := b.api.Request(tgbotapi.NewCallback(id, text)); err != nil {
		b.log.Error("answer callback", "err", b.errText(err))
	}
}

func (b *Bot) replyError(chatID int64, err error) {
	b.log.Error("aqara request failed", "chat", chatID, "err", b.errText(err))
	if errors.Is(err, aqara.ErrNoToken) {
		b.reply(chatID, "⚠️ Бот не авторизован в Aqara. См. docs/setup.md, шаг «Авторизация Aqara».")
		return
	}
	b.reply(chatID, "⚠️ Не удалось выполнить запрос к Aqara. Подробности в логах.")
}

func displayName(u *tgbotapi.User) string {
	if u == nil {
		return "Кто-то"
	}
	if u.FirstName != "" {
		return u.FirstName
	}
	return "@" + u.UserName
}
