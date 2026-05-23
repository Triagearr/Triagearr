package pollers_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Triagearr/Triagearr/internal/pollers"
	"github.com/Triagearr/Triagearr/internal/store"
	"github.com/Triagearr/Triagearr/internal/triagearr"
)

type fakeFilesQbit struct {
	files map[triagearr.Hash][]triagearr.TorrentFile
}

func (f *fakeFilesQbit) TorrentFiles(_ context.Context, h triagearr.Hash) ([]triagearr.TorrentFile, error) {
	return f.files[h], nil
}

// TestFilesPoller_PersistsRealNlink uses real os.Link calls in t.TempDir to
// validate that DefaultStatNlink reads st_nlink correctly and that the values
// flow through to MaxNlinkByHashes. This is the property the Decider's
// cross-seed pre-filter relies on.
func TestFilesPoller_PersistsRealNlink(t *testing.T) {
	dir := t.TempDir()
	// One file with one extra hardlink (nlink=2 — the standard arr-imported case),
	// one file with two extra hardlinks (nlink=3 — a cross-seed shape).
	saveDir := filepath.Join(dir, "torrents", "Show")
	require.NoError(t, os.MkdirAll(saveDir, 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "media"), 0o755))

	mkFile := func(rel string, payload []byte) string {
		full := filepath.Join(saveDir, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, payload, 0o644))
		return full
	}
	a := mkFile("ep01.mkv", []byte("aaaa"))
	b := mkFile("ep02.mkv", []byte("bb"))
	require.NoError(t, os.Link(a, filepath.Join(dir, "media", "ep01.mkv")))   // a: nlink=2
	require.NoError(t, os.Link(b, filepath.Join(dir, "media", "ep02.mkv")))   // b: nlink=2
	require.NoError(t, os.Link(b, filepath.Join(dir, "media", "ep02-x.mkv"))) // b: nlink=3 → cross-seed shape

	s, err := store.Open(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	require.NoError(t, s.Migrate())

	require.NoError(t, s.UpsertTorrent(context.Background(), triagearr.Torrent{
		Hash: "h1", Name: "Show", SavePath: filepath.Join(dir, "torrents"),
		Size: 6, AddedOn: time.Now().UTC(),
	}))

	fake := &fakeFilesQbit{files: map[triagearr.Hash][]triagearr.TorrentFile{
		"h1": {
			{Name: "Show/ep01.mkv", Size: 4},
			{Name: "Show/ep02.mkv", Size: 2},
			{Name: "Show/missing.mkv", Size: 1}, // never created on disk → ENOENT path
		},
	}}

	p := &pollers.FilesPoller{Store: s, Qbit: fake, Interval: time.Hour}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- p.Run(ctx) }()

	require.Eventually(t, func() bool {
		rows, err := s.TorrentFilesByHash(context.Background(), "h1")
		return err == nil && len(rows) == 3
	}, 2*time.Second, 10*time.Millisecond)

	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("files poller did not stop within 2s of cancellation")
	}

	rows, err := s.TorrentFilesByHash(context.Background(), "h1")
	require.NoError(t, err)
	got := map[string]*int64{}
	for _, r := range rows {
		got[r.RelPath] = r.Nlink
	}
	require.NotNil(t, got["Show/ep01.mkv"])
	require.Equal(t, int64(2), *got["Show/ep01.mkv"])
	require.NotNil(t, got["Show/ep02.mkv"])
	require.Equal(t, int64(3), *got["Show/ep02.mkv"])
	require.Nil(t, got["Show/missing.mkv"], "ENOENT must persist as nlink=NULL")

	maxN, err := s.MaxNlinkByHashes(context.Background(), []triagearr.Hash{"h1"})
	require.NoError(t, err)
	require.Equal(t, int64(3), maxN["h1"], "MaxNlinkByHashes should reflect the cross-seed shape")
}
