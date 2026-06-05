package triggers

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Triagearr/Triagearr/internal/notify"
	"github.com/Triagearr/Triagearr/internal/triagearr"
)

func newHealthWatcher(s HealthStore, fn *fakeNotifier, clock *time.Time) *HealthWatcher {
	return &HealthWatcher{
		Store:    s,
		Notifier: notify.NewDispatcher(fn),
		Interval: time.Minute,
		now:      func() time.Time { return *clock },
	}
}

func TestHealthWatcher_DegradedThenRecovered(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	clock := time.Now().UTC()
	fn := &fakeNotifier{}
	w := newHealthWatcher(s, fn, &clock)

	// First poll records the instance as unhealthy.
	require.NoError(t, s.UpsertArrInstance(ctx, triagearr.ArrType("sonarr"), "http://sonarr:8989", false, "dial tcp: connection refused"))

	require.NoError(t, w.tick(ctx))
	require.Len(t, fn.got, 1)
	require.Equal(t, notify.EventHealthDegraded, fn.got[0].Kind)
	require.NotNil(t, fn.got[0].Health)
	require.Equal(t, "sonarr", fn.got[0].Health.Component)
	require.Contains(t, fn.got[0].Text, "connection refused")

	// Still unhealthy on the next tick → no re-fire (throttled by state row).
	require.NoError(t, w.tick(ctx))
	require.Len(t, fn.got, 1)

	// Recovery fires once, then goes quiet.
	require.NoError(t, s.UpsertArrInstance(ctx, triagearr.ArrType("sonarr"), "http://sonarr:8989", true, ""))
	require.NoError(t, w.tick(ctx))
	require.Len(t, fn.got, 2)
	require.Equal(t, notify.EventHealthRecovered, fn.got[1].Kind)

	require.NoError(t, w.tick(ctx))
	require.Len(t, fn.got, 2)
}

func TestHealthWatcher_HealthyFromStartIsSilent(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	clock := time.Now().UTC()
	fn := &fakeNotifier{}
	w := newHealthWatcher(s, fn, &clock)

	require.NoError(t, s.UpsertTorrentClientInstance(ctx, "qbittorrent", "http://qbit:8080", true, ""))
	require.NoError(t, w.tick(ctx))
	require.Empty(t, fn.got)
}

func TestHealthWatcher_TorrentClientDegraded(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	clock := time.Now().UTC()
	fn := &fakeNotifier{}
	w := newHealthWatcher(s, fn, &clock)

	require.NoError(t, s.UpsertTorrentClientInstance(ctx, "qbittorrent", "http://qbit:8080", false, "401 unauthorized"))
	require.NoError(t, w.tick(ctx))
	require.Len(t, fn.got, 1)
	require.Equal(t, notify.EventHealthDegraded, fn.got[0].Kind)
	require.Equal(t, "torrent_client", fn.got[0].Health.Kind)
}
