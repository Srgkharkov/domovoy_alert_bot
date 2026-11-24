// Package config loads application settings from environment variables.
//
// Secrets are never stored in code: locally they live in a .env file
// (git-ignored), in Docker they are passed via `env_file` in docker-compose.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config is the full application configuration.
type Config struct {
	Telegram Telegram
	Aqara    Aqara
	Alerts   Alerts
	DataDir  string // directory for persistent state (Aqara tokens)
	LogLevel string // debug | info | warn | error
}

// Alerts configures sensor notifications.
type Alerts struct {
	ChatIDs      []int64       // where to send alerts; default = Telegram.AllowedChatIDs
	PollInterval time.Duration // fallback polling of leak sensors
	// HTTP receiver for Aqara Message Push. Disabled if PushSecret is empty.
	PushListenAddr string // e.g. ":8080"
	PushSecret     string // random path segment: /aqara/push/<secret>
}

// Telegram holds Telegram Bot API settings.
type Telegram struct {
	Token          string
	AllowedChatIDs []int64 // only these chats may control the bot
}

// Aqara holds Aqara Open API (developer.aqara.com) settings.
type Aqara struct {
	APIURL  string // regional endpoint, e.g. https://open-ger.aqara.com
	AppID   string
	AppKey  string
	KeyID   string
	Account string // Aqara Home account (email / phone) used for authorization
}

// Load reads configuration from the environment.
func Load() (*Config, error) {
	chatIDs, err := parseInt64List(os.Getenv("TELEGRAM_ALLOWED_CHAT_IDS"))
	if err != nil {
		return nil, fmt.Errorf("TELEGRAM_ALLOWED_CHAT_IDS: %w", err)
	}

	alertChats, err := parseInt64List(os.Getenv("TELEGRAM_ALERT_CHAT_IDS"))
	if err != nil {
		return nil, fmt.Errorf("TELEGRAM_ALERT_CHAT_IDS: %w", err)
	}
	if len(alertChats) == 0 {
		alertChats = chatIDs
	}
	poll, err := time.ParseDuration(getenv("ALERT_POLL_INTERVAL", "5m"))
	if err != nil {
		return nil, fmt.Errorf("ALERT_POLL_INTERVAL: %w", err)
	}

	return &Config{
		Telegram: Telegram{
			Token:          os.Getenv("TELEGRAM_BOT_TOKEN"),
			AllowedChatIDs: chatIDs,
		},
		Aqara: Aqara{
			APIURL:  getenv("AQARA_API_URL", "https://open-ger.aqara.com"),
			AppID:   os.Getenv("AQARA_APP_ID"),
			AppKey:  os.Getenv("AQARA_APP_KEY"),
			KeyID:   os.Getenv("AQARA_KEY_ID"),
			Account: os.Getenv("AQARA_ACCOUNT"),
		},
		Alerts: Alerts{
			ChatIDs:        alertChats,
			PollInterval:   poll,
			PushListenAddr: getenv("PUSH_LISTEN_ADDR", ":8080"),
			PushSecret:     os.Getenv("PUSH_SECRET"),
		},
		DataDir:  getenv("DATA_DIR", "./data"),
		LogLevel: getenv("LOG_LEVEL", "info"),
	}, nil
}

// ValidateAqara checks that all Aqara credentials are present.
func (c *Config) ValidateAqara() error {
	var missing []string
	for name, v := range map[string]string{
		"AQARA_API_URL": c.Aqara.APIURL,
		"AQARA_APP_ID":  c.Aqara.AppID,
		"AQARA_APP_KEY": c.Aqara.AppKey,
		"AQARA_KEY_ID":  c.Aqara.KeyID,
		"AQARA_ACCOUNT": c.Aqara.Account,
	} {
		if v == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required env: %s", strings.Join(missing, ", "))
	}
	return nil
}

// ValidateBot checks everything needed to run the Telegram bot.
func (c *Config) ValidateBot() error {
	if c.Telegram.Token == "" {
		return errors.New("missing required env: TELEGRAM_BOT_TOKEN")
	}
	return c.ValidateAqara()
}

func getenv(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func parseInt64List(s string) ([]int64, error) {
	var out []int64
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		n, err := strconv.ParseInt(part, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid chat id %q: %w", part, err)
		}
		out = append(out, n)
	}
	return out, nil
}
