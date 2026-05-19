package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Triagearr/Triagearr/internal/triagearr"
)

func TestReplaceTrackers_RemovesStale(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.ReplaceTrackers(ctx, "h1", []triagearr.TrackerInfo{
		{URL: "https://a/announce", Host: "a", Status: triagearr.TrackerWorking},
		{URL: "https://b/announce", Host: "b", Status: triagearr.TrackerWorking},
	}))
	require.NoError(t, s.ReplaceTrackers(ctx, "h1", []triagearr.TrackerInfo{
		{URL: "https://b/announce", Host: "b", Status: triagearr.TrackerWorking},
	}))

	rows, err := s.ListTrackers(ctx, "h1")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "b", rows[0].Host)
}

func TestUpsertMediaFile_RoundTrip(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.UpsertMediaFile(ctx, triagearr.MediaFile{
		ArrName: "main", ArrType: triagearr.ArrTypeSonarr,
		FileID: 42, MediaID: 7, Path: "/files/tv/Foo/E01.mkv", Size: 1000,
	}))
	require.NoError(t, s.UpsertMediaFile(ctx, triagearr.MediaFile{
		ArrName: "main", ArrType: triagearr.ArrTypeSonarr,
		FileID: 42, MediaID: 7, Path: "/files/tv/Foo/E01.mkv", Size: 2000,
	}))

	rows, err := s.ListMediaFilesByMedia(ctx, "main", triagearr.ArrTypeSonarr, 7)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, int64(2000), rows[0].Size)
}

func TestDownsampleRange_AggregatesAndDeletes(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	// Anchor 5 days in the past so date-bucketing is stable regardless of clock drift.
	base := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	require.NoError(t, s.UpsertTorrent(ctx, triagearr.Torrent{Hash: "h1", Name: "Foo", AddedOn: base}))

	// Three samples on day 1, two on day 2.
	day1 := base
	day2 := base.Add(24 * time.Hour)
	for i, ts := range []time.Time{day1, day1.Add(time.Hour), day1.Add(2 * time.Hour), day2, day2.Add(time.Hour)} {
		require.NoError(t, s.InsertSnapshot(ctx, triagearr.Snapshot{
			Hash: "h1", Timestamp: ts,
			Ratio: float64(i + 1), Seeders: i + 1, State: "uploading", LastActivity: ts,
		}))
	}

	cutoff := day2.Add(48 * time.Hour) // well in the future relative to the synthetic data
	daily, rawDeleted, err := s.DownsampleRange(ctx, cutoff)
	require.NoError(t, err)
	require.Equal(t, 2, daily, "expected one daily row per distinct day")
	require.Equal(t, 5, rawDeleted)

	// Calling again must be idempotent: no raw rows left, but daily survives.
	daily2, rawDeleted2, err := s.DownsampleRange(ctx, cutoff)
	require.NoError(t, err)
	require.Equal(t, 0, daily2)
	require.Equal(t, 0, rawDeleted2)
}

func TestEnforceRetention(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	require.NoError(t, s.UpsertTorrent(ctx, triagearr.Torrent{Hash: "h1", Name: "Foo", AddedOn: now}))
	require.NoError(t, s.InsertSnapshot(ctx, triagearr.Snapshot{
		Hash: "h1", Timestamp: now.Add(-30 * 24 * time.Hour),
		Ratio: 1, Seeders: 1, State: "uploading", LastActivity: now,
	}))
	require.NoError(t, s.InsertSnapshot(ctx, triagearr.Snapshot{
		Hash: "h1", Timestamp: now.Add(-time.Hour),
		Ratio: 1, Seeders: 1, State: "uploading", LastActivity: now,
	}))

	rawDel, _, err := s.EnforceRetention(ctx, 7*24*time.Hour, 365*24*time.Hour)
	require.NoError(t, err)
	require.Equal(t, 1, rawDel, "the 30-day-old row must be dropped")
}

func TestArrImports_JoinFiltersOrphanedFileIDs(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	// Two imports under the same download_id (season pack: 2 episodes).
	rec1 := triagearr.ImportRecord{
		HistoryID: 100, FileID: 10, DownloadID: "abcd1234",
		DroppedPath:  "/files/torrents/pack/E01.mkv",
		ImportedPath: "/files/media/E01.mkv", Size: 1000, ImportedAt: now,
	}
	rec2 := triagearr.ImportRecord{
		HistoryID: 101, FileID: 11, DownloadID: "abcd1234",
		DroppedPath:  "/files/torrents/pack/E02.mkv",
		ImportedPath: "/files/media/E02.mkv", Size: 2000, ImportedAt: now,
	}
	require.NoError(t, s.UpsertArrImport(ctx, "main", triagearr.ArrTypeSonarr, rec1))
	require.NoError(t, s.UpsertArrImport(ctx, "main", triagearr.ArrTypeSonarr, rec2))

	// Only the first fileId still exists in media_files (the second was deleted/upgraded).
	require.NoError(t, s.UpsertMediaFile(ctx, triagearr.MediaFile{
		ArrName: "main", ArrType: triagearr.ArrTypeSonarr,
		FileID: 10, MediaID: 7, Path: "/files/media/E01.mkv", Size: 1000,
	}))

	got, err := s.LinksByHash(ctx, "abcd1234")
	require.NoError(t, err)
	require.Len(t, got, 1, "the orphaned arr_imports row (fileId=11 absent from media_files) must be filtered out")
	require.Equal(t, int64(10), got[0].FileID)
	require.Equal(t, "/files/media/E01.mkv", got[0].LivePath)
}

func TestMaxHistoryID_ReturnsZeroWhenEmpty(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	max, err := s.MaxHistoryID(ctx, "main", triagearr.ArrTypeSonarr)
	require.NoError(t, err)
	require.Equal(t, int64(0), max)

	require.NoError(t, s.UpsertArrImport(ctx, "main", triagearr.ArrTypeSonarr, triagearr.ImportRecord{
		HistoryID: 42, FileID: 1, DownloadID: "deadbeef", ImportedAt: time.Now().UTC(),
	}))
	require.NoError(t, s.UpsertArrImport(ctx, "main", triagearr.ArrTypeSonarr, triagearr.ImportRecord{
		HistoryID: 17, FileID: 2, DownloadID: "deadbeef", ImportedAt: time.Now().UTC(),
	}))
	max, err = s.MaxHistoryID(ctx, "main", triagearr.ArrTypeSonarr)
	require.NoError(t, err)
	require.Equal(t, int64(42), max)
}

func TestVacuum_SkipsBelowThreshold(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	ran, _, err := s.Vacuum(ctx, 1<<40) // 1 TiB threshold
	require.NoError(t, err)
	require.False(t, ran)
}
