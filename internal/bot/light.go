package bot

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/srgkharkov/domovoy_alert_bot/internal/aqara"
)

// Callback data: "sw:<did>|<resourceId>" toggles a channel, "sw:refresh" redraws.
const (
	callbackSwitchPrefix  = "sw:"
	callbackSwitchRefresh = callbackSwitchPrefix + "refresh"
)

// switchChannels lists on/off resources per device model.
// Values: "0" = off, "1" = on (see query.resource.info for the model).
var switchChannels = map[string][]string{
	"lumi.relay.c2acn01": {"4.1.85", "4.2.85"}, // Aqara relay T1/LLKZMK11LM, 2 channels
	"lumi.plug.maeu01":   {"4.1.85"},           // Aqara smart plug EU
}

// defaultChannelName matches names the Aqara app assigns automatically,
// which carry no information ("переключатель 1", "Channel 2", ...).
var defaultChannelName = regexp.MustCompile(`(?i)^\s*(переключатель|канал|switch|channel)?\s*\d*\s*$`)

// switchChannel is one button on the light panel.
type switchChannel struct {
	DID        string
	ResourceID string
	Label      string
	On         bool
}

// loadSwitches returns all on/off channels of online devices with their state.
func (b *Bot) loadSwitches(ctx context.Context) ([]switchChannel, error) {
	devices, err := b.home.ListDevices(ctx)
	if err != nil {
		return nil, err
	}

	var (
		queries []aqara.ResourceQuery
		dids    []string
		byDID   = map[string]aqara.Device{}
	)
	for _, d := range devices {
		ids, ok := switchChannels[d.Model]
		if !ok || !d.Online() {
			continue
		}
		queries = append(queries, aqara.ResourceQuery{SubjectID: d.DID, ResourceIDs: ids})
		dids = append(dids, d.DID)
		byDID[d.DID] = d
	}
	if len(queries) == 0 {
		return nil, nil
	}

	values, err := b.home.ReadResources(ctx, queries)
	if err != nil {
		return nil, err
	}
	state := map[string]bool{}
	for _, v := range values {
		state[v.SubjectID+"|"+v.ResourceID] = v.Value == "1"
	}

	// Channel names are optional: without them we fall back to "Device · N".
	names := map[string]string{}
	if rn, err := b.home.ResourceNames(ctx, dids); err == nil {
		for _, n := range rn {
			names[n.SubjectID+"|"+n.ResourceID] = n.Name
		}
	} else {
		b.log.Warn("resource names", "err", err)
	}

	var out []switchChannel
	for _, q := range queries {
		d := byDID[q.SubjectID]
		for i, rid := range q.ResourceIDs {
			key := d.DID + "|" + rid
			out = append(out, switchChannel{
				DID:        d.DID,
				ResourceID: rid,
				Label:      channelLabel(d.DeviceName, names[key], i+1, len(q.ResourceIDs)),
				On:         state[key],
			})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Label < out[j].Label })
	return out, nil
}

// channelLabel builds a button caption from the device and channel names.
func channelLabel(device, channel string, n, total int) string {
	if channel != "" && !defaultChannelName.MatchString(channel) {
		if total == 1 {
			return channel
		}
		return device + " · " + channel
	}
	if total == 1 {
		return device
	}
	return fmt.Sprintf("%s · %d", device, n)
}

func lightKeyboard(switches []switchChannel) tgbotapi.InlineKeyboardMarkup {
	rows := make([][]tgbotapi.InlineKeyboardButton, 0, len(switches)+1)
	for _, s := range switches {
		icon := "⚫"
		if s.On {
			icon = "🟡"
		}
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData(
			icon+" "+s.Label, callbackSwitchPrefix+s.DID+"|"+s.ResourceID)))
	}
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("🔄 Обновить", callbackSwitchRefresh)))
	return tgbotapi.NewInlineKeyboardMarkup(rows...)
}

func lightText(footer string) string {
	text := "💡 Свет и розетки\n🟡 включено · ⚫ выключено"
	if footer != "" {
		text += "\n\n" + footer
	}
	return text
}

// cmdLight sends the light control panel.
func (b *Bot) cmdLight(ctx context.Context, chatID int64) {
	switches, err := b.loadSwitches(ctx)
	if err != nil {
		b.replyError(chatID, err)
		return
	}
	if len(switches) == 0 {
		b.reply(chatID, "Нет доступных выключателей (или все не в сети).")
		return
	}
	msg := tgbotapi.NewMessage(chatID, lightText(""))
	msg.ReplyMarkup = lightKeyboard(switches)
	if _, err := b.api.Send(msg); err != nil {
		b.log.Error("send light panel", "err", b.errText(err))
	}
}

// handleSwitchCallback toggles a channel and redraws the panel in place.
func (b *Bot) handleSwitchCallback(ctx context.Context, q *tgbotapi.CallbackQuery, data string) {
	chatID, msgID := q.Message.Chat.ID, q.Message.MessageID
	footer := ""
	var did, rid string
	var turnOn bool

	if data != callbackSwitchRefresh {
		var ok bool
		did, rid, ok = strings.Cut(strings.TrimPrefix(data, callbackSwitchPrefix), "|")
		if !ok {
			b.answerCallback(q.ID, "")
			return
		}

		// Read the current state right before writing: the panel may be stale.
		vals, err := b.home.ReadResources(ctx, []aqara.ResourceQuery{{SubjectID: did, ResourceIDs: []string{rid}}})
		if err != nil || len(vals) == 0 {
			b.answerCallback(q.ID, "⚠️ Устройство не отвечает")
			b.log.Error("read switch state", "did", did, "err", err)
			return
		}
		turnOn = vals[0].Value != "1"
		newValue, verb := "0", "выключил(а)"
		if turnOn {
			newValue, verb = "1", "включил(а)"
		}

		b.log.Info("switch", "did", did, "resource", rid, "value", newValue, "chat", chatID, "user", q.From.UserName)
		if err := b.home.WriteResource(ctx, did, rid, newValue); err != nil {
			b.answerCallback(q.ID, "⚠️ Не удалось переключить")
			b.log.Error("write switch", "did", did, "err", err)
			return
		}
		b.answerCallback(q.ID, "✅ Готово")
		footer = fmt.Sprintf("%s %s %s: %s", time.Now().Format("15:04"), displayName(q.From), verb, b.switchLabel(q.Message, data))
	} else {
		b.answerCallback(q.ID, "🔄")
	}

	switches, err := b.loadSwitches(ctx)
	if err != nil {
		b.log.Error("reload switches", "err", err)
		return
	}
	// The cloud may report the old value for a moment after a write;
	// trust what we just wrote for the pressed button.
	for i := range switches {
		if switches[i].DID == did && switches[i].ResourceID == rid {
			switches[i].On = turnOn
		}
	}

	edit := tgbotapi.NewEditMessageTextAndMarkup(chatID, msgID, lightText(footer), lightKeyboard(switches))
	if _, err := b.api.Send(edit); err != nil && !strings.Contains(err.Error(), "message is not modified") {
		b.log.Error("edit light panel", "err", b.errText(err))
	}
}

// switchLabel finds the caption of the pressed button in the original message.
func (b *Bot) switchLabel(m *tgbotapi.Message, data string) string {
	if m.ReplyMarkup != nil {
		for _, row := range m.ReplyMarkup.InlineKeyboard {
			for _, btn := range row {
				if btn.CallbackData != nil && *btn.CallbackData == data {
					_, label, _ := strings.Cut(btn.Text, " ")
					return label
				}
			}
		}
	}
	return "выключатель"
}
