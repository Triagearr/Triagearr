package radarr_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Triagearr/Triagearr/internal/clients/arr/radarr"
	"github.com/Triagearr/Triagearr/internal/triagearr"
)

func newClient(t *testing.T, handler http.Handler) *radarr.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c, err := radarr.New(radarr.Options{
		Name: "main", BaseURL: srv.URL, APIKey: "k", Poll: true,
	})
	require.NoError(t, err)
	return c
}

func TestHealthCheck_OK(t *testing.T) {
	c := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v3/health", r.URL.Path)
		require.Equal(t, "k", r.Header.Get("X-Api-Key"))
		_, _ = w.Write([]byte(`[]`))
	}))
	require.NoError(t, c.HealthCheck(context.Background()))
}

func TestListMedia(t *testing.T) {
	c := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/movie":
			_, _ = w.Write([]byte(`[
				{"id":42,"title":"Foo","path":"/movies/Foo","sizeOnDisk":1048576,"tags":[1]}
			]`))
		case "/api/v3/tag":
			_, _ = w.Write([]byte(`[{"id":1,"label":"keep"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	items, err := c.ListMedia(context.Background())
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, triagearr.MediaID(42), items[0].ID)
	require.Equal(t, "Foo", items[0].Title)
	require.Equal(t, []string{"keep"}, items[0].Tags)
	require.Equal(t, triagearr.ArrTypeRadarr, items[0].ArrType)
}

func TestType(t *testing.T) {
	c := newClient(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	require.Equal(t, triagearr.ArrTypeRadarr, c.Type())
}

func TestListMediaFiles(t *testing.T) {
	c := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v3/moviefile", r.URL.Path)
		require.Equal(t, "7", r.URL.Query().Get("movieId"))
		_, _ = w.Write([]byte(`[
			{"id":20,"movieId":7,"path":"/movies/M/m.mkv","size":900}
		]`))
	}))
	files, err := c.ListMediaFiles(context.Background(), triagearr.MediaID(7))
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.Equal(t, int64(20), files[0].FileID)
	require.Equal(t, triagearr.MediaID(7), files[0].MediaID)
	require.Equal(t, "/movies/M/m.mkv", files[0].Path)
	require.Equal(t, triagearr.ArrTypeRadarr, files[0].ArrType)
}

func TestDeleteMediaFile_OK(t *testing.T) {
	var seen struct {
		method, path, query, apiKey string
	}
	c := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.method = r.Method
		seen.path = r.URL.Path
		seen.query = r.URL.RawQuery
		seen.apiKey = r.Header.Get("X-Api-Key")
		w.WriteHeader(http.StatusOK)
	}))
	err := c.DeleteMediaFile(context.Background(), 7, triagearr.DeleteOpts{DeleteFiles: true})
	require.NoError(t, err)
	require.Equal(t, http.MethodDelete, seen.method)
	require.Equal(t, "/api/v3/moviefile/7", seen.path)
	require.Contains(t, seen.query, "deleteFiles=true")
	require.Equal(t, "k", seen.apiKey)
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
	_, err := radarr.New(radarr.Options{})
	require.Error(t, err)
}
