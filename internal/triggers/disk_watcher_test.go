package triggers

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Triagearr/Triagearr/internal/decider"
	"github.com/Triagearr/Triagearr/internal/notify"
	"github.com/Triagearr/Triagearr/internal/store"
	"github.com/Triagearr/Triagearr/internal/triagearr"
)

// fakeNotifier records every message it is handed.
type fakeNotifier struct {
	got []notify.Message
}

func (f *fakeNotifier) Name() string { return "fake" }
func (f *fakeNotifier) Send(_ context.Context, m notify.Message) error {
	f.got = append(f.got, m)
	return nil
}

func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "trig.db")
	s, err := store.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	require.NoError(t, s.Migrate(context.Background()))
	return s
}

func seedDisk(t *testing.T, s *store.Store, freePct float64) {
	t.Helper()
	err := s.InsertDiskUsage(context.Background(), triagearr.DiskUsage{
		Path:        "/data",
		Timestamp:   time.Now().UTC(),
		TotalBytes:  100 * 1024 * 1024 * 1024,
		UsedBytes:   uint64(float64(100*1024*1024*1024) * (1 - freePct/100.0)),
		FreeBytes:   uint64(float64(100*1024*1024*1024) * (freePct / 100.0)),
		FreePercent: freePct,
	})
	require.NoError(t, err)
}

func seedScoredTorrent(t *testing.T, s *store.Store, hash string, savePath string, sizeGB int64, score float64) {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, s.UpsertTorrent(ctx, triagearr.Torrent{
		Hash: triagearr.Hash(hash), Name: hash, Category: "tv",
		SavePath: savePath, Size: sizeGB * 1024 * 1024 * 1024,
		AddedOn: time.Now().UTC().Add(-30 * 24 * time.Hour),
	}))
	require.NoError(t, s.UpsertScore(ctx, store.ScoreRow{
		Hash: hash, Score: score, Private: false, AnyTrackerAlive: true,
		Excluded: false, ExclusionReasons: "", FactorsJSON: "[]",
		ComputedAt: time.Now().UTC(),
	}))
}

func newWatcher(s *store.Store, freshClock *time.Time) *DiskWatcher {
	return &DiskWatcher{
		Rule: VolumeRule{
			Name: "data", Path: "/data",
			ThresholdFreePercent: 10, TargetFreePercent: 20,
		},
		Decider:     decider.New(s),
		Store:       s,
		Interval:    time.Hour, // unused; we call tick() directly
		ReFireGrace: time.Hour,
		now:         func() time.Time { return *freshClock },
	}
}

func TestDiskWatcher_FireOnTransition(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	clock := time.Now().UTC()
	w := newWatcher(s, &clock)

	seedScoredTorrent(t, s, "a", "/data/dl", 5, 100)
	seedDisk(t, s, 5) // below threshold (10)

	require.NoError(t, w.tick(ctx, time.Hour))
	runs, err := s.ListRuns(ctx, store.ListRunsOpts{})
	require.NoError(t, err)
	require.Len(t, runs, 1)
	require.Equal(t, triagearr.RunTriggerDiskPressure, runs[0].TriggeredBy)
}

func TestDiskWatcher_NoReFireWithinGrace(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	clock := time.Now().UTC()
	w := newWatcher(s, &clock)

	seedScoredTorrent(t, s, "a", "/data/dl", 5, 100)
	seedDisk(t, s, 5)

	require.NoError(t, w.tick(ctx, time.Hour))
	require.NoError(t, w.tick(ctx, time.Hour))
	require.NoError(t, w.tick(ctx, time.Hour))

	runs, err := s.ListRuns(ctx, store.ListRunsOpts{})
	require.NoError(t, err)
	require.Len(t, runs, 1, "subsequent ticks while still under should not re-fire within grace")
}

func TestDiskWatcher_ReFireAfterGrace(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	clock := time.Now().UTC()
	w := newWatcher(s, &clock)

	seedScoredTorrent(t, s, "a", "/data/dl", 5, 100)
	seedDisk(t, s, 5)

	require.NoError(t, w.tick(ctx, time.Hour))
	clock = clock.Add(2 * time.Hour)
	require.NoError(t, w.tick(ctx, time.Hour))

	runs, err := s.ListRuns(ctx, store.ListRunsOpts{})
	require.NoError(t, err)
	require.Len(t, runs, 2)
}

func TestDiskWatcher_NotifyRun(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	clock := time.Now().UTC()
	w := newWatcher(s, &clock)

	fn := &fakeNotifier{}
	w.Notifier = notify.NewDispatcher(fn)
	w.Sampler = func(_ string) (triagearr.DiskUsage, error) {
		return triagearr.DiskUsage{FreePercent: 22, FreeBytes: 22 * 1024 * 1024 * 1024}, nil
	}

	seedScoredTorrent(t, s, "h1", "/data/dl", 5, 100)
	seedScoredTorrent(t, s, "h2", "/data/dl", 3, 90)

	runID, err := s.InsertRun(ctx, triagearr.Run{
		TriggeredBy: triagearr.RunTriggerDiskPressure, TriggeredAt: clock,
		Mode: "live", StopReason: triagearr.StopNoMoreCandidates,
		Status: "completed",
	})
	require.NoError(t, err)
	items := []triagearr.RunItem{
		{RunID: runID, Rank: 0, TorrentHash: "h1", SizeBytes: 5 * 1024 * 1024 * 1024},
		{RunID: runID, Rank: 1, TorrentHash: "h2", SizeBytes: 3 * 1024 * 1024 * 1024},
	}
	require.NoError(t, s.InsertRunItems(ctx, runID, items))

	a1, err := s.InsertAction(ctx, triagearr.Action{
		RunID: runID, Rank: 0, TorrentHash: "h1", StartedAt: clock, Status: triagearr.ActionRunning,
	})
	require.NoError(t, err)
	require.NoError(t, s.FinishAction(ctx, a1, triagearr.ActionSucceeded, clock, 5*1024*1024*1024))
	a2, err := s.InsertAction(ctx, triagearr.Action{
		RunID: runID, Rank: 1, TorrentHash: "h2", StartedAt: clock, Status: triagearr.ActionRunning,
	})
	require.NoError(t, err)
	require.NoError(t, s.FinishAction(ctx, a2, triagearr.ActionFailedQbit, clock, 0))

	snap := triagearr.DiskUsage{
		Path:        "/data",
		FreePercent: 5, FreeBytes: 5 * 1024 * 1024 * 1024,
	}
	w.notifyRun(ctx, snap, runID, triagearr.RunModeLive, items)

	require.Len(t, fn.got, 1)
	msg := fn.got[0]
	require.Equal(t, notify.EventRunReport, msg.Kind)
	// The provider sees formatted text (ADR-0032); assert the run details landed.
	require.Contains(t, msg.Text, `disk pressure on "data"`)
	require.Contains(t, msg.Text, "5.0% -> 22.0%")
	require.Contains(t, msg.Text, "Deleted 1/2 items")
	// Display name resolves from the torrents table (seeded name == hash).
	require.Contains(t, msg.Text, "h1 — 5.0 GiB [ok]")
	// The failed item must still carry its real size (not actions.freed_bytes).
	require.Contains(t, msg.Text, "h2 — 3.0 GiB [failed: qbit]")
}

func TestDiskWatcher_NotifyRun_NoActionsIsSilent(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	clock := time.Now().UTC()
	w := newWatcher(s, &clock)

	fn := &fakeNotifier{}
	w.Notifier = notify.NewDispatcher(fn)

	runID, err := s.InsertRun(ctx, triagearr.Run{
		TriggeredBy: triagearr.RunTriggerDiskPressure, TriggeredAt: clock,
		Mode: "live", StopReason: triagearr.StopNoMoreCandidates,
		Status: "completed",
	})
	require.NoError(t, err)

	w.notifyRun(ctx, triagearr.DiskUsage{}, runID, triagearr.RunModeLive, nil)
	require.Empty(t, fn.got, "a run that executed nothing must not notify")
}

func TestDiskWatcher_NoFireWhenAboveThreshold(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	clock := time.Now().UTC()
	w := newWatcher(s, &clock)

	seedScoredTorrent(t, s, "a", "/data/dl", 5, 100)
	seedDisk(t, s, 50) // well above

	require.NoError(t, w.tick(ctx, time.Hour))
	runs, err := s.ListRuns(ctx, store.ListRunsOpts{})
	require.NoError(t, err)
	require.Empty(t, runs)
}
