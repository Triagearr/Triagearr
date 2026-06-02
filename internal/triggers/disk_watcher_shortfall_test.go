package triggers

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Triagearr/Triagearr/internal/notify"
	"github.com/Triagearr/Triagearr/internal/store"
)

// A dry-run pressure fire that can't reach target (no_more_candidates) emits the
// target-unreachable alert, then suppresses reminders until the configured
// interval elapses — independent of the run re-fire grace.
func TestDiskWatcher_ShortfallAlert_ThrottledReminder(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	clock := time.Now().UTC()
	w := newWatcher(s, &clock) // dry-run: DaemonLive false, no Actor
	fn := &fakeNotifier{}
	w.Notifier = notify.NewDispatcher(fn)
	w.TargetUnreachableReminder = 24 * time.Hour // re-fire grace is 1h

	// 5 GiB reclaimable, but reaching 20% of a 100 GiB disk from 5% needs 15 GiB.
	seedScoredTorrent(t, s, "a", "/data/dl", 5, 100)
	seedDisk(t, s, 5)

	require.NoError(t, w.tick(ctx, time.Hour))
	require.Len(t, fn.got, 1, "first shortfall fire alerts")
	require.Equal(t, notify.EventTargetUnreachable, fn.got[0].Kind)
	require.Contains(t, fn.got[0].Text, `target unreachable on "data"`)

	// Past the re-fire grace but inside the reminder window: a second run fires,
	// but the reminder is suppressed.
	clock = clock.Add(2 * time.Hour)
	require.NoError(t, w.tick(ctx, time.Hour))
	require.Len(t, fn.got, 1, "reminder suppressed within the interval")

	runs, err := s.ListRuns(ctx, store.ListRunsOpts{})
	require.NoError(t, err)
	require.Len(t, runs, 2, "the run itself still fires each grace period")

	// Past the reminder interval: the alert re-emits.
	clock = clock.Add(23 * time.Hour)
	require.NoError(t, w.tick(ctx, time.Hour))
	require.Len(t, fn.got, 2, "reminder re-emitted after the interval elapsed")
}

// When a fire resolves to target_reached, the throttle row is cleared so a later
// episode alerts immediately rather than waiting out a stale reminder window.
func TestDiskWatcher_ShortfallResolved_ClearsThrottle(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	clock := time.Now().UTC()
	w := newWatcher(s, &clock)
	fn := &fakeNotifier{}
	w.Notifier = notify.NewDispatcher(fn)
	w.TargetUnreachableReminder = 24 * time.Hour

	// Simulate a prior shortfall episode that left a throttle row behind.
	const key = "target_unreachable:data"
	require.NoError(t, s.MarkNotificationSent(ctx, key, clock.Add(-time.Hour)))

	// A 30 GiB candidate easily covers the 15 GiB needed → target_reached.
	seedScoredTorrent(t, s, "big", "/data/dl", 30, 100)
	seedDisk(t, s, 5)

	require.NoError(t, w.tick(ctx, time.Hour))
	require.Empty(t, fn.got, "a fire that reaches target must not alert")

	_, ok, err := s.GetNotificationState(ctx, key)
	require.NoError(t, err)
	require.False(t, ok, "resolving the condition clears the throttle row")
}

// With no provider configured the alert path is inert: no dispatch, no throttle
// write — the alert rides entirely on configured notifications (ADR-0032).
func TestDiskWatcher_ShortfallAlert_NoProviderInert(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	clock := time.Now().UTC()
	w := newWatcher(s, &clock) // Notifier left nil
	w.TargetUnreachableReminder = 24 * time.Hour

	seedScoredTorrent(t, s, "a", "/data/dl", 5, 100)
	seedDisk(t, s, 5)

	require.NoError(t, w.tick(ctx, time.Hour))
	_, ok, err := s.GetNotificationState(ctx, "target_unreachable:data")
	require.NoError(t, err)
	require.False(t, ok, "no throttle state is written when there is no provider")
}
