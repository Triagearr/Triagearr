package pollers_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Triagearr/Triagearr/internal/pollers"
	"github.com/Triagearr/Triagearr/internal/store"
	"github.com/Triagearr/Triagearr/internal/triagearr"
)

type fakeQbit struct {
	torrents []triagearr.Torrent
}

func (f *fakeQbit) ListTorrents(_ context.Context) ([]triagearr.Torrent, error) {
	return f.torrents, nil
}
func (f *fakeQbit) TorrentFiles(_ context.Context, _ triagearr.Hash) ([]triagearr.TorrentFile, error) {
	return nil, nil
}
func (f *fakeQbit) ListTrackers(_ context.Context, _ triagearr.Hash) ([]triagearr.TrackerInfo, error) {
	return nil, nil
}
func (f *fakeQbit) Delete(_ context.Context, _ triagearr.Hash, _ triagearr.DeleteOpts) error {
	return nil
}

func TestQbitPoller_PersistsTickThenExits(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	require.NoError(t, s.Migrate())

	now := time.Now().UTC()
	fake := &fakeQbit{
		torrents: []triagearr.Torrent{{
			Hash: "h1", Name: "Foo", Size: 100, AddedOn: now,
			Ratio: 1.5, Seeders: 4, Leechers: 1, State: "uploading",
		}},
	}

	p := &pollers.QbitPoller{Client: fake, Store: s, Interval: time.Hour}
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
