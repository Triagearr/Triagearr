package server_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Triagearr/Triagearr/internal/actor"
	"github.com/Triagearr/Triagearr/internal/decider"
	"github.com/Triagearr/Triagearr/internal/server"
	"github.com/Triagearr/Triagearr/internal/store"
	"github.com/Triagearr/Triagearr/internal/triagearr"
)

// blockingClient stalls the destructive pipeline inside TorrentFiles until
// release is closed, so a test can observe a live run while it is in flight and
// fire a second (rejected) trigger against the held single-run slot.
type blockingClient struct{ release chan struct{} }

func (c *blockingClient) TorrentFiles(ctx context.Context, _ triagearr.Hash) ([]triagearr.TorrentFile, error) {
	select {
	case <-c.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return []triagearr.TorrentFile{{Name: "x"}}, nil
}

func (c *blockingClient) Delete(_ context.Context, _ triagearr.Hash, _ triagearr.DeleteOpts) error {
	return nil
}

// buildSrvWithActor wires a live-capable server whose Actor blocks in the qBit
// files step until the returned release func is called. seed() gives one
// candidate (h1) over a 5%-free volume, so a live run elects it.
func buildSrvWithActor(t *testing.T) (http.Handler, *store.Store, func()) {
	t.Helper()
	s := testStore(t)
	seed(t, s)
	vol := decider.Volume{Name: "data", Path: "/data", TargetFreePercent: 20}
	bc := &blockingClient{release: make(chan struct{})}
	act := actor.New(actor.Options{
		Source:  s,
		Client:  bc,
		Deleter: func(string) (triagearr.FileDeleter, bool) { return nil, false },
	})
	srv := server.New(server.Options{
		Bind:   "127.0.0.1:0",
		APIKey: testAPIKey,
		Store:  s,
	}, &server.Engine{
		Decider:    decider.New(s),
		Volume:     func() decider.Volume { return vol },
		DaemonLive: true,
		Actor:      act,
	})
	var once bool
	release := func() {
		if !once {
			once = true
			close(bc.release)
		}
	}
	t.Cleanup(release)
	return srv.Handler(), s, release
}

func TestPostRun_Live_Async_ReturnsInFlight(t *testing.T) {
	h, s, release := buildSrvWithActor(t)

	w := httptest.NewRecorder()
	h.ServeHTTP(w, authedReq(http.MethodPost, "/api/v1/runs", `{"mode":"live"}`))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, "live", body["mode"])
	// The Actor is still blocked, so the run must not be terminal yet.
	require.Contains(t, []any{"pending", "running"}, body["status"], "run should be returned in flight")

	// Releasing the pipeline lets the background goroutine finish the run.
	release()
	require.Eventually(t, func() bool {
		run, _, err := s.GetRun(context.Background(), int64(body["run_id"].(float64)))
		return err == nil && run.Status == "completed"
	}, 3*time.Second, 10*time.Millisecond, "run never reached completed after release")
}

func TestPostRun_Live_ConcurrentRejected409(t *testing.T) {
	h, _, release := buildSrvWithActor(t)

	// First live run acquires the single-run slot and blocks in the Actor.
	w1 := httptest.NewRecorder()
	h.ServeHTTP(w1, authedReq(http.MethodPost, "/api/v1/runs", `{"mode":"live"}`))
	require.Equal(t, http.StatusOK, w1.Code, w1.Body.String())

	// Second live run while the first holds the slot → 409.
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, authedReq(http.MethodPost, "/api/v1/runs", `{"mode":"live"}`))
	require.Equal(t, http.StatusConflict, w2.Code, w2.Body.String())

	release()
}

func TestPreviewRun_DoesNotPersist(t *testing.T) {
	_, s, h := buildSrv(t, "")

	w := httptest.NewRecorder()
	h.ServeHTTP(w, authedReq(http.MethodGet, "/api/v1/runs/preview", ""))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var body struct {
		EstimatedFreedBytes int64 `json:"estimated_freed_bytes"`
		Candidates          []struct {
			TorrentHash string `json:"torrent_hash"`
		} `json:"candidates"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.NotEmpty(t, body.Candidates, "preview should elect the seeded candidate")

	// Preview is read-only: no run row may have been written.
	runs, err := s.ListRuns(context.Background(), store.ListRunsOpts{})
	require.NoError(t, err)
	require.Len(t, runs, 0)
}
