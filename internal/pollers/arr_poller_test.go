package pollers_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Triagearr/Triagearr/internal/pollers"
	"github.com/Triagearr/Triagearr/internal/store"
	"github.com/Triagearr/Triagearr/internal/triagearr"
)

type fakeArr struct {
	name    string
	typ     triagearr.ArrType
	healthy bool
	items   []triagearr.MediaItem
}

func (f *fakeArr) Name() string            { return f.name }
func (f *fakeArr) Type() triagearr.ArrType { return f.typ }
func (f *fakeArr) Poll() bool              { return true }
func (f *fakeArr) Act() bool               { return false }
func (f *fakeArr) HealthCheck(_ context.Context) error {
	if !f.healthy {
		return errors.New("unhealthy")
	}
	return nil
}
func (f *fakeArr) ListMedia(_ context.Context) ([]triagearr.MediaItem, error) {
	return f.items, nil
}
func (f *fakeArr) DeleteMedia(_ context.Context, _ triagearr.MediaID, _ triagearr.DeleteOpts) error {
	return errors.New("not used in test")
}

func openStoreForArrTest(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	require.NoError(t, s.Migrate())
	return s
}

func TestArrPoller_HealthyInstancePersistsMedia(t *testing.T) {
	s := openStoreForArrTest(t)
	fa := &fakeArr{
		name: "sonarr", typ: triagearr.ArrTypeSonarr, healthy: true,
		items: []triagearr.MediaItem{
			{ID: 1, ArrType: triagearr.ArrTypeSonarr, Title: "S1"},
			{ID: 2, ArrType: triagearr.ArrTypeSonarr, Title: "S2"},
		},
	}
	p := &pollers.ArrPoller{
		Instances: []triagearr.ArrInstance{fa},
		URLs:      map[string]string{pollers.URLKey(triagearr.ArrTypeSonarr): "http://sonarr"},
		Store:     s,
		Interval:  time.Hour,
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- p.Run(ctx) }()

	require.Eventually(t, func() bool {
		n, err := s.CountMedia(context.Background(), triagearr.ArrTypeSonarr)
		return err == nil && n == 2
	}, 2*time.Second, 10*time.Millisecond)

	rows, err := s.ListArrInstances(context.Background())
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.True(t, rows[0].Healthy)
	require.Equal(t, "http://sonarr", rows[0].URL)

	cancel()
	<-done
}

func TestArrPoller_UnhealthyInstanceSkipsListMedia(t *testing.T) {
	s := openStoreForArrTest(t)
	fa := &fakeArr{
		name: "radarr", typ: triagearr.ArrTypeRadarr, healthy: false,
		items: []triagearr.MediaItem{{ID: 99, ArrType: triagearr.ArrTypeRadarr, Title: "Should not land"}},
	}
	p := &pollers.ArrPoller{
		Instances: []triagearr.ArrInstance{fa},
		URLs:      map[string]string{},
		Store:     s,
		Interval:  time.Hour,
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- p.Run(ctx) }()

	require.Eventually(t, func() bool {
		rows, _ := s.ListArrInstances(context.Background())
		return len(rows) == 1 && !rows[0].Healthy
	}, 2*time.Second, 10*time.Millisecond)

	n, err := s.CountMedia(context.Background(), triagearr.ArrTypeRadarr)
	require.NoError(t, err)
	require.Equal(t, 0, n, "unhealthy instance must not have media inserted")

	cancel()
	<-done
}
