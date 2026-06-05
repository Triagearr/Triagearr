package triggers

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/Triagearr/Triagearr/internal/notify"
	"github.com/Triagearr/Triagearr/internal/pollers"
	"github.com/Triagearr/Triagearr/internal/store"
)

// HealthStore is the subset of store ops the health watcher reads. The instance
// rows carry the last-known health recorded by the arr/torrent pollers; the
// notification-state trio detects degraded→recovered transitions and de-dups
// reminders across restarts (ADR-0033).
type HealthStore interface {
	ListArrInstances(ctx context.Context) ([]store.ArrInstanceRow, error)
	ListTorrentClientInstances(ctx context.Context) ([]store.TorrentClientInstanceRow, error)
	GetNotificationState(ctx context.Context, eventKey string) (time.Time, bool, error)
	MarkNotificationSent(ctx context.Context, eventKey string, at time.Time) error
	ClearNotificationState(ctx context.Context, eventKey string) error
}

// HealthWatcher emits connection-health notifications on transitions (ADR-0033).
// It reads the health the arr/torrent pollers already persist — a single,
// decoupled emission point with one throttle key per component, rather than
// notify logic threaded through every poller. Implements pollers.Poller.
//
// Transition semantics, keyed by "health:<kind>:<component>":
//   - unhealthy and no state row  → emit health.degraded, mark the row
//   - healthy   and a state row   → emit health.recovered, clear the row
//
// Because instance rows only exist after the first poll, a never-configured
// component never fires.
type HealthWatcher struct {
	Store    HealthStore
	Notifier *notify.Dispatcher
	Interval time.Duration

	now func() time.Time
}

// Name implements pollers.Poller.
func (w *HealthWatcher) Name() string { return "health_watcher" }

// Run blocks until ctx is cancelled, checking health every Interval.
func (w *HealthWatcher) Run(ctx context.Context) error {
	if w.now == nil {
		w.now = func() time.Time { return time.Now().UTC() }
	}
	return pollers.TickLoop(ctx, w.Name(), w.Interval, w.tick, nil)
}

func (w *HealthWatcher) tick(ctx context.Context) error {
	if w.Notifier == nil || w.Notifier.Empty() {
		return nil
	}
	arrs, err := w.Store.ListArrInstances(ctx)
	if err != nil {
		return fmt.Errorf("listing arr_instances: %w", err)
	}
	for _, a := range arrs {
		w.reconcile(ctx, "arr", a.Kind, a.Healthy, derefErr(a.LastError))
	}
	tcs, err := w.Store.ListTorrentClientInstances(ctx)
	if err != nil {
		return fmt.Errorf("listing torrent_client_instances: %w", err)
	}
	for _, t := range tcs {
		w.reconcile(ctx, "torrent_client", t.Kind, t.Healthy, derefErr(t.LastError))
	}
	return nil
}

// reconcile compares the current health against the persisted alert state and
// emits the transition. Best-effort: every store/notify failure is logged and
// swallowed so it can never taint a run.
func (w *HealthWatcher) reconcile(ctx context.Context, kind, component string, healthy bool, lastErr string) {
	key := "health:" + kind + ":" + component
	_, alerted, err := w.Store.GetNotificationState(ctx, key)
	if err != nil {
		slog.Warn("notify: reading health state failed", "component", component, "err", err)
		return
	}
	switch {
	case !healthy && !alerted:
		w.Notifier.DispatchHealth(ctx, notify.HealthEvent{
			Component: component, Kind: kind, Healthy: false, LastError: lastErr,
		})
		if err := w.Store.MarkNotificationSent(ctx, key, w.now()); err != nil {
			slog.Warn("notify: recording health-degraded state failed", "component", component, "err", err)
		}
	case healthy && alerted:
		w.Notifier.DispatchHealth(ctx, notify.HealthEvent{
			Component: component, Kind: kind, Healthy: true,
		})
		if err := w.Store.ClearNotificationState(ctx, key); err != nil {
			slog.Warn("notify: clearing health state failed", "component", component, "err", err)
		}
	}
}

func derefErr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
