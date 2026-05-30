package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Triagearr/Triagearr/internal/triagearr"
)

func TestReplaceTrackers_FirstSeenDead_AliveToDead(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.ReplaceTrackers(ctx, "h", []triagearr.TrackerInfo{
		{URL: "https://a/announce", Host: "a", Status: triagearr.TrackerWorking},
	}))
	rows, err := s.ListTrackers(ctx, "h")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Nil(t, rows[0].FirstSeenDead, "alive tracker carries no first_seen_dead")

	before := time.Now().UTC()
	require.NoError(t, s.ReplaceTrackers(ctx, "h", []triagearr.TrackerInfo{
		{URL: "https://a/announce", Host: "a", Status: triagearr.TrackerNotWorking},
	}))
	rows, err = s.ListTrackers(ctx, "h")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.NotNil(t, rows[0].FirstSeenDead, "alive→dead transition must record first_seen_dead")
	require.WithinDuration(t, before, *rows[0].FirstSeenDead, 5*time.Second)
}

func TestReplaceTrackers_FirstSeenDead_SustainedAcrossPolls(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.ReplaceTrackers(ctx, "h", []triagearr.TrackerInfo{
		{URL: "https://a/announce", Host: "a", Status: triagearr.TrackerNotWorking},
	}))
	rows, err := s.ListTrackers(ctx, "h")
	require.NoError(t, err)
	require.NotNil(t, rows[0].FirstSeenDead)
	first := *rows[0].FirstSeenDead

	// Re-poll twice; first_seen_dead must not budge.
	for i := 0; i < 2; i++ {
		time.Sleep(10 * time.Millisecond)
		require.NoError(t, s.ReplaceTrackers(ctx, "h", []triagearr.TrackerInfo{
			{URL: "https://a/announce", Host: "a", Status: triagearr.TrackerNotWorking},
		}))
	}
	rows, err = s.ListTrackers(ctx, "h")
	require.NoError(t, err)
	require.NotNil(t, rows[0].FirstSeenDead)
	require.True(t, rows[0].FirstSeenDead.Equal(first),
		"sustained dead must preserve first_seen_dead, got %s vs %s", rows[0].FirstSeenDead, first)
	// last_checked, however, advances on every poll — proving why we can't
	// use it for the sustained-dead computation.
	require.True(t, rows[0].LastChecked.After(first),
		"last_checked must advance on every poll, got %s ≤ %s", rows[0].LastChecked, first)
}

func TestReplaceTrackers_FirstSeenDead_ClearsOnRecovery(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.ReplaceTrackers(ctx, "h", []triagearr.TrackerInfo{
		{URL: "https://a/announce", Host: "a", Status: triagearr.TrackerNotWorking},
	}))
	require.NoError(t, s.ReplaceTrackers(ctx, "h", []triagearr.TrackerInfo{
		{URL: "https://a/announce", Host: "a", Status: triagearr.TrackerWorking},
	}))
	rows, err := s.ListTrackers(ctx, "h")
	require.NoError(t, err)
	require.Nil(t, rows[0].FirstSeenDead, "dead→alive must clear first_seen_dead")
}

func TestReplaceTrackers_FirstSeenDead_ResetsAfterRecovery(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.ReplaceTrackers(ctx, "h", []triagearr.TrackerInfo{
		{URL: "https://a/announce", Host: "a", Status: triagearr.TrackerNotWorking},
	}))
	rows, err := s.ListTrackers(ctx, "h")
	require.NoError(t, err)
	require.NotNil(t, rows[0].FirstSeenDead)
	original := *rows[0].FirstSeenDead

	time.Sleep(10 * time.Millisecond)
	require.NoError(t, s.ReplaceTrackers(ctx, "h", []triagearr.TrackerInfo{
		{URL: "https://a/announce", Host: "a", Status: triagearr.TrackerWorking},
	}))
	time.Sleep(10 * time.Millisecond)
	require.NoError(t, s.ReplaceTrackers(ctx, "h", []triagearr.TrackerInfo{
		{URL: "https://a/announce", Host: "a", Status: triagearr.TrackerNotWorking},
	}))
	rows, err = s.ListTrackers(ctx, "h")
	require.NoError(t, err)
	require.NotNil(t, rows[0].FirstSeenDead)
	require.True(t, rows[0].FirstSeenDead.After(original),
		"dead→alive→dead must restart the clock, got %s ≤ %s", rows[0].FirstSeenDead, original)
}

// A dead tracker on a torrent whose swarm went quiet months ago must seed
// first_seen_dead from that last activity, not from "now" — otherwise a DB
// wipe restarts the grace clock and silently zeroes the tracker_dead bonus
// across the whole graveyard library (SCORING.md §Factor 7).
func TestReplaceTrackers_FirstSeenDead_SeededFromLastActivity(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	lastActive := time.Now().UTC().Add(-100 * 24 * time.Hour)
	require.NoError(t, s.UpsertTorrent(ctx, triagearr.Torrent{
		Hash: "h", Name: "Graveyard",
		AddedOn: lastActive.Add(-200 * 24 * time.Hour), LastActivity: lastActive,
	}))

	require.NoError(t, s.ReplaceTrackers(ctx, "h", []triagearr.TrackerInfo{
		{URL: "https://a/announce", Host: "a", Status: triagearr.TrackerNotWorking},
	}))
	rows, err := s.ListTrackers(ctx, "h")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.NotNil(t, rows[0].FirstSeenDead)
	require.WithinDuration(t, lastActive, *rows[0].FirstSeenDead, 5*time.Second,
		"first_seen_dead must anchor on last_activity, not now")
}

// With no snapshots yet (e.g. the first tracker poll right after a wipe), the
// proxy falls back to the torrent's completion_on so old torrents still qualify.
func TestReplaceTrackers_FirstSeenDead_SeededFromCompletionOn(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	completed := time.Now().UTC().Add(-300 * 24 * time.Hour)
	require.NoError(t, s.UpsertTorrent(ctx, triagearr.Torrent{
		Hash: "h", Name: "Old", AddedOn: completed.Add(-time.Hour), CompletionOn: completed,
	}))

	require.NoError(t, s.ReplaceTrackers(ctx, "h", []triagearr.TrackerInfo{
		{URL: "https://a/announce", Host: "a", Status: triagearr.TrackerNotWorking},
	}))
	rows, err := s.ListTrackers(ctx, "h")
	require.NoError(t, err)
	require.NotNil(t, rows[0].FirstSeenDead)
	require.WithinDuration(t, completed, *rows[0].FirstSeenDead, 5*time.Second)
}

// A torrent that was active moments ago must NOT back-date its death: the grace
// window still protects a tracker that just blipped offline on a live swarm.
func TestReplaceTrackers_FirstSeenDead_RecentActivityNotBackdated(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	recent := time.Now().UTC().Add(-2 * time.Hour)
	require.NoError(t, s.UpsertTorrent(ctx, triagearr.Torrent{
		Hash: "h", Name: "Alive",
		AddedOn: recent.Add(-24 * time.Hour), LastActivity: recent,
	}))

	before := time.Now().UTC()
	require.NoError(t, s.ReplaceTrackers(ctx, "h", []triagearr.TrackerInfo{
		{URL: "https://a/announce", Host: "a", Status: triagearr.TrackerNotWorking},
	}))
	rows, err := s.ListTrackers(ctx, "h")
	require.NoError(t, err)
	require.NotNil(t, rows[0].FirstSeenDead)
	// Seeded from the 2h-old last_activity — recent enough that a 7d grace
	// still holds, but provably not "now".
	require.WithinDuration(t, recent, *rows[0].FirstSeenDead, 5*time.Second)
	require.True(t, rows[0].FirstSeenDead.Before(before), "must not seed a future/now timestamp")
}

func TestReplaceTrackers_FirstSeenDead_PerURL(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	// Two trackers: a transitions alive→dead immediately, b stays alive.
	require.NoError(t, s.ReplaceTrackers(ctx, "h", []triagearr.TrackerInfo{
		{URL: "https://a/announce", Host: "a", Status: triagearr.TrackerNotWorking},
		{URL: "https://b/announce", Host: "b", Status: triagearr.TrackerWorking},
	}))
	rows, err := s.ListTrackers(ctx, "h")
	require.NoError(t, err)
	require.Len(t, rows, 2)
	by := map[string]*time.Time{}
	for _, r := range rows {
		by[r.Host] = r.FirstSeenDead
	}
	require.NotNil(t, by["a"])
	require.Nil(t, by["b"])
}
