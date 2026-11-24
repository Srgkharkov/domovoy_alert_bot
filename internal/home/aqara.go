package home

import (
	"context"

	"github.com/srgkharkov/domovoy_alert_bot/internal/aqara"
)

// AqaraID is the home ID of the (single) Aqara home.
const AqaraID = "aqara"

// AqaraAPI is the subset of the Aqara client used here.
type AqaraAPI interface {
	ListDevices(ctx context.Context) ([]aqara.Device, error)
}

// Aqara adapts the Aqara account to the Home interface.
type Aqara struct {
	name string
	api  AqaraAPI
}

func NewAqara(name string, api AqaraAPI) *Aqara { return &Aqara{name: name, api: api} }

func (a *Aqara) ID() string   { return AqaraID }
func (a *Aqara) Name() string { return a.name }

func (a *Aqara) Devices(ctx context.Context) ([]Device, error) {
	list, err := a.api.ListDevices(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Device, 0, len(list))
	for _, d := range list {
		out = append(out, Device{Name: d.DeviceName, Model: d.Model, Online: d.Online()})
	}
	return out, nil
}

// Readings: Aqara sensors are covered by alerts; /status is not wired yet.
func (a *Aqara) Readings(context.Context) ([]Reading, error) { return nil, ErrNotSupported }
