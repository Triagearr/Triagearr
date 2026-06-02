package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNotificationState_RoundTrip(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	const key = "target_unreachable:data"

	// Absent → never sent.
	_, ok, err := s.GetNotificationState(ctx, key)
	require.NoError(t, err)
	require.False(t, ok, "no row should exist before the first send")

	at := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, s.MarkNotificationSent(ctx, key, at))

	got, ok, err := s.GetNotificationState(ctx, key)
	require.NoError(t, err)
	require.True(t, ok)
	require.WithinDuration(t, at, got, time.Second)

	// Upsert advances the timestamp rather than inserting a second row.
	later := at.Add(2 * time.Hour)
	require.NoError(t, s.MarkNotificationSent(ctx, key, later))
	got, _, err = s.GetNotificationState(ctx, key)
	require.NoError(t, err)
	require.WithinDuration(t, later, got, time.Second)

	// Clear reverts to the never-sent state; clearing again is a no-op.
	require.NoError(t, s.ClearNotificationState(ctx, key))
	_, ok, err = s.GetNotificationState(ctx, key)
	require.NoError(t, err)
	require.False(t, ok)
	require.NoError(t, s.ClearNotificationState(ctx, key))
}
