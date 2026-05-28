package qbit

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

// newTestClient builds a Client with backoff disabled. Internal test → it can
// reach the unexported sleep hook.
func newTestClient(t *testing.T, url string) *Client {
	t.Helper()
	c, err := New(Options{BaseURL: url})
	require.NoError(t, err)
	c.sleep = func(time.Duration) {}
	return c
}

func TestGetJSON_RetriesRetryableStatus(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if hits.Add(1) <= 2 {
			http.Error(w, "blip", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(srv.Close)

	c := newTestClient(t, srv.URL)
	_, err := c.ListTorrents(context.Background())
	require.NoError(t, err)
	require.Equal(t, int32(3), hits.Load())
}

func TestGetJSON_403ResetsSessionNotRetried(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		http.Error(w, "no cookie", http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)

	c := newTestClient(t, srv.URL)
	_, err := c.ListTorrents(context.Background())
	require.Error(t, err)
	require.Equal(t, int32(1), hits.Load(), "403 is not retried in-loop")
	c.mu.Lock()
	require.False(t, c.loggedIn, "403 forces re-login on the next call")
	c.mu.Unlock()
}

func TestDelete_5xxNotSelfRetried(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		http.Error(w, "boom", http.StatusBadGateway)
	}))
	t.Cleanup(srv.Close)

	c := newTestClient(t, srv.URL)
	err := c.Delete(context.Background(), "abc", triagearr.DeleteOpts{DeleteFiles: true})
	require.Error(t, err)
	require.True(t, errors.Is(err, triagearr.ErrTransient), "writes still mark 5xx transient for the Actor")
	require.Equal(t, int32(1), hits.Load(), "the Actor owns write retries, not the client")
}
