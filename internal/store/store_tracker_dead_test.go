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

func TestMigration0007_BackfillsFirstSeenDead(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	// Simulate a pre-0007 state by inserting a dead row with first_seen_dead
	// explicitly NULL — then re-run the backfill statement.
	require.NoError(t, s.ReplaceTrackers(ctx, "h", []triagearr.TrackerInfo{
		{URL: "https://a/announce", Host: "a", Status: triagearr.TrackerNotWorking},
	}))
	_, err := s.DB().ExecContext(ctx,
		`UPDATE torrent_trackers SET first_seen_dead = NULL WHERE torrent_hash = 'h'`)
	require.NoError(t, err)

	// Backfill SQL (identical to migration 0007's UPDATE).
	_, err = s.DB().ExecContext(ctx,
		`UPDATE torrent_trackers SET first_seen_dead = last_checked WHERE status = 4 AND first_seen_dead IS NULL`)
	require.NoError(t, err)

	rows, err := s.ListTrackers(ctx, "h")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.NotNil(t, rows[0].FirstSeenDead, "backfill must populate first_seen_dead for dead rows")
	require.True(t, rows[0].FirstSeenDead.Equal(rows[0].LastChecked),
		"backfill must copy last_checked, got fsd=%s lc=%s", rows[0].FirstSeenDead, rows[0].LastChecked)
}
