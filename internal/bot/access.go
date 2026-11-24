package bot

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

const (
	callbackAccessPrefix = "acc:" // acc:<action>:<chatID>, action = ok | no | rm
	accessApprove        = "ok"
	accessDeny           = "no"
	accessRevoke         = "rm"
)

// AccessEntry is a chat the admin has decided on.
type AccessEntry struct {
	ChatID    int64     `json:"chat_id"`
	Title     string    `json:"title"`
	DecidedAt time.Time `json:"decided_at"`
}

type accessData struct {
	Approved []AccessEntry `json:"approved"`
	Denied   []AccessEntry `json:"denied"`
}

// AccessStore persists the admin's approve/deny decisions in a JSON file.
// It is safe for concurrent use.
type AccessStore struct {
	path string
	now  func() time.Time

	mu       sync.Mutex
	approved map[int64]AccessEntry
	denied   map[int64]AccessEntry
}

// OpenAccessStore loads decisions from path; a missing file means none yet.
func OpenAccessStore(path string) (*AccessStore, error) {
	s := &AccessStore{
		path:     path,
		now:      time.Now,
		approved: map[int64]AccessEntry{},
		denied:   map[int64]AccessEntry{},
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	var d accessData
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	for _, e := range d.Approved {
		s.approved[e.ChatID] = e
	}
	for _, e := range d.Denied {
		s.denied[e.ChatID] = e
	}
	return s, nil
}

func (s *AccessStore) IsApproved(chatID int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.approved[chatID]
	return ok
}

func (s *AccessStore) IsDenied(chatID int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.denied[chatID]
	return ok
}

// Approve grants access (and clears an earlier denial).
func (s *AccessStore) Approve(chatID int64, title string) error {
	return s.update(func() {
		delete(s.denied, chatID)
		s.approved[chatID] = AccessEntry{ChatID: chatID, Title: title, DecidedAt: s.now()}
	})
}

// Deny rejects a request; further messages from the chat no longer reach the admin.
func (s *AccessStore) Deny(chatID int64, title string) error {
	return s.update(func() {
		delete(s.approved, chatID)
		s.denied[chatID] = AccessEntry{ChatID: chatID, Title: title, DecidedAt: s.now()}
	})
}

// Revoke removes access; the chat may send a new request afterwards.
// It returns the removed entry, if any.
func (s *AccessStore) Revoke(chatID int64) (AccessEntry, bool, error) {
	var e AccessEntry
	var ok bool
	err := s.update(func() {
		e, ok = s.approved[chatID]
		delete(s.approved, chatID)
	})
	return e, ok, err
}

// Approved lists approved chats, oldest first.
func (s *AccessStore) Approved() []AccessEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	return sortedEntries(s.approved)
}

func (s *AccessStore) update(fn func()) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn()
	return s.save()
}

// save writes atomically (tmp file + rename). Caller holds s.mu.
func (s *AccessStore) save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(accessData{
		Approved: sortedEntries(s.approved),
		Denied:   sortedEntries(s.denied),
	}, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func sortedEntries(m map[int64]AccessEntry) []AccessEntry {
	out := make([]AccessEntry, 0, len(m))
	for _, e := range m {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].DecidedAt.Equal(out[j].DecidedAt) {
			return out[i].DecidedAt.Before(out[j].DecidedAt)
		}
		return out[i].ChatID < out[j].ChatID
	})
	return out
}

// EnableAccessRequests turns on approval by the admin: messages from unknown
// chats produce a request with Approve/Deny buttons in adminChatID.
func (b *Bot) EnableAccessRequests(adminChatID int64, store *AccessStore) {
	b.adminChatID = adminChatID
	b.access = store
	b.log.Info("access requests enabled", "approved_chats", len(store.Approved()))
}

// chatTitle describes a chat for the admin: a person or a group.
func chatTitle(c *tgbotapi.Chat, from *tgbotapi.User) string {
	if c.IsPrivate() {
		name := strings.TrimSpace(c.FirstName + " " + c.LastName)
		if name == "" && from != nil {
			name = strings.TrimSpace(from.FirstName + " " + from.LastName)
		}
		if c.UserName != "" {
			name += " (@" + c.UserName + ")"
		}
		if name == "" {
			name = "без имени"
		}
		return name
	}
	return "группа «" + c.Title + "»"
}

// handleUnknownChat reacts to a message from a chat that is not allowed.
func (b *Bot) handleUnknownChat(m *tgbotapi.Message) {
	chatID := m.Chat.ID
	if b.access == nil {
		if m.IsCommand() {
			b.reply(chatID, "⛔ Этот чат не авторизован. Добавьте его ID в TELEGRAM_ALLOWED_CHAT_IDS.")
		}
		return
	}
	if b.access.IsDenied(chatID) {
		if m.IsCommand() {
			b.reply(chatID, "⛔ Администратор отклонил доступ к боту.")
		}
		return
	}
	if b.pending[chatID] {
		b.reply(chatID, "⏳ Запрос уже у администратора, ожидайте ответа.")
		return
	}

	title := chatTitle(m.Chat, m.From)
	text := fmt.Sprintf("🔔 Запрос доступа к боту\n\nОт: %s\nID чата: %d", title, chatID)
	if !m.Chat.IsPrivate() && m.From != nil {
		text += "\nНаписал(а): " + displayName(m.From)
	}
	req := tgbotapi.NewMessage(b.adminChatID, text)
	id := strconv.FormatInt(chatID, 10)
	req.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("✅ Разрешить", callbackAccessPrefix+accessApprove+":"+id),
		tgbotapi.NewInlineKeyboardButtonData("❌ Отклонить", callbackAccessPrefix+accessDeny+":"+id),
	))
	if _, err := b.api.Send(req); err != nil {
		b.log.Error("send access request", "err", b.errText(err))
		b.reply(chatID, "⚠️ Не удалось отправить запрос администратору, попробуйте позже.")
		return
	}
	b.pending[chatID] = true
	b.pendingTitle[chatID] = title
	b.log.Info("access requested", "chat", chatID)
	b.reply(chatID, "👋 Это бот умного дома. Запрос на доступ отправлен администратору — я напишу, когда он ответит.")
}

// parseAccessData splits "acc:<action>:<chatID>".
func parseAccessData(data string) (action string, chatID int64, ok bool) {
	rest, ok := strings.CutPrefix(data, callbackAccessPrefix)
	if !ok {
		return "", 0, false
	}
	action, idStr, ok := strings.Cut(rest, ":")
	if !ok {
		return "", 0, false
	}
	switch action {
	case accessApprove, accessDeny, accessRevoke:
	default:
		return "", 0, false
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return "", 0, false
	}
	return action, id, true
}

// handleAccessCallback processes the admin's button presses.
func (b *Bot) handleAccessCallback(q *tgbotapi.CallbackQuery) {
	if b.access == nil || q.Message.Chat.ID != b.adminChatID {
		b.answerCallback(q.ID, "⛔ Только для администратора")
		return
	}
	action, chatID, ok := parseAccessData(q.Data)
	if !ok {
		b.answerCallback(q.ID, "")
		return
	}

	if action == accessRevoke {
		e, found, err := b.access.Revoke(chatID)
		if err != nil {
			b.log.Error("save access", "err", err)
			b.answerCallback(q.ID, "⚠️ Не удалось сохранить")
			return
		}
		if !found {
			b.answerCallback(q.ID, "Уже удалён")
		} else {
			b.log.Info("access revoked", "chat", chatID)
			b.answerCallback(q.ID, "Доступ отозван: "+e.Title)
		}
		b.editAccessList(q.Message)
		return
	}

	title := b.pendingTitle[chatID]
	if title == "" {
		title = strconv.FormatInt(chatID, 10)
	}
	var err error
	var result, toUser string
	if action == accessApprove {
		err = b.access.Approve(chatID, title)
		result = "✅ Доступ разрешён"
		toUser = "✅ Администратор открыл доступ.\n\n" + helpText
	} else {
		err = b.access.Deny(chatID, title)
		result = "❌ Доступ отклонён"
		toUser = "⛔ Администратор отклонил доступ к боту."
	}
	if err != nil {
		b.log.Error("save access", "err", err)
		b.answerCallback(q.ID, "⚠️ Не удалось сохранить")
		return
	}
	delete(b.pending, chatID)
	delete(b.pendingTitle, chatID)
	b.log.Info("access decided", "chat", chatID, "action", action)

	b.answerCallback(q.ID, result)
	edit := tgbotapi.NewEditMessageText(q.Message.Chat.ID, q.Message.MessageID,
		fmt.Sprintf("%s\n\n%s · %s", q.Message.Text, result, time.Now().Format("15:04")))
	if _, err := b.api.Send(edit); err != nil {
		b.log.Error("edit access request", "err", b.errText(err))
	}
	b.reply(chatID, toUser)
}

// cmdAccess shows the admin who has access, with revoke buttons.
func (b *Bot) cmdAccess(chatID int64) {
	if b.access == nil || chatID != b.adminChatID {
		b.reply(chatID, "⛔ Команда только для администратора.")
		return
	}
	text, markup := b.accessList()
	msg := tgbotapi.NewMessage(chatID, text)
	if markup != nil {
		msg.ReplyMarkup = *markup
	}
	if _, err := b.api.Send(msg); err != nil {
		b.log.Error("send access list", "err", b.errText(err))
	}
}

func (b *Bot) editAccessList(m *tgbotapi.Message) {
	text, markup := b.accessList()
	edit := tgbotapi.NewEditMessageText(m.Chat.ID, m.MessageID, text)
	edit.ReplyMarkup = markup
	if markup == nil {
		edit.ReplyMarkup = &tgbotapi.InlineKeyboardMarkup{InlineKeyboard: [][]tgbotapi.InlineKeyboardButton{}}
	}
	if _, err := b.api.Send(edit); err != nil {
		b.log.Error("edit access list", "err", b.errText(err))
	}
}

func (b *Bot) accessList() (string, *tgbotapi.InlineKeyboardMarkup) {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Доступ из настроек (TELEGRAM_ALLOWED_CHAT_IDS): %d чат(ов)\n", len(b.allowed))
	entries := b.access.Approved()
	if len(entries) == 0 {
		sb.WriteString("\nОдобренных по запросу пока нет.")
		return sb.String(), nil
	}
	sb.WriteString("\nОдобрены по запросу (нажмите, чтобы отозвать):")
	var rows [][]tgbotapi.InlineKeyboardButton
	for _, e := range entries {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData(
			"🗑 "+e.Title, callbackAccessPrefix+accessRevoke+":"+strconv.FormatInt(e.ChatID, 10))))
	}
	markup := tgbotapi.NewInlineKeyboardMarkup(rows...)
	return sb.String(), &markup
}
