// Package notify delivers operator-facing notifications for Triagearr's
// advisory events: a disk-pressure run that reached the Actor (ADR-0021), a
// "target unreachable" disk shortfall (ADR-0032), and connection-health
// transitions. Manual HTTP/CLI runs stay silent — the operator triggered them
// knowingly.
//
// Events flow through one typed seam (ADR-0033). An Event carries a fixed
// Severity, a universal plain-text fallback (Text) and, for providers that can
// render structure, the typed payload (Report/Alert/HealthEvent). Text sinks
// (every shoutrrr-backed channel) read Text; the native webhook serialises the
// typed payload to JSON. The Dispatcher fans an Event out to every provider
// whose Routing admits it, best-effort: a provider failure is logged, never
// propagated, so a broken bot token cannot abort or taint a run.
package notify

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// Severity ranks an event so a provider can subscribe to a floor instead of
// enumerating every kind (ADR-0033). Ordered: info < warning < error.
type Severity int

const (
	SeverityInfo Severity = iota
	SeverityWarning
	SeverityError
)

// String renders the severity as the stable lowercase token used in config,
// the API and logs.
func (s Severity) String() string {
	switch s {
	case SeverityInfo:
		return "info"
	case SeverityWarning:
		return "warning"
	case SeverityError:
		return "error"
	default:
		return fmt.Sprintf("severity(%d)", int(s))
	}
}

// ParseSeverity maps a token back to a Severity. An empty string defaults to
// info (the most permissive floor — a provider with no configured floor sees
// everything).
func ParseSeverity(s string) (Severity, error) {
	switch s {
	case "", "info":
		return SeverityInfo, nil
	case "warning":
		return SeverityWarning, nil
	case "error":
		return SeverityError, nil
	default:
		return SeverityInfo, fmt.Errorf("unknown severity %q (want info|warning|error)", s)
	}
}

// EventKind is the stable, dotted taxonomy of notification events. The kind is
// the routing identity (providers mute by kind) and the wire value the webhook
// emits, so these strings must not change casually.
type EventKind string

const (
	EventRunExecuted       EventKind = "run.executed"
	EventRunPartial        EventKind = "run.partial"
	EventRunFailed         EventKind = "run.failed"
	EventTargetUnreachable EventKind = "disk.target_unreachable"
	EventHealthDegraded    EventKind = "health.degraded"
	EventHealthRecovered   EventKind = "health.recovered"
	EventTest              EventKind = "test"
)

// kindSeverity is the single source of truth mapping each kind to its fixed
// severity. Severity is intrinsic to the kind, not operator-tunable: routing
// happens by floor (MinSeverity) + mute, never by reclassifying an event.
var kindSeverity = map[EventKind]Severity{
	EventRunExecuted:       SeverityInfo,
	EventRunPartial:        SeverityWarning,
	EventRunFailed:         SeverityError,
	EventTargetUnreachable: SeverityWarning,
	EventHealthDegraded:    SeverityError,
	EventHealthRecovered:   SeverityInfo,
	EventTest:              SeverityInfo,
}

// Severity returns the fixed severity for the kind (info for an unknown kind).
func (k EventKind) Severity() Severity { return kindSeverity[k] }

// Known reports whether the kind is part of the taxonomy — used to validate a
// provider's mute list at config time.
func (k EventKind) Known() bool {
	_, ok := kindSeverity[k]
	return ok
}

// Catalogue returns every event kind with its severity, in a stable order, for
// the settings UI (event catalogue + mute pickers).
func Catalogue() []CatalogueEntry {
	order := []EventKind{
		EventRunExecuted, EventRunPartial, EventRunFailed,
		EventTargetUnreachable, EventHealthDegraded, EventHealthRecovered,
		EventTest,
	}
	out := make([]CatalogueEntry, 0, len(order))
	for _, k := range order {
		out = append(out, CatalogueEntry{Kind: k, Severity: k.Severity()})
	}
	return out
}

// CatalogueEntry is one row of the event catalogue.
type CatalogueEntry struct {
	Kind     EventKind
	Severity Severity
}

// HealthEvent is the payload for a connection-health transition: a configured
// *arr or torrent-client instance became unreachable, or recovered.
type HealthEvent struct {
	Component string // instance kind label, e.g. "sonarr", "qbittorrent"
	Kind      string // "arr" | "torrent_client"
	Healthy   bool
	LastError string
}

// Event is the typed notification seam (ADR-0033). Exactly one payload pointer
// is set, matching Kind. Text is always populated as the plain-text fallback so
// a text-only provider never needs to read the payload.
type Event struct {
	Kind     EventKind
	Severity Severity
	Title    string // short one-liner for providers with a title slot
	Text     string // universal plain-text body

	Run    *Report      // run.executed / run.partial / run.failed
	Alert  *Alert       // disk.target_unreachable
	Health *HealthEvent // health.degraded / health.recovered
}

// Routing is a provider's subscription: every event at or above MinSeverity,
// minus the muted kinds (ADR-0033). The zero value (info floor, no mutes)
// admits everything.
type Routing struct {
	MinSeverity Severity
	Mute        map[EventKind]bool
}

// Allows reports whether the event should be delivered to a provider with this
// routing.
func (r Routing) Allows(ev Event) bool {
	if r.Mute[ev.Kind] {
		return false
	}
	return ev.Severity >= r.MinSeverity
}

// Notifier delivers one Event to a single provider. Text providers read
// ev.Text; the webhook reads the typed payload.
type Notifier interface {
	// Send delivers the event. A transient failure (5xx/timeout) may be wrapped
	// with triagearr.ErrTransient; the Dispatcher does not retry either way.
	Send(ctx context.Context, ev Event) error
	// Name identifies the provider in logs and the delivery log (e.g. "telegram").
	Name() string
	// Routing returns this provider's severity floor + mute set.
	Routing() Routing
}

// deliveryRingSize bounds the in-memory recent-deliveries log surfaced by the
// dashboard. Advisory data only — it does not survive a restart (ADR-0033).
const deliveryRingSize = 100

// Delivery is one recorded fan-out attempt for the recent-deliveries view.
type Delivery struct {
	Provider string
	Kind     EventKind
	Severity Severity
	OK       bool
	Err      string
	At       time.Time
}

// Dispatcher fans an Event out to every configured Notifier whose Routing
// admits it, and keeps a bounded ring of recent deliveries.
type Dispatcher struct {
	notifiers []Notifier

	mu         sync.Mutex
	deliveries []Delivery // ring buffer, oldest first
}

// NewDispatcher builds a Dispatcher over the given notifiers. A nil/empty slice
// yields a Dispatcher whose dispatch methods are no-ops.
func NewDispatcher(notifiers ...Notifier) *Dispatcher {
	return &Dispatcher{notifiers: notifiers}
}

// Empty reports whether no providers are configured.
func (d *Dispatcher) Empty() bool {
	return d == nil || len(d.notifiers) == 0
}

// Dispatch delivers an executed-run report. The kind (executed/partial/failed)
// is derived from the run outcome; an empty run is silently dropped.
func (d *Dispatcher) Dispatch(ctx context.Context, r Report) {
	ev := FormatRun(r)
	if ev.Kind == "" {
		return
	}
	d.dispatch(ctx, ev)
}

// DispatchAlert delivers a target-unreachable alert (ADR-0032), best-effort.
func (d *Dispatcher) DispatchAlert(ctx context.Context, a Alert) {
	d.dispatch(ctx, FormatAlert(a))
}

// DispatchHealth delivers a connection-health transition, best-effort.
func (d *Dispatcher) DispatchHealth(ctx context.Context, h HealthEvent) {
	d.dispatch(ctx, FormatHealth(h))
}

// dispatch fans an Event out to every admitting notifier best-effort. Each
// failure is logged, recorded and swallowed: notifications are advisory and
// must never affect run outcome.
func (d *Dispatcher) dispatch(ctx context.Context, ev Event) {
	if d == nil {
		return
	}
	for _, n := range d.notifiers {
		if !n.Routing().Allows(ev) {
			continue
		}
		err := n.Send(ctx, ev)
		d.record(n.Name(), ev, err)
		if err != nil {
			slog.Warn("notification delivery failed", "provider", n.Name(), "kind", string(ev.Kind), "err", err)
			continue
		}
		slog.Info("notification delivered", "provider", n.Name(), "kind", string(ev.Kind))
	}
}

// record appends a delivery to the bounded ring (oldest dropped first).
func (d *Dispatcher) record(provider string, ev Event, err error) {
	if d == nil {
		return
	}
	del := Delivery{
		Provider: provider,
		Kind:     ev.Kind,
		Severity: ev.Severity,
		OK:       err == nil,
		At:       time.Now().UTC(),
	}
	if err != nil {
		del.Err = err.Error()
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.deliveries = append(d.deliveries, del)
	if len(d.deliveries) > deliveryRingSize {
		d.deliveries = d.deliveries[len(d.deliveries)-deliveryRingSize:]
	}
}

// Deliveries returns a copy of the recent-deliveries ring, newest first.
func (d *Dispatcher) Deliveries() []Delivery {
	if d == nil {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]Delivery, len(d.deliveries))
	for i, del := range d.deliveries {
		out[len(d.deliveries)-1-i] = del
	}
	return out
}

// TestOptions narrows a test send. An empty Provider targets every enabled
// provider; an empty Kind sends the generic connectivity-check event.
type TestOptions struct {
	Provider string
	Kind     EventKind
}

// SendTest delivers a generic connectivity-check to every configured provider.
func (d *Dispatcher) SendTest(ctx context.Context) error {
	return d.SendTestEvent(ctx, TestOptions{})
}

// SendTestEvent delivers a representative event to the targeted provider(s).
// Unlike dispatch it bypasses routing (a test must reach the provider it
// targets regardless of floor/mute) and surfaces failures joined by provider
// name so the dashboard can show the operator a bad credential.
func (d *Dispatcher) SendTestEvent(ctx context.Context, opts TestOptions) error {
	if d.Empty() {
		return errors.New("no notification provider is enabled")
	}
	ev := sampleEvent(opts.Kind)
	var errs []error
	matched := false
	for _, n := range d.notifiers {
		if opts.Provider != "" && n.Name() != opts.Provider {
			continue
		}
		matched = true
		err := n.Send(ctx, ev)
		d.record(n.Name(), ev, err)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", n.Name(), err))
		}
	}
	if !matched {
		return fmt.Errorf("provider %q is not enabled", opts.Provider)
	}
	return errors.Join(errs...)
}
