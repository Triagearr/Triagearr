package fake_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Triagearr/Triagearr/internal/clients/radarr"
	"github.com/Triagearr/Triagearr/internal/clients/radarr/fake"
	"github.com/Triagearr/Triagearr/internal/triagearr"
)

func seed(srv *fake.Server) {
	srv.State().AddTag(fake.Tag{ID: 1, Label: "french"})
	srv.State().AddMovie(fake.Movie{
		ID:         1,
		Title:      "Cosmic Voyage (2024)",
		Path:       "/data/movies/Cosmic Voyage (2024)",
		SizeOnDisk: 8_000_000_000,
		Tags:       []int{1},
		Files: []fake.MovieFile{
			{ID: 201, MovieID: 1, Path: "/data/movies/Cosmic Voyage (2024)/Cosmic.Voyage.mkv", Size: 8_000_000_000},
		},
	})
	srv.State().AddMovie(fake.Movie{
		ID:         2,
		Title:      "Untagged Movie",
		Path:       "/data/movies/Untagged",
		SizeOnDisk: 0,
		Tags:       []int{},
	})
	now := time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC)
	srv.State().AddHistory(fake.HistoryRecord{
		ID:         10,
		Date:       now.Add(-2 * time.Hour),
		EventType:  "downloadFolderImported",
		DownloadID: "ABCDEF1234567890",
		Data: fake.HistoryRecordData{
			FileID:         "201",
			DownloadClient: "qBittorrent",
			ImportedPath:   "/data/movies/Cosmic Voyage (2024)/Cosmic.Voyage.mkv",
			Size:           "8000000000",
		},
	})
	srv.State().AddHistory(fake.HistoryRecord{
		ID:        11,
		Date:      now,
		EventType: "grabbed",
	})
}

func newClient(t *testing.T, url string) *radarr.Client {
	t.Helper()
	c, err := radarr.New(radarr.Options{
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
	require.Equal(t, "Cosmic Voyage (2024)", items[0].Title)
	require.Equal(t, []string{"french"}, items[0].Tags)
	require.Equal(t, int64(8_000_000_000), items[0].Size)
}

func TestFake_DeleteMovieFile(t *testing.T) {
	srv := fake.New(fake.Options{APIKey: "test-key"})
	seed(srv)
	httpSrv := httptest.NewServer(srv.Handler())
	t.Cleanup(httpSrv.Close)

	c := newClient(t, httpSrv.URL)
	ctx := context.Background()

	files, err := c.ListMediaFiles(ctx, 1)
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.Equal(t, int64(201), files[0].FileID)

	require.NoError(t, c.DeleteMediaFile(ctx, 201, triagearr.DeleteOpts{DeleteFiles: true}))
	require.Equal(t, int64(1), srv.State().MovieFileDeletes())

	files, err = c.ListMediaFiles(ctx, 1)
	require.NoError(t, err)
	require.Empty(t, files)
}

func TestFake_ListImportsFiltersGrabbed(t *testing.T) {
	srv := fake.New(fake.Options{APIKey: "test-key"})
	seed(srv)
	httpSrv := httptest.NewServer(srv.Handler())
	t.Cleanup(httpSrv.Close)

	c := newClient(t, httpSrv.URL)
	imports, err := c.ListImports(context.Background(), 0)
	require.NoError(t, err)
	require.Len(t, imports, 1)
	require.Equal(t, int64(10), imports[0].HistoryID)
}

func TestFake_BadAPIKey(t *testing.T) {
	srv := fake.New(fake.Options{APIKey: "test-key"})
	httpSrv := httptest.NewServer(srv.Handler())
	t.Cleanup(httpSrv.Close)

	c, err := radarr.New(radarr.Options{
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
