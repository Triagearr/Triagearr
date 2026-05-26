package store_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Triagearr/Triagearr/internal/triagearr"
)

func TestScoringDefaults_SeededAndUpdatable(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	// Singleton row is seeded by the migration with conservative defaults.
	d, err := s.GetScoringDefaults(ctx)
	require.NoError(t, err)
	require.Equal(t, 1.0, d.MinRatio)
	require.Equal(t, 30, d.MinSeedDays)
	require.Equal(t, 3, d.RareThreshold)

	require.NoError(t, s.SetScoringDefaults(ctx, triagearr.ScoringDefaults{
		MinRatio:      0.7,
		MinSeedDays:   14,
		RareThreshold: 5,
	}))

	d, err = s.GetScoringDefaults(ctx)
	require.NoError(t, err)
	require.Equal(t, 0.7, d.MinRatio)
	require.Equal(t, 14, d.MinSeedDays)
	require.Equal(t, 5, d.RareThreshold)
}

func TestTrackerPolicies_CRUD(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	threshold := 5
	saved, err := s.UpsertTrackerPolicy(ctx, triagearr.TrackerPolicy{
		TrackerHost:   "ygg.example",
		MinRatio:      1.5,
		MinSeedDays:   45,
		RareThreshold: &threshold,
		Enabled:       true,
	})
	require.NoError(t, err)
	require.Equal(t, "ygg.example", saved.TrackerHost)
	require.NotNil(t, saved.RareThreshold)
	require.Equal(t, 5, *saved.RareThreshold)

	// Upsert is idempotent: a second write updates in place.
	saved2, err := s.UpsertTrackerPolicy(ctx, triagearr.TrackerPolicy{
		TrackerHost: "ygg.example",
		MinRatio:    2.0,
		MinSeedDays: 60,
		Enabled:     false,
	})
	require.NoError(t, err)
	require.Equal(t, 2.0, saved2.MinRatio)
	require.False(t, saved2.Enabled)
	require.Nil(t, saved2.RareThreshold, "passing a zero-value (nil) override must clear the column")

	got, err := s.GetTrackerPolicy(ctx, "ygg.example")
	require.NoError(t, err)
	require.Equal(t, saved2, got)

	require.NoError(t, s.DeleteTrackerPolicy(ctx, "ygg.example"))
	_, err = s.GetTrackerPolicy(ctx, "ygg.example")
	require.True(t, errors.Is(err, sql.ErrNoRows))
	require.True(t, errors.Is(s.DeleteTrackerPolicy(ctx, "ygg.example"), sql.ErrNoRows))
}

func TestTrackerPolicies_ListMergesWithDiscoveredHosts(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	// Two torrents on three trackers: tracker.a (alive), tracker.b (alive),
	// tracker.dead (all-dead). Only tracker.a has a configured override.
	require.NoError(t, s.ReplaceTrackers(ctx, "t1", []triagearr.TrackerInfo{
		{URL: "https://a/announce", Host: "tracker.a", Status: triagearr.TrackerWorking},
		{URL: "https://dead/announce", Host: "tracker.dead", Status: triagearr.TrackerNotWorking},
	}))
	require.NoError(t, s.ReplaceTrackers(ctx, "t2", []triagearr.TrackerInfo{
		{URL: "https://a/announce", Host: "tracker.a", Status: triagearr.TrackerWorking},
		{URL: "https://b/announce", Host: "tracker.b", Status: triagearr.TrackerWorking},
	}))
	_, err := s.UpsertTrackerPolicy(ctx, triagearr.TrackerPolicy{
		TrackerHost: "tracker.a", MinRatio: 1.0, MinSeedDays: 30, Enabled: true,
	})
	require.NoError(t, err)
	// An orphan policy: configured for a host that has no torrents.
	_, err = s.UpsertTrackerPolicy(ctx, triagearr.TrackerPolicy{
		TrackerHost: "tracker.orphan", MinRatio: 2.0, MinSeedDays: 60, Enabled: true,
	})
	require.NoError(t, err)

	stats, err := s.ListTrackerHostStats(ctx)
	require.NoError(t, err)

	by := map[string]int{}
	for i, st := range stats {
		by[st.Host] = i
	}
	require.Contains(t, by, "tracker.a")
	require.Contains(t, by, "tracker.b")
	require.Contains(t, by, "tracker.dead")
	require.Contains(t, by, "tracker.orphan")

	require.Equal(t, 2, stats[by["tracker.a"]].TorrentCount)
	require.True(t, stats[by["tracker.a"]].AnyAlive)
	require.NotNil(t, stats[by["tracker.a"]].Policy)

	require.True(t, stats[by["tracker.dead"]].AllDead)
	require.False(t, stats[by["tracker.dead"]].AnyAlive)
	require.Nil(t, stats[by["tracker.dead"]].Policy, "dead tracker has no configured override")

	require.Equal(t, 0, stats[by["tracker.orphan"]].TorrentCount, "orphan policy surfaces with zero torrents so the UI can clean it up")
	require.NotNil(t, stats[by["tracker.orphan"]].Policy)
}
