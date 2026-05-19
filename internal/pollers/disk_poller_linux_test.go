//go:build linux

package pollers_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Triagearr/Triagearr/internal/pollers"
	"github.com/Triagearr/Triagearr/internal/store"
)

func TestDiskPoller_PersistsRealStatfs(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	require.NoError(t, s.Migrate())

	dir := t.TempDir()
	p := &pollers.DiskPoller{
		Volumes:  []pollers.Volume{{Name: "test", Path: dir}},
		Store:    s,
		Interval: time.Hour,
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- p.Run(ctx) }()

	require.Eventually(t, func() bool {
		rows, err := s.LatestDiskUsage(context.Background())
		return err == nil && len(rows) == 1
	}, 2*time.Second, 10*time.Millisecond)

	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("poller did not stop within 2s of cancellation")
	}

	rows, err := s.LatestDiskUsage(context.Background())
	require.NoError(t, err)
	require.Len(t, rows, 1)
	r := rows[0]
	require.Equal(t, "test", r.VolumeName)
	require.Equal(t, dir, r.Path)
	require.Greater(t, r.TotalBytes, uint64(0))
	require.Equal(t, r.TotalBytes, r.UsedBytes+r.FreeBytes)
	require.GreaterOrEqual(t, r.FreePercent, 0.0)
	require.LessOrEqual(t, r.FreePercent, 100.0)
}

func TestDiskPoller_BadPathIsLoggedNotFatal(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	require.NoError(t, s.Migrate())

	p := &pollers.DiskPoller{
		Volumes: []pollers.Volume{
			{Name: "bogus", Path: "/this/path/does/not/exist/at/all"},
			{Name: "ok", Path: t.TempDir()},
		},
		Store:    s,
		Interval: time.Hour,
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- p.Run(ctx) }()

	require.Eventually(t, func() bool {
		rows, err := s.LatestDiskUsage(context.Background())
		return err == nil && len(rows) == 1
	}, 2*time.Second, 10*time.Millisecond)

	cancel()
	<-done

	rows, _ := s.LatestDiskUsage(context.Background())
	require.Len(t, rows, 1)
	require.Equal(t, "ok", rows[0].VolumeName)
}
