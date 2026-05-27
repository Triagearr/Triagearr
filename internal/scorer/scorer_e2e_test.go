package scorer_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Triagearr/Triagearr/internal/config"
	"github.com/Triagearr/Triagearr/internal/scorer"
	"github.com/Triagearr/Triagearr/internal/store"
	"github.com/Triagearr/Triagearr/internal/triagearr"
)

// openTestStore mirrors the helper in internal/store but lives here so the
// scorer E2E test stays self-contained (avoids exporting a shared helper).
func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "scorer.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	require.NoError(t, s.Migrate())
	return s
}

// testConfig returns a ScoringConfig with explicit defaults so test assertions
// do not silently depend on applyScoringDefaults running upstream.
func testConfig() config.ScoringConfig {
	return config.ScoringConfig{
		HnRWindowDays:    14,
		TrackerDeadGrace: 7 * 24 * time.Hour,
		Weights: config.ScoringWeights{
			RatioObligationMet: 50,
			UploadVelocityInv:  30,
			AgeDays:            0.1,
			SeedersLowGuard:    -1000,
			SwarmHealthBonus:   5,
			TrackerDeadBonus:   40,
		},
	}
}

// seedSnapshots writes a synthetic time-series for one torrent. Used to drive
// the velocity factor: oldest sample has uploaded=0, latest has uploaded=u.
func seedSnapshots(t *testing.T, s *store.Store, hash triagearr.Hash, now time.Time, seeders int, totalUploaded int64) {
	t.Helper()
	ctx := context.Background()
	// Two samples spanning 1 day so velocity_bytes_per_day = totalUploaded.
	require.NoError(t, s.InsertSnapshot(ctx, triagearr.Snapshot{
		Hash: hash, Timestamp: now.Add(-24 * time.Hour),
		Ratio: 1.0, Uploaded: 0, Seeders: seeders, State: "uploading", LastActivity: now,
	}))
	require.NoError(t, s.InsertSnapshot(ctx, triagearr.Snapshot{
		Hash: hash, Timestamp: now, Ratio: 2.5, Uploaded: totalUploaded,
		Seeders: seeders, State: "uploading", LastActivity: now,
	}))
}

// findFactor returns the named factor or fails the test.
func findFactor(t *testing.T, factors []scorer.Factor, name string) scorer.Factor {
	t.Helper()
	for _, f := range factors {
		if f.Name == name {
			return f
		}
	}
	t.Fatalf("factor %q not found in %#v", name, factors)
	return scorer.Factor{}
}

func TestScoreAll_PublicHealthyVsRareVsGraveyard(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	now := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)

	// Example A — public, healthy swarm (high score).
	require.NoError(t, s.UpsertTorrent(ctx, triagearr.Torrent{
		Hash: "publichealthy", Name: "Public Healthy",
		AddedOn: now.Add(-180 * 24 * time.Hour), CompletionOn: now.Add(-179 * 24 * time.Hour),
		Private: false,
	}))
	require.NoError(t, s.ReplaceTrackers(ctx, "publichealthy", []triagearr.TrackerInfo{
		{URL: "https://public/announce", Host: "public", Status: triagearr.TrackerWorking},
	}))
	seedSnapshots(t, s, "publichealthy", now, 287, 0)

	// Example B — public, rare content (negative score: rare guard triggers).
	require.NoError(t, s.UpsertTorrent(ctx, triagearr.Torrent{
		Hash: "publicrare", Name: "Public Rare",
		AddedOn: now.Add(-180 * 24 * time.Hour), CompletionOn: now.Add(-179 * 24 * time.Hour),
		Private: false,
	}))
	require.NoError(t, s.ReplaceTrackers(ctx, "publicrare", []triagearr.TrackerInfo{
		{URL: "https://public/announce", Host: "public", Status: triagearr.TrackerWorking},
	}))
	seedSnapshots(t, s, "publicrare", now, 2, 0)

	// Example D — private, dead tracker (graveyard: top candidate).
	// CompletionOn is inside the HnR window on purpose so the test asserts
	// that the all_trackers_dead gate degrades the HnR veto.
	require.NoError(t, s.UpsertTorrent(ctx, triagearr.Torrent{
		Hash: "graveyard", Name: "Graveyard",
		AddedOn: now.Add(-120 * 24 * time.Hour), CompletionOn: now.Add(-5 * 24 * time.Hour),
		Private: true,
	}))
	// Tracker dead, first_seen_dead sustained beyond the 7d grace.
	require.NoError(t, s.ReplaceTrackers(ctx, "graveyard", []triagearr.TrackerInfo{
		{URL: "https://dead/announce", Host: "dead", Status: triagearr.TrackerNotWorking},
	}))
	_, err := s.DB().ExecContext(ctx, `
		UPDATE torrent_trackers SET first_seen_dead = ? WHERE torrent_hash = 'graveyard'
	`, now.Add(-30*24*time.Hour).Format(time.RFC3339Nano))
	require.NoError(t, err)
	seedSnapshots(t, s, "graveyard", now, 0, 0)

	// Fresh private torrent — must be vetoed by HnR (deeply negative).
	require.NoError(t, s.UpsertTorrent(ctx, triagearr.Torrent{
		Hash: "freshprivate", Name: "Fresh Private",
		AddedOn: now.Add(-3 * 24 * time.Hour), CompletionOn: now.Add(-2 * 24 * time.Hour),
		Private: true,
	}))
	require.NoError(t, s.ReplaceTrackers(ctx, "freshprivate", []triagearr.TrackerInfo{
		{URL: "https://priv/announce", Host: "priv", Status: triagearr.TrackerWorking},
	}))
	seedSnapshots(t, s, "freshprivate", now, 30, 0)

	// Excluded torrent — has tag matching qbit.tags_exclude.
	require.NoError(t, s.UpsertTorrent(ctx, triagearr.Torrent{
		Hash: "excluded", Name: "Excluded",
		AddedOn: now.Add(-180 * 24 * time.Hour), Private: false, Tags: "keep",
	}))
	require.NoError(t, s.ReplaceTrackers(ctx, "excluded", []triagearr.TrackerInfo{
		{URL: "https://public/announce", Host: "public", Status: triagearr.TrackerWorking},
	}))
	seedSnapshots(t, s, "excluded", now, 200, 0)

	sc := scorer.New(scorer.Options{
		Cfg:   testConfig(),
		Qbit:  config.TorrentClientInstanceConfig{TagsExclude: []string{"keep"}},
		Store: s,
		Now:   func() time.Time { return now },
	})

	stats, err := sc.ScoreAll(ctx)
	require.NoError(t, err)
	require.Equal(t, 5, stats.Scored)
	require.Equal(t, 1, stats.Excluded)
	require.Equal(t, 0, stats.Errors)

	// Ordering: graveyard > publichealthy > publicrare (vetoed) > freshprivate (HnR vetoed).
	rows, err := s.ListScores(ctx, store.ListScoresOpts{IncludeExcluded: true, WithFactors: true})
	require.NoError(t, err)
	require.Len(t, rows, 5)
	require.Equal(t, "graveyard", rows[0].Hash, "graveyard should top the eligible list")
	require.Greater(t, rows[0].Score, 0.0)

	// publicrare must be deeply negative (rare guard fired).
	by := map[string]store.ScoreRow{}
	for _, r := range rows {
		by[r.Hash] = r
	}
	require.Less(t, by["publicrare"].Score, -900.0, "rare guard should dominate")
	require.Less(t, by["freshprivate"].Score, -9000.0, "HnR veto should dominate")
	require.True(t, by["excluded"].Excluded)
	require.Contains(t, by["excluded"].ExclusionReasons, "qbit_tag:keep")

	// Spot-check the breakdown for the graveyard torrent.
	var factors []scorer.Factor
	require.NoError(t, json.Unmarshal([]byte(by["graveyard"].FactorsJSON), &factors))
	hnr := findFactor(t, factors, scorer.FactorHnRVeto)
	require.Equal(t, scorer.GateAllDead, hnr.Gate, "HnR veto must be gated by all_trackers_dead")
	guard := findFactor(t, factors, scorer.FactorSeedersGuard)
	require.Equal(t, scorer.GateAllDead, guard.Gate, "seeders guard must be gated by all_trackers_dead")
	dead := findFactor(t, factors, scorer.FactorTrackerDead)
	require.InDelta(t, 40.0, dead.Contribution, 1e-9)

	// Eligible-only list excludes the tagged torrent.
	eligible, err := s.ListScores(ctx, store.ListScoresOpts{})
	require.NoError(t, err)
	require.Len(t, eligible, 4)
	for _, r := range eligible {
		require.False(t, r.Excluded)
	}

	// Protect the healthy public torrent: rescoring it must surface the
	// triagearr_protected exclusion reason. Done last so it doesn't perturb
	// the eligible-count assertion above.
	require.NoError(t, s.SetTorrentProtected(ctx, "publichealthy", true))
	b, err := sc.ScoreOne(ctx, "publichealthy")
	require.NoError(t, err)
	require.True(t, b.Excluded)
	require.Contains(t, b.ExclusionReasons, "triagearr_protected")
}

func TestScoreOne_VelocityWorksAcrossDownsampleBoundary(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	now := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)

	// Active uploader: 30 days of history, uploaded grows 1 GB/day.
	const gb = int64(1 << 30)
	require.NoError(t, s.UpsertTorrent(ctx, triagearr.Torrent{
		Hash: "active", Name: "Active", Private: false,
		AddedOn: now.Add(-60 * 24 * time.Hour),
	}))
	require.NoError(t, s.ReplaceTrackers(ctx, "active", []triagearr.TrackerInfo{
		{URL: "https://t/announce", Host: "t", Status: triagearr.TrackerWorking},
	}))
	for d := 30; d >= 0; d-- {
		ts := now.Add(time.Duration(-d) * 24 * time.Hour)
		require.NoError(t, s.InsertSnapshot(ctx, triagearr.Snapshot{
			Hash: "active", Timestamp: ts, Uploaded: int64(30-d) * gb,
			Ratio: 2.0, Seeders: 50, State: "uploading", LastActivity: ts,
		}))
	}

	// Idle uploader (graveyard candidate): same 30 days but uploaded stays at 0.
	require.NoError(t, s.UpsertTorrent(ctx, triagearr.Torrent{
		Hash: "idle", Name: "Idle", Private: false,
		AddedOn: now.Add(-60 * 24 * time.Hour),
	}))
	require.NoError(t, s.ReplaceTrackers(ctx, "idle", []triagearr.TrackerInfo{
		{URL: "https://t/announce", Host: "t", Status: triagearr.TrackerWorking},
	}))
	for d := 30; d >= 0; d-- {
		ts := now.Add(time.Duration(-d) * 24 * time.Hour)
		require.NoError(t, s.InsertSnapshot(ctx, triagearr.Snapshot{
			Hash: "idle", Timestamp: ts, Uploaded: 0,
			Ratio: 5.0, Seeders: 50, State: "stalledUP", LastActivity: ts,
		}))
	}

	// Collapse everything to snapshots_daily.
	_, _, err := s.DownsampleRange(ctx, now.Add(time.Hour))
	require.NoError(t, err)

	sc := scorer.New(scorer.Options{Cfg: testConfig(), Store: s, Now: func() time.Time { return now }})

	// Active: factor should be near zero (its velocity matches the global avg).
	bActive, err := sc.ScoreOne(ctx, "active")
	require.NoError(t, err)
	fa := findFactor(t, bActive.Factors, scorer.FactorVelocityInv)
	require.Empty(t, fa.Gate, "global avg must be live → no gate")

	// Idle: velocity = 0 → factor value = 1 (max bonus).
	bIdle, err := sc.ScoreOne(ctx, "idle")
	require.NoError(t, err)
	fi := findFactor(t, bIdle.Factors, scorer.FactorVelocityInv)
	require.InDelta(t, 1.0, fi.Value, 1e-6, "idle torrent across downsample boundary should still earn the full velocity bonus")
	require.Greater(t, fi.Contribution, 0.0)
}

func TestScoreOne_PersistsAndRoundTrips(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	now := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)

	require.NoError(t, s.UpsertTorrent(ctx, triagearr.Torrent{
		Hash: "h", Name: "Foo", Private: false,
		AddedOn: now.Add(-30 * 24 * time.Hour),
	}))
	seedSnapshots(t, s, "h", now, 50, 0)

	sc := scorer.New(scorer.Options{Cfg: testConfig(), Store: s, Now: func() time.Time { return now }})
	b, err := sc.ScoreOne(ctx, "h")
	require.NoError(t, err)
	require.Equal(t, "h", b.Hash)
	require.Len(t, b.Factors, 7)

	row, err := s.GetScore(ctx, "h")
	require.NoError(t, err)
	require.InDelta(t, b.Score, row.Score, 1e-9)
	require.False(t, row.Excluded)
}
