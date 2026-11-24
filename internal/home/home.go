// Package home models a physical house controlled by the bot, independent of
// the vendor cloud behind it (Aqara, Tuya, ...).
//
// Each chat is bound to one or more homes; commands like /devices and
// /status work through the Home interface, while vendor-specific features
// (the Aqara light panel and scenes) stay in their own code paths.
package home

import (
	"context"
	"errors"
)

// ErrNotSupported is returned when a home cannot provide some data.
var ErrNotSupported = errors.New("not supported for this home")

// Kind is a type of sensor reading. Values are normalized to fixed units.
type Kind int

const (
	Temperature Kind = iota // °C
	Humidity                // %
	Battery                 // %
	Power                   // W
	Energy                  // kWh, cumulative meter total
)

// Device is a device as shown in /devices.
type Device struct {
	Name   string
	Model  string
	Online bool
}

// Reading is one current sensor value.
type Reading struct {
	DeviceID string
	Device   string // device name
	Online   bool
	Kind     Kind
	Value    float64
}

// Home is one house.
type Home interface {
	ID() string   // stable identifier used in config and callbacks, e.g. "aqara", "tuya-123"
	Name() string // display name, e.g. "Викендица"
	Devices(ctx context.Context) ([]Device, error)
	Readings(ctx context.Context) ([]Reading, error)
}
