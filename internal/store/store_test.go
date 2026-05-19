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
	require.Equal(t, 1, count, "expected exactly one migration recorded")

	var hasTable int
	require.NoError(t, s.DB().Get(&hasTable,
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='snapshots_raw'`))
	require.Equal(t, 1, hasTable)
}

func TestUpsertArrInstance(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.UpsertArrInstance(ctx, "main", triagearr.ArrTypeSonarr, "http://sonarr:8989", true, ""))
	require.NoError(t, s.UpsertArrInstance(ctx, "main", triagearr.ArrTypeSonarr, "http://sonarr:8989", false, "boom"))

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

func TestUpsertMediaAndCount(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.UpsertMedia(ctx, triagearr.MediaItem{
		ID: 1, ArrName: "main", ArrType: triagearr.ArrTypeSonarr,
		Title: "S1", Path: "/m/S1", Size: 10, Tags: []string{"a", "b"},
	}))
	require.NoError(t, s.UpsertMedia(ctx, triagearr.MediaItem{
		ID: 1, ArrName: "main", ArrType: triagearr.ArrTypeSonarr,
		Title: "S1-updated", Path: "/m/S1", Size: 20,
	}))
	require.NoError(t, s.UpsertMedia(ctx, triagearr.MediaItem{
		ID: 2, ArrName: "main", ArrType: triagearr.ArrTypeSonarr, Title: "S2",
	}))

	n, err := s.CountMedia(ctx, "main", triagearr.ArrTypeSonarr)
	require.NoError(t, err)
	require.Equal(t, 2, n)
}

func TestInsertDiskUsageAndLatest(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	t0 := time.Now().UTC().Truncate(time.Second).Add(-time.Hour)
	t1 := t0.Add(30 * time.Minute)

	require.NoError(t, s.InsertDiskUsage(ctx, triagearr.DiskUsage{
		VolumeName: "media", Path: "/share", Timestamp: t0,
		TotalBytes: 1000, UsedBytes: 600, FreeBytes: 400, FreePercent: 40,
	}))
	require.NoError(t, s.InsertDiskUsage(ctx, triagearr.DiskUsage{
		VolumeName: "media", Path: "/share", Timestamp: t1,
		TotalBytes: 1000, UsedBytes: 700, FreeBytes: 300, FreePercent: 30,
	}))

	rows, err := s.LatestDiskUsage(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.InDelta(t, 30.0, rows[0].FreePercent, 1e-9)
	require.True(t, rows[0].Timestamp.Equal(t1))
}
