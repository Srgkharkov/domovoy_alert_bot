package home

import (
	"context"
	"testing"

	"github.com/srgkharkov/domovoy_alert_bot/internal/tuya"
)

type fakeTuyaAPI struct{ specCalls int }

func (f *fakeTuyaAPI) HomeDevices(context.Context, int64) ([]tuya.Device, error) {
	return []tuya.Device{
		{ID: "m", Name: "Счётчик", Online: true, Status: []tuya.Status{
			{Code: "forward_energy_total", Value: float64(317428)},
			{Code: "power_total", Value: float64(1250)}, // kW, scale 3
			{Code: "fault", Value: float64(0)},
		}},
		{ID: "g", Name: "Шлюз", Online: true}, // no readings, no spec request
		{ID: "k", Name: "Кухня", Online: false, Status: []tuya.Status{
			{Code: "va_temperature", Value: float64(121)},
			{Code: "temp_unit_convert", Value: "c"},
		}},
	}, nil
}

func (f *fakeTuyaAPI) Specification(_ context.Context, id string) ([]tuya.StatusSpec, error) {
	f.specCalls++
	switch id {
	case "m":
		return []tuya.StatusSpec{
			{Code: "forward_energy_total", Unit: "kW·h", Scale: 2},
			{Code: "power_total", Unit: "kW", Scale: 3},
		}, nil
	case "k":
		return []tuya.StatusSpec{{Code: "va_temperature", Unit: "℃", Scale: 1}}, nil
	}
	return nil, nil
}

func TestTuyaReadings(t *testing.T) {
	api := &fakeTuyaAPI{}
	h := NewTuya(api, 1, "Викендица")
	if h.ID() != "tuya-1" {
		t.Fatalf("ID = %q", h.ID())
	}

	for range 2 {
		rs, err := h.Readings(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		want := []Reading{
			{DeviceID: "k", Device: "Кухня", Online: false, Kind: Temperature, Value: 12.1},
			{DeviceID: "m", Device: "Счётчик", Online: true, Kind: Power, Value: 1250},
			{DeviceID: "m", Device: "Счётчик", Online: true, Kind: Energy, Value: 3174.28},
		}
		if len(rs) != len(want) {
			t.Fatalf("readings = %+v", rs)
		}
		for i := range want {
			d := rs[i].Value - want[i].Value
			if rs[i].DeviceID != want[i].DeviceID || rs[i].Kind != want[i].Kind || rs[i].Online != want[i].Online || d > 1e-9 || d < -1e-9 {
				t.Errorf("reading %d = %+v, want %+v", i, rs[i], want[i])
			}
		}
	}
	if api.specCalls != 2 {
		t.Errorf("spec calls = %d, want 2 (cached, gateway skipped)", api.specCalls)
	}
}
