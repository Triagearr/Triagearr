package arrclient

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
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
