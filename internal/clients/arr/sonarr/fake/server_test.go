package fake_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Triagearr/Triagearr/internal/clients/arr/sonarr"
	"github.com/Triagearr/Triagearr/internal/clients/arr/sonarr/fake"
	"github.com/Triagearr/Triagearr/internal/triagearr"
)

func seed(srv *fake.Server) {
	srv.State().AddTag(fake.Tag{ID: 1, Label: "french"})
	srv.State().AddTag(fake.Tag{ID: 2, Label: "hd"})
	srv.State().AddSeries(fake.Series{
		ID:         1,
		Title:      "Cosmic Drift",
		Path:       "/data/tv/Cosmic Drift",
		Tags:       []int{1, 2},
		Statistics: fake.SeriesStats{SizeOnDisk: 50_000_000_000},
		Files: []fake.EpisodeFile{
			{ID: 101, SeriesID: 1, Path: "/data/tv/Cosmic Drift/S01E01.mkv", Size: 2_000_000_000},
			{ID: 102, SeriesID: 1, Path: "/data/tv/Cosmic Drift/S01E02.mkv", Size: 2_000_000_000},
		},
	})
	srv.State().AddSeries(fake.Series{
		ID:         2,
		Title:      "Untagged Show",
		Path:       "/data/tv/Untagged",
		Tags:       []int{},
		Statistics: fake.SeriesStats{SizeOnDisk: 0},
	})
	now := time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC)
	srv.State().AddHistory(fake.HistoryRecord{
		ID:         10,
		Date:       now.Add(-2 * time.Hour),
		EventType:  "downloadFolderImported",
		DownloadID: "ABCDEF1234567890",
		Data: fake.HistoryRecordData{
			FileID:         "101",
			DownloadClient: "qBittorrent",
			DroppedPath:    "/downloads/Cosmic.Drift.S01E01.mkv",
			ImportedPath:   "/data/tv/Cosmic Drift/S01E01.mkv",
			Size:           "2000000000",
		},
	})
	srv.State().AddHistory(fake.HistoryRecord{
		ID:         11,
		Date:       now.Add(-1 * time.Hour),
		EventType:  "downloadFolderImported",
		DownloadID: "FEDCBA0987654321",
		Data: fake.HistoryRecordData{
			FileID:         "102",
			DownloadClient: "qBittorrent",
			DroppedPath:    "/downloads/Cosmic.Drift.S01E02.mkv",
			ImportedPath:   "/data/tv/Cosmic Drift/S01E02.mkv",
			Size:           "2000000000",
		},
	})
	// Grabbed event must be filtered out by the eventType=3 query.
	srv.State().AddHistory(fake.HistoryRecord{
		ID:        12,
		Date:      now,
		EventType: "grabbed",
	})
}

func newClient(t *testing.T, url string) *sonarr.Client {
	t.Helper()
	c, err := sonarr.New(sonarr.Options{
		Name:    "fake",
		BaseURL: url,
		APIKey:  "test-key",
		Poll:    true,
	})
	require.NoError(t, err)
	return c
}

func TestFake_HealthAndListMedia(t *testing.T) {
	srv := fake.New(fake.Options{APIKey: "test-key"})
	seed(srv)
	httpSrv := httptest.NewServer(srv.Handler())
	t.Cleanup(httpSrv.Close)

	c := newClient(t, httpSrv.URL)
	ctx := context.Background()

	require.NoError(t, c.HealthCheck(ctx))

	items, err := c.ListMedia(ctx)
	require.NoError(t, err)
	require.Len(t, items, 2)
	require.Equal(t, "Cosmic Drift", items[0].Title)
	require.ElementsMatch(t, []string{"french", "hd"}, items[0].Tags)
	require.Equal(t, int64(50_000_000_000), items[0].Size)
	require.Empty(t, items[1].Tags)
}

func TestFake_ListMediaFiles(t *testing.T) {
	srv := fake.New(fake.Options{APIKey: "test-key"})
	seed(srv)
	httpSrv := httptest.NewServer(srv.Handler())
	t.Cleanup(httpSrv.Close)

	c := newClient(t, httpSrv.URL)
	files, err := c.ListMediaFiles(context.Background(), 1)
	require.NoError(t, err)
	require.Len(t, files, 2)
	require.Equal(t, int64(101), files[0].FileID)
}

func TestFake_DeleteEpisodeFile(t *testing.T) {
	srv := fake.New(fake.Options{APIKey: "test-key"})
	seed(srv)
	httpSrv := httptest.NewServer(srv.Handler())
	t.Cleanup(httpSrv.Close)

	c := newClient(t, httpSrv.URL)
	ctx := context.Background()

	require.NoError(t, c.DeleteMediaFile(ctx, 101, triagearr.DeleteOpts{DeleteFiles: true, AddImportExclusion: true}))
	require.Equal(t, int64(1), srv.State().EpisodeFileDeletes())

	files, err := c.ListMediaFiles(ctx, 1)
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.Equal(t, int64(102), files[0].FileID)
}

func TestFake_DeleteEpisodeFile_NotFound(t *testing.T) {
	srv := fake.New(fake.Options{APIKey: "test-key"})
	httpSrv := httptest.NewServer(srv.Handler())
	t.Cleanup(httpSrv.Close)

	c := newClient(t, httpSrv.URL)
	err := c.DeleteMediaFile(context.Background(), 999, triagearr.DeleteOpts{})
	require.Error(t, err)
}

func TestFake_ListImportsFiltersToDownloadFolderImported(t *testing.T) {
	srv := fake.New(fake.Options{APIKey: "test-key"})
	seed(srv)
	httpSrv := httptest.NewServer(srv.Handler())
	t.Cleanup(httpSrv.Close)

	c := newClient(t, httpSrv.URL)
	imports, err := c.ListImports(context.Background(), 0)
	require.NoError(t, err)
	// Only the two downloadFolderImported records — the grabbed entry must
	// not surface (server-side filter via eventType=3 in URL).
	require.Len(t, imports, 2)
	// arrhistory.Fetch reverses to ascending; oldest first.
	require.Equal(t, int64(10), imports[0].HistoryID)
	require.Equal(t, int64(11), imports[1].HistoryID)
}

func TestFake_ListImportsSinceCursor(t *testing.T) {
	srv := fake.New(fake.Options{APIKey: "test-key"})
	seed(srv)
	httpSrv := httptest.NewServer(srv.Handler())
	t.Cleanup(httpSrv.Close)

	c := newClient(t, httpSrv.URL)
	imports, err := c.ListImports(context.Background(), 10)
	require.NoError(t, err)
	require.Len(t, imports, 1)
	require.Equal(t, int64(11), imports[0].HistoryID)
}

func TestFake_BadAPIKey(t *testing.T) {
	srv := fake.New(fake.Options{APIKey: "test-key"})
	httpSrv := httptest.NewServer(srv.Handler())
	t.Cleanup(httpSrv.Close)

	c, err := sonarr.New(sonarr.Options{
		Name:    "fake",
		BaseURL: httpSrv.URL,
		APIKey:  "wrong",
		Poll:    true,
	})
	require.NoError(t, err)
	err = c.HealthCheck(context.Background())
	require.Error(t, err)
}

func TestFake_UnknownEndpoint501(t *testing.T) {
	srv := fake.New(fake.Options{APIKey: "test-key"})
	httpSrv := httptest.NewServer(srv.Handler())
	t.Cleanup(httpSrv.Close)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, httpSrv.URL+"/api/v3/system/status", nil)
	require.NoError(t, err)
	req.Header.Set("X-Api-Key", "test-key")
	resp, err := httpSrv.Client().Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	require.Equal(t, 501, resp.StatusCode)
}
