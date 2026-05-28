package arrclient

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Triagearr/Triagearr/internal/triagearr"
)

// newTestClient builds a BaseClient against srv with backoff disabled so retry
// scenarios run instantly. Internal test → it can reach the unexported sleep.
func newTestClient(t *testing.T, url string) *BaseClient {
	t.Helper()
	c, err := New(Options{Label: "test", Name: "main", BaseURL: url, APIKey: "k", Poll: true})
	require.NoError(t, err)
	c.sleep = func(time.Duration) {}
	return c
}

func TestGet_RetriesRetryableStatus(t *testing.T) {
	for _, code := range []int{
		http.StatusTooManyRequests,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout,
	} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			var hits atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				// Fail twice, then succeed — exercises the full default budget.
				if hits.Add(1) <= 2 {
					http.Error(w, "blip", code)
					return
				}
				_, _ = w.Write([]byte(`[]`))
			}))
			t.Cleanup(srv.Close)

			c := newTestClient(t, srv.URL)
			var out []any
			require.NoError(t, c.Get(context.Background(), "/x", &out))
			require.Equal(t, int32(3), hits.Load())
		})
	}
}

func TestGet_500NotRetried(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	c := newTestClient(t, srv.URL)
	err := c.Get(context.Background(), "/x", nil)
	require.Error(t, err)
	require.False(t, errors.Is(err, triagearr.ErrTransient))
	require.Equal(t, int32(1), hits.Load(), "500 is a hard failure, not retried")
}

func TestGet_404NotRetried(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		http.Error(w, "gone", http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	c := newTestClient(t, srv.URL)
	err := c.Get(context.Background(), "/x", nil)
	require.Error(t, err)
	require.False(t, errors.Is(err, triagearr.ErrTransient))
	require.Equal(t, int32(1), hits.Load())
}

func TestGet_TransportErrorRetried(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close() // nothing is listening now → every Do is a transport error

	c := newTestClient(t, url)
	err := c.Get(context.Background(), "/x", nil)
	require.Error(t, err)
	require.True(t, errors.Is(err, triagearr.ErrTransient), "transport failures are transient and retried")
}

func TestGet_CancelledCtxShortCircuits(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		http.Error(w, "blip", http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)

	c := newTestClient(t, srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := c.Get(ctx, "/x", nil)
	require.Error(t, err)
	require.Zero(t, hits.Load(), "no request once ctx is cancelled")
}

func TestGet_DecodesBodyAndSendsAuth(t *testing.T) {
	var gotKey, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("X-Api-Key")
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{"id":7,"title":"hi"}`))
	}))
	t.Cleanup(srv.Close)

	c := newTestClient(t, srv.URL)
	var out struct {
		ID    int    `json:"id"`
		Title string `json:"title"`
	}
	require.NoError(t, c.Get(context.Background(), "/api/v3/x", &out))
	require.Equal(t, 7, out.ID)
	require.Equal(t, "hi", out.Title)
	require.Equal(t, "k", gotKey, "X-Api-Key header is sent")
	require.Equal(t, "/api/v3/x", gotPath)
}

func TestGet_MalformedBodyIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{not json`))
	}))
	t.Cleanup(srv.Close)

	c := newTestClient(t, srv.URL)
	var out map[string]any
	err := c.Get(context.Background(), "/x", &out)
	require.Error(t, err)
	require.False(t, errors.Is(err, triagearr.ErrTransient), "a decode failure is not transient")
}

func TestNew_Validation(t *testing.T) {
	base := Options{Label: "sonarr", Name: "main", BaseURL: "http://h", APIKey: "k"}
	missing := func(mut func(*Options)) Options {
		o := base
		mut(&o)
		return o
	}
	tests := map[string]Options{
		"no label":   missing(func(o *Options) { o.Label = "" }),
		"no name":    missing(func(o *Options) { o.Name = "" }),
		"no baseURL": missing(func(o *Options) { o.BaseURL = "" }),
		"no apiKey":  missing(func(o *Options) { o.APIKey = "" }),
	}
	for name, opts := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := New(opts)
			require.Error(t, err)
		})
	}
}

func TestNew_DefaultsAndGetters(t *testing.T) {
	c, err := New(Options{
		Label: "sonarr", Name: "main", BaseURL: "http://host:8989/", APIKey: "k",
		Poll: true, Act: true,
	})
	require.NoError(t, err)
	require.Equal(t, "main", c.Name())
	require.True(t, c.Poll())
	require.True(t, c.Act())
	require.Equal(t, "http://host:8989", c.BaseURL(), "trailing slash is trimmed")
	require.Equal(t, defaultTimeout, c.http.Timeout, "zero Timeout falls back to default")
}

func TestDeleteFile_SuccessSendsOptsAsQuery(t *testing.T) {
	for _, code := range []int{http.StatusOK, http.StatusNoContent} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			var gotPath, gotKey string
			var gotQuery url.Values
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, http.MethodDelete, r.Method)
				gotPath = r.URL.Path
				gotQuery = r.URL.Query()
				gotKey = r.Header.Get("X-Api-Key")
				w.WriteHeader(code)
			}))
			t.Cleanup(srv.Close)

			c := newTestClient(t, srv.URL)
			err := c.DeleteFile(context.Background(), "/api/v3/episodefile", 42,
				triagearr.DeleteOpts{DeleteFiles: true, AddImportExclusion: true})
			require.NoError(t, err)
			require.Equal(t, "/api/v3/episodefile/42", gotPath)
			require.Equal(t, "true", gotQuery.Get("deleteFiles"))
			require.Equal(t, "true", gotQuery.Get("addImportExclusion"))
			require.Equal(t, "k", gotKey)
		})
	}
}

func TestDeleteFile_NoOptsOmitsQuery(t *testing.T) {
	var gotRawQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRawQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	c := newTestClient(t, srv.URL)
	require.NoError(t, c.DeleteFile(context.Background(), "/api/v3/moviefile", 1, triagearr.DeleteOpts{}))
	require.Empty(t, gotRawQuery, "no query string when no opts are set")
}

func TestDeleteFile_5xxIsTransient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	c := newTestClient(t, srv.URL)
	err := c.DeleteFile(context.Background(), "/api/v3/episodefile", 9, triagearr.DeleteOpts{})
	require.Error(t, err)
	require.True(t, errors.Is(err, triagearr.ErrTransient), "5xx on delete is retryable for the Actor")
}

func TestDeleteFile_4xxIsHardFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "gone", http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	c := newTestClient(t, srv.URL)
	err := c.DeleteFile(context.Background(), "/api/v3/episodefile", 9, triagearr.DeleteOpts{})
	require.Error(t, err)
	require.False(t, errors.Is(err, triagearr.ErrTransient), "404 is a hard failure")
}

func TestFetchTags(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v3/tag", r.URL.Path)
		_, _ = w.Write([]byte(`[{"id":1,"label":"keep"},{"id":2,"label":"archive"}]`))
	}))
	t.Cleanup(srv.Close)

	c := newTestClient(t, srv.URL)
	tags, err := c.FetchTags(context.Background())
	require.NoError(t, err)
	require.Equal(t, map[int]string{1: "keep", 2: "archive"}, tags)
}

func TestResolveTags(t *testing.T) {
	labels := map[int]string{1: "keep", 2: "archive"}
	require.Equal(t, []string{"keep", "archive"}, ResolveTags([]int{1, 2}, labels))
	require.Equal(t, []string{"keep"}, ResolveTags([]int{1, 99}, labels), "unknown ids are dropped")
	require.Empty(t, ResolveTags(nil, labels))
}
