package store_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Triagearr/Triagearr/internal/triagearr"
)

func seedRunForActions(ctx context.Context, t *testing.T, s storeIface) int64 {
	t.Helper()
	id, err := s.InsertRun(ctx, triagearr.Run{
		TriggeredBy: triagearr.RunTriggerDiskPressure,
		TriggeredAt: time.Now().UTC().Truncate(time.Second),
		Mode:        "live",
		StopReason:  triagearr.StopTargetReached,
		Status:      "running",
	})
	require.NoError(t, err)
	return id
}

// storeIface lets test helpers depend on the subset of methods they need
// without dragging the full *store.Store type into the helper signature.
type storeIface interface {
	InsertRun(ctx context.Context, r triagearr.Run) (int64, error)
}

func TestActionRoundTrip(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	runID := seedRunForActions(ctx, t, s)
	now := time.Now().UTC().Truncate(time.Second)

	id, err := s.InsertAction(ctx, triagearr.Action{
		RunID:       runID,
		Rank:        0,
		TorrentHash: "aaa",
		StartedAt:   now,
		Status:      triagearr.ActionRunning,
	})
	require.NoError(t, err)
	require.Positive(t, id)

	require.NoError(t, s.FinishAction(ctx, id, triagearr.ActionSucceeded, now.Add(2*time.Second), 12345))

	got, err := s.GetAction(ctx, id)
	require.NoError(t, err)
	require.Equal(t, triagearr.ActionSucceeded, got.Status)
	require.Equal(t, int64(12345), got.FreedBytes)
	require.Equal(t, triagearr.Hash("aaa"), got.TorrentHash)
	require.False(t, got.FinishedAt.IsZero())
}

func TestGetAction_NotFound(t *testing.T) {
	s := openTestStore(t)
	_, err := s.GetAction(context.Background(), 999)
	require.True(t, errors.Is(err, sql.ErrNoRows))
}

func TestAuditPerFile_8ok_1fail_1notattempted(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	runID := seedRunForActions(ctx, t, s)
	now := time.Now().UTC().Truncate(time.Second)

	actionID, err := s.InsertAction(ctx, triagearr.Action{
		RunID:       runID,
		Rank:        0,
		TorrentHash: "pack",
		StartedAt:   now,
		Status:      triagearr.ActionRunning,
	})
	require.NoError(t, err)

	// 8 OK files
	for i := 1; i <= 8; i++ {
		require.NoError(t, s.AppendAudit(ctx, triagearr.AuditEntry{
			ActionID:  actionID,
			Timestamp: now,
			Step:      triagearr.AuditStepArrDelete,
			ArrType:   "sonarr",
			ArrFileID: int64(i),
			Outcome:   triagearr.AuditOutcomeOK,
		}))
	}
	// 1 failed
	require.NoError(t, s.AppendAudit(ctx, triagearr.AuditEntry{
		ActionID:  actionID,
		Timestamp: now,
		Step:      triagearr.AuditStepArrDelete,
		ArrType:   "sonarr",
		ArrFileID: 9,
		Outcome:   triagearr.AuditOutcomeFailed,
		Detail:    "HTTP 500",
	}))
	// 1 not_attempted (would have been file_id=10)
	require.NoError(t, s.AppendAudit(ctx, triagearr.AuditEntry{
		ActionID:  actionID,
		Timestamp: now,
		Step:      triagearr.AuditStepArrDelete,
		ArrType:   "sonarr",
		ArrFileID: 10,
		Outcome:   triagearr.AuditOutcomeNotAttempted,
	}))

	rows, err := s.ListAuditByAction(ctx, actionID)
	require.NoError(t, err)
	require.Len(t, rows, 10)

	var ok, failed, notAttempted int
	for _, r := range rows {
		require.Equal(t, triagearr.AuditStepArrDelete, r.Step)
		require.Equal(t, "sonarr", r.ArrType)
		switch r.Outcome {
		case triagearr.AuditOutcomeOK:
			ok++
		case triagearr.AuditOutcomeFailed:
			failed++
			require.Equal(t, "HTTP 500", r.Detail)
		case triagearr.AuditOutcomeNotAttempted:
			notAttempted++
		}
	}
	require.Equal(t, 8, ok)
	require.Equal(t, 1, failed)
	require.Equal(t, 1, notAttempted)
}

func TestActionsCascadeOnRunDelete(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	runID := seedRunForActions(ctx, t, s)

	actionID, err := s.InsertAction(ctx, triagearr.Action{
		RunID:       runID,
		Rank:        0,
		TorrentHash: "xxx",
		StartedAt:   time.Now().UTC(),
		Status:      triagearr.ActionRunning,
	})
	require.NoError(t, err)
	require.NoError(t, s.AppendAudit(ctx, triagearr.AuditEntry{
		ActionID:  actionID,
		Timestamp: time.Now().UTC(),
		Step:      triagearr.AuditStepQbitDelete,
		Outcome:   triagearr.AuditOutcomeOK,
	}))

	_, err = s.DB().ExecContext(ctx, `PRAGMA foreign_keys=ON`)
	require.NoError(t, err)
	_, err = s.DB().ExecContext(ctx, `DELETE FROM runs WHERE id = ?`, runID)
	require.NoError(t, err)

	var nActions, nAudit int
	require.NoError(t, s.DB().Get(&nActions, `SELECT COUNT(*) FROM actions WHERE run_id = ?`, runID))
	require.NoError(t, s.DB().Get(&nAudit, `SELECT COUNT(*) FROM audit_log WHERE action_id = ?`, actionID))
	require.Equal(t, 0, nActions)
	require.Equal(t, 0, nAudit)
}

func TestListActionsByRun_OrderByRank(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	runID := seedRunForActions(ctx, t, s)
	now := time.Now().UTC()

	for _, rank := range []int{2, 0, 1} {
		_, err := s.InsertAction(ctx, triagearr.Action{
			RunID:       runID,
			Rank:        rank,
			TorrentHash: triagearr.Hash("h"),
			StartedAt:   now,
			Status:      triagearr.ActionRunning,
		})
		require.NoError(t, err)
	}

	rows, err := s.ListActionsByRun(ctx, runID)
	require.NoError(t, err)
	require.Len(t, rows, 3)
	require.Equal(t, 0, rows[0].Rank)
	require.Equal(t, 1, rows[1].Rank)
	require.Equal(t, 2, rows[2].Rank)
}
