package whisparr_v2_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	whisparr_v2 "github.com/Triagearr/Triagearr/internal/clients/arr/whisparr_v2"
	"github.com/Triagearr/Triagearr/internal/clients/arr/whisparr_v2/fake"
	"github.com/Triagearr/Triagearr/internal/triagearr"
)

// newFakeClient wires a real client against the in-memory fake Whisparr v2.
func newFakeClient(t *testing.T) (*whisparr_v2.Client, *fake.Server) {
	t.Helper()
	srv := fake.New(fake.Options{APIKey: "k"})
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	c, err := whisparr_v2.New(whisparr_v2.Options{
		Name: "whisparr", BaseURL: ts.URL, APIKey: "k", Poll: true, Act: true,
	})
	require.NoError(t, err)
	return c, srv
}

func newClient(t *testing.T, handler http.Handler) *whisparr_v2.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c, err := whisparr_v2.New(whisparr_v2.Options{
		Name: "whisparr", BaseURL: srv.URL, APIKey: "k", Poll: true,
	})
	require.NoError(t, err)
	return c
}

func TestType(t *testing.T) {
	c, _ := newFakeClient(t)
	require.Equal(t, triagearr.ArrTypeWhisparrV2, c.Type())
}

func TestHealthCheck_OK(t *testing.T) {
	c, _ := newFakeClient(t)
	require.NoError(t, c.HealthCheck(context.Background()))
}

func TestListMedia(t *testing.T) {
	c, srv := newFakeClient(t)
	srv.State().AddTag(fake.Tag{ID: 1, Label: "keep"})
	srv.State().AddTag(fake.Tag{ID: 2, Label: "4k"})
	srv.State().AddSeries(fake.Series{
		ID: 1, Title: "S1", TitleSlug: "s1", Path: "/tv/S1", Tags: []int{1, 2},
		Statistics: fake.SeriesStats{SizeOnDisk: 1024},
	})

	items, err := c.ListMedia(context.Background())
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, triagearr.MediaID(1), items[0].ID)
	require.Equal(t, "S1", items[0].Title)
	require.Equal(t, int64(1024), items[0].Size)
	require.Equal(t, []string{"keep", "4k"}, items[0].Tags)
	require.Equal(t, triagearr.ArrTypeWhisparrV2, items[0].ArrType)
}

func TestListMediaFiles(t *testing.T) {
	c, srv := newFakeClient(t)
	srv.State().AddSeries(fake.Series{
		ID: 1, Title: "S1", Path: "/tv/S1",
		Files: []fake.EpisodeFile{
			{ID: 10, SeriesID: 1, Path: "/tv/S1/e01.mkv", Size: 500},
			{ID: 11, SeriesID: 1, Path: "/tv/S1/e02.mkv", Size: 700},
		},
	})

	files, err := c.ListMediaFiles(context.Background(), triagearr.MediaID(1))
	require.NoError(t, err)
	require.Len(t, files, 2)
	require.Equal(t, int64(10), files[0].FileID)
	require.Equal(t, triagearr.MediaID(1), files[0].MediaID)
	require.Equal(t, "/tv/S1/e02.mkv", files[1].Path)
	require.Equal(t, triagearr.ArrTypeWhisparrV2, files[0].ArrType)
}

func TestListImports(t *testing.T) {
	c, srv := newFakeClient(t)
	srv.State().AddHistory(fake.HistoryRecord{
		ID: 5, Date: time.Now().UTC(), EventType: "downloadFolderImported", DownloadID: "ABC123",
		Data: fake.HistoryRecordData{FileID: "10", DownloadClient: "qBittorrent", ImportedPath: "/tv/S1/e01.mkv"},
	})

	recs, err := c.ListImports(context.Background(), 0)
	require.NoError(t, err)
	require.Len(t, recs, 1)
	require.Equal(t, int64(5), recs[0].HistoryID)
	require.Equal(t, int64(10), recs[0].FileID)
	require.Equal(t, triagearr.Hash("abc123"), recs[0].DownloadID)
}

func TestDeleteMediaFile_FakeRemoves(t *testing.T) {
	c, srv := newFakeClient(t)
	srv.State().AddSeries(fake.Series{
		ID: 1, Title: "S1", Path: "/tv/S1",
		Files: []fake.EpisodeFile{{ID: 42, SeriesID: 1, Path: "/tv/S1/e01.mkv", Size: 500}},
	})

	err := c.DeleteMediaFile(context.Background(), 42, triagearr.DeleteOpts{DeleteFiles: true, AddImportExclusion: true})
	require.NoError(t, err)
	require.Equal(t, int64(1), srv.State().EpisodeFileDeletes())

	files, err := c.ListMediaFiles(context.Background(), triagearr.MediaID(1))
	require.NoError(t, err)
	require.Empty(t, files)
}

func TestDeleteMediaFile_QueryAndPath(t *testing.T) {
	var seen struct{ method, path, query string }
	c := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.method, seen.path, seen.query = r.Method, r.URL.Path, r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
	}))
	err := c.DeleteMediaFile(context.Background(), 42, triagearr.DeleteOpts{DeleteFiles: true, AddImportExclusion: true})
	require.NoError(t, err)
	require.Equal(t, http.MethodDelete, seen.method)
	require.Equal(t, "/api/v3/episodefile/42", seen.path)
	require.Contains(t, seen.query, "deleteFiles=true")
	require.Contains(t, seen.query, "addImportExclusion=true")
}

func TestDeleteMediaFile_404_HardFail(t *testing.T) {
	c := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "gone", http.StatusNotFound)
	}))
	err := c.DeleteMediaFile(context.Background(), 1, triagearr.DeleteOpts{DeleteFiles: true})
	require.Error(t, err)
	require.False(t, errors.Is(err, triagearr.ErrTransient))
}

func TestDeleteMediaFile_500_Transient(t *testing.T) {
	c := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	err := c.DeleteMediaFile(context.Background(), 1, triagearr.DeleteOpts{DeleteFiles: true})
	require.Error(t, err)
	require.True(t, errors.Is(err, triagearr.ErrTransient))
}

func TestNew_Validations(t *testing.T) {
	_, err := whisparr_v2.New(whisparr_v2.Options{})
	require.Error(t, err)
	_, err = whisparr_v2.New(whisparr_v2.Options{Name: "x"})
	require.Error(t, err)
	_, err = whisparr_v2.New(whisparr_v2.Options{Name: "x", BaseURL: "http://x"})
	require.Error(t, err)
}
