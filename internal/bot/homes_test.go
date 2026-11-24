package bot

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/srgkharkov/domovoy_alert_bot/internal/home"
)

type fakeHome struct{ id, name string }

func (f fakeHome) ID() string                                       { return f.id }
func (f fakeHome) Name() string                                     { return f.name }
func (f fakeHome) Devices(context.Context) ([]home.Device, error)   { return nil, nil }
func (f fakeHome) Readings(context.Context) ([]home.Reading, error) { return nil, nil }

func homeIDs(hs []home.Home) string {
	var ids []string
	for _, h := range hs {
		ids = append(ids, h.ID())
	}
	return strings.Join(ids, ",")
}

func TestHomesFor(t *testing.T) {
	homes := []home.Home{fakeHome{"aqara", "Старгород"}, fakeHome{"tuya-1", "Викендица"}}
	b := newBot(nil, homes, []int64{-100}, slog.Default())

	if got := homeIDs(b.homesFor(-100)); got != "aqara" {
		t.Errorf("config chat without access store: %q", got)
	}
	if b.isAllowed(5) {
		t.Error("unknown chat must not be allowed")
	}

	s, err := OpenAccessStore(filepath.Join(t.TempDir(), "access.json"))
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Approve(5, "Викендица чат", []string{"tuya-1"})
	_ = s.Approve(6, "старый", nil) // approved before multi-home support
	_ = s.Approve(-100, "семья", []string{"tuya-1"})
	b.adminChatID, b.access = 1, s

	for chat, want := range map[int64]string{
		1:    "aqara,tuya-1", // admin
		5:    "tuya-1",
		6:    "aqara",
		-100: "aqara,tuya-1", // config + approval
		9:    "",
	} {
		if got := homeIDs(b.homesFor(chat)); got != want {
			t.Errorf("homesFor(%d) = %q, want %q", chat, got, want)
		}
	}
	if b.hasHome(5, "aqara") || !b.hasHome(5, "tuya-1") {
		t.Error("hasHome mismatch for chat 5")
	}
}

func TestFormatReadings(t *testing.T) {
	got := formatReadings("Викендица", []home.Reading{
		{DeviceID: "m", Device: "Счётчик", Online: true, Kind: home.Power, Value: 1250},
		{DeviceID: "m", Device: "Счётчик", Online: true, Kind: home.Energy, Value: 3174.28},
		{DeviceID: "k", Device: "Кухня", Online: false, Kind: home.Temperature, Value: 12.14},
		{DeviceID: "k", Device: "Кухня", Online: false, Kind: home.Humidity, Value: 55.6},
	})
	for _, want := range []string{
		"🏠 Викендица",
		"⚡ Счётчик: 1250 Вт · счётчик 3174,28 кВт·ч",
		"🌡 Кухня: 12,1 °C · 💧 56 % (офлайн)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}
