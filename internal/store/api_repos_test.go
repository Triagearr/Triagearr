package store_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Triagearr/Triagearr/internal/store"
	"github.com/Triagearr/Triagearr/internal/triagearr"
)

// seedTorrentsFixture lays down three torrents with latest snapshots and
// scores so the API read paths have something to filter, sort and count.
//
//	alpha — public, category "tv",     seeders 3, ratio 1.0, score 80, eligible
//	bravo — private, category "movies", seeders 1, ratio 4.0, score 20, eligible
//	delta — public, category "tv",     seeders 9, ratio 0.5, score 95, excluded
func seedTorrentsFixture(t *testing.T, s *store.Store) time.Time {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	require.NoError(t, s.UpsertTorrents(ctx, []triagearr.Torrent{
		{Hash: "alpha", Name: "Alpha", Category: "tv", SavePath: "/data/tv", Size: 300, Private: false, AddedOn: now.Add(-72 * time.Hour)},
		{Hash: "bravo", Name: "Bravo", Category: "movies", SavePath: "/data/movies", Size: 100, Private: true, AddedOn: now.Add(-48 * time.Hour)},
		{Hash: "delta", Name: "Delta", Category: "tv", SavePath: "/data/tv", Size: 200, Private: false, AddedOn: now.Add(-24 * time.Hour)},
	}))

	for _, sn := range []triagearr.Snapshot{
		{Hash: "alpha", Timestamp: now, Ratio: 1.0, Uploaded: 300, Seeders: 3, Leechers: 0, State: "uploading", LastActivity: now},
		{Hash: "bravo", Timestamp: now, Ratio: 4.0, Uploaded: 400, Seeders: 1, Leechers: 2, State: "stalledUP", LastActivity: now},
		{Hash: "delta", Timestamp: now, Ratio: 0.5, Uploaded: 100, Seeders: 9, Leechers: 1, State: "uploading", LastActivity: now},
	} {
		require.NoError(t, s.InsertSnapshot(ctx, sn))
	}

	for _, sc := range []store.ScoreRow{
		{Hash: "alpha", Score: 80, Private: false, AnyTrackerAlive: true, Excluded: false, ComputedAt: now},
		{Hash: "bravo", Score: 20, Private: true, AnyTrackerAlive: true, Excluded: false, ComputedAt: now},
		{Hash: "delta", Score: 95, Private: false, AnyTrackerAlive: true, Excluded: true, ExclusionReasons: "hnr_window", ComputedAt: now},
	} {
		require.NoError(t, s.UpsertScore(ctx, sc))
	}
	return now
}

func TestListTorrentsFiltered_DefaultSortAndScores(t *testing.T) {
	s := openTestStore(t)
	seedTorrentsFixture(t, s)
	ctx := context.Background()

	rows, err := s.ListTorrentsFiltered(ctx, store.ListTorrentsOpts{})
	require.NoError(t, err)
	require.Len(t, rows, 3)
	// Default sort is name ascending.
	require.Equal(t, "Alpha", rows[0].Name)
	require.Equal(t, "Bravo", rows[1].Name)
	require.Equal(t, "Delta", rows[2].Name)
	// The scores join is always present.
	require.NotNil(t, rows[0].Score)
	require.InDelta(t, 80, *rows[0].Score, 1e-9)
}

func TestListTorrentsFiltered_QueryAndCategory(t *testing.T) {
	s := openTestStore(t)
	seedTorrentsFixture(t, s)
	ctx := context.Background()

	// Case-insensitive substring on name.
	rows, err := s.ListTorrentsFiltered(ctx, store.ListTorrentsOpts{Query: "alph"})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "Alpha", rows[0].Name)

	rows, err = s.ListTorrentsFiltered(ctx, store.ListTorrentsOpts{Category: "tv"})
	require.NoError(t, err)
	require.Len(t, rows, 2)

	n, err := s.CountTorrentsFiltered(ctx, store.ListTorrentsOpts{Category: "tv"})
	require.NoError(t, err)
	require.Equal(t, 2, n)
}

func TestListTorrentsFiltered_PrivateAndExcluded(t *testing.T) {
	s := openTestStore(t)
	seedTorrentsFixture(t, s)
	ctx := context.Background()

	rows, err := s.ListTorrentsFiltered(ctx, store.ListTorrentsOpts{PrivateOnly: true})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "Bravo", rows[0].Name)

	// ExcludedOnly forces the scores join in the count query too.
	rows, err = s.ListTorrentsFiltered(ctx, store.ListTorrentsOpts{ExcludedOnly: true})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "Delta", rows[0].Name)

	n, err := s.CountTorrentsFiltered(ctx, store.ListTorrentsOpts{ExcludedOnly: true})
	require.NoError(t, err)
	require.Equal(t, 1, n)
}

func TestListTorrentsFiltered_SortDirectionsAndPaging(t *testing.T) {
	s := openTestStore(t)
	seedTorrentsFixture(t, s)
	ctx := context.Background()

	// Score descending (default dir for the score key): delta(95), alpha(80), bravo(20).
	rows, err := s.ListTorrentsFiltered(ctx, store.ListTorrentsOpts{Sort: "score"})
	require.NoError(t, err)
	require.Equal(t, "Delta", rows[0].Name)
	require.Equal(t, "Bravo", rows[2].Name)

	// Explicit ascending order flips it.
	rows, err = s.ListTorrentsFiltered(ctx, store.ListTorrentsOpts{Sort: "size", Order: "asc"})
	require.NoError(t, err)
	require.Equal(t, "Bravo", rows[0].Name) // size 100
	require.Equal(t, "Alpha", rows[2].Name) // size 300

	// Limit + offset paginate a seeders-desc listing: delta(9), alpha(3), bravo(1).
	rows, err = s.ListTorrentsFiltered(ctx, store.ListTorrentsOpts{Sort: "seeders", Limit: 1, Offset: 1})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "Alpha", rows[0].Name)
}

func TestListTorrentsFiltered_BadSortRejected(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	_, err := s.ListTorrentsFiltered(ctx, store.ListTorrentsOpts{Sort: "bogus"})
	require.Error(t, err)
}

func TestDistinctCategoriesAndCounts(t *testing.T) {
	s := openTestStore(t)
	seedTorrentsFixture(t, s)
	ctx := context.Background()

	cats, err := s.DistinctCategories(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{"movies", "tv"}, cats)

	n, err := s.CountTorrents(ctx)
	require.NoError(t, err)
	require.Equal(t, 3, n)

	// Only the two non-excluded scores count as scored.
	scored, err := s.CountScored(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, scored)
}

func TestGetTorrent_DetailAndMissing(t *testing.T) {
	s := openTestStore(t)
	seedTorrentsFixture(t, s)
	ctx := context.Background()

	row, err := s.GetTorrent(ctx, "bravo")
	require.NoError(t, err)
	require.Equal(t, "Bravo", row.Name)
	require.Equal(t, "/data/movies", row.SavePath)
	require.True(t, row.Private)
	require.NotNil(t, row.Ratio)
	require.InDelta(t, 4.0, *row.Ratio, 1e-9)
	require.NotNil(t, row.Uploaded)
	require.Equal(t, int64(400), *row.Uploaded)

	_, err = s.GetTorrent(ctx, "ghost")
	require.ErrorIs(t, err, sql.ErrNoRows)
}

func TestListSnapshotsRaw_WindowAndOrder(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	require.NoError(t, s.UpsertTorrent(ctx, triagearr.Torrent{Hash: "h", Name: "H", AddedOn: now}))
	for i := 0; i < 4; i++ {
		require.NoError(t, s.InsertSnapshot(ctx, triagearr.Snapshot{
			Hash: "h", Timestamp: now.Add(time.Duration(i) * time.Hour),
			Ratio: float64(i), Uploaded: int64(i * 100), Seeders: i, State: "uploading", LastActivity: now,
		}))
	}

	// since cuts off the two earliest points; result is ascending by ts.
	pts, err := s.ListSnapshotsRaw(ctx, "h", now.Add(2*time.Hour), 0)
	require.NoError(t, err)
	require.Len(t, pts, 2)
	require.True(t, pts[0].Timestamp.Before(pts[1].Timestamp))
	require.InDelta(t, 2.0, pts[0].Ratio, 1e-9)
}

func TestListDiskUsageHistory(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	t0 := time.Now().UTC().Truncate(time.Second).Add(-2 * time.Hour)

	for i := 0; i < 3; i++ {
		require.NoError(t, s.InsertDiskUsage(ctx, triagearr.DiskUsage{
			Path: "/data", Timestamp: t0.Add(time.Duration(i) * time.Hour),
			TotalBytes: 1000, UsedBytes: uint64(500 + i*100),
			FreeBytes: uint64(500 - i*100), FreePercent: float64(50 - i*10),
		}))
	}

	pts, err := s.ListDiskUsageHistory(ctx, t0.Add(time.Hour), 0)
	require.NoError(t, err)
	require.Len(t, pts, 2)
	require.True(t, pts[0].Timestamp.Before(pts[1].Timestamp))
}

func TestListActionsRecent_OrderingAndCount(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	runID, err := s.InsertRun(ctx, triagearr.Run{
		TriggeredBy: triagearr.RunTriggerHTTP, TriggeredAt: now, Mode: "dry-run", Status: "completed",
	})
	require.NoError(t, err)

	for i, st := range []triagearr.ActionStatus{triagearr.ActionSucceeded, triagearr.ActionRunning, triagearr.ActionFailedQbit} {
		_, err := s.InsertAction(ctx, triagearr.Action{
			RunID: runID, Rank: i, TorrentHash: triagearr.Hash("h"),
			StartedAt: now.Add(time.Duration(i) * time.Minute), Status: st, FreedBytes: int64(i * 10),
		})
		require.NoError(t, err)
	}

	n, err := s.CountActions(ctx)
	require.NoError(t, err)
	require.Equal(t, 3, n)

	// Newest started_at first.
	acts, err := s.ListActionsRecent(ctx, 2, 0)
	require.NoError(t, err)
	require.Len(t, acts, 2)
	require.Equal(t, triagearr.ActionFailedQbit, acts[0].Status)
	require.Equal(t, triagearr.ActionRunning, acts[1].Status)

	// Offset paginates into the tail.
	acts, err = s.ListActionsRecent(ctx, 2, 2)
	require.NoError(t, err)
	require.Len(t, acts, 1)
	require.Equal(t, triagearr.ActionSucceeded, acts[0].Status)
}

func TestScoring_ListAndGet(t *testing.T) {
	s := openTestStore(t)
	seedTorrentsFixture(t, s)
	ctx := context.Background()

	// ListTorrentsForScoring streams every torrent ordered by hash.
	scoring, err := s.ListTorrentsForScoring(ctx)
	require.NoError(t, err)
	require.Len(t, scoring, 3)
	require.Equal(t, "alpha", scoring[0].Hash)
	require.True(t, scoring[1].Private) // bravo

	// ListScores defaults to eligible-only, score descending, with names.
	eligible, err := s.ListScores(ctx, store.ListScoresOpts{})
	require.NoError(t, err)
	require.Len(t, eligible, 2)
	require.Equal(t, "Alpha", eligible[0].Name) // score 80 beats bravo's 20

	withExcluded, err := s.ListScores(ctx, store.ListScoresOpts{IncludeExcluded: true, Limit: 1})
	require.NoError(t, err)
	require.Len(t, withExcluded, 1)
	require.Equal(t, "Delta", withExcluded[0].Name) // score 95, excluded

	got, err := s.GetScore(ctx, "delta")
	require.NoError(t, err)
	require.True(t, got.Excluded)
	require.Equal(t, "hnr_window", got.ExclusionReasons)

	_, err = s.GetScore(ctx, "ghost")
	require.ErrorIs(t, err, sql.ErrNoRows)
}

func TestTorrentSavePathAndNames(t *testing.T) {
	s := openTestStore(t)
	seedTorrentsFixture(t, s)
	ctx := context.Background()

	sp, err := s.TorrentSavePath(ctx, "alpha")
	require.NoError(t, err)
	require.Equal(t, "/data/tv", sp)

	names, err := s.TorrentNamesByHashes(ctx, []triagearr.Hash{"alpha", "delta", "ghost"})
	require.NoError(t, err)
	require.Equal(t, "Alpha", names["alpha"])
	require.Equal(t, "Delta", names["delta"])
	require.NotContains(t, names, triagearr.Hash("ghost"), "unknown hashes are omitted")

	// Empty input short-circuits to an empty map.
	empty, err := s.TorrentNamesByHashes(ctx, nil)
	require.NoError(t, err)
	require.Empty(t, empty)
}
