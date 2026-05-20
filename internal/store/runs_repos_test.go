package store_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Triagearr/Triagearr/internal/store"
	"github.com/Triagearr/Triagearr/internal/triagearr"
)

func TestInsertAndGetRun(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	r := triagearr.Run{
		TriggeredBy:         triagearr.RunTriggerDiskPressure,
		TriggeredAt:         now,
		Mode:                "dry-run",
		VolumeName:          "data",
		FreePctAtFire:       8.5,
		TargetFreePct:       20.0,
		EstimatedFreedBytes: 5 * 1024 * 1024 * 1024,
		StopReason:          triagearr.StopTargetReached,
		Status:              "completed",
	}
	id, err := s.InsertRun(ctx, r)
	require.NoError(t, err)
	require.Positive(t, id)

	items := []triagearr.RunItem{
		{Rank: 0, TorrentHash: "aaa", Score: 99.5, SizeBytes: 3e9, WouldFreeBytes: 3e9},
		{Rank: 1, TorrentHash: "bbb", Score: 90.0, SizeBytes: 2e9, WouldFreeBytes: 2e9},
	}
	require.NoError(t, s.InsertRunItems(ctx, id, items))

	got, gotItems, err := s.GetRun(ctx, id)
	require.NoError(t, err)
	require.Equal(t, r.TriggeredBy, got.TriggeredBy)
	require.Equal(t, "data", got.VolumeName)
	require.InDelta(t, 20.0, got.TargetFreePct, 1e-9)
	require.Equal(t, triagearr.StopTargetReached, got.StopReason)
	require.Len(t, gotItems, 2)
	require.Equal(t, triagearr.Hash("aaa"), gotItems[0].TorrentHash)
	require.Equal(t, 1, gotItems[1].Rank)
}

func TestGetRun_NotFound(t *testing.T) {
	s := openTestStore(t)
	_, _, err := s.GetRun(context.Background(), 999)
	require.True(t, errors.Is(err, sql.ErrNoRows))
}

func TestListRuns_Ordering(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	base := time.Now().UTC().Truncate(time.Second)

	for i := 0; i < 3; i++ {
		_, err := s.InsertRun(ctx, triagearr.Run{
			TriggeredBy: triagearr.RunTriggerCLI,
			TriggeredAt: base.Add(time.Duration(i) * time.Minute),
			Mode:        "dry-run",
			StopReason:  triagearr.StopNoMoreCandidates,
			Status:      "completed",
		})
		require.NoError(t, err)
	}

	rows, err := s.ListRuns(ctx, store.ListRunsOpts{})
	require.NoError(t, err)
	require.Len(t, rows, 3)
	// most-recent first
	require.True(t, rows[0].TriggeredAt.After(rows[1].TriggeredAt) || rows[0].TriggeredAt.Equal(rows[1].TriggeredAt))
	require.True(t, rows[1].TriggeredAt.After(rows[2].TriggeredAt) || rows[1].TriggeredAt.Equal(rows[2].TriggeredAt))

	limited, err := s.ListRuns(ctx, store.ListRunsOpts{Limit: 2})
	require.NoError(t, err)
	require.Len(t, limited, 2)
}

func TestRunItems_CascadeOnRunDelete(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	id, err := s.InsertRun(ctx, triagearr.Run{
		TriggeredBy: triagearr.RunTriggerHTTP,
		TriggeredAt: time.Now().UTC(),
		Mode:        "dry-run",
		StopReason:  triagearr.StopSizeCap,
		Status:      "completed",
	})
	require.NoError(t, err)
	require.NoError(t, s.InsertRunItems(ctx, id, []triagearr.RunItem{
		{Rank: 0, TorrentHash: "xxx", Score: 10, SizeBytes: 1, WouldFreeBytes: 1},
	}))

	_, err = s.DB().Exec(`PRAGMA foreign_keys=ON`)
	require.NoError(t, err)
	_, err = s.DB().Exec(`DELETE FROM runs WHERE id = ?`, id)
	require.NoError(t, err)

	var n int
	require.NoError(t, s.DB().Get(&n, `SELECT COUNT(*) FROM run_items WHERE run_id = ?`, id))
	require.Equal(t, 0, n)
}
