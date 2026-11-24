package bot

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

const testToken = "123456:SECRET-token"

func TestErrTextRedactsToken(t *testing.T) {
	b := &Bot{redact: strings.NewReplacer(testToken, "<TOKEN>")}
	err := errors.New(`Post "https://api.telegram.org/bot` + testToken + `/getMe": connection reset by peer`)

	got := b.errText(err)
	if strings.Contains(got, "SECRET") {
		t.Fatalf("token leaked: %s", got)
	}
	if !strings.Contains(got, "bot<TOKEN>/getMe") {
		t.Fatalf("unexpected text: %s", got)
	}
}

func TestLibLoggerRedactsToken(t *testing.T) {
	var buf bytes.Buffer
	l := libLogger{
		log:    slog.New(slog.NewTextHandler(&buf, nil)),
		redact: strings.NewReplacer(testToken, "<TOKEN>"),
	}
	l.Println("Failed to get updates:", "https://api.telegram.org/bot"+testToken+"/getUpdates")
	l.Printf("retry %s", testToken)

	if strings.Contains(buf.String(), "SECRET") {
		t.Fatalf("token leaked: %s", buf.String())
	}
}
