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

func TestUpsertTorrent_PersistsPrivateAndTags(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	require.NoError(t, s.UpsertTorrent(ctx, triagearr.Torrent{
		Hash: "priv", Name: "P", AddedOn: now, Private: false, Tags: "hd,french",
	}))
	require.NoError(t, s.UpsertTorrent(ctx, triagearr.Torrent{
		Hash: "pub", Name: "Q", AddedOn: now, Private: true, Tags: "",
	}))

	type row struct {
		Hash    string `db:"hash"`
		Private bool   `db:"private"`
		Tags    string `db:"tags"`
	}
	var rows []row
	require.NoError(t, s.DB().SelectContext(ctx, &rows,
		`SELECT hash, private, tags FROM torrents ORDER BY hash`))
	require.Len(t, rows, 2)
	require.Equal(t, "priv", rows[0].Hash)
	require.False(t, rows[0].Private)
	require.Equal(t, "hd,french", rows[0].Tags)
	require.Equal(t, "pub", rows[1].Hash)
	require.True(t, rows[1].Private)
	require.Empty(t, rows[1].Tags)

	// Updating a row must overwrite private and tags (not stale-read defaults).
	require.NoError(t, s.UpsertTorrent(ctx, triagearr.Torrent{
		Hash: "priv", Name: "P", AddedOn: now, Private: true, Tags: "archive",
	}))
	rows = nil
	require.NoError(t, s.DB().SelectContext(ctx, &rows,
		`SELECT hash, private, tags FROM torrents WHERE hash = 'priv'`))
	require.Len(t, rows, 1)
	require.True(t, rows[0].Private)
	require.Equal(t, "archive", rows[0].Tags)
}

func TestDownsampleRange_AggregatesAndDeletes(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	// Anchor 5 days in the past so date-bucketing is stable regardless of clock drift.
	base := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	require.NoError(t, s.UpsertTorrent(ctx, triagearr.Torrent{Hash: "h1", Name: "Foo", AddedOn: base}))

	// Three samples on day 1 (uploaded 100,200,300), two on day 2 (400,500).
	day1 := base
	day2 := base.Add(24 * time.Hour)
	samples := []struct {
		ts       time.Time
		uploaded int64
	}{
		{day1, 100}, {day1.Add(time.Hour), 200}, {day1.Add(2 * time.Hour), 300},
		{day2, 400}, {day2.Add(time.Hour), 500},
	}
	for i, sm := range samples {
		require.NoError(t, s.InsertSnapshot(ctx, triagearr.Snapshot{
			Hash: "h1", Timestamp: sm.ts, Uploaded: sm.uploaded,
			Ratio: float64(i + 1), Seeders: i + 1, State: "uploading", LastActivity: sm.ts,
		}))
	}

	cutoff := day2.Add(48 * time.Hour) // well in the future relative to the synthetic data
	daily, rawDeleted, err := s.DownsampleRange(ctx, cutoff)
	require.NoError(t, err)
	require.Equal(t, 2, daily, "expected one daily row per distinct day")
	require.Equal(t, 5, rawDeleted)

	// uploaded_max must be MAX(uploaded) per day.
	type dailyRow struct {
		Day         string `db:"day"`
		UploadedMax int64  `db:"uploaded_max"`
	}
	var drows []dailyRow
	require.NoError(t, s.DB().SelectContext(ctx, &drows, `
		SELECT day, uploaded_max FROM snapshots_daily WHERE torrent_hash = 'h1' ORDER BY day
	`))
	require.Len(t, drows, 2)
	require.Equal(t, int64(300), drows[0].UploadedMax, "day1 max(uploaded) = 300")
	require.Equal(t, int64(500), drows[1].UploadedMax, "day2 max(uploaded) = 500")

	// Calling again must be idempotent: no raw rows left, but daily survives.
	daily2, rawDeleted2, err := s.DownsampleRange(ctx, cutoff)
	require.NoError(t, err)
	require.Equal(t, 0, daily2)
	require.Equal(t, 0, rawDeleted2)
}

func TestScoringSnapshotStats_VelocityFromDailyAfterRawExpired(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)

	require.NoError(t, s.UpsertTorrent(ctx, triagearr.Torrent{
		Hash: "h1", Name: "Long-running", AddedOn: now.Add(-60 * 24 * time.Hour),
	}))

	// Seed one raw sample per day for 30 days with uploaded growing by 1 GB/day.
	const gb = int64(1 << 30)
	for d := 30; d >= 0; d-- {
		ts := now.Add(time.Duration(-d) * 24 * time.Hour)
		require.NoError(t, s.InsertSnapshot(ctx, triagearr.Snapshot{
			Hash: "h1", Timestamp: ts, Uploaded: int64(30-d) * gb,
			Ratio: 2.0, Seeders: 50, State: "uploading", LastActivity: ts,
		}))
	}

	// Downsample everything → raw is empty afterwards.
	_, rawDeleted, err := s.DownsampleRange(ctx, now.Add(time.Hour))
	require.NoError(t, err)
	require.Equal(t, 31, rawDeleted)

	stats, err := s.ScoringSnapshotStats(ctx, "h1", now)
	require.NoError(t, err)
	// Velocity should reconstruct from snapshots_daily.uploaded_max. The span
	// is ~30 days, delta is 30 GB → 1 GB/day, within tolerance for date-bucket
	// edge effects.
	require.InEpsilon(t, float64(gb), stats.VelocityBytesPerDay, 0.05,
		"velocity should rebuild from snapshots_daily after raw expired")
}

func TestPruneStaleTorrents_CascadeAndKeepFresh(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	// Stale torrent: last_seen is set by UpsertTorrent itself, so we override
	// via a direct UPDATE after the upsert. AddedOn is irrelevant for prune.
	require.NoError(t, s.UpsertTorrent(ctx, triagearr.Torrent{
		Hash: "stale", Name: "Stale", AddedOn: now.Add(-30 * 24 * time.Hour),
	}))
	stalePast := now.Add(-30 * 24 * time.Hour).Format(time.RFC3339Nano)
	_, err := s.DB().ExecContext(ctx, `UPDATE torrents SET last_seen = ? WHERE hash = 'stale'`, stalePast)
	require.NoError(t, err)

	// Fresh torrent: keep default last_seen (now).
	require.NoError(t, s.UpsertTorrent(ctx, triagearr.Torrent{
		Hash: "fresh", Name: "Fresh", AddedOn: now,
	}))

	// Dependents on both, to verify cascade only hits stale.
	for _, h := range []triagearr.Hash{"stale", "fresh"} {
		require.NoError(t, s.InsertSnapshot(ctx, triagearr.Snapshot{
			Hash: h, Timestamp: now.Add(-time.Hour),
			Ratio: 1, Seeders: 1, State: "uploading", LastActivity: now,
		}))
		require.NoError(t, s.ReplaceTrackers(ctx, h, []triagearr.TrackerInfo{
			{URL: "https://t.example/announce", Host: "t.example", Status: triagearr.TrackerWorking},
		}))
	}
	// Seed snapshots_daily directly (downsample path is its own test).
	_, err = s.DB().ExecContext(ctx, `
		INSERT INTO snapshots_daily(torrent_hash, day, ratio_avg, ratio_min, ratio_max, seeders_avg, seeders_min, seeders_max, samples)
		VALUES ('stale', '2026-04-01', 1,1,1, 1,1,1, 1), ('fresh', '2026-04-01', 1,1,1, 1,1,1, 1)
	`)
	require.NoError(t, err)

	pruned, err := s.PruneStaleTorrents(ctx, 7*24*time.Hour)
	require.NoError(t, err)
	require.Equal(t, 1, pruned)

	// torrents: only fresh remains.
	hashes, err := s.ListTorrentHashes(ctx)
	require.NoError(t, err)
	require.Equal(t, []triagearr.Hash{"fresh"}, hashes)

	// Cascade landed: stale rows gone, fresh rows kept.
	type cnt struct {
		Stale int `db:"stale"`
		Fresh int `db:"fresh"`
	}
	var c cnt
	require.NoError(t, s.DB().GetContext(ctx, &c, `
		SELECT
			(SELECT COUNT(*) FROM snapshots_raw    WHERE torrent_hash = 'stale')
		  + (SELECT COUNT(*) FROM snapshots_daily  WHERE torrent_hash = 'stale')
		  + (SELECT COUNT(*) FROM torrent_trackers WHERE torrent_hash = 'stale') AS stale,
			(SELECT COUNT(*) FROM snapshots_raw    WHERE torrent_hash = 'fresh')
		  + (SELECT COUNT(*) FROM snapshots_daily  WHERE torrent_hash = 'fresh')
		  + (SELECT COUNT(*) FROM torrent_trackers WHERE torrent_hash = 'fresh') AS fresh
	`))
	require.Equal(t, 0, c.Stale, "all dependents of stale must be gone")
	require.Equal(t, 3, c.Fresh, "all dependents of fresh must survive (1 snap + 1 daily + 1 tracker)")

	// Idempotent: second call is a no-op.
	pruned2, err := s.PruneStaleTorrents(ctx, 7*24*time.Hour)
	require.NoError(t, err)
	require.Equal(t, 0, pruned2)

	// Zero grace disables.
	pruned3, err := s.PruneStaleTorrents(ctx, 0)
	require.NoError(t, err)
	require.Equal(t, 0, pruned3)
}

func TestPruneStaleTorrents_KeepsArrImports(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	require.NoError(t, s.UpsertTorrent(ctx, triagearr.Torrent{Hash: "stale", Name: "Stale", AddedOn: now}))
	_, err := s.DB().ExecContext(ctx, `UPDATE torrents SET last_seen = ? WHERE hash = 'stale'`,
		now.Add(-30*24*time.Hour).Format(time.RFC3339Nano))
	require.NoError(t, err)

	require.NoError(t, s.UpsertArrImport(ctx, "main", triagearr.ArrTypeSonarr, triagearr.ImportRecord{
		FileID: 42, DownloadID: "stale", DroppedPath: "/dl/x", ImportedPath: "/files/tv/x.mkv",
		Size: 100, HistoryID: 1, ImportedAt: now.Add(-30 * 24 * time.Hour),
	}))

	pruned, err := s.PruneStaleTorrents(ctx, 7*24*time.Hour)
	require.NoError(t, err)
	require.Equal(t, 1, pruned)

	// arr_imports must survive: it's *arr-side history, independent of qBit lifecycle.
	var n int
	require.NoError(t, s.DB().GetContext(ctx, &n,
		`SELECT COUNT(*) FROM arr_imports WHERE download_id = 'stale'`))
	require.Equal(t, 1, n)
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
