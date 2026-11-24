// Package alert turns sensor state changes into chat notifications.
//
// Changes arrive from two sources:
//   - Aqara HTTP push (instant), via Watcher.HandleReports;
//   - periodic polling (fallback in case a push is lost), via Watcher.Run.
//
// The Watcher remembers the last value of every watched attribute, so the
// same change reported by both sources produces a single notification.
package alert

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/srgkharkov/domovoy_alert_bot/internal/aqara"
)

// Notifier delivers a message to the family chat(s).
type Notifier interface {
	Notify(ctx context.Context, text string)
}

// Home is the subset of the Aqara client the watcher needs.
type Home interface {
	ListDevices(ctx context.Context) ([]aqara.Device, error)
	ReadResources(ctx context.Context, queries []aqara.ResourceQuery) ([]aqara.ResourceValue, error)
	Subscribe(ctx context.Context, queries []aqara.ResourceQuery) error
}

// Rule describes one watched attribute. Value "1" means alarm, "0" — clear.
type Rule struct {
	ResourceID string
	Models     map[string]bool // nil = any model
	Alarm      string          // message format, %s = device name
	Clear      string          // "" = do not notify when cleared
}

// DefaultRules: water leak and low battery.
var DefaultRules = []Rule{
	{
		ResourceID: "3.1.85", // Water leak status: 0 no leak, 1 leak
		Models:     map[string]bool{"lumi.flood.agl02": true, "lumi.sensor_wleak.aq1": true, "lumi.flood.acn001": true},
		Alarm:      "🚨💧 ПРОТЕЧКА! Сработал датчик «%s»",
		Clear:      "✅ Датчик «%s»: протечки больше нет",
	},
	{
		ResourceID: "8.0.9001", // Battery status: 1 low, 0 restored
		Alarm:      "🔋 Низкий заряд батареи: «%s»",
	},
}

type device struct {
	name, model string
}

// Watcher tracks sensor state and sends notifications on changes.
type Watcher struct {
	home     Home
	notify   Notifier
	rules    []Rule
	interval time.Duration
	log      *slog.Logger

	mu         sync.Mutex
	devices    map[string]device // did -> device
	state      map[string]string // did|resourceId -> last value
	subscribed map[string]bool   // did -> push subscription done
}

func NewWatcher(home Home, notify Notifier, rules []Rule, pollInterval time.Duration, log *slog.Logger) *Watcher {
	return &Watcher{
		home: home, notify: notify, rules: rules, interval: pollInterval, log: log,
		devices:    map[string]device{},
		state:      map[string]string{},
		subscribed: map[string]bool{},
	}
}

// Run polls watched sensors until ctx is cancelled. The first poll also
// alerts if an alarm is already active at startup.
func (w *Watcher) Run(ctx context.Context) {
	t := time.NewTicker(w.interval)
	defer t.Stop()
	for {
		if err := w.poll(ctx); err != nil {
			w.log.Error("alert poll failed", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// HandleReports processes attribute changes from an Aqara push.
func (w *Watcher) HandleReports(reports []aqara.ResourceReport) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	for _, r := range reports {
		if r.StatusCode != 0 {
			continue
		}
		w.apply(ctx, r.SubjectID, r.ResourceID, r.Value, "push")
	}
}

func (w *Watcher) poll(ctx context.Context) error {
	if err := w.refreshDevices(ctx); err != nil {
		return err
	}

	w.mu.Lock()
	var queries []aqara.ResourceQuery
	for did, d := range w.devices {
		var ids []string
		for _, r := range w.rules {
			if r.matches(d.model) {
				ids = append(ids, r.ResourceID)
			}
		}
		// Polling every device for battery would be wasteful: poll only
		// devices covered by a model-specific rule (leak sensors).
		if len(ids) > 0 && w.hasModelRule(d.model) {
			queries = append(queries, aqara.ResourceQuery{SubjectID: did, ResourceIDs: ids})
		}
	}
	w.mu.Unlock()

	if len(queries) == 0 {
		return nil
	}
	w.subscribe(ctx, queries)

	values, err := w.home.ReadResources(ctx, queries)
	if err != nil {
		return err
	}
	for _, v := range values {
		w.apply(ctx, v.SubjectID, v.ResourceID, v.Value, "poll")
	}
	return nil
}

// subscribe asks Aqara to push changes of watched sensors. Done once per
// device; failures are retried on the next poll. In "All receiving mode"
// Aqara rejects subscriptions — then push works without them.
func (w *Watcher) subscribe(ctx context.Context, queries []aqara.ResourceQuery) {
	var todo []aqara.ResourceQuery
	w.mu.Lock()
	for _, q := range queries {
		if !w.subscribed[q.SubjectID] {
			todo = append(todo, q)
		}
	}
	w.mu.Unlock()
	if len(todo) == 0 {
		return
	}

	if err := w.home.Subscribe(ctx, todo); err != nil {
		var apiErr *aqara.APIError
		if !errors.As(err, &apiErr) || apiErr.Code != 403 {
			w.log.Warn("push subscribe failed, will retry", "err", err)
			return
		}
		// 403: the console is in "All receiving mode" — everything is pushed
		// without subscriptions, so stop trying.
		w.log.Info("push subscription not needed (All receiving mode)")
	}
	w.mu.Lock()
	for _, q := range todo {
		w.subscribed[q.SubjectID] = true
	}
	w.mu.Unlock()
	w.log.Debug("push subscriptions ensured", "devices", len(todo))
}

func (w *Watcher) refreshDevices(ctx context.Context) error {
	list, err := w.home.ListDevices(ctx)
	if err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, d := range list {
		w.devices[d.DID] = device{name: d.DeviceName, model: d.Model}
	}
	return nil
}

// apply records a value and notifies on a meaningful change.
func (w *Watcher) apply(ctx context.Context, did, resourceID, value, source string) {
	w.mu.Lock()
	d, known := w.devices[did]
	w.mu.Unlock()
	if !known {
		// A device added after startup: refresh once to learn its model.
		if err := w.refreshDevices(ctx); err != nil {
			w.log.Warn("refresh devices", "err", err)
		}
		w.mu.Lock()
		d, known = w.devices[did]
		w.mu.Unlock()
		if !known {
			return
		}
	}

	rule, ok := w.ruleFor(d.model, resourceID)
	if !ok {
		return
	}

	key := did + "|" + resourceID
	w.mu.Lock()
	prev, seen := w.state[key]
	w.state[key] = value
	w.mu.Unlock()

	if seen && prev == value {
		return
	}

	var text string
	switch {
	case value == "1":
		text = fmt.Sprintf(rule.Alarm, d.name)
	case value == "0" && seen && rule.Clear != "":
		text = fmt.Sprintf(rule.Clear, d.name)
	default:
		return // first observation of a normal state
	}

	w.log.Info("alert", "device", d.name, "resource", resourceID, "value", value, "source", source)
	w.notify.Notify(ctx, text)
}

func (w *Watcher) ruleFor(model, resourceID string) (Rule, bool) {
	for _, r := range w.rules {
		if r.ResourceID == resourceID && r.matches(model) {
			return r, true
		}
	}
	return Rule{}, false
}

func (w *Watcher) hasModelRule(model string) bool {
	for _, r := range w.rules {
		if r.Models != nil && r.Models[model] {
			return true
		}
	}
	return false
}

func (r Rule) matches(model string) bool {
	return r.Models == nil || r.Models[model]
}
