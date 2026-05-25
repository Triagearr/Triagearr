package sonarr_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Triagearr/Triagearr/internal/clients/sonarr"
	"github.com/Triagearr/Triagearr/internal/triagearr"
)

func newClient(t *testing.T, handler http.Handler) *sonarr.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c, err := sonarr.New(sonarr.Options{
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

func TestHealthCheck_Unauthorized(t *testing.T) {
	c := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusUnauthorized)
	}))
	err := c.HealthCheck(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "401")
}

func TestListMedia(t *testing.T) {
	c := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/series":
			_, _ = w.Write([]byte(`[
				{"id":1,"title":"S1","path":"/tv/S1","tags":[1,2],"statistics":{"sizeOnDisk":1024}},
				{"id":2,"title":"S2","path":"/tv/S2","tags":[],"statistics":{"sizeOnDisk":2048}}
			]`))
		case "/api/v3/tag":
			_, _ = w.Write([]byte(`[
				{"id":1,"label":"keep"},
				{"id":2,"label":"4k"}
			]`))
		default:
			http.NotFound(w, r)
		}
	}))
	items, err := c.ListMedia(context.Background())
	require.NoError(t, err)
	require.Len(t, items, 2)
	require.Equal(t, "S1", items[0].Title)
	require.Equal(t, []string{"keep", "4k"}, items[0].Tags)
	require.Equal(t, int64(2048), items[1].Size)
	require.Equal(t, triagearr.ArrTypeSonarr, items[0].ArrType)
}

func TestDeleteMediaFile_OK(t *testing.T) {
	var seen struct {
		method string
		path   string
		query  string
		apiKey string
	}
	c := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.method = r.Method
		seen.path = r.URL.Path
		seen.query = r.URL.RawQuery
		seen.apiKey = r.Header.Get("X-Api-Key")
		w.WriteHeader(http.StatusOK)
	}))
	err := c.DeleteMediaFile(context.Background(), 42, triagearr.DeleteOpts{DeleteFiles: true, AddImportExclusion: true})
	require.NoError(t, err)
	require.Equal(t, http.MethodDelete, seen.method)
	require.Equal(t, "/api/v3/episodefile/42", seen.path)
	require.Contains(t, seen.query, "deleteFiles=true")
	require.Contains(t, seen.query, "addImportExclusion=true")
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
	_, err := sonarr.New(sonarr.Options{})
	require.Error(t, err)
	_, err = sonarr.New(sonarr.Options{Name: "x"})
	require.Error(t, err)
	_, err = sonarr.New(sonarr.Options{Name: "x", BaseURL: "http://x"})
	require.Error(t, err)
}
