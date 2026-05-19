package radarr_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Triagearr/Triagearr/internal/clients/radarr"
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

func TestDeleteMedia_NotImplemented(t *testing.T) {
	c := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	require.Error(t, c.DeleteMedia(context.Background(), 1, triagearr.DeleteOpts{}))
}

func TestNew_Validations(t *testing.T) {
	_, err := radarr.New(radarr.Options{})
	require.Error(t, err)
}
