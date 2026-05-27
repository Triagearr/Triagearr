// Package triggers fires Decider runs in response to observable signals.
// The signal is disk pressure: when the watched volume (ADR-0024) drops under
// its threshold_free_percent, the watcher asks the Decider for a plan and
// persists the resulting Run + RunItems, handing live runs to the Actor.
package triggers

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/Triagearr/Triagearr/internal/actor"
	"github.com/Triagearr/Triagearr/internal/decider"
	"github.com/Triagearr/Triagearr/internal/notify"
	"github.com/Triagearr/Triagearr/internal/pollers"
	"github.com/Triagearr/Triagearr/internal/triagearr"
)

// NewDiskWatcher constructs a DiskWatcher for the single watched volume.
func NewDiskWatcher(rule VolumeRule, d *decider.Decider, store RunStore, interval time.Duration) *DiskWatcher {
	return &DiskWatcher{
		Rule:     rule,
		Decider:  d,
		Store:    store,
		Interval: interval,
		now:      func() time.Time { return time.Now().UTC() },
	}
}

// DefaultReFireGrace is the minimum delay between two consecutive fires.
// Prevents spamming runs when free% oscillates around the threshold.
const DefaultReFireGrace = time.Hour

// VolumeRule pairs the watched volume with its disk-pressure thresholds.
type VolumeRule struct {
	Name                 string
	Path                 string
	ThresholdFreePercent float64
	TargetFreePercent    float64
}

// RunStore is the subset of store ops the watcher writes through. The last two
// methods are read-only and serve the post-action notification (ADR-0021).
type RunStore interface {
	InsertRun(ctx context.Context, r triagearr.Run) (int64, error)
	InsertRunItems(ctx context.Context, runID int64, items []triagearr.RunItem) error
	LatestDiskUsage(ctx context.Context) (*triagearr.DiskUsage, error)
	ListActionsByRun(ctx context.Context, runID int64) ([]triagearr.Action, error)
	TorrentNamesByHashes(ctx context.Context, hashes []triagearr.Hash) (map[triagearr.Hash]string, error)
}

// DiskWatcher fires Decider runs when the volume drops below its threshold.
// Implements pollers.Poller.
type DiskWatcher struct {
	Rule     VolumeRule
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
	// Notifier delivers a post-action report after a live pressure run that
	// reached the Actor. Nil (or empty) disables notifications (ADR-0021).
	Notifier *notify.Dispatcher
	// Sampler re-measures the volume's free space right after the Actor
	// finishes, so the notification reports a real before/after delta rather
	// than an inferred one. Nil skips the "after" figure. Wired to
	// pollers.Statfs.
	Sampler func(path string) (triagearr.DiskUsage, error)

	now func() time.Time
	// lastFire is the time of the most recent fire. firingNow tracks whether
	// the latest sample was under the threshold, so transitions (above→below)
	// are distinguished from sustained-below ticks.
	lastFire  time.Time
	firingNow bool
}

// Name implements pollers.Poller.
func (w *DiskWatcher) Name() string { return "disk_watcher" }

// Run blocks until ctx is cancelled, polling LatestDiskUsage every Interval.
func (w *DiskWatcher) Run(ctx context.Context) error {
	if w.now == nil {
		w.now = func() time.Time { return time.Now().UTC() }
	}
	grace := w.ReFireGrace
	if grace <= 0 {
		grace = DefaultReFireGrace
	}
	return pollers.TickLoop(ctx, w.Name(), w.Interval, func(ctx context.Context) error {
		return w.tick(ctx, grace)
	}, nil)
}

func (w *DiskWatcher) tick(ctx context.Context, grace time.Duration) error {
	snap, err := w.Store.LatestDiskUsage(ctx)
	if err != nil {
		return fmt.Errorf("reading disk_usage: %w", err)
	}
	if snap == nil {
		return nil // no sample recorded yet
	}
	under := snap.FreePercent < w.Rule.ThresholdFreePercent
	wasUnder := w.firingNow
	w.firingNow = under
	if !under {
		return nil
	}
	// Below threshold: fire only on transition OR after grace from the last fire.
	now := w.now()
	if wasUnder && now.Sub(w.lastFire) < grace {
		return nil
	}
	if err := w.fire(ctx, *snap); err != nil {
		slog.Warn("disk_watcher fire failed", "err", err)
		return nil
	}
	w.lastFire = now
	return nil
}

func (w *DiskWatcher) fire(ctx context.Context, snap triagearr.DiskUsage) error {
	r := w.Rule
	v := decider.Volume{
		Name:              r.Name,
		Path:              r.Path,
		TargetFreePercent: r.TargetFreePercent,
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
		w.notifyRun(ctx, snap, id, mode, plan.Items)
	}
	return nil
}

// notifyRun builds and dispatches the post-action report for a live
// disk-pressure run. Best-effort throughout: any failure here is logged and
// swallowed so it can never taint the run outcome. No notification is sent
// when the run executed nothing (empty plan).
func (w *DiskWatcher) notifyRun(ctx context.Context, snap triagearr.DiskUsage, runID int64, mode triagearr.RunMode, items []triagearr.RunItem) {
	if w.Notifier == nil || w.Notifier.Empty() {
		return
	}
	actions, err := w.Store.ListActionsByRun(ctx, runID)
	if err != nil {
		slog.Warn("notify: loading actions failed", "run_id", runID, "err", err)
		return
	}
	if len(actions) == 0 {
		return // nothing executed — nothing worth notifying about
	}

	// Real torrent sizes come from the run plan; actions.freed_bytes is 0 on
	// failure, so it cannot stand in for the size of a failed item.
	sizeByHash := make(map[triagearr.Hash]int64, len(items))
	for _, it := range items {
		sizeByHash[it.TorrentHash] = it.SizeBytes
	}

	hashes := make([]triagearr.Hash, len(actions))
	for i, a := range actions {
		hashes[i] = a.TorrentHash
	}
	names, err := w.Store.TorrentNamesByHashes(ctx, hashes)
	if err != nil {
		slog.Warn("notify: resolving torrent names failed", "run_id", runID, "err", err)
		names = map[triagearr.Hash]string{}
	}

	report := notify.Report{
		VolumeName:      w.Rule.Name,
		Mode:            string(mode),
		RunID:           runID,
		FreePctBefore:   snap.FreePercent,
		FreeBytesBefore: snap.FreeBytes,
		TargetFreePct:   w.Rule.TargetFreePercent,
	}
	var haveAfter bool
	if w.Sampler != nil {
		after, err := w.Sampler(w.Rule.Path)
		if err != nil {
			slog.Warn("notify: post-action disk re-sample failed", "err", err)
		} else {
			report.FreePctAfter = after.FreePercent
			report.FreeBytesAfter = after.FreeBytes
			haveAfter = true
		}
	}
	for _, a := range actions {
		report.Items = append(report.Items, notify.ReportItem{
			Name:      names[a.TorrentHash],
			Hash:      a.TorrentHash,
			SizeBytes: sizeByHash[a.TorrentHash],
			Status:    a.Status,
		})
		if a.Status == triagearr.ActionSucceeded {
			report.TotalFreedBytes += a.FreedBytes
		}
	}
	if haveAfter {
		// Signed disk delta. Concurrent writes can render this negative — that
		// is itself useful signal (the operator's stack consumed more than we
		// freed during the action window).
		//nolint:gosec // disk free bytes are bounded well below int64 max
		report.RealFreedBytes = int64(report.FreeBytesAfter) - int64(report.FreeBytesBefore)
		const tolerancePct = 80 // see ADR-0024 / SCORING cross-seed pre-filter rationale
		if report.TotalFreedBytes > 0 && report.RealFreedBytes*100 < report.TotalFreedBytes*tolerancePct {
			slog.Warn("freed-space mismatch — actual disk delta below claimed",
				"run_id", runID,
				"claimed_bytes", report.TotalFreedBytes,
				"real_bytes", report.RealFreedBytes,
				"tolerance_pct", tolerancePct,
			)
		}
	}
	w.Notifier.Dispatch(ctx, report)
}
