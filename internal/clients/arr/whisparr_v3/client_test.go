package whisparr_v3_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	whisparr_v3 "github.com/Triagearr/Triagearr/internal/clients/arr/whisparr_v3"
	"github.com/Triagearr/Triagearr/internal/clients/arr/whisparr_v3/fake"
	"github.com/Triagearr/Triagearr/internal/triagearr"
)

// newFakeClient wires a real client against the in-memory fake Whisparr v3.
func newFakeClient(t *testing.T) (*whisparr_v3.Client, *fake.Server) {
	t.Helper()
	srv := fake.New(fake.Options{APIKey: "k"})
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	c, err := whisparr_v3.New(whisparr_v3.Options{
		Name: "whisparr3", BaseURL: ts.URL, APIKey: "k", Poll: true, Act: true,
	})
	require.NoError(t, err)
	return c, srv
}

func newClient(t *testing.T, handler http.Handler) *whisparr_v3.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c, err := whisparr_v3.New(whisparr_v3.Options{
		Name: "whisparr3", BaseURL: srv.URL, APIKey: "k", Poll: true,
	})
	require.NoError(t, err)
	return c
}

func TestType(t *testing.T) {
	c, _ := newFakeClient(t)
	require.Equal(t, triagearr.ArrTypeWhisparrV3, c.Type())
}

func TestHealthCheck_OK(t *testing.T) {
	c, _ := newFakeClient(t)
	require.NoError(t, c.HealthCheck(context.Background()))
}

func TestListMedia(t *testing.T) {
	c, srv := newFakeClient(t)
	srv.State().AddTag(fake.Tag{ID: 1, Label: "keep"})
	srv.State().AddMovie(fake.Movie{
		ID: 42, Title: "Foo", TitleSlug: "foo", Path: "/movies/Foo", SizeOnDisk: 1048576, Tags: []int{1},
	})

	items, err := c.ListMedia(context.Background())
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, triagearr.MediaID(42), items[0].ID)
	require.Equal(t, "Foo", items[0].Title)
	require.Equal(t, int64(1048576), items[0].Size)
	require.Equal(t, []string{"keep"}, items[0].Tags)
	require.Equal(t, triagearr.ArrTypeWhisparrV3, items[0].ArrType)
}

func TestListMediaFiles(t *testing.T) {
	c, srv := newFakeClient(t)
	srv.State().AddMovie(fake.Movie{
		ID: 7, Title: "M", Path: "/movies/M",
		Files: []fake.MovieFile{{ID: 20, MovieID: 7, Path: "/movies/M/m.mkv", Size: 900}},
	})

	files, err := c.ListMediaFiles(context.Background(), triagearr.MediaID(7))
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.Equal(t, int64(20), files[0].FileID)
	require.Equal(t, triagearr.MediaID(7), files[0].MediaID)
	require.Equal(t, "/movies/M/m.mkv", files[0].Path)
	require.Equal(t, triagearr.ArrTypeWhisparrV3, files[0].ArrType)
}

func TestListImports(t *testing.T) {
	c, srv := newFakeClient(t)
	srv.State().AddHistory(fake.HistoryRecord{
		ID: 5, Date: time.Now().UTC(), EventType: "downloadFolderImported", DownloadID: "ABC123",
		Data: fake.HistoryRecordData{FileID: "20", DownloadClient: "qBittorrent", ImportedPath: "/movies/M/m.mkv"},
	})

	recs, err := c.ListImports(context.Background(), 0)
	require.NoError(t, err)
	require.Len(t, recs, 1)
	require.Equal(t, int64(5), recs[0].HistoryID)
	require.Equal(t, int64(20), recs[0].FileID)
	require.Equal(t, triagearr.Hash("abc123"), recs[0].DownloadID)
}

func TestDeleteMediaFile_FakeRemoves(t *testing.T) {
	c, srv := newFakeClient(t)
	srv.State().AddMovie(fake.Movie{
		ID: 7, Title: "M", Path: "/movies/M",
		Files: []fake.MovieFile{{ID: 20, MovieID: 7, Path: "/movies/M/m.mkv", Size: 900}},
	})

	err := c.DeleteMediaFile(context.Background(), 20, triagearr.DeleteOpts{DeleteFiles: true})
	require.NoError(t, err)
	require.Equal(t, int64(1), srv.State().MovieFileDeletes())

	files, err := c.ListMediaFiles(context.Background(), triagearr.MediaID(7))
	require.NoError(t, err)
	require.Empty(t, files)
}

func TestDeleteMediaFile_QueryAndPath(t *testing.T) {
	var seen struct{ method, path, query string }
	c := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.method, seen.path, seen.query = r.Method, r.URL.Path, r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
	}))
	err := c.DeleteMediaFile(context.Background(), 7, triagearr.DeleteOpts{DeleteFiles: true})
	require.NoError(t, err)
	require.Equal(t, http.MethodDelete, seen.method)
	require.Equal(t, "/api/v3/moviefile/7", seen.path)
	require.Contains(t, seen.query, "deleteFiles=true")
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
	_, err := whisparr_v3.New(whisparr_v3.Options{})
	require.Error(t, err)
	_, err = whisparr_v3.New(whisparr_v3.Options{Name: "x"})
	require.Error(t, err)
	_, err = whisparr_v3.New(whisparr_v3.Options{Name: "x", BaseURL: "http://x"})
	require.Error(t, err)
}
