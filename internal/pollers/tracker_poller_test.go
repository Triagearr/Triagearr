package pollers_test

import (
	"context"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Triagearr/Triagearr/internal/pollers"
	"github.com/Triagearr/Triagearr/internal/store"
	"github.com/Triagearr/Triagearr/internal/triagearr"
)

type fakeTrackerClient struct {
	mu        sync.Mutex
	calls     atomic.Int32
	byHash    map[triagearr.Hash][]triagearr.TrackerInfo
	listError error
}

func (f *fakeTrackerClient) ListTorrents(_ context.Context) ([]triagearr.Torrent, error) {
	return nil, nil
}
func (f *fakeTrackerClient) TorrentFiles(_ context.Context, _ triagearr.Hash) ([]triagearr.TorrentFile, error) {
	return nil, nil
}
func (f *fakeTrackerClient) ListTrackers(_ context.Context, h triagearr.Hash) ([]triagearr.TrackerInfo, error) {
	f.calls.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listError != nil {
		return nil, f.listError
	}
	return f.byHash[h], nil
}
func (f *fakeTrackerClient) Delete(_ context.Context, _ triagearr.Hash, _ triagearr.DeleteOpts) error {
	return nil
}

func TestTrackerPoller_CatchupOnSignal(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	require.NoError(t, s.Migrate())

	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, s.UpsertTorrent(ctx, triagearr.Torrent{
		Hash: "fresh", Name: "Fresh", AddedOn: now,
	}))

	fake := &fakeTrackerClient{byHash: map[triagearr.Hash][]triagearr.TrackerInfo{
		"fresh": {{URL: "https://t/announce", Host: "t", Status: triagearr.TrackerWorking}},
	}}

	signal := make(chan struct{}, 1)
	// Long Interval so the periodic sweep doesn't fire during the test —
	// only the Signal path can populate torrent_trackers.
	p := &pollers.TrackerPoller{
		Client:   fake,
		Store:    s,
		Interval: time.Hour,
		Signal:   signal,
	}

	runCtx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- p.Run(runCtx) }()

	// Wait for the immediate initial sweep to finish so the catchup pass
	// below is the one that fetches "fresh". The initial sweep also fetches
	// every known hash, so we expect at least one call after the initial.
	require.Eventually(t, func() bool {
		rows, err := s.ListTrackers(context.Background(), "fresh")
		return err == nil && len(rows) == 1
	}, 5*time.Second, 10*time.Millisecond)

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("poller did not stop within 2s of cancellation")
	}
}

func TestTrackerPoller_CatchupFetchesMissingOnly(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	require.NoError(t, s.Migrate())

	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	for _, h := range []triagearr.Hash{"with", "without"} {
		require.NoError(t, s.UpsertTorrent(ctx, triagearr.Torrent{
			Hash: h, Name: string(h), AddedOn: now,
		}))
	}
	// "with" already has trackers; only "without" should be fetched on catchup.
	require.NoError(t, s.ReplaceTrackers(ctx, "with", []triagearr.TrackerInfo{
		{URL: "https://old/announce", Host: "old", Status: triagearr.TrackerWorking},
	}))

	fake := &fakeTrackerClient{byHash: map[triagearr.Hash][]triagearr.TrackerInfo{
		"without": {{URL: "https://new/announce", Host: "new", Status: triagearr.TrackerWorking}},
	}}

	signal := make(chan struct{}, 1)
	// Skip the initial full sweep by counting calls before/after the signal.
	p := &pollers.TrackerPoller{
		Client:   fake,
		Store:    s,
		Interval: time.Hour,
		Signal:   signal,
	}

	runCtx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- p.Run(runCtx) }()

	// Wait for the initial sweep (touches both hashes).
	require.Eventually(t, func() bool {
		return fake.calls.Load() >= 2
	}, 5*time.Second, 10*time.Millisecond)

	before := fake.calls.Load()
	signal <- struct{}{}

	// After debounce, exactly one extra call (catchup only fetches "without").
	require.Eventually(t, func() bool {
		return fake.calls.Load() == before+1
	}, 5*time.Second, 10*time.Millisecond)

	cancel()
	<-done
}
