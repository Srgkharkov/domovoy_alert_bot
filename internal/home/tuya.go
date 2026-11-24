package home

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/srgkharkov/domovoy_alert_bot/internal/tuya"
)

// TuyaAPI is the subset of the Tuya client used here.
type TuyaAPI interface {
	HomeDevices(ctx context.Context, homeID int64) ([]tuya.Device, error)
	Specification(ctx context.Context, deviceID string) ([]tuya.StatusSpec, error)
}

// tuyaReadingCodes maps Tuya data point codes to reading kinds.
// Different product lines use different codes for the same quantity.
var tuyaReadingCodes = map[string]Kind{
	"va_temperature":       Temperature,
	"temp_current":         Temperature,
	"va_humidity":          Humidity,
	"humidity_value":       Humidity,
	"battery_percentage":   Battery,
	"power_total":          Power, // energy meters, usually kW
	"cur_power":            Power, // smart plugs, usually W
	"forward_energy_total": Energy,
	"add_ele":              Energy,
}

// tuyaCategoryNames gives readable model names for common categories:
// product names are often Chinese ("温湿度").
var tuyaCategoryNames = map[string]string{
	"wsdcg": "датчик температуры и влажности",
	"zndb":  "счётчик электроэнергии",
	"wg2":   "шлюз",
	"cz":    "розетка",
	"pc":    "удлинитель",
	"kg":    "выключатель",
	"tdq":   "реле",
	"dj":    "лампа",
	"sj":    "датчик протечки",
	"wk":    "термостат",
}

// Tuya adapts one Tuya app home to the Home interface.
type Tuya struct {
	api    TuyaAPI
	homeID int64
	name   string

	mu    sync.Mutex
	specs map[string]map[string]tuya.StatusSpec // deviceID -> code -> spec
}

func NewTuya(api TuyaAPI, homeID int64, name string) *Tuya {
	return &Tuya{api: api, homeID: homeID, name: name, specs: map[string]map[string]tuya.StatusSpec{}}
}

// TuyaID builds the home ID for a Tuya home.
func TuyaID(homeID int64) string { return fmt.Sprintf("tuya-%d", homeID) }

func (t *Tuya) ID() string    { return TuyaID(t.homeID) }
func (t *Tuya) Name() string  { return t.name }
func (t *Tuya) HomeID() int64 { return t.homeID }

func (t *Tuya) Devices(ctx context.Context) ([]Device, error) {
	list, err := t.api.HomeDevices(ctx, t.homeID)
	if err != nil {
		return nil, err
	}
	out := make([]Device, 0, len(list))
	for _, d := range list {
		model := d.ProductName
		if name, ok := tuyaCategoryNames[d.Category]; ok {
			model = name
		} else if model == "" {
			model = d.Model
		}
		out = append(out, Device{Name: d.Name, Model: model, Online: d.Online})
	}
	return out, nil
}

// Readings returns sensor values of all devices, sorted by device name.
// Offline devices report their last known values with Online=false.
func (t *Tuya) Readings(ctx context.Context) ([]Reading, error) {
	list, err := t.api.HomeDevices(ctx, t.homeID)
	if err != nil {
		return nil, err
	}
	var out []Reading
	for _, d := range list {
		specs, err := t.spec(ctx, d)
		if err != nil {
			return nil, err
		}
		for _, s := range d.Status {
			kind, ok := tuyaReadingCodes[s.Code]
			if !ok {
				continue
			}
			spec := specs[s.Code]
			v, ok := tuya.Scaled(s.Value, spec.Scale)
			if !ok {
				continue
			}
			if kind == Power && strings.EqualFold(spec.Unit, "kW") {
				v *= 1000
			}
			out = append(out, Reading{DeviceID: d.ID, Device: d.Name, Online: d.Online, Kind: kind, Value: v})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Device != out[j].Device {
			return out[i].Device < out[j].Device
		}
		return out[i].Kind < out[j].Kind
	})
	return out, nil
}

// spec returns cached data point specs; they never change for a device.
func (t *Tuya) spec(ctx context.Context, d tuya.Device) (map[string]tuya.StatusSpec, error) {
	t.mu.Lock()
	cached, ok := t.specs[d.ID]
	t.mu.Unlock()
	if ok {
		return cached, nil
	}
	// Only devices with known reading codes need a spec request.
	need := false
	for _, s := range d.Status {
		if _, ok := tuyaReadingCodes[s.Code]; ok {
			need = true
			break
		}
	}
	m := map[string]tuya.StatusSpec{}
	if need {
		list, err := t.api.Specification(ctx, d.ID)
		if err != nil {
			return nil, err
		}
		for _, s := range list {
			m[s.Code] = s
		}
	}
	t.mu.Lock()
	t.specs[d.ID] = m
	t.mu.Unlock()
	return m, nil
}
