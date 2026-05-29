package pollers_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Triagearr/Triagearr/internal/pollers"
	"github.com/Triagearr/Triagearr/internal/store"
	"github.com/Triagearr/Triagearr/internal/triagearr"
)

type fakeTorrentClient struct {
	torrents  []triagearr.Torrent
	healthErr error
}

func (f *fakeTorrentClient) ListTorrents(_ context.Context) ([]triagearr.Torrent, error) {
	return f.torrents, nil
}
func (f *fakeTorrentClient) TorrentFiles(_ context.Context, _ triagearr.Hash) ([]triagearr.TorrentFile, error) {
	return nil, nil
}
func (f *fakeTorrentClient) ListTrackers(_ context.Context, _ triagearr.Hash) ([]triagearr.TrackerInfo, error) {
	return nil, nil
}
func (f *fakeTorrentClient) Delete(_ context.Context, _ triagearr.Hash, _ triagearr.DeleteOpts) error {
	return nil
}
func (f *fakeTorrentClient) HealthCheck(_ context.Context) error { return f.healthErr }

func TestTorrentClientPoller_PersistsTickThenExits(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	require.NoError(t, s.Migrate(context.Background()))

	now := time.Now().UTC()
	fake := &fakeTorrentClient{
		torrents: []triagearr.Torrent{{
			Hash: "h1", Name: "Foo", Size: 100, AddedOn: now,
			Ratio: 1.5, Seeders: 4, Leechers: 1, State: "uploading",
		}},
	}

	p := &pollers.TorrentClientPoller{Client: fake, Store: s, Interval: time.Hour}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- p.Run(ctx) }()

	// The first tick is immediate; give the goroutine a moment to run it.
	require.Eventually(t, func() bool {
		rows, err := s.ListTorrentsLatest(context.Background(), "name", 0)
		return err == nil && len(rows) == 1
	}, 2*time.Second, 10*time.Millisecond)

	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("poller did not stop within 2s of cancellation")
	}
}

func TestTorrentClientPoller_PersistsHealth(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	require.NoError(t, s.Migrate(context.Background()))

	fake := &fakeTorrentClient{healthErr: errors.New("connection refused")}
	p := &pollers.TorrentClientPoller{
		Client: fake, Kind: "qbittorrent", URL: "http://qbit:8080",
		Store: s, Interval: time.Hour,
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = p.Run(ctx) }()

	require.Eventually(t, func() bool {
		rows, err := s.ListTorrentClientInstances(context.Background())
		return err == nil && len(rows) == 1 && !rows[0].Healthy
	}, 2*time.Second, 10*time.Millisecond)

	rows, err := s.ListTorrentClientInstances(context.Background())
	require.NoError(t, err)
	require.Equal(t, "qbittorrent", rows[0].Kind)
	require.NotNil(t, rows[0].LastError)
	require.Contains(t, *rows[0].LastError, "connection refused")
}
