package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Triagearr/Triagearr/internal/store"
	"github.com/Triagearr/Triagearr/internal/triagearr"
)

// TestScoringSnapshotStatsAll_MatchesPerHash is the equivalence gate for the
// bulk scoring loader: for every seeded hash, ScoringSnapshotStatsAll must
// return exactly what calling the per-hash ScoringSnapshotStats would. The
// seed deliberately exercises the branches where the bulk SQL could diverge
// from the per-hash SQL: raw-only, daily-only, blended, uploaded_max=0,
// single-point (span=0), no-snapshots, and the windowed-anchor /
// unwindowed-newest asymmetry.
func TestScoringSnapshotStatsAll_MatchesPerHash(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	// Pinned so the 7-day and 30-day cutoffs are deterministic:
	//   cutoff7d  = 2026-05-12 12:00Z  (date 2026-05-12)
	//   cutoff30d = 2026-04-19 12:00Z  (date 2026-04-19)
	now := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)
	const gb = int64(1 << 30)

	rawAt := func(hash string, daysAgo int, uploaded int64, ratio float64, seeders int) {
		t.Helper()
		tsx := now.Add(time.Duration(-daysAgo) * 24 * time.Hour)
		require.NoError(t, s.InsertSnapshot(ctx, triagearr.Snapshot{
			Hash: triagearr.Hash(hash), Timestamp: tsx, Uploaded: uploaded,
			Ratio: ratio, Seeders: seeders, State: "uploading", LastActivity: tsx,
		}))
	}
	daily := func(hash, day string, ratioAvg, seedersAvg float64, uploadedMax int64) {
		t.Helper()
		_, err := s.DB().ExecContext(ctx, `
			INSERT INTO snapshots_daily(torrent_hash, day, ratio_avg, ratio_min, ratio_max, seeders_avg, seeders_min, seeders_max, uploaded_max, samples)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 1)
		`, hash, day, ratioAvg, ratioAvg, ratioAvg, seedersAvg, int64(seedersAvg), int64(seedersAvg), uploadedMax)
		require.NoError(t, err)
	}

	// rawonly: newest=day-1, anchor=oldest-in-window (day-20).
	rawAt("rawonly", 20, 0, 1.0, 5)
	rawAt("rawonly", 10, 10*gb, 1.5, 8)
	rawAt("rawonly", 3, 30*gb, 2.0, 12)
	rawAt("rawonly", 1, 50*gb, 2.5, 20)

	// dailyonly: ratio + velocity rebuilt from daily; only the 05-18 bucket is
	// inside the 7-day seeders window.
	daily("dailyonly", "2026-05-01", 1.0, 4, 5*gb)
	daily("dailyonly", "2026-05-10", 1.5, 6, 15*gb)
	daily("dailyonly", "2026-05-18", 2.2, 9, 25*gb)

	// mixed: raw wins latest-ratio; seeders blends; velocity newest is raw and
	// anchor is the oldest daily inside the 30-day window (cross-source).
	daily("mixed", "2026-04-25", 1.0, 3, 2*gb)
	daily("mixed", "2026-05-05", 1.2, 5, 8*gb)
	rawAt("mixed", 5, 20*gb, 2.0, 10)
	rawAt("mixed", 1, 28*gb, 2.4, 14)

	// dailyzeroup: uploaded_max=0 ⇒ no velocity point at all ⇒ velocity 0, but
	// seeders/ratio still populated.
	daily("dailyzeroup", "2026-05-02", 1.3, 5, 0)
	daily("dailyzeroup", "2026-05-15", 1.7, 7, 0)

	// single: one point ⇒ newest==anchor ⇒ span 0 ⇒ velocity 0.
	rawAt("single", 2, 12*gb, 1.9, 6)

	// outsidewindow: every raw point is older than 30 days. latest-ratio has no
	// time filter (so it resolves), but the anchor query is windowed (so it
	// finds nothing) ⇒ velocity 0, seeders 0.
	rawAt("outsidewindow", 40, 5*gb, 1.1, 3)
	rawAt("outsidewindow", 35, 9*gb, 1.4, 4)

	// empty: never seeded; must be absent from the bulk map and zero per-hash.
	hashes := []string{"rawonly", "dailyonly", "mixed", "dailyzeroup", "single", "outsidewindow", "empty"}

	all, err := s.ScoringSnapshotStatsAll(ctx, now)
	require.NoError(t, err)

	for _, h := range hashes {
		want, err := s.ScoringSnapshotStats(ctx, triagearr.Hash(h), now)
		require.NoError(t, err, "per-hash stats for %s", h)
		got := all[h] // zero value when absent — the documented equivalent of an empty per-hash result.

		require.InDelta(t, want.SeedersAvg7d, got.SeedersAvg7d, 1e-9, "SeedersAvg7d for %s", h)
		require.InDelta(t, want.LatestRatio, got.LatestRatio, 1e-9, "LatestRatio for %s", h)
		if want.VelocityBytesPerDay == 0 {
			require.Zero(t, got.VelocityBytesPerDay, "VelocityBytesPerDay for %s", h)
		} else {
			require.InEpsilon(t, want.VelocityBytesPerDay, got.VelocityBytesPerDay, 1e-9, "VelocityBytesPerDay for %s", h)
		}
	}

	// "empty" must genuinely be absent from the map (not merely zero-valued).
	_, present := all["empty"]
	require.False(t, present, "snapshot-less hash should be absent from the bulk map")
}

// TestUpsertScores_BatchRoundTrip proves the batched writer persists the same
// score + factor rows as the per-row UpsertScore, including ON CONFLICT
// overwrite and the score_factors child row (FK parent-first ordering).
func TestUpsertScores_BatchRoundTrip(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC).Truncate(time.Second)

	rows := []store.ScoreRow{
		{Hash: "a", Score: 80, Private: false, AnyTrackerAlive: true, Excluded: false, FactorsJSON: `[{"name":"f1"}]`, ComputedAt: now},
		{Hash: "b", Score: 20, Private: true, AnyTrackerAlive: true, Excluded: true, ExclusionReasons: "hnr_window", FactorsJSON: `[{"name":"f2"}]`, ComputedAt: now},
	}
	require.NoError(t, s.UpsertScores(ctx, rows))

	for _, want := range rows {
		got, err := s.GetScore(ctx, triagearr.Hash(want.Hash))
		require.NoError(t, err, "GetScore(%s)", want.Hash)
		require.InDelta(t, want.Score, got.Score, 1e-9)
		require.Equal(t, want.Private, got.Private)
		require.Equal(t, want.Excluded, got.Excluded)
		require.Equal(t, want.ExclusionReasons, got.ExclusionReasons)
		require.Equal(t, want.FactorsJSON, got.FactorsJSON, "score_factors must be persisted for %s", want.Hash)
	}

	// ON CONFLICT overwrite: re-run with changed values, same hashes.
	rows[0].Score = 99
	rows[0].FactorsJSON = `[{"name":"f1b"}]`
	require.NoError(t, s.UpsertScores(ctx, rows))
	got, err := s.GetScore(ctx, "a")
	require.NoError(t, err)
	require.InDelta(t, 99.0, got.Score, 1e-9)
	require.Equal(t, `[{"name":"f1b"}]`, got.FactorsJSON)

	// Empty batch is a no-op, not an error.
	require.NoError(t, s.UpsertScores(ctx, nil))
}
