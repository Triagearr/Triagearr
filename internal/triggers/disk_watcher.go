// Package triggers fires Decider runs in response to observable signals.
// In M4 the only signal is disk pressure: when a volume drops under its
// threshold_free_percent, the watcher asks the Decider for a plan and
// persists the resulting Run + RunItems (dry-run; M5 will hand it to the
// Actor).
package triggers

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/Triagearr/Triagearr/internal/actor"
	"github.com/Triagearr/Triagearr/internal/decider"
	"github.com/Triagearr/Triagearr/internal/pollers"
	"github.com/Triagearr/Triagearr/internal/triagearr"
)

// NewDiskWatcher constructs a DiskWatcher with its internal maps initialised.
// Prefer this over a struct literal so a future caller that invokes tick()
// directly (e.g. in tests) doesn't NPE on the lazy maps.
func NewDiskWatcher(rules []VolumeRule, d *decider.Decider, store RunStore, interval time.Duration) *DiskWatcher {
	return &DiskWatcher{
		Rules:     rules,
		Decider:   d,
		Store:     store,
		Interval:  interval,
		now:       func() time.Time { return time.Now().UTC() },
		lastFire:  map[string]time.Time{},
		firingNow: map[string]bool{},
	}
}

// DefaultReFireGrace is the minimum delay between two consecutive fires on
// the same volume. Prevents spamming runs when free% oscillates around the
// threshold.
const DefaultReFireGrace = time.Hour

// VolumeRule pairs a watched volume with its M4 pressure thresholds.
type VolumeRule struct {
	Name                 string
	Path                 string
	ThresholdFreePercent float64
	TargetFreePercent    float64
	MaxRunSizeGB         int
}

// RunStore is the subset of store ops the watcher writes through.
type RunStore interface {
	InsertRun(ctx context.Context, r triagearr.Run) (int64, error)
	InsertRunItems(ctx context.Context, runID int64, items []triagearr.RunItem) error
	LatestDiskUsage(ctx context.Context) ([]triagearr.DiskUsage, error)
}

// DiskWatcher fires Decider runs when a volume drops below its threshold.
// Implements pollers.Poller.
type DiskWatcher struct {
	Rules    []VolumeRule
	Decider  *decider.Decider
	Store    RunStore
	Interval time.Duration
	// ReFireGrace overrides the constant for tests.
	ReFireGrace time.Duration
	// DaemonLive mirrors config.Mode == "live". Pressure-driven runs go live
	// automatically when set; otherwise they stay dry-run regardless of
	// trigger (ADR-0015).
	DaemonLive bool
	// Actor executes runs resolved to "live". When nil the watcher behaves
	// as in M4 (plan only, no destructive call).
	Actor *actor.Actor

	now      func() time.Time
	lastFire map[string]time.Time
	// firingNow tracks volumes whose latest sample is under the threshold,
	// so transitions (above→below) are distinguished from sustained-below ticks.
	firingNow map[string]bool
}

// Name implements pollers.Poller.
func (w *DiskWatcher) Name() string { return "disk_watcher" }

// Run blocks until ctx is cancelled, polling LatestDiskUsage every Interval.
func (w *DiskWatcher) Run(ctx context.Context) error {
	if w.now == nil {
		w.now = func() time.Time { return time.Now().UTC() }
	}
	if w.lastFire == nil {
		w.lastFire = map[string]time.Time{}
	}
	if w.firingNow == nil {
		w.firingNow = map[string]bool{}
	}
	grace := w.ReFireGrace
	if grace <= 0 {
		grace = DefaultReFireGrace
	}
	return pollers.TickLoop(ctx, w.Name(), w.Interval, func(ctx context.Context) error {
		return w.tick(ctx, grace)
	})
}

func (w *DiskWatcher) tick(ctx context.Context, grace time.Duration) error {
	disks, err := w.Store.LatestDiskUsage(ctx)
	if err != nil {
		return fmt.Errorf("reading disk_usage: %w", err)
	}
	byName := make(map[string]triagearr.DiskUsage, len(disks))
	for _, d := range disks {
		byName[d.VolumeName] = d
	}
	for _, r := range w.Rules {
		snap, ok := byName[r.Name]
		if !ok {
			continue
		}
		under := snap.FreePercent < r.ThresholdFreePercent
		wasUnder := w.firingNow[r.Name]
		w.firingNow[r.Name] = under
		if !under {
			continue
		}
		// Below threshold: fire only on transition OR after grace from the last fire.
		now := w.now()
		if wasUnder && now.Sub(w.lastFire[r.Name]) < grace {
			continue
		}
		if err := w.fire(ctx, r, snap); err != nil {
			slog.Warn("disk_watcher fire failed", "volume", r.Name, "err", err)
			continue
		}
		w.lastFire[r.Name] = now
	}
	return nil
}

func (w *DiskWatcher) fire(ctx context.Context, r VolumeRule, snap triagearr.DiskUsage) error {
	v := decider.Volume{
		Name:              r.Name,
		Path:              r.Path,
		TargetFreePercent: r.TargetFreePercent,
		MaxRunSizeGB:      r.MaxRunSizeGB,
	}
	plan, err := w.Decider.Plan(ctx, v)
	if err != nil {
		return fmt.Errorf("planning: %w", err)
	}
	mode := triagearr.ResolveRunMode(w.DaemonLive, triagearr.RunTriggerDiskPressure, false)
	run := triagearr.Run{
		TriggeredBy:         triagearr.RunTriggerDiskPressure,
		TriggeredAt:         w.now(),
		Mode:                string(mode),
		VolumeName:          r.Name,
		FreePctAtFire:       snap.FreePercent,
		TargetFreePct:       r.TargetFreePercent,
		EstimatedFreedBytes: plan.EstimatedFreedBytes,
		StopReason:          plan.StopReason,
		Status:              "completed",
	}
	id, err := w.Store.InsertRun(ctx, run)
	if err != nil {
		return fmt.Errorf("persisting run: %w", err)
	}
	if err := w.Store.InsertRunItems(ctx, id, plan.Items); err != nil {
		return fmt.Errorf("persisting run items: %w", err)
	}
	slog.Warn("disk pressure run planned",
		"run_id", id,
		"volume", r.Name,
		"free_pct", snap.FreePercent,
		"target_pct", r.TargetFreePercent,
		"candidates", len(plan.Items),
		"estimated_freed_gb", float64(plan.EstimatedFreedBytes)/(1024*1024*1024),
		"stop_reason", string(plan.StopReason),
		"mode", string(mode),
	)
	if mode == triagearr.RunModeLive && w.Actor != nil {
		if err := w.Actor.Execute(ctx, id); err != nil {
			return fmt.Errorf("actor execute: %w", err)
		}
	}
	return nil
}
