package bot

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/srgkharkov/domovoy_alert_bot/internal/home"
)

// Callback data "home:<cmd>:<homeID>" runs a per-home command after the user
// picked a home (chats bound to several homes, e.g. the admin's).
const callbackHomePrefix = "home:"

// homesFor returns the homes a chat may control, in configuration order:
//   - the admin chat: all homes;
//   - TELEGRAM_ALLOWED_CHAT_IDS: the Aqara home;
//   - chats approved by the admin: the homes chosen at approval.
func (b *Bot) homesFor(chatID int64) []home.Home {
	ids := map[string]bool{}
	if b.access != nil && chatID == b.adminChatID {
		return b.homes
	}
	if b.allowed[chatID] {
		ids[home.AqaraID] = true
	}
	if b.access != nil {
		if hs, ok := b.access.Homes(chatID); ok {
			if len(hs) == 0 {
				// Approved before multi-home support: the Aqara home.
				hs = []string{home.AqaraID}
			}
			for _, id := range hs {
				ids[id] = true
			}
		}
	}
	var out []home.Home
	for _, h := range b.homes {
		if ids[h.ID()] {
			out = append(out, h)
		}
	}
	return out
}

// hasHome reports whether the chat may control the given home.
func (b *Bot) hasHome(chatID int64, homeID string) bool {
	for _, h := range b.homesFor(chatID) {
		if h.ID() == homeID {
			return true
		}
	}
	return false
}

// homeName returns the display name of a home ID, or the ID itself.
func (b *Bot) homeName(id string) string {
	if h, ok := b.byID[id]; ok {
		return h.Name()
	}
	return id
}

// perHomeCommands run against one home; with several homes the bot asks which.
var perHomeCommands = map[string]func(b *Bot, ctx context.Context, chatID int64, h home.Home){
	"devices": (*Bot).cmdDevices,
	"status":  (*Bot).cmdStatus,
}

// runPerHome runs cmd directly if the chat has one home, otherwise shows a
// home picker.
func (b *Bot) runPerHome(ctx context.Context, chatID int64, cmd string) {
	homes := b.homesFor(chatID)
	if len(homes) == 1 {
		perHomeCommands[cmd](b, ctx, chatID, homes[0])
		return
	}
	var row []tgbotapi.InlineKeyboardButton
	for _, h := range homes {
		row = append(row, tgbotapi.NewInlineKeyboardButtonData("🏠 "+h.Name(), callbackHomePrefix+cmd+":"+h.ID()))
	}
	msg := tgbotapi.NewMessage(chatID, "Какой дом?")
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(row)
	if _, err := b.api.Send(msg); err != nil {
		b.log.Error("send home picker", "err", b.errText(err))
	}
}

func (b *Bot) handleHomeCallback(ctx context.Context, q *tgbotapi.CallbackQuery) {
	chatID := q.Message.Chat.ID
	cmd, homeID, ok := strings.Cut(strings.TrimPrefix(q.Data, callbackHomePrefix), ":")
	run, known := perHomeCommands[cmd]
	if !ok || !known || !b.hasHome(chatID, homeID) {
		b.answerCallback(q.ID, "⛔ Нет доступа к этому дому")
		return
	}
	b.answerCallback(q.ID, "")
	run(b, ctx, chatID, b.byID[homeID])
}

func (b *Bot) cmdDevices(ctx context.Context, chatID int64, h home.Home) {
	devices, err := h.Devices(ctx)
	if err != nil {
		b.replyError(chatID, err)
		return
	}
	if len(devices) == 0 {
		b.reply(chatID, fmt.Sprintf("🏠 %s: устройств не найдено.", h.Name()))
		return
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "🏠 %s — устройства (%d):\n\n", h.Name(), len(devices))
	for _, d := range devices {
		status := "🟢"
		if !d.Online {
			status = "🔴"
		}
		fmt.Fprintf(&sb, "%s %s — %s\n", status, d.Name, d.Model)
	}
	b.reply(chatID, sb.String())
}

func (b *Bot) cmdStatus(ctx context.Context, chatID int64, h home.Home) {
	readings, err := h.Readings(ctx)
	if errors.Is(err, home.ErrNotSupported) {
		b.reply(chatID, fmt.Sprintf("🏠 %s: показания датчиков для этого дома пока не поддерживаются.", h.Name()))
		return
	}
	if err != nil {
		b.replyError(chatID, err)
		return
	}
	b.reply(chatID, formatReadings(h.Name(), readings))
}

// formatReadings renders one line per device, e.g.
// "🌡 Кухня: 12.1 °C · 💧 56 % · 🔋 73 %".
func formatReadings(homeName string, readings []home.Reading) string {
	if len(readings) == 0 {
		return fmt.Sprintf("🏠 %s: датчиков с показаниями нет.", homeName)
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "🏠 %s\n\n", homeName)
	for i := 0; i < len(readings); {
		j := i
		for j < len(readings) && readings[j].DeviceID == readings[i].DeviceID {
			j++
		}
		group := readings[i:j]
		icon := "•"
		var parts []string
		for _, r := range group {
			switch r.Kind {
			case home.Temperature:
				icon = "🌡"
				parts = append(parts, formatNum(r.Value, 1)+" °C")
			case home.Humidity:
				parts = append(parts, "💧 "+formatNum(r.Value, 0)+" %")
			case home.Battery:
				parts = append(parts, "🔋 "+formatNum(r.Value, 0)+" %")
			case home.Power:
				if icon == "•" {
					icon = "⚡"
				}
				parts = append(parts, formatNum(r.Value, 0)+" Вт")
			case home.Energy:
				if icon == "•" {
					icon = "⚡"
				}
				parts = append(parts, "счётчик "+formatNum(r.Value, 2)+" кВт·ч")
			}
		}
		line := fmt.Sprintf("%s %s: %s", icon, group[0].Device, strings.Join(parts, " · "))
		if !group[0].Online {
			line += " (офлайн)"
		}
		sb.WriteString(line + "\n")
		i = j
	}
	return sb.String()
}

// formatNum prints a number with a fixed number of decimals and a decimal
// comma, as is customary in Russian.
func formatNum(v float64, decimals int) string {
	return strings.Replace(strconv.FormatFloat(v, 'f', decimals, 64), ".", ",", 1)
}
