package alert

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/srgkharkov/domovoy_alert_bot/internal/aqara"
)

type fakeHome struct {
	values []aqara.ResourceValue
}

func (f *fakeHome) ListDevices(context.Context) ([]aqara.Device, error) {
	return []aqara.Device{
		{DID: "leak1", DeviceName: "Ванная", Model: "lumi.flood.agl02", State: 1},
		{DID: "relay1", DeviceName: "Кухня", Model: "lumi.relay.c2acn01", State: 1},
	}, nil
}

func (f *fakeHome) Subscribe(context.Context, []aqara.ResourceQuery) error { return nil }

func (f *fakeHome) ReadResources(context.Context, []aqara.ResourceQuery) ([]aqara.ResourceValue, error) {
	return f.values, nil
}

type recorder struct {
	mu   sync.Mutex
	msgs []string
}

func (r *recorder) Notify(_ context.Context, text string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.msgs = append(r.msgs, text)
}

func newTestWatcher(home *fakeHome) (*Watcher, *recorder) {
	rec := &recorder{}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewWatcher(home, rec, DefaultRules, time.Hour, log), rec
}

func TestLeakAlertAndClear(t *testing.T) {
	home := &fakeHome{values: []aqara.ResourceValue{{SubjectID: "leak1", ResourceID: "3.1.85", Value: "0"}}}
	w, rec := newTestWatcher(home)
	ctx := context.Background()

	// Initial poll: dry — no message.
	if err := w.poll(ctx); err != nil {
		t.Fatal(err)
	}
	if len(rec.msgs) != 0 {
		t.Fatalf("unexpected messages on dry start: %v", rec.msgs)
	}

	// Push: leak.
	w.HandleReports([]aqara.ResourceReport{{SubjectID: "leak1", ResourceID: "3.1.85", Value: "1"}})
	// Same leak seen by the poll — must not duplicate.
	home.values[0].Value = "1"
	_ = w.poll(ctx)
	// Clear.
	w.HandleReports([]aqara.ResourceReport{{SubjectID: "leak1", ResourceID: "3.1.85", Value: "0"}})

	if len(rec.msgs) != 2 {
		t.Fatalf("want 2 messages (alarm, clear), got %d: %v", len(rec.msgs), rec.msgs)
	}
}

func TestLeakActiveAtStartup(t *testing.T) {
	home := &fakeHome{values: []aqara.ResourceValue{{SubjectID: "leak1", ResourceID: "3.1.85", Value: "1"}}}
	w, rec := newTestWatcher(home)
	_ = w.poll(context.Background())
	if len(rec.msgs) != 1 {
		t.Fatalf("leak active at startup must alert, got %v", rec.msgs)
	}
}

func TestIgnoresUnwatchedResources(t *testing.T) {
	w, rec := newTestWatcher(&fakeHome{})
	w.HandleReports([]aqara.ResourceReport{
		{SubjectID: "relay1", ResourceID: "4.1.85", Value: "1"},               // light switched on
		{SubjectID: "relay1", ResourceID: "3.1.85", Value: "1"},               // leak resource on a non-leak model
		{SubjectID: "leak1", ResourceID: "3.1.85", Value: "1", StatusCode: 1}, // failed status
	})
	if len(rec.msgs) != 0 {
		t.Fatalf("unexpected messages: %v", rec.msgs)
	}
}

func TestLowBatteryAnyModel(t *testing.T) {
	w, rec := newTestWatcher(&fakeHome{})
	w.HandleReports([]aqara.ResourceReport{{SubjectID: "relay1", ResourceID: "8.0.9001", Value: "1"}})
	if len(rec.msgs) != 1 {
		t.Fatalf("want low battery alert, got %v", rec.msgs)
	}
}
