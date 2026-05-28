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

// fakeRichArr adds the optional FileLister + ImportLister capabilities so the
// file-fanout and import-sync branches of pollOne/syncImports are exercised.
type fakeRichArr struct {
	fakeArr
	filesByMedia map[triagearr.MediaID][]triagearr.MediaFile
	imports      []triagearr.ImportRecord
	gotSince     int64
}

func (f *fakeRichArr) ListMediaFiles(_ context.Context, id triagearr.MediaID) ([]triagearr.MediaFile, error) {
	return f.filesByMedia[id], nil
}

func (f *fakeRichArr) ListImports(_ context.Context, since int64) ([]triagearr.ImportRecord, error) {
	f.gotSince = since
	return f.imports, nil
}

func openStoreForArrTest(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	require.NoError(t, s.Migrate(context.Background()))
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

func TestArrPoller_SyncsFilesAndImports(t *testing.T) {
	s := openStoreForArrTest(t)

	// Pre-seed a higher history_id so we can prove syncImports asks for the delta.
	require.NoError(t, s.UpsertArrImport(context.Background(), triagearr.ArrTypeSonarr,
		triagearr.ImportRecord{HistoryID: 10, FileID: 1, DownloadID: "h0", ImportedAt: time.Now()}))

	fa := &fakeRichArr{
		fakeArr: fakeArr{
			name: "sonarr", typ: triagearr.ArrTypeSonarr, healthy: true,
			items: []triagearr.MediaItem{{ID: 7, ArrType: triagearr.ArrTypeSonarr, Title: "S"}},
		},
		filesByMedia: map[triagearr.MediaID][]triagearr.MediaFile{
			7: {{ArrType: triagearr.ArrTypeSonarr, FileID: 100, MediaID: 7, Path: "/m/s.mkv", Size: 5}},
		},
		imports: []triagearr.ImportRecord{
			{HistoryID: 11, FileID: 100, DownloadID: "habc", ImportedPath: "/m/s.mkv", ImportedAt: time.Now()},
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
		n, err := s.CountArrImports(context.Background(), triagearr.ArrTypeSonarr)
		return err == nil && n == 2 // the seeded one + the new delta record
	}, 2*time.Second, 10*time.Millisecond)

	cancel()
	<-done

	require.Equal(t, int64(10), fa.gotSince, "ListImports is asked only for the delta past the stored max history_id")

	mf, err := s.ListMediaFilesByMedia(context.Background(), triagearr.ArrTypeSonarr, 7)
	require.NoError(t, err)
	require.Len(t, mf, 1)
	require.Equal(t, int64(100), mf[0].FileID)
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
