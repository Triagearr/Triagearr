package store_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSettingsOverrides_UpsertGetList(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.UpsertSettingsOverride(ctx, "scoring.hnr_window_days", `21`))
	require.NoError(t, s.UpsertSettingsOverride(ctx, "polling.qbit_interval", `"5m"`))

	row, err := s.GetSettingsOverride(ctx, "scoring.hnr_window_days")
	require.NoError(t, err)
	require.Equal(t, "21", row.ValueJSON)
	require.False(t, row.UpdatedAt.IsZero())

	all, err := s.ListSettingsOverrides(ctx)
	require.NoError(t, err)
	require.Len(t, all, 2)
	require.Equal(t, "polling.qbit_interval", all[0].Key)
	require.Equal(t, "scoring.hnr_window_days", all[1].Key)
}

func TestSettingsOverrides_UpsertReplaces(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.UpsertSettingsOverride(ctx, "scoring.hnr_window_days", `21`))
	require.NoError(t, s.UpsertSettingsOverride(ctx, "scoring.hnr_window_days", `42`))

	row, err := s.GetSettingsOverride(ctx, "scoring.hnr_window_days")
	require.NoError(t, err)
	require.Equal(t, "42", row.ValueJSON)

	all, err := s.ListSettingsOverrides(ctx)
	require.NoError(t, err)
	require.Len(t, all, 1)
}

func TestSettingsOverrides_DeleteIdempotent(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.UpsertSettingsOverride(ctx, "scoring.hnr_window_days", `21`))
	require.NoError(t, s.DeleteSettingsOverride(ctx, "scoring.hnr_window_days"))
	require.NoError(t, s.DeleteSettingsOverride(ctx, "scoring.hnr_window_days"))

	_, err := s.GetSettingsOverride(ctx, "scoring.hnr_window_days")
	require.True(t, errors.Is(err, sql.ErrNoRows))
}
