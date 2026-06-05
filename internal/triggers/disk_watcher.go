// Package triggers fires Decider runs in response to observable signals.
// The signal is disk pressure: when the watched volume (ADR-0024) drops under
// its threshold_free_percent, the watcher asks the Decider for a plan and
// persists the resulting Run + RunItems, handing live runs to the Actor.
package triggers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/Triagearr/Triagearr/internal/actor"
	"github.com/Triagearr/Triagearr/internal/decider"
	"github.com/Triagearr/Triagearr/internal/notify"
	"github.com/Triagearr/Triagearr/internal/pollers"
	"github.com/Triagearr/Triagearr/internal/runlock"
	"github.com/Triagearr/Triagearr/internal/triagearr"
)

// errRunInProgress reports that a live pressure fire was skipped because the
// shared run-lock was held by another trigger. tick() treats it as a benign
// skip — not a failure — and leaves lastFire untouched so the next tick retries.
var errRunInProgress = errors.New("live run already in progress")

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

// RunStore is the subset of store ops the watcher writes through. The action/
// name lookups serve the post-action notification (ADR-0021); the notification-
// state trio backs the throttled target-unreachable alert (ADR-0032).
type RunStore interface {
	InsertRun(ctx context.Context, r triagearr.Run) (int64, error)
	InsertRunItems(ctx context.Context, runID int64, items []triagearr.RunItem) error
	LatestDiskUsage(ctx context.Context) (*triagearr.DiskUsage, error)
	ListActionsByRun(ctx context.Context, runID int64) ([]triagearr.Action, error)
	TorrentNamesByHashes(ctx context.Context, hashes []triagearr.Hash) (map[triagearr.Hash]string, error)
	GetNotificationState(ctx context.Context, eventKey string) (time.Time, bool, error)
	MarkNotificationSent(ctx context.Context, eventKey string, at time.Time) error
	ClearNotificationState(ctx context.Context, eventKey string) error
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
	// RunLock is the single-run guard shared with the HTTP server (and, via its
	// file lock, the CLI). A pressure run that resolves to live must hold it
	// across Actor.Execute so it can't race a concurrent live run from another
	// trigger. Nil disables guarding (dry-run daemons / tests that never act).
	RunLock *runlock.Lock
	// TargetUnreachableReminder is the minimum delay between two
	// target-unreachable reminders while the shortfall persists (ADR-0032).
	// Zero falls back to the config default at wiring time.
	TargetUnreachableReminder time.Duration

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
		// A skip (another trigger holds the run-lock) is benign: leave lastFire
		// untouched so the next tick retries promptly once the slot frees, rather
		// than waiting out the re-fire grace for a run that never happened.
		if !errors.Is(err, errRunInProgress) {
			slog.Warn("disk_watcher fire failed", "err", err)
		}
		return nil
	}
	w.lastFire = now
	return nil
}

func (w *DiskWatcher) fire(ctx context.Context, snap triagearr.DiskUsage) error {
	r := w.Rule
	// Resolve the mode up front (it depends only on DaemonLive, not the plan) so
	// a live run can claim the shared single-run slot BEFORE planning/persisting.
	// That way we never write a live run record that won't execute, and a
	// concurrent HTTP/CLI run blocks this fire cleanly instead of racing the
	// destructive pipeline.
	mode := triagearr.ResolveRunMode(w.DaemonLive, triagearr.RunTriggerDiskPressure, false)
	live := mode == triagearr.RunModeLive && w.Actor != nil
	if live && w.RunLock != nil {
		if !w.RunLock.TryAcquire() {
			slog.Info("skipping pressure run, a live run is already in progress",
				"free_pct", snap.FreePercent, "target_pct", r.TargetFreePercent)
			return errRunInProgress
		}
		defer w.RunLock.Release()
	}

	v := decider.Volume{
		Name:              r.Name,
		Path:              r.Path,
		TargetFreePercent: r.TargetFreePercent,
	}
	// Cap planning so a stalled store/clients can't freeze the poll tick.
	// The Decider scans candidates from SQLite; 30s is generous for 5k+ rows.
	planCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	plan, err := w.Decider.Plan(planCtx, v)
	if err != nil {
		return fmt.Errorf("planning: %w", err)
	}
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
	if live {
		// Arm the stop registry so an operator can halt this autonomous run from
		// the UI; the Actor observes the cancellation between candidates.
		runCtx := ctx
		if w.RunLock != nil {
			var cancel context.CancelFunc
			runCtx, cancel = context.WithCancel(ctx)
			defer cancel()
			w.RunLock.Arm(id, cancel)
		}
		if err := w.Actor.Execute(runCtx, id); err != nil {
			return fmt.Errorf("actor execute: %w", err)
		}
		w.notifyRun(ctx, snap, id, mode, plan.Items)
	}
	// Advisory, mode-independent: warn the operator when even deleting every
	// eligible candidate can't reach target. Runs after a live execute so the
	// message reflects what was actually reclaimable.
	w.maybeAlertShortfall(ctx, snap, plan, mode)
	return nil
}

// maybeAlertShortfall emits the throttled "target unreachable" alert (ADR-0032)
// when the plan stopped on no_more_candidates — the volume can't reach target
// even after deleting everything eligible. Best-effort: every failure is logged
// and swallowed so it never taints the run. When the condition does not hold the
// throttle is cleared so a future episode alerts immediately.
func (w *DiskWatcher) maybeAlertShortfall(ctx context.Context, snap triagearr.DiskUsage, plan decider.RunPlan, mode triagearr.RunMode) {
	const eventPrefix = "target_unreachable:"
	key := eventPrefix + w.Rule.Name

	if plan.StopReason != triagearr.StopNoMoreCandidates {
		if err := w.Store.ClearNotificationState(ctx, key); err != nil {
			slog.Warn("notify: clearing target-unreachable state failed", "err", err)
		}
		return
	}
	if w.Notifier == nil || w.Notifier.Empty() {
		return
	}

	last, sent, err := w.Store.GetNotificationState(ctx, key)
	if err != nil {
		slog.Warn("notify: reading target-unreachable state failed", "err", err)
		return
	}
	now := w.now()
	if sent && now.Sub(last) < w.TargetUnreachableReminder {
		return // still inside the reminder window
	}

	w.Notifier.DispatchAlert(ctx, notify.Alert{
		VolumeName:       w.Rule.Name,
		Mode:             string(mode),
		FreePct:          snap.FreePercent,
		TargetFreePct:    w.Rule.TargetFreePercent,
		NeedBytes:        decider.NeededBytes(snap.TotalBytes, snap.FreePercent, w.Rule.TargetFreePercent),
		ReclaimableBytes: plan.EstimatedFreedBytes,
		CandidateCount:   len(plan.Items),
	})
	if err := w.Store.MarkNotificationSent(ctx, key, now); err != nil {
		slog.Warn("notify: recording target-unreachable state failed", "err", err)
	}
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
