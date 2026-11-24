package tuya

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Home is a home (family) in the Tuya Smart / Smart Life app.
type Home struct {
	HomeID int64  `json:"home_id"`
	Name   string `json:"name"`
}

// Device is a device with its current data points.
type Device struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Category    string   `json:"category"` // e.g. wsdcg (T&H sensor), zndb (energy meter)
	ProductName string   `json:"product_name"`
	Model       string   `json:"model"`
	Online      bool     `json:"online"`
	Status      []Status `json:"status"`
}

// Status is one data point (DP) value. Value is a JSON number, bool or string.
type Status struct {
	Code  string `json:"code"`
	Value any    `json:"value"`
}

// StatusSpec describes a data point: for integer DPs, the real value is
// raw / 10^Scale in Unit.
type StatusSpec struct {
	Code   string
	Type   string // Integer, Boolean, Enum, ...
	Unit   string
	Scale  int
	Values string // raw JSON from the API
}

// Home returns home info (name).
func (c *Client) Home(ctx context.Context, homeID int64) (*Home, error) {
	var h Home
	if err := c.get(ctx, fmt.Sprintf("/v1.0/homes/%d", homeID), nil, &h); err != nil {
		return nil, err
	}
	return &h, nil
}

// HomeDevices lists devices of a home together with their current status.
func (c *Client) HomeDevices(ctx context.Context, homeID int64) ([]Device, error) {
	var ds []Device
	if err := c.get(ctx, fmt.Sprintf("/v1.0/homes/%d/devices", homeID), nil, &ds); err != nil {
		return nil, err
	}
	return ds, nil
}

// Specification returns status data point descriptions of a device.
func (c *Client) Specification(ctx context.Context, deviceID string) ([]StatusSpec, error) {
	var res struct {
		Status []struct {
			Code   string `json:"code"`
			Type   string `json:"type"`
			Values string `json:"values"`
		} `json:"status"`
	}
	if err := c.get(ctx, "/v1.0/iot-03/devices/"+deviceID+"/specification", nil, &res); err != nil {
		return nil, err
	}
	out := make([]StatusSpec, 0, len(res.Status))
	for _, s := range res.Status {
		spec := StatusSpec{Code: s.Code, Type: s.Type, Values: s.Values}
		var v struct {
			Unit  string `json:"unit"`
			Scale int    `json:"scale"`
		}
		if json.Unmarshal([]byte(s.Values), &v) == nil {
			spec.Unit, spec.Scale = v.Unit, v.Scale
		}
		out = append(out, spec)
	}
	return out, nil
}

// Scaled converts a raw integer DP value using the spec scale.
// ok is false for non-numeric values.
func Scaled(raw any, scale int) (float64, bool) {
	var f float64
	switch v := raw.(type) {
	case float64:
		f = v
	case json.Number:
		var err error
		if f, err = v.Float64(); err != nil {
			return 0, false
		}
	case string:
		var err error
		if f, err = strconv.ParseFloat(strings.TrimSpace(v), 64); err != nil {
			return 0, false
		}
	default:
		return 0, false
	}
	return f / math.Pow10(scale), true
}
