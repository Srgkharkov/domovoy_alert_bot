// Command domovoy runs the Telegram bot that controls an Aqara smart home.
//
// Usage:
//
//	domovoy                         run the bot
//	domovoy auth request            send an Aqara authorization code to AQARA_ACCOUNT
//	domovoy auth exchange <code>    exchange the code for tokens (saved to DATA_DIR)
//	domovoy check                   list homes, scenes and devices visible to the bot
//	domovoy read <did> <res>...     read device attribute values, e.g. 3.1.85
//	domovoy version                 print version
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/srgkharkov/domovoy_alert_bot/internal/alert"
	"github.com/srgkharkov/domovoy_alert_bot/internal/aqara"
	"github.com/srgkharkov/domovoy_alert_bot/internal/bot"
	"github.com/srgkharkov/domovoy_alert_bot/internal/config"
)

// version is set at build time: -ldflags "-X main.version=..."
var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := newLogger(cfg.LogLevel)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	store := aqara.NewFileTokenStore(filepath.Join(cfg.DataDir, "aqara_token.json"))
	home := aqara.NewClient(aqara.Options{
		APIURL: cfg.Aqara.APIURL,
		AppID:  cfg.Aqara.AppID,
		AppKey: cfg.Aqara.AppKey,
		KeyID:  cfg.Aqara.KeyID,
	}, store)

	if len(args) > 0 {
		switch args[0] {
		case "version":
			fmt.Println(version)
			return nil
		case "auth":
			if err := cfg.ValidateAqara(); err != nil {
				return err
			}
			return runAuth(ctx, home, cfg.Aqara.Account, args[1:])
		case "read":
			if len(args) < 3 {
				return fmt.Errorf("usage: domovoy read <did> <resourceId>...")
			}
			return runRead(ctx, home, args[1], args[2:])
		case "check":
			if err := cfg.ValidateAqara(); err != nil {
				return err
			}
			return runCheck(ctx, home)
		default:
			return fmt.Errorf("unknown command %q", args[0])
		}
	}

	if err := cfg.ValidateBot(); err != nil {
		return err
	}
	b, err := bot.New(ctx, cfg.Telegram.Token, home, cfg.Telegram.AllowedChatIDs, log)
	if err != nil {
		return fmt.Errorf("telegram: %w", err)
	}

	watcher := alert.NewWatcher(home, b.Notifier(cfg.Alerts.ChatIDs), alert.DefaultRules, cfg.Alerts.PollInterval, log)
	go watcher.Run(ctx)

	if cfg.Alerts.PushSecret != "" {
		go runPushServer(ctx, cfg.Alerts.PushListenAddr, cfg.Alerts.PushSecret, home, watcher, log)
	} else {
		log.Warn("PUSH_SECRET not set: Aqara push disabled, alerts rely on polling", "interval", cfg.Alerts.PollInterval)
	}

	log.Info("domovoy started", "version", version, "alert_chats", len(cfg.Alerts.ChatIDs))
	return b.Run(ctx)
}

// runPushServer serves Aqara Message Push callbacks on /aqara/push/<secret>.
// TLS is terminated by the reverse proxy (Caddy) in front of the container.
func runPushServer(ctx context.Context, addr, secret string, home *aqara.Client, w *alert.Watcher, log *slog.Logger) {
	mux := http.NewServeMux()
	mux.Handle("/aqara/push/"+secret, aqara.PushHandler(home, w.HandleReports, log))
	mux.HandleFunc("/healthz", func(rw http.ResponseWriter, _ *http.Request) {
		_, _ = rw.Write([]byte("ok"))
	})

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	log.Info("aqara push receiver listening", "addr", addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("push server stopped", "err", err)
	}
}

func runAuth(ctx context.Context, c *aqara.Client, account string, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: domovoy auth request | domovoy auth exchange <code>")
	}
	switch args[0] {
	case "request":
		if err := c.RequestAuthCode(ctx, account, "1y"); err != nil {
			return err
		}
		fmt.Printf("Код авторизации отправлен на %s.\nДальше: domovoy auth exchange <code>\n", account)
		return nil
	case "exchange":
		if len(args) < 2 {
			return fmt.Errorf("usage: domovoy auth exchange <code>")
		}
		tok, err := c.ExchangeAuthCode(ctx, account, args[1])
		if err != nil {
			return err
		}
		fmt.Printf("Готово. Токен сохранён, действует до %s.\n", tok.ExpiresAt.Format("2006-01-02"))
		return nil
	default:
		return fmt.Errorf("unknown auth subcommand %q", args[0])
	}
}

// runRead prints current values of device attributes with their timestamps.
func runRead(ctx context.Context, c *aqara.Client, did string, resourceIDs []string) error {
	vals, err := c.ReadResources(ctx, []aqara.ResourceQuery{{SubjectID: did, ResourceIDs: resourceIDs}})
	if err != nil {
		return err
	}
	for _, v := range vals {
		fmt.Printf("%-10s = %-8s (%s)\n", v.ResourceID, v.Value, time.UnixMilli(v.TimeStamp).Format("2006-01-02 15:04:05"))
	}
	return nil
}

// runCheck prints what the authorized Aqara account can see. Useful after setup.
func runCheck(ctx context.Context, c *aqara.Client) error {
	positions, err := c.ListPositions(ctx, "")
	if err != nil {
		return err
	}
	fmt.Printf("Дома (%d):\n", len(positions))
	for _, p := range positions {
		fmt.Printf("  🏠 %s  [%s]\n", p.PositionName, p.PositionID)
		scenes, err := c.ListScenes(ctx, p.PositionID)
		if err != nil {
			fmt.Printf("     сценарии: ошибка: %v\n", err)
			continue
		}
		for _, s := range scenes {
			fmt.Printf("     ▶️  %s  [%s]\n", s.Name, s.SceneID)
		}
	}

	devices, err := c.ListDevices(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("\nУстройства (%d):\n", len(devices))
	for _, d := range devices {
		status := "🟢"
		if !d.Online() {
			status = "🔴"
		}
		fmt.Printf("  %s %-30s %-28s %s\n", status, d.DeviceName, d.Model, d.DID)
	}
	return nil
}

func newLogger(level string) *slog.Logger {
	var l slog.Level
	switch strings.ToLower(level) {
	case "debug":
		l = slog.LevelDebug
	case "warn":
		l = slog.LevelWarn
	case "error":
		l = slog.LevelError
	default:
		l = slog.LevelInfo
	}
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: l}))
}
