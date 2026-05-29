package triggers

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Triagearr/Triagearr/internal/decider"
	"github.com/Triagearr/Triagearr/internal/store"
)

func TestNewDiskWatcher_Defaults(t *testing.T) {
	s := openTestStore(t)
	rule := VolumeRule{Name: "data", Path: "/data", ThresholdFreePercent: 10, TargetFreePercent: 20}

	w := NewDiskWatcher(rule, decider.New(s), s, 5*time.Minute)
	require.Equal(t, rule, w.Rule)
	require.Equal(t, 5*time.Minute, w.Interval)
	require.Equal(t, "disk_watcher", w.Name())
	require.NotNil(t, w.now, "constructor must wire a clock")
}

// TestDiskWatcher_RunStopsOnCancel drives the public Run loop (not tick
// directly): with the volume above threshold it ticks without firing, then
// returns promptly when the context is cancelled.
func TestDiskWatcher_RunStopsOnCancel(t *testing.T) {
	s := openTestStore(t)
	seedDisk(t, s, 50) // well above the 10% threshold — no run should fire

	w := NewDiskWatcher(
		VolumeRule{Name: "data", Path: "/data", ThresholdFreePercent: 10, TargetFreePercent: 20},
		decider.New(s), s, time.Millisecond,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after context cancellation")
	}

	// Nothing fired while above threshold.
	runs, err := s.ListRuns(context.Background(), store.ListRunsOpts{})
	require.NoError(t, err)
	require.Empty(t, runs)
}
