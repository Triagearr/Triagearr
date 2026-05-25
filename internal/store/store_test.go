package store_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Triagearr/Triagearr/internal/store"
	"github.com/Triagearr/Triagearr/internal/triagearr"
)

func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := store.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	require.NoError(t, s.Migrate())
	return s
}

func TestMigrate_Idempotent(t *testing.T) {
	s := openTestStore(t)
	require.NoError(t, s.Migrate())
	require.NoError(t, s.Migrate())

	var count int
	require.NoError(t, s.DB().Get(&count, `SELECT COUNT(*) FROM schema_migrations`))
	require.GreaterOrEqual(t, count, 1, "expected at least one migration recorded")

	var hasTable int
	require.NoError(t, s.DB().Get(&hasTable,
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='snapshots_raw'`))
	require.Equal(t, 1, hasTable)
}

func TestUpsertArrInstance(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.UpsertArrInstance(ctx, triagearr.ArrTypeSonarr, "http://sonarr:8989", true, ""))
	require.NoError(t, s.UpsertArrInstance(ctx, triagearr.ArrTypeSonarr, "http://sonarr:8989", false, "boom"))

	rows, err := s.ListArrInstances(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.False(t, rows[0].Healthy)
	require.NotNil(t, rows[0].LastError)
	require.Equal(t, "boom", *rows[0].LastError)
}

func TestUpsertTorrentAndSnapshot(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	tor := triagearr.Torrent{
		Hash: "abc123", Name: "Foo", Category: "tv", SavePath: "/data/torrents",
		Size: 1024, AddedOn: now.Add(-24 * time.Hour),
		Ratio: 2.5, Seeders: 10, Leechers: 1, State: "uploading",
	}
	require.NoError(t, s.UpsertTorrent(ctx, tor))

	snap := triagearr.Snapshot{
		Hash: tor.Hash, Timestamp: now, Ratio: 2.5,
		Uploaded: 2560, Seeders: 10, Leechers: 1,
		State: "uploading", LastActivity: now.Add(-time.Hour),
	}
	require.NoError(t, s.InsertSnapshot(ctx, snap))

	rows, err := s.ListTorrentsLatest(ctx, "name", 0)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "abc123", rows[0].Hash)
	require.NotNil(t, rows[0].Ratio)
	require.InDelta(t, 2.5, *rows[0].Ratio, 1e-9)
	require.NotNil(t, rows[0].Seeders)
	require.Equal(t, 10, *rows[0].Seeders)
}

func TestListTorrentsLatest_SortAndLimit(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	for i, name := range []string{"a", "b", "c"} {
		h := triagearr.Hash(name + "_hash")
		require.NoError(t, s.UpsertTorrent(ctx, triagearr.Torrent{
			Hash: h, Name: name, Size: int64(100 * (i + 1)), AddedOn: now,
		}))
		require.NoError(t, s.InsertSnapshot(ctx, triagearr.Snapshot{
			Hash: h, Timestamp: now, Seeders: i + 1, State: "uploading", LastActivity: now,
		}))
	}

	rows, err := s.ListTorrentsLatest(ctx, "seeders", 2)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.Equal(t, "c", rows[0].Name)
	require.Equal(t, "b", rows[1].Name)

	rows, err = s.ListTorrentsLatest(ctx, "size", 0)
	require.NoError(t, err)
	require.Equal(t, "c", rows[0].Name)

	_, err = s.ListTorrentsLatest(ctx, "bogus", 0)
	require.Error(t, err)
}

func TestResolveTorrentHash(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	hashes := []triagearr.Hash{
		"b1cb9b6ba0a0d15da62c873284f7d6f72d4b8316",
		"b1cb9b6bffffffffffffffffffffffffffffffff",
		"fa5b5d3032816abad463563c926d96853a9ce12b",
	}
	for _, h := range hashes {
		require.NoError(t, s.UpsertTorrent(ctx, triagearr.Torrent{
			Hash: h, Name: string(h[:6]), AddedOn: now,
		}))
	}

	// Unique short prefix.
	got, err := s.ResolveTorrentHash(ctx, "fa5b5d")
	require.NoError(t, err)
	require.Equal(t, hashes[2], got)

	// Full hash passes through.
	got, err = s.ResolveTorrentHash(ctx, string(hashes[0]))
	require.NoError(t, err)
	require.Equal(t, hashes[0], got)

	// Case-insensitive.
	got, err = s.ResolveTorrentHash(ctx, "FA5B5D")
	require.NoError(t, err)
	require.Equal(t, hashes[2], got)

	// Ambiguous prefix returns ErrHashAmbiguous with candidates.
	_, err = s.ResolveTorrentHash(ctx, "b1cb9b6b")
	var ambig *store.ErrHashAmbiguous
	require.ErrorAs(t, err, &ambig)
	require.Len(t, ambig.Candidates, 2)

	// Not found.
	_, err = s.ResolveTorrentHash(ctx, "deadbeef")
	require.ErrorIs(t, err, store.ErrHashNotFound)

	// Empty rejects without DB hit.
	_, err = s.ResolveTorrentHash(ctx, "   ")
	require.ErrorIs(t, err, store.ErrHashNotFound)
}

func TestUpsertMediaAndCount(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.UpsertMedia(ctx, triagearr.MediaItem{
		ID: 1, ArrType: triagearr.ArrTypeSonarr,
		Title: "S1", Path: "/m/S1", Size: 10, Tags: []string{"a", "b"},
	}))
	require.NoError(t, s.UpsertMedia(ctx, triagearr.MediaItem{
		ID: 1, ArrType: triagearr.ArrTypeSonarr,
		Title: "S1-updated", Path: "/m/S1", Size: 20,
	}))
	require.NoError(t, s.UpsertMedia(ctx, triagearr.MediaItem{
		ID: 2, ArrType: triagearr.ArrTypeSonarr, Title: "S2",
	}))

	n, err := s.CountMedia(ctx, triagearr.ArrTypeSonarr)
	require.NoError(t, err)
	require.Equal(t, 2, n)
}

func TestInsertDiskUsageAndLatest(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	t0 := time.Now().UTC().Truncate(time.Second).Add(-time.Hour)
	t1 := t0.Add(30 * time.Minute)

	require.NoError(t, s.InsertDiskUsage(ctx, triagearr.DiskUsage{
		Path: "/data", Timestamp: t0,
		TotalBytes: 1000, UsedBytes: 600, FreeBytes: 400, FreePercent: 40,
	}))
	require.NoError(t, s.InsertDiskUsage(ctx, triagearr.DiskUsage{
		Path: "/data", Timestamp: t1,
		TotalBytes: 1000, UsedBytes: 700, FreeBytes: 300, FreePercent: 30,
	}))

	row, err := s.LatestDiskUsage(ctx)
	require.NoError(t, err)
	require.NotNil(t, row)
	require.InDelta(t, 30.0, row.FreePercent, 1e-9)
	require.True(t, row.Timestamp.Equal(t1))
}
