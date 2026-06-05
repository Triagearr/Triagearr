package triggers

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Triagearr/Triagearr/internal/actor"
	"github.com/Triagearr/Triagearr/internal/runlock"
	"github.com/Triagearr/Triagearr/internal/store"
	"github.com/Triagearr/Triagearr/internal/triagearr"
)

// stubTorrentClient satisfies actor.TorrentClient. It is never exercised in
// these tests — the run-lock skip returns before the Actor is reached — so the
// methods are inert.
type stubTorrentClient struct{}

func (stubTorrentClient) TorrentFiles(context.Context, triagearr.Hash) ([]triagearr.TorrentFile, error) {
	return nil, nil
}
func (stubTorrentClient) Delete(context.Context, triagearr.Hash, triagearr.DeleteOpts) error {
	return nil
}

// liveWatcher returns a watcher resolving pressure runs to "live": DaemonLive
// set and a (real, store-backed) Actor wired so fire() consults the run-lock.
func liveWatcher(s *store.Store, clk *time.Time, lock *runlock.Lock) *DiskWatcher {
	w := newWatcher(s, clk)
	w.DaemonLive = true
	w.Actor = actor.New(actor.Options{
		Source:  s,
		Client:  stubTorrentClient{},
		Deleter: func(string) (triagearr.FileDeleter, bool) { return nil, false },
	})
	w.RunLock = lock
	return w
}

func TestDiskWatcher_SkipsWhenRunLockHeld(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	clock := time.Now().UTC()

	lock := runlock.New()
	require.True(t, lock.TryAcquire(), "test pre-holds the slot, simulating an in-flight HTTP/CLI run")

	w := liveWatcher(s, &clock, lock)
	seedScoredTorrent(t, s, "a", "/data/dl", 5, 100)
	seedDisk(t, s, 5) // below threshold — would fire if the slot were free

	require.NoError(t, w.tick(ctx, time.Hour))

	runs, err := s.ListRuns(ctx, store.ListRunsOpts{})
	require.NoError(t, err)
	require.Empty(t, runs, "no live run may be persisted while another run holds the lock")
	require.True(t, w.lastFire.IsZero(), "a skipped fire must not advance lastFire — the next tick retries")

	// The watcher must not leak an acquire: once the test releases its hold the
	// slot is free again, proving the failed TryAcquire rolled back.
	lock.Release()
	require.True(t, lock.TryAcquire(), "watcher rolled back its failed acquire — slot not leaked")
}

func TestDiskWatcher_RetriesAfterLockReleased(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	clock := time.Now().UTC()

	lock := runlock.New()
	require.True(t, lock.TryAcquire())

	w := liveWatcher(s, &clock, lock)
	// No scored torrents: the plan is empty, so the post-release live run
	// executes over zero candidates — enough to prove the watcher resumes once
	// the slot frees, without exercising the destructive per-candidate pipeline.
	seedDisk(t, s, 5) // below threshold

	require.NoError(t, w.tick(ctx, time.Hour)) // skipped: lock held
	runs, err := s.ListRuns(ctx, store.ListRunsOpts{})
	require.NoError(t, err)
	require.Empty(t, runs)

	lock.Release()
	require.NoError(t, w.tick(ctx, time.Hour))
	runs, err = s.ListRuns(ctx, store.ListRunsOpts{})
	require.NoError(t, err)
	require.Len(t, runs, 1, "watcher fires once the slot frees")
	require.Equal(t, string(triagearr.RunModeLive), runs[0].Mode)
}

func TestDiskWatcher_DryRunIgnoresRunLock(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	clock := time.Now().UTC()

	lock := runlock.New()
	require.True(t, lock.TryAcquire(), "hold the slot to prove a dry-run never consults it")

	// DaemonLive stays false and no Actor is wired → dry-run, which performs no
	// destructive work and so needs no lock.
	w := newWatcher(s, &clock)
	w.RunLock = lock

	seedScoredTorrent(t, s, "a", "/data/dl", 5, 100)
	seedDisk(t, s, 5)

	require.NoError(t, w.tick(ctx, time.Hour))
	runs, err := s.ListRuns(ctx, store.ListRunsOpts{})
	require.NoError(t, err)
	require.Len(t, runs, 1, "dry-run pressure run is persisted regardless of the held lock")
	require.Equal(t, string(triagearr.RunModeDryRun), runs[0].Mode)
}
