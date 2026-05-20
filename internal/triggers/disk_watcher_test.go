package triggers

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Triagearr/Triagearr/internal/decider"
	"github.com/Triagearr/Triagearr/internal/store"
	"github.com/Triagearr/Triagearr/internal/triagearr"
)

func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "trig.db")
	s, err := store.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	require.NoError(t, s.Migrate())
	return s
}

func seedDisk(t *testing.T, s *store.Store, vol string, freePct float64) {
	t.Helper()
	err := s.InsertDiskUsage(context.Background(), triagearr.DiskUsage{
		VolumeName:  vol,
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
	w := &DiskWatcher{
		Rules: []VolumeRule{{
			Name: "data", Path: "/data",
			ThresholdFreePercent: 10, TargetFreePercent: 20, MaxRunSizeGB: 100,
		}},
		Decider:     decider.New(s),
		Store:       s,
		Interval:    time.Hour, // unused; we call tick() directly
		ReFireGrace: time.Hour,
		now:         func() time.Time { return *freshClock },
		lastFire:    map[string]time.Time{},
		firingNow:   map[string]bool{},
	}
	return w
}

func TestDiskWatcher_FireOnTransition(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	clock := time.Now().UTC()
	w := newWatcher(s, &clock)

	seedScoredTorrent(t, s, "a", "/data/dl", 5, 100)
	seedDisk(t, s, "data", 5) // below threshold (10)

	require.NoError(t, w.tick(ctx, time.Hour))
	runs, err := s.ListRuns(ctx, store.ListRunsOpts{})
	require.NoError(t, err)
	require.Len(t, runs, 1)
	require.Equal(t, "data", runs[0].VolumeName)
}

func TestDiskWatcher_NoReFireWithinGrace(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	clock := time.Now().UTC()
	w := newWatcher(s, &clock)

	seedScoredTorrent(t, s, "a", "/data/dl", 5, 100)
	seedDisk(t, s, "data", 5)

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
	seedDisk(t, s, "data", 5)

	require.NoError(t, w.tick(ctx, time.Hour))
	clock = clock.Add(2 * time.Hour)
	require.NoError(t, w.tick(ctx, time.Hour))

	runs, err := s.ListRuns(ctx, store.ListRunsOpts{})
	require.NoError(t, err)
	require.Len(t, runs, 2)
}

func TestDiskWatcher_NoFireWhenAboveThreshold(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	clock := time.Now().UTC()
	w := newWatcher(s, &clock)

	seedScoredTorrent(t, s, "a", "/data/dl", 5, 100)
	seedDisk(t, s, "data", 50) // well above

	require.NoError(t, w.tick(ctx, time.Hour))
	runs, err := s.ListRuns(ctx, store.ListRunsOpts{})
	require.NoError(t, err)
	require.Empty(t, runs)
}
